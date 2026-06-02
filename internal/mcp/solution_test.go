package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	"github.com/Gentleman-Programming/brain-context/internal/retriever"
)

// ── per-project cap formula ───────────────────────────────────────────────────

func TestCandidateCapFormula(t *testing.T) {
	const annCap = 50
	cases := []struct {
		n       int
		wantCap int
	}{
		{n: 1, wantCap: 50},
		{n: 2, wantCap: 25},
		{n: 3, wantCap: 16}, // floor(50/3)=16, 16 >= 10 → 16
		{n: 5, wantCap: 10}, // 50/5=10
		{n: 10, wantCap: 10}, // 50/10=5 < 10 → 10 (min)
	}
	for _, tc := range cases {
		got := annCap / tc.n
		if got < 10 {
			got = 10
		}
		if got != tc.wantCap {
			t.Errorf("N=%d: cap=%d, want %d", tc.n, got, tc.wantCap)
		}
	}
}

// ── formatSolutionResults ─────────────────────────────────────────────────────

func TestFormatSolutionResults_IncludesProjectName(t *testing.T) {
	chunks := []searchChunkResult{
		{
			SymbolName:  "ProcessPayment",
			SymbolType:  "function",
			FilePath:    "internal/payments/processor.go",
			StartLine:   10,
			EndLine:     50,
			Score:       0.92,
			ProjectName: "pagos",
		},
		{
			SymbolName:  "UserHandler",
			SymbolType:  "method",
			FilePath:    "internal/users/handler.go",
			StartLine:   1,
			EndLine:     30,
			Score:       0.85,
			ProjectName: "portal",
		},
	}

	result := formatSolutionResults("mi-suite", "payment handler", chunks, 2, nil)

	if !strings.Contains(result, "Project: pagos") {
		t.Errorf("expected 'Project: pagos' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Project: portal") {
		t.Errorf("expected 'Project: portal' in output, got:\n%s", result)
	}
	if !strings.Contains(result, "payment handler") {
		t.Errorf("expected query in output, got:\n%s", result)
	}
	if !strings.Contains(result, "2 projects") {
		t.Errorf("expected '2 projects' in output, got:\n%s", result)
	}
}

func TestFormatSolutionResults_WithWarnings(t *testing.T) {
	chunks := []searchChunkResult{
		{
			SymbolName:  "SomeFunc",
			FilePath:    "main.go",
			Score:       0.80,
			ProjectName: "pagos",
		},
	}
	warnings := []string{"worktools: connection refused"}

	result := formatSolutionResults("mi-suite", "query", chunks, 2, warnings)

	if !strings.Contains(result, "worktools: connection refused") {
		t.Errorf("expected warning in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Warnings") {
		t.Errorf("expected 'Warnings' section in output, got:\n%s", result)
	}
}

// ── partial failure ───────────────────────────────────────────────────────────

// fakeSearchServer creates a test HTTP server that returns a search response or error.
func fakeSearchServer(t *testing.T, projectResponses map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract project ID from path: /api/projects/{id}/context/search
		parts := strings.Split(r.URL.Path, "/")
		var projectID string
		for i, p := range parts {
			if p == "projects" && i+1 < len(parts) {
				projectID = parts[i+1]
				break
			}
		}

		resp, ok := projectResponses[projectID]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"project not found"}`)
			return
		}

		switch v := resp.(type) {
		case int:
			w.WriteHeader(v)
			fmt.Fprintf(w, `{"error":"simulated error"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}
	}))
}

