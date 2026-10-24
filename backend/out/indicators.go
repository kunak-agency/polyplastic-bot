package out

import (
	"main/services"
)

// Indicators — GET /api/indicators (api_contract.md, «Показатели»).
type Indicators struct {
	Site        *string         `json:"site"`
	GeneratedAt string          `json:"generated_at"`
	Period      IndicatorPeriod `json:"period"`
	Cohort      int             `json:"cohort"`
	CohortPrev  int             `json:"cohort_prev"`
	Synthetic   int             `json:"synthetic"`
	MinGroup    int             `json:"min_group"`
	Today       IndicatorToday  `json:"today"`
	Tiles       []IndicatorTile `json:"tiles"`
	Funnel      IndicatorFunnel `json:"funnel"`
	Sites       IndicatorSites  `json:"sites"`
	Topics      IndicatorTopics `json:"topics"`
	HRLoad      IndicatorHRLoad `json:"hr_load"`
}

type IndicatorPeriod struct {
	Key      string `json:"key"`
	From     string `json:"from"`
	To       string `json:"to"`
	PrevFrom string `json:"prev_from"`
	PrevTo   string `json:"prev_to"`
	Days     int    `json:"days"`
}

type IndicatorToday struct {
	Total  int    `json:"total"`
	Urgent int    `json:"urgent"`
	Source string `json:"source"`
}

type IndicatorTile struct {
	Key       string  `json:"key"`
	Status    string  `json:"status"`
	Unit      string  `json:"unit"`
	Value     *int    `json:"value"`
	Num       int     `json:"num"`
	N         int     `json:"n"`
	Prev      *int    `json:"prev"`
	PrevN     int     `json:"prev_n"`
	Delta     *int    `json:"delta"`
	Direction *string `json:"direction"`
	Demo      bool    `json:"demo"`
	SLAPct    *int    `json:"sla_pct,omitempty"`
	SLAN      *int    `json:"sla_n,omitempty"`
}

type IndicatorFunnel struct {
	Cohort   int                   `json:"cohort"`
	Percents bool                  `json:"percents"`
	Hidden   bool                  `json:"hidden"`
	Steps    []IndicatorFunnelStep `json:"steps"`
}

type IndicatorFunnelStep struct {
	Key     string          `json:"key"`
	Count   *int            `json:"count"`
	Percent *int            `json:"percent"`
	Pending int             `json:"pending"`
	Lost    []IndicatorLoss `json:"lost"`
	Hidden  bool            `json:"hidden"`
}

type IndicatorLoss struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type IndicatorSites struct {
	Sites   []string           `json:"sites"`
	Cohorts map[string]int     `json:"cohorts"`
	Rows    []IndicatorSiteRow `json:"rows"`
	Flagged []string           `json:"flagged"`
}

type IndicatorSiteRow struct {
	Key   string              `json:"key"`
	Unit  string              `json:"unit"`
	Avg   *float64            `json:"avg"`
	AvgN  int                 `json:"avg_n"`
	Cells []IndicatorSiteCell `json:"cells"`
}

type IndicatorSiteCell struct {
	Site    string   `json:"site"`
	Value   *float64 `json:"value"`
	N       int      `json:"n"`
	Hidden  bool     `json:"hidden"`
	Diff    *float64 `json:"diff"`
	DiffPP  *float64 `json:"diff_pp"`
	Verdict *string  `json:"verdict"`
	Flag    bool     `json:"flag"`
}

type IndicatorTopics struct {
	Heatmap Heatmap               `json:"heatmap"`
	Trends  []IndicatorTopicTrend `json:"trends"`
}

type IndicatorTopicTrend struct {
	Category string  `json:"category"`
	Name     string  `json:"name"`
	Count    *int    `json:"count"`
	Prev     *int    `json:"prev"`
	Trend    *string `json:"trend"`
}

type IndicatorHRLoad struct {
	Tickets     int           `json:"tickets"`
	Employees   int           `json:"employees"`
	Per100      *float64      `json:"per_100"`
	BotClosed   IndicatorTile `json:"bot_closed"`
	FirstResp   IndicatorTile `json:"first_response"`
	KBGaps      *int          `json:"kb_gaps"`
	KBQuestions int           `json:"kb_questions"`
}

func indicatorTile(t services.IndicatorTile) IndicatorTile {
	r := IndicatorTile{
		Key: t.Key, Status: t.Status, Unit: t.Unit, Value: t.Value, Num: t.Num, N: t.N,
		Prev: t.Prev, PrevN: t.PrevN, Delta: t.Delta, Direction: strOrNil(t.Direction), Demo: t.Demo,
	}
	if t.Key == services.TileFirstResponse {
		n := t.SLAN
		r.SLAPct, r.SLAN = t.SLAPct, &n
	}
	return r
}

