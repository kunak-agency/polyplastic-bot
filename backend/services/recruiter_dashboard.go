package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Дашборд рекрутера (FEATURE_AUTH; ресерч agency/playbook/artifacts/research/
// 2026-10-24-role-dashboards.md §1, §6; контракт agency/system/api_contract_auth.md): «Сегодня»
// (до 10 строк, где автоматика исчерпана), 6 плиток-фильтров, «бот сам», пульс недели. Строки
// — те же, что у обзора (OverviewRepo.Load), пороги и помощники — overview.go: pulseValue,
// LowPulse, slaRunning, isOpenTicket, weekStart, AnonymityMinGroup. Текстов ответов нет.

// Типы строк «Сегодня» рекрутера.
const (
	RowInviteNotOpened = "invite_not_opened" // выход завтра или прошёл, приглашение не открыто
	RowSLA             = "sla"               // SLA обращения истекает сегодня или просрочен
	RowF7              = "f7_consent"        // намерение уйти, новичок согласился на передачу
	RowSilent          = "silent"            // не отвечает N дней после напоминаний
	RowNoManagerPlan   = "no_plan"           // выход сегодня, а плана от руководителя нет
	RowIPDecision      = "ip_decision"       // решение по ИС через 3 дня или раньше
	RowCatchupBehind   = "catchup_behind"    // догоняет и отстаёт
)

// Действия строки (кнопки фронта).
const (
	ActContact = "contact" // композер + «Отметить: связался»
	ActNudge   = "remind"  // «Напомнить»: POST /api/newcomers/{id}/remind
	ActTake    = "take"    // «Взять» обращение
	ActOpen    = "open"    // «Открыть» карточку / обращение
)

// Степени.
const (
	SevRed    = "red"
	SevYellow = "yellow"
	SevNone   = "none" // плитка: не горит (ноль фронт красит серым сам)
)

// Ключи плиток рекрутера.
const (
	TileNotAnswering = "not_answering"
	TileSLA          = "sla"
	TileStarting     = "starting"
	TileIPDecisions  = "ip_decisions"
	TileLowPulseWeek = "low_pulse"
	TileCatchingUp   = "catching_up"
)

// RecruiterTiles — порядок плиток.
var RecruiterTiles = []string{TileNotAnswering, TileSLA, TileStarting, TileIPDecisions, TileLowPulseWeek, TileCatchingUp}

// Пороги (стартовые, ресерч §1).
const (
	TodayLimit          = 10 // строк «Сегодня»
	SilentDays          = 3  // «не отвечает боту > 3 дней» без статуса после наджей
	SilentRedWorkdays   = 5  // «не отвечает» ≥ 5 рабочих дней — красная
	NotAnsweringRedMin  = 3  // плитка «Не отвечают» красная от 3
	IPDecisionDays      = 7  // плитка «Решения по ИС ≤ 7 дней»
	IPDecisionRowDays   = 3  // строка — за 3 дня и меньше (D ≥ 87)
	CatchupBehindDays   = 2  // догоняет и отстаёт на 2 дня и больше
	LowPulseRedPct      = 20 // плитка «Низкий пульс» красная от 20 % ответивших
	ContactHideWorkdays = 2  // «Отметить: связался» прячет строки новичка на 2 рабочих дня
	BotDidHours         = 24 // «бот сам» — за последние сутки
)

// DashboardScope — чьи новички: RecruiterID — рекрутера, Unassigned — без рекрутера, оба пустые
// — все (hr_lead).
type DashboardScope struct {
	RecruiterID *uuid.UUID
	Unassigned  bool
}

func (sc DashboardScope) match(e domain.Employee) bool {
	switch {
	case sc.Unassigned:
		return e.RecruiterID == nil
	case sc.RecruiterID != nil:
		return e.RecruiterID != nil && *e.RecruiterID == *sc.RecruiterID
	}
	return true
}

// TodayRow — строка «Сегодня».
type TodayRow struct {
	Type       string
	Severity   string
	NewcomerID uuid.UUID
	Name       string
	Site       string
	Day        int
	Deadline   *time.Time
	Reason     string
	Actions    []string
	TicketID   *uuid.UUID
	Count      int         // агрегатная строка (hr_lead «без рекрутера»): сколько новичков
	IDs        []uuid.UUID // агрегатная строка: какие
	// RecruiterName — hr_lead sla_overdue; Theme — hr_lead theme_no_owner.
	RecruiterName string
	Theme         string
}

// Tile — плитка-фильтр: число (Value), второе число парной плитки (Secondary: «истекает /
// просрочен», «сегодня / на неделе», «ответили / отправлено»), красная ли (порог считает сервер),
// новички под плиткой.
type Tile struct {
	Value     int
	Secondary int
	Red       bool
	IDs       []uuid.UUID
}

// CatchupProgressView — «догоняет N из M».
type CatchupProgressView struct {
	Done  int
	Total int
}

// DashboardItem — факты по новичку для чипов списка «мои».
type DashboardItem struct {
	NewcomerID        uuid.UUID
	SilentDays        *int
	Catchup           *CatchupProgressView
	LowPulse          *int
	IPDecisionDate    *time.Time
	TicketsOpen       int
	TicketsSLAOverdue int
	ContactedAt       *time.Time
	Zone              string // red | yellow | "" — зона риска обзора как есть
}

// BotDid — что автоматика сделала сама за сутки.
type BotDid struct {
	Nudges           int // авто-напоминания (nudges kind=auto, ушедшие)
	CatchupSummaries int // сводки «Что вы пропустили» (собранные догоняющие очереди)
}

// PulseWeek — пульс за неделю: Enough=false при меньше AnonymityMinGroup ответивших.
type PulseWeek struct {
	Sent     int // новичков, кому на неделе уходил опрос
	Answered int // из них ответили
	Low      int // с низким пульсом
	Enough   bool
	Tone     ToneCell // тон текстовых ответов недели; Enough=false — n < 5
}

// RecruiterDashboard — GET /api/recruiter/dashboard.
type RecruiterDashboard struct {
	Today      []TodayRow
	TodayTotal int
	Tiles      map[string]Tile
	BotDid     BotDid
	PulseWeek  PulseWeek
	Items      []DashboardItem
	// Пустое состояние (ресерч §1): сколько новичков ведёт бот, следующее событие.
	Newcomers int
	NextEvent *NextEvent
}

// NextEvent — ближайшее запланированное сообщение бота.
type NextEvent struct {
	Title        string
	At           time.Time
	NewcomerName string
}

// dashData — всё, из чего считается дашборд.
type dashData struct {
	rows    *repositories.OverviewRows
	events  []repositories.DashboardEvent
	signals *repositories.DashboardSignals
}

// loadDash — строки обзора по scope и сигналы.
func loadDash(ctx context.Context, repos *repositories.Repos, sc DashboardScope, site string, now time.Time) (*dashData, error) {
	rows, err := repos.Overview().Load(ctx, site, now.AddDate(0, 0, -14))
	if err != nil {
		return nil, err
	}
	rows.Employees = filterEmployees(rows.Employees, sc.match)
	ids := make([]uuid.UUID, len(rows.Employees))
	for i, e := range rows.Employees {
		ids[i] = e.ID
	}
	events, err := repos.Events.DashboardEvents(ctx, ids)
	if err != nil {
		return nil, err
	}
	sig, err := repos.DashboardSignals(ctx, ids, now.Add(-BotDidHours*time.Hour))
	if err != nil {
		return nil, err
	}
	return &dashData{rows: rows, events: events, signals: sig}, nil
}

// RecruiterDashboard — дашборд по scope и площадке (пусто — все).
// overview — зоны риска как есть (Overview.Risks); nil — без зон.
func (s *UserService) RecruiterDashboard(ctx context.Context, sc DashboardScope, site string, overview *OverviewService) (*RecruiterDashboard, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	d, err := loadDash(ctx, s.repos, sc, site, now)
	if err != nil {
		return nil, err
	}
	zones := map[uuid.UUID]string{}
	if overview != nil {
		o, err := overview.Get(ctx, site)
		if err != nil {
			return nil, err
		}
		for _, r := range o.Risks {
			zones[r.NewcomerID] = r.Zone
		}
	}
	return BuildRecruiterDashboard(d.rows, d.events, d.signals, zones, now, s.tax), nil
}

func filterEmployees(list []domain.Employee, keep func(domain.Employee) bool) []domain.Employee {
	out := list[:0:0]
	for _, e := range list {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// workdaysSince — сколько рабочих дней площадки прошло после from до now (from не считается).
func workdaysSince(from, now time.Time, site domain.Site) int {
	d := civilDate(from.In(site.Loc), site.Loc).AddDate(0, 0, 1)
	end := civilDate(now.In(site.Loc), site.Loc)
	n := 0
	for i := 0; !d.After(end) && i < 400; i++ {
		if site.IsWorkDate(d) {
			n++
		}
		d = d.AddDate(0, 0, 1)
	}
	return n
}

// ContactHiddenUntil — до какого момента строки новичка скрыты после «связался».
func ContactHiddenUntil(at time.Time, site domain.Site) time.Time {
	d := civilDate(at.In(site.Loc), site.Loc)
	for i := 0; i < ContactHideWorkdays; i++ {
		d = nextWorkday(d.AddDate(0, 0, 1), site)
	}
	return d.AddDate(0, 0, 1)
}

// person — новичок с собранными фактами.
type person struct {
	e        domain.Employee
	site     domain.Site
	day      int
	silent   int // дней; -1 — отвечает
	silentWD int // рабочих дней (для степени)
	catchup  *CatchupProgressView
	behind   int // на сколько дней отстаёт догоняющая очередь
	lowPulse *int
	pulseAt  time.Time
	d90      time.Time
	start    time.Time
	tickets  []repositories.OverviewTicket
	hidden   bool // «связался» недавно
}

// collect — факты по новичкам (без завершивших адаптацию). Общая часть дашбордов ролей.
func collect(rows *repositories.OverviewRows, events []repositories.DashboardEvent, sig *repositories.DashboardSignals, now time.Time) []*person {
	if rows == nil {
		return nil
	}
	if sig == nil {
		sig = &repositories.DashboardSignals{}
	}
	byEmp := map[uuid.UUID][]repositories.DashboardEvent{}
	for _, ev := range events {
		byEmp[ev.EmployeeID] = append(byEmp[ev.EmployeeID], ev)
	}
	ticketsBy := map[uuid.UUID][]repositories.OverviewTicket{}
	for _, t := range rows.Tickets {
		if !t.IsTest && isOpenTicket(t) {
			ticketsBy[t.EmployeeID] = append(ticketsBy[t.EmployeeID], t)
		}
	}
	// Пульс по опросу — среднее ответов одного события; у новичка — последний по времени.
	type pulse struct {
		emp  uuid.UUID
		sum  float64
		n    int
		last time.Time
	}
	pulses := map[uuid.UUID]*pulse{}
	for _, a := range rows.Answers {
		if a.IsTest {
			continue
		}
		v, ok := pulseValue(a)
		if !ok {
			continue
		}
		p := pulses[a.EventID]
		if p == nil {
			p = &pulse{emp: a.EmployeeID}
			pulses[a.EventID] = p
		}
		p.sum += v
		p.n++
		if a.CreatedAt.After(p.last) {
			p.last = a.CreatedAt
		}
	}
	lastPulse := map[uuid.UUID]*pulse{}
	for _, p := range pulses {
		if cur := lastPulse[p.emp]; cur == nil || p.last.After(cur.last) {
			lastPulse[p.emp] = p
		}
	}

	var out []*person
	for _, e := range rows.Employees {
		site := domain.SiteOf(e.Site)
		day := DayNumber(e.StartDate, now, site)
		if domain.StageForDay(day) == domain.StageDone {
			continue
		}
		p := &person{e: e, site: site, day: day, silent: -1, tickets: ticketsBy[e.ID],
			start: civilDate(e.StartDate, site.Loc)}
		p.d90 = p.start.AddDate(0, 0, 90)
		var oldest *time.Time
		done, total := 0, 0
		for _, ev := range byEmp[e.ID] {
			if ev.Catchup {
				total++
				if ev.Status == domain.EventAnswered || ev.Status == domain.EventSkipped || ev.Status == domain.EventFailed ||
					(ev.Status == domain.EventSent && ev.Waiting == "") {
					done++
				}
			}
			if ev.IsTest || ev.Status != domain.EventSent || ev.SentAt == nil || e.Connection == domain.ConnStopped {
				continue
			}
			if ev.Waiting != "" || ev.Kind == domain.KindSurvey {
				if oldest == nil || ev.SentAt.Before(*oldest) {
					oldest = ev.SentAt
				}
			}
		}
		// «Не отвечает»: статус после второго наджа (планировщик) или вопрос висит > 3 дней.
		var since *time.Time
		if e.UnresponsiveSince != nil {
			since = e.UnresponsiveSince
		}
		if oldest != nil && now.Sub(*oldest) > SilentDays*24*time.Hour && (since == nil || oldest.Before(*since)) {
			since = oldest
		}
		if since != nil {
			p.silent = int(now.Sub(*since).Hours() / 24)
			p.silentWD = workdaysSince(*since, now, site)
		}
		if e.CatchupStartedAt != nil && e.CatchupDoneAt == nil {
			p.catchup = &CatchupProgressView{Done: done, Total: total}
			// Очередь — один блок в день: за N дней ожидалось N+1 ответов.
			expected := int(now.Sub(*e.CatchupStartedAt).Hours()/24) + 1
			if expected > total {
				expected = total
			}
			p.behind = expected - done
		}
		if lp := lastPulse[e.ID]; lp != nil {
			if avg := lp.sum / float64(lp.n); avg <= LowPulse {
				v := int(avg + 0.5)
				p.lowPulse = &v
			}
			p.pulseAt = lp.last
		}
		if at, ok := sig.Contacted[e.ID]; ok && now.Before(ContactHiddenUntil(at, site)) {
			p.hidden = true
		}
		out = append(out, p)
	}
	return out
}

// tile — добавить новичка в плитку.
func addTile(tiles map[string]Tile, key string, id uuid.UUID) {
	t := tiles[key]
	t.Value++
	t.IDs = append(t.IDs, id)
	tiles[key] = t
}

func redIf(tiles map[string]Tile, key string, cond bool) {
	t := tiles[key]
	if cond && (t.Value > 0 || t.Secondary > 0) {
		t.Red = true
	}
	tiles[key] = t
}

func emptyTiles(keys []string) map[string]Tile {
	m := map[string]Tile{}
	for _, k := range keys {
		m[k] = Tile{IDs: []uuid.UUID{}}
	}
	return m
}

// sortToday — красные выше жёлтых, дальше по дедлайну (без дедлайна — ниже), затем по D.
func sortToday(rows []TodayRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Severity == SevRed) != (b.Severity == SevRed) {
			return a.Severity == SevRed
		}
		switch {
		case a.Deadline != nil && b.Deadline != nil && !a.Deadline.Equal(*b.Deadline):
			return a.Deadline.Before(*b.Deadline)
		case (a.Deadline == nil) != (b.Deadline == nil):
			return a.Deadline != nil
		}
		return a.Day > b.Day
	})
}

