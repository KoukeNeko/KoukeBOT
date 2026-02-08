#!/bin/bash
set -e

echo "=== Verifying Image Upload & Vision Feature ==="

# 1. Check domain types exist
echo ""
echo "1. Checking domain types..."
if grep -q "type ImageInput struct" internal/domain/message.go 2>/dev/null; then
  echo "✅ ImageInput type found"
else
  echo "⚠️  ImageInput type not found (will be created)"
fi

# 2. Check multimodal provider method
echo ""
echo "2. Checking multimodal provider interface..."
if grep -q "StreamChatMultimodal" internal/infra/llm/provider.go 2>/dev/null; then
  echo "✅ StreamChatMultimodal method found"
else
  echo "⚠️  StreamChatMultimodal method not found (will be created)"
fi

# 3. Check Discord attachment handling
echo ""
echo "3. Checking Discord attachment handling..."
if grep -q "attachment.ContentType" internal/interface/discord/handler.go 2>/dev/null; then
  echo "✅ Discord attachment handling found"
else
  echo "⚠️  Discord attachment handling not found (will be added)"
fi

# 4. Compile check
echo ""
echo "4. Checking Go compilation..."
if go build ./... 2>/dev/null; then
  echo "✅ Go code compiles successfully"
else
  echo "❌ Go compilation failed"
  exit 1
fi

# 5. Frontend build check
echo ""
echo "5. Checking frontend build..."
if cd web && npm run build >/dev/null 2>&1; then
  echo "✅ Frontend builds successfully"
  cd ..
else
  echo "❌ Frontend build failed"
  cd ..
  exit 1
fi

echo ""
echo "=== Basic checks passed ==="
echo ""
echo "Manual testing required:"
echo "  1. Start server: ./dev.sh"
echo "  2. Test Web UI image upload at http://localhost:5173/chat"
echo "  3. Test Discord image attachment with @mention"
