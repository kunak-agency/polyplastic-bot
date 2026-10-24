package out

import (
	"time"

	"main/domain"
	"main/services"

	"github.com/google/uuid"
)

// Вход, пользователи, команда, дашборды ролей (agency/system/api_contract_auth.md).

// User — пользователь кабинета; хэш пароля наружу не выходит.
type User struct {
	ID          string  `json:"id"`
	Login       string  `json:"login"`
	FullName    string  `json:"full_name"`
	Role        string  `json:"role"`
	TZ          *string `json:"tz"`
	ManagerName *string `json:"manager_name"`
	Active      bool    `json:"active"`
}

func UserFrom(u domain.User) User {
	return User{ID: u.ID.String(), Login: u.Login, FullName: u.FullName, Role: u.Role, TZ: u.TZ, ManagerName: u.ManagerName, Active: u.Active}
}

func UsersFrom(list []domain.User) []User {
	out := make([]User, 0, len(list))
	for _, u := range list {
		out = append(out, UserFrom(u))
	}
	return out
}

func ids(list []uuid.UUID) []string {
	out := make([]string, 0, len(list))
	for _, id := range list {
		out = append(out, id.String())
	}
	return out
}

func isoPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

// TeamMember — строка нагрузки (без времени ответа по человеку).
type TeamMember struct {
	UserID            string `json:"user_id"`
	FullName          string `json:"full_name"`
	Login             string `json:"login"`
	Role              string `json:"role"`
	Active            bool   `json:"active"`
	Newcomers         int    `json:"newcomers"`
	TicketsOpen       int    `json:"tickets_open"`
	TicketsSLAOverdue int    `json:"tickets_sla_overdue"`
}

// Team — GET /api/team.
type Team struct {
	Items      []TeamMember `json:"items"`
	Total      int          `json:"total"`
	Unassigned int          `json:"unassigned"`
	// TeamMedianFirstResponseMin — медиана первого ответа по команде за 30 дней; null — мало
	// данных (Enough=false, меньше 5 ответов).
	TeamMedianFirstResponseMin *int `json:"team_median_first_response_min"`
	TeamMedianN                int  `json:"team_median_n"`
	TeamMedianEnough           bool `json:"team_median_enough"`
}

func TeamFrom(t *services.Team) Team {
	o := Team{Items: []TeamMember{}}
	if t == nil {
		return o
	}
	for _, m := range t.Members {
		o.Items = append(o.Items, TeamMember{UserID: m.User.ID.String(), FullName: m.User.FullName, Login: m.User.Login,
			Role: m.User.Role, Active: m.User.Active, Newcomers: m.Newcomers, TicketsOpen: m.TicketsOpen, TicketsSLAOverdue: m.TicketsSLAOverdue})
	}
	o.Total, o.Unassigned = len(o.Items), t.Unassigned
	o.TeamMedianFirstResponseMin, o.TeamMedianN = t.MedianFirstResponseMin, t.MedianN
	o.TeamMedianEnough = t.MedianFirstResponseMin != nil
	return o
}

