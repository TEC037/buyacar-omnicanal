package rag

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	ragfixtures "buyacar.com.co/omnicanal/fixtures/rag"
)

// IndexChunks indexes a batch of pre-computed chunks directly into the InMemoryEngine.
func (e *InMemoryEngine) IndexChunks(ctx context.Context, chunks []Chunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	// 1. Compute missing embeddings outside the engine mutex
	for i := range chunks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(chunks[i].Vector) == 0 && e.embedder != nil {
			text := chunks[i].Title + "\n" + chunks[i].Content
			vec, err := e.embedder.Embed(ctx, text)
			if err != nil {
				return fmt.Errorf("failed to embed chunk %s: %w", chunks[i].ID, err)
			}
			chunks[i].Vector = vec
		}
	}

	// 2. Insert chunks, update BM25 index and backed numbers atomically
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, chunk := range chunks {
		e.chunks[chunk.ID] = chunk
		e.bm25.AddDocument(chunk.ID, chunk.Title+" "+chunk.Title+" "+chunk.Title+" "+chunk.Content)

		// Record backed numbers
		for _, num := range chunk.BackedNumbers {
			e.backedNumbers[num] = struct{}{}
		}

		// Update doc to chunk map
		docChunks := e.docChunks[chunk.DocID]
		found := false
		for _, cid := range docChunks {
			if cid == chunk.ID {
				found = true
				break
			}
		}
		if !found {
			e.docChunks[chunk.DocID] = append(docChunks, chunk.ID)
		}
	}

	return nil
}

// IndexChunk indexes a single chunk into the InMemoryEngine.
func (e *InMemoryEngine) IndexChunk(ctx context.Context, chunk Chunk) error {
	return e.IndexChunks(ctx, []Chunk{chunk})
}

// InferCategoryAndDocID derives standard docID and category from a fixture filename.
func InferCategoryAndDocID(filename string) (docID, category string) {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	cleanName := strings.TrimSuffix(base, ext)

	lower := strings.ToLower(cleanName)
	switch {
	case strings.Contains(lower, "vehicle") || strings.Contains(lower, "vehiculo"):
		category = "catalog"
		if ext == ".json" {
			docID = "vehicles_json"
		} else {
			docID = "vehicles_md"
		}
	case strings.Contains(lower, "warranty") || strings.Contains(lower, "garantia"):
		category = "warranty"
		docID = cleanName
	case strings.Contains(lower, "credit") || strings.Contains(lower, "credito"):
		category = "credit"
		docID = cleanName
	case strings.Contains(lower, "faq") || strings.Contains(lower, "pregunta"):
		category = "faq"
		docID = cleanName
	default:
		category = "general"
		docID = cleanName
	}
	return docID, category
}

// LoadEmbeddedFixtures loads and indexes all standard fixtures from the embedded ragfixtures.Files FS.
func LoadEmbeddedFixtures(ctx context.Context, engine *InMemoryEngine) ([]Chunk, error) {
	return LoadFixturesFromFS(ctx, engine, ragfixtures.Files, ".")
}

// LoadFixturesFromFS reads, chunks and optionally indexes all JSON and Markdown documents from an fs.FS.
func LoadFixturesFromFS(ctx context.Context, engine *InMemoryEngine, fsys fs.FS, rootDir string) ([]Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if rootDir == "" {
		rootDir = "."
	}

	entries, err := fs.ReadDir(fsys, rootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read fixtures directory %s: %w", rootDir, err)
	}

	// Sort entries deterministically
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	chunker := NewSemanticChunker()
	var allChunks []Chunk

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
			continue
		}

		filePath := path.Join(rootDir, name)
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read fixture file %s: %w", filePath, err)
		}

		docID, category := InferCategoryAndDocID(name)
		chunks, err := chunker.ChunkDocument(docID, category, name, data)
		if err != nil {
			return nil, fmt.Errorf("failed to chunk fixture %s: %w", name, err)
		}

		allChunks = append(allChunks, chunks...)
	}

	if engine != nil && len(allChunks) > 0 {
		if err := engine.IndexChunks(ctx, allChunks); err != nil {
			return nil, fmt.Errorf("failed to index fixtures into engine: %w", err)
		}
	}

	return allChunks, nil
}

// LoadFixturesFromDisk loads and optionally indexes fixtures from a physical filesystem directory.
func LoadFixturesFromDisk(ctx context.Context, engine *InMemoryEngine, dirPath string) ([]Chunk, error) {
	if dirPath == "" {
		// Attempt standard locations
		candidates := []string{
			"fixtures/rag",
			"../fixtures/rag",
			"../../fixtures/rag",
			"app_build/fixtures/rag",
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && info.IsDir() {
				dirPath = c
				break
			}
		}
	}

	if dirPath == "" {
		return nil, fmt.Errorf("fixtures directory not found on disk")
	}

	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", dirPath, err)
	}

	return LoadFixturesFromFS(ctx, engine, os.DirFS(absDir), ".")
}