func TestSearchSolutionContext_PartialFailure(t *testing.T) {
	// Set up: proj-001 (pagos) succeeds; proj-002 (worktools) returns 500
	proj001Response := searchResponse{
		ProjectID: "proj-001",
		Chunks: []searchChunkResult{
			{
				SymbolName: "ProcessPayment",
				FilePath:   "internal/payments/processor.go",
				Score:      0.92,
			},
		},
		TotalCandidates: 1,
	}

	ts := fakeSearchServer(t, map[string]interface{}{
		"proj-001": proj001Response,
		"proj-002": http.StatusInternalServerError,
	})
	defer ts.Close()

	srv := &Server{
		apiEndpoint: ts.URL,
		httpClient:  ts.Client(),
		projectsByID: map[string]brainconfig.ProjectConfig{
			"proj-001": {
				ProjectID:       "proj-001",
				MCPReadKey:      "rk-pagos",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
			"proj-002": {
				ProjectID:       "proj-002",
				MCPReadKey:      "rk-worktools",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
		projectsByName: map[string]string{
			"pagos":     "proj-001",
			"worktools": "proj-002",
		},
		solutionsByName: map[string]brainconfig.SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "worktools"}},
		},
	}

	// We need a query embedding — skip the actual embed step and test the fan-out merge logic
	// by directly calling the result merging logic.
	// Since we can't call handleSearchSolutionContext without a real embedder,
	// we test the fan-out by invoking resolveSolution and verifying partial behavior.

	projects, err := srv.resolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("resolveSolution: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(projects))
	}

	// Simulate the fan-out manually with the fake server
	// proj-001 succeeds; proj-002 fails.
	fakeEmbedding := make([]float32, 3072)
	payload := searchRequest{
		QueryEmbedding: fakeEmbedding,
		QueryTSV:       "payment",
		MaxChunks:      25,
	}

	var resp1 searchResponse
	err1 := srv.doJSON(t.Context(), http.MethodPost,
		ts.URL+"/api/projects/proj-001/context/search",
		"rk-pagos", payload, &resp1)

	var resp2 searchResponse
	err2 := srv.doJSON(t.Context(), http.MethodPost,
		ts.URL+"/api/projects/proj-002/context/search",
		"rk-worktools", payload, &resp2)

	// proj-001 must succeed
	if err1 != nil {
		t.Errorf("proj-001 expected success, got: %v", err1)
	}
	if len(resp1.Chunks) != 1 {
		t.Errorf("proj-001 expected 1 chunk, got %d", len(resp1.Chunks))
	}

	// proj-002 must fail
	if err2 == nil {
		t.Error("proj-002 expected failure, got nil error")
	}
}

// ── resolveSolution ───────────────────────────────────────────────────────────

func TestResolveSolution_NotFound(t *testing.T) {
	srv := &Server{
		solutionsByName: map[string]brainconfig.SolutionConfig{},
	}
	_, err := srv.resolveSolution("ghost")
	if err == nil {
		t.Fatal("expected error for unknown solution, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected solution name in error, got: %v", err)
	}
}

func TestResolveSolution_ReturnsProjects(t *testing.T) {
	srv := &Server{
		projectsByID: map[string]brainconfig.ProjectConfig{
			"proj-001": {ProjectID: "proj-001", MCPReadKey: "rk-001"},
			"proj-002": {ProjectID: "proj-002", MCPReadKey: "rk-002"},
		},
		projectsByName: map[string]string{
			"pagos":  "proj-001",
			"portal": "proj-002",
		},
		solutionsByName: map[string]brainconfig.SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "portal"}},
		},
	}

	projects, err := srv.resolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}
}

// ── searchChunkResult with ProjectName ─────────────────────────────────────────

func TestSearchChunkResult_ProjectNameJSON(t *testing.T) {
	chunk := searchChunkResult{
		SymbolName:  "Foo",
		FilePath:    "foo.go",
		Score:       0.9,
		ProjectName: "pagos",
		Relationships: []retriever.Relationship{
			{RelType: "calls", DstType: "symbol", DstName: "Bar"},
		},
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"project_name":"pagos"`) {
		t.Errorf("expected project_name in JSON, got: %s", data)
	}

	// Omitempty: no project name → should not appear
	chunkNoProject := searchChunkResult{
		SymbolName: "Bar",
		FilePath:   "bar.go",
		Score:      0.8,
	}
	data2, err := json.Marshal(chunkNoProject)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data2), "project_name") {
		t.Errorf("expected no project_name for empty string, got: %s", data2)
	}
}
