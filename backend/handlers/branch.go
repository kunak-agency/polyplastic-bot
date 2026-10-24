package handlers

import (
	"io"
	"strings"

	"main/domain"
	"main/out"
	"main/pkg/errs"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// v4 — исполнение веток: флаги новичка, личные файлы, «Отправить себе» (api_contract.md v4).

type newcomerFlagIn struct {
	Flag  string `json:"flag"`
	Value *bool  `json:"value"`
}

// SetNewcomerFlag — POST /api/newcomers/{id}/flags {flag, value}: «по отмашке рекрутера».
func (h *Handler) SetNewcomerFlag(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body newcomerFlagIn
	if err := bindBody(c, &body); err != nil {
		return err
	}
	v := true
	if body.Value != nil {
		v = *body.Value
	}
	flags, err := h.services.SetNewcomerFlag(c.UserContext(), id, body.Flag, v)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"flags": flags})
}

// UploadNewcomerFile — POST /api/newcomers/{id}/files (multipart file, name=adaptation_plan).
func (h *Handler) UploadNewcomerFile(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	fh, err := c.FormFile("file")
	if err != nil {
		return errs.NewValidationError("Нужен файл в поле file")
	}
	r, err := fh.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	mime := fh.Header.Get("Content-Type")
	f, err := h.services.UploadNewcomerFile(c.UserContext(), id, c.FormValue("name"), fh.Filename, mime, data)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"name": f.Name, "filename": f.Filename, "mime": f.Mime, "size": f.Size, "updated_at": f.UpdatedAt,
	})
}

type sendTestIn struct {
	NewcomerID  string `json:"newcomer_id"`
	PressButton *int   `json:"press_button"`
}

// SendStepTest — POST /api/scenarios/{id}/steps/{stepId}/send-test (только development).
func (h *Handler) SendStepTest(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	stepID, err := uuid.Parse(c.Params("stepId"))
	if err != nil {
		return errs.NewValidationError(errs.MsgInvalidID)
	}
	var body sendTestIn
	if err := bindBody(c, &body); err != nil {
		return err
	}
	nid, err := uuid.Parse(strings.TrimSpace(body.NewcomerID))
	if err != nil {
		return errs.NewValidationError("newcomer_id: нужен UUID сотрудника")
	}
	res, err := h.services.SendStepTest(c.UserContext(), id, stepID, nid, body.PressButton)
	if err != nil {
		return err
	}
	ev := res.Event
	return c.JSON(fiber.Map{
		"event_id": ev.ID, "status": ev.Status, "answer": ev.Answer, "waiting": ev.Waiting,
		"scenario_version": ev.ScenarioVersion, "messages": res.Messages,
	})
}

type scenarioPatchIn struct {
	Commands *domain.CommandList `json:"commands"`
	Rules    *domain.RuleList    `json:"rules"`
}

// PatchScenario — PATCH /api/scenarios/{id} {commands?, rules?}: команды бота и глобальные
// правила черновика; в бот попадают с публикацией (снимок версии).
func (h *Handler) PatchScenario(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body scenarioPatchIn
	if err := bindBody(c, &body); err != nil {
		return err
	}
	d, err := h.services.Scenarios.Patch(c.UserContext(), id, body.Commands, body.Rules)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioDetailFrom(*d))
}
