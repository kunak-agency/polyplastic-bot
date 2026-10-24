# Контракт: вход, роли, дашборды, пояс (v5, 24.10)

Автор черновика — фронт-агент D. Бэкенд (агент C, пояс — агент A) сверяется с этим файлом и
правит его, если что-то не так. Фронт уже работает под этот контракт на моках
(`VITE_USE_MOCKS=true`). Общие правила — как в `api_contract.md`: ошибки
`{"error":{"code","message","traceId"}}`, списки `{"items":[...],"total":N}`, id — UUID-строки.

## Флаг `FEATURE_AUTH`

- `FEATURE_AUTH=false` (по умолчанию): маршруты `/api/auth/*` **не регистрируются**, поэтому
  `GET /api/auth/me` отвечает **404**. Кабинет по этому 404 понимает, что входа нет, и работает
  как раньше: без экрана входа, все разделы, кроме «Сегодня».
- `FEATURE_AUTH=true`: все `/api/*` кабинета без сессии → **401** `UNAUTHENTICATED`. Исключения:
  `/api/health`, `/api/auth/login`, `/api/auth/logout`, `/api/dev/*` и send-test в development, бот
  (long polling, не HTTP). Роль не подходит → **403** `PERMISSION_DENIED` (общий код ошибок API).
- Фронт при любом 401 (кроме ответа на сам `login`) уводит на `/login?next=<путь>`.

## Сессия

- Cookie сессии `korsik_session`: `HttpOnly; Secure; SameSite=Lax; Path=/`, 12 ч (без продления).
  `Secure` не ставится только при `APP_ENV=development` по http. Фронт cookie не читает.
- Сессии — строки `sessions` в Postgres; пароли — bcrypt cost 12. Лимит — 5 неудач в минуту с IP
  (в памяти процесса; за Caddy IP — последний элемент `X-Forwarded-For`).
- CORS: `AllowCredentials: true` при явном `ORIGINS` (по умолчанию `http://localhost:5173`).
- Прод: кабинет и API на одном домене (`/api`), cookie уходит сама.
- Dev: `VITE_API_BASE_URL=http://localhost:8081/api` — это другой origin. Варианты:
  1. `VITE_API_BASE_URL=/api` и прокси Vite (`vite.config.ts` уже проксирует `/api` на :8081) —
     рекомендуется;
  2. `VITE_API_WITH_CREDENTIALS=true` на фронте и `AllowCredentials: true` в CORS бэкенда
     (`ORIGINS` — явный список, не `*`). Без `AllowCredentials` браузер отклонит **все** ответы,
     поэтому по умолчанию фронт `withCredentials` не включает.

## Пользователь

```json
{"id":"uuid","login":"anna","full_name":"Анна К.","role":"hr_lead|recruiter|manager",
 "tz":"Europe/Moscow|null","manager_name":"Смирнов А.|null","active":true}
```
- `manager_name` — для роли `manager`: имя, как в `employees.manager_name` (`domain.User.ManagerName`).
- `tz` — только для показа; `null` — пояс браузера.

### `/api/auth/*`
- `POST /api/auth/login` `{"login","password"}` → 200 `{"user":{…}}` + Set-Cookie.
  Ошибки: 401 `INVALID_CREDENTIALS` («Неверный логин или пароль», в том числе для
  деактивированного), 429 `TOO_MANY_ATTEMPTS` (больше 5 неудач в минуту с IP), 422 пустые поля.
- `POST /api/auth/logout` → 204, строка сессии удаляется, cookie гасится. Без сессии — тоже 204.
- `GET /api/auth/me` → 200 `{"user":{…}}` | 401 | 404 (флаг выключен). Фронт принимает и голый
  объект пользователя без обёртки `user`.

## Роли (матрица — ресерч §4)

| Маршрут | hr_lead | recruiter | manager |
|---|---|---|---|
| `/api/newcomers*`, `/api/tickets*`, `/api/overview`, сценарии, база знаний | всё | только свои (`employees.recruiter_id = me`); сценарии — чтение | 403 |
| `/api/recruiter/dashboard` | все новички или `?recruiter_id=<uuid>\|none` | свои | 403 |
| `/api/hrlead/dashboard`, `/api/team`, `/api/users*`, `/api/newcomers/reassign` | да | 403 | 403 |
| `/api/manager/newcomers`, `/api/manager/tasks/{id}` | любой `name` | любой `name` | `name` игнорируется: подчинённые по `users.manager_name`; чужая задача → 404 |
| `/api/manager/managers` | да | да | 403 |
| тексты ответов (`timeline.answer`/`text`), обращения | да | свои | 403 |

