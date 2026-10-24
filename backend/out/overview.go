package out

import (
	"strings"

	"main/services"

	"github.com/google/uuid"
)

// Overview — GET /api/overview (api_contract.md, v3).
type Overview struct {
	Site        *string         `json:"site"`
	GeneratedAt string          `json:"generated_at"`
	Week        OverviewWeek    `json:"week"`
	Tiles       OverviewTiles   `json:"tiles"`
	Stages      []OverviewStage `json:"stages"`
	RiskZones   RiskZones       `json:"risk_zones"`
	ActionToday []ActionItem    `json:"action_today"`
	Heatmap     Heatmap         `json:"heatmap"`
	Trend       Trend           `json:"trend"`
	ToneWeek    Tone            `json:"tone_week"`
}

type OverviewWeek struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type OverviewTiles struct {
	InAdaptation      int           `json:"in_adaptation"`
	StartingThisWeek  int           `json:"starting_this_week"`
	Overdue           int           `json:"overdue"`
	TicketsOpen       int           `json:"tickets_open"`
	TicketsSLAOverdue int           `json:"tickets_sla_overdue"`
	PulseResponse     PulseResponse `json:"pulse_response"`
}

type PulseResponse struct {
	Percent  *int `json:"percent"`
	Answered int  `json:"answered"`
	Sent     int  `json:"sent"`
	Days     int  `json:"period_days"`
}

type OverviewStage struct {
	Stage string `json:"stage"`
	Count int    `json:"count"`
}

type RiskZones struct {
	Items []RiskZone `json:"items"`
	Total int        `json:"total"`
}

type RiskZone struct {
	NewcomerID  uuid.UUID `json:"newcomer_id"`
	FullName    string    `json:"full_name"`
	Site        string    `json:"site"`
	Day         int       `json:"day"`
	Stage       string    `json:"stage"`
	Zone        string    `json:"zone"`
	Reason      string    `json:"reason"`
	Reasons     []string  `json:"reasons"`
	Overdue     bool      `json:"overdue"`
	LastPulse   *int      `json:"last_pulse"`
	OpenTickets int       `json:"open_tickets"`
}

type ActionItem struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type Heatmap struct {
	MinGroup    int            `json:"min_group"`
	PeriodDays  int            `json:"period_days"`
	Sites       []string       `json:"sites"`
	Categories  []HeatCategory `json:"categories"`
	Respondents map[string]int `json:"respondents"`
	Cells       []HeatCell     `json:"cells"`
}

type HeatCategory struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type HeatCell struct {
	Category string   `json:"category"`
	Site     string   `json:"site"`
	Count    *int     `json:"count"`
	Share    *float64 `json:"share"`
	Hidden   bool     `json:"hidden"`
}

type Trend struct {
	Weeks    []string      `json:"weeks"`
	MinGroup int           `json:"min_group"`
	Series   []TrendSeries `json:"series"`
}

type TrendSeries struct {
	Site   string       `json:"site"`
	Points []TrendPoint `json:"points"`
}

type TrendPoint struct {
	Week        string   `json:"week"`
	Avg         *float64 `json:"avg"`
	Respondents int      `json:"respondents"`
	Hidden      bool     `json:"hidden"`
}

const overviewDate = "2006-01-02"

// OverviewFrom — DTO обзора. Пустые списки — [], не null.
func OverviewFrom(o *services.Overview) Overview {
	r := Overview{
		GeneratedAt: o.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		Week:        OverviewWeek{From: o.WeekFrom.Format(overviewDate), To: o.WeekTo.Format(overviewDate)},
		Tiles: OverviewTiles{
			InAdaptation: o.Tiles.InAdaptation, StartingThisWeek: o.Tiles.StartingThisWeek,
			Overdue: o.Tiles.Overdue, TicketsOpen: o.Tiles.TicketsOpen, TicketsSLAOverdue: o.Tiles.TicketsSLAOverdue,
			PulseResponse: PulseResponse{Percent: o.Tiles.PulsePercent, Answered: o.Tiles.PulseAnswered, Sent: o.Tiles.PulseSent, Days: services.OverviewPeriodDays},
		},
		Stages:      []OverviewStage{},
		RiskZones:   RiskZones{Items: []RiskZone{}, Total: len(o.Risks)},
		ActionToday: []ActionItem{},
		Heatmap: Heatmap{
			MinGroup: o.Heatmap.MinGroup, PeriodDays: o.Heatmap.PeriodDays, Sites: o.Heatmap.Sites,
			Categories: []HeatCategory{}, Respondents: o.Heatmap.Respondents, Cells: []HeatCell{},
		},
		Trend:    Trend{Weeks: []string{}, MinGroup: services.AnonymityMinGroup, Series: []TrendSeries{}},
		ToneWeek: toneFrom(o.ToneWeek),
	}
	if o.Site != "" {
		s := o.Site
		r.Site = &s
	}
	for _, s := range o.Stages {
		r.Stages = append(r.Stages, OverviewStage{Stage: s.Stage, Count: s.Count})
	}
	for _, z := range o.Risks {
		reasons := z.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		r.RiskZones.Items = append(r.RiskZones.Items, RiskZone{
			NewcomerID: z.NewcomerID, FullName: z.FullName, Site: z.Site, Day: z.Day, Stage: z.Stage,
			Zone: z.Zone, Reason: strings.Join(reasons, " + "), Reasons: reasons, Overdue: z.Overdue,
			LastPulse: z.LastPulse, OpenTickets: z.OpenTickets,
		})
	}
	for _, a := range o.Actions {
		r.ActionToday = append(r.ActionToday, ActionItem{Kind: a.Kind, Count: a.Count})
	}
	for _, c := range o.Heatmap.Categories {
		r.Heatmap.Categories = append(r.Heatmap.Categories, HeatCategory{Code: c.Code, Name: c.Name})
	}
	for _, c := range o.Heatmap.Cells {
		r.Heatmap.Cells = append(r.Heatmap.Cells, HeatCell{Category: c.Category, Site: c.Site, Count: c.Count, Share: c.Share, Hidden: c.Hidden})
	}
	for _, w := range o.Trend.Weeks {
		r.Trend.Weeks = append(r.Trend.Weeks, w.Format(overviewDate))
	}
	for _, s := range o.Trend.Series {
		ts := TrendSeries{Site: s.Site, Points: []TrendPoint{}}
		for _, p := range s.Points {
			ts.Points = append(ts.Points, TrendPoint{Week: p.Week.Format(overviewDate), Avg: p.Avg, Respondents: p.Respondents, Hidden: p.Hidden})
		}
		r.Trend.Series = append(r.Trend.Series, ts)
	}
	return r
}
