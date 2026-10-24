package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/pkg/taxonomy"
)

// LLM-флаги (POST /flags): правила таксономии без базы и сквозные пути на Postgres + подделках
// MAX и rag. Тексты и разметка синтетические.

func testTax(t *testing.T) *taxonomy.Taxonomy {
	t.Helper()
	tx, err := taxonomy.LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func acts(ds []flagDecision) string {
	var s []string
	for _, d := range ds {
		s = append(s, d.code+":"+d.act)
	}
	return strings.Join(s, ",")
}

func TestDecideFlagsThresholds(t *testing.T) {
	tx := testTax(t)
	cases := []struct {
		name string
		in   []rag.Flag
		prev domain.StringList
		want string
	}{
		{"выше порога F4 → обращение", []rag.Flag{{Code: "F4", Severity: 2, Confidence: 0.85}}, nil, "F4:ticket"},
		{"F7 → предложение, не обращение", []rag.Flag{{Code: "F7", Severity: 3, Confidence: 0.9}}, nil, "F7:offer"},
		{"F7 severity 1 — критический код, всё равно предложение", []rag.Flag{{Code: "F7", Severity: 1, Confidence: 0.6}}, nil, "F7:offer"},
		{"уверенность ниже порога категории → риск", []rag.Flag{{Code: "F6", Severity: 2, Confidence: 0.5}}, nil, "F6:risk"},
		{"severity 1 некритичного → риск", []rag.Flag{{Code: "F1", Severity: 1, Confidence: 0.9}}, nil, "F1:risk"},
		{"ниже manual_review_floor → ничего", []rag.Flag{{Code: "F2", Severity: 2, Confidence: 0.35}}, nil, ""},
		{"F9 порог 0.4", []rag.Flag{{Code: "F9", Severity: 3, Confidence: 0.45}}, nil, "F9:ticket"},
		{"повтор в пульсе: severity 1 → 2 → обращение", []rag.Flag{{Code: "F1", Severity: 1, Confidence: 0.9}}, domain.StringList{"F1"}, "F1:ticket"},
		{"неизвестный код отброшен", []rag.Flag{{Code: "F42", Severity: 3, Confidence: 0.9}}, nil, ""},
		{"обращения раньше риска", []rag.Flag{{Code: "F6", Severity: 1, Confidence: 0.9}, {Code: "F2", Severity: 2, Confidence: 0.9}}, nil, "F2:ticket,F6:risk"},
	}
	for _, c := range cases {
		if got := acts(decideFlags(tx, c.in, c.prev)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	d := decideFlags(tx, []rag.Flag{{Code: "F2", Severity: 3, Confidence: 0.9}}, domain.StringList{"F2"})
	if d[0].severity != 3 || !d[0].repeated {
		t.Fatalf("повтор не выше 3: %+v", d[0])
	}
}

// flagsEnv — новичок на «вы» с согласием, подделка rag с заданной разметкой /flags.
func flagsEnv(t *testing.T, flagsReply string) (*env, *fakeRAGServer, *NewcomerCard) {
	t.Helper()
	e, f, c := askEnv(t, time.Second)
	f.set(ragAbstain)
	f.mu.Lock()
	f.flagsReply = flagsReply
	f.mu.Unlock()
	return e, f, c
}

func (f *fakeRAGServer) flagRequests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.flagReqs...)
}

// Выше порога — обращение категории по маршруту таксономии, ответ rag тоже уходит.
func TestLLMFlagAboveThresholdOpensTicket(t *testing.T) {
	e, f, c := flagsEnv(t, `{"flags":[{"code":"F4","severity":2,"confidence":0.85}],"sentiment":"neg","quote":"наставник ничего не объясняет"}`)
	e.withChats()
	e.ask("наставник ничего не объясняет, говорит разбирайся сам")
	if n := len(f.requests()); n != 1 {
		t.Fatalf("в rag /ask %d", n)
	}
	fr := f.flagRequests()
	if len(fr) != 1 || fr[0]["context"] != "free_text" || fr[0]["text"] != "наставник ничего не объясняет, говорит разбирайся сам" {
		t.Fatalf("/flags %v", fr)
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F4" || tk.Source != domain.TicketFromFlag || tk.NotifyManager || !strings.Contains(tk.Summary, "LLM") {
		t.Fatalf("F4 %+v", tk)
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskRed {
		t.Fatalf("риск %+v", emp)
	}
	var open string
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "Обращение #") {
			open = m.Text
		}
	}
	if !strings.Contains(open, "Понял: с наставником или коллегами сейчас непросто.") || !strings.Contains(open, "HR-партнёру") {
		t.Fatalf("OPEN %q", open)
	}
	if len(sentTo(e.fake, hrChatID)) != 1 {
		t.Fatal("карточка F4 не в HR-чате")
	}
}

