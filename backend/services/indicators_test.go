package services

import (
	"testing"
	"time"

	"main/domain"
	"main/repositories"

	"github.com/google/uuid"
)

// «Показатели»: чистые тесты BuildIndicators без базы (ovNow — среда 21.10.2026, 12:00 МСК).

func indPeriod() IndicatorPeriod { return IndicatorPeriodOf(Period30d, ovNow, ovMoscow) }

// indEmp — сотрудник на дне day; подключился (consent) в consentDay относительно D0 (nil — не
// подключался), последняя реакция — в reactDay (nil — не было).
func indEmp(name, site string, day int, consentDay, reactDay *int) domain.Employee {
	e := ovEmp(name, site, day)
	d0 := civilDate(e.StartDate, ovMoscow)
	e.LastActivityAt = nil
	if consentDay == nil {
		e.Connection = domain.ConnInvited
	} else {
		at := d0.AddDate(0, 0, *consentDay).Add(10 * time.Hour)
		e.ConsentAt = &at
	}
	if reactDay != nil {
		at := d0.AddDate(0, 0, *reactDay).Add(11 * time.Hour)
		e.LastActivityAt = &at
	}
	return e
}

func ip(n int) *int { return &n }

func findStep(f Funnel, key string) FunnelStep {
	for _, s := range f.Steps {
		if s.Key == key {
			return s
		}
	}
	return FunnelStep{}
}

func lost(s FunnelStep, reason string) int {
	for _, l := range s.Lost {
		if l.Reason == reason {
			return l.Count
		}
	}
	return 0
}

func findTile(ind Indicators, key string) IndicatorTile {
	for _, t := range ind.Tiles {
		if t.Key == key {
			return t
		}
	}
	return IndicatorTile{}
}

func TestIndicatorPeriod(t *testing.T) {
	p := IndicatorPeriodOf(Period30d, ovNow, ovMoscow)
	if p.Days != 30 || p.From.Format(time.DateOnly) != "2026-09-22" || p.PrevFrom.Format(time.DateOnly) != "2026-08-23" || !p.PrevTo.Equal(p.From) {
		t.Fatalf("30d %+v", p)
	}
	q := IndicatorPeriodOf(PeriodQuarter, ovNow, ovMoscow)
	if q.From.Format(time.DateOnly) != "2026-10-01" || q.PrevFrom.Format(time.DateOnly) != "2026-07-01" || q.Days != 21 {
		t.Fatalf("q %+v", q)
	}
	if IndicatorPeriodOf("bogus", ovNow, ovMoscow).Key != Period30d {
		t.Fatal("неизвестный период — 30d")
	}
}

