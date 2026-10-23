package handlers

import "github.com/gofiber/fiber/v2"

// RegisterRoutes — таблица маршрутов. ЕДИНСТВЕННОЕ место, где путь связывается с хендлером:
// маршрут, объявленный где-то ещё, обойдёт мидлвары группы.
func RegisterRoutes(app *fiber.App, h *Handler) {
	// Liveness для балансировщика и деплоя: процесс жив, зависимостей не трогает.
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Плоскость приложения. Фронт ходит сюда: VITE_API_BASE_URL указывает на /api.
	api := app.Group("/api")

	// Готовность: база и rag (agency/system/api_contract.md).
	api.Get("/health", h.Health)

	// v1 — новички, сценарии, машина времени (api_contract.md, «v1 — итерация 1»).
	api.Get("/newcomers", h.ListNewcomers)
	api.Post("/newcomers", h.CreateNewcomer)
	api.Get("/newcomers/:id", h.GetNewcomer)
	api.Patch("/newcomers/:id", h.PatchNewcomer)
	api.Get("/newcomers/:id/timeline", h.Timeline)
	api.Post("/newcomers/:id/time-travel", h.TimeTravel)

	api.Get("/scenarios", h.ListScenarios)
	api.Post("/scenarios", h.CreateScenario)
	// import — до :id, иначе «import» разберётся как id сценария.
	api.Post("/scenarios/import", h.ImportScenario)
	api.Get("/scenarios/:id", h.GetScenario)
	api.Delete("/scenarios/:id", h.DeleteScenario)
	api.Put("/scenarios/:id/steps", h.PutSteps)
	api.Post("/scenarios/:id/publish", h.PublishScenario)
	api.Post("/scenarios/:id/import", h.ImportSteps)

	// v2 — обращения (api_contract.md, «v2 — итерация 3» и «Уточнения v2»).
	api.Get("/tickets", h.ListTickets)
	api.Get("/tickets/:id", h.GetTicket)
	api.Patch("/tickets/:id", h.PatchTicket)
	api.Post("/tickets/:id/assign", h.AssignTicket)
	api.Post("/tickets/:id/reply", h.ReplyTicket)
	api.Post("/tickets/:id/note", h.NoteTicket)
	api.Post("/tickets/:id/resolve", h.ResolveTicket)

	// v3 — обзор (api_contract.md, «v3 — итерация 4»).
	api.Get("/overview", h.Overview)

	// v3 — база знаний (api_contract.md, «v3 — база знаний»). Права HR — hrOnly: ролей нет,
	// пускаем в development или с X-Operator-Token. Мидлвар на каждом маршруте, а не группой без
	// префикса: та повисла бы на всём /api, и неизвестный путь вне development стал бы 403.
	api.Get("/documents", h.hrOnly, h.ListDocuments)
	api.Post("/documents", h.hrOnly, h.UploadDocument)
	api.Post("/documents/ask", h.hrOnly, h.AskDocuments)
	api.Get("/documents/:id/versions", h.hrOnly, h.DocumentVersions)
	api.Get("/knowledge/unanswered", h.hrOnly, h.Unanswered)

	// dev — только APP_ENV=development, иначе 404 (api_contract.md, раздел «dev»).
	dev := api.Group("/dev", devOnly)
	dev.Post("/simulate-start", h.SimulateStart)
	dev.Post("/simulate-message", h.SimulateMessage)
	dev.Get("/chat/:id", h.DevChat)
	dev.Post("/chat/:id/press", h.DevChatPress)
}
