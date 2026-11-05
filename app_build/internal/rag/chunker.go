package rag

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ChunkingConfig defines deterministic boundary and sizing constraints for semantic chunking.
type ChunkingConfig struct {
	MaxTokensPerChunk int
	MinTokensPerChunk int
	MaxCharsPerChunk  int
}

// DefaultChunkingConfig returns production-ready deterministic limits.
func DefaultChunkingConfig() ChunkingConfig {
	return ChunkingConfig{
		MaxTokensPerChunk: 250,
		MinTokensPerChunk: 15,
		MaxCharsPerChunk:  1200,
	}
}

// SemanticChunker segments Markdown and JSON documents deterministically into semantic Chunks.
type SemanticChunker struct {
	cfg ChunkingConfig
}

// NewSemanticChunker creates a new SemanticChunker with optional custom config.
func NewSemanticChunker(cfg ...ChunkingConfig) *SemanticChunker {
	c := DefaultChunkingConfig()
	if len(cfg) > 0 {
		c = cfg[0]
	}
	// Sanity defaults
	if c.MaxTokensPerChunk <= 0 {
		c.MaxTokensPerChunk = 250
	}
	if c.MinTokensPerChunk < 0 {
		c.MinTokensPerChunk = 15
	}
	if c.MaxCharsPerChunk <= 0 {
		c.MaxCharsPerChunk = 1200
	}
	return &SemanticChunker{cfg: c}
}

// VehicleFixture represents the schema of items in vehicles.json.
type VehicleFixture struct {
	ID            string   `json:"id"`
	Brand         string   `json:"brand"`
	Model         string   `json:"model"`
	Year          int      `json:"year"`
	PriceCOP      int64    `json:"price_cop"`
	MileageKm     int      `json:"mileage_km"`
	Transmission  string   `json:"transmission"`
	Engine        string   `json:"engine"`
	Traction      string   `json:"traction"`
	Color         string   `json:"color"`
	BodyType      string   `json:"body_type"`
	PlateLastChar string   `json:"plate_last_char"`
	City          string   `json:"city"`
	Department    string   `json:"department"`
	Features      []string `json:"features"`
	SOATValid     bool     `json:"soat_valid"`
	TecnoValid    bool     `json:"tecno_valid"`
	Status        string   `json:"status"`
	OfficialURL   string   `json:"official_url"`
}

// ChunkDocument auto-detects format (Markdown vs JSON) and performs deterministic semantic chunking.
func (c *SemanticChunker) ChunkDocument(docID, category, filename string, content []byte) ([]Chunk, error) {
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".json") {
		return c.ChunkJSON(docID, category, content)
	}
	if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") || strings.HasSuffix(lower, ".txt") {
		return c.ChunkMarkdown(docID, category, content)
	}
	// Content-based heuristic
	trimmed := strings.TrimSpace(string(content))
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		if chunks, err := c.ChunkJSON(docID, category, content); err == nil && len(chunks) > 0 {
			return chunks, nil
		}
	}
	return c.ChunkMarkdown(docID, category, content)
}

