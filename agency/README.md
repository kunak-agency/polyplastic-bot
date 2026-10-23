# agency/ — база знаний репозитория

`polyplastic-bot` — хакатон «Открой#Моспром» 2026, кейс ПОЛИПЛАСТИК: «Корсик», ИИ-помощник
адаптации новичков в мессенджере MAX. Бот ведёт новичка по таймлайну D−3…D+90 от даты выхода,
отвечает по регламентам с цитатой пункта, собирает пульс-опросы и передаёт красные флаги
HR / руководителю / СБ. HR правит сценарии в web-кабинете.

Сроки: 23–25.10.2026; чек-поинты 24.10 11:00 и 18:00, pitch-roast 24.10 19:00, загрузка решения
до 25.10 10:00, защиты 25.10 с 12:00 (по ресерчу, сверить с ЛК).

Монорепозиторий (план, стек как в `hack-ru-clinik`): `backend/` (Go + Fiber + GORM + Postgres:
бот MAX, планировщик сценариев, обращения, REST кабинета), `services/` (Python в Docker: RAG,
OCR, эмбеддинги, реранкер, флаги), `frontend/` (Vue 3 SPA, кабинет HR). LLM — vLLM с T-lite на
поде RunPod, как в clinik. Подробно — `system/a_core_stack.md`.

## Карта

| Каталог | Что внутри | Кто пишет |
|---|---|---|
| `LOG.md` | Журнал работ: что сделано, решено, ждёт человека | Claude Code |
| `playbook/ROADMAP.md` | Итерации 0–6, стоп-краны, гейты человека | человек + Claude Code |
| `playbook/steps/` | Контекст кейса, архитектура, открытые вопросы | человек + Claude Code |
| `playbook/artifacts/research/` | Внешние ресерчи. **Не редактировать**, выводы выносить в `steps/` | человек |
| `playbook/artifacts/case/` | Материалы организатора (Excel-таймлайн ТЗ, ПВТР, PPTX). **Не редактировать** | человек |
| `playbook/artifacts/prompts/` | Промпты для внешнего ресерчера | Claude Code |
| `playbook/artifacts/runs/` | Отобранные прогоны бота и RAG | Claude Code |
| `pitch/` | Цифры, доводы, истории для питча (`facts.md`) | все |
| `system/` | `a_core_*` — стек, архитектура, конвенции | Documenter |
| `system/Human/` | Объяснения для нетехнических читателей | documenter-for-human |
| `tasks/`, `tasks/archive/` | PRD и Status Log | Planner / все |
| `migrations/` | Ручные миграции, агенты не выполняют | Implementer → человек |
| `prompts/` | Продуктовые LLM-промпты (RAG-ответ, флаги, дайджест) | prompt-helper |
| `diagrams/` | C4, Sequence, State | diagrammer |

## Ресерчи (23.10)

1. `playbook/artifacts/research/2026-10-23-deep-research.md` — «Корсик за 36 часов»: Bot API
   MAX, RAG на T-lite, флаги и индекс риска, рынок, экономика, голос, MoSCoW и почасовой план.
   Исходит из Python-бэкенда (`maxapi` + FastAPI) — у нас бот и планировщик на Go, см. стек.
2. `playbook/artifacts/research/2026-10-23-ux-spec-v1.md` — юзерфлоу бота и экраны кабинета:
   10 правил, таймлайн D−3…D+90, живые опросы, эскалации, конструктор сценариев, демо-сценарии A–C.

## Открыто до старта кода

- Материалы кейса (Excel-ТЗ таймлайна, ПВТР, презентации) — положить в `playbook/artifacts/case/`.
- Токен бота MAX и ключ RunPod — только в `.env` (шаблон `.env.example` в корне), не в git и не в чат.
- Тесты Bot API из ресерча (блок A): `POST /messages?user_id` без Start, `AudioAttachment.transcription`,
  ссылка на профиль, рендер разметки. Результаты — в `steps/`.
