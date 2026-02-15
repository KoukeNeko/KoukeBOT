# Project Structure

## Purpose
This document defines the clean-code structure for KoukeBOT after the refactor, with explicit ownership boundaries and stable external contracts.

## Top-Level Layout
- `cmd/character`: process entrypoint; no business logic.
- `internal/bootstrap`: runtime composition and startup wiring.
- `internal/app`: domain application services (chat, provider pool, scheduler, persona).
- `internal/infra`: infrastructure adapters (config, llm, store, discord, search, logger, mcp).
- `internal/interface`: delivery interfaces (HTTP API, Discord adapter, shared contracts).
- `web/src`: React app organized by feature + shared API/types barrels.
- `docs`: architecture docs, ADRs, runbooks.

## Backend Boundaries
- `internal/bootstrap/*`: builds dependencies and starts transports; no request-level logic.
- `internal/interface/api/*`: HTTP handlers + DTO/validation/patch/runtime-apply split.
- `internal/interface/discord/*`: Discord handler split by responsibilities (`message`, `stream`, `attachments`, `context`, `formatting`).
- `internal/app/provider/*`: pool logic split into core/selection/retry/usage/errors.
- `internal/app/scheduler/*`: service split into commands/queries/runner/monitor/notify.
- `internal/infra/llm/oauth_*`: OAuth flow split by state, PKCE, exchange, discovery, and token store.

## Frontend Boundaries
- `web/src/features/chat/*`: chat page controller, SSE service, and feature components.
- `web/src/features/settings/*`: settings data hook, OAuth actions hook, and page composition component.
- `web/src/features/mcp/*`: MCP page hook/components for server configuration and discover-based connection testing.
- `web/src/api/*`: transport and domain API modules with `web/src/api.ts` compatibility barrel.
- `web/src/types/*`: domain type modules with `web/src/types.ts` compatibility barrel.
- `web/src/pages/*`: thin route-level composition pages.

## MCP Runtime Notes
- MCP settings persist in `settings.json` under `mcp`.
- `mcp.verbose_logging` toggles detailed MCP transport diagnostics.
- Detailed MCP logs use module prefix `MCP` and redact sensitive keys (`authorization`, `api-key`, `token`, `secret`, `password`, `cookie`, `set-cookie`).
- Web MCP operations are managed from route `/mcp`; Settings page no longer embeds MCP forms.

## Compatibility Rules
- External HTTP routes, methods, and JSON keys stay unchanged.
- Persisted data formats (`settings.json`, OAuth pool, conversations) stay unchanged.
- Existing frontend import surfaces remain available through compatibility barrels.
