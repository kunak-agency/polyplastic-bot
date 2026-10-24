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

	// Итерация 24.10 — вход и роли (api_contract_auth.md). Только при FEATURE_AUTH: без флага ни
	// мидлвара, ни маршрутов нет (GET /api/auth/me → 404 — так кабинет узнаёт, что входа нет).
	// AuthRequired: сессия обязательна для всего /api, кроме health, login/logout и dev в
	// development; дальше матрица ролей (policy). Бот ходит не через HTTP — его это не касается.
	if h.authOn() {
		api.Use(h.AuthRequired)
		api.Post("/auth/login", h.Login)
		api.Post("/auth/logout", h.Logout)
		api.Get("/auth/me", h.Me)
		api.Get("/recruiter/dashboard", h.RecruiterDashboard)
		api.Get("/hrlead/dashboard", h.HRLeadDashboard)
		api.Get("/team", h.Team)
		api.Get("/users", h.ListUsers)
		api.Post("/users", h.CreateUser)
		api.Patch("/users/:id", h.PatchUser)
		api.Post("/newcomers/reassign", h.Reassign) // до /newcomers/:id
		api.Post("/newcomers/:id/contacted", h.MarkContacted)
	}

	// Справочник людей: руководители и наставники карточки (api_contract_auth.md, «Люди»).
	api.Get("/people", h.ListPeople)
	api.Post("/people", h.CreatePerson)

	// v1 — новички, сценарии, машина времени (api_contract.md, «v1 — итерация 1»).
	api.Get("/newcomers", h.ListNewcomers)
	api.Post("/newcomers", h.CreateNewcomer)
	api.Get("/newcomers/:id", h.GetNewcomer)
	api.Patch("/newcomers/:id", h.PatchNewcomer)
	api.Get("/newcomers/:id/timeline", h.Timeline)
	api.Post("/newcomers/:id/time-travel", h.TimeTravel)
	// v4 — флаги «по отмашке» и личные файлы новичка (branch.go).
	api.Post("/newcomers/:id/flags", h.SetNewcomerFlag)
	api.Post("/newcomers/:id/files", h.UploadNewcomerFile)
	// Решение по ИС (FEATURE_RISK_V2, api_contract.md «Решение по ИС»): рекрутер, hr_lead.
	api.Post("/newcomers/:id/ip-decision", h.SetIPDecision)
	// Итерация 24.10 — наджи: «Напомнить» и история (nudges.go).
	api.Post("/newcomers/:id/nudge", h.NudgeNewcomer)
	api.Get("/newcomers/:id/nudges", h.NewcomerNudges)
	api.Post("/newcomers/:id/remind", h.NudgeNewcomer) // алиас /nudge: кнопка «Напомнить» дашборда (api_contract_auth.md)
	api.Get("/newcomers/:id/files", h.NewcomerFiles)   // личные файлы новичка без содержимого (users.go)

	api.Get("/scenarios", h.ListScenarios)
	api.Post("/scenarios", h.CreateScenario)
	// import — до :id, иначе «import» разберётся как id сценария.
	api.Post("/scenarios/import", h.ImportScenario)
	api.Get("/scenarios/:id", h.GetScenario)
	api.Delete("/scenarios/:id", h.DeleteScenario)
	api.Patch("/scenarios/:id", h.PatchScenario) // v4: commands, rules (branch.go)
	api.Put("/scenarios/:id/steps", h.PutSteps)
	api.Post("/scenarios/:id/publish", h.PublishScenario)
	api.Post("/scenarios/:id/import", h.ImportSteps)
	api.Get("/scenarios/:id/export.xlsx", h.ExportScenario) // v4: экспорт в формат ТЗ (scenario_xlsx_export.go)
	// v4 — статистика кнопок и последствия публикации (scenario_stats.go).
	api.Get("/scenarios/:id/stats", h.ScenarioStats)
	api.Post("/scenarios/:id/publish/preview", h.PublishPreview)
	api.Post("/scenarios/:id/rewrite-vy", h.RewriteVy) // v4: «вы» и нейтральный род (scenario_rewrite.go)

	// v2 — обращения (api_contract.md, «v2 — итерация 3» и «Уточнения v2»).
	api.Get("/tickets", h.ListTickets)
	api.Get("/tickets/assignees", h.TicketAssignees) // до /tickets/:id
	api.Get("/tickets/:id", h.GetTicket)
	api.Patch("/tickets/:id", h.PatchTicket)
	api.Post("/tickets/:id/assign", h.AssignTicket)
	api.Post("/tickets/:id/reply", h.ReplyTicket)
	api.Post("/tickets/:id/note", h.NoteTicket)
	api.Post("/tickets/:id/resolve", h.ResolveTicket)

	// v3 — обзор (api_contract.md, «v3 — итерация 4»).
	api.Get("/overview", h.Overview)

	// v4 — дайджест и руководитель подразделения (api_contract.md, «v4 — дайджест и руководитель»).
	api.Get("/digest", h.Digest)
	api.Post("/newcomers/:id/manager-invite", h.ManagerInvite)
	api.Get("/manager/managers", h.Managers)
	api.Get("/manager/newcomers", h.ManagerNewcomers)
	api.Post("/manager/tasks/:id", h.ManagerTask) // «Встретился» / «Запланировал» из кабинета (users.go)

	// v3 — база знаний (api_contract.md, «v3 — база знаний»). Права HR — hrOnly: ролей нет,
	// пускаем в development или с X-Operator-Token. Мидлвар на каждом маршруте, а не группой без
	// префикса: та повисла бы на всём /api, и неизвестный путь вне development стал бы 403.
	api.Get("/documents", h.hrOnly, h.ListDocuments)
	api.Post("/documents", h.hrOnly, h.UploadDocument)
	api.Post("/documents/ask", h.hrOnly, h.AskDocuments)
	api.Get("/documents/:id/versions", h.hrOnly, h.DocumentVersions)
	api.Get("/knowledge/unanswered", h.hrOnly, h.Unanswered)

	// v4 — медиатека (api_contract.md, «v4»). Права — как у базы знаний. ensure — до :id.
	api.Get("/media", h.hrOnly, h.ListMedia)
	api.Post("/media", h.hrOnly, h.CreateMedia)
	api.Post("/media/ensure", h.hrOnly, h.EnsureMedia)
	api.Get("/media/:id", h.hrOnly, h.GetMedia)
	api.Put("/media/:id", h.hrOnly, h.PutMediaFile)
	api.Patch("/media/:id", h.hrOnly, h.RenameMedia)
	api.Delete("/media/:id", h.hrOnly, h.DeleteMedia)
	api.Get("/media/:id/content", h.hrOnly, h.MediaContent)
	api.Delete("/media/:id/files/:site", h.hrOnly, h.DeleteMediaFile)

	// dev — только APP_ENV=development, иначе 404 (api_contract.md, раздел «dev»).
	// v4 — «Отправить себе» / «Пройти этот путь» редактора (только development).
	api.Post("/scenarios/:id/steps/:stepId/send-test", devOnly, h.SendStepTest)

	dev := api.Group("/dev", devOnly)
	dev.Post("/simulate-start", h.SimulateStart)
	dev.Post("/simulate-message", h.SimulateMessage)
	dev.Get("/chat/:id", h.DevChat)
	dev.Post("/chat/:id/press", h.DevChatPress)
	dev.Post("/digest/send", h.DevDigestSend)
	dev.Post("/manager/push", h.DevManagerPush)
	dev.Post("/recruiter-morning", h.DevRecruiterMorning) // за сессией hr_lead (policy)
	dev.Post("/manager/start", h.DevManagerStart)
	dev.Post("/manager/press", h.DevManagerPress)
	dev.Get("/manager/chat", h.DevManagerChat)
}
