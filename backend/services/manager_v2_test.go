package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/repositories"

	"github.com/google/uuid"
)

// Push руководителю v2 (FEATURE_RISK_V2): плановые поводы всем, нейтральный повод вместо
// скрытого, D+87 с решением по ИС, D+1 «план», лимиты; утренняя сводка рекрутеру.

func TestManagerNudgeRulesV2(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	hidden := ovEmp("Скрытый Сигнал", domain.SiteGTP, 12) // последний низкий пульс сутки назад
	lowPulses(rows, hidden.ID, "F5")
	hiddenFresh := ovEmp("Скрытый Свежий", domain.SiteGTP, 12) // последний — 2 часа назад
	for _, at := range []time.Time{ovNow.Add(-72 * time.Hour), ovNow.Add(-2 * time.Hour)} {
		rows.Answers = append(rows.Answers, ovAnswer(hiddenFresh.ID, domain.Scale10, 3, at, "F9"))
	}
	d14 := ovEmp("Две Недели", domain.SiteGTP, 14)
	d87 := ovEmp("Конец ИС", domain.SiteGTP, 87)
	decided := ovEmp("Решено", domain.SiteGTP, 87)
	keep := domain.IPKeep
	decided.IPDecision = &keep
	d1 := ovEmp("Вышел Вчера", domain.SiteGTP, 1)
	d1plan := ovEmp("План Есть", domain.SiteGTP, 1)
	rows.Employees = append(rows.Employees, hidden, hiddenFresh, d14, d87, decided, d1, d1plan)

	// Без v2 — прежнее поведение: скрытый сигнал молчит, D+14 и D+1 поводов нет.
	v1 := ManagerNudgeCandidates(rows, ovNow, tax)
	for _, e := range []domain.Employee{hidden, hiddenFresh, d14, d1, d1plan} {
		if len(kinds(v1, e.ID)) != 0 {
			t.Fatalf("v1: %s получил повод %+v", e.FullName, kinds(v1, e.ID))
		}
	}
	if p := kinds(v1, d87.ID)[domain.NudgeProbation]; !strings.Contains(p.Text, "решение по испытательному сроку через 3 дня") {
		t.Fatalf("v1 D+87: %q", p.Text)
	}

	cs := ManagerNudgeCandidates(rows, ovNow, tax, NudgeOptions{V2: true, WithPlan: map[uuid.UUID]bool{d1plan.ID: true}})
	n, ok := kinds(cs, hidden.ID)[domain.NudgeRegular]
	if !ok || len(kinds(cs, hidden.ID)) != 1 {
		t.Fatalf("скрытый сигнал: нужен ровно нейтральный повод, есть %+v", kinds(cs, hidden.ID))
	}
	if len(kinds(cs, hiddenFresh.ID)) != 0 {
		t.Fatal("нейтральный повод раньше суток после пульса")
	}
	r, ok := kinds(cs, d14.ID)[domain.NudgeRegular]
	if !ok {
		t.Fatal("D+14 — нет плановой встречи")
	}
	// По тексту нейтральный повод не отличить от планового D+14.
	strip := func(c NudgeCandidate) string {
		return strings.Replace(strings.Replace(c.Text, c.Employee.FullName, "", 1), dayRu(c.Day), "", 1)
	}
	if strip(n) != strip(r) || n.Reason != r.Reason {
		t.Fatalf("нейтральный %q и плановый %q различаются", n.Text, r.Text)
	}
	p, ok := kinds(cs, d87.ID)[domain.NudgeProbation]
	if !ok || p.Text != "Через 3 дня заканчивается ИС у Конец ИС. Какое решение?" {
		t.Fatalf("D+87: %+v", p)
	}
	if len(kinds(cs, decided.ID)) != 0 {
		t.Fatal("решение уже записано — повода D+87 нет")
	}
	pl, ok := kinds(cs, d1.ID)[domain.NudgePlanNeeded]
	if !ok || !strings.Contains(pl.Text, "Вышел Вчера вышел вчера. Приложите план адаптации") {
		t.Fatalf("D+1: %+v", pl)
	}
	if len(kinds(cs, d1plan.ID)) != 0 {
		t.Fatal("план приложен — повода D+1 нет")
	}
	for _, c := range cs {
		if categoryLeak.MatchString(c.Text) || categoryLeak.MatchString(c.Reason) {
			t.Fatalf("код категории в push: %q", c.Text)
		}
	}
}

