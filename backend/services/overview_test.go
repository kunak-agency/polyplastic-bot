package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Агрегаты обзора (api_contract.md, v3): чистые тесты BuildOverview без базы и один
// интеграционный — через настоящий Postgres (TEST_DATABASE_URL).

var ovMoscow = domain.SiteOf(domain.SiteGTP).Loc

// ovNow — среда, 21 октября 2026, 12:00 по Москве: неделя 19–25 октября.
var ovNow = time.Date(2026, 10, 21, 12, 0, 0, 0, ovMoscow)

func ovTax(t *testing.T) *taxonomy.Taxonomy {
	t.Helper()
	tax, err := taxonomy.LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	return tax
}

// ovEmp — новичок площадки site, который сегодня на дне day.
func ovEmp(name, site string, day int) domain.Employee {
	id, _ := uuid.NewV7()
	d := civilDate(ovNow, ovMoscow).AddDate(0, 0, -day)
	return domain.Employee{
		UUIDModel: domain.UUIDModel{ID: id}, FullName: name, Site: site,
		StartDate:  time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC),
		Connection: domain.ConnConnected, Risk: domain.RiskGreen,
	}
}

func ovAnswer(emp uuid.UUID, scale string, v int, at time.Time, flags ...string) repositories.OverviewAnswer {
	ev, _ := uuid.NewV7()
	return repositories.OverviewAnswer{EmployeeID: emp, EventID: ev, QuestionNo: 1, Scale: scale, Value: &v, Flags: flags, CreatedAt: at}
}

func cell(h Heatmap, cat, site string) HeatCell {
	for _, c := range h.Cells {
		if c.Category == cat && c.Site == site {
			return c
		}
	}
	return HeatCell{}
}

func action(o Overview, kind string) int {
	for _, a := range o.Actions {
		if a.Kind == kind {
			return a.Count
		}
	}
	return 0
}

func TestOverviewHeatmapAnonymity(t *testing.T) {
	rows := &repositories.OverviewRows{}
	at := ovNow.Add(-48 * time.Hour)
	// GTP: 6 откликнувшихся, F2 у пятерых, F1 у двоих.
	for i := 0; i < 6; i++ {
		e := ovEmp("GTP "+string(rune('A'+i)), domain.SiteGTP, 10)
		rows.Employees = append(rows.Employees, e)
		var flags []string
		if i < 5 {
			flags = append(flags, "F2")
		}
		if i < 2 {
			flags = append(flags, "F1")
		}
		rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale5, 3, at, flags...))
	}
	// GrPP: всего 4 откликнувшихся — колонка скрыта целиком, даже ноль.
	for i := 0; i < 4; i++ {
		e := ovEmp("GrPP "+string(rune('A'+i)), domain.SiteGrPP, 10)
		rows.Employees = append(rows.Employees, e)
		rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale5, 2, at, "F1"))
	}
	// Тестовые ответы и старые сигналы в heat map не идут.
	old := ovEmp("Старый", domain.SiteGTP, 60)
	rows.Employees = append(rows.Employees, old)
	rows.Answers = append(rows.Answers, ovAnswer(old.ID, domain.Scale5, 1, ovNow.AddDate(0, 0, -45), "F6"))
	test := ovAnswer(rows.Employees[0].ID, domain.Scale5, 1, at, "F7")
	test.IsTest = true
	rows.Answers = append(rows.Answers, test)

	o := BuildOverview(rows, "", ovNow, ovTax(t))
	h := o.Heatmap
	if h.MinGroup != 5 || h.Respondents[domain.SiteGTP] != 6 || h.Respondents[domain.SiteGrPP] != 4 {
		t.Fatalf("respondents %+v", h.Respondents)
	}
	if c := cell(h, "F2", domain.SiteGTP); c.Hidden || c.Count == nil || *c.Count != 5 || c.Share == nil || *c.Share != 0.83 {
		t.Fatalf("F2 GTP %+v", c)
	}
	if c := cell(h, "F1", domain.SiteGTP); !c.Hidden || c.Count != nil || c.Share != nil {
		t.Fatalf("F1 GTP (2 человека) должна быть скрыта: %+v", c)
	}
	if c := cell(h, "F3", domain.SiteGTP); c.Hidden || c.Count == nil || *c.Count != 0 {
		t.Fatalf("F3 GTP — ноль виден: %+v", c)
	}
	for _, cat := range HeatmapCategories {
		if c := cell(h, cat, domain.SiteGrPP); !c.Hidden || c.Count != nil {
			t.Fatalf("GrPP %s при 4 откликнувшихся должна быть скрыта: %+v", cat, c)
		}
	}
	if c := cell(h, "F6", domain.SiteGTP); c.Count == nil || *c.Count != 0 {
		t.Fatalf("сигнал старше окна попал в heat map: %+v", c)
	}
	if c := cell(h, "F7", domain.SiteGTP); c.Count == nil || *c.Count != 0 {
		t.Fatalf("тестовый ответ попал в heat map: %+v", c)
	}
	if len(h.Categories) != 10 || h.Categories[1].Code != "F2" || !strings.Contains(h.Categories[1].Name, "доступ") {
		t.Fatalf("категории %+v", h.Categories)
	}

	// Фильтр площадки: одна колонка.
	g := BuildOverview(rows, domain.SiteGTP, ovNow, ovTax(t)).Heatmap
	if len(g.Sites) != 1 || len(g.Cells) != 10 {
		t.Fatalf("site=GTP: %v, ячеек %d", g.Sites, len(g.Cells))
	}
}

