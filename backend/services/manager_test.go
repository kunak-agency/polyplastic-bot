package services

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/repositories"

	"github.com/google/uuid"
)

// Руководитель подразделения (кейс, роль 03): правила push, видимость категорий, «Мои новички»,
// вход по ссылке mgr_<token> и кнопки «Встретился» / «Запланирую».

// lowPulses — два опроса подряд со средним 3 (низкий пульс), с флагами flags у второго.
func lowPulses(rows *repositories.OverviewRows, emp uuid.UUID, flags ...string) {
	for i, at := range []time.Time{ovNow.Add(-72 * time.Hour), ovNow.Add(-24 * time.Hour)} {
		a := ovAnswer(emp, domain.Scale10, 3, at)
		if i == 1 {
			a.Flags = flags
		}
		rows.Answers = append(rows.Answers, a)
	}
}

func kinds(cs []NudgeCandidate, emp uuid.UUID) map[string]NudgeCandidate {
	out := map[string]NudgeCandidate{}
	for _, c := range cs {
		if c.Employee.ID == emp {
			out[c.Kind] = c
		}
	}
	return out
}

var categoryLeak = regexp.MustCompile(`\bF\d{1,2}\b`)

func TestManagerNudgeRules(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}

	low := ovEmp("Низкий Пульс", domain.SiteGTP, 12)
	lowPulses(rows, low.ID)
	hidden := ovEmp("Скрытый Сигнал", domain.SiteGTP, 12) // низкий пульс + F5 → повода нет
	lowPulses(rows, hidden.ID, "F5")
	f4 := ovEmp("Наставник Конфликт", domain.SiteGTP, 12)
	lowPulses(rows, f4.ID, "F4")
	f6 := ovEmp("Перегрузка Видна", domain.SiteGTP, 12) // F6 руководителю видна — повод есть
	lowPulses(rows, f6.ID, "F6")

	silent := ovEmp("Молчун Тихий", domain.SiteGrPP, 15)
	sent4d := ovNow.Add(-4 * 24 * time.Hour)
	sent2d := ovNow.Add(-2 * 24 * time.Hour)
	rows.Events = append(rows.Events,
		repositories.OverviewEvent{ID: newID(), EmployeeID: silent.ID, Kind: domain.KindSurvey, Status: domain.EventSent, SentAt: &sent4d},
	)
	fresh := ovEmp("Свежий Опрос", domain.SiteGrPP, 15)
	rows.Events = append(rows.Events,
		repositories.OverviewEvent{ID: newID(), EmployeeID: fresh.ID, Kind: domain.KindSurvey, Status: domain.EventSent, SentAt: &sent2d},
	)
	stopped := ovEmp("Отключил Бота", domain.SiteGrPP, 15)
	stopped.Connection = domain.ConnStopped
	rows.Events = append(rows.Events,
		repositories.OverviewEvent{ID: newID(), EmployeeID: stopped.ID, Kind: domain.KindSurvey, Status: domain.EventSent, SentAt: &sent4d},
	)

	probation := ovEmp("Испытательный Срок", domain.SiteLuzhniki, 87)
	month := ovEmp("Месяц Работы", domain.SiteLuzhniki, 30)
	done := ovEmp("Уже Прошёл", domain.SiteLuzhniki, 120)
	rows.Employees = append(rows.Employees, low, hidden, f4, f6, silent, fresh, stopped, probation, month, done)

	cs := ManagerNudgeCandidates(rows, ovNow, tax)
	if _, ok := kinds(cs, low.ID)[domain.NudgeLowPulseTwice]; !ok {
		t.Fatal("второй низкий пульс — нет повода")
	}
	for _, e := range []domain.Employee{hidden, f4} {
		if len(kinds(cs, e.ID)) != 0 {
			t.Fatalf("%s: скрытая категория дала повод руководителю", e.FullName)
		}
	}
	if _, ok := kinds(cs, f6.ID)[domain.NudgeLowPulseTwice]; !ok {
		t.Fatal("F6 (видна руководителю) — повод должен быть")
	}
	if _, ok := kinds(cs, silent.ID)[domain.NudgeSilent]; !ok {
		t.Fatal("молчит 4 дня — нет повода")
	}
	if len(kinds(cs, fresh.ID)) != 0 || len(kinds(cs, stopped.ID)) != 0 {
		t.Fatal("2 дня или отключил бота — повода нет")
	}
	p, ok := kinds(cs, probation.ID)[domain.NudgeProbation]
	if !ok || !strings.Contains(p.Text, "через 3 дня") {
		t.Fatalf("решение по ИС через 3 дня: %+v", p)
	}
	m, ok := kinds(cs, month.ID)[domain.NudgeMonth]
	if !ok || !strings.Contains(m.Text, "1:1") {
		t.Fatalf("D+30: %+v", m)
	}
	if len(kinds(cs, done.ID)) != 0 {
		t.Fatal("после D+90 поводов нет")
	}
	for _, c := range cs {
		if categoryLeak.MatchString(c.Text) || categoryLeak.MatchString(c.Reason) {
			t.Fatalf("код категории в push: %q", c.Text)
		}
		for _, code := range []string{"F0", "F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"} {
			if cat, ok := tax.Get(code); ok && (strings.Contains(c.Text, cat.Name) || strings.Contains(c.Reason, cat.Name)) {
				t.Fatalf("категория %s в push: %q", code, c.Text)
			}
		}
	}
	// Ключи стабильны: повторный расчёт — те же ключи (идемпотентность).
	again := ManagerNudgeCandidates(rows, ovNow.Add(time.Hour), tax)
	keys := map[string]bool{}
	for _, c := range cs {
		keys[c.Key] = true
	}
	for _, c := range again {
		if !keys[c.Key] {
			t.Fatalf("новый ключ при повторе: %s", c.Key)
		}
	}
}