// ChunkMarkdown parses markdown documents by headers (#, ##, ###) and paragraph boundaries.
func (c *SemanticChunker) ChunkMarkdown(docID, category string, content []byte) ([]Chunk, error) {
	text := strings.TrimSpace(string(content))
	if text == "" {
		return nil, nil
	}

	lines := strings.Split(text, "\n")
	var docTitle string
	type rawSection struct {
		level   int
		title   string
		content []string
	}

	var sections []rawSection
	currentSection := rawSection{level: 0, title: "", content: nil}

	for _, rawLine := range lines {
		line := strings.TrimRightFunc(rawLine, unicode.IsSpace)
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, "# ") {
			if docTitle == "" {
				docTitle = strings.TrimPrefix(trimmedLine, "# ")
				docTitle = strings.TrimSpace(docTitle)
			}
			// If we had accumulated content before this header, flush it
			if len(currentSection.content) > 0 {
				sections = append(sections, currentSection)
				currentSection = rawSection{level: 1, title: docTitle, content: nil}
			} else {
				currentSection.level = 1
				currentSection.title = docTitle
			}
			continue
		}

		if strings.HasPrefix(trimmedLine, "## ") {
			if len(currentSection.content) > 0 || currentSection.title != "" {
				sections = append(sections, currentSection)
			}
			h2Title := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "## "))
			currentSection = rawSection{level: 2, title: h2Title, content: nil}
			continue
		}

		if strings.HasPrefix(trimmedLine, "### ") {
			if len(currentSection.content) > 0 || currentSection.title != "" {
				sections = append(sections, currentSection)
			}
			h3Title := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "### "))
			currentSection = rawSection{level: 3, title: h3Title, content: nil}
			continue
		}

		currentSection.content = append(currentSection.content, line)
	}

	if len(currentSection.content) > 0 || currentSection.title != "" {
		sections = append(sections, currentSection)
	}

	if docTitle == "" {
		docTitle = docID
	}

	var chunks []Chunk
	chunkIndex := 0

	for _, sec := range sections {
		body := strings.TrimSpace(strings.Join(sec.content, "\n"))
		if body == "" && sec.title == "" {
			continue
		}

		secTitle := sec.title
		if secTitle == "" {
			secTitle = docTitle
		}

		// Full chunk text candidate
		fullCandidate := secTitle
		if body != "" {
			fullCandidate = secTitle + "\n\n" + body
		}

		// If section fits within constraints, keep as a single chunk
		if len(fullCandidate) <= c.cfg.MaxCharsPerChunk && estimateTokens(fullCandidate) <= c.cfg.MaxTokensPerChunk {
			chunk := c.buildChunk(docID, category, secTitle, body, docTitle, chunkIndex)
			chunks = append(chunks, chunk)
			chunkIndex++
			continue
		}

		// Otherwise, break down by paragraphs deterministically
		paras := strings.Split(body, "\n\n")
		var currParas []string
		subIndex := 1

		for _, p := range paras {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}

			// Test candidate length if we append p
			testBody := strings.Join(append(currParas, p), "\n\n")
			testFull := secTitle + "\n\n" + testBody

			if len(currParas) > 0 && (len(testFull) > c.cfg.MaxCharsPerChunk || estimateTokens(testFull) > c.cfg.MaxTokensPerChunk) {
				// Flush accumulated chunk
				chunkBody := strings.Join(currParas, "\n\n")
				subTitle := fmt.Sprintf("%s (Parte %d)", secTitle, subIndex)
				chunks = append(chunks, c.buildChunk(docID, category, subTitle, chunkBody, docTitle, chunkIndex))
				chunkIndex++
				subIndex++
				currParas = []string{p}
			} else {
				// Check if even a single paragraph exceeds max constraints
				if len(currParas) == 0 && (len(p) > c.cfg.MaxCharsPerChunk || estimateTokens(p) > c.cfg.MaxTokensPerChunk) {
					// Split large single paragraph by sentences
					sentenceChunks := c.splitBySentences(p, c.cfg.MaxCharsPerChunk)
					for _, sc := range sentenceChunks {
						subTitle := fmt.Sprintf("%s (Parte %d)", secTitle, subIndex)
						chunks = append(chunks, c.buildChunk(docID, category, subTitle, sc, docTitle, chunkIndex))
						chunkIndex++
						subIndex++
					}
					currParas = nil
				} else {
					currParas = append(currParas, p)
				}
			}
		}

		if len(currParas) > 0 {
			chunkBody := strings.Join(currParas, "\n\n")
			subTitle := secTitle
			if subIndex > 1 {
				subTitle = fmt.Sprintf("%s (Parte %d)", secTitle, subIndex)
			}
			chunks = append(chunks, c.buildChunk(docID, category, subTitle, chunkBody, docTitle, chunkIndex))
			chunkIndex++
		}
	}

	return chunks, nil
}