// Воронка: каждый шаг — подмножество предыдущего, count + pending + потери = прошлый count.
func TestIndicatorsFunnel(t *testing.T) {
	rows := &repositories.OverviewRows{}
	add := func(e domain.Employee) { rows.Employees = append(rows.Employees, e) }
	// Когорта 30 дней: 8 человек.
	add(indEmp("Не подключился", domain.SiteGTP, 5, nil, nil))      // потерян: не подключился
	add(indEmp("Поздно", domain.SiteGTP, 5, ip(1), ip(4)))          // потерян: подключился после выхода
	add(indEmp("Сегодня вышел", domain.SiteGTP, 0, ip(-3), ip(-1))) // D0 сегодня: ждёт «прошёл D0»
	add(indEmp("Молчит с D0", domain.SiteGTP, 6, ip(-3), ip(-2)))   // потерян на D0: молчит
	stopped := indEmp("Остановил", domain.SiteGTP, 6, ip(-3), nil)  // потерян на D0: остановил бота
	stopped.Connection = domain.ConnStopped
	add(stopped)
	for i := 0; i < 3; i++ { // прошли D0, D+30 ещё не наступил
		add(indEmp("Активный "+string(rune('A'+i)), domain.SiteGTP, 10, ip(-2), ip(9)))
	}
	// Вне когорты: вышел 40 дней назад, выйдет через 3 дня.
	add(indEmp("Старый", domain.SiteGTP, 40, ip(-3), ip(39)))
	add(indEmp("Будущий", domain.SiteGTP, -3, ip(-5), nil))

	ind := BuildIndicators(IndicatorRowsAll{Overview: rows}, "", indPeriod(), ovNow, ovTax(t))
	f := ind.Funnel
	if ind.Cohort != 8 || f.Cohort != 8 || f.Hidden || f.Percents {
		t.Fatalf("когорта %d, funnel %+v", ind.Cohort, f)
	}
	inv := findStep(f, StepInvited)
	conn := findStep(f, StepConnected)
	d0 := findStep(f, StepPassedD0)
	d30 := findStep(f, StepActiveD30)
	if inv.Count != 8 || inv.Percent != nil {
		t.Fatalf("invited %+v: при n < 20 без процентов", inv)
	}
	if conn.Count != 6 || lost(conn, LossNotConnected) != 1 || lost(conn, LossConnectedLate) != 1 {
		t.Fatalf("connected %+v", conn)
	}
	if d0.Count != 3 || d0.Pending != 1 || lost(d0, LossSilent) != 1 || lost(d0, LossStopped) != 1 {
		t.Fatalf("passed_d0 %+v", d0)
	}
	if d30.Count != 0 || d30.Pending != 3 || len(d30.Lost) != 0 {
		t.Fatalf("active_d30 %+v", d30)
	}
	for i := 1; i < len(f.Steps); i++ {
		s, prev := f.Steps[i], f.Steps[i-1]
		sum := s.Count + s.Pending
		for _, l := range s.Lost {
			sum += l.Count
		}
		if sum != prev.Count {
			t.Fatalf("шаг %s: %d + ожидают + потери ≠ %d", s.Key, s.Count, prev.Count)
		}
	}
	if tile := findTile(ind, TileConnectedBeforeD0); tile.Status != StatusOK || tile.Value == nil || *tile.Value != 75 || tile.N != 8 {
		t.Fatalf("плитка подключения %+v", tile)
	}
}

// Активен на D+30: реакция не раньше D+23 и бот не остановлен; D+90 и решение по ИС.
func TestIndicatorsFunnelActive(t *testing.T) {
	rows := &repositories.OverviewRows{}
	for i := 0; i < 25; i++ {
		react := 40 // активен на D+30
		switch {
		case i < 5:
			react = 20 // молчит с D+20 — потерян на D+30
		case i < 7:
			react = 24 // D+24 — ещё в окне недели перед D+30
		}
		rows.Employees = append(rows.Employees, indEmp("Q", domain.SiteGTP, 45, ip(-3), ip(react)))
	}
	p := IndicatorPeriodOf(Period90d, ovNow, ovMoscow)
	f := BuildIndicators(IndicatorRowsAll{Overview: rows}, "", p, ovNow, ovTax(t)).Funnel
	d30 := findStep(f, StepActiveD30)
	if !f.Percents || d30.Count != 20 || lost(d30, LossSilent) != 5 || d30.Percent == nil || *d30.Percent != 80 {
		t.Fatalf("active_d30 %+v", d30)
	}
	if d90 := findStep(f, StepActiveD90); d90.Pending != 20 || d90.Count != 0 {
		t.Fatalf("active_d90 %+v", d90)
	}
}

