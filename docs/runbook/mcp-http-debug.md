# MCP HTTP Debug Runbook

## Purpose
Provide a deterministic checklist to debug HTTP MCP servers that use streamable responses.

## Prerequisites
- MCP server URL (for example: `https://font.emtech.cc/mcp`)
- Optional custom headers configured in settings
- `curl` available on runtime host

## Step 1: Initialize
Send an initialize request and capture `Mcp-Session-Id`.

```bash
curl -sS -D /tmp/mcp_init_headers.txt -o /tmp/mcp_init_body.txt \
  -X POST 'https://font.emtech.cc/mcp' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"koukebot","version":"0.1.0"}}}'
```

Expected:
- HTTP 2xx
- `Mcp-Session-Id` header present
- JSON-RPC initialize result appears in response stream body

## Step 2: Send initialized notification

```bash
SID="$(awk -F': ' 'tolower($1)==\"mcp-session-id\"{gsub(\"\\r\",\"\");print $2}' /tmp/mcp_init_headers.txt | tail -n1)"
curl -sS -X POST 'https://font.emtech.cc/mcp' \
  -H "Mcp-Session-Id: ${SID}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
```

Expected:
- HTTP 202 or HTTP 2xx with empty/no response body

## Step 3: Discover tools

```bash
curl -sS -X POST 'https://font.emtech.cc/mcp' \
  -H "Mcp-Session-Id: ${SID}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

Expected:
- JSON-RPC response with `result.tools` array

## Step 4: Call a tool

```bash
curl -sS -X POST 'https://font.emtech.cc/mcp' \
  -H "Mcp-Session-Id: ${SID}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  --data '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list-fonts","arguments":{}}}'
```

Expected:
- JSON-RPC response with `result.content` or `result.structuredContent`

## KoukeBOT Cross-check
- Enable `mcp.verbose_logging=true` in settings.
- Verify logs prefixed with `MCP`.
- Confirm request/response diagnostics show:
  - server id
  - transport
  - rpc method/id
  - status code
  - session id
  - redacted payload
