package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/config"
)

// claudeCodeMarketplace is the GitHub owner/repo used by
// `claude plugin marketplace add`.
const claudeCodeMarketplace = "jinkp/brain-context"

// claudeCodeMCPTools lists every tool exposed by the brain-context MCP server.
// Adding these to ~/.claude/settings.json permissions.allow prevents Claude Code
// from prompting the user for confirmation on each tool call.
// Pattern: mcp__plugin_{marketplace}_{plugin}__{tool}
// Both marketplace and plugin are "brain-context" → normalized to "brain_context".
var claudeCodeMCPTools = []string{
	"mcp__plugin_brain_context_brain_context__search_project_context",
	"mcp__plugin_brain_context_brain_context__get_file_summary",
	"mcp__plugin_brain_context_brain_context__get_related_files",
	"mcp__plugin_brain_context_brain_context__explain_flow",
	"mcp__plugin_brain_context_brain_context__find_impact",
	"mcp__plugin_brain_context_brain_context__preview_scan",
	"mcp__plugin_brain_context_brain_context__search_solution_context",
}

// Testable function vars — override in tests to avoid real exec calls.
var (
	lookPathFn = exec.LookPath
	runCmdFn   = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
)

// setupClient configures the MCP entry for the given client.
// This is called both from the TUI (doSetupClients) and from
// the CLI direct mode (brain setup <client>).
func setupClient(client, home, brainExe string) error {
	switch client {
	case "opencode":
		return setupOpenCode(home, brainExe)
	case "claude":
		return setupClaude(home, brainExe)
	case "cursor":
		return setupCursor(home, brainExe)
	case "gemini":
		return setupGemini(home, brainExe)
	case "windsurf":
		return setupWindsurf(home, brainExe)
	default:
		return fmt.Errorf("unknown client %q", client)
	}
}

// SetupClient is the exported entry point used by cmd/brain/main.go.
func SetupClient(client, home, brainExe string) error {
	return setupClient(client, home, brainExe)
}

func mcpCommandEntry(brainExe string) []string {
	return []string{brainExe, "mcp"}
}

// ── OpenCode ──────────────────────────────────────────────────────────────────

func setupOpenCode(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, "Library", "Application Support", "opencode", "opencode.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	// 1. Inject MCP entry
	mcp := getOrCreateMap(cfg, "mcp")
	mcp["brain-context"] = map[string]any{
		"type":    "local",
		"command": mcpCommandEntry(brainExe),
	}
	cfg["mcp"] = mcp

	if err := writeJSONMap(path, cfg); err != nil {
		return err
	}

	// 2. Inject protocol into the prompt file referenced by an agent
	configDir := filepath.Dir(path)
	promptFile := findOpenCodePromptFile(cfg, configDir)
	if promptFile == "" {
		// No agent references a {file:...} prompt — create AGENTS.md next to opencode.json
		promptFile = filepath.Join(configDir, "AGENTS.md")
	}

	return injectProtocolIntoFile(promptFile)
}

// ── Claude Code ───────────────────────────────────────────────────────────────

// claudeDotPath returns the canonical user-scope Claude config file path.
func claudeDotPath(home string) string {
	return filepath.Join(home, ".claude.json")
}

