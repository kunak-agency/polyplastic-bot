package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Обзор HR (UX-спецификация п. 3.1 и 3.7, api_contract.md «v3»). Всё считает код из строк
// employees, scheduled_events, survey_answers и tickets; LLM здесь нет. Тексты ответов
// новичков в обзор не попадают никогда: только числа, коды категорий и причины, которые
// пишет код.

// Пороги обзора.
const (
	// AnonymityMinGroup — порог анонимности (taxonomy.yaml rules.anonymity_min_group, UX 3.7):
	// ячейка heat map и точка тренда, за которыми меньше 5 человек, скрываются.
	AnonymityMinGroup = 5
	// OverviewPeriodDays — окно heat map и отклика на пульс.
	OverviewPeriodDays = 30
	// TrendWeeks — тренд пульса по неделям (UX 3.1: «Настроение за 4 недели»).
	TrendWeeks = 4
	// SurveyOverdueHours — опрос ушёл и без ответа дольше двух суток — просрочка.
	SurveyOverdueHours = 48
	// LowPulse — низкий пульс: среднее по опросу на шкале 10 не выше 5 (1–5 умножается на 2).
	LowPulse = 5.0
	// NotConnectedBeforeDays — приглашение не открыто, а до выхода не больше трёх дней.
	NotConnectedBeforeDays = 3
)

// Действия «Требуют действия сегодня» (kind в контракте).
const (
	ActionTicketsUnassigned  = "tickets_unassigned"
	ActionTicketsSLAOverdue  = "tickets_sla_overdue"
	ActionTicketsSLAToday    = "tickets_sla_today"
	ActionRedZones           = "red_zones"
	ActionNotConnectedSoon   = "not_connected_soon"
	ActionStartingToday      = "starting_today"
	ActionProbationDecisions = "probation_decisions"
	ActionSurveysOverdue     = "surveys_overdue"
)

// OverviewStages — этапы разбивки «до выхода · 1-я неделя · до D+30 · до D+90».
var OverviewStages = []string{domain.StageBefore, domain.StageDay0, domain.StageWeek1, domain.StageMonth1, domain.StageProbation}

// HeatmapCategories — темы heat map: F1–F10. F0 — экстренный канал, в аналитику тем не
// входит (UX 3.7: «тема (F1–F10) × площадка»).
var HeatmapCategories = []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10"}

// OverviewService — обзор HR.
type OverviewService struct {
	repos *repositories.Repos
	tax   *taxonomy.Taxonomy
	now   func() time.Time
}

// Overview — сервис обзора. Отдельного поля в Services нет: обзор только читает, и ему
// хватает репозиториев, часов и таксономии, которые уже есть у соседей.
func (s *Services) Overview() *OverviewService {
	now := time.Now
	var tax *taxonomy.Taxonomy
	if s.Newcomers != nil && s.Newcomers.now != nil {
		now = s.Newcomers.now
	}
	if s.Tickets != nil {
		tax = s.Tickets.tax
	}
	return &OverviewService{repos: s.repos, tax: tax, now: now}
}

// Overview — результат GET /api/overview.
type Overview struct {
	Site        string // пусто — все площадки
	GeneratedAt time.Time
	WeekFrom    time.Time
	WeekTo      time.Time
	Tiles       OverviewTiles
	Stages      []StageCount
	Risks       []RiskZone
	Actions     []ActionItem
	Heatmap     Heatmap
	Trend       Trend
}

// OverviewTiles — плитки.
type OverviewTiles struct {
	InAdaptation      int
	StartingThisWeek  int
	Overdue           int // новичков с просрочкой
	TicketsOpen       int
	TicketsSLAOverdue int
	PulseSent         int
	PulseAnswered     int
	PulsePercent      *int // nil — опросов за окно не уходило
}

type StageCount struct {
	Stage string
	Count int
}

