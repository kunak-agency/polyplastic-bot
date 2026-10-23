package handlers

import (
	"main/in"
	"main/out"

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
