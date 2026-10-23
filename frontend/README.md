# Корсик — frontend (кабинет HR)

Web-кабинет HR для «Корсика», бота адаптации новичков в МАКС (кейс ПОЛИПЛАСТИК).
Каркас: левая навигация по sitemap (Обзор, Новички, Обращения, Сценарии, База
знаний, Аналитика, Настройки), переключатель площадки «Все · GrPP · Luzhniki ·
GTP», плашка статуса API (`GET /api/health`). Итерация 1: «Новички» (реестр, запуск
адаптации с предпросмотром плана, приглашение с QR), индивидуальная траектория
`/newcomers/:id` с машиной времени, «Сценарии» (список, импорт Excel, редактор
`/scenarios/:id` в виде таблицы, публикация). Итерация 3: «Обращения» (список и канбан,
views, SLA-таймер, бейдж открытых в навигации) и карточка `/tickets/:id` (сводка, переписка,
ответ в МАКС, внутренняя заметка, контекст), блок «Обращения» на траектории; API — контракт v2.
Остальные разделы — заглушки. Экраны — `agency/playbook/artifacts/research/2026-10-23-ux-spec-v1.md`, §3;
API — `agency/system/api_contract.md`.

Стек и конфиги — как в `hack-ru-clinik/frontend`: Vue 3.5 + TypeScript, Vite 8,
vue-router 5, Pinia 3, axios, vue-i18n (только `ru`), reka-ui (shadcn-vue),
Tailwind 4, lucide-vue-next, vue-sonner, echarts + vue-echarts.

## Запуск

Node 22, pnpm 10.28.2 (`corepack enable`). На машине разработки pnpm запускается
с префиксом `npm_config_node_version=22.22.2`. Без него падает с ошибкой
`ERR_PNPM_INVALID_NODE_VERSION`.

```bash
cp .env.example .env
npm_config_node_version=22.22.2 pnpm install
npm_config_node_version=22.22.2 pnpm dev     # http://localhost:5173
```

| Ключ | Дефолт | Смысл |
|---|---|---|
| `VITE_API_BASE_URL` | `http://localhost:8081/api` | база API, его origin — единственный внешний в CSP |
| `VITE_APP_NAME` | `Корсик` | имя во вкладке браузера (иначе `common.appName`) |
| `VITE_USE_MOCKS` | `false` | ровно `true` — запросы обслуживает in-bundle mock-адаптер (`src/mocks`), бэкенд не нужен |

Ключи запекаются при сборке: после правки `.env` перезапустите dev-сервер.

## Гейты

Все должны быть зелёными, порядок как в CI:

```bash
export npm_config_node_version=22.22.2
pnpm install
pnpm lint                 # ESLint (warning = ошибка), src/components/ui игнорируется
pnpm format:check         # Prettier
pnpm type-check           # vue-tsc -b
pnpm build                # vue-tsc -b && vite build
pnpm check:cyrillic       # ни одной кириллицы в src/** вне i18n/locales и mocks
pnpm check:build-output   # dist: нет inline-скриптов, есть CSP-мета
pnpm test                 # node --test tests/*.test.ts — чистые функции из src/lib
```

Тесты грузят модули `src/lib` напрямую через type stripping Node 22, поэтому в
тестируемых модулях — только `import type`, без относительных импортов кода.

## Правила

- Пользовательские строки — только в `src/i18n/locales/ru/<module>.json`. Пишем
  на языке HR (ux-spec §6): «адаптация», а не «онбординг»; «обращение», а не
  «тикет»; «метка», а не «тег».
- Все запросы — через `http` из `src/api/http.ts`: там нормализация ошибок
  `{"error":{"code","message","traceId"}}` и подключение моков.
- Внешних origin нет: шрифты лежат в `public/fonts`, иконки — компоненты lucide,
  графики бандлятся.
- В моках — только синтетика, без реальных ПДн. Состояние моков (`src/mocks/state.ts`)
  живёт в памяти до перезагрузки страницы.
- QR-код приглашения рисует `qrcode` в браузере (data:-картинка), сетевых запросов нет.
- Только в `pnpm dev`: `/dev/chat` (пункт «Демо-чат») — эмулятор диалога новичка с ботом
  на подделке MAX (API с `APP_ENV=development` без `MAX_BOT_TOKEN`; `/api/dev/chat/*`).
  Тексты экрана — в `src/i18n/locales/dev/`, грузятся только в dev: в сборку не попадают.