// BuildRecruiterDashboard — дашборд из строк. Чистая функция: её проверяют тесты без базы.
func BuildRecruiterDashboard(rows *repositories.OverviewRows, events []repositories.DashboardEvent, sig *repositories.DashboardSignals, zones map[uuid.UUID]string, now time.Time, tax *taxonomy.Taxonomy) *RecruiterDashboard {
	d := &RecruiterDashboard{Today: []TodayRow{}, Tiles: emptyTiles(RecruiterTiles), Items: []DashboardItem{}}
	if sig == nil {
		sig = &repositories.DashboardSignals{}
	}
	d.BotDid.Nudges = sig.AutoNudges
	people := collect(rows, events, sig, now)
	d.Newcomers = len(people)
	inScope := map[uuid.UUID]string{}
	for _, p := range people {
		inScope[p.e.ID] = p.e.FullName
	}
	for _, ev := range events {
		name, ok := inScope[ev.EmployeeID]
		if ok && ev.Status == domain.EventScheduled && ev.FireAt.After(now) && !ev.IsTest {
			if d.NextEvent == nil || ev.FireAt.Before(d.NextEvent.At) {
				d.NextEvent = &NextEvent{Title: ev.Title, At: ev.FireAt, NewcomerName: name}
			}
		}
	}
	var today []TodayRow
	lowAnswered := map[uuid.UUID]bool{}
	for _, p := range people {
		e := p.e
		loc := p.site.Loc
		dToday := civilDate(now.In(loc), loc)
		endOfToday := dToday.AddDate(0, 0, 1)
		wTo := weekStart(now, loc).AddDate(0, 0, 7)
		row := func(typ, sev, reason string, deadline *time.Time, ticket *uuid.UUID, acts ...string) {
			today = append(today, TodayRow{Type: typ, Severity: sev, NewcomerID: e.ID, Name: e.FullName, Site: e.Site,
				Day: p.day, Deadline: deadline, Reason: reason, Actions: acts, TicketID: ticket})
		}
		it := DashboardItem{NewcomerID: e.ID, Zone: zones[e.ID]}
		if at, ok := sig.Contacted[e.ID]; ok {
			at := at
			it.ContactedAt = &at
		}

		// Обращения: SLA сегодня или просрочен; F7 с согласием на передачу.
		slaHot := false
		for _, t := range p.tickets {
			it.TicketsOpen++
			id := t.ID
			if slaRunning(t) && t.SLADueAt.Before(endOfToday) {
				due := *t.SLADueAt
				slaHot = true
				tile := d.Tiles[TileSLA]
				if due.Before(now) {
					tile.Secondary++ // просрочено
					tile.Red = true
				} else {
					tile.Value++ // истекает сегодня
				}
				d.Tiles[TileSLA] = tile
				if due.Before(now) {
					it.TicketsSLAOverdue++
					row(RowSLA, SevRed, fmt.Sprintf("SLA обращения «%s» просрочен", ticketTopic(t.Category, tax)), &due, &id, ActTake, ActOpen)
				} else {
					row(RowSLA, SevYellow, fmt.Sprintf("SLA обращения «%s» истекает сегодня", ticketTopic(t.Category, tax)), &due, &id, ActTake, ActOpen)
				}
			}
			// Обращение F7 создаётся только после согласия новичка на передачу (bot_tickets.go).
			if t.Category == "F7" {
				row(RowF7, SevRed, "Намерение уйти: новичок согласился на разговор", t.SLADueAt, &id, ActContact, ActOpen)
			}
		}
		if slaHot {
			tile := d.Tiles[TileSLA]
			tile.IDs = append(tile.IDs, e.ID)
			d.Tiles[TileSLA] = tile
		}

		// Приглашение не открыто, а выход завтра или уже прошёл.
		inviteRed := e.Connection == domain.ConnInvited && p.day >= -1
		if inviteRed && !p.hidden {
			reason := "Выход завтра, приглашение не открыто"
			if p.day >= 0 {
				reason = "Уже вышел(ла), приглашение не открыто"
			}
			start := p.start
			row(RowInviteNotOpened, SevRed, reason, &start, nil, ActContact, ActOpen)
		}

		// Не отвечает.
		if p.silent >= 0 {
			it.SilentDays = &p.silent
			addTile(d.Tiles, TileNotAnswering, e.ID)
			if !p.hidden {
				sev := SevYellow
				if p.silentWD >= SilentRedWorkdays {
					sev = SevRed
				}
				row(RowSilent, sev, "Не отвечает боту "+daysRu(p.silent)+" после напоминаний", nil, nil, ActContact, ActNudge, ActOpen)
			}
		}

		// Выход сегодня, а плана от руководителя нет.
		if p.day == 0 && !sig.Planned[e.ID] && !p.hidden {
			row(RowNoManagerPlan, SevYellow, "Выход сегодня, руководитель не подтвердил план первого дня", nil, nil, ActContact, ActOpen)
		}

		// Выходит сегодня (Value) или на неделе (Secondary, вместе с сегодняшними).
		if !p.start.Before(dToday) && p.start.Before(wTo) {
			tile := d.Tiles[TileStarting]
			if p.day == 0 {
				tile.Value++
			}
			tile.Secondary++
			tile.IDs = append(tile.IDs, e.ID)
			if inviteRed {
				tile.Red = true
			}
			d.Tiles[TileStarting] = tile
		}

		// Решения по ИС без решения (Employee.IPDecision): D+90 в ближайшие 7 дней; строка — за 3 дня и меньше.
		if e.IPDecision == nil && !p.d90.Before(dToday) && !p.d90.After(dToday.AddDate(0, 0, IPDecisionDays)) {
			d90 := p.d90
			it.IPDecisionDate = &d90
			addTile(d.Tiles, TileIPDecisions, e.ID)
			near := !p.d90.After(dToday.AddDate(0, 0, IPDecisionRowDays))
			redIf(d.Tiles, TileIPDecisions, near)
			if near && !p.hidden {
				row(RowIPDecision, SevYellow, "Решение по испытательному сроку "+d90.Format("02.01"), &d90, nil, ActOpen)
			}
		}

		// Низкий пульс за неделю.
		if p.lowPulse != nil && !p.pulseAt.Before(weekStart(now, loc)) {
			it.LowPulse = p.lowPulse
			addTile(d.Tiles, TileLowPulseWeek, e.ID)
			lowAnswered[e.ID] = true
		}

		// Догоняет.
		if p.catchup != nil {
			it.Catchup = p.catchup
			addTile(d.Tiles, TileCatchingUp, e.ID)
			redIf(d.Tiles, TileCatchingUp, p.behind >= CatchupBehindDays)
			if p.behind >= CatchupBehindDays && !p.hidden {
				row(RowCatchupBehind, SevYellow, fmt.Sprintf("Догоняет %d из %d, отстаёт на %s", p.catchup.Done, p.catchup.Total, daysRu(p.behind)), nil, nil, ActContact, ActOpen)
			}
		}
		if e.CatchupStartedAt != nil && now.Sub(*e.CatchupStartedAt) < BotDidHours*time.Hour {
			d.BotDid.CatchupSummaries++
		}

		if it.SilentDays != nil || it.Catchup != nil || it.LowPulse != nil || it.IPDecisionDate != nil || it.TicketsOpen > 0 || it.ContactedAt != nil {
			d.Items = append(d.Items, it)
		}
	}
	redIf(d.Tiles, TileNotAnswering, d.Tiles[TileNotAnswering].Value >= NotAnsweringRedMin)

	d.PulseWeek = pulseWeek(rows, people, now)
	d.PulseWeek.Low = len(lowAnswered)
	if rows != nil {
		d.PulseWeek.Tone = toneWeek(rows, "", weekStart(now, domain.SiteOf("").Loc), domain.SiteOf("").Loc)
	}
	if d.PulseWeek.Answered > 0 {
		redIf(d.Tiles, TileLowPulseWeek, d.PulseWeek.Low*100 >= LowPulseRedPct*d.PulseWeek.Answered)
	}

	sortToday(today)
	d.TodayTotal = len(today)
	if len(today) > TodayLimit {
		today = today[:TodayLimit]
	}
	d.Today = append(d.Today, today...)
	return d
}

