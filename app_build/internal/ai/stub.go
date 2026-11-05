package ai

import (
	"context"
	"sync"

	"buyacar.com.co/omnicanal/internal/skills"
)

// StubResponse define un comportamiento programado para pruebas de cuota, timeout o JSON invalido.
type StubResponse struct {
	RawJSON      []byte
	InputTokens  int
	OutputTokens int
	Err          error
}

// StubProvider simula Gemini u Ollama en SIMULATION sin llamadas de red.
type StubProvider struct {
	mu        sync.Mutex
	name      string
	tier      int
	responses []StubResponse
	callCount int
}

func NewStubProvider(name string, responses ...StubResponse) *StubProvider {
	return &StubProvider{
		name:      name,
		tier:      1,
		responses: responses,
	}
}

func (s *StubProvider) WithTier(t int) *StubProvider {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tier = t
	return s
}

func (s *StubProvider) Tier() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tier <= 0 {
		return 1
	}
	return s.tier
}

func (s *StubProvider) Name() string {
	return s.name
}

func (s *StubProvider) GenerateJSON(ctx context.Context, prompt skills.CompactContext) ([]byte, int, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.callCount
	s.callCount++
	if len(s.responses) == 0 {
		return nil, 0, 0, ErrProviderTimeout
	}
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	r := s.responses[idx]
	inTok := r.InputTokens
	if inTok == 0 && r.Err == nil {
		inTok = prompt.EstimatedTokens
	}
	return r.RawJSON, inTok, r.OutputTokens, r.Err
}
