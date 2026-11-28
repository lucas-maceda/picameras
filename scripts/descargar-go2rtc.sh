#!/bin/sh
# Descarga go2rtc (el motor de vídeo) y comprueba su SHA-256 antes de dejarlo usable.
# Si la suma no coincide, borra el fichero y para: nunca se ejecuta un binario sin comprobar.
#
#   scripts/descargar-go2rtc.sh [destino]        (por defecto, el directorio actual)
#
# Las sumas se fijaron al descargar la versión de los releases oficiales de go2rtc el 27-nov-2026.
set -eu

VERSION=v1.9.15
DEST=${1:-.}

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)          ASSET=go2rtc_linux_amd64;  SHA=20b46c6910734cb05ecefb234066f36868ff19b42ba1418a7a32e13a3e035712 ;;
  Linux-aarch64|Linux-arm64) ASSET=go2rtc_linux_arm64; SHA=af416f22b21a24ab83b8d78642190b4932ae695b4e386853f11d1b0319781763 ;;
  Linux-armv7l)          ASSET=go2rtc_linux_arm;    SHA=cbffe4075a004e2015fd9d7e324800ab7968b52f49e9ce803361879d5e246028 ;;
  Linux-armv6l)          ASSET=go2rtc_linux_armv6;  SHA=7dc73f47548fbe472fb6e90b01abe6c1eb50e0073b22bfc4bcf34feaabc6c88d ;;
  Darwin-arm64)          ASSET=go2rtc_mac_arm64.zip; SHA=d0cf858a71803dce2377423bc830639137ee96c335f88e9912e7f7898374266d ;;
  Darwin-x86_64)         ASSET=go2rtc_mac_amd64.zip; SHA=7ca6075421b824eebe0383d20bd42a6f6a7ec7591a60931d42290f1d375169b9 ;;
  *) echo "Plataforma no soportada: $(uname -s) $(uname -m). En Windows usa scripts/descargar-go2rtc.ps1." >&2; exit 1 ;;
esac

URL="https://github.com/AlexxIT/go2rtc/releases/download/$VERSION/$ASSET"
TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

echo "Descargando $ASSET ($VERSION)…"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL --proto '=https' --tlsv1.2 -o "$TMP" "$URL"
elif command -v wget >/dev/null 2>&1; then
  wget -q -O "$TMP" "$URL"
else
  echo "Hace falta curl o wget." >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  GOT=$(sha256sum "$TMP" | cut -d' ' -f1)
else
  GOT=$(shasum -a 256 "$TMP" | cut -d' ' -f1)
fi
if [ "$GOT" != "$SHA" ]; then
  echo "¡La suma SHA-256 no coincide! Esperaba $SHA y llegó $GOT. No instalo nada." >&2
  exit 1
fi

mkdir -p "$DEST"
case "$ASSET" in
  *.zip) unzip -o -q "$TMP" go2rtc -d "$DEST" ;;
  *)     cp "$TMP" "$DEST/go2rtc" ;;
esac
chmod 755 "$DEST/go2rtc"
echo "go2rtc $VERSION comprobado e instalado en $DEST/go2rtc"
