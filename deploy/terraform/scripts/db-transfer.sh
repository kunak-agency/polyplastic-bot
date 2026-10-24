#!/usr/bin/env bash
# Перенос базы с Mac на ВМ: pg_dump из локального Postgres (docker exec) → scp → pg_restore в
# Postgres стенда. Сохраняет всё, что есть локально: опубликованный сценарий ТЗ, живого тестового
# новичка, marker long polling бота.
#
# Запускать ТОЛЬКО в порядке cutover из README: локальный API остановлен, на ВМ поднят Postgres
# (terraform apply -var api_enabled=false). Скрипт сам останавливает API на ВМ, если тот запущен,
# и снимает копию базы ВМ перед заменой (/root/korsik-dumps/vm-before-*.dump).
#
#   ./scripts/db-transfer.sh               # перенести, API на ВМ не запускать
#   START_API=1 ./scripts/db-transfer.sh   # перенести и сразу запустить API на ВМ
#
# Переменные: HOST (72.56.247.146), SSH_KEY (~/.ssh/korsik_deploy),
# LOCAL_CONTAINER (polyplastic-postgres-1), START_API (0).
set -euo pipefail

HOST=${HOST:-72.56.247.146}
SSH_KEY=${SSH_KEY:-$HOME/.ssh/korsik_deploy}
LOCAL_CONTAINER=${LOCAL_CONTAINER:-polyplastic-postgres-1}
START_API=${START_API:-0}
REMOTE=/opt/korsik
DUMPS=/root/korsik-dumps
DB=polyplastic
DB_USER=polyplastic

ssh_vm() { ssh -i "$SSH_KEY" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "root@$HOST" "$@"; }

ts=$(date +%Y%m%d-%H%M%S)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
dump="$work/polyplastic-$ts.dump"

counts_sql="SELECT 'scenarios='||(SELECT count(*) FROM scenarios)||' published_versions='||(SELECT count(*) FROM scenario_versions)||' employees='||(SELECT count(*) FROM employees)||' bot_chats='||(SELECT count(*) FROM bot_chats);"

echo "1/5 локально: pg_dump из $LOCAL_CONTAINER (база $DB)"
docker exec "$LOCAL_CONTAINER" pg_isready -U "$DB_USER" -d "$DB" >/dev/null
docker exec "$LOCAL_CONTAINER" psql -U "$DB_USER" -d "$DB" -tAc "$counts_sql" | sed 's/^/    локально: /'
docker exec "$LOCAL_CONTAINER" pg_dump -U "$DB_USER" -d "$DB" -Fc --no-owner --no-privileges > "$dump"
docker exec -i "$LOCAL_CONTAINER" pg_restore -l < "$dump" > /dev/null # дамп читается целиком
echo "    дамп: $(du -h "$dump" | cut -f1)"

echo "2/5 ВМ: Postgres поднят, API остановлен"
ssh_vm "set -e; cd $REMOTE; docker compose up -d --wait --wait-timeout 120 postgres; docker compose stop api"

echo "3/5 ВМ: копия текущей базы ВМ → $DUMPS/vm-before-$ts.dump"
ssh_vm "set -e; mkdir -p $DUMPS; chmod 700 $DUMPS; cd $REMOTE; docker compose exec -T postgres pg_dump -U $DB_USER -d $DB -Fc > $DUMPS/vm-before-$ts.dump"

echo "4/5 scp дампа и pg_restore --clean (одной транзакцией: ошибка — база ВМ остаётся прежней)"
scp -q -i "$SSH_KEY" -o BatchMode=yes "$dump" "root@$HOST:$DUMPS/"
ssh_vm "set -e; cd $REMOTE; docker compose exec -T postgres pg_restore -U $DB_USER -d $DB --clean --if-exists --no-owner --no-privileges --single-transaction --exit-on-error < $DUMPS/$(basename "$dump")"

echo "5/5 проверка на ВМ"
ssh_vm "cd $REMOTE; docker compose exec -T postgres psql -U $DB_USER -d $DB -tAc \"$counts_sql\"" | sed 's/^/    ВМ: /'

if [ "$START_API" = "1" ]; then
  ssh_vm "set -e; cd $REMOTE; docker compose up -d api; sleep 5; docker compose logs --tail 30 api"
  echo "API на ВМ запущен."
else
  echo "Готово. API на ВМ остановлен: terraform apply -var rag_base_url=... (api_enabled=true по умолчанию)."
fi
