package handlers

import (
	"main/domain"
	"main/out"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Наджи — напоминания новичку о вопросе без ответа (api_contract.md, «Наджи»).

// viewerID — кто нажал «Напомнить»: пользователь входа (c.Locals("viewer"), FEATURE_AUTH);
// без входа — uuid.Nil (created_by = null).
func viewerID(c *fiber.Ctx) uuid.UUID {
	if u, ok := c.Locals("viewer").(*domain.User); ok && u != nil {
		return u.ID
	}
	return uuid.Nil
}

// NudgeNewcomer — POST /api/newcomers/{id}/nudge: текущий незакрытый вопрос ещё раз. В тихие
// часы новичка — 202 и время отправки по его поясу.
func (h *Handler) NudgeNewcomer(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	res, err := h.services.Newcomers.Nudge(c.UserContext(), id, viewerID(c))
	if err != nil {
		return err
	}
	body := out.NudgeResultFrom(*res)
	if body.Status == "planned" {
		return c.Status(fiber.StatusAccepted).JSON(body)
	}
	return c.JSON(body)
}

// NewcomerNudges — GET /api/newcomers/{id}/nudges: история напоминаний.
func (h *Handler) NewcomerNudges(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	items, tz, err := h.services.Newcomers.Nudges(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.NudgesFrom(items, tz))
}
