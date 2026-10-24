package services

import (
	"time"

	"main/domain"
)

// Обязательный вопрос держит таймлайн не дольше 48 ч (п. 27 плана 24.10, FEATURE_NUDGES_DEFAULT;
// ресерч 2026-10-24-sentiment-feedback.md). Вопрос без своего no_answer (наджи 4/24 ч) и опрос
// без ответа 48 ч от отправки:
//   - поток блока идёт дальше: holderOf такой вопрос не считает держателем, планировщик зовёт
//     Release;
//   - вопрос остаётся открытым: статус sent, waiting=answer — его по-прежнему находят наджи,
//     ручное «Напомнить» и общая шапка «У вас N вопросов»; ответ новичка его закрывает как обычно;
//   - в карточке — answer = answerHeldOut («не отвечен: …»), ответ новичка его перезапишет;
//   - новичок получает Employee.UnresponsiveSince (если ещё нет) — строка «Не отвечает боту» и
//     действие «Связаться» в «Сегодня» рекрутера.
// Шаг со своим no_answer живёт по своим срокам (после них — skipped «не отвечен», как раньше).

// questionHoldMax — сколько вопрос без ответа держит поток блока.
const questionHoldMax = 48 * time.Hour

// answerHeldOut — answer открытого вопроса, который больше не держит поток.
const answerHeldOut = "не отвечен: 48 ч без ответа, вопрос остаётся открытым"

// heldOut — вопрос без ответа 48 ч: открыт, но поток блока уже не держит.
func heldOut(ev *domain.ScheduledEvent) bool {
	return ev != nil && ev.Status == domain.EventSent && ev.Answer != nil && *ev.Answer == answerHeldOut
}

// answerBefore — прежний ответ события для дописывания нового нажатия: отметка «не отвечен» 48 ч
// ответом не считается.
func answerBefore(ev *domain.ScheduledEvent) *string {
	if ev == nil || ev.Answer == nil || *ev.Answer == answerHeldOut {
		return nil
	}
	return ev.Answer
}