// RiskZone — строка «Зоны риска»: причина всегда текстом, не только цветом (UX 3.1).
type RiskZone struct {
	NewcomerID  uuid.UUID
	FullName    string
	Site        string
	Day         int
	Stage       string
	Zone        string // red | yellow
	Reasons     []string
	Overdue     bool
	LastPulse   *int
	OpenTickets int
}

type ActionItem struct {
	Kind  string
	Count int
}

// Heatmap — тема × площадка: сколько разных новичков подняли тему за окно.
type Heatmap struct {
	MinGroup    int
	PeriodDays  int
	Sites       []string
	Categories  []HeatCategory
	Respondents map[string]int // площадка → сколько новичков дали хоть какой-то сигнал за окно
	Cells       []HeatCell
}

type HeatCategory struct {
	Code string
	Name string
}

// HeatCell — Count и Share nil, когда Hidden: меньше порога анонимности.
type HeatCell struct {
	Category string
	Site     string
	Count    *int
	Share    *float64
	Hidden   bool
}

// Trend — средний пульс по неделям: по площадкам и «all» — все вместе.
type Trend struct {
	Weeks  []time.Time
	Series []TrendSeries
}

type TrendSeries struct {
	Site   string // код площадки или "all"
	Points []TrendPoint
}

// TrendPoint — Avg nil, когда Hidden (меньше AnonymityMinGroup ответивших) или ответов нет.
type TrendPoint struct {
	Week        time.Time
	Avg         *float64
	Respondents int
	Hidden      bool
}

// TrendAll — серия «все площадки».
const TrendAll = "all"

// Get — обзор по площадке (пусто — все).
func (s *OverviewService) Get(ctx context.Context, site string) (*Overview, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	since := overviewSince(now)
	rows, err := s.repos.Overview().Load(ctx, site, since)
	if err != nil {
		return nil, err
	}
	o := BuildOverview(rows, site, now, s.tax)
	return &o, nil
}

// overviewSince — самое раннее, что нужно обзору: начало окна тренда или heat map.
func overviewSince(now time.Time) time.Time {
	loc := domain.SiteOf("").Loc
	trend := weekStart(now, loc).AddDate(0, 0, -7*(TrendWeeks-1))
	period := now.Add(-OverviewPeriodDays * 24 * time.Hour)
	if trend.Before(period) {
		return trend
	}
	return period
}