// setupClaude configures Claude Code as a native plugin via the marketplace
// system. This gives brain-context first-class plugin status with hooks, skills,
// and MCP tools — the same way Engram registers itself.
//
// Steps:
//  1. `claude plugin marketplace add jinkp/brain-context`
//  2. `claude plugin install brain-context`
//  3. Write a durable MCP fallback to ~/.claude/mcp/brain-context.json
//     (survives plugin cache updates; critical on Windows where PATH may not
//     propagate to MCP subprocesses)
//  4. Add tool permissions to ~/.claude/settings.json allowlist
//  5. Best-effort migration of stale entries from old locations
//  6. Inject protocol into ~/.claude/CLAUDE.md
func setupClaude(home, brainExe string) error {
	// 1. Add marketplace — requires `claude` CLI in PATH
	claudeBin, err := lookPathFn("claude")
	if err != nil {
		// Fallback: if claude CLI is not available, use the legacy direct MCP approach
		fmt.Fprintf(os.Stderr, "  claude CLI not found — falling back to direct MCP registration\n")
		return setupClaudeLegacy(home, brainExe)
	}

	addOut, err := runCmdFn(claudeBin, "plugin", "marketplace", "add", claudeCodeMarketplace)
	addOutputStr := strings.TrimSpace(string(addOut))
	if err != nil {
		// "already" means marketplace was previously added — that's fine
		if !strings.Contains(strings.ToLower(addOutputStr), "already") {
			fmt.Fprintf(os.Stderr, "  marketplace add failed: %s — falling back to direct MCP\n", addOutputStr)
			return setupClaudeLegacy(home, brainExe)
		}
	}

	// 2. Install the plugin
	installOut, err := runCmdFn(claudeBin, "plugin", "install", "brain-context")
	installOutputStr := strings.TrimSpace(string(installOut))
	if err != nil {
		if !strings.Contains(strings.ToLower(installOutputStr), "already") {
			fmt.Fprintf(os.Stderr, "  plugin install failed: %s — falling back to direct MCP\n", installOutputStr)
			return setupClaudeLegacy(home, brainExe)
		}
	}

	// 3. Write durable user-level MCP config at ~/.claude/mcp/brain-context.json
	//    This uses the absolute binary path so MCP subprocesses don't need PATH.
	if err := writeClaudeUserMCP(home, brainExe); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not write user MCP config: %v\n", err)
		fmt.Fprintf(os.Stderr, "  The plugin is installed but MCP may not start on Windows if brain is not in PATH.\n")
	}

	// 4. Add tool permissions to allowlist
	if err := addClaudeAllowlist(home); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not update allowlist: %v\n", err)
	}

	// 5. Best-effort migration: remove stale entry from old locations
	if err := migrateOldSettings(home); err != nil {
		fmt.Fprintf(os.Stderr, "  settings.json migration skipped: %v\n", err)
	}
	// Also remove the direct MCP entry from ~/.claude.json (now handled by plugin)
	migrateClaudeDotJSON(home)

	// 6. Inject protocol into ~/.claude/CLAUDE.md
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := injectProtocolIntoFile(claudeMD); err != nil {
		fmt.Fprintf(os.Stderr, "  protocol injection skipped: %v\n", err)
	}

	return nil
}

// setupClaudeLegacy is the fallback when `claude` CLI is not available.
// It writes the MCP entry directly to ~/.claude.json (the pre-plugin approach).
func setupClaudeLegacy(home, brainExe string) error {
	clodotPath := claudeDotPath(home)

	cfg, err := readJSONMap(clodotPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", clodotPath, err)
		}
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"type":    "stdio",
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	if err := writeJSONMap(clodotPath, cfg); err != nil {
		return err
	}

	if err := migrateOldSettings(home); err != nil {
		fmt.Fprintf(os.Stderr, "  settings.json migration skipped: %v\n", err)
	}

	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	return injectProtocolIntoFile(claudeMD)
}

// writeClaudeUserMCP writes ~/.claude/mcp/brain-context.json with the absolute
// path to the brain binary. This is a durable MCP config that survives plugin
// cache auto-updates and works on Windows where PATH may not propagate to MCP
// subprocesses.
func writeClaudeUserMCP(home, brainExe string) error {
	// Resolve symlinks so the path is stable across package manager updates
	if resolved, err := filepath.EvalSymlinks(brainExe); err == nil {
		brainExe = resolved
	}

	entry := map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mcp config: %w", err)
	}

	mcpDir := filepath.Join(home, ".claude", "mcp")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		return fmt.Errorf("create mcp dir: %w", err)
	}

	mcpPath := filepath.Join(mcpDir, "brain-context.json")
	return os.WriteFile(mcpPath, data, 0644)
}

// addClaudeAllowlist adds brain-context MCP tool names to
// ~/.claude/settings.json permissions.allow so Claude Code doesn't prompt
// for confirmation on each tool call. Idempotent: skips tools already present.
func addClaudeAllowlist(home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	var cfg map[string]json.RawMessage
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			cfg = make(map[string]json.RawMessage)
		} else {
			return fmt.Errorf("read settings: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parse settings: %w", err)
		}
	}

	// Parse or create permissions block
	var permissions map[string]json.RawMessage
	if raw, exists := cfg["permissions"]; exists {
		if err := json.Unmarshal(raw, &permissions); err != nil {
			return fmt.Errorf("parse permissions: %w", err)
		}
	} else {
		permissions = make(map[string]json.RawMessage)
	}

	// Parse or create allow list
	var allowList []string
	if raw, exists := permissions["allow"]; exists {
		if err := json.Unmarshal(raw, &allowList); err != nil {
			return fmt.Errorf("parse allow list: %w", err)
		}
	}

	// Build set of existing entries for O(1) lookup
	existing := make(map[string]bool, len(allowList))
	for _, entry := range allowList {
		existing[entry] = true
	}

	// Add only missing tools
	added := 0
	for _, tool := range claudeCodeMCPTools {
		if !existing[tool] {
			allowList = append(allowList, tool)
			added++
		}
	}

	if added == 0 {
		return nil // all tools already present
	}

	// Write back
	allowData, err := json.Marshal(allowList)
	if err != nil {
		return fmt.Errorf("marshal allow list: %w", err)
	}
	permissions["allow"] = allowData

	permData, err := json.Marshal(permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	cfg["permissions"] = permData

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	return os.WriteFile(settingsPath, out, 0644)
}

