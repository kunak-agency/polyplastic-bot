package services

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"

	"github.com/google/uuid"
)

// UX-правки после живого прогона в MAX (24.10): обещание времени после согласия, последний ряд
// «Спросить человека», без повтора предложения F7, цитата-обрывок в «Подробнее».

func TestSendWhenPhrase(t *testing.T) {
	now := msk(2026, 10, 30, 22, 0) // пятница, вечер
	cases := []struct {
		at   time.Time
		want string
	}{
		{msk(2026, 10, 30, 23, 30), "сегодня в 23:30"},
		{msk(2026, 10, 31, 9, 5), "завтра в 9:05"},
		{msk(2026, 11, 2, 10, 0), "в понедельник в 10:00"},
		{msk(2026, 11, 3, 10, 0), "во вторник в 10:00"},
		{msk(2026, 11, 9, 10, 0), "в понедельник, 9 ноября, в 10:00"},
		{time.Date(2026, 10, 30, 21, 30, 0, 0, time.UTC), "завтра в 0:30"}, // часы площадки, а не UTC
	}
	for _, c := range cases {
		if got := sendWhenPhrase(c.at, now, gtp); got != c.want {
			t.Errorf("%v: %q, want %q", c.at, got, c.want)
		}
	}
}

func TestConsentAnsweredText(t *testing.T) {
	ty, vy := domain.AddressTy, domain.AddressVy
	eTy := &domain.Employee{Site: domain.SiteGTP, AddressForm: &ty}
	eVy := &domain.Employee{Site: domain.SiteGTP, AddressForm: &vy}
	now := msk(2026, 10, 30, 22, 0)
	soon, monday := now.Add(2*time.Minute), msk(2026, 11, 2, 10, 0)

	if got := consentAnsweredText(eVy, true, &soon, now, domain.SiteOf(eVy.Site)); got != textConsentGiven {
		t.Errorf("шаг уходит сейчас: %q", got)
	}
	if got := consentAnsweredText(eVy, true, &monday, now, domain.SiteOf(eVy.Site)); got != "✅ Спасибо, согласие записано. Первые материалы пришлю "+
		"в понедельник в 10:00. А пока можете спросить меня о пропуске, графике или зарплате — отвечу по документам." {
		t.Errorf("вы, понедельник: %q", got)
	}
	if got := consentAnsweredText(eTy, true, &monday, now, domain.SiteOf(eTy.Site)); !strings.Contains(got, "пришлю в понедельник в 10:00. А пока можешь") {
		t.Errorf("ты, понедельник: %q", got)
	}
	if got := consentAnsweredText(eTy, true, nil, now, domain.SiteOf(eTy.Site)); got != textConsentGivenNoneTy {
		t.Errorf("шагов нет: %q", got)
	}
	if got := consentAnsweredText(eVy, false, &monday, now, domain.SiteOf(eVy.Site)); got != "Понял. Опросы присылать не буду — только напоминания и "+
		"ответы на вопросы. Первое напоминание пришлю в понедельник в 10:00. Если передумаете, напишите HR-команде." {
		t.Errorf("отказ, понедельник: %q", got)
	}
	if got := consentAnsweredText(eTy, false, &soon, now, domain.SiteOf(eTy.Site)); strings.Contains(got, "пришлю") || !strings.HasSuffix(got, "напиши HR-команде.") {
		t.Errorf("отказ, сейчас: %q", got)
	}
}

// consentReply — итог сообщения согласия (правка через AnswerCallback).
func consentReply(t *testing.T, f *max.Fake) string {
	t.Helper()
	for _, a := range f.Answers() {
		if a.Answer.Message != nil && (strings.HasPrefix(a.Answer.Message.Text, "✅ Спасибо, согласие") || strings.HasPrefix(a.Answer.Message.Text, "Понял. Опросы")) {
			return a.Answer.Message.Text
		}
	}
	t.Fatalf("нет итога согласия: %+v", f.Answers())
	return ""
}