Уточнения бэкенда: чужой новичок или обращение рекрутеру → **404** (не 403); рекрутеру сценарии —
GET и `POST /scenarios/{id}/publish/preview`, база знаний и медиатека — GET и `POST /documents/ask`,
остальные записи → 403. `/api/digest` и `/api/manager/managers` рекрутеру открыты (агрегаты).
Матрица — `handlers/auth.go` (`policy`), фильтры «только свои» — в хендлерах списков и `Overview.Get`.

Меню кабинета: manager — только «Мои новички»; recruiter — всё, кроме «Настроек» (пользователи);
hr_lead — всё. Стартовая: recruiter и hr_lead → «Сегодня» (`/dashboard`, по роли свой экран),
manager → «Мои новички». Старый «Обзор» (`/`) называется «Обзор площадок».

## Новичок: новые поля

```json
{"…": "как v1",
 "recruiter_id": "uuid|null", "recruiter_name": "Анна К.",
 "tz": "Asia/Yekaterinburg|null", "site_tz": "Asia/Yekaterinburg"}
```
- `recruiter_id` — закреплённый рекрутер (`employees.recruiter_id`). Если задан, сервер кладёт в
  `recruiter_name` его `full_name`; если нет — старое текстовое поле.
- `GET /api/newcomers?mine=1` — только `recruiter_id = me`. `?recruiter_id=<uuid>|none` — фильтр
  для hr_lead (`none` — «без рекрутера»). Для recruiter сервер ограничивает своими и без `mine`.
- `POST /api/newcomers` дополнительно принимает `recruiter_id?` и `tz?`. Роль recruiter: если
  `recruiter_id` не передан, сервер ставит `me`.
- `PATCH /api/newcomers/{id}` — **все поля необязательные**: `{"start_date"?, "tz"?, "recruiter_id"?}`.
  `recruiter_id` меняет только hr_lead (иначе 403). Ответ — новичок.

## Пояс

- Пояс площадки — `domain/site.go` (`Site.Loc`, IANA). Новичок: `tz` (nullable) переопределяет.
  В ответе всегда есть `site_tz`; действующий пояс новичка = `tz ?? site_tz`.
- `PATCH /api/newcomers/{id}` `{"tz":"Asia/Yekaterinburg"}` — задать; `{"tz":null}` — вернуть пояс
  площадки. Неизвестная IANA-зона → 422 `VALIDATION_FAILED` (`field: "tz"`). После смены сервер
  перепланирует неотправленные события (как при переносе даты).
- `fire_at` в таймлайне — ISO 8601 с зоной (момент времени). Кабинет сам показывает его в
  поясе новичка: «10:00 по времени GTP (UTC+5)» и рядом — по поясу пользователя кабинета, если
  смещения разные.
- Список поясов в выборе — фиксированный на фронте (зоны РФ, `src/lib/tz.ts`).

## Дашборды по ролям (ресерч `2026-10-24-role-dashboards.md`)

Фронт уже работает под эту форму на моках. Агент C может поменять форму — тогда фронт правит
типы `src/types/api.ts` и конфиг `src/lib/dashboard.ts`.

Общие части:
```json
"tiles": {"<id>": {"value": 3, "secondary": 1, "red": true, "ids": ["uuid"]}},
"today": [{"id": "строка", "kind": "silent", "zone": "red|yellow", "reason": "не отвечает 5 дн. после 2 напоминаний",
           "newcomer_id": "uuid|null", "newcomer_name": "…|null", "ticket_id": "uuid|null",
           "recruiter_name": "…|null", "theme": "…|null", "due_at": "ISO|null", "day": 12,
           "actions": ["contact","remind","take","open"]}],
"bot_did": {"nudges": 7, "summaries": 2}
```
- `tiles` — словарь по id плитки. Пороги («красная, если…», ресерч §1–2) считает **сервер**, фронт
  только красит `red`; нули фронт сам делает серыми. `ids` — новички под плиткой (плитка фильтрует
  список ниже); без `ids` плитка ведёт по ссылке из конфига фронта. `secondary` — второе число
  парной плитки («истекает / просрочен», «сегодня / на неделе», «ответили / отправлено»).
- `today` — до 10 строк, сервер сортирует: красные выше жёлтых, дальше по `due_at`, затем по D
  (фронт повторяет сортировку и режет до 10). `reason` — причина словами, готовая строка.
- Кнопки (`actions`): `contact` — карточка новичка с панелью «Отметить: связался»; `remind` —
  `POST /api/newcomers/{id}/remind`; `take` — `POST /api/tickets/{id}/assign` на себя и в
  обращение; `open` — обращение, иначе карточка; hr_lead: `assign` (страница обращения),
  `reassign` (обращение, иначе карточка с выбором рекрутера), `pin` (карточка), `owner` («Аналитика»).