func TestOverviewTrendAnonymity(t *testing.T) {
	rows := &repositories.OverviewRows{}
	thisWeek := ovNow.Add(-time.Hour)   // неделя 19.10
	lastWeek := ovNow.AddDate(0, 0, -7) // неделя 12.10
	for i := 0; i < 5; i++ {
		e := ovEmp("G", domain.SiteGTP, 20)
		rows.Employees = append(rows.Employees, e)
		rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale10, 6+i%2, thisWeek)) // 6,7,6,7,6
		if i < 4 {
			rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale5, 4, lastWeek)) // 8 на шкале 10
		}
	}
	tr := BuildOverview(rows, "", ovNow, ovTax(t)).Trend
	if len(tr.Weeks) != TrendWeeks || !tr.Weeks[TrendWeeks-1].Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, ovMoscow)) {
		t.Fatalf("недели %v", tr.Weeks)
	}
	var gtp, all TrendSeries
	for _, s := range tr.Series {
		switch s.Site {
		case domain.SiteGTP:
			gtp = s
		case TrendAll:
			all = s
		}
	}
	cur, prev := gtp.Points[TrendWeeks-1], gtp.Points[TrendWeeks-2]
	if cur.Hidden || cur.Avg == nil || *cur.Avg != 6.4 || cur.Respondents != 5 {
		t.Fatalf("текущая неделя %+v", cur)
	}
	if !prev.Hidden || prev.Avg != nil || prev.Respondents != 4 {
		t.Fatalf("4 ответивших — точка скрыта: %+v", prev)
	}
	if all.Points[TrendWeeks-1].Avg == nil || len(tr.Series) != 4 {
		t.Fatalf("серии %+v", tr.Series)
	}
}

