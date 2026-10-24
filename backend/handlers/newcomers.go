package handlers

import (
	"net/http"

	"main/in"
	"main/out"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// ListNewcomers — GET /api/newcomers?site=&q=&risk=.
func (h *Handler) ListNewcomers(c *fiber.Ctx) error {
	var q in.NewcomerQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	f, err := q.Validate()
	if err != nil {
		return err
	}
	if err := h.scopeNewcomers(c, &f); err != nil { // FEATURE_AUTH: «только свои» (users.go)
		return err
	}
	list, err := h.services.Newcomers.List(c.UserContext(), f)
	if err != nil {
		return err
	}
	h.fillRecruiterNames(c, list)
	return c.JSON(out.NewList(out.NewcomersFrom(list)))
}

// CreateNewcomer — POST /api/newcomers: «Запустить адаптацию» → 201 + сотрудник с invite_link.
func (h *Handler) CreateNewcomer(c *fiber.Ctx) error {
	var body in.NewcomerCreate
	if err := bindBody(c, &body); err != nil {
		return err
	}
	input, err := body.Validate()
	if err != nil {
		return err
	}
	if input.RecruiterID, err = h.createRecruiter(c); err != nil { // recruiter_id (users.go)
		return err
	}
	// manager_id / mentor_id (people.go); без них — по manager_name / mentor_name.
	mgr, mentor, err := newcomerPeople(c)
	if err != nil {
		return err
	}
	input.ManagerID, input.MentorID = mgr.ID, mentor.ID
	card, err := h.services.Newcomers.Create(c.UserContext(), input)
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.NewcomerFrom(*card))
}

// GetNewcomer — GET /api/newcomers/{id}.
func (h *Handler) GetNewcomer(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	cards := []services.NewcomerCard{*card}
	h.fillRecruiterNames(c, cards)
	return c.JSON(out.NewcomerFrom(cards[0]))
}

// PatchNewcomer — PATCH /api/newcomers/{id} {"start_date"?, "tz"?}: перенос даты выхода и/или
// пояс новичка. Расширение контракта v1 (api_contract.md, заметка backend и «Пояса»).
func (h *Handler) PatchNewcomer(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	// manager_id / mentor_id — люди из справочника (people.go): формат проверяем до изменений.
	mgr, mentor, err := newcomerPeople(c)
	if err != nil {
		return err
	}
	// recruiter_id — закрепление рекрутера (users.go).
	if done, err := h.patchRecruiter(c, id); done || err != nil {
		return err
	}
	if err := h.services.Newcomers.SetPeople(c.UserContext(), id, mgr, mentor); err != nil {
		return err
	}
	if onlyPeoplePatch(c) { // в теле только recruiter_id / manager_id / mentor_id
		card, err := h.services.Newcomers.Get(c.UserContext(), id)
		if err != nil {
			return err
		}
		cards := []services.NewcomerCard{*card}
		h.fillRecruiterNames(c, cards)
		return c.JSON(out.NewcomerFrom(cards[0]))
	}
	var body in.NewcomerPatch
	if err := bindBody(c, &body); err != nil {
		return err
	}
	start, tz, err := body.ValidateChange()
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.Patch(c.UserContext(), id, start, tz)
	if err != nil {
		return err
	}
	return c.JSON(out.NewcomerFrom(*card))
}

// Timeline — GET /api/newcomers/{id}/timeline.
func (h *Handler) Timeline(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	events, err := h.services.Newcomers.Timeline(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.TimelineFrom(events, card.Employee.Site))
}

// TimeTravel — POST /api/newcomers/{id}/time-travel {"day"} → таймлайн.
func (h *Handler) TimeTravel(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TimeTravel
	if err := bindBody(c, &body); err != nil {
		return err
	}
	day, err := body.Validate()
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	events, err := h.services.Newcomers.TimeTravel(c.UserContext(), id, day)
	if err != nil {
		return err
	}
	return c.JSON(out.TimelineFrom(events, card.Employee.Site))
}

// SetIPDecision — POST /api/newcomers/{id}/ip-decision {"decision"} → карточка новичка.
func (h *Handler) SetIPDecision(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.IPDecision
	if err := bindBody(c, &body); err != nil {
		return err
	}
	decision, err := body.Validate()
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.SetIPDecision(c.UserContext(), id, decision)
	if err != nil {
		return err
	}
	return c.JSON(out.NewcomerFrom(*card))
}