// ChunkJSON parses JSON documents, supporting structured vehicle inventories and general JSON.
func (c *SemanticChunker) ChunkJSON(docID, category string, content []byte) ([]Chunk, error) {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return nil, nil
	}

	// 1. Try specialized VehicleFixture slice
	var vehicles []VehicleFixture
	if err := json.Unmarshal(content, &vehicles); err == nil && len(vehicles) > 0 && vehicles[0].Brand != "" {
		chunks := make([]Chunk, len(vehicles))
		for i, v := range vehicles {
			cat := category
			if cat == "" {
				cat = "catalog"
			}
			title := fmt.Sprintf("%s %s %d", v.Brand, v.Model, v.Year)

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Ficha Técnica: %s %s (%d)\n", v.Brand, v.Model, v.Year))
			sb.WriteString(fmt.Sprintf("- Precio Oficial: $%d COP\n", v.PriceCOP))
			sb.WriteString(fmt.Sprintf("- Kilometraje: %d km\n", v.MileageKm))
			sb.WriteString(fmt.Sprintf("- Motor: %s\n", v.Engine))
			sb.WriteString(fmt.Sprintf("- Transmisión: %s\n", v.Transmission))
			sb.WriteString(fmt.Sprintf("- Tracción: %s\n", v.Traction))
			sb.WriteString(fmt.Sprintf("- Color: %s\n", v.Color))
			sb.WriteString(fmt.Sprintf("- Carrocería: %s\n", v.BodyType))
			sb.WriteString(fmt.Sprintf("- Placa terminada en: %s\n", v.PlateLastChar))
			sb.WriteString(fmt.Sprintf("- Ubicación: %s, %s\n", v.City, v.Department))
			if len(v.Features) > 0 {
				sb.WriteString("- Equipamiento:\n")
				for _, f := range v.Features {
					sb.WriteString(fmt.Sprintf("  * %s\n", f))
				}
			}
			if v.OfficialURL != "" {
				sb.WriteString(fmt.Sprintf("- Enlace Oficial: %s\n", v.OfficialURL))
			}

			chunkContent := sb.String()
			chunkID := fmt.Sprintf("%s_%s", docID, v.ID)
			tokens := estimateTokens(chunkContent)
			backed := extractFiveDigitNumbers(fmt.Sprintf("%s %d %d", chunkContent, v.PriceCOP, v.MileageKm))
			tags := extractDeterministicTags(title, chunkContent, cat)

			meta := map[string]string{
				"doc_id":       docID,
				"category":     cat,
				"title":        title,
				"vehicle_id":   v.ID,
				"brand":        v.Brand,
				"model":        v.Model,
				"year":         strconv.Itoa(v.Year),
				"price_cop":    strconv.FormatInt(v.PriceCOP, 10),
				"mileage_km":   strconv.Itoa(v.MileageKm),
				"transmission": v.Transmission,
				"engine":       v.Engine,
				"traction":     v.Traction,
				"status":       v.Status,
				"official_url": v.OfficialURL,
				"tags":         strings.Join(tags, ","),
				"chunk_index":  strconv.Itoa(i),
			}

			chunks[i] = Chunk{
				ID:            chunkID,
				DocID:         docID,
				Category:      cat,
				Title:         title,
				Content:       chunkContent,
				Tokens:        tokens,
				BackedNumbers: backed,
				Metadata:      meta,
			}
		}
		return chunks, nil
	}

	// 2. Try generic JSON array
	var items []map[string]any
	if err := json.Unmarshal(content, &items); err == nil && len(items) > 0 {
		chunks := make([]Chunk, len(items))
		for i, item := range items {
			cat := category
			if cat == "" {
				cat = "general"
			}
			title := fmt.Sprintf("%s Item #%d", docID, i+1)
			if t, ok := item["title"].(string); ok && t != "" {
				title = t
			} else if name, ok := item["name"].(string); ok && name != "" {
				title = name
			}

			// Format key-value pairs sorted for determinism
			keys := make([]string, 0, len(item))
			for k := range item {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			var sb strings.Builder
			for _, k := range keys {
				sb.WriteString(fmt.Sprintf("%s: %v\n", k, item[k]))
			}
			body := strings.TrimSpace(sb.String())

			chunks[i] = c.buildChunk(docID, cat, title, body, docID, i)
		}
		return chunks, nil
	}

	// 3. Try generic JSON object with sections
	var obj map[string]any
	if err := json.Unmarshal(content, &obj); err == nil && len(obj) > 0 {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var chunks []Chunk
		for i, k := range keys {
			val := obj[k]
			valBytes, _ := json.MarshalIndent(val, "", "  ")
			body := string(valBytes)
			title := fmt.Sprintf("%s - %s", docID, k)
			cat := category
			if cat == "" {
				cat = "general"
			}
			chunks = append(chunks, c.buildChunk(docID, cat, title, body, docID, i))
		}
		return chunks, nil
	}

	return nil, fmt.Errorf("unsupported JSON format for document %s", docID)
}

