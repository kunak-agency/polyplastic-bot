package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"

	"github.com/google/uuid"
)

// fakeRAG — httptest-подделка rag-сервиса: отдаёт заданный ответ и запоминает запросы. POST
// /flags — отдельно (flagsReply, flagReqs): по умолчанию разметка пустая.
type fakeRAGServer struct {
	srv        *httptest.Server
	mu         sync.Mutex
	reqs       []map[string]any
	reply      string
	delay      time.Duration
	flagsReply string
	flagReqs   []map[string]any
}

func newFakeRAG(t *testing.T) *fakeRAGServer {
	f := &fakeRAGServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		if r.URL.Path == "/flags" {
			f.flagReqs = append(f.flagReqs, body)
			reply := f.flagsReply
			f.mu.Unlock()
			if reply == "" {
				reply = `{"flags":[],"sentiment":"neu","quote":"","degraded":false}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(reply))
			return
		}
		f.reqs = append(f.reqs, body)
		reply, delay := f.reply, f.delay
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(delay):
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRAGServer) set(reply string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reply = reply
}

func (f *fakeRAGServer) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.reqs...)
}

const (
	ragAnswer = `{"answer":"Отпуск можно взять после 6 месяцев работы. Раньше — по соглашению с работодателем.",
		"sources":[{"doc":"ПВТР","clause_id":"6.7.8","page":14,"quote":"Право на использование отпуска за первый год работы возникает у работника по истечении шести месяцев."},
		           {"doc":"ПВТР","clause_id":"6.7.9","page":14,"quote":"Очерёдность предоставления отпусков определяется графиком отпусков."}],
		"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`
	ragAbstain = `{"answer":null,"sources":[],"confidence":"low","needs_escalation":true,"topic":"other","abstain_reason":"no_source"}`
	ragRouted  = `{"answer":null,"sources":[],"confidence":"low","needs_escalation":true,"topic":"hr","abstain_reason":"routed_topic"}`
)

// askEnv — окружение бота с подключённым новичком (на «вы», согласие дано) и подделкой rag.
func askEnv(t *testing.T, timeout time.Duration) (*env, *fakeRAGServer, *NewcomerCard) {
	t.Helper()
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	f := newFakeRAG(t)
	e.svc.Bot.rag = rag.New(f.srv.URL, timeout)
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	e.fake.Reset()
	return e, f, c
}

// ask — вопрос новичка и ожидание фонового ответа.
func (e *env) ask(text string) {
	e.say(text)
	e.svc.Bot.Wait()
}

// pressOn — нажатие кнопки в конкретном сообщении.
func (e *env) pressOn(payload, mid string) {
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCallback, UserID: userID, ChatID: chatID, Payload: payload, CallbackID: "cb-" + payload, MessageID: mid})
}

func lastSent(t *testing.T, f *max.Fake) max.OutMessage {
	t.Helper()
	s := f.Sent()
	if len(s) == 0 {
		t.Fatal("ничего не отправлено")
	}
	return s[len(s)-1]
}

func labels(m max.OutMessage) [][]string {
	var out [][]string
	for _, r := range m.Buttons {
		var row []string
		for _, b := range r {
			row = append(row, b.Text)
		}
		out = append(out, row)
	}
	return out
}

func sameRows(got [][]string, want ...[]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			return false
		}
	}
	return true
}

// questionID — id записи из payload первой callback-кнопки.
func questionID(t *testing.T, m max.OutMessage) uuid.UUID {
	t.Helper()
	p := m.Buttons[0][0].Payload
	id, err := uuid.Parse(strings.Split(p[3:], ":")[0])
	if err != nil {
		t.Fatalf("payload %q", p)
	}
	return id
}

func (e *env) question(t *testing.T, id uuid.UUID) *domain.BotQuestion {
	t.Helper()
	q, err := e.repos.Questions.ByID(context.Background(), id)
	if err != nil || q == nil {
		t.Fatalf("запись %v %v", q, err)
	}
	return q
}

