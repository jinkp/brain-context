package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ── setupClaudeLegacy tests (fallback when claude CLI is absent) ──────────────

// TestSetupClaudeLegacy_CreatesClaudeJSON verifies that setupClaudeLegacy creates
// ~/.claude.json from scratch with the correct structure when the file is absent.
func TestSetupClaudeLegacy_CreatesClaudeJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	if err := setupClaudeLegacy(home, brainExe); err != nil {
		t.Fatalf("setupClaudeLegacy: %v", err)
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

// TestSetupClaudeLegacy_MergesExistingClaudeJSON verifies that setupClaudeLegacy
// merges into an existing ~/.claude.json without clobbering pre-existing keys.
func TestSetupClaudeLegacy_MergesExistingClaudeJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	existing := map[string]any{
		"primaryApiKey": "sk-abc123",
		"tips":          []any{"tip1", "tip2"},
		"projects":      map[string]any{"myproject": "path/to/proj"},
	}
	writeJSON(t, filepath.Join(home, ".claude.json"), existing)

	if err := setupClaudeLegacy(home, brainExe); err != nil {
		t.Fatalf("setupClaudeLegacy: %v", err)
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

// TestSetupClaudeLegacy_InvalidJSONAborts verifies that setupClaudeLegacy returns
// an error and does NOT overwrite the file when ~/.claude.json contains invalid JSON.
func TestSetupClaudeLegacy_InvalidJSONAborts(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	clodotPath := filepath.Join(home, ".claude.json")
	corrupt := []byte(`{ "key": broken json `)
	if err := os.WriteFile(clodotPath, corrupt, 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	err := setupClaudeLegacy(home, brainExe)
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

// TestSetupClaude_FallsBackToLegacy verifies that setupClaude falls back
// to the legacy direct-MCP approach when `claude` CLI is not in PATH.
func TestSetupClaude_FallsBackToLegacy(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	// Mock lookPathFn to simulate claude CLI not being available
	origLookPath := lookPathFn
	lookPathFn = func(file string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	defer func() { lookPathFn = origLookPath }()

	if err := setupClaude(home, brainExe); err != nil {
		t.Fatalf("setupClaude: %v", err)
	}

	// Should have written to ~/.claude.json via legacy path
	clodotPath := filepath.Join(home, ".claude.json")
	cfg := readJSONFile(t, clodotPath)
	entry := mcpEntry(cfg)
	if entry == nil {
		t.Fatal("mcpServers[\"brain-context\"] not found — legacy fallback did not write")
	}
	if entry["type"] != "stdio" {
		t.Errorf("expected type=stdio, got %v", entry["type"])
	}
}

// TestSetupClaude_PluginPath verifies the full plugin installation path
// when `claude` CLI is available and marketplace/install commands succeed.
func TestSetupClaude_PluginPath(t *testing.T) {
	home := t.TempDir()
	brainExe := filepath.Join(home, "brain")

	// Create the brain binary file so EvalSymlinks works
	if err := os.WriteFile(brainExe, []byte("fake"), 0755); err != nil {
		t.Fatal(err)
	}

	// Track which commands were called
	var cmdsRun []string

	origLookPath := lookPathFn
	origRunCmd := runCmdFn
	lookPathFn = func(file string) (string, error) {
		if file == "claude" {
			return "/usr/local/bin/claude", nil
		}
		return "", fmt.Errorf("not found")
	}
	runCmdFn = func(name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		cmdsRun = append(cmdsRun, cmd)
		return []byte("ok"), nil
	}
	defer func() {
		lookPathFn = origLookPath
		runCmdFn = origRunCmd
	}()

	if err := setupClaude(home, brainExe); err != nil {
		t.Fatalf("setupClaude: %v", err)
	}

	// Verify marketplace add was called
	if len(cmdsRun) < 1 {
		t.Fatal("expected marketplace add command to be called")
	}
	if !strings.Contains(cmdsRun[0], "marketplace add") {
		t.Errorf("first command should be marketplace add, got: %s", cmdsRun[0])
	}
	if !strings.Contains(cmdsRun[0], claudeCodeMarketplace) {
		t.Errorf("marketplace add should reference %s, got: %s", claudeCodeMarketplace, cmdsRun[0])
	}

	// Verify plugin install was called
	if len(cmdsRun) < 2 {
		t.Fatal("expected plugin install command to be called")
	}
	if !strings.Contains(cmdsRun[1], "plugin install") {
		t.Errorf("second command should be plugin install, got: %s", cmdsRun[1])
	}

	// Verify durable MCP config was written
	mcpPath := filepath.Join(home, ".claude", "mcp", "brain-context.json")
	if _, err := os.Stat(mcpPath); os.IsNotExist(err) {
		t.Fatal("~/.claude/mcp/brain-context.json was not created")
	}
	mcpCfg := readJSONFile(t, mcpPath)
	if mcpCfg["command"] == nil {
		t.Error("durable MCP config missing command field")
	}

	// Verify allowlist was written to settings.json
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Fatal("~/.claude/settings.json was not created for allowlist")
	}

	// Verify direct MCP entry was NOT written to ~/.claude.json
	// (plugin path should not create a direct entry)
	clodotPath := filepath.Join(home, ".claude.json")
	if _, err := os.Stat(clodotPath); err == nil {
		cfg := readJSONFile(t, clodotPath)
		if mcpEntry(cfg) != nil {
			t.Error("direct MCP entry should NOT exist in ~/.claude.json when using plugin path")
		}
	}
}

// TestSetupClaude_MigratesSettingsJSON verifies that a stale brain-context entry
// is removed from ~/.claude/settings.json after setupClaude succeeds, while
// other keys in settings.json are preserved.
func TestSetupClaude_MigratesSettingsJSON(t *testing.T) {
	home := t.TempDir()
	brainExe := "/usr/local/bin/brain"

	// Use legacy path so the test focuses on migration behavior
	origLookPath := lookPathFn
	lookPathFn = func(file string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	defer func() { lookPathFn = origLookPath }()

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

// TestAddClaudeAllowlist verifies that addClaudeAllowlist adds all brain-context
// tool names to the permissions.allow list, is idempotent, and preserves existing entries.
func TestAddClaudeAllowlist(t *testing.T) {
	home := t.TempDir()

	// First call — creates settings.json from scratch
	if err := addClaudeAllowlist(home); err != nil {
		t.Fatalf("first addClaudeAllowlist: %v", err)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := readJSONFile(t, settingsPath)
	perms, _ := settings["permissions"].(map[string]any)
	allowRaw, _ := perms["allow"].([]any)

	if len(allowRaw) != len(claudeCodeMCPTools) {
		t.Errorf("expected %d tools in allowlist, got %d", len(claudeCodeMCPTools), len(allowRaw))
	}

	// Second call — should be idempotent
	if err := addClaudeAllowlist(home); err != nil {
		t.Fatalf("second addClaudeAllowlist: %v", err)
	}

	settings2 := readJSONFile(t, settingsPath)
	perms2, _ := settings2["permissions"].(map[string]any)
	allow2, _ := perms2["allow"].([]any)

	if len(allow2) != len(claudeCodeMCPTools) {
		t.Errorf("expected %d tools after idempotent call, got %d", len(claudeCodeMCPTools), len(allow2))
	}
}

// TestMigrateClaudeDotJSON verifies that migrateClaudeDotJSON removes the
// direct brain-context entry from ~/.claude.json while preserving other keys.
func TestMigrateClaudeDotJSON(t *testing.T) {
	home := t.TempDir()
	clodotPath := filepath.Join(home, ".claude.json")

	existing := map[string]any{
		"primaryApiKey": "sk-abc123",
		"mcpServers": map[string]any{
			"brain-context": map[string]any{
				"type":    "stdio",
				"command": "/usr/local/bin/brain",
				"args":    []any{"mcp"},
			},
			"other-server": map[string]any{
				"command": "other",
			},
		},
	}
	writeJSON(t, clodotPath, existing)

	migrateClaudeDotJSON(home)

	cfg := readJSONFile(t, clodotPath)

	// brain-context must be removed
	servers, _ := cfg["mcpServers"].(map[string]any)
	if _, exists := servers["brain-context"]; exists {
		t.Error("brain-context was NOT removed from ~/.claude.json")
	}

	// other-server must remain
	if _, exists := servers["other-server"]; !exists {
		t.Error("other-server was unexpectedly removed")
	}

	// other keys preserved
	if cfg["primaryApiKey"] != "sk-abc123" {
		t.Error("primaryApiKey was clobbered")
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