// Порог 5: меньше 5 — «мало данных», воронка только с «приглашены», ячейка площадки скрыта.
func TestIndicatorsMinGroup(t *testing.T) {
	rows := &repositories.OverviewRows{}
	for i := 0; i < 4; i++ {
		rows.Employees = append(rows.Employees, indEmp("Малая", domain.SiteLuzhniki, 5, ip(-3), ip(4)))
	}
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows}, domain.SiteLuzhniki, indPeriod(), ovNow, ovTax(t))
	if tile := findTile(ind, TileConnectedBeforeD0); tile.Status != StatusFew || tile.Value != nil {
		t.Fatalf("n=4 — мало данных: %+v", tile)
	}
	if !ind.Funnel.Hidden || findStep(ind.Funnel, StepInvited).Count != 4 || !findStep(ind.Funnel, StepConnected).Hidden {
		t.Fatalf("воронка при n < 5: %+v", ind.Funnel)
	}
	for _, row := range ind.Sites.Rows {
		if row.Key == SiteRowCohort {
			continue
		}
		for _, c := range row.Cells {
			if !c.Hidden || c.Value != nil || c.Verdict != VerdictHidden {
				t.Fatalf("%s %s при 4 сотрудниках должна быть скрыта: %+v", row.Key, c.Site, c)
			}
		}
	}
	if tile := findTile(ind, TileIPPassed); tile.Status != StatusLater90 {
		t.Fatalf("решений по ИС нет — «появится через 90 дней»: %+v", tile)
	}
	if tile := findTile(ind, TileEarlyLeave); tile.Status != StatusNoMarker || tile.Value != nil {
		t.Fatalf("признака увольнения нет: %+v", tile)
	}
}

// «В пределах колебаний»: n < 30 и разница со средним меньше 5 п.п.
func TestSiteVerdict(t *testing.T) {
	cases := []struct {
		pp   float64
		n    int
		want string
	}{
		{3, 20, VerdictNoise},
		{-4.9, 29, VerdictNoise},
		{5, 20, VerdictAbove},
		{3, 40, VerdictAbove},
		{-3, 40, VerdictBelow},
		{0.5, 40, VerdictSame},
	}
	for _, c := range cases {
		if got := siteVerdict(c.pp, c.n); got != c.want {
			t.Errorf("siteVerdict(%v, %d) = %s, want %s", c.pp, c.n, got, c.want)
		}
	}
}

// Площадки: среднее — по всем вместе; отклонение от 10 п.п. — флаг «что мешает».
func TestIndicatorsSitesTable(t *testing.T) {
	rows := &repositories.OverviewRows{}
	// GTP: 10 из 10 подключились до выхода; GrPP: 5 из 10.
	for i := 0; i < 10; i++ {
		rows.Employees = append(rows.Employees, indEmp("G", domain.SiteGTP, 5, ip(-3), ip(4)))
		c := ip(-3)
		if i%2 == 0 {
			c = ip(1)
		}
		rows.Employees = append(rows.Employees, indEmp("R", domain.SiteGrPP, 5, c, ip(4)))
	}
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows}, "", indPeriod(), ovNow, ovTax(t))
	var row SiteRow
	for _, r := range ind.Sites.Rows {
		if r.Key == SiteRowConnected {
			row = r
		}
	}
	if row.Avg == nil || *row.Avg != 75 {
		t.Fatalf("среднее %+v", row)
	}
	byKey := map[string]SiteCell{}
	for _, c := range row.Cells {
		byKey[c.Site] = c
	}
	gtp, grpp, luz := byKey[domain.SiteGTP], byKey[domain.SiteGrPP], byKey[domain.SiteLuzhniki]
	if gtp.Value == nil || *gtp.Value != 100 || *gtp.DiffPP != 25 || gtp.Verdict != VerdictAbove || !gtp.Flag {
		t.Fatalf("GTP %+v", gtp)
	}
	if grpp.Value == nil || *grpp.Value != 50 || *grpp.DiffPP != -25 || grpp.Verdict != VerdictBelow || !grpp.Flag {
		t.Fatalf("GrPP %+v", grpp)
	}
	if !luz.Hidden {
		t.Fatalf("Luzhniki без сотрудников — скрыто: %+v", luz)
	}
	if len(ind.Sites.Flagged) != 2 || ind.Sites.Rows[0].Key != SiteRowCohort {
		t.Fatalf("flagged %v, порядок строк %v", ind.Sites.Flagged, ind.Sites.Rows[0].Key)
	}
}