// buildChunk constructs a Chunk with deterministic metadata, backed numbers, and tokens.
func (c *SemanticChunker) buildChunk(docID, category, title, body, docTitle string, index int) Chunk {
	chunkID := fmt.Sprintf("%s_c%03d", docID, index)
	fullText := title
	if body != "" {
		fullText = title + "\n\n" + body
	}

	tokens := estimateTokens(fullText)
	backed := extractFiveDigitNumbers(fullText)
	tags := extractDeterministicTags(title, body, category)

	meta := map[string]string{
		"doc_id":      docID,
		"category":    category,
		"title":       title,
		"doc_title":   docTitle,
		"tags":        strings.Join(tags, ","),
		"chunk_index": strconv.Itoa(index),
		"char_count":  strconv.Itoa(len(fullText)),
	}

	return Chunk{
		ID:            chunkID,
		DocID:         docID,
		Category:      category,
		Title:         title,
		Content:       fullText,
		Tokens:        tokens,
		BackedNumbers: backed,
		Metadata:      meta,
	}
}

// splitBySentences breaks large text at sentence boundaries preserving max characters.
func (c *SemanticChunker) splitBySentences(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	// Split by ". "
	sentences := strings.Split(text, ". ")
	var parts []string
	var current strings.Builder

	for _, s := range sentences {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		candidate := s
		if !strings.HasSuffix(candidate, ".") {
			candidate += "."
		}

		if current.Len() > 0 && current.Len()+len(candidate)+1 > maxChars {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(candidate)
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

var tagKeywordMatchers = []string{
	"renault", "mazda", "chevrolet", "toyota", "duster", "stepway", "logan", "tracker", "cx-30", "cx5", "hilux",
	"suv", "sedan", "hatchback", "crossover", "4x4", "4x2", "fwd", "awd",
	"garantia", "cobertura", "motor", "caja", "desgaste", "taller", "talleres",
	"tunja", "duitama", "sogamoso", "boyaca",
	"credito", "financiacion", "cuota", "inicial", "empleados", "independientes",
	"bancolombia", "davivienda", "finandina", "sufi",
	"peritaje", "retoma", "traspaso", "transito", "horarios", "soat", "catalogo",
}

var reWordToken = regexp.MustCompile(`[a-zA-ZáéíóúÁÉÍÓÚñÑ0-9]{3,}`)

// extractDeterministicTags extracts deduplicated, sorted keyword tags.
func extractDeterministicTags(title, content, category string) []string {
	combined := stripAccents(strings.ToLower(title + " " + content + " " + category))
	tagMap := make(map[string]struct{})

	// 1. Category tag
	if category != "" {
		tagMap[stripAccents(strings.ToLower(category))] = struct{}{}
	}

	// 2. Predefined domain keywords
	for _, kw := range tagKeywordMatchers {
		if strings.Contains(combined, kw) {
			tagMap[kw] = struct{}{}
		}
	}

	// 3. Significant alphanumeric title words
	words := reWordToken.FindAllString(stripAccents(strings.ToLower(title)), -1)
	for _, w := range words {
		if len(w) >= 4 && !isCommonSpanishStopword(w) {
			tagMap[w] = struct{}{}
		}
	}

	result := make([]string, 0, len(tagMap))
	for tag := range tagMap {
		result = append(result, tag)
	}
	sort.Strings(result)
	return result
}

func isCommonSpanishStopword(word string) bool {
	switch word {
	case "para", "como", "este", "esta", "estos", "estas", "todos", "todas", "sobre", "entre", "hacia",
		"desde", "hasta", "donde", "cuando", "quien", "cual", "cuales", "frente", "segun", "parte":
		return true
	default:
		return false
	}
}
