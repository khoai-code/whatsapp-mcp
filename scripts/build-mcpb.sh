#!/usr/bin/env bash
# Build the Claude Desktop extension: dist/whatsapp-mcp.mcpb
# Needs Go, Xcode command line tools and Node (for the mcpb packer).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist"
STAGE="$OUT/bundle"
MCPB="npx -y @anthropic-ai/mcpb@2.1.2"

rm -rf "$STAGE"
mkdir -p "$STAGE/bin" "$STAGE/server"

# Universal bridge binary (Apple silicon + Intel). go-sqlite3 needs cgo, so
# each architecture is compiled with clang targeting it.
cd "$ROOT/whatsapp-bridge"
for arch in arm64 amd64; do
  clang_arch=$([ "$arch" = amd64 ] && echo x86_64 || echo arm64)
  CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" CC="clang -arch $clang_arch" \
    go build -trimpath -ldflags "-s -w" -o "$OUT/whatsapp-bridge-$arch" .
done
lipo -create -output "$STAGE/bin/whatsapp-bridge" "$OUT/whatsapp-bridge-arm64" "$OUT/whatsapp-bridge-amd64"
rm "$OUT/whatsapp-bridge-arm64" "$OUT/whatsapp-bridge-amd64"
codesign --force --sign - "$STAGE/bin/whatsapp-bridge"

# MCP server, run by the uv that Claude Desktop provides.
cd "$ROOT/whatsapp-mcp-server"
cp main.py whatsapp.py formatting.py config.py audio.py "$STAGE/server/"
cp pyproject.toml uv.lock .python-version "$STAGE/"
cp "$ROOT/extension/manifest.json" "$ROOT/LICENSE" "$STAGE/"

$MCPB validate "$STAGE/manifest.json"
$MCPB pack "$STAGE" "$OUT/whatsapp-mcp.mcpb"
echo "Built $OUT/whatsapp-mcp.mcpb"