// Regex в приоритете: эскалация по нему, /ask не зовётся; /flags — в фоне (v2), но новые коды
// модели повторной эскалации не дают: F4 выше порога — только риск, обращения нет.
func TestLLMFlagsRegexFiresModelOnlyEnriches(t *testing.T) {
	e, f, c := flagsEnv(t, `{"flags":[{"code":"F4","severity":3,"confidence":0.9}],"sentiment":"neu","tone_confidence":0.7,"quote":"думаю уволиться","summary":"Новичок думает об увольнении.","clarify_hint":null}`)
	e.ask("думаю уволиться")
	e.svc.Bot.Wait()
	if n := len(f.flagRequests()); n != 1 {
		t.Fatalf("/flags при regex: %d", n)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("в rag /ask %d", n)
	}
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 {
		t.Fatalf("повторная эскалация: обращений %d", n)
	}
	offers := 0
	for _, m := range e.fake.Sent() {
		if strings.HasPrefix(m.Text, "Слышу вас") {
			offers++
		}
	}
	if offers != 1 {
		t.Fatalf("предложений F7 %d", offers)
	}
	qs, _ := e.repos.Questions.Recent(context.Background(), c.Employee.ID, time.Time{}, 5)
	if len(qs) != 1 || qs[0].Kind != domain.QuestionFlag || qs[0].Sentiment == nil || *qs[0].Sentiment != domain.SentimentNeu ||
		qs[0].Summary == nil || *qs[0].Summary != "Новичок думает об увольнении." || qs[0].Quote == nil ||
		qs[0].ToneConfidence == nil || *qs[0].ToneConfidence != 0.7 {
		t.Fatalf("запись сигнала: %+v", qs)
	}
}

// F7 из LLM — предложение новичку с выбором, обращения без спроса нет.
func TestLLMFlagF7OffersWithoutTicket(t *testing.T) {
	e, _, c := flagsEnv(t, `{"flags":[{"code":"F7","severity":3,"confidence":0.9}],"sentiment":"neg","quote":"резюме обновил"}`)
	e.ask("честно, я уже резюме обновил")
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 {
		t.Fatalf("F7 без спроса создал обращение: %d", n)
	}
	var offer *string
	for _, m := range e.fake.Sent() {
		if strings.HasPrefix(m.Text, "Слышу вас") && len(m.Buttons) > 0 && strings.Join(labels(m)[0], "|") == "Передать рекрутеру|Сначала напишу, что случилось" {
			s := m.Text
			offer = &s
			q := e.question(t, questionID(t, m))
			if q.Kind != domain.QuestionFlag || !q.Flags.Has("F7") || q.TicketID != nil {
				t.Fatalf("запись сигнала %+v", q)
			}
			// Согласие новичка → обращение F7 рекрутеру.
			e.pressOn(m.Buttons[0][0].Payload, q.MessageID)
		}
	}
	if offer == nil {
		t.Fatalf("нет предложения F7: %+v", e.fake.Sent())
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F7" || tk.Route != taxonomy.RouteRecruiter {
		t.Fatalf("F7 после согласия %+v", tk)
	}
}

// Ниже порога — только отметка риска (жёлтая зона, код и категория в причине).
func TestLLMFlagBelowThresholdMarksRisk(t *testing.T) {
	e, _, c := flagsEnv(t, `{"flags":[{"code":"F6","severity":2,"confidence":0.5}],"sentiment":"neg","quote":""}`)
	e.ask("устаю, но вроде справляюсь")
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 {
		t.Fatalf("обращение ниже порога: %d", n)
	}
	emp := e.employee(t, c.Employee.ID)
	if emp.Risk != domain.RiskYellow || emp.RiskReason == nil || !strings.Contains(*emp.RiskReason, "F6 «Перегрузка, выгорание»") {
		t.Fatalf("риск %v %v", emp.Risk, deref(emp.RiskReason))
	}
}

