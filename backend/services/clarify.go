package services

import (
	"context"
	"strconv"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// Уточняющий вопрос после тревожного ответа (FEATURE_CLARIFY; ресерч 2026-10-24-sentiment-feedback,
// «Шаблоны уточнений (#6)»). Вопрос — по шаблону категории, не генерация моделью.
//
// Когда задаётся (кандидат считает applySurvey, задаёт afterSurvey → offerClarify):
//   - оценка ниже порога и выбрана кнопка причины без текста — категория из флага варианта
//     (option_flags оценки) или из подписи причины (reasonCategory);
//   - оценка ниже порога с флагом варианта у вопроса без комментария (typed scale);
//   - негативный текст короче 5 слов (regex-код или низкая оценка рядом) — категория из кода или
//     флага варианта;
//   - [точка расширения] флаг модели с уверенностью 0,3–0,6 (clarify_hint от /flags, агент H):
//     вызвать offerClarify(ctx, empID, evID, questionNo-1, code) — лимиты те же.
//
// Особые категории: F0 — без вопроса (контакты помощи уже ушли сигналом варианта: flagLead «112»
// и обращение); F5/F9 — без вопроса (сигнал варианта сам предлагает «Передать HR / СБ»);
// F10 — «Могу передать HR, если хотите» без вопросов.
//
// Лимиты: 1 уточнение на ответ, 2 на опрос, 1 в сутки на новичка; не во время живого диалога с
// HR (обращение в human_active); не когда у варианта своя ветка ТЗ или бот ждёт свободный текст.
//
// Где хранится: в SurveyResult (jsonb события) — clarify/clarify_at/clarify_done; ответ —
// дописывается к причине ответа «<причина> → <уточнение>» в SurveyAnswer.Reason и
// SurveyResult.Reason (схема таблиц не меняется). Без причины — «<тема шаблона> → <уточнение>».
// Так уточнение видно в строке опроса в траектории карточки (ev.Answer пересобирается) и
// попадает в темы дайджеста (digest_cluster.go, splitClarify).

const (
	preClarify   = "sq:" // sq:<event>:<q>:<i> — вариант уточнения
	preClarifyNo = "sx:" // sx:<event>:<q> — «Не хочу отвечать» / «Не нужно»
)

// clarifySep — разделитель причины и уточнения в SurveyAnswer.Reason.
const clarifySep = " → "

const (
	clarifyPerSurvey = 2
	clarifyGap       = 24 * time.Hour
	clarifyMaxWords  = 5 // негативный текст короче — повод уточнить
)

const (
	btnClarifyNo      = "Не хочу отвечать"
	btnClarifyHandoff = "Передать HR"
	btnClarifyNoNeed  = "Не нужно"
	textClarifyLeadTy = "Если можешь, уточни — так HR поможет точнее."
	textClarifyLeadVy = "Если можете, уточните — так HR поможет точнее."
	textClarifyThanks = "Спасибо, записал."
	textClarifyNo     = "Понял, не буду."
	textClarifyF10Ty  = "Могу передать HR, если хочешь."
	textClarifyF10Vy  = "Могу передать HR, если хотите."
)

// clarifyTemplate — шаблон уточнения категории. Theme — основа записи, когда у ответа нет кнопки
// причины; No — подпись кнопки отказа (пусто — «Не хочу отвечать»).
type clarifyTemplate struct {
	Theme    string
	Question string
	Options  []string
	No       string
}

var clarifyPeople = clarifyTemplate{Theme: "С кем сложно", Question: "С кем именно сложно?",
	Options: []string{"Наставник", "Руководитель", "Коллеги"}, No: "Не хочу говорить"}

var clarifyTemplates = map[string]clarifyTemplate{
	"F1": {Theme: "Непонятно", Question: "Что именно непонятно?", Options: []string{"Что делать", "Как оценят", "К кому идти"}},
	"F2": {Theme: "Не хватает", Question: "Чего не хватает?", Options: []string{"Техника", "Доступы", "СИЗ/пропуск"}},
	"F3": clarifyPeople,
	"F4": clarifyPeople,
	"F6": {Theme: "Перегружает", Question: "Что перегружает?", Options: []string{"Объём", "Смены/график", "Нет обучения"}},
	"F8": {Theme: "Не совпало", Question: "Что не совпало?", Options: []string{"Зарплата", "График", "Оформление"}},
}

// clarifySilent — категории без уточняющего вопроса: свой маршрут у сигнала варианта.
var clarifySilent = map[string]bool{"F0": true, "F5": true, "F9": true}

func (t clarifyTemplate) noLabel() string { return orDefault(t.No, btnClarifyNo) }

// reasonCategory — категория по подписи кнопки причины (defaultReasons и свои причины HR).
func reasonCategory(reason string) string {
	l := strings.ToLower(reason)
	has := func(ws ...string) bool {
		for _, w := range ws {
			if strings.Contains(l, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("люд", "общени", "коллег", "наставн", "руковод", "мастер"):
		return "F4"
	case has("непонят", "обучени"):
		return "F1"
	case has("задач", "нагруз", "перегруз", "смен"):
		return "F6"
	case has("услови", "место", "техник", "доступ", "сиз", "пропуск", "оборудован"):
		return "F2"
	case has("зарплат", "оплат", "график", "обещ"):
		return "F8"
	}
	return ""
}

// clarifyCode — категория уточнения: особый флаг варианта главнее, затем причина, затем флаг.
func clarifyCode(opt, reason string) string {
	if clarifySilent[opt] || opt == "F10" {
		return opt
	}
	if c := reasonCategory(reason); c != "" {
		return c
	}
	return opt
}

// optCode — код флага варианта для значения ответа r.
func optCode(q domain.SurveyQuestion, r domain.SurveyResult) string {
	if r.Value == nil {
		return ""
	}
	return q.OptionFlags[strconv.Itoa(*r.Value)].Code
}

// reasonClarify — кандидат после кнопки причины (оценка ниже порога, текста нет).
func reasonClarify(q domain.SurveyQuestion, r domain.SurveyResult, reason string) string {
	return clarifyCode(optCode(q, r), reason)
}

// answerClarify — кандидат после оценки ниже порога у вопроса без экрана причин и комментария,
// когда у варианта есть флаг и нет своей ветки.
func answerClarify(sv *domain.Survey, q domain.SurveyQuestion, v int, screen string, branch bool) string {
	if screen != "" || branch || !lowValue(sv, q, v) {
		return ""
	}
	return clarifyCode(q.OptionFlags[strconv.Itoa(v)].Code, "")
}

// textClarify — кандидат после текста короче clarifyMaxWords слов, если он негативный: regex-код
// или ответ рядом ниже порога / «нет».
func textClarify(sv *domain.Survey, q domain.SurveyQuestion, r domain.SurveyResult, text string, codes []string) string {
	if n := len(strings.Fields(text)); n == 0 || n >= clarifyMaxWords {
		return ""
	}
	low := r.Value != nil && (lowValue(sv, q, *r.Value) || (qScale(sv, q) == domain.ScaleYesNo && *r.Value == 0))
	if len(codes) == 0 && !low {
		return ""
	}
	for _, c := range codes {
		if _, ok := clarifyTemplates[c]; ok || clarifySilent[c] || c == "F10" {
			return c
		}
	}
	return clarifyCode(optCode(q, r), "")
}

// offerClarify — задать уточнение к ответу qi события evID, если позволяют флаг и лимиты.
// true — сообщение ушло.
func (b *BotService) offerClarify(ctx context.Context, empID, evID uuid.UUID, qi int, code string) bool {
	if !b.clarify || code == "" {
		return false
	}
	tpl, hasTpl := clarifyTemplates[code]
	if !hasTpl && code != "F10" {
		logger.Info("бот: уточнения для %s нет (новичок %s)", code, empID)
		return false
	}
	var emp *domain.Employee
	var ev *domain.ScheduledEvent
	why := ""
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		x, err := tx.Events.ByIDForUpdate(ctx, evID)
		if err != nil || x == nil || x.EmployeeID != empID || x.Survey == nil || qi < 0 || qi >= len(x.Survey.Results) {
			why = "нет ответа"
			return err
		}
		st := x.Survey
		if st.Results[qi].Clarify != "" {
			why = "уже было к этому ответу"
			return nil
		}
		n := 0
		for _, r := range st.Results {
			if r.Clarify != "" {
				n++
			}
		}
		if n >= clarifyPerSurvey {
			why = "лимит на опрос"
			return nil
		}
		if e.AwaitFreeText != nil {
			why = "бот ждёт свободный текст"
			return nil
		}
		if t, err := tx.Tickets.Conversation(ctx, e.ID); err != nil {
			return err
		} else if t != nil && t.Status == domain.TicketHumanActive {
			why = "идёт диалог с HR"
			return nil
		}
		if code == "F10" {
			if t, err := tx.Tickets.Active(ctx, e.ID, code); err != nil {
				return err
			} else if t != nil {
				why = "обращение F10 уже открыто"
				return nil
			}
		}
		now := b.now()
		evs, err := tx.Events.ByEmployee(ctx, e.ID)
		if err != nil {
			return err
		}
		for _, o := range evs {
			if o.Survey == nil {
				continue
			}
			for _, r := range o.Survey.Results {
				if r.ClarifyAt != nil && now.Sub(*r.ClarifyAt) < clarifyGap {
					why = "уже было за сутки"
					return nil
				}
			}
		}
		st.Results[qi].Clarify, st.Results[qi].ClarifyAt = code, &now
		if err := tx.Events.Save(ctx, x); err != nil {
			return err
		}
		emp, ev = e, x
		return nil
	})
	if err != nil {
		logger.Warn("бот: уточнение %s новичку %s не задано: %v", code, empID, err)
		return false
	}
	if emp == nil {
		if why != "" {
			logger.Info("бот: уточнение %s новичку %s пропущено: %s", code, empID, why)
		}
		return false
	}
	id, qn := evID.String(), strconv.Itoa(qi)
	var m max.OutMessage
	if code == "F10" {
		m = recipientOf(emp).msg(clarifyPrefix(ev)+byForm(emp, textClarifyF10Ty, textClarifyF10Vy), "", [][]max.Button{
			{{Text: btnClarifyHandoff, Payload: preHuman + id}, {Text: btnClarifyNoNeed, Payload: preClarifyNo + id + ":" + qn}},
		})
	} else {
		var btns []max.Button
		for i, o := range tpl.Options {
			btns = append(btns, max.Button{Text: o, Payload: preClarify + id + ":" + qn + ":" + strconv.Itoa(i)})
		}
		btns = append(btns, max.Button{Text: tpl.noLabel(), Payload: preClarifyNo + id + ":" + qn})
		rows := append(packRows(btns), humanRow(&evID))
		m = recipientOf(emp).msg(clarifyQuestionText(emp, ev, tpl), formatMarkdown, rows)
	}
	logger.Info("бот: уточнение %s новичку %s (событие %s, вопрос %d)", code, empID, evID, qi+1)
	b.send(ctx, m)
	return true
}

func clarifyPrefix(ev *domain.ScheduledEvent) string {
	if ev != nil && ev.IsTest {
		return testPrefix
	}
	return ""
}

func clarifyQuestionText(e *domain.Employee, ev *domain.ScheduledEvent, tpl clarifyTemplate) string {
	return escapeMarkdown(clarifyPrefix(ev)+byForm(e, textClarifyLeadTy, textClarifyLeadVy)) + "\n\n**" + tpl.Question + "**"
}

func isClarifyPayload(p string) bool {
	return strings.HasPrefix(p, preClarify) || strings.HasPrefix(p, preClarifyNo)
}

// clarifyCallback — ответ на уточнение: вариант дописывается к причине ответа, «Не хочу
// отвечать» — «Понял, не буду» (отказом не считается, наджей нет).
func (b *BotService) clarifyCallback(ctx context.Context, u max.Update, empID uuid.UUID) error {
	no := strings.HasPrefix(u.Payload, preClarifyNo)
	rest := strings.TrimPrefix(strings.TrimPrefix(u.Payload, preClarifyNo), preClarify)
	parts := strings.Split(rest, ":")
	evID, err := uuid.Parse(parts[0])
	qi, pick := -1, -1
	if err == nil && len(parts) > 1 {
		qi, err = strconv.Atoi(parts[1])
	}
	if err == nil && !no && len(parts) > 2 {
		pick, err = strconv.Atoi(parts[2])
	}
	if err != nil || qi < 0 || (!no && pick < 0) {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	note := ""
	var done *max.OutMessage
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		if ev == nil || e == nil || ev.Survey == nil || qi >= len(ev.Survey.Results) || ev.Survey.Results[qi].Clarify == "" {
			note = textButtonStale
			return nil
		}
		r := &ev.Survey.Results[qi]
		if r.ClarifyDone {
			note = "Уже записал"
			return nil
		}
		code := r.Clarify
		tpl, hasTpl := clarifyTemplates[code]
		if !no && (!hasTpl || pick >= len(tpl.Options)) {
			note = "Эта кнопка больше не работает"
			return nil
		}
		now := b.now()
		e.LastActivityAt = &now
		r.ClarifyDone = true
		to := recipientOf(e)
		if no {
			m := to.msg(clarifyPrefix(ev)+textClarifyNo, "", nil)
			done, note = &m, "Хорошо"
			logger.Info("бот: уточнение %s новичка %s — не хочет отвечать", code, empID)
		} else {
			label := tpl.Options[pick]
			r.Reason = withClarify(r.Reason, tpl.Theme, label)
			a, err := tx.Events.Answer(ctx, ev.ID, qi+1)
			if err != nil {
				return err
			}
			if a != nil {
				a.Reason = withClarify(a.Reason, tpl.Theme, label)
				if err := tx.Events.UpsertAnswer(ctx, a); err != nil {
					return err
				}
			}
			if ev.Status == domain.EventAnswered && step.Survey != nil {
				ev.Answer = strPtr(surveySummary(step.Survey, ev.Survey))
			}
			m := to.msg(clarifyQuestionText(e, ev, tpl)+"\n✅ "+escapeMarkdown(label)+"\n"+textClarifyThanks, formatMarkdown, nil)
			done, note = &m, "Записал"
			logger.Info("бот: уточнение %s новичка %s записано", code, empID)
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, max.Answer{Notification: note, Message: done})
	return nil
}

// withClarify — «<причина> → <уточнение>»; без причины — от темы шаблона. Повтор не дописывается.
func withClarify(reason, theme, label string) string {
	base, _ := splitClarify(reason)
	if base == "" {
		base = theme
	}
	return base + clarifySep + label
}

// splitClarify — причина и уточнение из SurveyAnswer.Reason.
func splitClarify(reason string) (string, string) {
	base, clar, ok := strings.Cut(reason, clarifySep)
	if !ok {
		return reason, ""
	}
	return base, clar
}
