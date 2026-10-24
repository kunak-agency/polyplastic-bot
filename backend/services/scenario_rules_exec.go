package services

import (
	"context"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
	"main/repositories"
)

// Исполнение глобальных правил сценария (ТЗ r69, r70; scenario.rules в снимке версии новичка).
//   - free_question (r69): бот не нашёл ответа в документах (или справочная база не ответила) —
//     вместо стандартного отказа текст правила, его картинка и пара кнопок: «передать через
//     Корсика» (вопрос уходит обращением по маршруту кнопки) и «связаться с рекрутером»
//     (обращение-переписка, human_active).
//   - survey_reminder (r70): опрос отправлен и не закончен — напоминание текстом правила через
//     remind_after_h часов от отправки (по умолчанию 24 и 48), только в рабочие часы площадки.

// preQChat — кнопка правила «связаться»: qc:<question>:<route> — обращение сразу в переписку.
const preQChat = "qc:"

// textSurveyReminderTail — к напоминанию: какой опрос ждёт.
const textSurveyReminderTail = "Опрос: «%s» — он выше в чате."

// scenarioRule — правило вида kind из снимка версии новичка (nil — нет).
func scenarioRule(ctx context.Context, r *repositories.Repos, e *domain.Employee, kind string) *domain.ScenarioRule {
	if e == nil || r == nil {
		return nil
	}
	v, err := r.Scenarios.Version(ctx, e.ScenarioID, e.ScenarioVersion)
	if err != nil || v == nil {
		return nil
	}
	for i := range v.Rules {
		if v.Rules[i].Kind == kind {
			return &v.Rules[i]
		}
	}
	return nil
}

// ruleRefusal — отказ RAG по правилу r69 подходит этому виду ответа.
func ruleRefusal(kind string) bool {
	return kind == domain.QuestionAbstain || kind == domain.QuestionError
}

// ruleButtonPayload — кнопка правила → payload вопроса: escalate/open_free_text — передать сам
// вопрос (qr: рекрутеру, qh: HR); handoff — переписка (qc:). Пусто — кнопку не показываем.
func ruleButtonPayload(b domain.StepButton, q *domain.BotQuestion) string {
	acts := withButtonAction(nil, b)
	if b.Branch != nil {
		acts = withButtonAction(b.Branch.Actions, b)
	}
	id := q.ID.String()
	for _, a := range acts {
		switch a.Type {
		case domain.ActionHandoff:
			return preQChat + id + ":" + routeOr(a.To, "recruiter")
		case domain.ActionEscalate:
			if a.Route == "hr" {
				return preQHR + id
			}
			return preQRecruiter + id
		}
	}
	if _, ok := hasAction(acts, domain.ActionOpenFreeText); ok || b.OpensFreeText {
		return preQRecruiter + id
	}
	return ""
}

// ruleRefusalMessage — отказ по правилу r69: текст правила, кнопки правила и «Переформулирую /
// Не нужно».
func ruleRefusalMessage(to recipient, e *domain.Employee, q *domain.BotQuestion, rule *domain.ScenarioRule) max.OutMessage {
	var btns []max.Button
	for _, b := range rule.Buttons {
		if strings.TrimSpace(b.Label) == "" {
			continue
		}
		if p := ruleButtonPayload(b, q); p != "" {
			btns = append(btns, max.Button{Text: b.Label, Payload: p})
		}
	}
	rows := packRows(btns)
	rows = append(rows, []max.Button{{Text: btnRephrase, Payload: qPayload(preQRephrase, q.ID)}, {Text: btnNoNeed, Payload: qPayload(preQNoNeed, q.ID)}})
	return to.msg(byForm(e, rule.TextTy, rule.TextVy), "", rows)
}

// remindSurveys — правило r70: опросы, отправленные и не законченные, получают напоминание
// через remind_after_h[k] часов от отправки (в рабочие часы). Счётчик — ScheduledEvent.Reminded.
func (s *SchedulerService) remindSurveys(ctx context.Context) (int, error) {
	if s.max == nil || !s.repos.Enabled() {
		return 0, nil
	}
	now := s.now()
	n := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		evs, err := tx.Events.OpenSurveys(ctx, now.Add(-time.Hour), 200)
		if err != nil {
			return err
		}
		for i := range evs {
			ev, err := tx.Events.ByIDForUpdate(ctx, evs[i].ID)
			if err != nil {
				return err
			}
			if ev == nil || ev.Status != domain.EventSent || ev.Kind != domain.KindSurvey || ev.SentAt == nil || ev.Snapshot != nil {
				continue
			}
			e, err := tx.Employees.ByID(ctx, ev.EmployeeID)
			if err != nil || e == nil {
				return err
			}
			if e.Connection != domain.ConnConnected || !reachable(e) {
				continue
			}
			rule := scenarioRule(ctx, tx, e, domain.RuleSurveyReminder)
			if rule == nil {
				continue
			}
			hours := rule.RemindAfterH
			if len(hours) == 0 {
				hours = []int{24, 48}
			}
			due := 0 // сколько сроков напоминания уже прошло
			for _, h := range hours {
				if !now.Before(ev.SentAt.Add(time.Duration(h) * time.Hour)) {
					due++
				}
			}
			if ev.Reminded >= due || !InWorkHours(now, s.SiteFor(e)) {
				continue
			}
			text := strings.TrimSpace(stepText(domain.ScenarioStep{TextTy: rule.TextTy, TextVy: rule.TextVy}, e, now))
			if ev.Title != "" {
				text += "\n\n" + strings.Replace(textSurveyReminderTail, "%s", ev.Title, 1)
			}
			if ev.IsTest {
				text = testPrefix + text
			}
			msg := recipientOf(e).msg(text, "", [][]max.Button{humanRow(&ev.ID)})
			atts, _ := s.attachFor(ctx, e, rule.Media, ev.Title)
			if groups := GroupAttachments(atts); len(groups) > 0 {
				msg.Attachments = groups[len(groups)-1]
			}
			if _, err := s.max.Send(ctx, msg); err != nil {
				logger.Warn("планировщик: напоминание об опросе %s не ушло: %v", ev.ID, err)
				continue
			}
			// сроки, пропущенные за выходные или ночь, сливаются в одно напоминание
			ev.Reminded = due
			logger.Info("планировщик: напоминание %d об опросе %s (правило r70)", ev.Reminded, ev.ID)
			if err := tx.Events.Save(ctx, ev); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