// Согласие в пятницу ночью: D−3 (суббота) перенесён на понедельник 10:00 — бот так и говорит,
// а не «дальше — немного полезного». Ничего не уходит до понедельника.
func TestConsentAtNightPromisesTime(t *testing.T) {
	e := newEnv(t, msk(2026, 10, 30, 22, 0)) // пятница 22:00, выход во вторник 3.11
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	if got := consentReply(t, e.fake); !strings.Contains(got, "Первые материалы пришлю в понедельник в 10:00. А пока можете") {
		t.Fatalf("итог согласия: %q", got)
	}
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "паспорт") {
			t.Fatalf("шаг ушёл ночью: %q", m.Text)
		}
	}
	if n, _ := e.svc.Scheduler.Tick(context.Background()); n != 0 {
		t.Fatalf("такт ночью отправил %d", n)
	}

	// Четверг ночью, выход в понедельник: D−3 — пятница 10:00, «завтра», на «ты».
	e = newEnv(t, msk(2026, 10, 29, 22, 0))
	c = e.launch(t, date(2026, 11, 2))
	e.connect(t, c, domain.AddressTy, true)
	if got := consentReply(t, e.fake); !strings.Contains(got, "Первые материалы пришлю завтра в 10:00. А пока можешь") {
		t.Fatalf("итог согласия на «ты»: %q", got)
	}

	// Отказ ночью — то же время для первого напоминания.
	e = newEnv(t, msk(2026, 10, 30, 22, 0))
	c = e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, false)
	if got := consentReply(t, e.fake); !strings.Contains(got, "Первое напоминание пришлю в понедельник в 10:00.") {
		t.Fatalf("итог отказа: %q", got)
	}
}

// Днём, когда пакет D−3 созрел, текст прежний.
func TestConsentDaytimeKeepsText(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	if got := consentReply(t, e.fake); got != textConsentGiven {
		t.Fatalf("итог согласия: %q", got)
	}
}

// Правило 3: в каждом сообщении с кнопками последний ряд — «Спросить человека». Исключения:
// отказ, тематический маршрут, сбой rag и «Передать HR?» — там уже есть «Передать HR»;
// предложение F7 — «Передать рекрутеру»; статусы обращения (человек уже подключён).
func TestKeyboardsEndWithHuman(t *testing.T) {
	to := recipient{UserID: 1}
	vy := domain.AddressVy
	e := &domain.Employee{FullName: "Тестов Пётр", Site: domain.SiteGTP, AddressForm: &vy}
	evID := uuid.New()
	step := domain.ScenarioStep{TextVy: "Шаг", Buttons: domain.ButtonList{{Label: "Да"}, {Label: "Открыть", Action: "link:https://example.org"}}}
	linkOnly := domain.ScenarioStep{TextVy: "Справочник", Buttons: domain.ButtonList{{Label: "Открыть", Action: "link:https://example.org"}}}

	var withHuman []max.OutMessage
	withHuman = append(withHuman, introMessage(to), consentMessage(to),
		stepMessage(to, step, e, evID, false, time.Time{}), stepMessage(to, linkOnly, e, evID, false, time.Time{}),
		accessReasonsMessage(to, step, e, &domain.ScheduledEvent{ID: evID}, time.Time{}))
	for _, scale := range []string{domain.Scale10, domain.Scale5, domain.ScaleYesNo, domain.ScaleChoice} {
		sv := domain.ScenarioStep{Kind: domain.KindSurvey, TextVy: "Опрос", Survey: &domain.Survey{Scale: scale, Threshold: 3,
			Questions: []domain.SurveyQuestion{{Text: "Как?", Options: []string{"А", "Б", "В"}}}}}
		for _, screen := range []string{domain.ScreenQuestion, domain.ScreenReasons, domain.ScreenWrite} {
			ev := &domain.ScheduledEvent{ID: evID, Survey: &domain.SurveyState{Screen: screen}}
			withHuman = append(withHuman, surveyMessage(to, sv, e, ev, time.Time{}))
		}
	}
	q := &domain.BotQuestion{UUIDModel: domain.UUIDModel{ID: uuid.New()}, Kind: domain.QuestionAnswer, Answer: "Ответ.",
		Sources: domain.SourceList{{Doc: "ПВТР", ClauseID: "1", Quote: "Цитата один."}, {Doc: "ПВТР", ClauseID: "2", Quote: "Цитата два."}}}
	withHuman = append(withHuman, answerMessage(to, q, false), answerMessage(to, q, true), altMessage(to, q, q.Sources[1]),
		notThatMessage(to, e, q), questionMessage(to, e, &domain.BotQuestion{UUIDModel: q.UUIDModel, Kind: domain.QuestionRouted, Topic: "security"}))
	for i, m := range withHuman {
		if len(m.Buttons) == 0 {
			t.Fatalf("#%d без клавиатуры: %q", i, m.Text)
		}
		last := m.Buttons[len(m.Buttons)-1]
		if len(last) != 1 || last[0].Text != btnHuman {
			t.Errorf("#%d %q: последний ряд %+v", i, m.Text, last)
		}
	}

	// Исключения: передача человеку уже на клавиатуре.
	handoff := func(m max.OutMessage) bool {
		for _, r := range m.Buttons {
			for _, b := range r {
				if b.Text == btnToHR || b.Text == btnToRecruiter || b.Text == btnHuman {
					return true
				}
			}
		}
		return false
	}
	for _, kind := range []string{domain.QuestionAbstain, domain.QuestionRouted, domain.QuestionError} {
		for attempt := 1; attempt <= 3; attempt++ {
			m := questionMessage(to, e, &domain.BotQuestion{UUIDModel: q.UUIDModel, Kind: kind, Attempt: attempt})
			if !handoff(m) {
				t.Errorf("%s/%d без передачи человеку: %v", kind, attempt, labels(m))
			}
		}
	}
	if m := questionMessage(to, e, &domain.BotQuestion{UUIDModel: q.UUIDModel, Kind: domain.QuestionFlag, Flags: domain.StringList{"F7"}}); !handoff(m) {
		t.Errorf("F7 без передачи: %v", labels(m))
	}
}

