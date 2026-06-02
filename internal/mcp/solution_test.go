package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// ── probeMaxScore ─────────────────────────────────────────────────────────────

func TestProbeMaxScore(t *testing.T) {
	cases := []struct {
		name   string
		chunks []searchChunkResult
		want   float64
	}{
		{
			name:   "empty slice returns 0.0",
			chunks: []searchChunkResult{},
			want:   0.0,
		},
		{
			name:   "nil slice returns 0.0",
			chunks: nil,
			want:   0.0,
		},
		{
			name: "single chunk returns its score",
			chunks: []searchChunkResult{
				{Score: 0.75},
			},
			want: 0.75,
		},
		{
			name: "multiple chunks returns max",
			chunks: []searchChunkResult{
				{Score: 0.42},
				{Score: 0.88},
				{Score: 0.61},
			},
			want: 0.88,
		},
		{
			name: "all same score",
			chunks: []searchChunkResult{
				{Score: 0.50},
				{Score: 0.50},
			},
			want: 0.50,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := probeMaxScore(tc.chunks)
			if got != tc.want {
				t.Errorf("probeMaxScore() = %.4f, want %.4f", got, tc.want)
			}
		})
	}
}

// ── formatSmartResults ────────────────────────────────────────────────────────

func TestFormatSmartResults_SkippedSection(t *testing.T) {
	chunks := []searchChunkResult{
		{
			SymbolName:  "ProcessPayment",
			SymbolType:  "function",
			FilePath:    "payments/processor.go",
			StartLine:   10,
			EndLine:     50,
			Score:       0.92,
			ProjectName: "pagos",
		},
	}
	skipped := []skippedProject{
		{Name: "portal-socios", ProbeScore: 0.21},
		{Name: "worktools", ProbeScore: 0.18},
	}

	result := formatSmartResults("mi-suite", chunks, 3, skipped, 0.50, nil)

	if !strings.Contains(result, "portal-socios") {
		t.Errorf("expected 'portal-socios' in skipped section, got:\n%s", result)
	}
	if !strings.Contains(result, "0.21") {
		t.Errorf("expected probe score 0.21 in output, got:\n%s", result)
	}
	if !strings.Contains(result, "worktools") {
		t.Errorf("expected 'worktools' in skipped section, got:\n%s", result)
	}
	if !strings.Contains(result, "0.18") {
		t.Errorf("expected probe score 0.18 in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Skipped (below threshold)") {
		t.Errorf("expected 'Skipped (below threshold)' section header, got:\n%s", result)
	}
	if !strings.Contains(result, "threshold: 0.50") {
		t.Errorf("expected threshold in header, got:\n%s", result)
	}
	if !strings.Contains(result, "mode: smart") {
		t.Errorf("expected mode in header, got:\n%s", result)
	}
}

func TestFormatSmartResults_NoSkipped(t *testing.T) {
	// Should not panic or emit skipped section when skipped is nil/empty
	result := formatSmartResults("suite", nil, 2, nil, 0.50, nil)
	if strings.Contains(result, "Skipped") {
		t.Errorf("unexpected 'Skipped' section with empty skipped list, got:\n%s", result)
	}
}

// ── threshold default constant ────────────────────────────────────────────────

func TestSmartThresholdDefault(t *testing.T) {
	// SolutionConfig with zero SmartThreshold → constant must be 0.50
	sol := brainconfig.SolutionConfig{SmartThreshold: 0}
	if sol.SmartThreshold != 0 {
		t.Fatalf("expected zero value, got %f", sol.SmartThreshold)
	}
	if defaultSmartThreshold != 0.50 {
		t.Errorf("defaultSmartThreshold = %.2f, want 0.50", defaultSmartThreshold)
	}
}

// ── SolutionConfig threshold validation ──────────────────────────────────────

func TestSolutionValidation_ThresholdOutOfRange(t *testing.T) {
	// Common project config valid for embed checks
	validProject := brainconfig.ProjectConfig{
		ProjectID:       "proj-001",
		EmbedModel:      "openai/text-embedding-3-large",
		EmbedDimensions: 3072,
	}

	cases := []struct {
		name      string
		threshold float64
		wantErr   bool
	}{
		{name: "threshold 1.5 → error", threshold: 1.5, wantErr: true},
		{name: "threshold -0.1 → error", threshold: -0.1, wantErr: true},
		{name: "threshold 0.5 → ok", threshold: 0.5, wantErr: false},
		{name: "threshold 0.0 → ok (zero means default)", threshold: 0.0, wantErr: false},
		{name: "threshold 1.0 → ok", threshold: 1.0, wantErr: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := brainconfig.Config{
				Projects: map[string]brainconfig.ProjectConfig{
					"pagos":  validProject,
					"portal": validProject,
				},
				Solutions: map[string]brainconfig.SolutionConfig{
					"suite": {
						Projects:       []string{"pagos", "portal"},
						SmartThreshold: tc.threshold,
					},
				},
			}
			err := brainconfig.Validate(cfg)
			if tc.wantErr && err == nil {
				t.Errorf("expected validation error for threshold %.2f, got nil", tc.threshold)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error for threshold %.2f: %v", tc.threshold, err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "smart_threshold") {
				t.Errorf("expected 'smart_threshold' in error message, got: %v", err)
			}
		})
	}
}

