#!/bin/bash
# brain-context — UserPromptSubmit hook for Claude Code
#
# On the FIRST message of a session: injects a ToolSearch instruction to force
# Claude Code to load brain-context MCP tools (which may be deferred by default).
#
# MUST exit 0 always and output valid JSON — otherwise Claude Code blocks the message.

# Read hook input from stdin
INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')

# Default: no injection
OUTPUT="{}"

# Build a stable session key
if [ -n "$SESSION_ID" ]; then
  SESSION_KEY="brain-context-claude-${SESSION_ID}-tools-loaded"
else
  SESSION_KEY="brain-context-claude-$$-tools-loaded"
fi

STATE_FILE="/tmp/${SESSION_KEY}"

if [ ! -f "$STATE_FILE" ]; then
  # First message — force tool loading
  touch "$STATE_FILE" 2>/dev/null || true

  # Tool name pattern: mcp__plugin_{marketplace}_{plugin}__{tool}
  # Marketplace and plugin are both "brain-context" → normalized to "brain_context"
  TOOL_MSG="Load brain-context tools NOW before responding:"$'\n'"select:mcp__plugin_brain_context_brain_context__search_project_context,mcp__plugin_brain_context_brain_context__get_file_summary,mcp__plugin_brain_context_brain_context__get_related_files,mcp__plugin_brain_context_brain_context__explain_flow,mcp__plugin_brain_context_brain_context__find_impact"
  OUTPUT=$(jq -n --arg msg "$TOOL_MSG" '{"systemMessage": $msg}')

  printf '%s\n' "$OUTPUT"
  exit 0
fi

echo "$OUTPUT"
exit 0
