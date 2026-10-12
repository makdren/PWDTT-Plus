#!/usr/bin/env bash
# Выпуск новой версии PWDTT Plus: проверки → тег → push.
# Сборку и публикацию релиза делает GitHub Actions (.github/workflows/release.yml),
# приложение подхватит его через «Проверить обновления».
#
#   scripts/release.sh 1.7.1 [--watch]
set -euo pipefail

VERSION="${1:-}"
WATCH="${2:-}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Использование: $0 X.Y.Z [--watch]"; exit 1; }
TAG="v$VERSION"
cd "$(dirname "$0")/.."

[[ -z "$(git status --porcelain)" ]] || { echo "Есть незакоммиченные изменения — сначала commit."; exit 1; }
git fetch --tags origin

if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
  echo "Тег $TAG уже существует."; exit 1
fi
LATEST="$(git tag -l 'v*' --sort=-v:refname | head -1)"
if [[ -n "$LATEST" ]]; then
  HIGHEST="$(printf '%s\n%s\n' "${LATEST#v}" "$VERSION" | sort -V | tail -1)"
  [[ "$HIGHEST" == "$VERSION" && "${LATEST#v}" != "$VERSION" ]] || { echo "$TAG должен быть больше $LATEST"; exit 1; }
fi

echo "== Тесты"
(cd core && go test ./...)
go test ./backend/ ./tests/
(cd frontend && npx vitest run >/dev/null && echo "frontend: ok")

echo "== Публикация $TAG"
git push origin HEAD:main
git tag -a "$TAG" -m "PWDTT Plus $TAG"
git push origin "$TAG"

echo "Тег отправлен: сборка https://github.com/makdren/PWDTT-Plus/actions"
if [[ "$WATCH" == "--watch" ]] && command -v gh >/dev/null; then
  sleep 5
  RUN_ID="$(gh run list --workflow release.yml --limit 1 --json databaseId -q '.[0].databaseId')"
  gh run watch "$RUN_ID" --exit-status
  gh release view "$TAG" --json url,assets -q '.url, (.assets[].name)'
fi