// Ответ с источником, «Подробнее» правит то же сообщение цитатой, 👍 — в аналитику, «Не то»
// дважды — другой пункт, затем «Передать HR?».
func TestAskAnswerWithSource(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(ragAnswer)
	e.ask("когда можно в отпуск?")

	reqs := f.requests()
	if len(reqs) != 1 || reqs[0]["question"] != "когда можно в отпуск?" || reqs[0]["site"] != "GTP" ||
		reqs[0]["address_form"] != "vy" || reqs[0]["trace_id"] == "" || reqs[0]["trace_id"] == nil {
		t.Fatalf("запрос в rag %v", reqs)
	}
	m := lastSent(t, e.fake)
	if !strings.HasPrefix(m.Text, "**Отпуск можно взять после 6 месяцев работы.**\nРаньше — по соглашению с работодателем.") ||
		!strings.HasSuffix(m.Text, "📄 Источник: ПВТР, п. 6.7.8") || m.Format != formatMarkdown {
		t.Fatalf("текст %q", m.Text)
	}
	if !sameRows(labels(m), []string{btnMore}, []string{btnHelpful, btnNotThat}, []string{btnHuman}) {
		t.Fatalf("кнопки %v", labels(m))
	}
	for _, r := range m.Buttons {
		for _, b := range r {
			if len(b.Payload) > 1024 || len(b.Payload) > 45 {
				t.Fatalf("payload %q", b.Payload)
			}
		}
	}
	if tc := e.fake.TypingChats(); len(tc) == 0 || tc[0] != chatID {
		t.Fatalf("«печатает» %v", tc)
	}
	qid := questionID(t, m)
	q := e.question(t, qid)
	if q.Kind != domain.QuestionAnswer || q.MessageID == "" || q.Question != "когда можно в отпуск?" || len(q.Sources) != 2 {
		t.Fatalf("%+v", q)
	}

	// Подробнее — правка того же сообщения через AnswerCallback.
	e.pressOn(qPayload(preQMore, qid), q.MessageID)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.Contains(a.Answer.Message.Text, textQuoteLead) ||
		!strings.Contains(a.Answer.Message.Text, "«Право на использование отпуска за первый год работы возникает у работника по истечении шести месяцев.»") ||
		!strings.Contains(a.Answer.Message.Text, "📄 Источник: ПВТР, п. 6.7.9") {
		t.Fatalf("подробнее %+v", a.Answer.Message)
	}
	if !sameRows(labels(*a.Answer.Message), []string{btnHelpful, btnNotThat}, []string{btnHuman}) {
		t.Fatalf("кнопки после «Подробнее» %v", labels(*a.Answer.Message))
	}

	// 👍 — запись, сообщение не меняется.
	sentBefore := len(e.fake.Sent())
	e.pressOn(qPayload(preQHelpful, qid), q.MessageID)
	if a := lastAnswer(t, e.fake); a.Answer.Message != nil || a.Answer.Notification != "Спасибо! Записал" {
		t.Fatalf("%+v", a)
	}
	if e.question(t, qid).HelpfulAt == nil || len(e.fake.Sent()) != sentBefore {
		t.Fatal("👍 не записан или ушло сообщение")
	}
	e.pressOn(qPayload(preQHelpful, qid), q.MessageID)
	if a := lastAnswer(t, e.fake); a.Answer.Notification != "Уже записал" {
		t.Fatalf("%+v", a)
	}

	// Не то №1 — уточнение с кнопкой другого пункта.
	e.pressOn(qPayload(preQNotThat, qid), q.MessageID)
	nt := lastSent(t, e.fake)
	if nt.Text != textNotThatAltVy || !sameRows(labels(nt), []string{"ПВТР, п. 6.7.9"}, []string{btnRephrase}, []string{btnHuman}) {
		t.Fatalf("не то: %q %v", nt.Text, labels(nt))
	}
	e.pressOn(nt.Buttons[0][0].Payload, "mid.other")
	if alt := lastSent(t, e.fake); !strings.Contains(alt.Text, "«Очерёдность предоставления отпусков определяется графиком отпусков.»") {
		t.Fatalf("другой пункт %q", alt.Text)
	}
	// Не то №2 — «Передать HR?».
	e.pressOn(qPayload(preQNotThat, qid), "mid.other")
	offer := lastSent(t, e.fake)
	if offer.Text != textNotThatOffer || !sameRows(labels(offer), []string{btnToHR, btnNoNeed}) {
		t.Fatalf("offer %q %v", offer.Text, labels(offer))
	}
	e.pressOn(offer.Buttons[0][0].Payload, "mid.offer")
	a = lastAnswer(t, e.fake)
	if a.Answer.Message == nil || a.Answer.Message.Text != textHandedHR || a.Answer.Message.Buttons != nil {
		t.Fatalf("передача %+v", a.Answer.Message)
	}
	if q := e.question(t, qid); q.HumanAt == nil || q.NotThat != 2 || q.Outcome != "handoff" {
		t.Fatalf("%+v", q)
	}
}

