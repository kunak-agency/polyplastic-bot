#!/usr/bin/env bash
# Локальная сборка (local-exec): бинарник API под linux/amd64 и статика кабинета в $VM_DIR.
# На ВМ ничего не собирается — там 2 ГБ памяти.
set -euo pipefail
: "${REPO:?}" "${VM_DIR:?}"

mkdir -p "$VM_DIR/bin"
echo "build: API (linux/amd64)"
( cd "$REPO/backend" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o "$VM_DIR/bin/api.new" . )
mv -f "$VM_DIR/bin/api.new" "$VM_DIR/bin/api"
chmod 755 "$VM_DIR/bin/api"

echo "build: кабинет (VITE_API_BASE_URL=/api, демо-инструменты включены)"
cd "$REPO/frontend"
[ -d node_modules ] || pnpm install --frozen-lockfile
# Тот же режим, что DEMO_DEVTOOLS=1 в frontend/Dockerfile: NODE_ENV=development даёт
# import.meta.env.DEV=true — в меню «Демо-чат» (/dev/chat) и машина времени. API отвечает на
# /api/dev/* только при APP_ENV=development (см. README, раздел APP_ENV).
export VITE_API_BASE_URL=/api VITE_USE_MOCKS=false
pnpm exec vue-tsc -b
rm -rf "$VM_DIR/site.new"
NODE_ENV=development pnpm exec vite build --outDir "$VM_DIR/site.new" --emptyOutDir
test -f "$VM_DIR/site.new/index.html"
rm -rf "$VM_DIR/site"
mv "$VM_DIR/site.new" "$VM_DIR/site"
echo "build: готово ($(du -sh "$VM_DIR/bin/api" | cut -f1) бинарник, $(du -sh "$VM_DIR/site" | cut -f1) кабинет)"
