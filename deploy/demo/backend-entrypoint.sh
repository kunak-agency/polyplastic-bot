#!/bin/sh
# Демо-стек (docker-compose.demo.yml): сид демо-данных, затем API и бот.
# Сид идемпотентен: повторный старт контейнера никого не дублирует. Данные синтетические.
# DEMO_SEED=0 — пропустить сид (например, после `make seed-reset` вручную).
set -u
cd /app

if [ "${DEMO_SEED:-1}" != "0" ]; then
  echo "demo: сид — 2 сценария и 3 новичка (cmd/seed)"
  /app/seed || echo "demo: WARN сид завершился с ошибкой, API всё равно стартует" >&2
  echo "demo: сид — демо-поток для «Обзора» (cmd/seed-demo, ~150 персон)"
  /app/seed-demo || echo "demo: WARN seed-demo завершился с ошибкой, API всё равно стартует" >&2
fi

if [ -z "${MAX_BOT_TOKEN:-}" ]; then
  echo "demo: MAX_BOT_TOKEN пуст — бот на подделке MAX, путь новичка проходится в «Демо-чате» кабинета"
else
  echo "demo: MAX_BOT_TOKEN задан — живой бот MAX (long polling). Один токен — один запущенный бот."
fi

exec /app/api
