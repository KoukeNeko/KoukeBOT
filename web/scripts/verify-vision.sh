#!/bin/bash
set -e

echo "=== Verifying Image Upload & Vision Feature ==="

# 1. Check domain types
echo ""
echo "1. Checking domain types..."
if grep -q "type ImageInput struct" internal/domain/conversation.go; then
  echo "✅ ImageInput type found"
else
  echo "❌ ImageInput type not found"
  exit 1
fi

if grep -q "type MultimodalMessage struct" internal/domain/conversation.go; then
  echo "✅ MultimodalMessage type found"
else
  echo "❌ MultimodalMessage type not found"
  exit 1
fi

# 2. Check provider interface
echo ""
echo "2. Checking multimodal provider interface..."
if grep -q "StreamChatMultimodal" internal/infra/llm/provider.go; then
  echo "✅ StreamChatMultimodal method found"
else
  echo "❌ StreamChatMultimodal method not found"
  exit 1
fi

# 3. Check Discord attachment handling
echo ""
echo "3. Checking Discord attachment handling..."
if grep -q "isImageAttachment" internal/interface/discord/handler.go; then
  echo "✅ Discord attachment handling found"
else
  echo "❌ Discord attachment handling not found"
  exit 1
fi

# 4. Check Go compilation
echo ""
echo "4. Checking Go compilation..."
if go build ./... 2>&1 | grep -q "error"; then
  echo "❌ Go compilation failed"
  exit 1
else
  echo "✅ Go compilation succeeded"
fi

# 5. Check frontend build
echo ""
echo "5. Checking frontend build..."
cd web
if npm run build > /dev/null 2>&1; then
  echo "✅ Frontend build succeeded"
else
  echo "❌ Frontend build failed"
  exit 1
fi
cd ..

echo ""
echo "=== All checks passed ==="