func TestManagerViewVisibility(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	e := ovEmp("Видимый Новичок", domain.SiteGTP, 12)
	e.ManagerName = "Руководитель Цеха"
	reason := "обращение «Грубость, унижение, харассмент»"
	e.Risk, e.RiskReason = domain.RiskRed, &reason
	lowPulses(rows, e.ID)
	rows.Answers[len(rows.Answers)-1].Flags = []string{"F6"}
	rows.Employees = append(rows.Employees, e)
	rows.Tickets = append(rows.Tickets, repositories.OverviewTicket{ID: newID(), EmployeeID: e.ID, Category: "F5", Status: domain.TicketOpen})

	nudge := domain.ManagerNudge{UUIDModel: domain.UUIDModel{ID: newID()}, EmployeeID: e.ID, Kind: domain.NudgeLowPulseTwice, Status: domain.NudgePlanned, Text: "повод"}
	met := domain.ManagerNudge{UUIDModel: domain.UUIDModel{ID: newID()}, EmployeeID: e.ID, Kind: domain.NudgeMonth, Status: domain.NudgeMet}
	v := BuildManagerView(rows, []domain.ManagerNudge{nudge, met}, e.ManagerName, ovNow, tax)
	if len(v.Items) != 1 {
		t.Fatalf("карточек %d", len(v.Items))
	}
	c := v.Items[0]
	if len(c.Meetings) != 1 || c.Meetings[0].Status != domain.NudgePlanned {
		t.Fatalf("незакрытые встречи: %+v", c.Meetings)
	}
	if len(c.Attention) == 0 {
		t.Fatal("повод «второй низкий пульс» не показан")
	}
	b, _ := json.Marshal(v)
	s := string(b)
	for _, leak := range []string{"Грубость", "F5", "F6", "харассмент", "Перегрузка", "обращение"} {
		if strings.Contains(s, leak) {
			t.Fatalf("руководителю видно %q: %s", leak, s)
		}
	}
}

