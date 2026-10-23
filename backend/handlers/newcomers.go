package handlers

import (
	"net/http"

	"main/in"
	"main/out"

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
	list, err := h.services.Newcomers.List(c.UserContext(), f)
	if err != nil {
		return err
	}
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
	return c.JSON(out.NewcomerFrom(*card))
}

// PatchNewcomer — PATCH /api/newcomers/{id} {"start_date"}: перенос даты выхода. Расширение
// контракта v1 (заметка backend в api_contract.md).
func (h *Handler) PatchNewcomer(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.NewcomerPatch
	if err := bindBody(c, &body); err != nil {
		return err
	}
	start, err := body.Validate()
	if err != nil {
		return err
	}
	card, err := h.services.Newcomers.Reschedule(c.UserContext(), id, start)
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
