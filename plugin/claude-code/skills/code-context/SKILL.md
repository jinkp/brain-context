---
name: brain-context
description: "ALWAYS ACTIVE when available — Pre-indexed code intelligence. Query brain-context BEFORE reading files for search, flow analysis, impact detection, and file summaries."
---

# Brain-Context — Code Intelligence Protocol

You have brain-context MCP tools for pre-indexed code search. These return semantically
ranked results from indexed projects — FASTER and CHEAPER than scanning files directly.

## AVAILABLE TOOLS

Tools are loaded automatically at session start by the UserPromptSubmit hook.

- `search_project_context` — semantic search across the whole project
- `explain_flow` — trace end-to-end flows across files
- `get_file_summary` — file structure without reading content
- `get_related_files` — find connected files by imports/calls/dependencies
- `find_impact` — assess blast radius before modifying an entity

**Fallback**: If tools are unexpectedly unavailable, trigger ToolSearch manually:
```
select:mcp__plugin_brain_context__search_project_context,mcp__plugin_brain_context__get_file_summary,mcp__plugin_brain_context__get_related_files,mcp__plugin_brain_context__explain_flow,mcp__plugin_brain_context__find_impact
```

## PRIORITY RULE (mandatory)

When brain-context tools are available and the project is indexed:
1. ALWAYS query brain-context FIRST before reading or scanning files
2. Only fall back to reading files if brain-context doesn't return enough context
3. If a tool returns "Project not registered", tell the user to run `brain register`

This applies to ALL code-related tasks: understanding, searching, modifying, reviewing.

## TOOL SELECTION

| Need | Tool |
|------|------|
| Find relevant code for a question | `search_project_context` — try this FIRST |
| Understand an end-to-end flow | `explain_flow` — architecture, "how does X work" |
| See file structure without reading it | `get_file_summary` — symbols, line ranges |
| Find connected files | `get_related_files` — imports, dependencies, calls |
| Assess blast radius before changes | `find_impact` — know what breaks before modifying |

## WORKFLOW

1. **Search first**: `search_project_context` or `explain_flow` to understand
2. **Assess impact**: `find_impact` BEFORE modifying any entity
3. **Narrow reads**: Use `get_file_summary` to decide which files actually need reading
4. **Read only what's needed**: Open files only when tools don't give enough detail

## RULES

- Pass the correct `project_id` (project name works too)
- Prefer `search_project_context` over grep/find/read for discovery
- Use `find_impact` before ANY refactor or rename to understand blast radius
- Use `explain_flow` for architecture questions instead of reading multiple files
- If a tool returns "Run 'brain login' first", tell the user to authenticate
