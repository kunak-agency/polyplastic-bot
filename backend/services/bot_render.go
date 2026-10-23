package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"

	"github.com/google/uuid"
)

// Рендер сообщений бота: тексты системных шагов, клавиатуры, «живой опрос». Правила
// UX-спецификации (п. 0, 2.2, 2.4): последний ряд — «Спросить человека», ≤ 2 текстовых кнопки
// в ряд, шкала 1–10 — два ряда по пять, после ответа сообщение правится в итог без клавиатуры.
//
// Пользовательский и редактируемый HR текст в разметку не вставляем как есть: в сообщениях с
// format=markdown он проходит через escapeMarkdown, шаги сценария уходят без формата.

const formatMarkdown = "markdown"

// Подписи общих кнопок.
const (
	btnHuman    = "Спросить человека"
	btnSkip     = "Пропустить"
	btnRemind   = "Напомнить завтра"
	btnWriteOwn = "Напишу сам(а)"
)

// Payload кнопок. Короткие префиксы: payload callback в MAX ограничен по длине, а id события
// (UUID) и так занимает 36 символов.
const (
	payAddressTy   = "a:ty"
	payAddressVy   = "a:vy"
	payConsentYes  = "c:yes"
	payConsentNo   = "c:no"
	payConsentMore = "c:more"
	payHuman       = "h"
	preHuman       = "h:"  // h:<event>
	preStepButton  = "b:"  // b:<event>:<i>
	preAnswer      = "s:"  // s:<event>:<q>:<value>
	preReason      = "sr:" // sr:<event>:<q>:<i>
	preWrite       = "sw:" // sw:<event>:<q>
	preSkip        = "ss:" // ss:<event>:<q>
	preRemind      = "sm:" // sm:<event>
)

// Тексты системных сообщений. Демо-формулировки по UX-спецификации п. 2.1; юридическую часть
// согласия сверить с юристом (там же).
const (
	textIntro = "**Привет! Я Корсик 🐾 — помощник новых сотрудников ПОЛИПЛАСТИК.**\n" +
		"Я бот, но за мной стоит команда HR.\n\n" +
		"Что умею: 1️⃣ напомню, что взять в первый день · 2️⃣ отвечу на вопросы по правилам, графику, " +
		"пропускам · 3️⃣ иногда коротко спрошу, как дела · 4️⃣ передам вопрос живому человеку, если сам " +
		"не справлюсь.\n\n**Как мне к вам обращаться?**"
	textConsent = "**Коротко о данных.**\n" +
		"Чтобы отвечать и помогать HR, я обрабатываю наши сообщения. Ответы на опросы видит HR-команда; " +
		"руководитель видит только общую картину по своим новичкам. Автоматических кадровых решений " +
		"система не принимает."
	textConsentMore = "Подробнее о данных (демо-текст).\n" +
		"Я храню ваши ответы на кнопки и сообщения, чтобы HR мог помочь с адаптацией. Опросы " +
		"необязательны: можно отказаться, и я буду только напоминать о важном и отвечать на вопросы. " +
		"Передумаете — нажмите кнопку согласия в сообщении выше."
	textConsentGiven    = "✅ Спасибо, согласие записано. Дальше — немного полезного перед первым днём."
	textConsentDeclined = "Понял. Опросы присылать не буду — только напоминания и ответы на вопросы. " +
		"Если передумаете, напишите HR-команде."
	// Варианты на «ты»: новичок уже выбрал форму обращения в сообщении 1.
	textConsentGivenTy    = textConsentGiven
	textConsentDeclinedTy = "Понял. Опросы присылать не буду — только напоминания и ответы на вопросы. " +
		"Если передумаешь, напиши HR-команде."
	textInviteBroken  = "Ссылка не сработала. Напишите рекрутеру — он пришлёт новую."
	btnCheckPhone     = "Проверить по телефону"
	textHumanNoted    = "Записал: передам ваш запрос HR-команде, с вами свяжутся."
	textWelcomeBack   = "С возвращением! Я на связи 🐾"
	textContactNoted  = "Спасибо! Передам рекрутеру, он проверит и пришлёт новую ссылку."
	textSurveyDone    = "✅ Спасибо! Ответы записаны."
	textRemindLater   = "🕓 Хорошо, напомню завтра."
	textRemindStopped = "Хорошо, не буду напоминать. Если захотите вернуться к опросу — напишите HR-команде."
	textWriteOwn      = "Напишите одним сообщением, что не так. Я привяжу ответ к этому вопросу."
	testPrefix        = "[ТЕСТ] "
)

