# Стек и окружение

Стек тот же, что в `hack-ru-clinik`: версии пакетов и конвенции берём оттуда
(`backend/go.mod`, `frontend/package.json`, `services/*/requirements.txt`). Ресерч от 23.10
предлагал Python-бэкенд (`maxapi` + FastAPI + APScheduler). У нас Python остаётся только для
ML. Бот, планировщик и бизнес-правила пишем на Go.

Статус: план, кода ещё нет. Пометка **[проверить]** значит, что пункт не сверен с
первоисточником.

## Раскладка

| Часть | Где | Что делает |
|---|---|---|
| `backend/` | Go 1.25, модуль `main` | бот MAX (long polling `GET /updates`), планировщик `scenario_steps` → `scheduled_events` + поллер, троттлинг ≤ 2 сообщений/с на диалог, обращения и маршруты F1–F10, regex-триггеры флагов, REST кабинета, «машина времени» |
| `services/rag` | Python 3.13 в Docker, FastAPI | парсинг PDF/PPTX/XLSX, OCR, чанкинг по пунктам, гибридный поиск, реранкер, gate, `POST /ask` → JSON с цитатами |
| `services/flags` (можно слить с rag) | Python, FastAPI | разметка свободного текста по таксономии F1–F10 через LLM + rubert-tiny как второй сигнал |
| `frontend/` | Vue 3 SPA | кабинет HR: новички, траектория, обращения, сценарии, база знаний, обзор |
| LLM | под RunPod, vLLM | T-lite-it-2.1-FP8, OpenAI-совместимый API (как `hack-ru-clinik/deploy/pod/README.md`) |
| БД | Postgres 16 в Docker | всё состояние; векторы — pgvector или Qdrant (решить на старте) |

Правило из clinik и из ресерча: **правила держит код, а не промпт.** Эскалацию, маршруты
(F5/F9 не попадают к руководителю), лимиты сообщений, тихие часы и проверку цитат (`clause_id`
должен быть среди найденных чанков, цитата должна совпадать с текстом чанка) делает Go или
Python-код. LLM только формулирует ответ.

## Backend (Go)

Пакеты как в clinik: `gofiber/fiber/v2`, `gorm.io/gorm` + `gorm.io/driver/postgres`,
`jackc/pgx/v5`, `google/uuid`, `oklog/ulid/v2`, `joho/godotenv`, `gopkg.in/yaml.v3`,
`xuri/excelize/v2` (импорт Excel-таймлайна ТЗ). Раскладка каталогов — по clinik
(`domain/`, `repositories/`, `services/`, `handlers/`, `externalApi/`, `cmd/`, `wiring/`).

Bot API MAX:
- REST `https://platform-api2.max.ru`, токен в заголовке `Authorization` без `Bearer`.
- Нужен корневой сертификат Минцифры в trust store клиента.
- Отложенной отправки нет, планировщик свой.
- Клиент: официальный `github.com/max-messenger/max-bot-api-client-go/v2`, версия **v2.4.6**.
  Зафиксировать её в `go.mod`. Обернуть в наш интерфейс в `externalApi/max`, чтобы бизнес-код
  не зависел от SDK и тесты шли на подделке. Фреймворк `maxbot` не берём (0 звёзд, обёртка
  над тем же клиентом).

### Проверка клиента (23.10, ветка `v2`, коммит `971ffd4` от 22.10)

Живой: Apache-2.0, 93 звезды, коммиты каждую неделю, 1 открытый issue. `go build` пакета и
`model/` проходит на Go 1.24. Пример `example/simple` требует Go 1.26 (`errors.AsType`). У нас
1.25, нам это не мешает.

Что есть и закрывает кейс:
- `GetUpdates` (long polling с `marker`, таймаут через `WithPollingTimeout`) и webhook-хендлер
  `GetHandler` с секретом.
- `Send` с `user_id`/`chat_id`/`disable_link_preview` в query, `notify`, `format`.
- `EditMessage`, `AnswerOnCallback` с `message` или `notification` — на этом строятся «живые
  опросы».
- Кнопки всех семи типов: `AddCallBack`, `AddLink`, `AddMessage`, `AddContact`,
  `AddGeoLocation`, `AddOpenApp`, `AddClipboard`.
- `Upload`: image/video/audio/file. Ретрай на `attachment.not.ready` с паузами 1 и 2 с, до 3
  попыток.
- Типы апдейтов `bot_started` (поле `Payload`), `message_callback`, `bot_added`, `bot_stopped`,
  `dialog_muted`.
