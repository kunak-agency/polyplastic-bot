# Стенд «Корсика» на ВМ (Terraform)

Postgres 16, API с ботом MAX (Go-бинарник, собранный на Mac) и кабинет (статика за Caddy) на готовой
ВМ Timeweb Cloud `72.56.247.146` (Ubuntu 26.04, 1 vCPU, 2 ГБ). На ВМ ничего не собирается. RAG
работает отдельно, на GPU-поде RunPod (`rag_base_url`).

```
браузер ── HTTPS + basic auth ──► Caddy :443 ─┬─ /api/* ──► api:8081 (Go: API + бот + планировщик) ──► postgres:5432
                                              └─ /*     ──► /srv/site (SPA, fallback на index.html)
api ──► MAX (long polling, CA Минцифры)    api ──► RAG на RunPod (RAG_BASE_URL + RAG_API_KEY)
```

## Что делает `terraform apply`

| Ресурс | Где | Что |
|---|---|---|
| `null_resource.bootstrap` | ВМ | docker.io + docker-compose-v2, swap 1 ГБ, ufw: 22, 80, 443 (tcp и udp), 10050. Идемпотентно |
| `null_resource.build` | Mac | `scripts/build.sh`: `bin/api` (linux/amd64) и кабинет (`VITE_API_BASE_URL=/api`, `VITE_USE_MOCKS=false`) в `.build/vm/` |
| `null_resource.env` | Mac | `scripts/make-env.sh`: `.build/vm/.env` (права 600) из `.env` и `backend/.env` |
| `local_file.*` | Mac | `compose.yml`, `caddy/Caddyfile`, `data/taxonomy.yaml`, `certs/russian_trusted_root_ca.pem` |
| `null_resource.upload` | Mac → ВМ | `scripts/upload.sh`: rsync `.build/vm/` → `/opt/korsik/` |
| `null_resource.up` | ВМ | `docker compose up -d`, перезапуск API, перечитывание конфига Caddy |

Повторный apply выкатывает только изменения. Триггеры: хэши исходников `backend/` и `frontend/`,
хэши локальных `.env`, содержимое конфигов, переменные.

На ВМ всё лежит в `/opt/korsik`:

| Путь | Что |
|---|---|
| `compose.yml` | проект `korsik` |
| `.env` | секреты, права 600 |
| `bin/api` | бинарник API |
| `site/` | кабинет |
| `data/` | таксономия |
| `certs/` | CA Минцифры |
| `caddy/Caddyfile` | конфиг Caddy |

База и сертификаты Caddy хранятся в томах Docker (`korsik_pgdata`, `korsik_caddy_data`), а не в
этом каталоге, поэтому `rsync --delete` их не затрагивает. Дампы базы лежат в `/root/korsik-dumps`.

## Домены и DNS

| Домен | Что обслуживает | Когда заработает |
|---|---|---|
| `72-56-247-146.sslip.io` (`fallback_domain`) | кабинет и `/api` | сразу |
| `lk.plexet.ru` (`web_domain`) | кабинет и `/api` | после A-записи |
| `api-mosprom.plexet.ru` (`api_domain`) | только API, весь путь | после A-записи |

**Какие A-записи завести** в зоне `plexet.ru`:

```
lk            A   72.56.247.146
api-mosprom   A   72.56.247.146
```

Пока записей нет, работает только адрес sslip.io. Caddy пробует получить сертификаты на домены
plexet.ru и повторяет попытки с нарастающей паузой. Когда записи появятся, сертификаты выпустятся
сами, apply для этого не нужен. Ускорить можно перезапуском Caddy:
`ssh root@72.56.247.146 'cd /opt/korsik && docker compose restart caddy'`.

После того как `lk.plexet.ru` заработает, ссылки бота в MAX («Открыть аналитику») стоит перевести на
этот домен: `terraform apply -var rag_base_url=... -var cabinet_url=https://lk.plexet.ru`. Пока
`cabinet_url` пуст, ссылки ведут на sslip.io.

Кабинет обращается к API по тому же адресу (`/api` на домене кабинета). Браузер уже прошёл
basic auth на этом домене и отправляет те же учётные данные с каждым XHR, поэтому CORS не нужен. На
всякий случай `ORIGINS` в API перечисляет все домены кабинета.