// weekStart — понедельник 00:00 недели t в зоне loc.
func weekStart(t time.Time, loc *time.Location) time.Time {
	d := civilDate(t.In(loc), loc)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// pulseValue — ответ на шкале 10: 1–5 удваивается, да/нет и выбор в пульс не входят.
func pulseValue(a repositories.OverviewAnswer) (float64, bool) {
	if a.Skipped || a.Value == nil {
		return 0, false
	}
	switch a.Scale {
	case domain.Scale10:
		return float64(*a.Value), true
	case domain.Scale5:
		return float64(*a.Value) * 2, true
	}
	return 0, false
}

// isOpenTicket — обращение ещё ждёт человека.
func isOpenTicket(t repositories.OverviewTicket) bool { return t.Status != domain.TicketResolved }

// slaRunning — таймер SLA идёт: до первого ответа человека.
func slaRunning(t repositories.OverviewTicket) bool {
	return t.SLADueAt != nil && (t.Status == domain.TicketOpen || t.Status == domain.TicketAssigned)
}

// BuildOverview — все агрегаты из строк. Чистая функция: её проверяют тесты без базы.
func BuildOverview(rows *repositories.OverviewRows, site string, now time.Time, tax *taxonomy.Taxonomy) Overview {
	loc := domain.SiteOf(site).Loc
	today := civilDate(now.In(loc), loc)
	endOfToday := today.AddDate(0, 0, 1)
	wFrom := weekStart(now, loc)
	wTo := wFrom.AddDate(0, 0, 6)
	periodFrom := now.Add(-OverviewPeriodDays * 24 * time.Hour)

	o := Overview{Site: site, GeneratedAt: now, WeekFrom: wFrom, WeekTo: wTo}
	if rows == nil {
		rows = &repositories.OverviewRows{}
	}

	type person struct {
		e       domain.Employee
		day     int
		stage   string
		events  []repositories.OverviewEvent
		pulses  []float64 // средние по опросам, по времени
		tickets []repositories.OverviewTicket
	}
	people := make(map[uuid.UUID]*person, len(rows.Employees))
	order := make([]uuid.UUID, 0, len(rows.Employees))
	for _, e := range rows.Employees {
		day := DayNumber(e.StartDate, now, domain.SiteOf(e.Site))
		people[e.ID] = &person{e: e, day: day, stage: domain.StageForDay(day)}
		order = append(order, e.ID)
	}
	for _, ev := range rows.Events {
		if p := people[ev.EmployeeID]; p != nil {
			p.events = append(p.events, ev)
		}
	}
	for _, t := range rows.Tickets {
		if p := people[t.EmployeeID]; p != nil {
			p.tickets = append(p.tickets, t)
		}
	}
	// Пульс по опросам: среднее ответов одного события (ответы приходят по времени).
	type evAgg struct {
		emp uuid.UUID
		sum float64
		n   int
	}
	byEvent := map[uuid.UUID]*evAgg{}
	var evOrder []uuid.UUID
	for _, a := range rows.Answers {
		if a.IsTest {
			continue
		}
		v, ok := pulseValue(a)
		if !ok {
			continue
		}
		ag := byEvent[a.EventID]
		if ag == nil {
			ag = &evAgg{emp: a.EmployeeID}
			byEvent[a.EventID] = ag
			evOrder = append(evOrder, a.EventID)
		}
		ag.sum += v
		ag.n++
	}
	for _, id := range evOrder {
		ag := byEvent[id]
		if p := people[ag.emp]; p != nil {
			p.pulses = append(p.pulses, ag.sum/float64(ag.n))
		}
	}

	// ── Плитки, этапы, зоны риска ──────────────────────────────────────────
	stages := map[string]int{}
	overdueSurveys := 0
	for _, id := range order {
		p := people[id]
		e := p.e
		if p.stage == domain.StageDone {
			continue
		}
		o.Tiles.InAdaptation++
		stages[p.stage]++
		start := civilDate(e.StartDate, loc)
		if !start.Before(wFrom) && !start.After(wTo) {
			o.Tiles.StartingThisWeek++
		}

		zone := ""
		raise := func(z string) {
			if z == domain.RiskRed || zone == "" {
				zone = z
			}
		}
		var reasons []string
		add := func(r string) {
			for _, x := range reasons {
				if x == r {
					return
				}
			}
			reasons = append(reasons, r)
		}
		if e.Risk == domain.RiskRed || e.Risk == domain.RiskYellow {
			raise(e.Risk)
			if e.RiskReason != nil && *e.RiskReason != "" {
				add(*e.RiskReason)
			} else {
				add("риск отмечен ботом")
			}
		}
		// Два низких пульса подряд — красная зона (UX 3.1, 3.7).
		if n := len(p.pulses); n >= 2 && p.pulses[n-1] <= LowPulse && p.pulses[n-2] <= LowPulse {
			raise(domain.RiskRed)
			add("низкий пульс 2 раза подряд")
		}
		if e.Connection == domain.ConnInvited && p.day >= -NotConnectedBeforeDays {
			raise(domain.RiskYellow)
			if p.day < 0 {
				add(fmt.Sprintf("не открыл(а) приглашение, выход через %s", daysRu(-p.day)))
			} else {
				add("не открыл(а) приглашение, уже вышел(ла)")
			}
		}
		// Просрочки: опрос без ответа дольше двух суток, сообщение не доставлено. Несколько
		// опросов без ответа — одна причина «не отвечает боту N дней» от самого старого.
		overdue := false
		var pending []repositories.OverviewEvent
		if e.Connection != domain.ConnStopped {
			for _, ev := range p.events {
				if ev.IsTest {
					continue
				}
				switch {
				case ev.Kind == domain.KindSurvey && ev.Status == domain.EventSent && ev.SentAt != nil &&
					now.Sub(*ev.SentAt) >= SurveyOverdueHours*time.Hour:
					pending = append(pending, ev)
				case ev.Status == domain.EventFailed:
					overdue = true
					add(fmt.Sprintf("сообщение «%s» не доставлено", ev.Title))
				}
			}
		}
		switch len(pending) {
		case 0:
		case 1:
			add(fmt.Sprintf("опрос «%s» без ответа %s", pending[0].Title, daysRu(int(now.Sub(*pending[0].SentAt).Hours()/24))))
		default:
			oldest := pending[0].SentAt
			for _, ev := range pending[1:] {
				if ev.SentAt.Before(*oldest) {
					oldest = ev.SentAt
				}
			}
			add(fmt.Sprintf("не отвечает боту %s: опросов без ответа %d", daysRu(int(now.Sub(*oldest).Hours()/24)), len(pending)))
		}
		if len(pending) > 0 {
			overdue = true
			overdueSurveys += len(pending)
		}
		if overdue {
			o.Tiles.Overdue++
			raise(domain.RiskYellow)
		}
		open := 0
		for _, t := range p.tickets {
			if !isOpenTicket(t) {
				continue
			}
			open++
			add("обращение «" + ticketTopic(t.Category, tax) + "»")
			if t.Priority == taxonomy.P0 {
				raise(domain.RiskRed)
			} else {
				raise(domain.RiskYellow)
			}
		}
		if zone == "" {
			continue
		}
		o.Risks = append(o.Risks, RiskZone{
			NewcomerID: e.ID, FullName: e.FullName, Site: e.Site, Day: p.day, Stage: p.stage,
			Zone: zone, Reasons: reasons, Overdue: overdue, LastPulse: e.LastPulse, OpenTickets: open,
		})
	}
	sort.SliceStable(o.Risks, func(i, j int) bool {
		a, b := o.Risks[i], o.Risks[j]
		if a.Zone != b.Zone {
			return a.Zone == domain.RiskRed
		}
		if len(a.Reasons) != len(b.Reasons) {
			return len(a.Reasons) > len(b.Reasons)
		}
		return a.Day > b.Day
	})
	for _, st := range OverviewStages {
		o.Stages = append(o.Stages, StageCount{Stage: st, Count: stages[st]})
	}

	// ── Обращения ───────────────────────────────────────────────────────────
	unassigned, slaToday := 0, 0
	for _, t := range rows.Tickets {
		if people[t.EmployeeID] == nil || !isOpenTicket(t) {
			continue
		}
		o.Tiles.TicketsOpen++
		if t.Status == domain.TicketOpen && (t.AssigneeName == nil || *t.AssigneeName == "") {
			unassigned++
		}
		if slaRunning(t) {
			if t.SLADueAt.Before(now) {
				o.Tiles.TicketsSLAOverdue++
			} else if t.SLADueAt.Before(endOfToday) {
				slaToday++
			}
		}
	}

	// ── Отклик на пульс: из ушедших за окно опросов — сколько отвечены ──────────
	for _, id := range order {
		for _, ev := range people[id].events {
			if ev.IsTest || ev.Kind != domain.KindSurvey || ev.SentAt == nil || ev.SentAt.Before(periodFrom) {
				continue
			}
			o.Tiles.PulseSent++
			if ev.Status == domain.EventAnswered {
				o.Tiles.PulseAnswered++
			}
		}
	}
	if o.Tiles.PulseSent > 0 {
		pct := int(math.Round(float64(o.Tiles.PulseAnswered) * 100 / float64(o.Tiles.PulseSent)))
		o.Tiles.PulsePercent = &pct
	}

	// ── Требуют действия сегодня ─────────────────────────────────────────────
	red, notConnected, startToday, probation := 0, 0, 0, 0
	for _, r := range o.Risks {
		if r.Zone == domain.RiskRed {
			red++
		}
	}
	for _, id := range order {
		p := people[id]
		if p.e.Connection == domain.ConnInvited && p.day >= -NotConnectedBeforeDays && p.day <= 0 {
			notConnected++
		}
		if p.day == 0 {
			startToday++
		}
		// Решение по испытательному сроку: D+90 приходится на эту неделю, ещё не прошло.
		d90 := civilDate(p.e.StartDate, loc).AddDate(0, 0, 90)
		if !d90.Before(today) && !d90.After(wTo) {
			probation++
		}
	}
	for _, a := range []ActionItem{
		{ActionTicketsSLAOverdue, o.Tiles.TicketsSLAOverdue},
		{ActionTicketsUnassigned, unassigned},
		{ActionRedZones, red},
		{ActionTicketsSLAToday, slaToday},
		{ActionNotConnectedSoon, notConnected},
		{ActionSurveysOverdue, overdueSurveys},
		{ActionStartingToday, startToday},
		{ActionProbationDecisions, probation},
	} {
		if a.Count > 0 {
			o.Actions = append(o.Actions, a)
		}
	}

	o.Heatmap = buildHeatmap(rows, people2site(rows), site, periodFrom, tax)
	o.Trend = buildTrend(rows, people2site(rows), site, now, loc)
	return o
}

// people2site — площадка каждого новичка.
func people2site(rows *repositories.OverviewRows) map[uuid.UUID]string {
	out := make(map[uuid.UUID]string, len(rows.Employees))
	for _, e := range rows.Employees {
		out[e.ID] = e.Site
	}
	return out
}

// overviewSites — колонки: одна площадка при фильтре, иначе все по порядку.
func overviewSites(site string) []string {
	if site != "" {
		return []string{site}
	}
	return domain.SiteCodes
}

// buildHeatmap — тема × площадка. Тема новичка — код флага в ответе опроса или категория
// обращения за окно; считается разных новичков, не упоминаний. Ячейка, за которой от 1 до 4
// человек, скрыта (hidden, count=null); колонка площадки, где за окно сигналы дали меньше 5
// новичков, скрыта целиком — иначе и ноль кого-то выдаёт.
func buildHeatmap(rows *repositories.OverviewRows, siteOf map[uuid.UUID]string, site string, from time.Time, tax *taxonomy.Taxonomy) Heatmap {
	h := Heatmap{MinGroup: AnonymityMinGroup, PeriodDays: OverviewPeriodDays, Sites: overviewSites(site), Respondents: map[string]int{}}
	for _, c := range HeatmapCategories {
		h.Categories = append(h.Categories, HeatCategory{Code: c, Name: categoryName(c, tax)})
	}
	themes := map[string]map[string]map[uuid.UUID]bool{} // категория → площадка → новички
	respondents := map[string]map[uuid.UUID]bool{}
	mark := func(emp uuid.UUID, codes ...string) {
		s, ok := siteOf[emp]
		if !ok {
			return
		}
		if respondents[s] == nil {
			respondents[s] = map[uuid.UUID]bool{}
		}
		respondents[s][emp] = true
		for _, c := range codes {
			if themes[c] == nil {
				themes[c] = map[string]map[uuid.UUID]bool{}
			}
			if themes[c][s] == nil {
				themes[c][s] = map[uuid.UUID]bool{}
			}
			themes[c][s][emp] = true
		}
	}
	for _, a := range rows.Answers {
		if a.IsTest || a.CreatedAt.Before(from) || (a.Skipped && len(a.Flags) == 0) {
			continue
		}
		mark(a.EmployeeID, a.Flags...)
	}
	for _, t := range rows.Tickets {
		if t.IsTest || t.CreatedAt.Before(from) {
			continue
		}
		mark(t.EmployeeID, t.Category)
	}
	for _, s := range h.Sites {
		h.Respondents[s] = len(respondents[s])
	}
	for _, c := range HeatmapCategories {
		for _, s := range h.Sites {
			n := len(themes[c][s])
			cell := HeatCell{Category: c, Site: s}
			if h.Respondents[s] < AnonymityMinGroup || (n > 0 && n < AnonymityMinGroup) {
				cell.Hidden = true
			} else {
				share := math.Round(float64(n)/float64(h.Respondents[s])*100) / 100
				cell.Count, cell.Share = &n, &share
			}
			h.Cells = append(h.Cells, cell)
		}
	}
	return h
}

// buildTrend — средний пульс (шкала 10) по неделям. Точка, за которой меньше 5 ответивших,
// скрыта: среднее по трём людям легко примерить на конкретного новичка.
func buildTrend(rows *repositories.OverviewRows, siteOf map[uuid.UUID]string, site string, now time.Time, loc *time.Location) Trend {
	first := weekStart(now, loc).AddDate(0, 0, -7*(TrendWeeks-1))
	tr := Trend{}
	for i := 0; i < TrendWeeks; i++ {
		tr.Weeks = append(tr.Weeks, first.AddDate(0, 0, 7*i))
	}
	type acc struct {
		sum  float64
		n    int
		resp map[uuid.UUID]bool
	}
	// «all» — только без фильтра площадки: при фильтре она совпала бы с единственной серией.
	series := append([]string{}, overviewSites(site)...)
	if site == "" {
		series = append(series, TrendAll)
	}
	data := map[string][]acc{}
	for _, s := range series {
		data[s] = make([]acc, TrendWeeks)
	}
	for _, a := range rows.Answers {
		if a.IsTest || a.CreatedAt.Before(first) {
			continue
		}
		v, ok := pulseValue(a)
		if !ok {
			continue
		}
		s, ok := siteOf[a.EmployeeID]
		if !ok {
			continue
		}
		w := int(weekStart(a.CreatedAt, loc).Sub(first).Hours()/24) / 7
		if w < 0 || w >= TrendWeeks {
			continue
		}
		for _, key := range []string{s, TrendAll} {
			if data[key] == nil {
				continue
			}
			x := &data[key][w]
			x.sum += v
			x.n++
			if x.resp == nil {
				x.resp = map[uuid.UUID]bool{}
			}
			x.resp[a.EmployeeID] = true
		}
	}
	for _, s := range series {
		ts := TrendSeries{Site: s}
		for i, x := range data[s] {
			p := TrendPoint{Week: tr.Weeks[i], Respondents: len(x.resp)}
			switch {
			case x.n == 0:
			case len(x.resp) < AnonymityMinGroup:
				p.Hidden = true
			default:
				avg := math.Round(x.sum/float64(x.n)*10) / 10
				p.Avg = &avg
			}
			ts.Points = append(ts.Points, p)
		}
		tr.Series = append(tr.Series, ts)
	}
	return tr
}

// ticketTopic — название категории для HR. F5 и F9 видят только HR-партнёр и СБ: в обзоре
// без названия (контракт v2, restricted).
func ticketTopic(code string, tax *taxonomy.Taxonomy) string {
	if code == "F5" || code == "F9" {
		return "конфиденциальное"
	}
	if code == taxonomy.CategoryQuestion {
		return "вопрос человеку"
	}
	return categoryName(code, tax)
}

// categoryName — название категории из таксономии; нет таксономии — код.
func categoryName(code string, tax *taxonomy.Taxonomy) string {
	if tax != nil {
		if c, ok := tax.Get(code); ok && c.Name != "" {
			return c.Name
		}
	}
	return code
}

// daysRu — «1 день», «3 дня», «5 дней».
func daysRu(n int) string {
	if n < 0 {
		n = -n
	}
	word := "дней"
	switch {
	case n%100 >= 11 && n%100 <= 14:
	case n%10 == 1:
		word = "день"
	case n%10 >= 2 && n%10 <= 4:
		word = "дня"
	}
	return fmt.Sprintf("%d %s", n, word)
}
