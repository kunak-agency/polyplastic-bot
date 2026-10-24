package services

import (
	"time"

	"main/config"
	"main/externalApi"
	"main/pkg/taxonomy"
	"main/repositories"
)

// Services — бизнес-логика по ограниченным контекстам: один контекст — один файл в этом
// пакете. Слой знает про repositories и externalApi; про Fiber и HTTP — не знает.
//
// Новый контекст приезжает полем этой структуры и строкой в NewServices — иначе хендлеру
// не через что звать логику.
type Services struct {
	repos *repositories.Repos
	ext   *externalApi.ExternalAPI

	Health    *HealthService    // готовность базы и rag (health.go)
	Scheduler *SchedulerService // таймлайн и поллер (scheduler.go)
	Newcomers *NewcomerService  // реестр, запуск, машина времени (newcomers.go)
	Scenarios *ScenarioService  // конструктор сценариев (scenarios.go)
	Tickets   *TicketService    // обращения (tickets.go)
	Documents *DocumentService  // база знаний (documents.go)
	Media     *MediaService     // медиатека и вложения для бота (media.go, media_max.go)
	Auth      *AuthService      // вход в кабинет, сессии (auth.go; FEATURE_AUTH)
	Users     *UserService      // пользователи, команда, дашборды ролей (users.go, *_dashboard.go)
	// Bot — обработка событий MAX (bot.go); nil, если бот выключен (нет токена и не подделка).
	Bot *BotService
	// digestOpts — получатели дайджеста, ссылка на кабинет, LLM (digest.go, manager.go).
	digestOpts DigestOptions
	botCfg     config.Bot
	// rewriteLLM — подмена модели переписывания обращения (тесты); nil — по digestOpts.LLM.
	rewriteLLM RewriteLLM
	// features — фича-флаги (зоны риска v2 и push v2 читают RiskV2: overview.go, manager.go).
	features config.Features
}

// Features — фича-флаги слоя (GET /api/health отдаёт кабинету те, от которых зависит его вид).
func (s *Services) Features() config.Features { return s.features }

// WithFeatures — флаги для тестов HTTP-слоя без NewServices.
func (s *Services) WithFeatures(f config.Features) *Services {
	s.features = f
	return s
}

// Options — настройки слоя.
type Options struct {
	RAGTimeout time.Duration // срок проверки rag в health
	Bot        config.Bot
	PackGap    time.Duration    // пауза между сообщениями пакета одному новичку
	Now        func() time.Time // часы; nil — time.Now (тесты подставляют свои)
	// Taxonomy — маршруты флагов (data/taxonomy.yaml). nil — ищется от рабочего каталога;
	// main загружает сам и падает понятной ошибкой, если файла нет.
	Taxonomy *taxonomy.Taxonomy
	// HRChatID, SBChatID — чаты для карточек обращений; 0 — только лог.
	HRChatID int64
	SBChatID int64
	// Docs — база знаний: потолок файла, срок загрузки в rag, токен оператора.
	Docs DocsOptions
	// Media — медиатека: потолок файла (MEDIA_MAX_MB), срок токена вложения MAX.
	Media MediaOptions
	// Digest — дайджест и руководитель: получатели в MAX, адрес кабинета, LLM.
	Digest DigestOptions
	// Features — фича-флаги FEATURE_* (config.Features). Replies — ответы бота реплаем.
	Features config.Features
}

// NewServices — репозитории могут быть выключены (nil): каркас поднимается без базы.
func NewServices(r *repositories.Repos, ext *externalApi.ExternalAPI, o Options) *Services {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	managerCabinetURL = cabinetBase(o.Digest.CabinetURL) // кнопки бота руководителю (manager_bot.go)
	sch := NewSchedulerService(r, ext.Max, now, o.PackGap)
	sch.SetFeatures(o.Features) // пояса, наджи по умолчанию, догоняющая очередь (scheduler_*.go)
	tax := o.Taxonomy
	if tax == nil {
		t, err := taxonomy.LoadDefault("")
		if err != nil {
			panic(err) // main передаёт загруженную; сюда приходят тесты и сид из дерева репозитория
		}
		tax = t
	}
	tickets := &TicketService{repos: r, max: ext.Max, tax: tax, now: now, hrChat: o.HRChatID, sbChat: o.SBChatID, replies: o.Features.Replies}
	s := &Services{
		repos:     r,
		ext:       ext,
		Health:    NewHealthService(r, ext.RAG, o.RAGTimeout),
		Scheduler: sch,
		Newcomers: &NewcomerService{repos: r, scheduler: sch, bot: o.Bot, now: now, botReady: ext.Max != nil},
		Scenarios: &ScenarioService{repos: r, scheduler: sch, now: now},
		Tickets:   tickets,
		Auth:      newAuthService(r, o.Features.Auth, now),
		Users:     &UserService{repos: r, now: now, tax: tax},
		// дайджест и руководитель (digest.go, manager.go)
		digestOpts: o.Digest,
		botCfg:     o.Bot,
		features:   o.Features,
	}
	var docsRAG DocsRAG
	if ext.RAG != nil { // nil-указатель в интерфейсе выглядел бы подключённым клиентом
		docsRAG = ext.RAG
	}
	s.Documents = newDocumentService(r, docsRAG, o.Docs, now)
	var mediaUp MediaUploader
	if ext.Max != nil { // nil-интерфейс в интерфейсе выглядел бы подключённым клиентом
		mediaUp = ext.Max
	}
	s.Media = newMediaService(r, mediaUp, o.Media, now)
	if ext.Max != nil {
		s.Bot = NewBotService(ext.Max, r, sch, now)
		s.Bot.tickets = tickets
		s.Bot.replies = o.Features.Replies
		s.Bot.clarify = o.Features.Clarify
		if ext.RAG != nil { // nil-указатель в интерфейсе выглядел бы подключённым клиентом
			s.Bot.rag = ext.RAG
			s.Bot.asr = ext.RAG // голосовые: POST /transcribe того же сервиса
		}
	}
	s.SetMedia(s.Media) // вложения шагов и веток (branch_media.go)
	return s
}