func TestQuoteFit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Работник обязан соблюдать ПВТР.", "Работник обязан соблюдать ПВТР."},
		{"работник обязан соблюдать ПВТР.", "…работник обязан соблюдать ПВТР."},
		{"  — по истечении шести месяцев", "…— по истечении шести месяцев"},
		{"«работник» обязан", "…«работник» обязан"},
		{"а) соблюдать ПВТР;", "а) соблюдать ПВТР;"},
		{"6.7.8 отпуск предоставляется", "6.7.8 отпуск предоставляется"},
		{"…уже с многоточием", "…уже с многоточием"},
		{"работник *обязан*", `…работник \*обязан\*`},
	}
	for _, c := range cases {
		if got := quoteFit(c.in, 200); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
	if got := quoteFit(strings.Repeat("слово ", 50), 40); !strings.HasPrefix(got, "…слово") || len([]rune(got)) > 40 {
		t.Errorf("обрезка: %q (%d)", got, len([]rune(got)))
	}
}

// «Подробнее» с чанком-обрывком: цитата с «…» в начале, текст дословный.
func TestAskMoreQuoteFragment(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(`{"answer":"Аванс выплачивают 25-го числа.","sources":[{"doc":"Положение об оплате труда","clause_id":"6.1",
		"page":3,"quote":"заработная плата выплачивается не реже чем каждые полмесяца: 25-го и 10-го числа."}],
		"confidence":"high","needs_escalation":false,"topic":"pay","abstain_reason":null}`)
	e.ask("когда аванс?")
	m := lastSent(t, e.fake)
	q := e.question(t, questionID(t, m))
	e.pressOn(qPayload(preQMore, q.ID), q.MessageID)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.Contains(a.Answer.Message.Text, "«…заработная плата выплачивается не реже чем каждые полмесяца: 25-го и 10-го числа.»") {
		t.Fatalf("подробнее %+v", a.Answer.Message)
	}
}

// countOffers — сколько раз бот предложил передать F7 (текст сигнала с именем).
func countOffers(f *max.Fake) int {
	n := 0
	for _, m := range f.Sent() {
		if strings.HasPrefix(m.Text, "Слышу вас, Пётр.") && len(m.Buttons) > 0 {
			n++
		}
	}
	return n
}

