package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"buyacar.com.co/omnicanal/internal/skills"
)

const (
	// DefaultGroqEndpoint endpoint oficial de chat completions de Groq Cloud API.
	DefaultGroqEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	// DefaultGroqModel modelo gratuito recomendado de alta velocidad en Groq.
	DefaultGroqModel = "llama-3.1-8b-instant"
)

var (
	ErrMissingGroqAPIKey = errors.New("ai/groq: GROQ_API_KEY environment variable is empty")
)

// GroqProvider implementa Provider y CascadeProvider (Tier 2: Cloud Free Tier) consumiendo la API de Groq.
type GroqProvider struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// NewGroqProvider crea un nuevo adaptador para Groq validando la seguridad del endpoint.
func NewGroqProvider(apiKey, model, endpoint string, timeout time.Duration) (*GroqProvider, error) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultGroqEndpoint
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultGroqModel
	}
	if timeout <= 0 {
		timeout = DefaultAIProviderTimeout
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("ai/groq: invalid endpoint: %w", err)
	}

	host := strings.ToLower(u.Hostname())
	isLoopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
	if !isLoopback && !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("ai/groq: scheme must be https for non-loopback host")
	}

	if host != "api.groq.com" && !isLoopback {
		return nil, fmt.Errorf("%w: %s", ErrUntrustedAIHost, host)
	}

	return &GroqProvider{
		endpoint: endpoint,
		model:    model,
		apiKey:   strings.TrimSpace(apiKey),
		client:   &http.Client{Timeout: timeout},
	}, nil
}

// NewGroqProviderFromEnv instancia el proveedor leyendo GROQ_API_KEY de variables de entorno.
func NewGroqProviderFromEnv(endpoint, model string, timeout time.Duration) (*GroqProvider, error) {
	apiKey := os.Getenv("GROQ_API_KEY")
	return NewGroqProvider(apiKey, model, endpoint, timeout)
}

// Name retorna el identificador del proveedor.
func (p *GroqProvider) Name() string {
	return "groq"
}

// Tier retorna el nivel en la cascada de inferencia (Tier 2: Cloud Free).
func (p *GroqProvider) Tier() int {
	return 2
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponseFormat struct {
	Type string `json:"type"`
}

type groqRequestPayload struct {
	Model          string              `json:"model"`
	Messages       []groqMessage       `json:"messages"`
	ResponseFormat *groqResponseFormat `json:"response_format,omitempty"`
	Temperature    float64             `json:"temperature"`
	MaxTokens      int                 `json:"max_tokens"`
}

type groqResponsePayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// GenerateJSON ejecuta la inferencia contra Groq usando el formato OpenAI estructurado y retornando el JSON crudo.
func (p *GroqProvider) GenerateJSON(ctx context.Context, compact skills.CompactContext) ([]byte, int, int, error) {
	key := p.apiKey
	if key == "" {
		key = strings.TrimSpace(os.Getenv("GROQ_API_KEY"))
	}
	if key == "" {
		return nil, 0, 0, ErrMissingGroqAPIKey
	}

	prompt := BuildDelimitedPrompt(compact)
	payload := groqRequestPayload{
		Model: p.model,
		Messages: []groqMessage{
			{Role: "user", Content: prompt},
		},
		ResponseFormat: &groqResponseFormat{Type: "json_object"},
		Temperature:    0.1,
		MaxTokens:      skills.MaxOutputTokensBudget,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, compact.EstimatedTokens, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, compact.EstimatedTokens, 0, ErrQuotaExceeded
	}
	if resp.StatusCode != http.StatusOK {
		return nil, compact.EstimatedTokens, 0, fmt.Errorf("ai/groq: status %d", resp.StatusCode)
	}

	// Mandamiento 4: Limite maximo de lectura 256 KB
	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, compact.EstimatedTokens, 0, err
	}

	var parsed groqResponsePayload
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return nil, compact.EstimatedTokens, 0, err
	}
	if len(parsed.Choices) == 0 {
		return nil, compact.EstimatedTokens, 0, errors.New("ai/groq: empty choices")
	}

	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return nil, compact.EstimatedTokens, 0, errors.New("ai/groq: empty choice content")
	}

	inTok := parsed.Usage.PromptTokens
	if inTok <= 0 {
		inTok = compact.EstimatedTokens
	}
	outTok := parsed.Usage.CompletionTokens
	if outTok <= 0 {
		outTok = (len(text) / 4) + 1
	}

	return []byte(text), inTok, outTok, nil
}
