# Makefile — обвязка. Цели описываются через `## `: help собирается из этих строк.
SHELL := /bin/bash

# go.mod требует Go 1.25; на машине может стоять старее — тулчейн докачается сам.
export GOTOOLCHAIN := auto

.DEFAULT_GOAL := help
.PHONY: help up down test check secrets env run seed seed-reset

help: ## показать список целей
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

up: ## поднять Postgres и rag в Docker
	docker compose up -d --build

down: ## остановить контейнеры (данные Postgres остаются в томе)
	docker compose down

# Интеграционные тесты пойдут на отдельной базе polyplastic_test из `make up`; без неё они
# пропускаются, остальные проходят. -p 1: пакеты будут делить одну базу и чистить её.
TEST_DATABASE_URL ?= postgres://polyplastic:polyplastic@localhost:5437/polyplastic_test?sslmode=disable

test: ## go vet + go test бэкенда
	cd backend && go vet ./... && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -p 1 ./...

check: ## всё перед пушем: gofmt, go vet, тесты, поиск секретов
	@out=$$(cd backend && gofmt -l .); if [ -n "$$out" ]; then echo "gofmt: $$out"; exit 1; fi
	$(MAKE) test
	$(MAKE) secrets

secrets: ## поиск секретов в истории git и в подготовленном коммите (gitleaks)
	@if command -v gitleaks >/dev/null; then gitleaks git --no-banner --redact . && \
	gitleaks git --pre-commit --staged --no-banner --redact . ; \
	else echo "gitleaks не установлен (brew install gitleaks) — проверка секретов пропущена"; fi

# Пароль polyplastic в DATABASE_URL — только для локальной разработки, не для эксплуатации.
env: ## backend/.env для локального запуска с Postgres из docker-compose.yml
	@if [ -f backend/.env ]; then echo "backend/.env уже есть — не трогаю"; else \
	sed -e 's#^DATABASE_URL=.*#DATABASE_URL=postgres://polyplastic:polyplastic@localhost:5437/polyplastic?sslmode=disable#' \
	    backend/.env.example > backend/.env && echo "создан backend/.env (локальный Postgres; MAX_BOT_TOKEN и LLM_* — вручную)"; fi

run: ## API и бот на :8081 из исходников
	cd backend && go run .

# Сид идемпотентен: сценарий или новичок с тем же именем пропускается. Данные синтетические.
seed: ## демо-сценарии «Завод GTP», «Офис GrPP» и 3 синтетических новичка в базу из DATABASE_URL
	cd backend && go run ./cmd/seed

# Только APP_ENV=development: удаляет новичков (и смоук-данные агентов), сценарии, события, вопросы
# и обращения, затем заливает сид заново. Перед удалением показывает, что сотрёт, и ждёт «yes».
seed-reset: ## чистая пересборка демо-данных (development, с подтверждением)
	cd backend && go run ./cmd/seed -reset

# ---- Демо-стек одной командой (docker-compose.demo.yml) ----
# Порты демо: кабинет 18080, API 18081, RAG 18093, Postgres 15440 (переменные DEMO_*_PORT).
# Порты меняются в .env или окружением: DEMO_WEB_PORT=28080 make demo.
DEMO_COMPOSE := docker compose -f docker-compose.demo.yml
# Порт для подсказки после старта: окружение, затем .env, затем значение по умолчанию.
demo_port = $${$(1):-$$(grep -E '^$(1)=.+' .env 2>/dev/null | tail -1 | cut -d= -f2)}; $(1)=$${$(1):-$(2)}

.PHONY: demo demo-build demo-down demo-clean demo-logs demo-ps rag-models

demo: ## поднять всё в Docker: Postgres, API+бот, RAG, кабинет; сид демо-данных
	@if [ ! -f .env ]; then cp .env.example .env && echo "создан .env из .env.example (MAX и LLM пусты — демо-режим)"; fi
	@if [ ! -d services/rag/.cache/hub/models--BAAI--bge-m3 ]; then \
	  echo "ВНИМАНИЕ: нет весов моделей в services/rag/.cache — RAG стартует на BM25."; \
	  echo "          Рабочая конфигурация hybrid+bge: make rag-models (~6 ГБ), затем make demo."; fi
	$(DEMO_COMPOSE) up -d --build --wait --wait-timeout 900
	@DEMO_WEB_PORT=$(call demo_port,DEMO_WEB_PORT,18080); DEMO_API_PORT=$(call demo_port,DEMO_API_PORT,18081); \
	DEMO_RAG_PORT=$(call demo_port,DEMO_RAG_PORT,18093); \
	echo ""; echo "Готово:"; \
	echo "  кабинет HR   http://localhost:$$DEMO_WEB_PORT"; \
	echo "  демо-чат     http://localhost:$$DEMO_WEB_PORT/dev/chat   (без MAX_BOT_TOKEN)"; \
	echo "  API health   http://localhost:$$DEMO_API_PORT/api/health"; \
	echo "  RAG health   http://localhost:$$DEMO_RAG_PORT/health"; \
	echo "Остановить: make demo-down"

demo-build: ## только собрать образы демо-стека
	$(DEMO_COMPOSE) build

demo-down: ## остановить демо-стек (данные в томе остаются)
	$(DEMO_COMPOSE) down

demo-clean: ## остановить демо-стек и удалить его базу (следующий make demo засеет заново)
	$(DEMO_COMPOSE) down -v

demo-logs: ## логи демо-стека (Ctrl+C — выход)
	$(DEMO_COMPOSE) logs -f --tail=100

demo-ps: ## состояние контейнеров демо-стека
	$(DEMO_COMPOSE) ps

# Веса bge-m3 и bge-reranker-v2-m3 (~6 ГБ, ~10 мин) в services/rag/.cache — тот же каталог,
# что у локального rag (run.sh). Качаются образом rag, Python на хосте не нужен.
rag-models: ## скачать веса моделей RAG в services/rag/.cache (~6 ГБ)
	mkdir -p services/rag/.cache
	$(DEMO_COMPOSE) build rag
	$(DEMO_COMPOSE) run --rm --no-deps --user "$$(id -u):$$(id -g)" -e HF_HUB_OFFLINE=0 --entrypoint python rag -c \
	  "from sentence_transformers import SentenceTransformer, CrossEncoder; SentenceTransformer('BAAI/bge-m3'); CrossEncoder('BAAI/bge-reranker-v2-m3'); print('веса в services/rag/.cache')"
