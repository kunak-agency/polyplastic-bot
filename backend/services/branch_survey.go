package services

import (
	"context"
	"strconv"
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// Повопросные опросы (api_contract.md v4, survey.questions[].type): scale|yesno|choice|multi|
// text, свой порог, comment below|on_no, other — «свой вариант». Вопрос без type живёт по шкале
// и порогу опроса целиком, как раньше.

const (
	qLegacy = ""
	qScaleT = "scale"
	qYesNo  = "yesno"
	qChoice = "choice"
	qMulti  = "multi"
	qText   = "text"
)

// preDone — «Готово» у вопроса с несколькими ответами: sd:<event>:<q>.
const preDone = "sd:"

// Экраны живого опроса сверх domain.Screen*: comment — «Добавить комментарий / Дальше»
// (comment: optional), frozen — итоги без вопроса (перед сообщениями ветки варианта).
const (
	screenComment = "comment"
	screenFrozen  = "frozen"
)

const (
	btnAddComment    = "Добавить комментарий"
	textCommentOffer = "Хотите добавить комментарий?"
	btnDone          = "Готово"
	btnOther         = "Другое — свой ответ"
	textOwnAnswer    = "Напишите свой вариант одним сообщением."
	textTextAsk      = "Напишите ответ одним сообщением."
	textComment      = "Напишите, пожалуйста, одним сообщением, что не так."
)

func qType(q domain.SurveyQuestion) string { return strings.ToLower(strings.TrimSpace(q.Type)) }

// qScale — шкала вопроса: своя у типизированного, общая — у старого.
func qScale(sv *domain.Survey, q domain.SurveyQuestion) string {
	switch qType(q) {
	case qLegacy:
		return sv.Scale
	case qScaleT:
		if q.Scale == domain.Scale5 || q.Scale == domain.Scale10 {
			return q.Scale
		}
		if sv.Scale == domain.Scale5 || sv.Scale == domain.Scale10 {
			return sv.Scale
		}
		return domain.Scale10
	case qYesNo:
		return domain.ScaleYesNo
	case qChoice:
		return domain.ScaleChoice
	case qMulti:
		return qMulti
	case qText:
		return qText
	}
	return sv.Scale
}

// qThreshold — порог вопроса: свой или общий опроса.
func qThreshold(sv *domain.Survey, q domain.SurveyQuestion) int {
	if q.Threshold != nil {
		return *q.Threshold
	}
	return sv.Threshold
}

// lowValue — оценка ниже порога (для риска и «ниже порога → причина»).
func lowValue(sv *domain.Survey, q domain.SurveyQuestion, v int) bool {
	thr := qThreshold(sv, q)
	return thr > 0 && scaleMax(qScale(sv, q)) > 0 && v < thr
}

// commentScreen — что спросить после ответа: причины (старый опрос ниже порога), комментарий
// своими словами (comment below|on_no) или ничего.
func commentScreen(sv *domain.Survey, q domain.SurveyQuestion, v int) string {
	if qType(q) == qLegacy {
		if lowValue(sv, q, v) {
			return domain.ScreenReasons
		}
		return ""
	}
	switch q.Comment {
	case "optional":
		return screenComment
	case "below":
		if lowValue(sv, q, v) {
			return domain.ScreenWrite
		}
	case "on_no":
		if qScale(sv, q) == domain.ScaleYesNo && v == 0 {
			return domain.ScreenWrite
		}
	}
	return ""
}

// multiLabels — отмеченные варианты подписями.
func multiLabels(q domain.SurveyQuestion, picks []int) []string {
	var out []string
	for _, v := range picks {
		if v >= 1 && v <= len(q.Options) {
			out = append(out, q.Options[v-1])
		}
	}
	return out
}

func toggle(picks []int, v int) []int {
	for i, x := range picks {
		if x == v {
			return append(picks[:i:i], picks[i+1:]...)
		}
	}
	return append(picks, v)
}

func hasPick(picks []int, v int) bool {
	for _, x := range picks {
		if x == v {
			return true
		}
	}
	return false
}

// multiRows — переключатели «✓ …» по два в ряд и «Готово».
func multiRows(q domain.SurveyQuestion, picks []int, id, qn string) [][]max.Button {
	var btns []max.Button
	for i, o := range q.Options {
		label := o
		if hasPick(picks, i+1) {
			label = "✓ " + o
		}
		btns = append(btns, max.Button{Text: label, Payload: preAnswer + id + ":" + qn + ":" + strconv.Itoa(i+1)})
	}
	return append(packRows(btns), []max.Button{{Text: btnDone, Payload: preDone + id + ":" + qn}})
}

// evalOutcomes — правила после опроса, которые сработали (индексы). В группе — первое
// подходящее («ЛИБО»), разные группы и правила без группы — независимо («+»).
func evalOutcomes(sv *domain.Survey, st *domain.SurveyState) []int {
	var out []int
	taken := map[string]bool{}
	for k, o := range sv.Outcomes {
		if o.Group != "" && taken[o.Group] {
			continue
		}
		if !outcomeMatch(o, st) {
			continue
		}
		if o.Group != "" {
			taken[o.Group] = true
		}
		out = append(out, k)
	}
	return out
}

func outcomeMatch(o domain.Outcome, st *domain.SurveyState) bool {
	if len(o.If) == 0 {
		return true // безусловное правило
	}
	any := strings.EqualFold(o.Match, "any")
	for _, c := range o.If {
		ok := condMatch(c, st)
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	return !any
}

func condMatch(c domain.OutcomeCond, st *domain.SurveyState) bool {
	i := c.Q - 1
	if i < 0 || i >= len(st.Results) {
		return false
	}
	r := st.Results[i]
	if len(c.In) > 0 && r.Value != nil && containsInt(c.In, *r.Value) {
		return true
	}
	for _, v := range c.Contains {
		if hasPick(r.Multi, v) {
			return true
		}
	}
	return false
}

// severityOf — severity варианта в шкалу таксономии 1–3.
func severityOf(s string) int {
	switch strings.ToLower(s) {
	case "low", "1":
		return 1
	case "high", "3":
		return 3
	}
	return 2
}

// afterSurvey — после правки живого сообщения: сигналы вариантов, сообщения ветки варианта и
// следующий вопрос новым сообщением, правила после опроса, затем поток блока.
func (b *BotService) afterSurvey(ctx context.Context, empID uuid.UUID, out surveyOutcome) {
	if out.emp == nil || out.ev == nil {
		return
	}
	if len(out.optFlags) > 0 && b.tickets != nil {
		var in []rag.Flag
		for _, f := range out.optFlags {
			in = append(in, rag.Flag{Code: f.Code, Severity: severityOf(f.Severity), Confidence: 1})
		}
		evID := out.ev.ID
		fi := flagInput{eventID: &evID, questionNo: out.flagQ, via: flagViaOption,
			where: "в ответе «" + out.optLabel + "» на опрос «" + out.step.Title + "»"}
		if ds := decideFlags(b.tickets.tax, in, nil); len(ds) > 0 {
			if err := b.applyFlagDecisions(ctx, empID, fi, ds); err != nil {
				logger.Warn("бот: сигнал варианта новичка %s не обработан: %v", empID, err)
			}
		}
	}
	if out.branch != nil {
		p := &pressPlan{emp: out.emp, ev: *out.ev, step: out.step, src: out.branchSrc, branch: out.branch,
			messages: true, acts: out.branch.Actions, flagged: out.flagged, freeText: out.freeText}
		b.runPlan(ctx, p)
		switch {
		case out.next != nil && out.freeText:
			// «ответь в чате или нажми кнопку» (ТЗ r63): следующий вопрос — после текста или кнопки
			// (resumeSurvey в freeText и terminalButton), иначе причина ушла бы ответом на него
		case out.next != nil:
			n := out
			n.msg, n.stale = out.next, true
			b.resend(ctx, n)
		}
	}
	for _, k := range out.outcomes {
		o := out.step.Survey.Outcomes[k]
		br := &domain.Branch{Messages: o.Messages, Actions: o.Actions}
		var flagged, free bool
		err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
			e, err := tx.Employees.ByIDForUpdate(ctx, empID)
			if err != nil || e == nil {
				return err
			}
			flagged, free = applySilent(e, o.Actions, out.ev.ID, "итог опроса", nil)
			return tx.Employees.Save(ctx, e)
		})
		if err != nil {
			logger.Warn("бот: правило после опроса не применено: %v", err)
			continue
		}
		b.runPlan(ctx, &pressPlan{emp: out.emp, ev: *out.ev, step: out.step, src: branchSrcOutcome(k), branch: br,
			messages: true, acts: o.Actions, flagged: flagged, freeText: free, label: "итог опроса"})
	}
	if out.release {
		if err := b.scheduler.Release(ctx, empID, out.eventID); err != nil {
			logger.Warn("бот: поток после опроса %s не продолжен: %v", out.eventID, err)
		}
	}
}
