// Package config — настройки сервиса из окружения. ЕДИНСТВЕННОЕ место, где API читает
// переменные: остальные пакеты получают готовые значения, а неверная конфигурация видна при
// старте одной сводкой, а не отказом на десятом сообщении.
//
// Секретов в логи не попадает: Summary печатает только, задан ли ключ.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Значения APP_ENV.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

type Config struct {
	AppEnv  string   // development | production
	Addr    string   // адрес HTTP-сервера, 127.0.0.1:8081 по умолчанию
	Origins []string // CORS; пусто — только dev-сервер фронта

	// DatabaseURL — пусто: сервис стартует без базы (WARN), health отвечает db: down.
	DatabaseURL string
	// TaxonomyPath — data/taxonomy.yaml (TAXONOMY_PATH); пусто — ищется от рабочего каталога.
	TaxonomyPath string

	Max       Max
	Bot       Bot
	Scheduler Scheduler
	RAG       RAG
	LLM       LLM
	Docs      Docs
	Digest    Digest
}

// Digest — еженедельный дайджест руководителю подбора и ссылки из MAX в кабинет
// (services/digest.go, services/manager.go).
type Digest struct {
	// UserIDs — получатели дайджеста в MAX (MAX_DIGEST_USER_IDS, user_id через запятую). Пусто —
	// дайджест только в лог.
	UserIDs []int64
	// CabinetURL — адрес кабинета для кнопок-ссылок «Открыть аналитику», «Подробнее» (CABINET_URL).
	CabinetURL string
}

// DefaultCabinetURL — кабинет в разработке (dev-сервер Vite).
const DefaultCabinetURL = "http://localhost:5173"

// Bot — то, что видит новичок: ссылка-приглашение и сроки её жизни.
type Bot struct {
	// Username — имя бота в MAX для deep link (MAX_BOT_USERNAME). Пусто — в ссылке заглушка
	// PlaceholderBotUsername, и об этом говорит Summary.
	Username string
	// InviteLinkFormat — fmt-шаблон ссылки: первый %s — имя бота, второй — payload inv_<token>.
	// Формат не проверен на телефоне (a_core_stack.md, «Deep link»): второй вариант —
	// https://max.ru/%s/start/%s.
	InviteLinkFormat string
	// InviteTTL — сколько живёт приглашение после даты выхода.
	InviteTTL time.Duration
	// Fake — бот на подделке MAX (без сети): только в development и без MAX_BOT_TOKEN. Нужен,
	// чтобы машина времени и сид работали на машине без доступа к MAX.
	Fake bool
}

// PlaceholderBotUsername — подстановка, пока MAX_BOT_USERNAME не задан: видно, что это не
// настоящий бот.
const PlaceholderBotUsername = "your_bot"

// DefaultInviteLinkFormat — формат deep link из UX-спецификации (п. 2.1).
const DefaultInviteLinkFormat = "https://max.ru/%s?start=%s"

// BotUsername — имя бота для ссылки или заглушка.
func (b Bot) BotUsername() string {
	if b.Username == "" {
		return PlaceholderBotUsername
	}
	return b.Username
}

// Scheduler — поллер таймлайна.
type Scheduler struct {
	Interval time.Duration // как часто искать созревшие события; по умолчанию 30 с
	// PackGap — пауза между сообщениями одному новичку в одном такте (пакет D−3 «с интервалом
	// 2–3 с», UX-спецификация п. 2.3). Лимит 2/с держит очередь обёртки MAX отдельно.
	PackGap time.Duration
}

// Max — бот в мессенджере MAX. Пустой токен — бот не стартует, HTTP работает.
type Max struct {
	Token string // MAX_BOT_TOKEN
	// CAFile — корневой сертификат Минцифры (PEM или DER), добавляется к системному пулу.
	// Пусто — только системный пул.
	CAFile string
	// PollTimeout — сколько сервер держит GET /updates без событий (параметр timeout).
	PollTimeout time.Duration
	// HRChatID, SBChatID — групповые чаты для карточек обращений (MAX_HR_CHAT_ID,
	// MAX_SB_CHAT_ID). 0 — не задан: карточка только в лог (номер и категория).
	HRChatID int64
	SBChatID int64
}

// Enabled — бот включён: токен задан.
func (m Max) Enabled() bool { return m.Token != "" }

// RAG — Python-сервис services/rag (поиск по базе знаний с цитатами).
type RAG struct {
	BaseURL       string        // http://localhost:8093
	HealthTimeout time.Duration // GET /health; короткий — health кабинета не должен висеть
	// AskTimeout — POST /ask (RAG_ASK_TIMEOUT, 12 с). Дольше бот не ждёт молча: пишет новичку
	// «ищу дольше обычного» и досылает поздний ответ (services/bot_ask.go, lateAskWindow).
	AskTimeout time.Duration
	// APIKey — общий ключ rag (RAG_API_KEY), уходит заголовком «Authorization: Bearer» в каждом
	// запросе к rag. Пусто — без заголовка: локальный rag открыт. На публичном адресе (под RunPod)
	// задавать обязательно, и тем же значением, что RAG_API_KEY у services/rag.
	APIKey string
}

