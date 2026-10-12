#!/usr/bin/env bash
# Собирает клиент WDTT Plus (go_client из github.com/Ivan4537/WDTT-Plus) для
# Windows в assets/wdtt-client.exe — его встраивает RWDTT.
# Версия WDTT Plus закреплена в engine/WDTT_PLUS_REF; обновить:
#   echo <commit> > engine/WDTT_PLUS_REF && scripts/build-engine.sh
set -euo pipefail
cd "$(dirname "$0")/.."
REF="$(tr -d '[:space:]' < engine/WDTT_PLUS_REF)"
OUT="$PWD/assets/wdtt-client.exe"
SRC="${WDTT_PLUS_SRC:-$(mktemp -d)/wdtt}"

if [[ ! -d "$SRC/.git" ]]; then
  git -c core.longpaths=true clone --quiet --filter=blob:none https://github.com/Ivan4537/WDTT-Plus "$SRC"
fi
git -C "$SRC" -c core.longpaths=true fetch --quiet origin "$REF" 2>/dev/null || true
git -C "$SRC" -c core.longpaths=true checkout --quiet "$REF"

(cd "$SRC/go_client" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w" -o "$OUT" .)
echo "WDTT Plus ${REF:0:7} → $OUT ($(wc -c < "$OUT") байт)"