// maxReminders — «Напомнить завтра» не более двух раз (UX-спецификация п. 2.3).
const maxReminders = 2

// defaultReasons — причины низкой оценки, если HR не задал свои у вопроса.
var defaultReasons = []string{"Задачи и нагрузка", "Люди и общение", "Условия и место", "Другое"}

// moodLabels — шкала 1–5 эмодзи (UX-спецификация п. 2.3, D0 18:00); хранится число.
var moodLabels = []string{"😞", "😕", "😐", "🙂", "😄"}

// escapeMarkdown — текст, который вставляется в сообщение с format=markdown. Символы разметки
// экранируются обратной чертой. [проверить]: что MAX понимает «\*» (CommonMark — да); если
// нет — такие сообщения слать без формата.
func escapeMarkdown(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '~', '`', '[', ']', '<', '>', '+':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// FirstName — имя из «Фамилия Имя Отчество» (порядок HR-базы); пометки в скобках отбрасываются.
func FirstName(full string) string {
	if i := strings.Index(full, "("); i >= 0 {
		full = full[:i]
	}
	f := strings.Fields(full)
	switch len(f) {
	case 0:
		return ""
	case 1:
		return f[0]
	}
	return f[1]
}

// humanRow — последний ряд любой клавиатуры.
func humanRow(eventID *uuid.UUID) []max.Button {
	p := payHuman
	if eventID != nil {
		p = preHuman + eventID.String()
	}
	return []max.Button{{Text: btnHuman, Payload: p}}
}

// introMessage — сообщение 1: знакомство и выбор «ты/вы».
func introMessage(to recipient) max.OutMessage {
	return to.msg(textIntro, formatMarkdown, [][]max.Button{
		{{Text: "На «ты»", Payload: payAddressTy}, {Text: "На «вы»", Payload: payAddressVy}},
		humanRow(nil),
	})
}

// addressChosenText — итог сообщения 1 после выбора.
func addressChosenText(form, fullName string) string {
	how := "на «вы»"
	if form == domain.AddressTy {
		how = "на «ты»"
	}
	s := "✅ Договорились: " + how + "."
	if name := FirstName(fullName); name != "" {
		s += " Очень приятно, " + escapeMarkdown(name) + "!"
	}
	return s
}

// consentMessage — сообщение 2: согласие.
func consentMessage(to recipient) max.OutMessage {
	return to.msg(textConsent, formatMarkdown, [][]max.Button{
		{{Text: "Согласен(на)", Payload: payConsentYes}, {Text: "Подробнее", Payload: payConsentMore}},
		{{Text: "Не согласен(на)", Payload: payConsentNo}},
		humanRow(nil),
	})
}

// inviteBrokenMessage — неизвестный или просроченный токен: текст спецификации и запрос контакта.
func inviteBrokenMessage(to recipient) max.OutMessage {
	return to.msg(textInviteBroken, "", [][]max.Button{{{Text: btnCheckPhone, Contact: true}}})
}

// recipient — адресат: чат, если известен, иначе пользователь.
type recipient struct {
	ChatID int64
	UserID int64
}

func (r recipient) msg(text, format string, buttons [][]max.Button) max.OutMessage {
	return max.OutMessage{ChatID: r.ChatID, UserID: r.UserID, Text: text, Format: format, Buttons: buttons}
}

// sentAt — момент, от которого считается {start_day} при правке уже ушедшего сообщения: когда
// оно ушло; ещё не ушло — now.
func sentAt(ev *domain.ScheduledEvent, now time.Time) time.Time {
	if ev != nil && ev.SentAt != nil {
		return *ev.SentAt
	}
	return now
}

func recipientOf(e *domain.Employee) recipient {
	var r recipient
	if e.ChatID != nil {
		r.ChatID = *e.ChatID
	}
	if e.MaxUserID != nil {
		r.UserID = *e.MaxUserID
	}
	return r
}

// stepText — текст шага в выбранной форме обращения, с подстановкой имени и дня выхода. Пока
// форма не выбрана — «вы». Пустой вариант берётся из другого. at — когда сообщение уходит (или
// ушло): от него зависит, «завтра» ли день выхода.
func stepText(s domain.ScenarioStep, e *domain.Employee, at time.Time) string {
	text := s.TextVy
	if e.AddressForm != nil && *e.AddressForm == domain.AddressTy {
		text = s.TextTy
	}
	if strings.TrimSpace(text) == "" {
		text = s.TextTy + s.TextVy
	}
	if strings.TrimSpace(text) == "" {
		text = s.Title
	}
	name := FirstName(e.FullName)
	day := StartDayPhrase(e.StartDate, at, domain.SiteOf(e.Site))
	return strings.NewReplacer("{Имя}", name, "{имя}", name, "{name}", name,
		"{start_day}", day, "{Start_day}", capitalize(day)).Replace(text)
}

// Плейсхолдер дня выхода (api_contract.md, «Плейсхолдеры в текстах шагов»).
var (
	weekdayIn = [7]string{"в воскресенье", "в понедельник", "во вторник", "в среду", "в четверг", "в пятницу", "в субботу"}
	monthGen  = [12]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
)

// StartDayPhrase — {start_day}: «завтра», если выход на следующий календарный день после at
// (по часам площадки), «сегодня» — в сам день выхода, иначе «в понедельник, 26 октября». Шаг
// D−1, перенесённый с выходного на пятницу, так не обещает «завтра». Нулевой at — всегда дата.
func StartDayPhrase(start, at time.Time, site domain.Site) string {
	d0 := civilDate(start, site.Loc)
	if !at.IsZero() {
		switch DayNumber(start, at, site) {
		case -1:
			return "завтра"
		case 0:
			return "сегодня"
		}
	}
	return weekdayIn[d0.Weekday()] + ", " + strconv.Itoa(d0.Day()) + " " + monthGen[d0.Month()-1]
}

// capitalize — первая буква заглавной: {Start_day} в начале предложения.
func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// buttonURL — адрес кнопки-ссылки из action: «link:<url>» или сам URL.
func buttonURL(action string) string {
	if u, ok := strings.CutPrefix(action, "link:"); ok {
		return strings.TrimSpace(u)
	}
	if strings.HasPrefix(action, "https://") || strings.HasPrefix(action, "http://") {
		return action
	}
	return ""
}

// stepMessage — обычный шаг (не опрос): текст без формата, кнопки шага по две в ряд и
// «Спросить человека» последним рядом. Без кнопок — без клавиатуры.
func stepMessage(to recipient, s domain.ScenarioStep, e *domain.Employee, eventID uuid.UUID, test bool, at time.Time) max.OutMessage {
	text := stepText(s, e, at)
	if test {
		text = testPrefix + text
	}
	var rows [][]max.Button
	var row []max.Button
	for i, b := range s.Buttons {
		if b.Action == "human" {
			continue // «Спросить человека» и так последним рядом
		}
		btn := max.Button{Text: b.Label, Payload: fmt.Sprintf("%s%s:%d", preStepButton, eventID, i)}
		if u := buttonURL(b.Action); u != "" {
			btn = max.Button{Text: b.Label, URL: u}
		}
		row = append(row, btn)
		if len(row) == 2 {
			rows, row = append(rows, row), nil
		}
	}
	if row != nil {
		rows = append(rows, row)
	}
	if len(s.Buttons) > 0 {
		rows = append(rows, humanRow(&eventID))
	}
	return to.msg(text, "", rows)
}

// stepAnsweredText — итог обычного шага после нажатия: исходный текст и отметка выбора.
func stepAnsweredText(s domain.ScenarioStep, e *domain.Employee, label string, test bool, at time.Time) string {
	text := stepText(s, e, at)
	if test {
		text = testPrefix + text
	}
	return text + "\n\n✅ " + label
}

// scaleMax — верх шкалы для строки итога.
func scaleMax(scale string) int {
	switch scale {
	case domain.Scale10:
		return 10
	case domain.Scale5:
		return 5
	}
	return 0
}

// lowScore — ниже порога: спросить причину. Только числовые шкалы.
func lowScore(sv *domain.Survey, v int) bool {
	return sv.Threshold > 0 && scaleMax(sv.Scale) > 0 && v < sv.Threshold
}

// valueLabel — как ответ выглядит в строке итога.
func valueLabel(sv *domain.Survey, q domain.SurveyQuestion, v int) string {
	switch sv.Scale {
	case domain.Scale10, domain.Scale5:
		return fmt.Sprintf("%d/%d", v, scaleMax(sv.Scale))
	case domain.ScaleYesNo:
		if v == 1 {
			return "да"
		}
		return "нет"
	case domain.ScaleChoice:
		if v >= 1 && v <= len(q.Options) {
			return escapeMarkdown(q.Options[v-1])
		}
	}
	return strconv.Itoa(v)
}

// shortQuestion — вопрос в строке итога: до 48 символов.
func shortQuestion(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > 48 {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:47])) + "…"
	}
	return escapeMarkdown(s)
}

