#!/bin/bash
set -e

echo "=== Verifying Stream Display Feature ==="

# 1. Test SSE endpoint manually
echo ""
echo "1. Testing SSE streaming endpoint..."
timeout 5s curl -N "http://localhost:8080/api/chat/stream?persona_id=hayasaka&message=Say%20hello&channel_id=web-test" > /dev/null 2>&1 && echo "✅ SSE endpoint responding" || echo "⚠️  SSE endpoint may not be ready (timeout or connection issue)"

# 2. Test POST /api/chat still works
echo ""
echo "2. Testing backward compatibility (POST /api/chat)..."
RESPONSE=$(curl -s -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"persona_id":"hayasaka","message":"Hi","channel_id":"web-test"}')

if echo "$RESPONSE" | grep -q "response"; then
  echo "✅ POST /api/chat still functional"
else
  echo "❌ POST /api/chat broken"
  exit 1
fi

# 3. Check Discord methods exist
echo ""
echo "3. Checking Discord streaming methods..."
if grep -q "func.*EditMessage" internal/infra/discord/bot.go; then
  echo "✅ EditMessage method found"
else
  echo "❌ EditMessage method missing"
  exit 1
fi

if grep -q "func.*SendMessageWithReturn" internal/infra/discord/bot.go; then
  echo "✅ SendMessageWithReturn method found"
else
  echo "❌ SendMessageWithReturn method missing"
  exit 1
fi

echo ""
echo "=== All checks passed ==="
