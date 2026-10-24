package services

import (
	"context"
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/repositories"

	"github.com/google/uuid"
)

// «Пропустить» в надже на шаг-вопрос (план итерации 24.10 §3.4). У необязательного шага-вопроса
// без своей кнопки skip надж добавляет ряд «Пропустить» над «Спросить человека»: вопрос — skipped
// «пропущено новичком», поток блока идёт дальше. Обязательный вопрос кнопку не получает (и
// нажатие старой кнопки на нём — «Этот вопрос обязательный»). У опроса «Пропустить» уже есть на
// экране вопроса (surveyMessage), у правила r70 — свой текст без кнопок шага.

const preNudgeSkip = "nk:" // nk:<event>

const (
	answerNudgeSkipped = "пропущено новичком (из напоминания)"
	labelNudgeSkipped  = "Пропущено"
)

// nudgeSkippable — надж на этот шаг получает «Пропустить».
func nudgeSkippable(st domain.ScenarioStep) bool {
	if isSurvey(st) || !isQuestion(st) || st.Required {
		return false
	}
	for _, b := range st.Buttons {
		if b.Action == "skip" {
			return false
		}
	}
	return true
}

// withNudgeSkip — ряд «Пропустить» перед последним рядом («Спросить человека»).
func withNudgeSkip(m max.OutMessage, eventID uuid.UUID) max.OutMessage {
	row := []max.Button{{Text: btnSkip, Payload: preNudgeSkip + eventID.String()}}
	if n := len(m.Buttons); n > 0 {
		rows := append([][]max.Button{}, m.Buttons[:n-1]...)
		m.Buttons = append(append(rows, row), m.Buttons[n-1])
		return m
	}
	m.Buttons = [][]max.Button{row}
	return m
}

// nudgeSkip — «Пропустить» из наджа: вопрос пропущен, сообщение наджа правится в итог без
// клавиатуры, дальше — следующие реплики блока.
func (b *BotService) nudgeSkip(ctx context.Context, u max.Update, empID uuid.UUID, rest string) error {
	evID, err := uuid.Parse(strings.TrimSpace(rest))
	if err != nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	var reply max.Answer
	release := false
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		if ev == nil {
			reply = max.Answer{Notification: textButtonStale}
			return nil
		}
		if ev.Status != domain.EventSent || ev.Waiting != domain.WaitAnswer {
			reply = max.Answer{Notification: "Уже записал"}
			return nil
		}
		if step.Required {
			reply = max.Answer{Notification: "Этот вопрос обязательный"}
			return nil
		}
		now := b.now()
		ev.Status, ev.Answer, ev.Waiting = domain.EventSkipped, strPtr(answerNudgeSkipped), ""
		e.LastActivityAt = &now
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		done := b.recipient(e).msg(stepAnsweredTextIn(step, e, labelNudgeSkipped, ev.IsTest, sentAt(ev, now), b.siteFor(e)), "", nil)
		reply = max.Answer{Notification: "Пропустили", Message: &done}
		release = true
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, reply)
	if release {
		return b.scheduler.Release(ctx, empID, evID)
	}
	return nil
}