// pulseWeek — кому на этой неделе уходил опрос и кто ответил (новичков, не событий).
func pulseWeek(rows *repositories.OverviewRows, people []*person, now time.Time) PulseWeek {
	var pw PulseWeek
	if rows == nil {
		return pw
	}
	from := weekStart(now, domain.SiteOf("").Loc)
	in := map[uuid.UUID]bool{}
	for _, p := range people {
		in[p.e.ID] = true
	}
	sent, answered := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, ev := range rows.Events {
		if !in[ev.EmployeeID] || ev.IsTest || ev.Kind != domain.KindSurvey || ev.SentAt == nil || ev.SentAt.Before(from) {
			continue
		}
		sent[ev.EmployeeID] = true
		if ev.Status == domain.EventAnswered {
			answered[ev.EmployeeID] = true
		}
	}
	pw.Sent, pw.Answered = len(sent), len(answered)
	pw.Enough = pw.Answered >= AnonymityMinGroup
	return pw
}

// MarkContacted — «Отметить: связался»: строка в журнале nudges (kind=contacted, без сообщения).
// Строки «Сегодня» этого новичка скрываются на 2 рабочих дня.
func (s *UserService) MarkContacted(ctx context.Context, employeeID uuid.UUID) (time.Time, time.Time, error) {
	if !s.repos.Enabled() {
		return time.Time{}, time.Time{}, repositories.ErrNoDatabase
	}
	e, err := s.repos.Employees.ByID(ctx, employeeID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if e == nil {
		return time.Time{}, time.Time{}, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	now := s.now()
	n := &domain.Nudge{EmployeeID: e.ID, Kind: repositories.NudgeContacted, SentAt: now}
	if v := ViewerFrom(ctx); v != nil {
		id := v.ID
		n.CreatedBy = &id
	}
	if err := s.repos.Nudges.Create(ctx, n); err != nil {
		return time.Time{}, time.Time{}, err
	}
	return now, ContactHiddenUntil(now, domain.SiteOf(e.Site)), nil
}