// surveyMessage — текущий экран живого опроса. Готовый опрос (state.Q за последним вопросом)
// — итог без клавиатуры.
func surveyMessage(to recipient, s domain.ScenarioStep, e *domain.Employee, ev *domain.ScheduledEvent, at time.Time) max.OutMessage {
	sv := s.Survey
	st := ev.Survey
	var b strings.Builder
	if ev.IsTest {
		b.WriteString(testPrefix)
	}
	done := st.Q >= len(sv.Questions)
	if done {
		b.WriteString(textSurveyDone)
	} else if intro := strings.TrimSpace(stepText(s, e, at)); intro != "" {
		b.WriteString(escapeMarkdown(intro))
	}
	for i, r := range st.Results {
		if i >= len(sv.Questions) {
			break
		}
		q := sv.Questions[i]
		b.WriteString("\n")
		switch {
		case r.Skipped:
			fmt.Fprintf(&b, "— %d. %s: пропущено", i+1, shortQuestion(q.Text))
		case r.Value != nil:
			fmt.Fprintf(&b, "✅ %d. %s: %s", i+1, shortQuestion(q.Text), valueLabel(sv, q, *r.Value))
			if r.Reason != "" {
				fmt.Fprintf(&b, " (%s)", escapeMarkdown(r.Reason))
			} else if r.Wrote {
				b.WriteString(" (причина записана)")
			}
		}
	}
	if done {
		return to.msg(b.String(), formatMarkdown, nil)
	}

	q := sv.Questions[st.Q]
	id := ev.ID.String()
	qn := strconv.Itoa(st.Q)
	var rows [][]max.Button
	switch st.Screen {
	case domain.ScreenReasons:
		b.WriteString("\n\n**Что именно не так?**\nВыберите вариант или напишите своими словами.")
		reasons := q.Options
		if len(reasons) == 0 || sv.Scale == domain.ScaleChoice {
			reasons = defaultReasons
		}
		var row []max.Button
		for i, label := range reasons {
			row = append(row, max.Button{Text: label, Payload: preReason + id + ":" + qn + ":" + strconv.Itoa(i)})
			if len(row) == 2 {
				rows, row = append(rows, row), nil
			}
		}
		if row != nil {
			rows = append(rows, row)
		}
		rows = append(rows, []max.Button{{Text: btnWriteOwn, Payload: preWrite + id + ":" + qn}, {Text: btnSkip, Payload: preSkip + id + ":" + qn}})
	case domain.ScreenWrite:
		b.WriteString("\n\n" + textWriteOwn)
		rows = append(rows, []max.Button{{Text: btnSkip, Payload: preSkip + id + ":" + qn}})
	default:
		fmt.Fprintf(&b, "\n\n**Вопрос %d из %d**\n%s", st.Q+1, len(sv.Questions), escapeMarkdown(q.Text))
		rows = append(rows, scaleRows(sv, q, id, qn)...)
		rows = append(rows, []max.Button{{Text: btnSkip, Payload: preSkip + id + ":" + qn}, {Text: btnRemind, Payload: preRemind + id}})
	}
	rows = append(rows, humanRow(&ev.ID))
	return to.msg(b.String(), formatMarkdown, rows)
}

