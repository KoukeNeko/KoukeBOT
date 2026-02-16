# Tool Loop Guard Runbook

## Purpose
Explain when the system blocks tool calls and how final user responses are produced.

## Guard Rules
- Duplicate guard: identical tool signature in same user turn is blocked.
- Web search limit guard: `web_search` calls capped by `max_web_search_calls_per_turn`.
- Max rounds guard: tool rounds are always capped by `max_tool_rounds` (even when duplicate/limit guard is disabled) to prevent infinite loops.

## Decision Table
| Condition | Action | User-facing behavior |
|---|---|---|
| Duplicate tool call | Block call, append guard result | No extra Discord guard error; continue loop |
| `web_search` over per-turn limit | Block call, append guard result | Continue loop with existing results |
| Hit `max_tool_rounds` | Stop tool loop | Attempt forced final answer |
| Forced final succeeds | Return forced answer | Normal final response |
| Forced final fails and no response | Fallback response | User sees loop-safe fallback text |

## Forced Final
- Triggered when tool guard summary indicates loop-limit conditions.
- Executes a follow-up LLM call with `ToolMode=disabled`.
- Uses existing conversation + tool results only.

## Operational Checks
- Runtime metrics endpoint: `/api/metrics/runtime`
- Key counters:
  - `tool_guard_duplicate_blocked`
  - `tool_guard_limit_blocked`
  - `tool_guard_max_rounds`
  - `tool_forced_final_success`
  - `tool_forced_final_failed`
  - `tool_loop_fallback`

## MCP Duplicate Diagnosis
- `tool loop guard blocked duplicate call` means the same tool signature was requested again in the same turn.
- This is different from MCP transport failure.
- If the call before duplicate succeeded, the MCP server is usually healthy.
- To verify MCP transport independently, use discover test in `/mcp` or follow `docs/runbook/mcp-http-debug.md`.