// TodayRow — строка «Сегодня» (api_contract_auth.md, «Дашборды по ролям»).
type TodayRow struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Zone          string   `json:"zone"` // red | yellow
	Reason        string   `json:"reason"`
	NewcomerID    *string  `json:"newcomer_id"`
	NewcomerName  *string  `json:"newcomer_name"`
	Site          *string  `json:"site"`
	TicketID      *string  `json:"ticket_id"`
	RecruiterName *string  `json:"recruiter_name"`
	Theme         *string  `json:"theme"`
	DueAt         *string  `json:"due_at"`
	Day           *int     `json:"day"`
	Actions       []string `json:"actions"`
	Count         int      `json:"count,omitempty"`
	NewcomerIDs   []string `json:"newcomer_ids,omitempty"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func todayFrom(list []services.TodayRow) []TodayRow {
	out := make([]TodayRow, 0, len(list))
	for _, r := range list {
		row := TodayRow{Kind: r.Type, Zone: r.Severity, Reason: r.Reason, Actions: r.Actions, DueAt: isoPtr(r.Deadline),
			Count: r.Count, RecruiterName: strPtr(r.RecruiterName), Theme: strPtr(r.Theme)}
		if row.Actions == nil {
			row.Actions = []string{}
		}
		row.ID = r.Type
		if r.NewcomerID != uuid.Nil {
			id, day := r.NewcomerID.String(), r.Day
			row.NewcomerID, row.NewcomerName, row.Site, row.Day = &id, strPtr(r.Name), strPtr(r.Site), &day
			row.ID += ":" + id
		}
		if r.TicketID != nil {
			t := r.TicketID.String()
			row.TicketID = &t
			row.ID += ":" + t
		}
		if r.Theme != "" {
			row.ID += ":" + r.Theme
		}
		if len(r.IDs) > 0 {
			row.NewcomerIDs = ids(r.IDs)
		}
		out = append(out, row)
	}
	return out
}

// Tile — плитка: value, secondary (второе число парной плитки), red (порог считает сервер), ids.
type Tile struct {
	Value     int      `json:"value"`
	Secondary *int     `json:"secondary,omitempty"`
	Red       bool     `json:"red"`
	IDs       []string `json:"ids"`
}

// pairedTiles — плитки с двумя числами.
var pairedTiles = map[string]bool{services.TileSLA: true, services.TileStarting: true, services.TilePulseResponse: true}

func tilesFrom(m map[string]services.Tile) map[string]Tile {
	out := map[string]Tile{}
	for k, t := range m {
		x := Tile{Value: t.Value, Red: t.Red, IDs: ids(t.IDs)}
		if pairedTiles[k] {
			sec := t.Secondary
			x.Secondary = &sec
		}
		out[k] = x
	}
	return out
}

// DashboardItem — факты по новичку для чипов.
type DashboardItem struct {
	NewcomerID        string           `json:"newcomer_id"`
	Zone              *string          `json:"zone"`
	SilentDays        *int             `json:"silent_days"`
	Catchup           *CatchupProgress `json:"catchup"`
	LowPulse          *int             `json:"low_pulse"`
	IPDecisionDate    *string          `json:"ip_decision_date"`
	TicketsOpen       int              `json:"tickets_open"`
	TicketsSLAOverdue int              `json:"tickets_sla_overdue"`
	ContactedAt       *string          `json:"contacted_at"`
}

type CatchupProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type BotDid struct {
	Nudges    int `json:"nudges"`
	Summaries int `json:"summaries"`
}

type DashTone struct {
	Positive int `json:"positive"`
	Neutral  int `json:"neutral"`
	Negative int `json:"negative"`
	N        int `json:"n"`
}

type PulseWeek struct {
	Sent     int       `json:"sent"`
	Answered int       `json:"answered"`
	Low      int       `json:"low"`
	Enough   bool      `json:"enough"`
	Tone     *DashTone `json:"tone"` // null при n < 5
}

type NextEvent struct {
	Title        string `json:"title"`
	At           string `json:"at"`
	NewcomerName string `json:"newcomer_name"`
}

// RecruiterDashboard — GET /api/recruiter/dashboard.
type RecruiterDashboard struct {
	Today        []TodayRow      `json:"today"`
	TodayTotal   int             `json:"today_total"`
	Tiles        map[string]Tile `json:"tiles"`
	BotDid       BotDid          `json:"bot_did"`
	PulseWeek    PulseWeek       `json:"pulse_week"`
	Items        []DashboardItem `json:"items"`
	NextEvent    *NextEvent      `json:"next_event"`
	NewcomersLed int             `json:"newcomers_led"`
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func RecruiterDashboardFrom(d *services.RecruiterDashboard) RecruiterDashboard {
	o := RecruiterDashboard{Today: todayFrom(d.Today), TodayTotal: d.TodayTotal, Tiles: tilesFrom(d.Tiles), Items: []DashboardItem{},
		BotDid: BotDid{Nudges: d.BotDid.Nudges, Summaries: d.BotDid.CatchupSummaries}, NewcomersLed: d.Newcomers}
	pw := d.PulseWeek
	o.PulseWeek = PulseWeek{Sent: pw.Sent, Answered: pw.Answered, Low: pw.Low, Enough: pw.Enough}
	if pw.Tone.Enough {
		o.PulseWeek.Tone = &DashTone{Positive: deref(pw.Tone.Pos), Neutral: deref(pw.Tone.Neu), Negative: deref(pw.Tone.Neg), N: pw.Tone.N}
	}
	if ne := d.NextEvent; ne != nil {
		o.NextEvent = &NextEvent{Title: ne.Title, At: ne.At.Format(time.RFC3339), NewcomerName: ne.NewcomerName}
	}
	for _, it := range d.Items {
		x := DashboardItem{NewcomerID: it.NewcomerID.String(), Zone: strPtr(it.Zone), SilentDays: it.SilentDays, LowPulse: it.LowPulse,
			TicketsOpen: it.TicketsOpen, TicketsSLAOverdue: it.TicketsSLAOverdue, ContactedAt: isoPtr(it.ContactedAt)}
		if it.Catchup != nil {
			x.Catchup = &CatchupProgress{Done: it.Catchup.Done, Total: it.Catchup.Total}
		}
		if it.IPDecisionDate != nil {
			s := it.IPDecisionDate.Format(dateLayout)
			x.IPDecisionDate = &s
		}
		o.Items = append(o.Items, x)
	}
	return o
}

// LoadRow — строка нагрузки руководителя HR (только активные рекрутеры).
type LoadRow struct {
	UserID            string `json:"user_id"`
	FullName          string `json:"full_name"`
	Newcomers         int    `json:"newcomers"`
	TicketsOpen       int    `json:"tickets_open"`
	TicketsSLAOverdue int    `json:"tickets_sla_overdue"`
}

// DigestLine — строка карточки «Дайджест недели» с числами для фронта (lines — готовый текст).
type DigestLine struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Value  *int   `json:"value"`
	Prev   *int   `json:"prev"`
	Delta  *int   `json:"delta"`
	N      int    `json:"n"`
	Enough bool   `json:"enough"`
}

type Digest3 struct {
	Week  string       `json:"week"`
	Lines []string     `json:"lines"`
	Facts []DigestLine `json:"facts"`
	Link  string       `json:"link"`
}

// HRLeadDashboard — GET /api/hrlead/dashboard.
type HRLeadDashboard struct {
	Today                  []TodayRow      `json:"today"`
	TodayTotal             int             `json:"today_total"`
	Tiles                  map[string]Tile `json:"tiles"`
	BotDid                 BotDid          `json:"bot_did"`
	Load                   []LoadRow       `json:"load"`
	Unassigned             int             `json:"unassigned"`
	MedianFirstResponseMin *int            `json:"median_first_response_min"`
	MedianN                int             `json:"median_n"`
	Digest                 *Digest3        `json:"digest"`
}

func HRLeadDashboardFrom(d *services.HRLeadDashboard) HRLeadDashboard {
	o := HRLeadDashboard{Today: todayFrom(d.Today), TodayTotal: d.TodayTotal, Tiles: tilesFrom(d.Tiles), Load: []LoadRow{},
		BotDid: BotDid{Nudges: d.BotDid.Nudges, Summaries: d.BotDid.CatchupSummaries}}
	if t := d.Team; t != nil {
		for _, m := range t.Members {
			if !m.User.Active || m.User.Role != domain.RoleRecruiter {
				continue
			}
			o.Load = append(o.Load, LoadRow{UserID: m.User.ID.String(), FullName: m.User.FullName, Newcomers: m.Newcomers,
				TicketsOpen: m.TicketsOpen, TicketsSLAOverdue: m.TicketsSLAOverdue})
		}
		o.Unassigned, o.MedianFirstResponseMin, o.MedianN = t.Unassigned, t.MedianFirstResponseMin, t.MedianN
	}
	if c := d.DigestCard; c != nil {
		dg := &Digest3{Week: c.Week, Link: c.Link, Lines: []string{}, Facts: []DigestLine{}}
		for _, l := range c.Lines {
			dg.Lines = append(dg.Lines, l.Text)
			dg.Facts = append(dg.Facts, DigestLine{Key: l.Key, Text: l.Text, Value: l.Value, Prev: l.Prev, Delta: l.Delta, N: l.N, Enough: l.Enough})
		}
		o.Digest = dg
	}
	return o
}
