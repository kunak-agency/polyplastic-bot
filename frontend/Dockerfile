# syntax=docker/dockerfile:1

# Сборка интерфейса и раздача статики (docker-compose.demo.yml, `make demo`).
# nginx раздаёт SPA и проксирует /api на бэкенд (API_UPSTREAM), поэтому в демо
# VITE_API_BASE_URL=/api: кабинет и API на одном origin, CORS не нужен.
#
# Почему адрес API — аргумент СБОРКИ, а не переменная окружения контейнера:
# это SPA, весь код исполняется в браузере, и подставить туда значение после
# сборки нечем. Тот же адрес уходит в заголовок CSP (vite.config.ts,
# cspPlugin), то есть смена адреса API требует пересборки образа, а не
# перезапуска контейнера. Относительный "/api" покрывается 'self'.

# ---- сборка ----
# Версия Node закреплена: сборка на разных мажорных версиях даёт разный
# результат чаще, чем хотелось бы.
FROM node:22-alpine AS builder

WORKDIR /src

# pnpm ставится через corepack — версия берётся из поля packageManager в
# package.json (сейчас 10.28.2). Так в образе оказывается ровно тот pnpm,
# которым собирали локально, без отдельной строки с номером здесь.
RUN corepack enable

# Манифест и лок-файл отдельным слоем: правка кода не инвалидирует установку
# зависимостей, а их установка — самый долгий шаг.
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .

ARG VITE_API_BASE_URL
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
# DEMO_DEVTOOLS=1 — сборка с import.meta.env.DEV=true (NODE_ENV=development): в меню
# появляется «Демо-чат» (/dev/chat), эмулятор диалога новичка с ботом. API отвечает
# на /api/dev/* только при APP_ENV=development — для показа без токена MAX это и нужно.
ARG DEMO_DEVTOOLS=0

# `pnpm build` — это `vue-tsc -b && vite build`: сборка падает на ошибке типов,
# и это правильно. Молча собранный фронт с несходящимися типами — то, что
# потом ловится на демонстрации.
RUN test -n "$VITE_API_BASE_URL" || (echo "VITE_API_BASE_URL не задан: фронт собрался бы с localhost" >&2; exit 1) \
    && if [ "$DEMO_DEVTOOLS" = "1" ]; then NODE_ENV=development pnpm build; else pnpm build; fi

# ---- раздача ----
FROM nginx:1.27-alpine

COPY --from=builder /src/dist /usr/share/nginx/html

# Шаблон обрабатывает envsubst из entrypoint образа nginx: подставляются только
# заданные переменные окружения (API_UPSTREAM), $uri и прочие переменные nginx
# не трогаются. try_files с фолбэком на index.html обязателен: навигация
# клиентская, без него прямой заход по ссылке или F5 на внутренней странице
# даёт 404.
ENV API_UPSTREAM=http://backend:8081
COPY <<'NGINX' /etc/nginx/templates/default.conf.template
server {
    listen 80;
    server_name _;
    root /usr/share/nginx/html;

    # Загрузка документов базы знаний и медиатеки: потолок тела запроса API — 26 МБ.
    client_max_body_size 30m;
    gzip on;
    gzip_types text/css application/javascript application/json image/svg+xml;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;

    location /api/ {
        proxy_pass ${API_UPSTREAM};
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        # Разбор скана в базе знаний (OCR) идёт минутами: срок загрузки в API — 3 мин.
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }

    location / {
        try_files $uri /index.html;
    }
}
NGINX

EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/ || exit 1