// Отказ: шаблон п. 2.5, «Переформулирую» правит сообщение в итог без кнопок; лестница ошибок.
func TestAskAbstainLadder(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(ragAbstain)
	e.ask("сколько стоит парковка у проходной")
	m := lastSent(t, e.fake)
	if m.Text != textAbstain || !sameRows(labels(m), []string{btnToHR, btnRephrase}, []string{btnNoNeed}) {
		t.Fatalf("%q %v", m.Text, labels(m))
	}
	qid := questionID(t, m)
	e.pressOn(qPayload(preQRephrase, qid), e.question(t, qid).MessageID)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || a.Answer.Message.Text != textAbstain+"\n\n"+textRephrase || a.Answer.Message.Buttons != nil {
		t.Fatalf("%+v", a.Answer.Message)
	}

	e.ask("парковка для сотрудников")
	if m := lastSent(t, e.fake); !strings.Contains(m.Text, textAbstainExampleVy) {
		t.Fatalf("второй отказ %q", m.Text)
	}
	e.ask("где ставить машину")
	m = lastSent(t, e.fake)
	if m.Text != textAbstainOffer || !sameRows(labels(m), []string{btnToHR, btnNoNeed}) {
		t.Fatalf("третий отказ %q %v", m.Text, labels(m))
	}
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, questionID(t, m)).MessageID)
	if a := lastAnswer(t, e.fake); a.Answer.Message == nil || !strings.HasSuffix(a.Answer.Message.Text, textHandedHR) {
		t.Fatalf("%+v", a.Answer.Message)
	}
}

// Тематический маршрут: «Это вопрос для человека» + HR / рекрутер.
func TestAskRoutedTopic(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(ragRouted)
	e.ask("сколько получает мой бригадир")
	m := lastSent(t, e.fake)
	if m.Text != textRouted || !sameRows(labels(m), []string{btnToHR, btnToRecruiter}, []string{btnNoNeed}) {
		t.Fatalf("%q %v", m.Text, labels(m))
	}
	qid := questionID(t, m)
	e.pressOn(m.Buttons[0][1].Payload, e.question(t, qid).MessageID)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.HasSuffix(a.Answer.Message.Text, textHandedRecruiter) || a.Answer.Message.Buttons != nil {
		t.Fatalf("%+v", a.Answer.Message)
	}
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, qid).MessageID)
	if a := lastAnswer(t, e.fake); a.Answer.Notification != textHandedAlready {
		t.Fatalf("повторная передача %+v", a)
	}
}

// F7 по regex: в rag не идёт, ответ по правилу трёх шагов, HumanRequest, «Сначала напишу» →
// следующий текст — детали, а не вопрос.
func TestAskFlagNotSentToRAG(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	f.set(ragAnswer)
	e.ask("Хочу уволиться, тяжело")
	if n := len(f.requests()); n != 0 {
		t.Fatalf("в rag ушло %d запросов", n)
	}
	m := lastSent(t, e.fake)
	if !strings.HasPrefix(m.Text, "Слышу вас, Пётр. Спасибо, что сказали прямо") ||
		!sameRows(labels(m), []string{"Передать рекрутеру", "Сначала напишу, что случилось"}, []string{btnNotNow}) {
		t.Fatalf("%q %v", m.Text, labels(m))
	}
	if strings.ContainsAny(m.Text, "🐾🐶✅") {
		t.Fatalf("эмодзи в ветке флага: %q", m.Text)
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskRed {
		t.Fatalf("риск %+v", emp.Risk)
	}
	qid := questionID(t, m)
	if q := e.question(t, qid); q.Kind != domain.QuestionFlag || !q.Flags.Has("F7") {
		t.Fatalf("%+v", q)
	}
	e.pressOn(m.Buttons[0][1].Payload, e.question(t, qid).MessageID)
	if a := lastAnswer(t, e.fake); a.Answer.Message == nil || !strings.HasSuffix(a.Answer.Message.Text, textFlagWriteVy) {
		t.Fatalf("%+v", a.Answer.Message)
	}
	e.ask("мастер ставит в ночь без предупреждения")
	if n := len(f.requests()); n != 0 {
		t.Fatalf("детали ушли в rag: %d", n)
	}
	if d := lastSent(t, e.fake); !strings.HasPrefix(d.Text, textFlagDetails) || !strings.Contains(d.Text, "рекрутеру") {
		t.Fatalf("%q", d.Text)
	}
	if q := e.question(t, qid); q.Details == "" || q.HumanAt == nil || q.AwaitDetails {
		t.Fatalf("%+v", q)
	}
	// Ожидание снято — следующий текст снова вопрос.
	e.ask("когда отпуск")
	if n := len(f.requests()); n != 1 {
		t.Fatalf("запросов в rag %d", n)
	}
}

// Таймаут rag — честное «не могу проверить по документам» и «Передать HR».
func TestAskRAGTimeout(t *testing.T) {
	defer setLateWindow(300 * time.Millisecond)()
	e, f, _ := askEnv(t, 100*time.Millisecond)
	f.set(ragAnswer)
	f.delay = time.Second
	start := time.Now()
	e.ask("когда зарплата")
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Fatalf("ждали %v", d)
	}
	sent := e.fake.Sent()
	if len(sent) < 2 || sent[len(sent)-2].Text != textAskLateVy {
		t.Fatalf("до отказа — «ищу дольше обычного»: %+v", sent)
	}
	m := lastSent(t, e.fake)
	if m.Text != textRAGDown || !sameRows(labels(m), []string{btnToHR, btnNoNeed}) {
		t.Fatalf("%q %v", m.Text, labels(m))
	}
	if q := e.question(t, questionID(t, m)); q.Kind != domain.QuestionError {
		t.Fatalf("%+v", q)
	}
}

