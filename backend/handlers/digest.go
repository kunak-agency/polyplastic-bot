package handlers

import (
	"main/in"
	"main/out"

	"github.com/gofiber/fiber/v2"
)

// Digest — GET /api/digest?week=&site= (api_contract.md, «v4 — дайджест»).
func (h *Handler) Digest(c *fiber.Ctx) error {
	var q in.DigestQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	week, site, err := q.Validate()
	if err != nil {
		return err
	}
	d, err := h.services.Digest().Get(c.UserContext(), week, site)
	if err != nil {
		return err
	}
	return c.JSON(out.DigestFrom(d))
}

// DevDigestSend — POST /api/dev/digest/send: отправить дайджест сейчас (только development).
func (h *Handler) DevDigestSend(c *fiber.Ctx) error {
	var body in.DigestSend
	if err := bindBody(c, &body); err != nil {
		return err
	}
	r, err := h.services.Digest().Send(c.UserContext(), body.Week)
	if err != nil {
		return err
	}
	return c.JSON(out.DigestSent{Week: r.Week, SentTo: r.SentTo, Logged: r.Logged, Text: r.Text})
}