// ── Integration: per-project call counter ────────────────────────────────────

// fakeSearchServerWithCounter creates a test HTTP server that returns per-project
// search responses and counts the number of calls per project ID using atomics.
func fakeSearchServerWithCounter(t *testing.T, projectResponses map[string]interface{}, callCounters map[string]*atomic.Int32) *httptest.Server {
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

		// Increment call counter if tracked
		if counter, ok := callCounters[projectID]; ok {
			counter.Add(1)
		}

		resp, ok := projectResponses[projectID]
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"project not found: %s"}`, projectID)
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

// buildThreeProjectSrv builds a Server wired to three projects: pagos (proj-001),
// portal-socios (proj-002), worktools (proj-003). The solution "mi-suite"
// contains all three.
func buildThreeProjectSrv(apiEndpoint string, httpClient *http.Client, threshold float64) *Server {
	return &Server{
		apiEndpoint: apiEndpoint,
		httpClient:  httpClient,
		projectsByID: map[string]brainconfig.ProjectConfig{
			"proj-001": {ProjectID: "proj-001", MCPReadKey: "rk-pagos", EmbedModel: "openai/text-embedding-3-large", EmbedDimensions: 3072},
			"proj-002": {ProjectID: "proj-002", MCPReadKey: "rk-portal", EmbedModel: "openai/text-embedding-3-large", EmbedDimensions: 3072},
			"proj-003": {ProjectID: "proj-003", MCPReadKey: "rk-worktools", EmbedModel: "openai/text-embedding-3-large", EmbedDimensions: 3072},
		},
		projectsByName: map[string]string{
			"pagos":         "proj-001",
			"portal-socios": "proj-002",
			"worktools":     "proj-003",
		},
		solutionsByName: map[string]brainconfig.SolutionConfig{
			"mi-suite": {
				Projects:       []string{"pagos", "portal-socios", "worktools"},
				SmartThreshold: threshold,
			},
		},
	}
}

func TestSmartRouting_OneOfThreePasses(t *testing.T) {
	// Probe: pagos scores 0.8 (passes), portal-socios 0.2 (fails), worktools 0.1 (fails)
	// Deep: only pagos gets called
	// We distinguish probe vs deep calls by max_chunks in the request body.

	probeResp001 := searchResponse{ProjectID: "proj-001", Chunks: []searchChunkResult{{Score: 0.8, SymbolName: "Pay", FilePath: "a.go"}}}
	probeResp002 := searchResponse{ProjectID: "proj-002", Chunks: []searchChunkResult{{Score: 0.2, SymbolName: "Portal", FilePath: "b.go"}}}
	probeResp003 := searchResponse{ProjectID: "proj-003", Chunks: []searchChunkResult{{Score: 0.1, SymbolName: "Work", FilePath: "c.go"}}}
	deepResp001 := searchResponse{ProjectID: "proj-001", Chunks: []searchChunkResult{{Score: 0.85, SymbolName: "PayDeep", FilePath: "a.go"}}}

	var calls001, calls002, calls003 atomic.Int32
	counters := map[string]*atomic.Int32{
		"proj-001": &calls001,
		"proj-002": &calls002,
		"proj-003": &calls003,
	}

	// Server returns different responses based on max_chunks in body
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		var projectID string
		for i, p := range parts {
			if p == "projects" && i+1 < len(parts) {
				projectID = parts[i+1]
				break
			}
		}
		if counter, ok := counters[projectID]; ok {
			counter.Add(1)
		}
		var body searchRequest
		_ = json.NewDecoder(r.Body).Decode(&body)

		var resp interface{}
		if body.MaxChunks == probeCap { // probe round
			switch projectID {
			case "proj-001":
				resp = probeResp001
			case "proj-002":
				resp = probeResp002
			case "proj-003":
				resp = probeResp003
			}
		} else { // deep round
			if projectID == "proj-001" {
				resp = deepResp001
			}
		}
		if resp == nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"unexpected call for %s"}`, projectID)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	srv := buildThreeProjectSrv(ts.URL, ts.Client(), 0.50)
	projects, err := srv.resolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("resolveSolution: %v", err)
	}

	params := solutionSearchParams{
		solutionName:   "mi-suite",
		query:          "payment retry logic",
		queryEmbedding: make([]float32, 3072),
		maxChunks:      20,
		mode:           "smart",
		threshold:      0.50,
	}
	result, err := srv.solutionFanOut(context.Background(), params, projects)
	if err != nil {
		t.Fatalf("solutionFanOut: %v", err)
	}

	// Each project gets exactly 1 probe call
	if calls001.Load() < 1 {
		t.Errorf("pagos: expected at least 1 call (probe), got %d", calls001.Load())
	}
	if calls002.Load() != 1 {
		t.Errorf("portal-socios: expected exactly 1 call (probe only), got %d", calls002.Load())
	}
	if calls003.Load() != 1 {
		t.Errorf("worktools: expected exactly 1 call (probe only), got %d", calls003.Load())
	}
	// pagos gets probe + deep = 2 calls total
	if calls001.Load() != 2 {
		t.Errorf("pagos: expected 2 calls (probe+deep), got %d", calls001.Load())
	}

	// Output contains skipped projects
	if !strings.Contains(result, "portal-socios") {
		t.Errorf("expected 'portal-socios' in skipped section, got:\n%s", result)
	}
	if !strings.Contains(result, "worktools") {
		t.Errorf("expected 'worktools' in skipped section, got:\n%s", result)
	}
}