// Docs — база знаний (api_contract.md, «v3 — база знаний»): загрузка документов HR через backend в rag.
type Docs struct {
	// MaxUploadMB — потолок файла на backend (KB_MAX_UPLOAD_MB, 20). У rag свой (25): наш ниже,
	// чтобы отказ «файл большой» приходил от нас понятным текстом, а не 413 от rag.
	MaxUploadMB int
	// UploadTimeout — сколько ждём POST /documents rag (KB_UPLOAD_TIMEOUT, 3 мин): индекс
	// пересобирается синхронно, с bge-m3 регламент на сотню пунктов — десятки секунд.
	UploadTimeout time.Duration
	// OperatorToken — секрет заголовка X-Operator-Token (KB_OPERATOR_TOKEN). Ролей пока нет:
	// вне development загрузка и проверка базы знаний открыты только с этим заголовком.
	// Пусто — вне development раздел закрыт (403).
	OperatorToken string
	// MediaMaxMB — потолок файла медиатеки (MEDIA_MAX_MB, 20; не больше 25 — тело запроса API 26 МБ).
	MediaMaxMB int
}

// LLM — модель на поде RunPod (vLLM, OpenAI-совместимый API), как в hack-ru-clinik. В каркасе
// бэкенд её не зовёт: значения читаются здесь, чтобы сводка старта показывала, что задано.
type LLM struct {
	BaseURL         string
	APIKey          string
	Model           string
	Temperature     float64
	PresencePenalty float64 // для T-lite обязателен 1.0: без него модель зацикливается
}

// Load — конфигурация из окружения. Ошибку возвращает только для того, без чего сервис не
// поднимется осмысленно; всё необязательное получает значение по умолчанию.
func Load() (Config, error) {
	c := Config{
		AppEnv:       env("APP_ENV", EnvDevelopment),
		Addr:         addr(os.Getenv("PORT")),
		Origins:      list(os.Getenv("ORIGINS")),
		DatabaseURL:  env("DATABASE_URL", ""),
		TaxonomyPath: env("TAXONOMY_PATH", ""),
		Max: Max{
			Token:       strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")),
			CAFile:      env("MAX_CA_FILE", ""),
			PollTimeout: duration("MAX_POLL_TIMEOUT", 30*time.Second),
			HRChatID:    integer("MAX_HR_CHAT_ID"),
			SBChatID:    integer("MAX_SB_CHAT_ID"),
		},
		Bot: Bot{
			Username:         strings.TrimPrefix(env("MAX_BOT_USERNAME", ""), "@"),
			InviteLinkFormat: env("INVITE_LINK_FORMAT", DefaultInviteLinkFormat),
			InviteTTL:        duration("INVITE_TTL", 30*24*time.Hour),
		},
		Scheduler: Scheduler{
			Interval: duration("SCHEDULER_INTERVAL", 30*time.Second),
			PackGap:  duration("SCHEDULER_PACK_GAP", 2*time.Second),
		},
		RAG: RAG{
			BaseURL:       strings.TrimRight(env("RAG_BASE_URL", "http://localhost:8093"), "/"),
			HealthTimeout: duration("RAG_HEALTH_TIMEOUT", 2*time.Second),
			AskTimeout:    duration("RAG_ASK_TIMEOUT", 12*time.Second),
			APIKey:        strings.TrimSpace(os.Getenv("RAG_API_KEY")),
		},
		Docs: Docs{
			MaxUploadMB:   int(integer("KB_MAX_UPLOAD_MB")),
			UploadTimeout: duration("KB_UPLOAD_TIMEOUT", 3*time.Minute),
			OperatorToken: strings.TrimSpace(os.Getenv("KB_OPERATOR_TOKEN")),
			MediaMaxMB:    int(integer("MEDIA_MAX_MB")),
		},
		LLM: LLM{
			BaseURL:         os.Getenv("LLM_BASE_URL"),
			APIKey:          os.Getenv("LLM_API_KEY"),
			Model:           env("LLM_MODEL", "tlite"),
			Temperature:     float("LLM_TEMPERATURE", 0.2),
			PresencePenalty: float("LLM_PRESENCE_PENALTY", 1.0),
		},
	}
	c.Digest = Digest{UserIDs: ids(os.Getenv("MAX_DIGEST_USER_IDS")), CabinetURL: strings.TrimRight(env("CABINET_URL", DefaultCabinetURL), "/")}
	c.Bot.Fake = c.AppEnv == EnvDevelopment && !c.Max.Enabled()
	if c.Docs.MaxUploadMB <= 0 || c.Docs.MaxUploadMB > 25 {
		c.Docs.MaxUploadMB = 20 // больше лимита rag (25) не имеет смысла: он всё равно откажет
	}
	if c.Docs.MediaMaxMB <= 0 || c.Docs.MediaMaxMB > 25 {
		c.Docs.MediaMaxMB = 20
	}
	return c, c.Validate()
}