// Сравнение с прошлым периодом: отклик на пульс 80 % против 50 % — «растёт» на 30 п.п.
func TestIndicatorsCompare(t *testing.T) {
	rows := &repositories.OverviewRows{}
	e := indEmp("P", domain.SiteGTP, 50, ip(-3), ip(49))
	rows.Employees = append(rows.Employees, e)
	survey := func(at time.Time, answered bool) {
		id, _ := uuid.NewV7()
		st := domain.EventSent
		if answered {
			st = domain.EventAnswered
		}
		rows.Events = append(rows.Events, repositories.OverviewEvent{ID: id, EmployeeID: e.ID, Kind: domain.KindSurvey, Status: st, SentAt: &at})
	}
	cur, prev := ovNow.AddDate(0, 0, -5), ovNow.AddDate(0, 0, -35)
	for i := 0; i < 10; i++ {
		survey(cur, i < 8)
		survey(prev, i < 5)
	}
	tile := findTile(BuildIndicators(IndicatorRowsAll{Overview: rows}, "", indPeriod(), ovNow, ovTax(t)), TilePulse)
	if tile.Value == nil || *tile.Value != 80 || tile.Prev == nil || *tile.Prev != 50 || *tile.Delta != 30 || tile.Direction != DirUp {
		t.Fatalf("pulse %+v", tile)
	}
	if d, dir := compare(ip(50), ip(50), SameDeltaPP); *d != 0 || dir != DirSame {
		t.Fatalf("равные — как было: %v %s", *d, dir)
	}
	if _, dir := compare(ip(40), nil, SameDeltaPP); dir != "" {
		t.Fatal("без прошлого периода сравнения нет")
	}
	// Прошлый период меньше 5 — без сравнения.
	if tl := rateTile(TilePulse, 8, 10, 2, 4); tl.Prev != nil || tl.Direction != "" {
		t.Fatalf("прошлое n=4: %+v", tl)
	}
}

// Бот без человека и первый ответ в рабочих минутах.
func TestIndicatorsHRLoad(t *testing.T) {
	rows := &repositories.OverviewRows{}
	extra := &repositories.IndicatorRows{FirstStaff: map[uuid.UUID]time.Time{}}
	e := indEmp("H", domain.SiteGTP, 20, ip(-3), ip(19))
	rows.Employees = append(rows.Employees, e)
	for i := 0; i < 5; i++ {
		rows.Employees = append(rows.Employees, indEmp("X", domain.SiteGTP, 20, ip(-3), ip(19)))
	}
	at := ovNow.AddDate(0, 0, -2) // понедельник 12:00
	for i := 0; i < 10; i++ {
		q := repositories.IndicatorQuestion{EmployeeID: e.ID, Kind: domain.QuestionAnswer, CreatedAt: at}
		switch i {
		case 7:
			q.NotThat = 1
		case 8:
			now := at
			q.HumanAt = &now
		case 9:
			q.Kind = domain.QuestionAbstain
		}
		extra.Questions = append(extra.Questions, q)
	}
	for i := 0; i < 5; i++ {
		id, _ := uuid.NewV7()
		due := at.Add(time.Hour)
		rows.Tickets = append(rows.Tickets, repositories.OverviewTicket{ID: id, EmployeeID: e.ID, Category: "F1", Status: domain.TicketHumanActive, CreatedAt: at, SLADueAt: &due})
		extra.FirstStaff[id] = at.Add(time.Duration(30*(i+1)) * time.Minute) // 30…150 минут
	}
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows, Extra: extra}, "", indPeriod(), ovNow, ovTax(t))
	if bot := findTile(ind, TileBotClosed); bot.Value == nil || *bot.Value != 70 || bot.N != 10 {
		t.Fatalf("бот без человека %+v", bot)
	}
	fr := findTile(ind, TileFirstResponse)
	if fr.Value == nil || *fr.Value != 90 || fr.SLAPct == nil || *fr.SLAPct != 40 || fr.SLAN != 5 {
		t.Fatalf("первый ответ %+v", fr)
	}
	if ind.HRLoad.Tickets != 5 || ind.HRLoad.Employees != 6 || ind.HRLoad.Per100 == nil || *ind.HRLoad.Per100 != 83.3 {
		t.Fatalf("нагрузка %+v", ind.HRLoad)
	}
}