func TestSmartRouting_FallbackWhenNonePass(t *testing.T) {
	// All probe scores below threshold → fallback warning + all projects get deep call
	lowResp := func(id string) searchResponse {
		return searchResponse{ProjectID: id, Chunks: []searchChunkResult{{Score: 0.1, SymbolName: "X", FilePath: "x.go"}}}
	}

	var calls001, calls002, calls003 atomic.Int32
	counters := map[string]*atomic.Int32{
		"proj-001": &calls001,
		"proj-002": &calls002,
		"proj-003": &calls003,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		var projectID string
		for i, p := range parts {
			if p == "projects" && i+1 < len(parts) {
				projectID = parts[i+1]
				break
			}
		}
		if counter, ok := counters[projectID]; ok {
			counter.Add(1)
		}
		var resp interface{}
		switch projectID {
		case "proj-001":
			resp = lowResp("proj-001")
		case "proj-002":
			resp = lowResp("proj-002")
		case "proj-003":
			resp = lowResp("proj-003")
		default:
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	srv := buildThreeProjectSrv(ts.URL, ts.Client(), 0.50)
	projects, err := srv.resolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("resolveSolution: %v", err)
	}

	params := solutionSearchParams{
		solutionName:   "mi-suite",
		query:          "anything",
		queryEmbedding: make([]float32, 3072),
		maxChunks:      20,
		mode:           "smart",
		threshold:      0.50,
	}
	result, err := srv.solutionFanOut(context.Background(), params, projects)
	if err != nil {
		t.Fatalf("solutionFanOut: %v", err)
	}

	// Each project gets probe + deep = 2 calls
	if calls001.Load() != 2 {
		t.Errorf("pagos: expected 2 calls, got %d", calls001.Load())
	}
	if calls002.Load() != 2 {
		t.Errorf("portal-socios: expected 2 calls, got %d", calls002.Load())
	}
	if calls003.Load() != 2 {
		t.Errorf("worktools: expected 2 calls, got %d", calls003.Load())
	}

	// Output contains fallback warning
	if !strings.Contains(result, "No project exceeded threshold") {
		t.Errorf("expected fallback warning in output, got:\n%s", result)
	}
	// No skipped section (fallback clears skipped)
	if strings.Contains(result, "Skipped (below threshold)") {
		t.Errorf("unexpected 'Skipped' section in fallback output, got:\n%s", result)
	}
}

func TestSmartRouting_FullModeBypassesProbe(t *testing.T) {
	// mode="full" → each project gets exactly 1 call (no probe round)
	resp001 := searchResponse{ProjectID: "proj-001", Chunks: []searchChunkResult{{Score: 0.9, SymbolName: "A", FilePath: "a.go"}}}
	resp002 := searchResponse{ProjectID: "proj-002", Chunks: []searchChunkResult{{Score: 0.8, SymbolName: "B", FilePath: "b.go"}}}
	resp003 := searchResponse{ProjectID: "proj-003", Chunks: []searchChunkResult{{Score: 0.7, SymbolName: "C", FilePath: "c.go"}}}

	var calls001, calls002, calls003 atomic.Int32
	counters := map[string]*atomic.Int32{
		"proj-001": &calls001,
		"proj-002": &calls002,
		"proj-003": &calls003,
	}

	ts := fakeSearchServerWithCounter(t, map[string]interface{}{
		"proj-001": resp001,
		"proj-002": resp002,
		"proj-003": resp003,
	}, counters)
	defer ts.Close()

	srv := buildThreeProjectSrv(ts.URL, ts.Client(), 0.50)
	projects, err := srv.resolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("resolveSolution: %v", err)
	}

	params := solutionSearchParams{
		solutionName:   "mi-suite",
		query:          "anything",
		queryEmbedding: make([]float32, 3072),
		maxChunks:      20,
		mode:           "full",
	}
	result, err := srv.solutionFanOut(context.Background(), params, projects)
	if err != nil {
		t.Fatalf("solutionFanOut: %v", err)
	}

	// Each project gets exactly 1 call in full mode (no probe round)
	if calls001.Load() != 1 {
		t.Errorf("pagos: expected exactly 1 call, got %d", calls001.Load())
	}
	if calls002.Load() != 1 {
		t.Errorf("portal-socios: expected exactly 1 call, got %d", calls002.Load())
	}
	if calls003.Load() != 1 {
		t.Errorf("worktools: expected exactly 1 call, got %d", calls003.Load())
	}

	// Full mode output does NOT contain smart-mode headers
	if strings.Contains(result, "mode: smart") {
		t.Errorf("unexpected smart-mode header in full-mode output, got:\n%s", result)
	}
	if strings.Contains(result, "Skipped") {
		t.Errorf("unexpected 'Skipped' section in full-mode output, got:\n%s", result)
	}
}
