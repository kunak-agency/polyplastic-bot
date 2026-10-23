# Makefile — обвязка. Цели описываются через `## `: help собирается из этих строк.
SHELL := /bin/bash

# go.mod требует Go 1.25; на машине может стоять старее — тулчейн докачается сам.
export GOTOOLCHAIN := auto

.DEFAULT_GOAL := help
.PHONY: help up down test check secrets env run seed seed-reset

help: ## показать список целей
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

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
