package handlers

import (
	"main/in"
	"main/out"

	"github.com/gofiber/fiber/v2"
)

// RewriteVy — POST /api/scenarios/{id}/rewrite-vy: тексты «вы» черновика из текстов «ты»
// (LLM с проверками, правило-фолбэк) и, по желанию, нейтральный род.
func (h *Handler) RewriteVy(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.RewriteVy
	if err := bindBody(c, &body); err != nil {
		return err
	}
	o, err := body.Validate()
	if err != nil {
		return err
	}
	rep, err := h.services.RewriteVyScenario(c.UserContext(), id, o)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioRewriteFrom(*rep))
}