func TestNudgeLimiter(t *testing.T) {
	e := ovEmp("Новичок", domain.SiteGTP, 20)
	e.ManagerName = "Руководитель"
	c := func(kind string) NudgeCandidate { return NudgeCandidate{Employee: e, Kind: kind} }
	l := &nudgeLimiter{day: map[string]int{}, week: map[string]int{"Руководитель": 1}}
	if !l.allow(c(domain.NudgeRegular)) {
		t.Fatal("первый за день, второй за неделю — можно")
	}
	l.take(c(domain.NudgeRegular))
	if l.allow(c(domain.NudgeProbation)) {
		t.Fatal("второй за день — нельзя даже D+87")
	}
	l.day = map[string]int{}
	if l.allow(c(domain.NudgeMonth)) {
		t.Fatal("третий повод 1:1 за неделю — нельзя")
	}
	if !l.allow(c(domain.NudgeProbation)) || !l.allow(c(domain.NudgePlanNeeded)) {
		t.Fatal("D+87 и D+1 недельный лимит не ограничивает")
	}
}

// TestManagerIPDecisionPush — интеграционный: D+87 и D+1 одному руководителю — за день уходит
// один (D+87), кнопки решения, «Оставить» пишет ip_decision; «Обсудить с HR» — задача HR.
func TestManagerIPDecisionPush(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, ovMoscow) // понедельник
	env := newEnv(t, now)
	env.svc.features.RiskV2 = true
	ctx := context.Background()
	if err := env.repos.Manager().TruncateManager(ctx); err != nil {
		t.Fatal(err)
	}
	day := func(n int) time.Time {
		d := civilDate(now, ovMoscow).AddDate(0, 0, -n)
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	}
	bind := func(start time.Time, manager string, uid int64) *domain.Employee {
		c := env.launch(t, start)
		e := env.employee(t, c.Employee.ID)
		e.ManagerName, e.ManagerUserID = manager, &uid
		if err := env.repos.Employees.Save(ctx, e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	const mgrA, mgrB = int64(778001), int64(778002)
	ip := bind(day(87), "Руководитель А", mgrA)
	bind(day(1), "Руководитель А", mgrA) // D+1 без плана — тому же руководителю
	ip2 := bind(day(87), "Руководитель Б", mgrB)

	env.fake.Reset()
	sent, err := env.svc.Manager().Tick(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	perMgr := map[string][]string{}
	for _, s := range sent {
		perMgr[s.Manager] = append(perMgr[s.Manager], s.Kind)
	}
	if got := perMgr["Руководитель А"]; len(got) != 1 || got[0] != domain.NudgeProbation {
		t.Fatalf("лимит 1 в день и приоритет D+87: %v", got)
	}
	var push *max.OutMessage
	for _, m := range env.fake.Sent() {
		if m.UserID == mgrA {
			m := m
			push = &m
		}
	}
	if push == nil || !strings.HasPrefix(push.Text, "Через 3 дня заканчивается ИС у") || len(push.Buttons) != 2 ||
		push.Buttons[0][0].Text != "Оставить" || push.Buttons[0][1].Text != "Продлить" || push.Buttons[1][0].Text != "Обсудить с HR" {
		t.Fatalf("push D+87: %+v", push)
	}
	press := func(uid int64, p string) {
		env.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateMessageCallback, UserID: uid, ChatID: uid, Payload: p, CallbackID: "cb-" + p})
	}
	press(mgrB, push.Buttons[0][0].Payload) // чужая кнопка
	if e := env.employee(t, ip.ID); e.IPDecision != nil {
		t.Fatal("чужой руководитель записал решение")
	}
	press(mgrA, push.Buttons[0][0].Payload)
	e := env.employee(t, ip.ID)
	if e.IPDecision == nil || *e.IPDecision != domain.IPKeep || e.IPDecisionAt == nil || e.IPDecisionBy == nil ||
		*e.IPDecisionBy != "руководитель Руководитель А (MAX)" {
		t.Fatalf("решение «Оставить»: %v %v %v", e.IPDecision, e.IPDecisionAt, e.IPDecisionBy)
	}
	press(mgrA, push.Buttons[0][1].Payload) // повторно «Продлить» — не перезаписывает
	if e := env.employee(t, ip.ID); *e.IPDecision != domain.IPKeep {
		t.Fatal("кнопка перезаписала решение")
	}
	// Руководитель Б: «Обсудить с HR» — решения нет, задача HR есть.
	var pushB *max.OutMessage
	for _, m := range env.fake.Sent() {
		if m.UserID == mgrB && len(m.Buttons) == 2 {
			m := m
			pushB = &m
		}
	}
	if pushB == nil {
		t.Fatal("push руководителю Б не ушёл")
	}
	press(mgrB, pushB.Buttons[1][0].Payload)
	if e := env.employee(t, ip2.ID); e.IPDecision != nil {
		t.Fatal("«Обсудить» записал решение")
	}
	list, _ := env.repos.Manager().NudgesOf(ctx, []uuid.UUID{ip2.ID})
	found := false
	for _, n := range list {
		if n.Kind == domain.NudgeIPDiscuss && n.Status == domain.NudgeOpen {
			found = true
		}
	}
	if !found {
		t.Fatalf("задача HR «обсудить» не создана: %+v", list)
	}
	// Повторный такт в тот же день — ничего (D+1 ждёт завтра по лимиту).
	if again, _ := env.svc.Manager().Tick(ctx, false); len(again) != 0 {
		t.Fatalf("повтор в тот же день: %+v", again)
	}

	// Кабинет: решение по ИС — валидация и запись с автором.
	bad := "maybe"
	if _, err := env.svc.Newcomers.SetIPDecision(ctx, ip2.ID, &bad); err == nil {
		t.Fatal("недопустимое решение принято")
	}
	part := domain.IPPart
	card, err := env.svc.Newcomers.SetIPDecision(WithViewer(ctx, &domain.User{FullName: "Рекрутер Тестовый"}), ip2.ID, &part)
	if err != nil || card.Employee.IPDecision == nil || *card.Employee.IPDecision != domain.IPPart || *card.Employee.IPDecisionBy != "Рекрутер Тестовый" {
		t.Fatalf("решение из кабинета: %+v %v", card, err)
	}
	if card, err = env.svc.Newcomers.SetIPDecision(ctx, ip2.ID, nil); err != nil || card.Employee.IPDecision != nil {
		t.Fatalf("сброс решения: %v", err)
	}
}