// Validate — что сделает сервис бесполезным или опасным. Отсутствие базы, бота и модели — не
// ошибка старта: про них говорит Summary и WARN в main.
func (c Config) Validate() error {
	var problems []string
	switch c.AppEnv {
	case EnvDevelopment, EnvProduction:
	default:
		problems = append(problems, fmt.Sprintf("APP_ENV=%q: допустимо %s или %s", c.AppEnv, EnvDevelopment, EnvProduction))
	}
	if c.Max.CAFile != "" {
		if _, err := os.Stat(c.Max.CAFile); err != nil {
			problems = append(problems, fmt.Sprintf("MAX_CA_FILE: %v", err))
		}
	}
	if strings.Count(c.Bot.InviteLinkFormat, "%s") != 2 {
		problems = append(problems, "INVITE_LINK_FORMAT: нужно ровно два %s — имя бота и payload")
	}
	if c.RAG.AskTimeout <= 0 {
		problems = append(problems, "RAG_ASK_TIMEOUT: должен быть больше нуля")
	}
	if c.Scheduler.Interval <= 0 {
		problems = append(problems, "SCHEDULER_INTERVAL: должен быть больше нуля")
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// Summary — строки для лога при старте: что подключено, без значений секретов.
func (c Config) Summary() []string {
	return []string{
		fmt.Sprintf("HTTP %s, APP_ENV=%s", c.Addr, c.AppEnv),
		fmt.Sprintf("база: %s", set(c.DatabaseURL)),
		fmt.Sprintf("бот MAX: токен %s, CA %s, long polling %v", set(c.Max.Token), orNone(c.Max.CAFile), c.Max.PollTimeout),
		fmt.Sprintf("ссылки-приглашения: бот %s, формат %s; подделка MAX вместо бота: %v", botName(c.Bot.Username), c.Bot.InviteLinkFormat, c.Bot.Fake),
		fmt.Sprintf("обращения: HR-чат %s, чат СБ %s, таксономия %s", chat(c.Max.HRChatID), chat(c.Max.SBChatID), orNone(c.TaxonomyPath)),
		fmt.Sprintf("планировщик: такт %v, пауза в пакете %v", c.Scheduler.Interval, c.Scheduler.PackGap),
		fmt.Sprintf("rag: %s, срок /ask %v, ключ %s", c.RAG.BaseURL, c.RAG.AskTimeout, set(c.RAG.APIKey)),
		fmt.Sprintf("база знаний: файл до %d МБ, срок загрузки %v, токен оператора %s", c.Docs.MaxUploadMB, c.Docs.UploadTimeout, set(c.Docs.OperatorToken)),
		fmt.Sprintf("LLM: модель %s, адрес %s, ключ %s, temperature %.2f, presence_penalty %.2f",
			c.LLM.Model, orNone(c.LLM.BaseURL), set(c.LLM.APIKey), c.LLM.Temperature, c.LLM.PresencePenalty),
		fmt.Sprintf("дайджест: получателей в MAX %d, кабинет %s", len(c.Digest.UserIDs), c.Digest.CabinetURL),
	}
}

// addr — PORT: голый номер («8000») слушается только на loopback (api_contract.md: локально
// только 127.0.0.1), значение с двоеточием берётся как есть — в контейнере PORT=0.0.0.0:8081.
func addr(p string) string {
	p = strings.TrimSpace(p)
	switch {
	case p == "":
		return "127.0.0.1:8081"
	case strings.Contains(p, ":"):
		return p
	}
	return "127.0.0.1:" + p
}

func list(raw string) []string {
	var items []string
	for _, o := range strings.Split(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			items = append(items, o)
		}
	}
	return items
}

// ids — user_id MAX через запятую; мусор пропускается (сколько взято — в Summary).
func ids(raw string) []int64 {
	var out []int64
	for _, p := range strings.Split(raw, ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// float, duration — мусор в переменной даёт значение по умолчанию, а не падение старта; что
// взято, видно в Summary.
func float(key string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64); err == nil {
		return f
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil {
		return d
	}
	return def
}

// integer — id чата; пусто или мусор — 0 («не задан», видно в Summary).
func integer(key string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(key)), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func chat(id int64) string {
	if id == 0 {
		return "не задан"
	}
	return "задан"
}

func set(secret string) string {
	if secret == "" {
		return "не задан"
	}
	return "задан"
}

func botName(u string) string {
	if u == "" {
		return "не задан (в ссылке " + PlaceholderBotUsername + ")"
	}
	return u
}

func orNone(s string) string {
	if s == "" {
		return "не задан"
	}
	return s
}
