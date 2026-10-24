package services

import (
	"context"
	"fmt"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Дашборд руководителя HR (FEATURE_AUTH; ресерч role-dashboards §2, §6): «Сегодня» из 4 типов
// строк, 6 плиток, нагрузка команды (без времени ответа по человеку — только медиана по
// команде), карточка «Дайджест недели» в 3 строки. Зоны риска — из BuildOverview как есть,
// дайджест — BuildDigest без LLM-текста.

// Типы строк «Сегодня» руководителя HR.
const (
	RowTicketUnassigned = "unassigned_ticket" // обращение без исполнителя дольше 2 ч
	RowSLAOverdue       = "sla_overdue"       // просроченный SLA, с именем рекрутера
	RowNoRecruiter      = "no_recruiter"      // новички без рекрутера (одна строка на всех)
	RowThemeNoOwner     = "theme_no_owner"    // тема недели без ответственного
)

// Действия руководителя HR.
const (
	ActAssign      = "assign"   // «Назначить» исполнителя обращения
	ActReassign    = "reassign" // «Переназначить»
	ActAttach      = "pin"      // «Закрепить» новичков за рекрутером
	ActAssignOwner = "owner"    // «Назначить ответственного» за тему
)

// Плитки руководителя HR.
const (
	TileUnassigned     = "unassigned_tickets"
	TileSLAOverdue     = "sla_overdue"
	TileNoRecruiter    = "no_recruiter"
	TileRedZone        = "red_zone"
	TilePulseResponse  = "pulse_response"
	TileIPDecisionWeek = "ip_week"
)

// HRLeadTiles — порядок плиток.
var HRLeadTiles = []string{TileUnassigned, TileSLAOverdue, TileNoRecruiter, TileRedZone, TilePulseResponse, TileIPDecisionWeek}

// UnassignedAfter — обращение без исполнителя попадает в «Сегодня» через 2 ч.
const UnassignedAfter = 2 * time.Hour

// PulseResponse — плитка «Отклик на пульс» (30 дней, как в обзоре): Percent nil при Enough=false.
type PulseResponse struct {
	Sent, Answered int
	Percent        *int
	Enough         bool
}

// DigestLine — строка карточки «Дайджест недели». Value/Prev/Delta nil — мало данных
// (Enough=false, n < AnonymityMinGroup).
type DigestLine struct {
	Key    string // response | w | theme
	Label  string
	Value  *int
	Prev   *int
	Delta  *int
	N      int
	Enough bool
	Text   string // готовая строка карточки (правила малых чисел §6 уже применены)
}

// DigestCard — карточка «Дайджест недели».
type DigestCard struct {
	Week  string
	Lines []DigestLine
	Link  string
}

// HRLeadDashboard — GET /api/hrlead/dashboard.
type HRLeadDashboard struct {
	Today         []TodayRow
	TodayTotal    int
	Tiles         map[string]Tile
	PulseResponse PulseResponse
	Team          *Team
	DigestCard    *DigestCard
	BotDid        BotDid
}

// HRLeadDashboard — дашборд руководителя HR по площадке (пусто — все).
func (s *UserService) HRLeadDashboard(ctx context.Context, site string, digest *DigestService, overview *OverviewService) (*HRLeadDashboard, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	rows, err := s.repos.Overview().Load(ctx, site, overviewSince(now))
	if err != nil {
		return nil, err
	}
	users, err := s.repos.Users.List(ctx)
	if err != nil {
		return nil, err
	}
	team, err := s.Team(ctx)
	if err != nil {
		return nil, err
	}
	var card *DigestCard
	var themes []DigestThemeDelta
	if digest != nil {
		if dg, err := digest.Numbers(ctx, "", site); err == nil {
			card = BuildDigestCard(dg)
			themes = dg.Themes
		}
	}
	var ov *Overview
	if overview != nil {
		if ov, err = overview.Get(ctx, site); err != nil {
			return nil, err
		}
	}
	d := BuildHRLeadDashboard(rows, users, themes, ov, site, now, s.tax)
	d.Team, d.DigestCard = team, card
	// «Бот сам» — по всем новичкам площадки за сутки.
	ids := make([]uuid.UUID, 0, len(rows.Employees))
	for _, e := range rows.Employees {
		ids = append(ids, e.ID)
		if e.CatchupStartedAt != nil && now.Sub(*e.CatchupStartedAt) < BotDidHours*time.Hour {
			d.BotDid.CatchupSummaries++
		}
	}
	sig, err := s.repos.DashboardSignals(ctx, ids, now.Add(-BotDidHours*time.Hour))
	if err != nil {
		return nil, err
	}
	d.BotDid.Nudges = sig.AutoNudges
	return d, nil
}

// BuildHRLeadDashboard — «Сегодня» и плитки из строк. Чистая функция.
// ov — обзор (зоны риска, отклик на пульс) как есть; nil — посчитать по rows прежними правилами.
func BuildHRLeadDashboard(rows *repositories.OverviewRows, users []domain.User, themes []DigestThemeDelta, ov *Overview, site string, now time.Time, tax *taxonomy.Taxonomy) *HRLeadDashboard {
	d := &HRLeadDashboard{Today: []TodayRow{}, Tiles: emptyTiles(HRLeadTiles)}
	if rows == nil {
		rows = &repositories.OverviewRows{}
	}
	if ov == nil {
		x := BuildOverview(rows, site, now, tax)
		ov = &x
	}
	o := *ov
	names := map[uuid.UUID]string{}
	for _, u := range users {
		names[u.ID] = u.FullName
	}
	emp := map[uuid.UUID]domain.Employee{}
	for _, e := range rows.Employees {
		emp[e.ID] = e
	}
	var today []TodayRow
	for _, t := range rows.Tickets {
		e, ok := emp[t.EmployeeID]
		if !ok || t.IsTest || !isOpenTicket(t) {
			continue
		}
		site := domain.SiteOf(e.Site)
		day := DayNumber(e.StartDate, now, site)
		id := t.ID
		unassigned := t.Status == domain.TicketOpen && (t.AssigneeName == nil || *t.AssigneeName == "")
		overdue := slaRunning(t) && t.SLADueAt.Before(now)
		if unassigned {
			addTile(d.Tiles, TileUnassigned, e.ID)
		}
		if overdue {
			addTile(d.Tiles, TileSLAOverdue, e.ID)
		}
		topic := ticketTopic(t.Category, tax)
		base := TodayRow{NewcomerID: e.ID, Name: e.FullName, Site: e.Site, Day: day, TicketID: &id, Deadline: t.SLADueAt}
		switch {
		case overdue:
			who := e.RecruiterName
			if e.RecruiterID != nil && names[*e.RecruiterID] != "" {
				who = names[*e.RecruiterID]
			}
			r := base
			r.Type, r.Severity, r.RecruiterName = RowSLAOverdue, SevRed, who
			if who != "" {
				r.Reason = fmt.Sprintf("SLA обращения «%s» просрочен · рекрутер %s", topic, who)
			} else {
				r.Reason = fmt.Sprintf("SLA обращения «%s» просрочен · без рекрутера", topic)
			}
			r.Actions = []string{ActReassign, ActOpen}
			if unassigned {
				r.Actions = []string{ActAssign, ActOpen}
			}
			today = append(today, r)
		case unassigned && now.Sub(t.CreatedAt) >= UnassignedAfter:
			r := base
			r.Type, r.Severity = RowTicketUnassigned, SevYellow
			if t.Priority == taxonomy.P0 {
				r.Severity = SevRed
			}
			r.Reason = fmt.Sprintf("Обращение «%s» без исполнителя %s", topic, hoursRu(int(now.Sub(t.CreatedAt).Hours())))
			r.Actions = []string{ActAssign, ActOpen}
			today = append(today, r)
		}
	}
	redIf(d.Tiles, TileSLAOverdue, true)

	// Новички без рекрутера — одна строка на всех; завершившие адаптацию не считаются.
	noRec := TodayRow{Type: RowNoRecruiter, Severity: SevYellow, Actions: []string{ActAttach}, IDs: []uuid.UUID{}}
	for _, e := range rows.Employees {
		if e.RecruiterID != nil || domain.StageForDay(DayNumber(e.StartDate, now, domain.SiteOf(e.Site))) == domain.StageDone {
			continue
		}
		addTile(d.Tiles, TileNoRecruiter, e.ID)
		noRec.IDs = append(noRec.IDs, e.ID)
	}
	if n := len(noRec.IDs); n > 0 {
		noRec.Count = n
		noRec.Reason = fmt.Sprintf("Без рекрутера: %d", n)
		today = append(today, noRec)
	}

	// Тема недели без ответственного (видимые темы — уже ≥ 5 человек).
	for _, th := range themes {
		if th.Owner != nil || th.Newcomers < AnonymityMinGroup {
			continue
		}
		reason := fmt.Sprintf("Тема недели «%s»: %d новичков", th.Label, th.Newcomers)
		if th.Delta != nil && *th.Delta != 0 {
			reason += fmt.Sprintf(" (%s)", signed(*th.Delta))
		}
		today = append(today, TodayRow{Type: RowThemeNoOwner, Severity: SevYellow, Reason: reason, Theme: th.Label,
			Count: th.Newcomers, Actions: []string{ActAssignOwner}, IDs: []uuid.UUID{}})
	}

	// Красная зона — зоны обзора как есть.
	for _, r := range o.Risks {
		if r.Zone == domain.RiskRed {
			addTile(d.Tiles, TileRedZone, r.NewcomerID)
		}
	}
	redIf(d.Tiles, TileRedZone, true)

	// Решения по ИС на этой неделе.
	for _, e := range rows.Employees {
		loc := domain.SiteOf(e.Site).Loc
		dToday := civilDate(now.In(loc), loc)
		wTo := weekStart(now, loc).AddDate(0, 0, 6)
		d90 := civilDate(e.StartDate, loc).AddDate(0, 0, 90)
		if !d90.Before(dToday) && !d90.After(wTo) {
			addTile(d.Tiles, TileIPDecisionWeek, e.ID)
		}
	}

	// Отклик на пульс — как в обзоре (30 дней); мало данных — без процента.
	pr := PulseResponse{Sent: o.Tiles.PulseSent, Answered: o.Tiles.PulseAnswered, Enough: o.Tiles.PulseSent >= AnonymityMinGroup}
	if pr.Enough {
		pr.Percent = o.Tiles.PulsePercent
	}
	d.PulseResponse = pr
	t := d.Tiles[TilePulseResponse]
	t.Value, t.Secondary = pr.Answered, pr.Sent
	d.Tiles[TilePulseResponse] = t

	sortToday(today)
	d.TodayTotal = len(today)
	if len(today) > TodayLimit {
		today = today[:TodayLimit]
	}
	d.Today = append(d.Today, today...)
	return d
}

// hoursRu — «3 ч» / «2 дня» для возраста обращения.
func hoursRu(h int) string {
	if h < 48 {
		return fmt.Sprintf("%d ч", h)
	}
	return daysRu(h / 24)
}

// BuildDigestCard — 3 строки из агрегатов дайджеста: отклик, индекс W, главная тема. Малые
// группы (n < 5) — без чисел.
func BuildDigestCard(dg *Digest) *DigestCard {
	if dg == nil {
		return nil
	}
	c := &DigestCard{Week: dg.Week, Link: dg.Link}
	resp := DigestLine{Key: "response", Label: "Ответили на пульс", N: dg.Totals.OnPulse, Enough: dg.Totals.OnPulse >= AnonymityMinGroup}
	if resp.Enough {
		resp.Value = dg.Totals.ResponseRatePct
		resp.Prev = dg.Prev.ResponseRatePct
		resp.Delta = deltaPtr(resp.Value, resp.Prev)
	}
	w := DigestLine{Key: "w", Label: "Индекс W", N: dg.Totals.Respondents, Enough: dg.Totals.WAvg != nil}
	if w.Enough {
		w.Value, w.Prev = dg.Totals.WAvg, dg.Prev.WAvg
		w.Delta = deltaPtr(w.Value, w.Prev)
	}
	theme := DigestLine{Key: "theme", Label: "Главная тема"}
	if len(dg.Themes) > 0 {
		th := dg.Themes[0]
		n := th.Newcomers
		theme.Label, theme.N, theme.Enough = th.Label, n, n >= AnonymityMinGroup
		if theme.Enough {
			theme.Value, theme.Prev, theme.Delta = &n, th.PrevCount, th.Delta
		}
	}
	if resp.Enough {
		resp.Text = fmt.Sprintf("Ответили на пульс: %d из %d", dg.Totals.Answered, dg.Totals.OnPulse)
		if resp.N >= 20 && resp.Value != nil {
			resp.Text = fmt.Sprintf("Ответили на пульс: %d %%", *resp.Value)
		}
		resp.Text += arrow(resp.Delta, " п.п.")
	} else {
		resp.Text = "Ответили на пульс: мало данных"
	}
	if w.Enough {
		w.Text = fmt.Sprintf("Индекс W: %d", *w.Value) + arrow(w.Delta, "")
	} else {
		w.Text = "Индекс W: мало данных"
	}
	switch {
	case len(dg.Themes) == 0:
		theme.Text = "Тем недели нет"
	case theme.Enough:
		theme.Text = fmt.Sprintf("Главная тема: «%s» — %d новичков", theme.Label, theme.N) + arrow(theme.Delta, "")
	default:
		theme.Text = "Главная тема: мало данных"
	}
	c.Lines = []DigestLine{resp, w, theme}
	return c
}

// arrow — абсолютная дельта со стрелкой: « (↑ 3)», « (↓ 2)»; нет или ноль — пусто.
func arrow(d *int, unit string) string {
	switch {
	case d == nil || *d == 0:
		return ""
	case *d > 0:
		return fmt.Sprintf(" (↑ %d%s)", *d, unit)
	}
	return fmt.Sprintf(" (↓ %d%s)", -*d, unit)
}

func deltaPtr(v, prev *int) *int {
	if v == nil || prev == nil {
		return nil
	}
	d := *v - *prev
	return &d
}

// Numbers — цифры дайджеста недели без LLM-текста (карточка дашборда не ждёт модель).
func (s *DigestService) Numbers(ctx context.Context, week, site string) (*Digest, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	if week == "" {
		week = s.CurrentWeek()
	}
	from, err := ParseISOWeek(week)
	if err != nil {
		return nil, err
	}
	prevFrom := from.AddDate(0, 0, -7)
	rows, err := s.repos.Overview().Load(ctx, site, prevFrom.AddDate(0, 0, -28))
	if err != nil {
		return nil, err
	}
	texts, err := s.repos.Overview().DigestTexts(ctx, site, prevFrom, from.AddDate(0, 0, 7))
	if err != nil {
		return nil, err
	}
	d := BuildDigest(rows, texts, site, from, s.now(), s.tax)
	d.Link = s.link(week, site)
	return d, nil
}
