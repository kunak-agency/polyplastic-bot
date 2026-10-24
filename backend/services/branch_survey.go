package services

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/logger"
	"main/pkg/taxonomy"
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

// surveyFor — шаг, у которого вопросы и подписи вариантов опроса в форме обращения новичка: на
// «вы» (и пока форма не выбрана, как stepText) — text_vy и options_vy, где они заполнены.
// Значения ответов от формы не зависят. Повторный вызов ничего не меняет.
func surveyFor(s domain.ScenarioStep, e *domain.Employee) domain.ScenarioStep {
	if s.Survey == nil || e == nil || (e.AddressForm != nil && *e.AddressForm == domain.AddressTy) {
		return s
	}
	sv := *s.Survey
	sv.Questions = slices.Clone(sv.Questions)
	for i := range sv.Questions {
		q := &sv.Questions[i]
		if strings.TrimSpace(q.TextVy) != "" {
			q.Text = q.TextVy
		}
		if len(q.Options) > 0 && len(q.OptionsVy) == len(q.Options) {
			opts := slices.Clone(q.Options)
			for j, o := range q.OptionsVy {
				if strings.TrimSpace(o) != "" {
					opts[j] = o
				}
			}
			q.Options = opts
		}
	}
	s.Survey = &sv
	return s
}

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
		b.optionSignals(ctx, empID, out)
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

// optionSignals — сигналы вариантов ответа (option_flags) по правилам таксономии и ветке варианта:
//   - конфиденциальные и экстренные коды (F0, F5, F9) — сразу по своему маршруту, как regex;
//   - у варианта ветка с эскалацией (escalate, handoff, open_free_text) — только сигнал риска
//     (карточка, зона риска, дайджест): обращение заведёт нажатие или текст новичка, с категорией
//     и серьёзностью флага (optionFlagOf, withOptionFlag);
//   - без ветки эскалации обращение сразу — только при серьёзности high или коде немедленной
//     эскалации таксономии (escalate_if.any_of_codes: F7 — предложение рекрутера); medium и low —
//     сигнал риска, HR увидит его в зоне риска;
//   - у варианта своя ветка ТЗ без эскалации — говорит она: F7 — сигнал риска без общего диалога,
//     остальное — обращение без сообщения новичку (карточка HR).
func (b *BotService) optionSignals(ctx context.Context, empID uuid.UUID, out surveyOutcome) {
	tax := b.tickets.tax
	var in []rag.Flag
	for _, f := range out.optFlags {
		in = append(in, rag.Flag{Code: f.Code, Severity: severityOf(f.Severity), Confidence: 1})
	}
	ds := decideFlags(tax, in, nil)
	quiet := false
	for i := range ds {
		d := &ds[i]
		if d.act == flagActRisk || immediateCategory(d.cat) {
			continue
		}
		switch {
		case out.optEsc:
			d.act = flagActRisk
		case d.severity < 3 && !slices.Contains(tax.EscalateCodes, d.code):
			d.act = flagActRisk
		case out.optBranch && d.act == flagActOffer:
			d.act = flagActRisk
		case out.optBranch:
			quiet = true
		}
	}
	if len(ds) == 0 {
		return
	}
	evID := out.ev.ID
	fi := flagInput{eventID: &evID, questionNo: out.flagQ, via: flagViaOption, quiet: quiet,
		where: "в ответе «" + out.optLabel + "» на опрос «" + out.step.Title + "»"}
	for _, d := range ds {
		logger.Info("бот: сигнал варианта %s sev %d → %s (ветка %v, эскалация в ветке %v, новичок %s)", d.code, d.severity, d.act, out.optBranch, out.optEsc, empID)
	}
	if err := b.applyFlagDecisions(ctx, empID, fi, ds); err != nil {
		logger.Warn("бот: сигнал варианта новичка %s не обработан: %v", empID, err)
	}
}

// immediateCategory — флаг, который уходит сразу при любой ветке: экстренный F0 и
// конфиденциальные F5/F9 (HR-партнёру или СБ, мимо руководителя).
func immediateCategory(cat taxonomy.Category) bool {
	return cat.Code == "F0" || cat.Restricted || cat.SecurityChat
}

// branchEscalates — в ветке есть путь к человеку: escalate, handoff или open_free_text в действиях
// ветки или терминальных кнопках («Описать проблему», «Чат с HR»).
func branchEscalates(br *domain.Branch) bool {
	if br == nil {
		return false
	}
	esc := func(acts []domain.Action) bool {
		for _, a := range acts {
			switch a.Type {
			case domain.ActionEscalate, domain.ActionHandoff, domain.ActionOpenFreeText:
				return true
			}
		}
		return false
	}
	if esc(br.Actions) {
		return true
	}
	for _, m := range br.Messages {
		for _, tb := range m.Buttons {
			if _, ok := actionOf(tb.Action); ok || tb.OpensFreeText || (tb.Branch != nil && esc(tb.Branch.Actions)) {
				return true
			}
		}
	}
	return false
}

// optionFlagOf — флаг варианта, чья ветка src («q<q>v<v>»); nil — не вариант или без флага.
func optionFlagOf(s domain.ScenarioStep, src string) *domain.OptionFlag {
	if s.Survey == nil || !strings.HasPrefix(src, "q") {
		return nil
	}
	qs, vs, ok := strings.Cut(src[1:], "v")
	q, err1 := strconv.Atoi(qs)
	_, err2 := strconv.Atoi(vs)
	if !ok || err1 != nil || err2 != nil || q < 0 || q >= len(s.Survey.Questions) {
		return nil
	}
	f, ok := s.Survey.Questions[q].OptionFlags[vs]
	if !ok || f.Code == "" {
		return nil
	}
	return &f
}

// withOptionFlag — ожидание текста после ветки варианта получает категорию и серьёзность флага
// (если ветка не задала категорию сама).
func withOptionFlag(w *domain.FreeTextWait, evID uuid.UUID, f domain.OptionFlag) {
	if w == nil || w.EventID != evID || f.Code == "" || w.Category != "" {
		return
	}
	w.Category, w.Severity = f.Code, f.Severity
}
