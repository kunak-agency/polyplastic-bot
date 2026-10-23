# syntax=docker/dockerfile:1

# Сборка интерфейса и раздача статики для локального запуска
# (корневой docker-compose.yml).
#
# Почему адрес API — аргумент СБОРКИ, а не переменная окружения контейнера:
# это SPA, весь код исполняется в браузере, и подставить туда значение после
# сборки нечем. Тот же адрес уходит в заголовок CSP (vite.config.ts,
# cspPlugin), то есть смена адреса API требует пересборки образа, а не
# перезапуска контейнера.

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

# `pnpm build` — это `vue-tsc -b && vite build`: сборка падает на ошибке типов,
# и это правильно. Молча собранный фронт с несходящимися типами — то, что
# потом ловится на демонстрации.
RUN test -n "$VITE_API_BASE_URL" || (echo "VITE_API_BASE_URL не задан: фронт собрался бы с localhost" >&2; exit 1) \
    && pnpm build

# ---- раздача ----
FROM caddy:2-alpine

COPY --from=builder /src/dist /srv

# try_files с фолбэком на index.html обязателен: навигация клиентская, и без
# него прямой заход по ссылке или F5 на внутренней странице даёт 404 — а ссылки
# из README и с защиты открываются именно прямо.
RUN printf '%s\n' \
    ':80 {' \
    '  root * /srv' \
    '  encode zstd gzip' \
    '  try_files {path} /index.html' \
    '  file_server' \
    '}' > /etc/caddy/Caddyfile

EXPOSE 80
