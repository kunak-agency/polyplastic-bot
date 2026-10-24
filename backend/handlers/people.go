package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"main/out"
	"main/pkg/errs"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Справочник людей (api_contract_auth.md, «Люди»): читают и создают без входа recruiter и
// hr_lead (manager — 403 в policy), создать со входом — только hr_lead (проверка в сервисе).

// ListPeople — GET /api/people?role=manager|mentor|recruiter&q= → {"items","total"}.
func (h *Handler) ListPeople(c *fiber.Ctx) error {
	list, err := h.services.Users.People(c.UserContext(), strings.TrimSpace(c.Query("role")), strings.TrimSpace(c.Query("q")))
	if err != nil {
		return err
	}
	return c.JSON(out.NewList(out.PeopleFrom(list)))
}

type personBody struct {
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

// CreatePerson — POST /api/people {full_name, role: manager|mentor, login?, password?} → 201
// новый или 200 — человек той же роли с тем же ФИО уже был.
func (h *Handler) CreatePerson(c *fiber.Ctx) error {
	var b personBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	u, created, err := h.services.Users.CreatePerson(c.UserContext(), services.PersonInput{FullName: b.FullName,
		Role: strings.TrimSpace(b.Role), Login: b.Login, Password: b.Password})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	return c.Status(status).JSON(out.PersonFrom(*u))
}

// peopleKeys — ключи тела PATCH /api/newcomers/{id}, которые разбирает не NewcomerPatch.
var peopleKeys = map[string]bool{"recruiter_id": true, "manager_id": true, "mentor_id": true}

// newcomerPersonField — manager_id / mentor_id из тела: Set — ключ есть; null или "" — снять.
func newcomerPersonField(c *fiber.Ctx, key string) (services.PersonRef, error) {
	var m map[string]json.RawMessage
	if len(c.Body()) == 0 || json.Unmarshal(c.Body(), &m) != nil {
		return services.PersonRef{}, nil // разбор тела — забота основного DTO
	}
	v, ok := m[key]
	if !ok {
		return services.PersonRef{}, nil
	}
	ref := services.PersonRef{Set: true}
	if string(v) == "null" {
		return ref, nil
	}
	bad := errs.NewValidationError(errs.MsgInvalidFields).
		WithDetails(errs.FieldError{Field: key, Code: errs.CodeInvalidFormat, Message: "Ожидается UUID или null"})
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ref, bad
	}
	if s = strings.TrimSpace(s); s == "" {
		return ref, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return ref, bad
	}
	ref.ID = &id
	return ref, nil
}

// newcomerPeople — manager_id и mentor_id из тела.
func newcomerPeople(c *fiber.Ctx) (mgr, mentor services.PersonRef, err error) {
	if mgr, err = newcomerPersonField(c, "manager_id"); err != nil {
		return
	}
	mentor, err = newcomerPersonField(c, "mentor_id")
	return
}

// onlyPeoplePatch — в теле PATCH только recruiter_id / manager_id / mentor_id (без start_date/tz).
func onlyPeoplePatch(c *fiber.Ctx) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(c.Body(), &m) != nil || len(m) == 0 {
		return false
	}
	for k := range m {
		if !peopleKeys[k] {
			return false
		}
	}
	return true
}