// TestManagerBindAndPush — интеграционный: ссылка руководителя, привязка, push на подделке,
// «Запланирую» и «Встретился».
func TestManagerBindAndPush(t *testing.T) {
	moscow := ovMoscow
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, moscow) // понедельник, рабочее время
	env := newEnv(t, now)
	ctx := context.Background()
	if err := env.repos.Manager().TruncateManager(ctx); err != nil {
		t.Fatal(err)
	}
	start := civilDate(now, moscow).AddDate(0, 0, -30) // сегодня D+30
	c := env.launch(t, time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC))
	e := env.employee(t, c.Employee.ID)
	e.ManagerName = "Руководитель Тестового Цеха"
	if err := env.repos.Employees.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	// Второй новичок того же руководителя — привяжется той же ссылкой.
	c2 := env.launch(t, time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 10))
	e2 := env.employee(t, c2.Employee.ID)
	e2.ManagerName = e.ManagerName
	if err := env.repos.Employees.Save(ctx, e2); err != nil {
		t.Fatal(err)
	}

	mgr := env.svc.Manager()
	inv, err := mgr.Invite(ctx, e.ID)
	if err != nil || !strings.Contains(inv.Link, "start=mgr_") || inv.Connected {
		t.Fatalf("ссылка: %+v %v", inv, err)
	}
	again, _ := mgr.Invite(ctx, e2.ID)
	if again.Link != inv.Link {
		t.Fatal("для того же руководителя — та же действующая ссылка")
	}

	// Без привязки push только в лог, задача создаётся.
	const mgrUser = int64(777001)
	payload := inv.Link[strings.Index(inv.Link, "mgr_"):]
	env.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: mgrUser, ChatID: mgrUser, Payload: payload})
	if got := env.employee(t, e2.ID); got.ManagerUserID == nil || *got.ManagerUserID != mgrUser {
		t.Fatalf("второй новичок не привязан: %v", got.ManagerUserID)
	}
	welcome := env.fake.Sent()
	if len(welcome) == 0 || !strings.Contains(welcome[len(welcome)-1].Text, "руководителя") {
		t.Fatalf("приветствие руководителю: %+v", welcome)
	}
	// Чужой аккаунт той же ссылкой — «не сработала».
	env.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: 777002, ChatID: 777002, Payload: payload})
	if s := env.fake.Sent(); !strings.Contains(s[len(s)-1].Text, "не сработала") {
		t.Fatalf("чужой аккаунт: %q", s[len(s)-1].Text)
	}

	env.fake.Reset()
	sent, err := mgr.Tick(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var push *SentNudge
	for i := range sent {
		if sent[i].NewcomerID == e.ID && sent[i].Kind == domain.NudgeMonth {
			push = &sent[i]
		}
	}
	if push == nil || !push.Delivered {
		t.Fatalf("push D+30 не ушёл: %+v", sent)
	}
	msgs := env.fake.Sent()
	var out *max.OutMessage
	for i := range msgs {
		if msgs[i].UserID == mgrUser && strings.Contains(msgs[i].Text, "1:1") {
			out = &msgs[i]
		}
	}
	if out == nil || len(out.Buttons) != 2 || out.Buttons[0][0].Text != "Встретился" || out.Buttons[0][1].Text != "Запланирую" || out.Buttons[1][0].URL == "" {
		t.Fatalf("push и кнопки: %+v", out)
	}
	if again, _ := mgr.Tick(ctx, false); len(again) != 0 {
		t.Fatalf("повторный такт прислал снова: %+v", again)
	}

	press := func(p string) {
		env.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateMessageCallback, UserID: mgrUser, ChatID: mgrUser, Payload: p, CallbackID: "cb-" + p})
	}
	press(payMgrPlan + push.NudgeID.String())
	v, err := mgr.View(ctx, e.ManagerName)
	if err != nil || !v.Connected {
		t.Fatalf("вид руководителя: %+v %v", v, err)
	}
	open := 0
	for _, it := range v.Items {
		open += len(it.Meetings)
	}
	if open == 0 {
		t.Fatal("«Запланирую» — задача остаётся открытой")
	}
	press(payMgrMet + push.NudgeID.String())
	n, _ := env.repos.Manager().Nudge(ctx, push.NudgeID)
	if n.Status != domain.NudgeMet || n.AnsweredAt == nil {
		t.Fatalf("«Встретился»: %+v", n)
	}
	// Чужой user_id кнопку не нажмёт.
	env.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateMessageCallback, UserID: 1, ChatID: 1, Payload: payMgrPlan + push.NudgeID.String(), CallbackID: "x"})
	if n2, _ := env.repos.Manager().Nudge(ctx, push.NudgeID); n2.Status != domain.NudgeMet {
		t.Fatal("чужое нажатие изменило задачу")
	}
}