func TestWorkMinutesBetween(t *testing.T) {
	site := domain.SiteOf(domain.SiteGTP)
	fri := time.Date(2026, 10, 23, 16, 0, 0, 0, ovMoscow)
	mon := time.Date(2026, 10, 26, 10, 0, 0, 0, ovMoscow)
	// Пятница 16–17 (короткий день) + понедельник 9–10 = 120 минут; выходные не считаются.
	if got := workMinutesBetween(fri, mon, site); got != 120 {
		t.Fatalf("got %v", got)
	}
	if got := workMinutesBetween(mon, fri, site); got != 0 {
		t.Fatalf("обратный порядок: %v", got)
	}
}

// Темы: порог 5 и динамика словами.
func TestTopicTrend(t *testing.T) {
	cases := []struct {
		cur, prev *int
		want      string
	}{
		{ip(8), nil, DirNew},
		{nil, ip(6), DirDown},
		{ip(9), ip(6), DirUp},
		{ip(6), ip(9), DirDown},
		{ip(6), ip(7), DirSame},
		{nil, nil, ""},
	}
	for _, c := range cases {
		if got := topicTrend(c.cur, c.prev); got != c.want {
			t.Errorf("topicTrend = %q, want %q", got, c.want)
		}
	}
	rows := &repositories.OverviewRows{}
	at := ovNow.AddDate(0, 0, -3)
	for i := 0; i < 6; i++ {
		e := indEmp("T", domain.SiteGTP, 10, ip(-3), ip(9))
		rows.Employees = append(rows.Employees, e)
		rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale5, 2, at, "F2"))
		if i < 3 {
			rows.Answers = append(rows.Answers, ovAnswer(e.ID, domain.Scale5, 2, at, "F4"))
		}
	}
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows}, "", indPeriod(), ovNow, ovTax(t))
	for _, tt := range ind.Topics.Trends {
		switch tt.Category {
		case "F2":
			if tt.Count == nil || *tt.Count != 6 || tt.Trend != DirNew {
				t.Fatalf("F2 %+v", tt)
			}
		case "F4":
			if tt.Count != nil || tt.Trend != "" {
				t.Fatalf("F4 (3 человека) скрыта: %+v", tt)
			}
		}
	}
	if ind.Topics.Heatmap.PeriodDays != 30 {
		t.Fatalf("heatmap days %d", ind.Topics.Heatmap.PeriodDays)
	}
}

// Синтетика: исходы помечаются «демо».
func TestIndicatorsDemo(t *testing.T) {
	rows := &repositories.OverviewRows{}
	for i := 0; i < 6; i++ {
		e := indEmp("Сотрудник (синтетика)", domain.SiteGTP, 100, ip(-3), ip(99))
		d := domain.IPKeep
		if i == 0 {
			d = domain.IPPart
		}
		e.IPDecision = &d
		rows.Employees = append(rows.Employees, e)
	}
	p := IndicatorPeriodOf(Period30d, ovNow, ovMoscow)
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows}, "", p, ovNow, ovTax(t))
	tile := findTile(ind, TileIPPassed)
	if tile.Status != StatusOK || tile.Value == nil || *tile.Value != 83 || tile.N != 6 || !tile.Demo {
		t.Fatalf("ИС %+v", tile)
	}
	if !findTile(ind, TileEarlyLeave).Demo {
		t.Fatal("ранний уход на синтетике — демо")
	}
	if findTile(ind, TilePulse).Demo {
		t.Fatal("процессные плитки не помечаются демо")
	}
}