func lastOffer(t *testing.T, f *max.Fake) max.OutMessage {
	t.Helper()
	s := f.Sent()
	for i := len(s) - 1; i >= 0; i-- {
		if strings.HasPrefix(s[i].Text, "Слышу вас, Пётр.") && len(s[i].Buttons) > 0 {
			return s[i]
		}
	}
	t.Fatal("нет предложения F7")
	return max.OutMessage{}
}

// F7 regex: пока предложение открыто — второго нет, только напоминание; после «Пока не надо» —
// снова можно; после передачи — ссылка на открытое обращение; через сутки без ответа — снова.
func TestQuitOfferNotRepeated(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	e.ask("думаю уволиться")
	if countOffers(e.fake) != 1 {
		t.Fatalf("первое предложение: %+v", e.fake.Sent())
	}
	e.ask("да, точно уволюсь")
	if countOffers(e.fake) != 1 {
		t.Fatal("предложение F7 повторилось при открытом")
	}
	if got := lastSent(t, e.fake); got.Text != textQuitOfferedVy || len(got.Buttons) != 0 {
		t.Fatalf("напоминание: %+v", got)
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskRed {
		t.Fatalf("риск: %+v", emp)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("в rag ушло %d", n)
	}

	// «Пока не надо» закрывает предложение — следующий сигнал снова предлагает.
	offer := lastOffer(t, e.fake)
	q := e.question(t, questionID(t, offer))
	e.pressOn(offer.Buttons[1][0].Payload, q.MessageID)
	e.ask("всё-таки уволюсь")
	if countOffers(e.fake) != 2 {
		t.Fatal("после «Пока не надо» предложение не повторилось")
	}

	// «Передать рекрутеру» → обращение F7; новый сигнал — ссылка на него, без предложения.
	offer = lastOffer(t, e.fake)
	q = e.question(t, questionID(t, offer))
	e.pressOn(offer.Buttons[0][0].Payload, q.MessageID)
	tk := e.onlyTicket(t, c.Employee.ID)
	e.ask("уволюсь завтра")
	if countOffers(e.fake) != 2 {
		t.Fatal("предложение при открытом обращении F7")
	}
	if got := lastSent(t, e.fake); got.Text != fmt.Sprintf(textQuitTicketVy, tk.Number) {
		t.Fatalf("при обращении: %q", got.Text)
	}
	if n := len(e.tickets(t, c.Employee.ID)); n != 1 {
		t.Fatalf("обращений %d", n)
	}
}

// Неотвеченное предложение живёт сутки.
func TestQuitOfferExpires(t *testing.T) {
	e, _, _ := askEnv(t, time.Second)
	e.ask("думаю уволиться")
	*e.clock = e.clock.Add(23 * time.Hour)
	e.ask("уволюсь")
	if countOffers(e.fake) != 1 {
		t.Fatal("повтор раньше суток")
	}
	*e.clock = e.clock.Add(2 * time.Hour)
	e.ask("уволюсь")
	if countOffers(e.fake) != 2 {
		t.Fatal("через сутки без ответа предложение не повторилось")
	}
}

// LLM-сигнал F7 при открытом предложении: второго предложения нет, напоминания тоже (ответ rag
// уже ушёл).
func TestLLMQuitOfferNotRepeated(t *testing.T) {
	e, _, _ := flagsEnv(t, `{"flags":[{"code":"F7","severity":3,"confidence":0.9}],"sentiment":"neg","quote":""}`)
	e.ask("честно, я уже резюме обновил")
	if countOffers(e.fake) != 1 {
		t.Fatalf("первое предложение: %+v", e.fake.Sent())
	}
	e.ask("смотрю вакансии на hh")
	if countOffers(e.fake) != 1 {
		t.Fatal("LLM повторил предложение F7")
	}
	for _, m := range e.fake.Sent() {
		if m.Text == textQuitOfferedVy {
			t.Fatal("напоминание на LLM-сигнал")
		}
	}
}
