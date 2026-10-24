package services

import (
	"context"
	"slices"

	"main/domain"
	"main/pkg/errs"

	"github.com/google/uuid"
)

// Решение по испытательному сроку (api_contract.md, «Решение по ИС»): keep | extend | part | null.
// Ставит рекрутер или руководитель HR в карточке (POST /api/newcomers/{id}/ip-decision) или
// руководитель подразделения кнопкой под push D+87 (manager_bot.go).

// SetIPDecision — записать решение (nil — сбросить в «не задано») и вернуть карточку.
func (s *NewcomerService) SetIPDecision(ctx context.Context, id uuid.UUID, decision *string) (*NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if decision != nil && !slices.Contains(domain.IPDecisions, *decision) {
		return nil, errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "decision", Code: errs.CodeOutOfRange, Message: "Решение: keep, extend, part или null"})
	}
	e, err := s.repos.Employees.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	by := "кабинет"
	if u := ViewerFrom(ctx); u != nil {
		by = u.FullName
		if by == "" {
			by = u.LoginOr("")
		}
	}
	if err := s.repos.Employees.SetIPDecision(ctx, id, decision, s.now(), by); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// ipDecisionRu — решение словами для сообщений.
func ipDecisionRu(d string) string {
	switch d {
	case domain.IPKeep:
		return "оставить"
	case domain.IPExtend:
		return "продлить испытательный срок"
	case domain.IPPart:
		return "расстаться"
	}
	return d
}
