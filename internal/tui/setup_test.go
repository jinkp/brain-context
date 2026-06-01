package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Gentleman-Programming/brain-context/internal/config"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return m
}

func mcpEntry(m map[string]any) map[string]any {
	servers, ok := m["mcpServers"].(map[string]any)
	if !ok {
		return nil
	}
	entry, ok := servers["brain-context"].(map[string]any)
	if !ok {
		return nil
	}
	return entry
}

// ── setupClaude tests ─────────────────────────────────────────────────────────

// TestSetupClaude_CreatesClaudeJSON verifies that setupClaude creates
// ~/.claude.json from scratch with the correct structure when the file is absent.
func TestSetupClaude_CreatesClaudeJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	if err := setupClaude(home, brainExe); err != nil {
		t.Fatalf("setupClaude: %v", err)
	}

	clodotPath := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(clodotPath); os.IsNotExist(err) {
		t.Fatal("~/.claude.json was not created")
	}

	cfg := readJSONFile(t, clodotPath)
	entry := mcpEntry(cfg)
	if entry == nil {
		t.Fatal("mcpServers[\"brain-context\"] not found in ~/.claude.json")
	}
	if entry["type"] != "stdio" {
		t.Errorf("expected type=stdio, got %v", entry["type"])
	}
	if entry["command"] != brainExe {
		t.Errorf("expected command=%s, got %v", brainExe, entry["command"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) == 0 || args[0] != "mcp" {
		t.Errorf("expected args=[mcp], got %v", entry["args"])
	}
}

// TestSetupClaude_MergesExistingClaudeJSON verifies that setupClaude merges
// into an existing ~/.claude.json without clobbering pre-existing keys.
func TestSetupClaude_MergesExistingClaudeJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	existing := map[string]any{
		"primaryApiKey": "sk-abc123",
		"tips":          []any{"tip1", "tip2"},
		"projects":      map[string]any{"myproject": "path/to/proj"},
	}
	writeJSON(t, filepath.Join(home, ".claude.json"), existing)

	if err := setupClaude(home, brainExe); err != nil {
		t.Fatalf("setupClaude: %v", err)
	}

	cfg := readJSONFile(t, filepath.Join(home, ".claude.json"))

	// Pre-existing keys must be preserved
	if cfg["primaryApiKey"] != "sk-abc123" {
		t.Errorf("primaryApiKey was clobbered, got %v", cfg["primaryApiKey"])
	}
	if _, ok := cfg["tips"]; !ok {
		t.Error("tips key was lost")
	}
	if _, ok := cfg["projects"]; !ok {
		t.Error("projects key was lost")
	}

	// brain-context entry must be present with type:stdio
	entry := mcpEntry(cfg)
	if entry == nil {
		t.Fatal("mcpServers[\"brain-context\"] missing after merge")
	}
	if entry["type"] != "stdio" {
		t.Errorf("expected type=stdio, got %v", entry["type"])
	}
}

// TestSetupClaude_InvalidJSONAborts verifies that setupClaude returns an error
// and does NOT overwrite the file when ~/.claude.json contains invalid JSON.
func TestSetupClaude_InvalidJSONAborts(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	clodotPath := filepath.Join(home, ".claude.json")
	corrupt := []byte(`{ "key": broken json `)
	if err := os.WriteFile(clodotPath, corrupt, 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	err := setupClaude(home, brainExe)
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}

	// File must not have been modified
	data, readErr := os.ReadFile(clodotPath)
	if readErr != nil {
		t.Fatalf("read file after abort: %v", readErr)
	}
	if string(data) != string(corrupt) {
		t.Errorf("file was modified despite corrupt JSON; got: %s", string(data))
	}
}

