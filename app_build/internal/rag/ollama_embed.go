package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"
)

const (
	DefaultOllamaEndpoint = "http://127.0.0.1:11434/api/embeddings"
	DefaultOllamaModel    = "nomic-embed-text"
	DefaultOllamaTimeout  = 3 * time.Second
	MaxResponseBytes      = 256 * 1024 // 256 KB per Mandamiento 4
	DefaultVectorDim      = 256        // Fallback vector dimension
)

// FallbackEmbedder produces deterministic in-memory vector embeddings using
// tokenization, sign-hashing (Hashing Trick) and sublinear TF weighting.
type FallbackEmbedder struct {
	dim int
}

// NewFallbackEmbedder creates a deterministic in-memory vectorizer.
func NewFallbackEmbedder(dim int) *FallbackEmbedder {
	if dim <= 0 {
		dim = DefaultVectorDim
	}
	return &FallbackEmbedder{dim: dim}
}

// Dimension returns the vector dimensionality.
func (f *FallbackEmbedder) Dimension() int {
	return f.dim
}

// hashToken computes a 64-bit FNV-1a hash of a token.
func hashToken(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// Embed computes a normalized dense vector in [-1, 1]^dim for the given text.
func (f *FallbackEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	tokens := Tokenize(text)
	vec := make([]float32, f.dim)
	if len(tokens) == 0 {
		return vec, nil
	}

	// Count term frequencies
	tf := make(map[string]int, len(tokens))
	for _, tok := range tokens {
		tf[tok]++
	}

	// Unigram feature hashing
	for tok, count := range tf {
		h := hashToken(tok)
		idx := int(h % uint64(f.dim))
		sign := float32(1.0)
		if (h>>31)&1 == 1 {
			sign = -1.0
		}
		// Sublinear term frequency: 1 + ln(count)
		weight := float32(1.0 + math.Log(float64(count)))
		vec[idx] += sign * weight
	}

	// Bigram feature hashing for phrase sensitivity
	for i := 0; i < len(tokens)-1; i++ {
		bigram := tokens[i] + "_" + tokens[i+1]
		h := hashToken(bigram)
		idx := int(h % uint64(f.dim))
		sign := float32(1.0)
		if (h>>31)&1 == 1 {
			sign = -1.0
		}
		vec[idx] += sign * 0.75
	}

	NormalizeInPlace(vec)
	return vec, nil
}

// OllamaEmbedder manages embeddings via local Ollama nomic-embed-text,
// gracefully falling back to FallbackEmbedder if unavailable or timed out.
type OllamaEmbedder struct {
	client   *http.Client
	endpoint string
	model    string
	timeout  time.Duration
	fallback Embedder
}

// OllamaOption configures OllamaEmbedder.
type OllamaOption func(*OllamaEmbedder)

// WithOllamaEndpoint overrides the endpoint (must be on 127.0.0.1 or localhost).
func WithOllamaEndpoint(endpoint string) OllamaOption {
	return func(o *OllamaEmbedder) {
		o.endpoint = endpoint
	}
}

// WithOllamaModel sets the Ollama embedding model.
func WithOllamaModel(model string) OllamaOption {
	return func(o *OllamaEmbedder) {
		o.model = model
	}
}

// WithOllamaTimeout sets the client timeout.
func WithOllamaTimeout(d time.Duration) OllamaOption {
	return func(o *OllamaEmbedder) {
		if d > 0 && d <= 30*time.Second {
			o.timeout = d
		}
	}
}

// WithFallbackEmbedder configures the fallback embedder.
func WithFallbackEmbedder(fb Embedder) OllamaOption {
	return func(o *OllamaEmbedder) {
		o.fallback = fb
	}
}

// NewOllamaEmbedder creates a new embedder with Ollama local client and fallback.
func NewOllamaEmbedder(opts ...OllamaOption) (*OllamaEmbedder, error) {
	emb := &OllamaEmbedder{
		endpoint: DefaultOllamaEndpoint,
		model:    DefaultOllamaModel,
		timeout:  DefaultOllamaTimeout,
		fallback: NewFallbackEmbedder(DefaultVectorDim),
		client: &http.Client{
			Timeout: DefaultOllamaTimeout,
		},
	}

	for _, opt := range opts {
		opt(emb)
	}

	// Security validation per Mandamiento 4: Ollama exclusive in 127.0.0.1 / localhost
	u, err := url.Parse(emb.endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid ollama endpoint: %w", err)
	}
	hostname := u.Hostname()
	if hostname != "127.0.0.1" && hostname != "localhost" {
		return nil, fmt.Errorf("security violation: ollama endpoint must be localhost/127.0.0.1, got: %s", hostname)
	}

	emb.client.Timeout = emb.timeout
	return emb, nil
}

// Dimension returns dimension of embeddings (default 768 for nomic-embed-text or fallback dim).
func (o *OllamaEmbedder) Dimension() int {
	return o.fallback.Dimension()
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float64 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

// Embed calls Ollama nomic-embed-text; on network/timeout/status failure, switches to fallback.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if text == "" {
		return o.fallback.Embed(ctx, text)
	}

	reqBody, err := json.Marshal(ollamaRequest{
		Model:  o.model,
		Prompt: text,
	})
	if err != nil {
		return o.fallback.Embed(ctx, text)
	}

	reqCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, o.endpoint, bytes.NewReader(reqBody))
	if err != nil {
		slog.Debug("rag: failed to create ollama request, falling back", "err", err)
		return o.fallback.Embed(ctx, text)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		slog.Debug("rag: ollama call failed, engaging fallback vectorizer", "endpoint", o.endpoint, "err", err)
		return o.fallback.Embed(ctx, text)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("rag: ollama returned non-200, engaging fallback", "status", resp.StatusCode)
		return o.fallback.Embed(ctx, text)
	}

	limitedReader := io.LimitReader(resp.Body, MaxResponseBytes)
	var ollamaResp ollamaResponse
	if err := json.NewDecoder(limitedReader).Decode(&ollamaResp); err != nil {
		slog.Debug("rag: failed to decode ollama response, engaging fallback", "err", err)
		return o.fallback.Embed(ctx, text)
	}

	if len(ollamaResp.Embedding) == 0 {
		if ollamaResp.Error != "" {
			slog.Debug("rag: ollama error response, engaging fallback", "err", ollamaResp.Error)
		}
		return o.fallback.Embed(ctx, text)
	}

	vec := make([]float32, len(ollamaResp.Embedding))
	for i, v := range ollamaResp.Embedding {
		vec[i] = float32(v)
	}
	NormalizeInPlace(vec)
	return vec, nil
}

// Fallback returns the underlying fallback embedder.
func (o *OllamaEmbedder) Fallback() Embedder {
	return o.fallback
}

// ErrOllamaUnavailable is returned when Ollama is unreachable in strict check.
var ErrOllamaUnavailable = errors.New("ollama embedding service unavailable")
