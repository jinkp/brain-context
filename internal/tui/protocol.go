package tui

// brainContextProtocol is the protocol text injected into agent config files
// so the LLM knows WHEN and HOW to use brain-context tools proactively.
//
// The same protocol is used by:
//   - brain setup opencode  → injected into AGENTS.md with markers
//   - brain setup claude    → written as ~/.claude/mcp/brain-context-protocol.md (future)
//   - Claude Code plugin    → injected via session-start.sh hook
const brainContextProtocol = `## Brain-Context — Code Intelligence Protocol

You have brain-context MCP tools for pre-indexed code search. These return semantically
ranked results from indexed projects — FASTER and CHEAPER than scanning files directly.

### PRIORITY RULE (mandatory)

When brain-context tools are available and the project is indexed:
1. ALWAYS query brain-context FIRST before reading or scanning files
2. Only fall back to reading files if brain-context doesn't return enough context
3. If a tool returns "Project not registered", tell the user to run ` + "`brain register`" + `

This applies to ALL code-related tasks: understanding, searching, modifying, reviewing.

### TOOL SELECTION

| Need | Tool |
|------|------|
| Find relevant code for a question | ` + "`search_project_context`" + ` — try this FIRST |
| Understand an end-to-end flow | ` + "`explain_flow`" + ` — architecture, "how does X work" |
| See file structure without reading it | ` + "`get_file_summary`" + ` — symbols, line ranges |
| Find connected files | ` + "`get_related_files`" + ` — imports, dependencies, calls |
| Assess blast radius before changes | ` + "`find_impact`" + ` — know what breaks before modifying |

### WORKFLOW

1. **Search first**: ` + "`search_project_context`" + ` or ` + "`explain_flow`" + ` to understand
2. **Assess impact**: ` + "`find_impact`" + ` BEFORE modifying any entity
3. **Narrow reads**: Use ` + "`get_file_summary`" + ` to decide which files actually need reading
4. **Read only what's needed**: Open files only when tools don't give enough detail

### RULES

- Pass the correct ` + "`project_id`" + ` (project name works too)
- Prefer ` + "`search_project_context`" + ` over grep/find/read for discovery
- Use ` + "`find_impact`" + ` before ANY refactor or rename to understand blast radius
- Use ` + "`explain_flow`" + ` for architecture questions instead of reading multiple files
- If a tool returns "Run 'brain login' first", tell the user to authenticate`

const (
	// Markers used for non-destructive injection into config files.
	// Content between these markers is owned by brain-context and can be updated.
	markerStart = "<!-- brain-context:protocol -->"
	markerEnd   = "<!-- /brain-context:protocol -->"
)

// wrappedProtocol returns the protocol text wrapped in markers for injection.
func wrappedProtocol() string {
	return markerStart + "\n" + brainContextProtocol + "\n" + markerEnd
}
