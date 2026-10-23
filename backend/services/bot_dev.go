package services

import (
	"context"
	"fmt"

	"main/domain"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// SimulateFlag — только для dev (POST /api/dev/simulate-message с полем flag): играет разметку
// /flags, которую бот пока не вызывает. Текст новичка считается размеченным кодом F0–F10:
// риск в карточку, HumanRequest и обращение этой категории, как у regex-флага, но без выбора
// новичка. Нужен, чтобы показать категории без regex (F4, F10…) без живой модели.
func (b *BotService) SimulateFlag(ctx context.Context, empID uuid.UUID, code, text string) error {
	cat, ok := b.tickets.tax.Get(code)
	if !ok {
		return errs.NewValidationError("Неизвестный код флага").
			WithDetails(errs.FieldError{Field: "flag", Code: errs.CodeOutOfRange, Message: "Код флага: F0–F10"})
	}
	var (
		res handoffResult
		emp *domain.Employee
	)
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, empID)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError("Новичок не найден")
		}
		now := b.now()
		e.LastActivityAt = &now
		markRisk(e, domain.RiskRed, "сигнал "+code+" в сообщении новичка")
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		h := domain.HumanRequest{EmployeeID: e.ID, Source: domain.HumanFromFreeText, Flags: domain.StringList{code}, Target: domain.TargetHR}
		if cat.PartnerOnly {
			h.Target = domain.TargetHRPartner
		}
		if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
			return err
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: cat, source: domain.TicketFromFlag, quote: text, humanRequestID: &h.ID,
			what: fmt.Sprintf("Сработал сигнал %s в свободном тексте (разметка /flags), передано сразу.", code),
		})
		if err != nil {
			return err
		}
		res = handoffResult{ticket: t, created: created, lead: "Передаю " + ticketTo(cat) + "."}
		emp = e
		return nil
	})
	if err != nil {
		return err
	}
	logger.Info("бот (dev): сигнал %s от новичка %s → обращение #%d", code, empID, res.ticket.Number)
	b.afterHandoff(ctx, emp, &res)
	return nil
}