// scaleRows — кнопки шкалы: 1–10 двумя рядами по пять, 1–5 одним рядом эмодзи, да/нет,
// варианты по два в ряд.
func scaleRows(sv *domain.Survey, q domain.SurveyQuestion, id, qn string) [][]max.Button {
	answer := func(label string, v int) max.Button {
		return max.Button{Text: label, Payload: preAnswer + id + ":" + qn + ":" + strconv.Itoa(v)}
	}
	var rows [][]max.Button
	switch sv.Scale {
	case domain.Scale10:
		for start := 1; start <= 10; start += 5 {
			var row []max.Button
			for v := start; v < start+5; v++ {
				row = append(row, answer(strconv.Itoa(v), v))
			}
			rows = append(rows, row)
		}
	case domain.Scale5:
		var row []max.Button
		for v := 1; v <= 5; v++ {
			row = append(row, answer(moodLabels[v-1], v))
		}
		rows = append(rows, row)
	case domain.ScaleYesNo:
		rows = append(rows, []max.Button{answer("Да", 1), answer("Нет", 0)})
	case domain.ScaleChoice:
		var row []max.Button
		for i, o := range q.Options {
			row = append(row, answer(o, i+1))
			if len(row) == 2 {
				rows, row = append(rows, row), nil
			}
		}
		if row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

// validValue — значение допустимо для шкалы вопроса.
func validValue(sv *domain.Survey, q domain.SurveyQuestion, v int) bool {
	switch sv.Scale {
	case domain.Scale10:
		return v >= 1 && v <= 10
	case domain.Scale5:
		return v >= 1 && v <= 5
	case domain.ScaleYesNo:
		return v == 0 || v == 1
	case domain.ScaleChoice:
		return v >= 1 && v <= len(q.Options)
	}
	return false
}

// numberWords — оценка словом («семь»): UX-спецификация п. 2.4 принимает текстовый ответ.
var numberWords = map[string]int{
	"один": 1, "одна": 1, "два": 2, "две": 2, "три": 3, "четыре": 4, "пять": 5,
	"шесть": 6, "семь": 7, "восемь": 8, "девять": 9, "десять": 10,
}

// parseScore — оценка из текста: «7», «7/10», «семь». ok=false — это не оценка.
func parseScore(text string) (int, bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimSuffix(strings.TrimSuffix(t, "/10"), "/5")
	t = strings.Trim(t, " .!")
	if v, err := strconv.Atoi(t); err == nil {
		return v, true
	}
	v, ok := numberWords[t]
	return v, ok
}
