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
	// Bot — обработка событий MAX (bot.go); nil, если бот выключен (нет токена и не подделка).
	Bot *BotService
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
}

// NewServices — репозитории могут быть выключены (nil): каркас поднимается без базы.
func NewServices(r *repositories.Repos, ext *externalApi.ExternalAPI, o Options) *Services {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	sch := NewSchedulerService(r, ext.Max, now, o.PackGap)
	tax := o.Taxonomy
	if tax == nil {
		t, err := taxonomy.LoadDefault("")
		if err != nil {
			panic(err) // main передаёт загруженную; сюда приходят тесты и сид из дерева репозитория
		}
		tax = t
	}
	tickets := &TicketService{repos: r, max: ext.Max, tax: tax, now: now, hrChat: o.HRChatID, sbChat: o.SBChatID}
	s := &Services{
		repos:     r,
		ext:       ext,
		Health:    NewHealthService(r, ext.RAG, o.RAGTimeout),
		Scheduler: sch,
		Newcomers: &NewcomerService{repos: r, scheduler: sch, bot: o.Bot, now: now, botReady: ext.Max != nil},
		Scenarios: &ScenarioService{repos: r, scheduler: sch, now: now},
		Tickets:   tickets,
	}
	if ext.Max != nil {
		s.Bot = NewBotService(ext.Max, r, sch, now)
		s.Bot.tickets = tickets
		if ext.RAG != nil { // nil-указатель в интерфейсе выглядел бы подключённым клиентом
			s.Bot.rag = ext.RAG
		}
	}
	return s
}
