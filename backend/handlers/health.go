package handlers

import (
	"main/out"

	"github.com/gofiber/fiber/v2"
)

// Health — GET /api/health. Всегда 200: процесс жив, раз ответил; лежащая база или rag видны
// в полях db и rag, а не статусом — иначе балансировщик снимал бы API из-за упавшего rag.
func (h *Handler) Health(c *fiber.Ctx) error {
	r := h.services.Health.Check(c.UserContext())
	return c.JSON(out.Health{Status: "ok", DB: out.OkDown(r.DB), RAG: out.OkDown(r.RAG)})
}