## Basic auth

Весь сайт и оба домена закрыты basic auth: у кабинета нет своего логина, а в базе лежит
конфиденциальный сценарий ТЗ. Логин `jury`. Пароль генерирует `random_password`, Caddy получает
bcrypt-хэш этого пароля (`bcrypt_hash`). Пароль хранится только в state.

```sh
terraform output basic_auth_user              # jury
terraform output -raw basic_auth_password     # пароль
```

Basic auth не мешает ни одному маршруту:

- бот MAX работает через long polling, то есть сам ходит в MAX, и входящих вебхуков нет;
- dev-хуки (`/api/dev/*`) вызывает кабинет из браузера, который уже залогинен;
- `/health` наружу не проброшен, а `/api/health` доступен только с паролем. Для проверки:
  `curl -u jury:<пароль> https://72-56-247-146.sslip.io/api/health`.

Сменить пароль: `terraform apply -replace=random_password.basic_auth -var rag_base_url=...`.

## APP_ENV=development (за basic auth)

На стенде стоит `APP_ENV=development`, а кабинет собран так же, как демо-образ с `DEMO_DEVTOOLS=1`
(`NODE_ENV=development vite build`). Причины:

| Что | При `production` |
|---|---|
| «Демо-чат» (`/api/dev/chat`, `simulate-start`, `simulate-message`) и пуши руководителю, дайджест по кнопке (`/api/dev/*`) | 404: показать путь новичка без телефона нельзя |
| «Отправить себе» / «Пройти этот путь» в редакторе (`/scenarios/:id/steps/:stepId/send-test`) | 404 |
| База знаний и медиатека (`hrOnly`) | 403 без заголовка `X-Operator-Token`, а кабинет его не отправляет |
| Пункты «Демо-чат» и машина времени в меню кабинета | видны только в сборке с `import.meta.env.DEV` |

Чем за это платим: в ответах API с ошибкой есть поле `caller` (файл и строка), а `/api/dev/*`
открыт любому, кто знает пароль. На закрытом стенде для жюри это допустимо. С живым токеном
`MAX_BOT_TOKEN` бот настоящий: подделку MAX включает только пустой токен.

Сиды (`cmd/seed`, `cmd/seed-demo`) на стенде не запускаются: базу переносим с Mac.

## Переменные

| Переменная | По умолчанию | Что это |
|---|---|---|
| `rag_base_url` | обязательная | внешний адрес RAG на поде RunPod |
| `host` | `72.56.247.146` | IP ВМ |
| `ssh_key` | `~/.ssh/korsik_deploy` | ключ для root |
| `web_domain` | `lk.plexet.ru` | домен кабинета |
| `api_domain` | `api-mosprom.plexet.ru` | домен, который целиком проксируется в API |
| `fallback_domain` | `72-56-247-146.sslip.io` | домен, который работает без DNS |
| `cabinet_url` | пусто, то есть `https://<fallback_domain>` | `CABINET_URL` для ссылок бота |
| `api_enabled` | `true` | `false`: подняты только Postgres и Caddy, API и бот остановлены |
| `acme_email` | пусто | e-mail для Let's Encrypt |

Секреты (`MAX_BOT_TOKEN`, `RAG_API_KEY`, `LLM_*`) передаются не через переменные Terraform:
`scripts/make-env.sh` берёт их из корневого `.env` и `backend/.env` при apply. Если ключ есть в обоих
файлах, побеждает `backend/.env`. Под ВМ скрипт переопределяет `APP_ENV`, `PORT`, `DATABASE_URL`,
`TAXONOMY_PATH`, `MAX_CA_FILE`, `RAG_BASE_URL`, `ORIGINS` и `CABINET_URL`. Пароль Postgres
генерирует `random_password` (он лежит в state, наружу база не открыта).

## Первый запуск и переключение бота (cutover)

На одном токене MAX может работать только один long polling. Пока бот работает и на Mac, и на ВМ,
обновления делятся между двумя процессами. Поэтому порядок такой:

```sh
cd deploy/terraform
export TF_VAR_rag_base_url='https://<pod>-8093.proxy.runpod.net'

terraform init

# 1. Bootstrap ВМ, сборка, выкладка. Postgres и Caddy поднимаются, API и бот НЕ стартуют.
terraform plan  -var api_enabled=false
terraform apply -var api_enabled=false

# 2. Остановить локальный API (go run / процесс API на Mac). Локальный Postgres
#    (polyplastic-postgres-1) НЕ останавливать: из него снимается дамп.

# 3. Перенести базу: сценарий ТЗ, тестового новичка, marker long polling.
./scripts/db-transfer.sh

# 4. Поднять API и бота на ВМ.
terraform apply

# 5. Проверить.
curl -fsS -u "jury:$(terraform output -raw basic_auth_password)" https://72-56-247-146.sslip.io/api/health  # gitleaks:allow
ssh -i ~/.ssh/korsik_deploy root@72.56.247.146 'cd /opt/korsik && docker compose logs --tail 50 api'
```

Что делает `db-transfer.sh`: снимает `pg_dump -Fc` из локального контейнера и копирует его по scp
в `/root/korsik-dumps`. Затем на ВМ останавливает API, снимает копию текущей базы ВМ
(`vm-before-*.dump`) и выполняет `pg_restore --clean --if-exists --single-transaction`. В конце
сравнивает число сценариев, версий и сотрудников на Mac и на ВМ. С `START_API=1` скрипт сам
запускает API после восстановления, и шаг 4 тогда не нужен.

## Обновление

Код или локальные `.env` изменились:

```sh
cd deploy/terraform
terraform apply      # с TF_VAR_rag_base_url в окружении
```

Пересобирается только то, что изменилось. API перезапускается: бот продолжит с marker из базы.
Caddy перечитывает конфиг без простоя.

Принудительно пересобрать и выложить всё (например, если удалили `.build/`):

```sh
terraform apply -replace=null_resource.build -replace=null_resource.env
```

Посмотреть логи и состояние:

```sh
ssh -i ~/.ssh/korsik_deploy root@72.56.247.146 'cd /opt/korsik && docker compose ps && docker compose logs --tail 100 api caddy'
```

## Откат на Mac

1. Остановить API на ВМ, чтобы освободить токен MAX:
   ```sh
   ssh -i ~/.ssh/korsik_deploy root@72.56.247.146 'cd /opt/korsik && docker compose stop api'
   ```
   Пока не будет нового apply с `api_enabled=true`, API на ВМ не стартует, даже после перезагрузки ВМ:
   для `restart: unless-stopped` остановка вручную сохраняется. Надёжнее зафиксировать это в
   конфигурации: `terraform apply -var api_enabled=false`.
2. Если на ВМ появились данные, которые нужно сохранить (новые новички, ответы), перенести базу
   обратно:
   ```sh
   ssh -i ~/.ssh/korsik_deploy root@72.56.247.146 \
     'cd /opt/korsik && docker compose exec -T postgres pg_dump -U polyplastic -d polyplastic -Fc' > /tmp/korsik-vm.dump
   docker exec -i polyplastic-postgres-1 pg_restore -U polyplastic -d polyplastic --clean --if-exists --no-owner --no-privileges --single-transaction < /tmp/korsik-vm.dump
   ```
3. Запустить API локально, как обычно (`make run` или `go run .` в `backend/`).

Снести стенд целиком: `ssh ... 'cd /opt/korsik && docker compose down'`. Тома с базой останутся;
удалить их можно командой `down -v`, но это необратимо. `terraform destroy` удалит только state и
локальные файлы, ВМ он не трогает.

## Проверки без apply

```sh
terraform fmt -check && terraform validate
REPO=$(cd ../.. && pwd) VM_DIR=$PWD/.build/vm ./scripts/build.sh
docker compose -f files/compose.yml config -q   # нужен .env рядом, иначе ругается на POSTGRES_PASSWORD
docker run --rm -v "$PWD/.build/vm/caddy:/etc/caddy:ro" caddy:2.10-alpine caddy validate --config /etc/caddy/Caddyfile
```

## Секреты и git

`.build/` (бинарник, кабинет, `.env` с секретами), `*.tfstate*` (пароли basic auth и Postgres) и
`.terraform/` перечислены в `deploy/terraform/.gitignore`. `.terraform.lock.hcl` коммитится.