### `GET /api/recruiter/dashboard?site=`
Свои новички (`recruiter_id = me`).
```json
{"today": [...], "bot_did": {...},
 "tiles": {"not_answering":{}, "sla":{"value":"истекает сегодня","secondary":"просрочено"},
           "starting":{"value":"сегодня","secondary":"на неделе"}, "ip_decisions":{}, "low_pulse":{},
           "catching_up":{}},
 "pulse_week": {"sent": 15, "answered": 12, "low": 2,
                "tone": {"positive": 3, "neutral": 4, "negative": 1, "n": 8} | null},
 "items": [{"newcomer_id","zone":"red|yellow|null","silent_days":3|null,
            "catchup":{"done":1,"total":4}|null,"ip_decision_date":"YYYY-MM-DD|null",
            "tickets_open":1,"tickets_sla_overdue":0}],
 "next_event": {"title","at","newcomer_name"} | null,
 "newcomers_led": 12}
```
- `kind` строк: `invite_not_opened`, `sla`, `f7_consent`, `silent`, `no_plan`, `ip_decision`,
  `catchup_behind`.
- `items` — факты для чипов списка «мои» («Не отвечает 3 дня», «Догоняет 1/4», «SLA просрочен»).
- `pulse_week` — строка под списком; малые числа (§6) фронт применяет сам: n < 5 — «мало данных»,
  n < 20 — «12 из 15», дальше проценты; `tone` при n < 5 — `null`.
- Пустое состояние (нет строк и все плитки 0) — три факта: `bot_did`, `next_event`, `newcomers_led`.

### `GET /api/hrlead/dashboard?site=` (hr_lead)
```json
{"today": [...], "bot_did": {...},
 "tiles": {"unassigned_tickets":{}, "sla_overdue":{}, "no_recruiter":{}, "red_zone":{},
           "pulse_response":{"value":"ответили","secondary":"отправлено"}, "ip_week":{}},
 "load": [{"user_id","full_name","newcomers","tickets_open","tickets_sla_overdue"}],
 "median_first_response_min": 47 | null,
 "digest": {"week": "2026-W43", "lines": ["три строки, уже по правилам n ≥ 5"]} | null}
```
- `kind` строк: `unassigned_ticket` (без исполнителя > 2 ч), `sla_overdue` (с `recruiter_name`),
  `no_recruiter`, `theme_no_owner` (с `theme`).
- `load` — только активные рекрутеры; фронт сортирует по просрочкам. Время ответа по человеку не
  отдаётся, только медиана по команде. Рейтингов нет.

### Действия
- `POST /api/newcomers/{id}/contacted` `{"note"?}` → 204 (note не хранится). Пишет строку
  `nudges` с `kind=contacted` (без `message_id`: в бюджет наджей не входит; колонки
  `employees.contacted_at` нет). Все строки «Сегодня» этого новичка, кроме `sla` и `f7_consent`,
  скрыты 2 рабочих дня; время отметки — `items[].contacted_at`.
- `POST /api/newcomers/{id}/remind` — алиас `POST /api/newcomers/{id}/nudge` (агент A, ответ и
  ошибки — как у него).

## Пользователи (hr_lead, в «Настройках»)

- `GET /api/team` →
  ```json
  {"items":[{"user_id":"uuid","full_name":"Анна К.","login":"anna","active":true,
             "newcomers":12,"tickets_open":3,"tickets_sla_overdue":1}],
   "unassigned": 2}
  ```
- `GET /api/users?role=recruiter&active=1` → `{"items":[пользователь],"total"}` — для выбора
  рекрутера в карточке, при запуске и переназначении.
- `POST /api/users` `{"login","full_name","password","role","tz"?,"manager_name"?}` → 201 пользователь.
  409 `LOGIN_TAKEN`; 422 пароль короче 8 символов / неизвестная роль.
- `PATCH /api/users/{id}` `{"active"?,"role"?,"full_name"?,"password"?}` → пользователь.
  Деактивация удаляет его сессии; его новички остаются за ним. Себя → 409 `SELF_DEACTIVATE`.
- `POST /api/newcomers/reassign` `{"newcomer_ids":["uuid"],"recruiter_id":"uuid|null"}` →
  `{"updated":N}`. `null` — снять рекрутера. Неактивный рекрутер → 422.

## Руководитель подразделения

- `GET /api/manager/newcomers` — для роли manager `name` не нужен: сервер берёт
  `users.manager_name` (фронт всё равно передаёт его, чтобы старый бэкенд тоже работал).
  Ответ как в v4 — **без** текстов ответов, обращений и категорий. Пульс фронт показывает только
  уровнем 🙂/😐/☹.
