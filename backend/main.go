// «Корсик» — бот адаптации новичков в MAX (кейс ПОЛИПЛАСТИК): HTTP-сервер кабинета (Fiber) и
// long polling бота и планировщик таймлайна в одном процессе. Схему мигрирует этот бинарник
// (и cmd/seed — перед заливкой демо-данных в пустую базу).
// RAG и разметка флагов — в Python-сервисе services/rag, модель — на поде RunPod.
package main

import (
	"context"
	"log"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/externalApi/max"
	"main/handlers"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

// devOrigin — источник dev-сервера Vite. Подставляется, когда ORIGINS не задан: пустое
// значение fiber трактует как «*», и браузер начинает пускать к API любую страницу.
const devOrigin = "http://localhost:5173"

func main() {
	_ = godotenv.Load() // в проде файла нет, ошибка игнорируется намеренно

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("конфигурация: %v", err)
	}
	for _, line := range cfg.Summary() {
		logger.Info("%s", line)
	}

	repos, err := repositories.InitRepos(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("база: %v (DATABASE_URL задан, но не открывается — make up?)", err)
	}
	if err := repos.MigrateDB(); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	tax, err := taxonomy.LoadDefault(cfg.TaxonomyPath)
	if err != nil {
		log.Fatalf("%v — без таксономии обращения некуда направить (TAXONOMY_PATH)", err)
	}
	logger.Info("таксономия флагов: %s, версия %d, категорий %d", tax.Path, tax.Version, len(tax.Codes()))
	ext, err := externalApi.New(cfg)
	if err != nil {
		log.Fatalf("клиент MAX: %v", err)
	}
	svc := services.NewServices(repos, ext, services.Options{
		RAGTimeout: cfg.RAG.HealthTimeout,
		Bot:        cfg.Bot,
		PackGap:    cfg.Scheduler.PackGap,
		Taxonomy:   tax,
		HRChatID:   cfg.Max.HRChatID,
		SBChatID:   cfg.Max.SBChatID,
		Digest:     services.DigestOptions{UserIDs: cfg.Digest.UserIDs, CabinetURL: cfg.Digest.CabinetURL, LLM: cfg.LLM},
		Media:      services.MediaOptions{MaxUploadMB: cfg.Docs.MediaMaxMB},
		Docs:       services.DocsOptions{MaxUploadMB: cfg.Docs.MaxUploadMB, UploadTimeout: cfg.Docs.UploadTimeout, OperatorToken: cfg.Docs.OperatorToken},
	})
	h := handlers.NewHandlerManager(svc)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Бот — только с токеном (или на подделке в development): без него HTTP и кабинет работают,
	// про бота сказал WARN в externalApi. marker long polling живёт в БД: после рестарта бот
	// читает очередь с того же места.
	if svc.Bot != nil {
		poller := &max.Poller{Client: ext.Max, Handle: svc.Bot.Handle}
		if repos.Enabled() {
			if m, err := repos.BotState.Marker(ctx, domain.MarkerKeyMax); err != nil {
				logger.Warn("бот: marker не прочитан, читаем с начала очереди: %v", err)
			} else {
				poller.Marker = m
			}
			poller.OnMarker = func(m int64) {
				if err := repos.BotState.SaveMarker(context.WithoutCancel(ctx), domain.MarkerKeyMax, m); err != nil {
					logger.Warn("бот: marker не сохранён: %v", err)
				}
			}
		}
		go func() { _ = poller.Run(ctx) }()
		// Меню команд в MAX — после рестарта тоже: свои + команды опубликованных сценариев.
		go svc.SyncBotCommands(ctx)
		go func() { <-ctx.Done(); svc.Bot.Wait() }()
	}
	// Планировщик таймлайна: без базы ему нечего читать, без бота — некому отправлять.
	if repos.Enabled() && ext.Max != nil {
		go svc.Scheduler.Run(ctx, cfg.Scheduler.Interval)
		go svc.Digest().RunWeekly(ctx) // дайджест руководителю подбора, пн 9:00
		go svc.Manager().Run(ctx)      // push руководителю подразделения
	}

	// База знаний: rag держит загрузки в памяти — дождаться его и перезалить сохранённые документы.
	go svc.Documents.Resync(ctx)

	app := fiber.New(handlers.ServerConfig(handlers.DefaultTimeouts))
	// Порядок: recover (самый внешний) → номер запроса → fiberlogger → cors → routes.
	// Номер должен появиться до logger, чтобы traceId был в контексте раньше.
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e any) {
			logger.Error("[%s] panic: %v\n%s", handlers.TraceID(c), e, debug.Stack())
		},
	}))
	app.Use(handlers.RequestID)
	app.Use(fiberlogger.New())
	// AllowHeaders не задаём: при пустом значении fiber отражает запрошенные заголовки.
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Join(origins(cfg.Origins), ","),
	}))
	handlers.RegisterRoutes(app, h)

	go func() {
		<-ctx.Done()
		logger.Info("остановка: сигнал получен")
		_ = app.ShutdownWithTimeout(5 * time.Second)
	}()

	logger.Info("HTTP on %s", cfg.Addr)
	if err := app.Listen(cfg.Addr); err != nil {
		log.Fatal(err)
	}
}

// origins — список источников для CORS. Пустой ORIGINS означает локальную разработку: вместо
// «*» подставляем dev-сервер фронта и говорим об этом в лог, чтобы подстановка не осталась
// незамеченной в развёрнутом окружении.
func origins(list []string) []string {
	if len(list) == 0 {
		logger.Warn("ORIGINS пуст — CORS открыт только для %s", devOrigin)
		return []string{devOrigin}
	}
	return list
}