// Разметка из документа и имени экранируется; раскрытый ответ не длиннее 4000.
func TestAskEscapingAndLimit(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	long := strings.Repeat("Пункт *важный* _очень_ <b>. ", 300)
	reply, _ := json.Marshal(map[string]any{
		"answer": "Спецодежда *бесплатно* выдаётся_в_день выхода. Подробности [тут].",
		"sources": []map[string]any{
			{"doc": "Положение_о_СИЗ", "clause_id": "3.1", "page": 2, "quote": long},
			{"doc": "ПВТР", "clause_id": "2.2", "page": 1, "quote": long},
		},
		"confidence": "high", "needs_escalation": false, "topic": "hr", "abstain_reason": nil,
	})
	f.set(string(reply))
	e.ask("когда выдают спецодежду")
	m := lastSent(t, e.fake)
	if !strings.HasPrefix(m.Text, `**Спецодежда \*бесплатно\* выдаётся\_в\_день выхода.**`) ||
		!strings.Contains(m.Text, `Подробности \[тут\].`) || !strings.Contains(m.Text, `Положение\_о\_СИЗ`) {
		t.Fatalf("%q", m.Text)
	}
	qid := questionID(t, m)
	e.pressOn(qPayload(preQMore, qid), e.question(t, qid).MessageID)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil {
		t.Fatal("нет правки")
	}
	txt := a.Answer.Message.Text
	if n := utf8.RuneCountInString(txt); n > maxTextLen {
		t.Fatalf("длина %d", n)
	}
	if !strings.Contains(txt, "…»") || !strings.Contains(txt, `\<b\>`) || strings.Contains(txt, " *важный*") {
		t.Fatalf("обрезка/экранирование: %q", txt[len(txt)-200:])
	}
}

func TestEscapeFit(t *testing.T) {
	if got := escapeFit("a*b", 10); got != `a\*b` {
		t.Fatal(got)
	}
	got := escapeFit(strings.Repeat("*", 50), 11)
	if utf8.RuneCountInString(got) > 11 || strings.HasSuffix(strings.TrimSuffix(got, "…"), `\`) {
		t.Fatalf("%q", got)
	}
	got = escapeFit("слово слово слово слово", 15)
	if got != "слово слово…" {
		t.Fatalf("%q", got)
	}
}

func TestSplitLead(t *testing.T) {
	cases := [][3]string{
		{"Первое. Второе.", "Первое.", "Второе."},
		{"Без точки", "Без точки", ""},
		{"Строка\nдальше", "Строка", "дальше"},
		{"По п. 6.7.8 ПВТР отпуск через полгода. Дальше — график.", "По п. 6.7.8 ПВТР отпуск через полгода.", "Дальше — график."},
		{"Т. е. так. Да.", "Т. е. так.", "Да."},
	}
	for _, c := range cases {
		if l, r := splitLead(c[0]); l != c[1] || r != c[2] {
			t.Errorf("%q → %q / %q", c[0], l, r)
		}
	}
}

// setLateWindow — подменить lateAskWindow на время теста.
func setLateWindow(d time.Duration) func() {
	old := lateAskWindow
	lateAskWindow = d
	return func() { lateAskWindow = old }
}

// rag ответил позже RAG_ASK_TIMEOUT, но в окне досылки: «ищу дольше обычного», затем ответ.
func TestAskLateAnswerDelivered(t *testing.T) {
	defer setLateWindow(2 * time.Second)()
	e, f, _ := askEnv(t, 100*time.Millisecond)
	f.set(ragAnswer)
	f.delay = 400 * time.Millisecond
	e.ask("когда отпуск")
	sent := e.fake.Sent()
	if len(sent) != 2 || sent[0].Text != textAskLateVy {
		t.Fatalf("%+v", sent)
	}
	if !strings.Contains(sent[1].Text, "Отпуск можно взять") {
		t.Fatalf("поздний ответ не дослан: %q", sent[1].Text)
	}
	if q := e.question(t, questionID(t, sent[1])); q.Kind != domain.QuestionAnswer {
		t.Fatalf("%+v", q)
	}
}