// degraded и ошибка /flags — ничего не меняют.
func TestLLMFlagDegradedIgnored(t *testing.T) {
	e, _, c := flagsEnv(t, `{"flags":[],"sentiment":"neu","quote":"","degraded":true}`)
	e.ask("когда аванс")
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskGreen && emp.Risk != "" {
		t.Fatalf("риск при degraded: %v", emp.Risk)
	}
}

// Комментарий к опросу: контекст pulse_dN и вопрос опроса; повтор флага во втором пульсе подряд —
// severity +1 и обращение.
func TestLLMFlagRepeatInPulses(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 18, 30)) // D0 вечер: пульс «Итог дня» уже ушёл
	f := newFakeRAG(t)
	e.svc.Bot.rag = rag.New(f.srv.URL, time.Second)
	f.flagsReply = `{"flags":[{"code":"F1","severity":1,"confidence":0.9}],"sentiment":"neg","quote":""}`
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()
	// вопрос 10:00 держит поток блока (api_contract v4): ответ — и пульс 18:00 уходит
	first := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	e.press(preStepButton + first.ID.String() + ":0")

	pulse := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	id := pulse.ID.String()
	e.press(preAnswer + id + ":0:2") // 2 < 3 → причины
	e.press(preWrite + id + ":0")
	e.say("пока не понимаю, что от меня хотят")
	e.svc.Bot.Wait()
	fr := f.flagRequests()
	if len(fr) != 1 || fr[0]["context"] != "pulse_d0" || fr[0]["survey_question"] != "Как прошёл день?" {
		t.Fatalf("/flags %v", fr)
	}
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 {
		t.Fatalf("первый пульс F1 sev 1 — без обращения: %d", n)
	}
	answers, _ := e.repos.Events.Answers(ctx, pulse.ID)
	if len(answers) != 1 || !answers[0].Flags.Has("F1") {
		t.Fatalf("код в ответе опроса: %+v", answers)
	}

	if _, err := e.svc.Newcomers.TimeTravel(ctx, c.Employee.ID, 14); err != nil {
		t.Fatal(err)
	}
	check := eventByTitle(t, e.events(t, c.Employee.ID), "Чек двух недель")
	id = check.ID.String()
	e.press(preAnswer + id + ":0:5")
	e.press(preWrite + id + ":0")
	e.say("задачи так и не ставят")
	e.svc.Bot.Wait()
	fr = f.flagRequests()
	if len(fr) != 2 || fr[1]["context"] != "pulse_d14" || fr[1]["survey_question"] != "Задачи понятны?" {
		t.Fatalf("/flags %v", fr)
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F1" || !strings.Contains(tk.Summary, "повтор") {
		t.Fatalf("повтор F1 %+v", tk)
	}
}

// Подпись и заглушка исполнителя по роли: F4 — HR-партнёр, F5/F9 — СБ, F7 — рекрутер.
func TestStaffRoleByCategory(t *testing.T) {
	tx := testTax(t)
	want := map[string][2]string{
		"F2": {"HR", "команда HR"},
		"F4": {"HR-партнёр", "HR-партнёр"},
		"F5": {"HR-партнёр", "HR-партнёр"}, // как в таксономии и на фронте; карточка F5 всё равно уходит в чат СБ
		"F9": {"служба безопасности", "служба безопасности"},
		"F7": {"рекрутер", "рекрутер"},
	}
	vy := domain.AddressVy
	e := &domain.Employee{AddressForm: &vy}
	for code, w := range want {
		cat, _ := tx.Get(code)
		if staffRole(cat) != w[0] || staffPlaceholder(cat) != w[1] {
			t.Errorf("%s: %q / %q", code, staffRole(cat), staffPlaceholder(cat))
		}
		tk := &domain.Ticket{Number: 1001}
		if got := ticketAssignedText(e, tk, cat); !strings.Contains(got, "занимается "+w[1]+".") {
			t.Errorf("%s ASSIGNED без исполнителя: %q", code, got)
		}
		name := "Анна"
		tk.AssigneeName = &name
		if got := staffSignature(tk, cat); got != "**Анна, "+w[0]+"** (по обращению #1001):" {
			t.Errorf("%s подпись: %q", code, got)
		}
	}
}
