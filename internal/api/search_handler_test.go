package api

import (
	"context"
	"errors"
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/embedder"
	"github.com/Gentleman-Programming/brain-context/internal/store"
	"github.com/google/uuid"
)

type fakeEmbedder struct {
	vectors [][]float32
	err     error
}

type queryTestCounters struct {
	loadCalls     int
	decryptCalls  int
	embedderCalls int
}

func (f fakeEmbedder) Embed(_ context.Context, _ []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vectors, nil
}

func (f fakeEmbedder) Dimensions() int {
	if len(f.vectors) == 0 {
		return 0
	}
	return len(f.vectors[0])
}

func TestResolveSearchQuery(t *testing.T) {
	origNewEmbedder := newEmbedderFn
	origDecrypt := decryptEmbedKeyFn
	origLoadKey := loadEmbedKeyFn
	t.Cleanup(func() {
		newEmbedderFn = origNewEmbedder
		decryptEmbedKeyFn = origDecrypt
		loadEmbedKeyFn = origLoadKey
	})

	project := store.Project{
		ID:              uuid.New(),
		EmbedModel:      "openai/text-embedding-3-large",
		EmbedDimensions: 3,
	}
	tenantID := uuid.New()
	h := &Handler{}

	tests := []struct {
		name              string
		project           store.Project
		req               searchRequest
		setup             func(t *testing.T, counters *queryTestCounters)
		wantVector        []float32
		wantRerank        string
		wantErrIs         error
		wantLoadKeyCalls  int
		wantDecryptCalls  int
		wantEmbedderCalls int
	}{
		{
			name:      "missing query and embedding",
			project:   project,
			req:       searchRequest{},
			wantErrIs: errSearchQueryRequired,
		},
		{
			name:      "legacy embedding dimension mismatch",
			project:   project,
			req:       searchRequest{QueryEmbedding: []float32{1, 2}},
			wantErrIs: errSearchEmbeddingLength,
		},
		{
			name:    "server side query embedding succeeds",
			project: project,
			req:     searchRequest{Query: "find payments"},
			setup: func(t *testing.T, counters *queryTestCounters) {
				loadEmbedKeyFn = func(_ context.Context, _ *store.Store, tenantID, projectID uuid.UUID) (string, error) {
					counters.loadCalls++
					if tenantID == uuid.Nil || projectID == uuid.Nil {
						t.Fatal("expected tenantID and projectID")
					}
					return "encrypted", nil
				}
				decryptEmbedKeyFn = func(cipher string) (string, error) {
					counters.decryptCalls++
					if cipher != "encrypted" {
						t.Fatalf("unexpected cipher %q", cipher)
					}
					return "plain-key", nil
				}
				newEmbedderFn = func(model, apiKey string) (embedder.Embedder, error) {
					counters.embedderCalls++
					if model != "openai/text-embedding-3-large" {
						t.Fatalf("unexpected model %q", model)
					}
					if apiKey != "plain-key" {
						t.Fatalf("unexpected api key %q", apiKey)
					}
					return fakeEmbedder{vectors: [][]float32{{0.1, 0.2, 0.3}}}, nil
				}
			},
			wantVector:        []float32{0.1, 0.2, 0.3},
			wantRerank:        "find payments",
			wantLoadKeyCalls:  1,
			wantDecryptCalls:  1,
			wantEmbedderCalls: 1,
		},
		{
			name:    "query takes precedence over legacy embedding",
			project: project,
			req: searchRequest{
				Query:          "find retries",
				QueryEmbedding: []float32{9},
				QueryTSV:       "legacy",
			},
			setup: func(t *testing.T, counters *queryTestCounters) {
				loadEmbedKeyFn = func(_ context.Context, _ *store.Store, _, _ uuid.UUID) (string, error) {
					counters.loadCalls++
					return "encrypted", nil
				}
				decryptEmbedKeyFn = func(_ string) (string, error) {
					counters.decryptCalls++
					return "plain-key", nil
				}
				newEmbedderFn = func(_, _ string) (embedder.Embedder, error) {
					counters.embedderCalls++
					return fakeEmbedder{vectors: [][]float32{{0.4, 0.5, 0.6}}}, nil
				}
			},
			wantVector:        []float32{0.4, 0.5, 0.6},
			wantRerank:        "find retries",
			wantLoadKeyCalls:  1,
			wantDecryptCalls:  1,
			wantEmbedderCalls: 1,
		},
		{
			name:    "missing server embed key",
			project: project,
			req:     searchRequest{Query: "find mit"},
			setup: func(_ *testing.T, counters *queryTestCounters) {
				loadEmbedKeyFn = func(_ context.Context, _ *store.Store, _, _ uuid.UUID) (string, error) {
					counters.loadCalls++
					return "", errors.New("not configured")
				}
			},
			wantErrIs:        errSearchEmbedKeyMissing,
			wantLoadKeyCalls: 1,
		},
		{
			name: "ollama query does not need key",
			project: store.Project{
				ID:              uuid.New(),
				EmbedModel:      "ollama/nomic-embed-text",
				EmbedDimensions: 3,
			},
			req: searchRequest{Query: "local search"},
			setup: func(t *testing.T, counters *queryTestCounters) {
				newEmbedderFn = func(model, apiKey string) (embedder.Embedder, error) {
					counters.embedderCalls++
					if apiKey != "" {
						t.Fatalf("expected empty api key for ollama, got %q", apiKey)
					}
					return fakeEmbedder{vectors: [][]float32{{0.7, 0.8, 0.9}}}, nil
				}
			},
			wantVector:        []float32{0.7, 0.8, 0.9},
			wantRerank:        "local search",
			wantEmbedderCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters := &queryTestCounters{}

			newEmbedderFn = func(model, apiKey string) (embedder.Embedder, error) {
				counters.embedderCalls++
				return fakeEmbedder{vectors: [][]float32{{1, 2, 3}}}, nil
			}
			decryptEmbedKeyFn = func(cipher string) (string, error) {
				counters.decryptCalls++
				return cipher, nil
			}
			loadEmbedKeyFn = func(_ context.Context, _ *store.Store, _, _ uuid.UUID) (string, error) {
				counters.loadCalls++
				return "encrypted", nil
			}

			if tt.setup != nil {
				tt.setup(t, counters)
			}

			gotVector, gotRerank, err := h.resolveSearchQuery(context.Background(), tenantID, tt.project, tt.req)
			if !errors.Is(err, tt.wantErrIs) {
				t.Fatalf("error = %v, want %v", err, tt.wantErrIs)
			}
			if counters.loadCalls != tt.wantLoadKeyCalls {
				t.Fatalf("loadEmbedKey calls = %d, want %d", counters.loadCalls, tt.wantLoadKeyCalls)
			}
			if counters.decryptCalls != tt.wantDecryptCalls {
				t.Fatalf("decrypt calls = %d, want %d", counters.decryptCalls, tt.wantDecryptCalls)
			}
			if counters.embedderCalls != tt.wantEmbedderCalls {
				t.Fatalf("newEmbedder calls = %d, want %d", counters.embedderCalls, tt.wantEmbedderCalls)
			}
			if tt.wantErrIs != nil {
				return
			}
			if len(gotVector) != len(tt.wantVector) {
				t.Fatalf("vector length = %d, want %d", len(gotVector), len(tt.wantVector))
			}
			for i := range gotVector {
				if gotVector[i] != tt.wantVector[i] {
					t.Fatalf("vector[%d] = %v, want %v", i, gotVector[i], tt.wantVector[i])
				}
			}
			if gotRerank != tt.wantRerank {
				t.Fatalf("rerank = %q, want %q", gotRerank, tt.wantRerank)
			}
		})
	}
}