func TestOverviewTilesRisksActions(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	add := func(e domain.Employee) domain.Employee {
		rows.Employees = append(rows.Employees, e)
		return e
	}
	before := add(ovEmp("До выхода", domain.SiteGTP, -2)) // выход в пятницу этой недели
	before.Connection = domain.ConnInvited
	rows.Employees[len(rows.Employees)-1] = before
	add(ovEmp("Сегодня вышел", domain.SiteGTP, 0))
	low := add(ovEmp("Низкий пульс", domain.SiteGTP, 15))
	silentE := ovEmp("Молчит", domain.SiteGrPP, 20)
	old := ovNow.AddDate(0, 0, -7)
	silentE.LastActivityAt = &old
	silent := add(silentE)
	add(ovEmp("Завершил", domain.SiteGTP, 120))
	probation := add(ovEmp("Решение по ИС", domain.SiteLuzhniki, 88))
	botRisk := ovEmp("Бот отметил", domain.SiteLuzhniki, 40)
	botRisk.Risk, botRisk.RiskReason = domain.RiskYellow, strPtr("оценка 2/5 в «Итог первого дня»")
	add(botRisk)
	twice := add(ovEmp("Два опроса", domain.SiteGrPP, 25))

	// Низкий пульс два раза подряд: 2/5 и 4/10 — красная зона.
	rows.Answers = append(rows.Answers,
		ovAnswer(low.ID, domain.Scale5, 2, ovNow.AddDate(0, 0, -10)),
		ovAnswer(low.ID, domain.Scale10, 4, ovNow.AddDate(0, 0, -1)),
	)
	// Опросы за окно: один отвечен, один без ответа трое суток (просрочка), один тестовый.
	sent, sent6 := ovNow.AddDate(0, 0, -3), ovNow.AddDate(0, 0, -6)
	evID := func() uuid.UUID { id, _ := uuid.NewV7(); return id }
	rows.Events = append(rows.Events,
		repositories.OverviewEvent{ID: evID(), EmployeeID: low.ID, Kind: domain.KindSurvey, Title: "Чек двух недель", Status: domain.EventAnswered, SentAt: &sent},
		repositories.OverviewEvent{ID: evID(), EmployeeID: silent.ID, Kind: domain.KindSurvey, Title: "Чек двух недель", Status: domain.EventSent, SentAt: &sent},
		repositories.OverviewEvent{ID: evID(), EmployeeID: silent.ID, Kind: domain.KindSurvey, Title: "Тест", Status: domain.EventSent, SentAt: &sent, IsTest: true},
		repositories.OverviewEvent{ID: evID(), EmployeeID: twice.ID, Kind: domain.KindSurvey, Title: "Пульс", Status: domain.EventSent, SentAt: &sent6},
		repositories.OverviewEvent{ID: evID(), EmployeeID: twice.ID, Kind: domain.KindSurvey, Title: "Чек", Status: domain.EventSent, SentAt: &sent},
	)
	// Обращения: открытое без исполнителя с просроченным SLA (P0), назначенное с SLA сегодня,
	// решённое.
	past, later := ovNow.Add(-time.Hour), ovNow.Add(2*time.Hour)
	tid := evID
	anna := "Анна (синтетика)"
	rows.Tickets = append(rows.Tickets,
		repositories.OverviewTicket{ID: tid(), EmployeeID: silent.ID, Category: "F2", Priority: taxonomy.P0, Status: domain.TicketOpen, SLADueAt: &past, CreatedAt: ovNow.AddDate(0, 0, -1)},
		repositories.OverviewTicket{ID: tid(), EmployeeID: low.ID, Category: "F9", Priority: taxonomy.P1, Status: domain.TicketAssigned, AssigneeName: &anna, SLADueAt: &later, CreatedAt: ovNow.AddDate(0, 0, -1)},
		repositories.OverviewTicket{ID: tid(), EmployeeID: probation.ID, Category: "F1", Priority: taxonomy.P2, Status: domain.TicketResolved, CreatedAt: ovNow.AddDate(0, 0, -2)},
	)

	o := BuildOverview(rows, "", ovNow, tax)
	tl := o.Tiles
	if tl.InAdaptation != 7 || tl.StartingThisWeek != 2 || tl.Overdue != 2 || tl.TicketsOpen != 2 || tl.TicketsSLAOverdue != 1 {
		t.Fatalf("плитки %+v", tl)
	}
	if tl.PulseSent != 4 || tl.PulseAnswered != 1 || tl.PulsePercent == nil || *tl.PulsePercent != 25 {
		t.Fatalf("отклик %+v", tl)
	}
	stages := map[string]int{}
	for _, s := range o.Stages {
		stages[s.Stage] = s.Count
	}
	if stages[domain.StageBefore] != 1 || stages[domain.StageDay0] != 1 || stages[domain.StageMonth1] != 3 || stages[domain.StageProbation] != 2 || len(o.Stages) != 5 {
		t.Fatalf("этапы %+v", o.Stages)
	}

	zones := map[string]RiskZone{}
	for _, r := range o.Risks {
		zones[r.FullName] = r
	}
	if r := zones["Низкий пульс"]; r.Zone != domain.RiskRed || !hasReason(r, "низкий пульс 2 раза подряд") || !hasReason(r, "обращение «конфиденциальное»") {
		t.Fatalf("низкий пульс: %+v", r)
	}
	if r := zones["Молчит"]; r.Zone != domain.RiskRed || !r.Overdue || !hasReason(r, "опрос «Чек двух недель» без ответа 3 дня") || !hasReason(r, "обращение «Нет доступов, техники, СИЗ, пропуска»") {
		t.Fatalf("молчит: %+v", r)
	}
	if r := zones["Два опроса"]; r.Zone != domain.RiskYellow || !r.Overdue || len(r.Reasons) != 1 || r.Reasons[0] != "не отвечает боту 6 дней: опросов без ответа 2" {
		t.Fatalf("два опроса: %+v", r)
	}
	if r := zones["До выхода"]; r.Zone != domain.RiskYellow || !hasReason(r, "не открыл(а) приглашение, выход через 2 дня") {
		t.Fatalf("до выхода: %+v", r)
	}
	if r := zones["Бот отметил"]; r.Zone != domain.RiskYellow || r.Reasons[0] != "оценка 2/5 в «Итог первого дня»" {
		t.Fatalf("бот: %+v", r)
	}
	if _, ok := zones["Завершил"]; ok {
		t.Fatal("завершивший адаптацию не в зонах риска")
	}
	if _, ok := zones["Решение по ИС"]; ok {
		t.Fatal("решённое обращение не делает зону риска")
	}
	if o.Risks[0].Zone != domain.RiskRed || o.Risks[len(o.Risks)-1].Zone != domain.RiskYellow {
		t.Fatalf("красные — первыми: %+v", o.Risks)
	}

	if action(o, ActionTicketsSLAOverdue) != 1 || action(o, ActionTicketsUnassigned) != 1 || action(o, ActionTicketsSLAToday) != 1 ||
		action(o, ActionRedZones) != 2 || action(o, ActionNotConnectedSoon) != 1 || action(o, ActionStartingToday) != 1 ||
		action(o, ActionProbationDecisions) != 1 || action(o, ActionSurveysOverdue) != 3 {
		t.Fatalf("действия %+v", o.Actions)
	}
	if o.Actions[0].Kind != ActionTicketsSLAOverdue {
		t.Fatalf("порядок действий %+v", o.Actions)
	}
	if !o.WeekFrom.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, ovMoscow)) || !o.WeekTo.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, ovMoscow)) {
		t.Fatalf("неделя %v–%v", o.WeekFrom, o.WeekTo)
	}

	// Пусто: ни плиток, ни процента, массивы есть.
	e := BuildOverview(&repositories.OverviewRows{}, "", ovNow, tax)
	if e.Tiles.PulsePercent != nil || len(e.Stages) != 5 || len(e.Heatmap.Cells) != 30 || len(e.Trend.Series) != 4 {
		t.Fatalf("пустой обзор %+v", e)
	}
}

