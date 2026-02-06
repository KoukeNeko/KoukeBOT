#!/usr/bin/env bash
set -euo pipefail

# Load .env if present (optional — keys can also be set via Web UI)
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

trap 'kill 0' EXIT

# Start Go backend
echo "==> Starting Go backend on :${PORT:-8080}..."
go run ./cmd/character &

# Start React dev server
echo "==> Starting React dev server..."
cd web && npm run dev &

echo ""
echo "  Web UI:   http://localhost:5173"
echo "  API:      http://localhost:${PORT:-8080}/api/health"
echo "  Settings: http://localhost:5173/settings"
echo ""

wait