// migrateClaudeDotJSON removes the direct brain-context MCP entry from
// ~/.claude.json if present. Now that brain-context is a plugin, the MCP
// server is managed by the plugin system + the durable ~/.claude/mcp/ fallback.
func migrateClaudeDotJSON(home string) {
	clodotPath := claudeDotPath(home)
	cfg, err := readJSONMap(clodotPath)
	if err != nil {
		return
	}

	mcpServers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	if _, has := mcpServers["brain-context"]; !has {
		return
	}

	delete(mcpServers, "brain-context")
	// If mcpServers is now empty, remove the key entirely
	if len(mcpServers) == 0 {
		delete(cfg, "mcpServers")
	} else {
		cfg["mcpServers"] = mcpServers
	}
	_ = writeJSONMap(clodotPath, cfg)
}

// migrateOldSettings removes the brain-context entry from ~/.claude/settings.json
// if present. Best-effort — errors are returned but callers should not fail on them.
func migrateOldSettings(home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file doesn't exist — no-op
		}
		return fmt.Errorf("read settings.json: %w", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse settings.json: %w", err)
	}

	mcpServers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return nil // no mcpServers key — no-op
	}
	if _, has := mcpServers["brain-context"]; !has {
		return nil // key not present — no-op
	}

	delete(mcpServers, "brain-context")
	cfg["mcpServers"] = mcpServers
	return writeJSONMap(settingsPath, cfg)
}

// setupClaudeLocal configures Claude Code at local scope: for each registered
// brain-context project, writes .mcp.json in the project repo root with a
// --project <name> arg so Claude Code scopes the MCP server to that project.
func setupClaudeLocal(home, brainExe string) error {
	brainCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load brain config: %w", err)
	}
	return setupClaudeLocalWithConfig(brainCfg, brainExe)
}

// setupClaudeLocalWithConfig is the testable core of setupClaudeLocal.
// It accepts an explicit config.Config so tests can inject temp-dir projects
// without touching the real ~/.brain/config.yaml.
func setupClaudeLocalWithConfig(brainCfg config.Config, brainExe string) error {
	for name, proj := range brainCfg.Projects {
		repoPath := strings.TrimSpace(proj.RepoPath)
		if repoPath == "" {
			continue
		}

		// 1. Write .mcp.json
		mcpPath := filepath.Join(repoPath, ".mcp.json")
		mcpCfg, err := readJSONMap(mcpPath)
		if err != nil {
			if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "  ⚠️  claude local: parse %s: %v\n", mcpPath, err)
				continue
			}
			mcpCfg = map[string]any{}
		}

		mcpServers := getOrCreateMap(mcpCfg, "mcpServers")
		mcpServers["brain-context"] = map[string]any{
			"command": brainExe,
			"args":    []string{"mcp", "--project", name},
		}
		mcpCfg["mcpServers"] = mcpServers

		if err := writeJSONMap(mcpPath, mcpCfg); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  claude local: write %s: %v\n", mcpPath, err)
			continue
		}

		// 2. Inject protocol into <repo>/CLAUDE.md
		claudeMD := filepath.Join(repoPath, "CLAUDE.md")
		if err := injectProtocolIntoFile(claudeMD); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  claude local: protocol injection skipped for %s: %v\n", repoPath, err)
		}
	}

	return nil
}

// SetupClientLocal is the exported entry point for local-scope setup.
// For "claude" it calls setupClaudeLocal; all other clients fall through
// to the standard user-scope setup (local scope only differs for Claude Code).
func SetupClientLocal(client, home, brainExe string) error {
	switch client {
	case "claude":
		return setupClaudeLocal(home, brainExe)
	default:
		return setupClient(client, home, brainExe)
	}
}

