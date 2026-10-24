package handlers

import (
	"main/domain"
	"main/in"
	"main/out"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// Overview — GET /api/overview?site= (api_contract.md, «v3 — итерация 4»).
func (h *Handler) Overview(c *fiber.Ctx) error {
	var q in.OverviewQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	site, err := q.Validate()
	if err != nil {
		return err
	}
	o, err := h.services.Overview().Get(c.UserContext(), site)
	if err != nil {
		return err
	}
	return c.JSON(out.OverviewFrom(o))
}

// Indicators — GET /api/indicators?site=&period=30d|90d|q («Показатели», api_contract.md).
// Рекрутеру — 403 (policy). Строка «Сегодня у команды» для вошедшего руководителя HR — из его
// «Сегодня»; без входа — из «Требуют действия» обзора. Темы без ответа в базе знаний — группы
// отказов rag за дни периода.
func (h *Handler) Indicators(c *fiber.Ctx) error {
	var q in.IndicatorsQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	site, period, err := q.Validate()
	if err != nil {
		return err
	}
	ctx := c.UserContext()
	ind, err := h.services.Indicators().Get(ctx, site, period)
	if err != nil {
		return err
	}
	if u := Viewer(c); u != nil && u.Role == domain.RoleHRLead && h.services.Users != nil {
		d, err := h.services.Users.HRLeadDashboard(ctx, site, h.services.Digest(), h.services.Overview())
		if err != nil {
			return err
		}
		ind.Today = services.TodayLink{Total: d.TodayTotal, Urgent: d.TodayRed, Source: "hrlead"}
	}
	if h.services.Documents != nil {
		if r, err := h.services.Documents.Unanswered(ctx, services.UnansweredFilter{Site: site, Days: ind.Period.Days}); err == nil {
			n := len(r.Clusters)
			ind.HRLoad.KBGaps, ind.HRLoad.KBQuestions = &n, r.Questions
		}
	}
	return c.JSON(out.IndicatorsFrom(ind))
}
