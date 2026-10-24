#!/usr/bin/env bash
# Выкладка .build/vm → /opt/korsik на ВМ (local-exec). --delete: на ВМ ровно то, что собрано.
# Тома Docker (база, сертификаты Caddy) живут в /var/lib/docker и не затрагиваются.
set -euo pipefail
: "${VM_DIR:?}" "${HOST:?}" "${SSH_KEY:?}" "${REMOTE:?}"

for f in bin/api site/index.html .env compose.yml caddy/Caddyfile data/taxonomy.yaml certs/russian_trusted_root_ca.pem; do
  if [ ! -e "$VM_DIR/$f" ]; then
    echo "upload: нет $VM_DIR/$f — каталог .build очищен? terraform apply -replace=null_resource.build -replace=null_resource.env" >&2
    exit 1
  fi
done

rsync -rlptz --delete \
  -e "ssh -i $SSH_KEY -o BatchMode=yes -o StrictHostKeyChecking=accept-new" \
  "$VM_DIR/" "root@$HOST:$REMOTE/"
echo "upload: выложено в $HOST:$REMOTE"