- `POST /api/manager/tasks/{id}` `{"status":"done|planned"}` → 204 — «Встретился» / «Запланировал»
  из кабинета, то же, что кнопки `mg:met`/`mg:plan` в MAX (`manager_nudges.status` = met|planned).
  `id` — `meetings[].id` из `GET /api/manager/newcomers`. Нет задачи / чужая → 404, иной статус → 422.
- «Приложить план» и «Решение по ИС» — заготовки в кабинете (кнопки неактивны), API позже.

## Личный файл: адаптационный план

Кнопка «Загрузить адаптационный план» в карточке → `POST /api/newcomers/{id}/files` (v4, уже есть:
multipart `file`, `name=adaptation_plan`, ≤ 20 МБ). `GET /api/newcomers/{id}/files` →
`{"items":[{"name","filename","mime","size","updated_at"}],"total"}` (без содержимого).

## Демо-аккаунты моков (фронт)

`hr` / `maria` / `anna` / `smirnov`, пароль у всех `demo` (только `VITE_USE_MOCKS=true`).
`VITE_MOCK_AUTH=false` — мок отвечает 404 на `/auth/me`, кабинет работает без входа.

## Бэкенд (агент C): фактические формы дашбордов

Сверено с формой выше; отличия и дополнения:
- `tiles.<id>` = `{"value", "secondary"?, "red", "ids"}`. `secondary` есть только у парных плиток
  (`sla`: value — истекает сегодня, secondary — просрочено, считаются обращения; `starting`: value —
  сегодня, secondary — на неделе вместе с сегодня; `pulse_response`: value — ответили, secondary —
  отправлено). Ключи рекрутера: `not_answering, sla, starting, ip_decisions, low_pulse, catching_up`;
  hr_lead: `unassigned_tickets, sla_overdue, no_recruiter, red_zone, pulse_response, ip_week`.
- `today[]` = `{"id","kind","zone","reason","newcomer_id","newcomer_name","site","ticket_id",
  "recruiter_name","theme","due_at","day","actions"}`; агрегатная строка `no_recruiter` —
  `newcomer_id: null`, `count`, `newcomer_ids`. `today_total` — строк до обрезки до 10.
  `actions` рекрутера: `contact, remind, take, open`; hr_lead: `assign, reassign, pin, owner, open`.
- Рекрутер: `pulse_week` = `{"sent","answered","low","enough","tone"}` (новичков, не опросов;
  `enough` — answered ≥ 5; `tone` = `{"positive","neutral","negative","n"}` или `null` при n < 5);
  `items[]` дополнительно `low_pulse`, `contacted_at`; `zone` — зона риска обзора как есть.
  `next_event` = `{"title","at","newcomer_name"}`, `newcomers_led` — новичков в адаптации.
- hr_lead: `load[]` — активные рекрутеры; `unassigned`; `median_first_response_min` (`null` при
  меньше 5 обращений с ответом за 30 дней), `median_n`; `digest` = `{"week","lines":[3 строки],
  "facts":[{"key","text","value","prev","delta","n","enough"}],"link"}` (без LLM); `bot_did`.
- `bot_did` = `{"nudges","summaries"}` за 24 ч: ушедшие авто-напоминания (`nudges.kind=auto`) и
  собранные догоняющие очереди (`employees.catchup_started_at`).
- Строки «Сегодня»: решение по ИС — только без `employees.ip_decision`; `no_plan` — D0 и нет
  `manager_nudges` со статусом planned/met; `catchup_behind` — очередь «1 блок в день» отстала на
  ≥ 2; `silent` — `unresponsive_since` (после 2 наджей) или вопрос без ответа > 3 дней, красная от
  5 рабочих дней.
- `GET /api/team` — для «Настроек»: все рекрутеры (и hr_lead с новичками), включая неактивных,
  поле `role`, плюс `team_median_first_response_min`, `team_median_n`, `team_median_enough`.

## Демо-аккаунты бэкенда

`cd backend && go run ./cmd/seed -users` (база из `DATABASE_URL`): `hr` (hr_lead «Анна Кадрова»),
`recruiter1`, `recruiter2` (имена — два самых частых `recruiter_name` в карточках), `manager`
(`manager_name` — самый частый руководитель в карточках). Пароли — `DEMO_PASSWORD_HR`,
`DEMO_PASSWORD_RECRUITER1`, `DEMO_PASSWORD_RECRUITER2`, `DEMO_PASSWORD_MANAGER`; без них новому
аккаунту пароль генерируется и печатается один раз, у существующего не меняется. Затем новичкам
без рекрутера проставляется `recruiter_id` по совпадению `recruiter_name` с `full_name`.