// ── Cursor ────────────────────────────────────────────────────────────────────

func setupCursor(home, brainExe string) error {
	// 1. Inject MCP entry into mcp.json
	candidates := []string{
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "Cursor", "mcp.json"),
		filepath.Join(os.Getenv("APPDATA"), "Cursor", "mcp.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	mcpCfg, err := readJSONMap(path)
	if err != nil {
		mcpCfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(mcpCfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	mcpCfg["mcpServers"] = mcpServers

	if err := writeJSONMap(path, mcpCfg); err != nil {
		return err
	}

	// 2. Inject protocol into .cursor/rules/brain-context.md for each registered project
	brainCfg, err := config.Load()
	if err != nil {
		// Config not available — skip protocol injection, MCP is already registered
		return nil
	}
	for _, proj := range brainCfg.Projects {
		repoPath := strings.TrimSpace(proj.RepoPath)
		if repoPath == "" {
			continue
		}
		rulesDir := filepath.Join(repoPath, ".cursor", "rules")
		rulesFile := filepath.Join(rulesDir, "brain-context.md")
		if err := injectProtocolIntoFile(rulesFile); err != nil {
			// Log but don't fail — MCP setup succeeded, protocol is best-effort
			fmt.Fprintf(os.Stderr, "  ⚠️  cursor rules injection skipped for %s: %v\n", repoPath, err)
		}
	}

	return nil
}

// ── Gemini CLI ────────────────────────────────────────────────────────────────

func setupGemini(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".gemini", "settings.json"),
		filepath.Join(home, ".config", "gemini", "settings.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── Windsurf ──────────────────────────────────────────────────────────────────

func setupWindsurf(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"),
		filepath.Join(home, "Library", "Application Support", "Windsurf", "mcp_config.json"),
		filepath.Join(os.Getenv("APPDATA"), "Windsurf", "mcp_config.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── JSON helpers ──────────────────────────────────────────────────────────────

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func writeJSONMap(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func getOrCreateMap(parent map[string]any, key string) map[string]any {
	if v, ok := parent[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ── Protocol injection helpers ───────────────────────────────────────────────

// findOpenCodePromptFile looks through the agent configs in opencode.json
// for the first primary agent with a {file:...} prompt reference and returns
// the resolved absolute path. Returns "" if none found.
func findOpenCodePromptFile(cfg map[string]any, configDir string) string {
	agents, ok := cfg["agent"]
	if !ok {
		return ""
	}
	agentMap, ok := agents.(map[string]any)
	if !ok {
		return ""
	}

	// Look for the first primary agent with a file-referenced prompt
	for _, agentCfg := range agentMap {
		agent, ok := agentCfg.(map[string]any)
		if !ok {
			continue
		}
		prompt, ok := agent["prompt"].(string)
		if !ok {
			continue
		}
		// Match {file:./path} or {file:path}
		if !strings.HasPrefix(prompt, "{file:") || !strings.HasSuffix(prompt, "}") {
			continue
		}
		relPath := strings.TrimSuffix(strings.TrimPrefix(prompt, "{file:"), "}")
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}
		// Resolve relative to config directory
		if !filepath.IsAbs(relPath) {
			return filepath.Join(configDir, relPath)
		}
		return relPath
	}
	return ""
}

// injectProtocolIntoFile injects or updates the brain-context protocol block
// in a markdown/text file using marker comments. Non-destructive:
//   - If markers exist → replaces ONLY the content between them
//   - If markers don't exist → appends the block at the end
//   - If file doesn't exist → creates it with the block
//   - All other content is preserved untouched
func injectProtocolIntoFile(path string) error {
	protocol := wrappedProtocol()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist — create it with just the protocol
			if mkErr := os.MkdirAll(filepath.Dir(path), 0755); mkErr != nil {
				return fmt.Errorf("create directory for %s: %w", path, mkErr)
			}
			return os.WriteFile(path, []byte(protocol+"\n"), 0644)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)

	if startIdx >= 0 && endIdx >= 0 && endIdx > startIdx {
		// Markers exist — replace the section between them (inclusive)
		updated := content[:startIdx] + protocol + content[endIdx+len(markerEnd):]
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// No markers found — append at the end, separated by a blank line
	separator := "\n\n"
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed == "" {
		separator = ""
	}
	updated := trimmed + separator + protocol + "\n"
	return os.WriteFile(path, []byte(updated), 0644)
}
