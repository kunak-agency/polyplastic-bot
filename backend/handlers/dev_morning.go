package handlers

import (
	"main/out"

	"github.com/gofiber/fiber/v2"
)

// DevRecruiterMorning — POST /api/dev/recruiter-morning: утренняя сводка рекрутерам сейчас, без
// окна 9:00–12:00 (демо push). Только development; при входе — только hr_lead (policy).
func (h *Handler) DevRecruiterMorning(c *fiber.Ctx) error {
	list, err := h.services.RecruiterMorning().Tick(c.UserContext(), true)
	if err != nil {
		return err
	}
	items := []out.RecruiterMorningSent{}
	for _, m := range list {
		items = append(items, out.RecruiterMorningSent{UserID: m.UserID, Text: m.Text})
	}
	return c.JSON(fiber.Map{"items": items, "total": len(items)})
}