// IndicatorsFrom — DTO «Показателей». Пустые списки — [], не null.
func IndicatorsFrom(x *services.Indicators) Indicators {
	p := x.Period
	r := Indicators{
		GeneratedAt: x.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		Period: IndicatorPeriod{
			Key: p.Key, From: p.From.Format(overviewDate), To: p.To.Format(overviewDate),
			PrevFrom: p.PrevFrom.Format(overviewDate), PrevTo: p.PrevTo.AddDate(0, 0, -1).Format(overviewDate), Days: p.Days,
		},
		Cohort: x.Cohort, CohortPrev: x.CohortPrev, Synthetic: x.Synthetic, MinGroup: services.AnonymityMinGroup,
		Today:  IndicatorToday{Total: x.Today.Total, Urgent: x.Today.Urgent, Source: x.Today.Source},
		Tiles:  []IndicatorTile{},
		Funnel: IndicatorFunnel{Cohort: x.Funnel.Cohort, Percents: x.Funnel.Percents, Hidden: x.Funnel.Hidden, Steps: []IndicatorFunnelStep{}},
		Sites:  IndicatorSites{Sites: x.Sites.Sites, Cohorts: x.Sites.Cohorts, Rows: []IndicatorSiteRow{}, Flagged: x.Sites.Flagged},
		Topics: IndicatorTopics{Heatmap: heatmapFrom(x.Topics.Heatmap), Trends: []IndicatorTopicTrend{}},
		HRLoad: IndicatorHRLoad{
			Tickets: x.HRLoad.Tickets, Employees: x.HRLoad.Employees, Per100: x.HRLoad.Per100,
			BotClosed: indicatorTile(x.HRLoad.BotClosed), FirstResp: indicatorTile(x.HRLoad.FirstResp),
			KBGaps: x.HRLoad.KBGaps, KBQuestions: x.HRLoad.KBQuestions,
		},
	}
	if x.Site != "" {
		s := x.Site
		r.Site = &s
	}
	if r.Sites.Flagged == nil {
		r.Sites.Flagged = []string{}
	}
	for _, t := range x.Tiles {
		r.Tiles = append(r.Tiles, indicatorTile(t))
	}
	for _, s := range x.Funnel.Steps {
		st := IndicatorFunnelStep{Key: s.Key, Percent: s.Percent, Pending: s.Pending, Hidden: s.Hidden, Lost: []IndicatorLoss{}}
		if !s.Hidden {
			n := s.Count
			st.Count = &n
		}
		for _, l := range s.Lost {
			st.Lost = append(st.Lost, IndicatorLoss{Reason: l.Reason, Count: l.Count})
		}
		r.Funnel.Steps = append(r.Funnel.Steps, st)
	}
	for _, row := range x.Sites.Rows {
		rr := IndicatorSiteRow{Key: row.Key, Unit: row.Unit, Avg: row.Avg, AvgN: row.AvgN, Cells: []IndicatorSiteCell{}}
		for _, c := range row.Cells {
			rr.Cells = append(rr.Cells, IndicatorSiteCell{
				Site: c.Site, Value: c.Value, N: c.N, Hidden: c.Hidden, Diff: c.Diff, DiffPP: c.DiffPP,
				Verdict: strOrNil(c.Verdict), Flag: c.Flag,
			})
		}
		r.Sites.Rows = append(r.Sites.Rows, rr)
	}
	for _, t := range x.Topics.Trends {
		r.Topics.Trends = append(r.Topics.Trends, IndicatorTopicTrend{Category: t.Category, Name: t.Name, Count: t.Count, Prev: t.Prev, Trend: strOrNil(t.Trend)})
	}
	return r
}

// heatmapFrom — тепловая карта в DTO обзора.
func heatmapFrom(h services.Heatmap) Heatmap {
	r := Heatmap{MinGroup: h.MinGroup, PeriodDays: h.PeriodDays, Sites: h.Sites, Categories: []HeatCategory{}, Respondents: h.Respondents, Cells: []HeatCell{}}
	for _, c := range h.Categories {
		r.Categories = append(r.Categories, HeatCategory{Code: c.Code, Name: c.Name})
	}
	for _, c := range h.Cells {
		r.Cells = append(r.Cells, HeatCell{Category: c.Category, Site: c.Site, Count: c.Count, Share: c.Share, Hidden: c.Hidden})
	}
	return r
}

// TrendFrom — тренд пульса в DTO (обзор и дайджест).
func TrendFrom(t services.Trend) Trend {
	r := Trend{Weeks: []string{}, MinGroup: services.AnonymityMinGroup, Series: []TrendSeries{}}
	for _, w := range t.Weeks {
		r.Weeks = append(r.Weeks, w.Format(overviewDate))
	}
	for _, s := range t.Series {
		ts := TrendSeries{Site: s.Site, Points: []TrendPoint{}}
		for _, p := range s.Points {
			ts.Points = append(ts.Points, TrendPoint{Week: p.Week.Format(overviewDate), Avg: p.Avg, Respondents: p.Respondents, Hidden: p.Hidden})
		}
		r.Series = append(r.Series, ts)
	}
	return r
}
