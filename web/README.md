# KoukeBOT Web

Frontend for KoukeBOT built with React + TypeScript + Vite.

## Scripts
- `npm run dev`: start local dev server (default `http://localhost:5173`).
- `npm run build`: build production assets into `web/dist`.
- `npm run lint`: run ESLint on `src`.
- `npm run test`: run Vitest once (`vitest run`).

## Quality Gates
Use these commands from repository root:

```bash
npm --prefix web run lint
npm --prefix web run test
npm --prefix web run build
```

## Structure
- `src/pages`: route-level composition pages.
- `src/features/chat`: chat feature hooks, services, and components.
- `src/features/settings`: settings feature hooks and components.
- `src/features/mcp`: MCP management/testing page components and hooks.
- `src/api`: API modules with `src/api.ts` compatibility barrel.
- `src/types`: domain type modules with `src/types.ts` compatibility barrel.

## Web Tool Mode
- `classic`: uses `web_search` / `web_fetch` settings in Tools section.
- `playwright_mcp`: classic web tools are disabled at runtime; use `/mcp` for managed Playwright MCP discovery/testing.