func hasReason(r RiskZone, want string) bool {
	for _, x := range r.Reasons {
		if x == want {
			return true
		}
	}
	return false
}

func TestDaysRu(t *testing.T) {
	for n, want := range map[int]string{1: "1 день", 2: "2 дня", 5: "5 дней", 11: "11 дней", 21: "21 день", 24: "24 дня"} {
		if got := daysRu(n); got != want {
			t.Fatalf("%d: %q", n, got)
		}
	}
}

// TestOverviewDB — те же агрегаты через базу: Load отбирает площадку, текст ответа не
// читает, тестовые ответы не считает.
func TestOverviewDB(t *testing.T) {
	e := newEnv(t, ovNow)
	ctx := context.Background()
	c := e.launch(t, civilDate(ovNow, ovMoscow).AddDate(0, 0, -15))
	emp := e.employee(t, c.Employee.ID)
	emp.Connection = domain.ConnConnected
	if err := e.repos.Employees.Save(ctx, emp); err != nil {
		t.Fatal(err)
	}
	evs := e.events(t, emp.ID)
	d0 := eventByTitle(t, evs, "Итог дня")
	sentAt := ovNow.AddDate(0, 0, -15)
	d0.Status, d0.SentAt = domain.EventAnswered, &sentAt
	if err := e.repos.Events.Save(ctx, &d0); err != nil {
		t.Fatal(err)
	}
	v := 2
	if err := e.repos.Events.UpsertAnswer(ctx, &domain.SurveyAnswer{
		UUIDModel:  domain.UUIDModel{CreatedAt: ovNow.AddDate(0, 0, -15)},
		EmployeeID: emp.ID, EventID: d0.ID, QuestionNo: 1, Scale: domain.Scale5, Value: &v,
		Text: "текст новичка (синтетика)", Flags: domain.StringList{"F2"},
	}); err != nil {
		t.Fatal(err)
	}
	d14 := eventByTitle(t, evs, "Чек двух недель")
	sent14 := ovNow.AddDate(0, 0, -1)
	d14.Status, d14.SentAt = domain.EventAnswered, &sent14
	if err := e.repos.Events.Save(ctx, &d14); err != nil {
		t.Fatal(err)
	}
	for q, val := range []int{3, 4, 5} {
		val := val
		if err := e.repos.Events.UpsertAnswer(ctx, &domain.SurveyAnswer{
			UUIDModel:  domain.UUIDModel{CreatedAt: sent14},
			EmployeeID: emp.ID, EventID: d14.ID, QuestionNo: q + 1, Scale: domain.Scale10, Value: &val, Flags: domain.StringList{},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.repos.Tickets.Create(ctx, &domain.Ticket{
		EmployeeID: emp.ID, Category: "F2", Priority: taxonomy.P1, Status: domain.TicketOpen, Route: "hr",
		Summary: "синтетика", Source: domain.TicketFromAccess,
	}); err != nil {
		t.Fatal(err)
	}

	o, err := e.svc.Overview().Get(ctx, domain.SiteGTP)
	if err != nil {
		t.Fatal(err)
	}
	if o.Tiles.InAdaptation != 1 || o.Tiles.TicketsOpen != 1 || o.Tiles.PulseAnswered != 2 || o.Tiles.PulseSent != 2 {
		t.Fatalf("плитки %+v", o.Tiles)
	}
	if len(o.Risks) != 1 || o.Risks[0].Zone != domain.RiskRed || !hasReason(o.Risks[0], "низкий пульс 2 раза подряд") {
		t.Fatalf("зоны %+v", o.Risks)
	}
	// Один новичок — heat map скрыта по порогу.
	if c := cell(o.Heatmap, "F2", domain.SiteGTP); !c.Hidden {
		t.Fatalf("heat map при одном человеке %+v", c)
	}
	other, err := e.svc.Overview().Get(ctx, domain.SiteGrPP)
	if err != nil {
		t.Fatal(err)
	}
	if other.Tiles.InAdaptation != 0 || other.Tiles.TicketsOpen != 0 || len(other.Risks) != 0 {
		t.Fatalf("GrPP видит чужих: %+v", other.Tiles)
	}
}

func TestOverviewTrendSiteFilter(t *testing.T) {
	tr := BuildOverview(&repositories.OverviewRows{}, domain.SiteGTP, ovNow, ovTax(t)).Trend
	if len(tr.Series) != 1 || tr.Series[0].Site != domain.SiteGTP {
		t.Fatalf("при фильтре площадки — одна серия без all: %+v", tr.Series)
	}
}
