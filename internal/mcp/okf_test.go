package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	"github.com/Gentleman-Programming/brain-context/internal/okf"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// buildOKFTestIndex creates a temp bundle with one stable concept, builds the
// SQLite FTS5 index, and returns an *okf.Index ready for use in Server tests.
// HOME/USERPROFILE are set to tmpHome so the DB lands in a clean location.
func buildOKFTestIndex(t *testing.T, tmpHome, projectID, bundleID, bundleDir string) *okf.Index {
	t.Helper()

	content := "---\ntitle: \"Auth Handler\"\ntype: FileSummary\nstatus: stable\ntrust_tier: human-reviewed\ndescription: \"Handles JWT authentication and session creation.\"\ntags: [auth, jwt, handler]\nsource_files:\n  - internal/auth/handler.go\n---\n\nAuth handler body.\n"
	if err := os.WriteFile(filepath.Join(bundleDir, "auth-handler.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write concept: %v", err)
	}

	if err := okf.BuildIndex(projectID, bundleID, bundleDir); err != nil {
		t.Logf("BuildIndex warning: %v", err)
	}

	idx, err := okf.OpenIndex(map[string]brainconfig.ProjectConfig{
		"myapp": {
			ProjectID:     projectID,
			OKFBundlePath: bundleDir,
			OKFBundleID:   bundleID,
		},
	})
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	if idx == nil {
		t.Fatal("OpenIndex returned nil")
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// makeSearchProjectContextRequest builds a CallToolRequest for search_project_context.
func makeSearchProjectContextRequest(projectID, query string) mcpgo.CallToolRequest {
	return mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name: "search_project_context",
			Arguments: map[string]any{
				"project_id": projectID,
				"query":      query,
			},
		},
	}
}

// ── Task 4.7: OKF gate hit ───────────────────────────────────────────────────

func TestHandleSearchProjectContext_OKFGateHit(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	projectID := "proj-okf-test"
	bundleID := "bundle-okf-test"

	idx := buildOKFTestIndex(t, tmpHome, projectID, bundleID, bundleDir)

	// Construct a Server with the real OKF index and a fake HTTP backend
	// that should NOT be called (OKF should intercept before RAG)
	ragCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ragCalled = true
		http.Error(w, "should not have been called", http.StatusInternalServerError)
	}))
	defer ts.Close()

	srv := &Server{
		apiEndpoint: ts.URL,
		httpClient:  ts.Client(),
		projectsByID: map[string]brainconfig.ProjectConfig{
			projectID: {
				ProjectID:    projectID,
				MCPReadKey:   "rk-test",
				OKFThreshold: 0.000001, // very low — accept any FTS5 match
			},
		},
		projectsByName: map[string]string{
			"myapp": projectID,
		},
		okfIndex: idx,
	}

	req := makeSearchProjectContextRequest(projectID, "auth jwt handler")
	result, err := srv.handleSearchProjectContext(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchProjectContext: %v", err)
	}

	// The result should come from OKF, not RAG
	if ragCalled {
		t.Error("RAG backend was called — OKF gate should have intercepted first")
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	text := extractText(result)
	if !strings.Contains(text, "[OKF]") {
		t.Errorf("result does not contain '[OKF]' prefix — OKF gate may not have fired: %q", text)
	}
	if !strings.Contains(text, "Auth Handler") {
		t.Errorf("result does not contain concept title 'Auth Handler': %q", text)
	}
}

// ── Task 4.8: nil okfIndex guard ──────────────────────────────────────────────

func TestHandleSearchProjectContext_NilOKFIndex_NoPanic(t *testing.T) {
	// Server with okfIndex=nil — should fall through to RAG without panicking.
	// The RAG will fail (backend returns 401), but the test just verifies no panic.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	srv := &Server{
		apiEndpoint: ts.URL,
		httpClient:  ts.Client(),
		projectsByID: map[string]brainconfig.ProjectConfig{
			"proj-nil": {
				ProjectID:  "proj-nil",
				MCPReadKey: "rk-nil",
			},
		},
		projectsByName: map[string]string{
			"nilapp": "proj-nil",
		},
		okfIndex: nil, // explicitly nil — the key invariant
	}

	req := makeSearchProjectContextRequest("proj-nil", "any query")

	// Must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handleSearchProjectContext panicked with nil okfIndex: %v", r)
		}
	}()

	result, err := srv.handleSearchProjectContext(context.Background(), req)
	// We expect either an error result or a tool-error result (401 from fake backend)
	// The critical assertion is: no panic, and result is non-nil
	if err != nil {
		t.Logf("got expected error (backend unavailable): %v", err)
	}
	if result == nil {
		t.Error("result is nil — expected a tool-error result, not nil")
	}
}

// ── OKF gate in handleExplainFlow ────────────────────────────────────────────

func TestHandleExplainFlow_NilOKFIndex_NoPanic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()

	srv := &Server{
		apiEndpoint: ts.URL,
		httpClient:  ts.Client(),
		projectsByID: map[string]brainconfig.ProjectConfig{
			"proj-ef": {
				ProjectID:  "proj-ef",
				MCPReadKey: "rk-ef",
			},
		},
		projectsByName: map[string]string{
			"efapp": "proj-ef",
		},
		okfIndex: nil,
	}

	req := mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{
			Name: "explain_flow",
			Arguments: map[string]any{
				"project_id": "proj-ef",
				"query":      "auth flow",
			},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handleExplainFlow panicked with nil okfIndex: %v", r)
		}
	}()

	result, err := srv.handleExplainFlow(context.Background(), req)
	if err != nil {
		t.Logf("expected error (backend unavailable): %v", err)
	}
	if result == nil {
		t.Error("result is nil")
	}
}

// extractText extracts the text content from a CallToolResult.
func extractText(result *mcpgo.CallToolResult) string {
	if result == nil {
		return ""
	}
	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcpgo.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "")
}
