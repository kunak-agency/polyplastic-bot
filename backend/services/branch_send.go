package services

import (
	"context"
	"fmt"
	"strconv"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// «Отправить себе» и «Пройти этот путь» для редактора (только development):
// POST /api/scenarios/{id}/steps/{stepId}/send-test {newcomer_id, press_button?}. Шаг черновика
// уходит тестовому новичку с пометкой [ТЕСТ]; событие хранит снимок шага (StepSnap) и
// отрицательный номер версии — нажатия резолвятся по снимку, реальный таймлайн не трогается.

// SendTestResult — событие и сколько сообщений ушло в MAX.
type SendTestResult struct {
	Event    domain.ScheduledEvent
	Messages int
}

// SendStepTest — отправить шаг и, если press != nil, сразу нажать кнопку (вариант опроса).
func (s *Services) SendStepTest(ctx context.Context, scenarioID, stepID, newcomerID uuid.UUID, press *int) (*SendTestResult, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	if s.Bot == nil || s.Scheduler.max == nil {
		return nil, errs.NewConflictError("Бот выключен: нет ни MAX_BOT_TOKEN, ни подделки MAX")
	}
	steps, err := s.repos.Scenarios.Steps(ctx, scenarioID)
	if err != nil {
		return nil, err
	}
	var step *domain.ScenarioStep
	for i := range steps {
		if steps[i].ID == stepID {
			step = &steps[i]
		}
	}
	if step == nil {
		return nil, errs.NewNotFoundError("Шаг не найден в черновике сценария")
	}
	e, err := s.repos.Employees.ByID(ctx, newcomerID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError("Новичок не найден")
	}
	if e.Connection != domain.ConnConnected || (e.MaxUserID == nil && e.ChatID == nil) {
		return nil, errs.NewConflictError("Новичок не подключён к боту — отправлять некуда")
	}
	before := 0
	if f, ok := s.Scheduler.max.(*max.Fake); ok {
		before = len(f.Sent())
	}
	snap := domain.StepSnap(*step)
	var ev domain.ScheduledEvent
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		minV, err := tx.Events.MinVersion(ctx, e.ID, step.ID)
		if err != nil {
			return err
		}
		if minV > 0 {
			minV = 0
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		ev = domain.ScheduledEvent{
			ID: id, EmployeeID: e.ID, StepID: step.ID, ScenarioVersion: minV - 1, Position: step.Position,
			DayOffset: step.DayOffset, Title: step.Title, Kind: step.Kind, FireAt: s.Scheduler.now(),
			Status: domain.EventScheduled, IsTest: true, Snapshot: &snap, Block: "test:" + id.String(),
		}
		if err := tx.Events.Create(ctx, &ev); err != nil {
			return err
		}
		if _, err := s.Scheduler.sendEvent(ctx, tx, e, nil, &ev, true, false); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if press != nil && ev.Status == domain.EventSent {
		if err := s.Bot.Press(ctx, e, ev, *step, *press); err != nil {
			return nil, err
		}
	}
	out := &SendTestResult{Event: ev}
	if cur, err := s.repos.Events.ByIDForUpdate(ctx, ev.ID); err == nil && cur != nil {
		out.Event = *cur
	}
	if f, ok := s.Scheduler.max.(*max.Fake); ok {
		out.Messages = len(f.Sent()) - before
	}
	return out, nil
}

// Press — эмуляция нажатия кнопки i шага (опрос — варианта i первого вопроса) без callback:
// сообщение правится напрямую.
func (b *BotService) Press(ctx context.Context, e *domain.Employee, ev domain.ScheduledEvent, step domain.ScenarioStep, i int) error {
	u := max.Update{Type: max.UpdateMessageCallback, ChatID: derefID(e.ChatID), UserID: derefID(e.MaxUserID)}
	if isSurvey(step) {
		q := step.Survey.Questions[0]
		v := i + 1
		if qScale(step.Survey, q) == domain.ScaleYesNo {
			v = 1 - min(i, 1)
		}
		u.Payload = preAnswer + ev.ID.String() + ":0:" + strconv.Itoa(v)
		out, err := b.applySurvey(ctx, e.ID, ev.ID, surveyAction{kind: "answer", q: 0, value: v})
		if err != nil {
			return err
		}
		b.deliverEdit(ctx, out)
		b.afterSurvey(ctx, e.ID, out)
		return nil
	}
	if i < 0 || i >= len(step.Buttons) {
		return errs.NewValidationError(fmt.Sprintf("press_button: у шага %d кнопок", len(step.Buttons)))
	}
	return b.stepButton(ctx, u, e.ID, ev.ID.String()+":"+strconv.Itoa(i))
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