func TestRecruiterMorningText(t *testing.T) {
	z := func(reason string) RiskZone { return RiskZone{Zone: domain.RiskRed, Reasons: []string{reason}} }
	got := RecruiterMorningText([]RiskZone{
		z("не отвечает 6 рабочих дней после двух напоминаний"), z("срочное обращение «X» ждёт ответа"),
		z("сигнал F7 в ответе на опрос"), z("не отвечает 5 рабочих дней после двух напоминаний"),
	})
	want := "Доброе утро! Сегодня: срочные обращения — 1, не отвечают после напоминаний — 2, сигналы в ответах — 1."
	if got != want {
		t.Fatalf("%q", got)
	}
}

// TestRecruiterMorning — интеграционный: 9:30 понедельника, у рекрутера с MAX красный новичок
// (выход завтра, приглашение не открыто) — одна сводка в день; без флага — ничего.
func TestRecruiterMorning(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 30, 0, 0, ovMoscow)
	env := newEnv(t, now)
	ctx := context.Background()
	if err := env.repos.Manager().TruncateManager(ctx); err != nil {
		t.Fatal(err)
	}
	uid := int64(779001)
	u := &domain.User{Login: "rec-morning", FullName: "Рекрутер Утро", Role: domain.RoleRecruiter, Active: true, MaxUserID: &uid}
	if err := env.repos.Users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	tomorrow := civilDate(now, ovMoscow).AddDate(0, 0, 1)
	c := env.launch(t, time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC))
	e := env.employee(t, c.Employee.ID)
	e.RecruiterID = &u.ID
	if err := env.repos.Employees.Save(ctx, e); err != nil {
		t.Fatal(err)
	}

	if got, _ := env.svc.RecruiterMorning().Tick(ctx, false); len(got) != 0 {
		t.Fatal("без FEATURE_RISK_V2 сводка ушла")
	}
	env.svc.features.RiskV2 = true
	env.fake.Reset()
	got, err := env.svc.RecruiterMorning().Tick(ctx, false)
	if err != nil || len(got) != 1 || got[0].Text != "Доброе утро! Сегодня: не открыли приглашение — 1." {
		t.Fatalf("сводка: %+v %v", got, err)
	}
	msgs := env.fake.Sent()
	if len(msgs) != 1 || msgs[0].UserID != uid || len(msgs[0].Buttons) != 1 || !strings.HasSuffix(msgs[0].Buttons[0][0].URL, "/dashboard") {
		t.Fatalf("сообщение: %+v", msgs)
	}
	if again, _ := env.svc.RecruiterMorning().Tick(ctx, false); len(again) != 0 {
		t.Fatal("вторая сводка за день")
	}
}
