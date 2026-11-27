#!/bin/sh
# Compila PiCameras para todas las plataformas en dist/. Hace falta Go 1.26 o posterior.
# La Raspberry Pi no necesita tener Go: compila aquí y copia el binario.
set -eu
cd "$(dirname "$0")/.."
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
mkdir -p dist
for t in linux/arm64 linux/arm/7 linux/arm/6 linux/amd64 windows/amd64 darwin/arm64 darwin/amd64; do
  os=${t%%/*}; rest=${t#*/}; arch=${rest%%/*}; armv=""
  [ "$arch" = arm ] && armv=${rest#*/}
  name="picameras_${os}_${arch}${armv:+v$armv}"
  [ "$os" = windows ] && name="$name.exe"
  echo "→ $name"
  GOOS=$os GOARCH=$arch GOARM=$armv CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "dist/$name" ./cmd/picameras
done
( cd dist && (sha256sum picameras_* 2>/dev/null || shasum -a 256 picameras_*) > SHA256SUMS )
echo "Listo: dist/ (con SHA256SUMS)"
