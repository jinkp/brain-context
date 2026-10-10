package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// joinConfigOnDisk is a minimal struct for asserting what gets written to
// ~/.brain/config.yaml after a brain join — independent of brainconfig.Config
// so the test stays stable even if the config struct changes.
type joinConfigOnDisk struct {
	Projects map[string]joinProjectOnDisk `yaml:"projects"`
}

type joinProjectOnDisk struct {
	ProjectID       string `yaml:"project_id"`
	MCPReadKey      string `yaml:"mcp_read_key"`
	EmbedModel      string `yaml:"embed_model"`
	EmbedDimensions int    `yaml:"embed_dimensions"`
	EmbedAPIKeyEnc  string `yaml:"embed_api_key_enc"`
}

// redeemServer returns an httptest.Server that responds to POST /api/invite/redeem
// with a valid join response that intentionally omits embed_api_key_enc.
func redeemServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/invite/redeem" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project_id":       "proj-uuid-abc123",
			"project_name":     "test-project",
			"embed_model":      "openai/text-embedding-3-large",
			"embed_dimensions": 1024,
			"mcp_read_key":     "brn_mcp_testreadkey",
			"message":          "Store your mcp_read_key securely — shown once.",
		})
	}))
}

func TestRunJoin_DoesNotPersistEmbedAPIKeyEnc(t *testing.T) {
	srv := redeemServer(t)
	defer srv.Close()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)

	err := runJoin([]string{"--code", "brn_invite_testcode", "--api", srv.URL})
	if err != nil {
		t.Fatalf("runJoin returned unexpected error: %v", err)
	}

	// Read and parse the written config file
	cfgPath := filepath.Join(tmpDir, ".brain", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not written at %s: %v", cfgPath, err)
	}

	var cfg joinConfigOnDisk
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	proj, ok := cfg.Projects["test-project"]
	if !ok {
		t.Fatalf("project %q not found in saved config; got keys: %v", "test-project", projectKeys(cfg.Projects))
	}

	// Core assertion: embed_api_key_enc must NOT be written
	if proj.EmbedAPIKeyEnc != "" {
		t.Errorf("embed_api_key_enc must be empty after brain join; got %q", proj.EmbedAPIKeyEnc)
	}

	// Sanity: read-safe fields should be persisted correctly
	if proj.MCPReadKey != "brn_mcp_testreadkey" {
		t.Errorf("mcp_read_key = %q, want %q", proj.MCPReadKey, "brn_mcp_testreadkey")
	}
	if proj.ProjectID != "proj-uuid-abc123" {
		t.Errorf("project_id = %q, want %q", proj.ProjectID, "proj-uuid-abc123")
	}
	if proj.EmbedModel != "openai/text-embedding-3-large" {
		t.Errorf("embed_model = %q, want %q", proj.EmbedModel, "openai/text-embedding-3-large")
	}
	if proj.EmbedDimensions != 1024 {
		t.Errorf("embed_dimensions = %d, want %d", proj.EmbedDimensions, 1024)
	}
}

func TestRunJoin_ServerReturnsEmbedKeyEnc_FieldIsIgnored(t *testing.T) {
	// Even if an old server (pre-migration) returns embed_api_key_enc,
	// the client must not write it (field is absent from redeemInviteResponse).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project_id":       "proj-legacy",
			"project_name":     "legacy-project",
			"embed_model":      "openai/text-embedding-3-large",
			"embed_dimensions": 1024,
			"mcp_read_key":     "brn_mcp_legacykey",
			// Old server still sends this field — client must ignore it
			"embed_api_key_enc": "enc:somesecretvalue",
			"message":           "legacy server",
		})
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)

	if err := runJoin([]string{"--code", "brn_invite_legacy", "--api", srv.URL}); err != nil {
		t.Fatalf("runJoin: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, ".brain", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}

	var cfg joinConfigOnDisk
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if proj := cfg.Projects["legacy-project"]; proj.EmbedAPIKeyEnc != "" {
		t.Errorf("embed_api_key_enc must not be written even when server returns it; got %q", proj.EmbedAPIKeyEnc)
	}
}

func projectKeys(m map[string]joinProjectOnDisk) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