// TestSetupClaude_MigratesSettingsJSON verifies that a stale brain-context entry
// is removed from ~/.claude/settings.json after setupClaude succeeds, while
// other keys in settings.json are preserved.
func TestSetupClaude_MigratesSettingsJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	staleSettings := map[string]any{
		"theme": "dark",
		"mcpServers": map[string]any{
			"brain-context": map[string]any{
				"command": "old-brain",
				"args":    []any{"mcp"},
			},
			"other-server": map[string]any{
				"command": "other",
			},
		},
	}
	writeJSON(t, settingsPath, staleSettings)

	if err := setupClaude(home, brainExe); err != nil {
		t.Fatalf("setupClaude: %v", err)
	}

	settings := readJSONFile(t, settingsPath)

	// brain-context must be gone
	servers, _ := settings["mcpServers"].(map[string]any)
	if _, exists := servers["brain-context"]; exists {
		t.Error("stale brain-context entry was NOT removed from settings.json")
	}

	// other-server must still be there
	if _, exists := servers["other-server"]; !exists {
		t.Error("other-server was unexpectedly removed from settings.json")
	}

	// other top-level keys must be preserved
	if settings["theme"] != "dark" {
		t.Errorf("theme key was clobbered, got %v", settings["theme"])
	}
}

// ── setupClaudeLocal tests ────────────────────────────────────────────────────

// TestSetupClaudeLocal_WritesMcpJSON verifies that setupClaudeLocalWithConfig
// creates .mcp.json in each project repo with the correct --project arg.
func TestSetupClaudeLocal_WritesMcpJSON(t *testing.T) {
	brainExe := "/usr/local/bin/brain"

	// Create a temp repo dir to act as the project repo
	repoDir := t.TempDir()
	projectName := "my-app"

	cfg := config.Config{
		Projects: map[string]config.ProjectConfig{
			projectName: {
				RepoPath:        repoDir,
				EmbedDimensions: 1024,
			},
		},
	}

	if err := setupClaudeLocalWithConfig(cfg, brainExe); err != nil {
		t.Fatalf("setupClaudeLocalWithConfig: %v", err)
	}

	mcpPath := filepath.Join(repoDir, ".mcp.json")
	if _, err := os.Stat(mcpPath); os.IsNotExist(err) {
		t.Fatal(".mcp.json was not created in repo dir")
	}

	mcp := readJSONFile(t, mcpPath)
	entry := mcpEntry(mcp)
	if entry == nil {
		t.Fatal("mcpServers[\"brain-context\"] not found in .mcp.json")
	}
	if entry["command"] != brainExe {
		t.Errorf("expected command=%s, got %v", brainExe, entry["command"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) < 3 {
		t.Fatalf("expected args=[mcp, --project, my-app], got %v", entry["args"])
	}
	if args[0] != "mcp" || args[1] != "--project" || args[2] != projectName {
		t.Errorf("expected args=[mcp, --project, %s], got %v", projectName, args)
	}
	// type field must NOT be present in .mcp.json
	if _, hasType := entry["type"]; hasType {
		t.Error(".mcp.json entry must not have a \"type\" field")
	}
}

// TestSetupClaudeLocal_MergesExistingMcpJSON verifies that setupClaudeLocalWithConfig
// merges with an existing .mcp.json without overwriting other servers.
func TestSetupClaudeLocal_MergesExistingMcpJSON(t *testing.T) {
	brainExe := "/usr/local/bin/brain"
	repoDir := t.TempDir()
	projectName := "my-app"

	// Pre-seed .mcp.json with another server
	existing := map[string]any{
		"mcpServers": map[string]any{
			"existing-server": map[string]any{
				"command": "existing-cmd",
			},
		},
	}
	writeJSON(t, filepath.Join(repoDir, ".mcp.json"), existing)

	cfg := config.Config{
		Projects: map[string]config.ProjectConfig{
			projectName: {
				RepoPath:        repoDir,
				EmbedDimensions: 1024,
			},
		},
	}

	if err := setupClaudeLocalWithConfig(cfg, brainExe); err != nil {
		t.Fatalf("setupClaudeLocalWithConfig: %v", err)
	}

	mcp := readJSONFile(t, filepath.Join(repoDir, ".mcp.json"))
	servers, _ := mcp["mcpServers"].(map[string]any)

	// existing-server must still be present
	if _, ok := servers["existing-server"]; !ok {
		t.Error("existing-server was removed from .mcp.json")
	}
	// brain-context must be added
	if _, ok := servers["brain-context"]; !ok {
		t.Error("brain-context was not added to existing .mcp.json")
	}
}