- В contact-вложении есть `vcf_info` и `hash`.
- `ValidateInitData` проверяет подпись Mini App (HMAC от `WebAppData`).
- `PatchCommands` задаёт команды бота; работа с чатами (pin, admins, members).
- Хост по умолчанию `platform-api2.max.ru`, токен уходит в `Authorization` без `Bearer`.

Чего нет, дописываем сами:
- **`transcription` у аудио.** В SDK вложена `schema.yaml` v0.0.30, в модели `Attachment`
  этого поля нет. Ресерч ссылается на v0.0.33. Голосовые парсим сырым JSON-ом, поверх
  SDK-модели **[проверить: приходит ли поле вообще]**.
- **Лимит 2 сообщения/с на диалог и 429.** SDK повторяет только `attachment.not.ready`.
  Очередь отправки по чатам с паузой и обработку 429 делаем в своей обёртке.
- **Сертификат Минцифры.** SDK его сам не подкладывает. Передаём свой `http.Client` с CA через
  `WithHTTPClient`.
- `AddCallback(text, intent, payload)` игнорирует intent (цвет кнопки) — берём `AddCallBack`.
- **Deep link.** Вложенная схема даёт формат `https://max.ru/<bot>/start/<payload>`, ресерч и
  UX-спецификация — `https://max.ru/<bot>?start=<payload>`. Проверить оба на телефоне.

Вывод: тонкий клиент с нуля писать не нужно, берём SDK и закрываем перечисленные дыры в своей
обёртке.
- Официальный `max-agents-skill` пригодится агентам как справочник по API.

LLM-клиент — как `hack-ru-clinik/backend/externalApi/llm` (OpenAI-совместимый, `presence_penalty`
1.0 обязателен для T-lite).

## Python-сервисы

Конвенции из `hack-ru-clinik/services/tts`: FastAPI + uvicorn + pydantic, точные версии в
`requirements.txt`, `run.sh`, `/health`, свой `README.md`. Отличие от clinik: сервисы
запускаются в Docker (в compose), а не из venv на хосте.

Библиотеки из ресерча, блок B (версии зафиксировать при установке):
- OCR: Tesseract 5 `rus+eng`, `pypdfium2` (300 dpi); запасной вариант RapidOCR.
- Парсеры: `python-pptx`, `openpyxl`; Docling как вариант.
- Эмбеддинги `BAAI/bge-m3` (запасной `ai-forever/FRIDA`), лексика `rank_bm25` + `pymorphy3`,
  RRF k=60, реранкер `BAAI/bge-reranker-v2-m3`. Gate по порогу реранкера: стартовое значение
  0.3, калибровать.
- Флаги: `cointegrated/rubert-tiny-sentiment-balanced` плюс LLM со structured outputs vLLM.
- Голос, контур (а): поле `transcription` из MAX **[проверить]**, иначе ffmpeg → T-one через
  sherpa-onnx. В clinik уже есть `sherpa-onnx-go`, так что это можно сделать на Go.

## Frontend (Vue)

Зависимости как в `hack-ru-clinik/frontend/package.json`: vue 3.5, vue-router 5, pinia 3, axios,
vue-i18n, @vueuse/core, reka-ui (shadcn-vue), tailwindcss 4, lucide-vue-next, vue-sonner. Для
графиков на дашборде — `echarts` + `vue-echarts`, как в toplivo. pnpm 10.28.2, Node 22. На этой
машине pnpm запускать с префиксом `npm_config_node_version=22.22.2`.

В UX-спецификации упоминается Gravity UI. Это React, поэтому берём только паттерны экранов, а
компоненты собираем на shadcn-vue.

## Окружение

- Секреты лежат только в `.env` (шаблон `.env.example` в корне): `MAX_BOT_TOKEN`,
  `RUNPOD_API_KEY`, `LLM_*`, `DATABASE_URL`.
- Фича-флаги итерации 24.10 (`config.Features`, `backend/.env.example`): `FEATURE_REPLIES`,
  `FEATURE_AUTH`, `FEATURE_TZ`, `FEATURE_NUDGES_DEFAULT`, `FEATURE_CATCHUP`. Все bool
  (`strconv.ParseBool`), по умолчанию выключены; сводка старта пишет, какие включены. На ВМ
  переходят через `deploy/terraform/scripts/make-env.sh`. Включаем по одному после прогона на
  телефоне: replies → auth → tz → nudges_default → catchup; откат — выключить флаг.
- Имя compose-проекта задаём явно (`name: polyplastic`). Без него соседние `backend/`
  конфликтуют, на этом уже обжигались в hack-kosmos.
- Postgres слушает `127.0.0.1:5437`. Порт 5432 занят системным Postgres, 5433–5436 заняты
  соседними проектами.
- Под RunPod поднимаем и гасим сами. Каждый раз сначала называем цену в час.
