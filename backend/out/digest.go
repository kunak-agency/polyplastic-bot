package out

import (
	"main/services"

	"github.com/google/uuid"
)

// Digest — GET /api/digest (api_contract.md, «v4 — дайджест»).
type Digest struct {
	Week         string             `json:"week"`
	From         string             `json:"from"`
	To           string             `json:"to"`
	Site         *string            `json:"site"`
	GeneratedAt  string             `json:"generated_at"`
	Partial      bool               `json:"partial"`
	MinGroup     int                `json:"min_group"`
	Totals       DigestTotals       `json:"totals"`
	Previous     DigestTotals       `json:"previous"`
	Themes       []DigestTheme      `json:"themes"`
	HiddenThemes int                `json:"hidden_themes"`
	Confidential DigestConfidential `json:"confidential"`
	Risks        DigestRisks        `json:"risk_zones"`
	Sites        []DigestSite       `json:"sites"`
	Probation    DigestProbation    `json:"probation"`
	Text         DigestText         `json:"text"`
	Message      DigestMessage      `json:"max_message"`
}

type DigestTotals struct {
	OnPulse         int      `json:"on_pulse"`
	Answered        int      `json:"answered"`
	ResponseRatePct *int     `json:"response_rate_pct"`
	WAvg            *int     `json:"w_avg"`
	PulseAvg        *float64 `json:"pulse_avg"`
	Respondents     int      `json:"respondents"`
	Problems        int      `json:"problems"`
	Critical        int      `json:"critical"`
}

type DigestTheme struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Category   *string  `json:"category"`
	Newcomers  int      `json:"newcomers"`
	Mentions   int      `json:"mentions"`
	Sites      []string `json:"sites"`
	Owner      *string  `json:"owner"`
	Keywords   []string `json:"keywords"`
	Examples   []string `json:"examples"`
	PrevCount  *int     `json:"prev_count"`
	PrevHidden bool     `json:"prev_hidden"`
	Delta      *int     `json:"delta"`
}

type DigestConfidential struct {
	Present bool `json:"present"`
	Count   *int `json:"count"`
}

type DigestRisks struct {
	Red    int        `json:"red"`
	Yellow int        `json:"yellow"`
	Items  []RiskZone `json:"items"`
}

type DigestSite struct {
	Site     string  `json:"site"`
	OnPulse  int     `json:"on_pulse"`
	Answered int     `json:"answered"`
	WAvg     *int    `json:"w_avg"`
	Problems int     `json:"problems"`
	TopTheme *string `json:"top_theme"`
}

type DigestProbation struct {
	Count    int  `json:"count"`
	ThisWeek bool `json:"this_week"`
}

type DigestText struct {
	Summary  string   `json:"summary"`
	Themes   *string  `json:"themes"`
	Changes  *string  `json:"changes"`
	Source   string   `json:"source"`
	Rejected []string `json:"rejected"`
}

type DigestMessage struct {
	Text   string `json:"text"`
	Length int    `json:"length"`
	Link   string `json:"link"`
}

// DigestFrom — DTO дайджеста; пустые списки — [].
func DigestFrom(d *services.Digest) Digest {
	r := Digest{
		Week: d.Week, From: d.From.Format(overviewDate), To: d.To.Format(overviewDate),
		GeneratedAt: d.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"), Partial: d.Partial,
		MinGroup: services.AnonymityMinGroup, Totals: digestTotals(d.Totals), Previous: digestTotals(d.Prev),
		Themes: []DigestTheme{}, HiddenThemes: d.Hidden,
		Confidential: DigestConfidential{Present: d.ConfidentialAny, Count: d.Confidential},
		Risks:        DigestRisks{Red: d.Risks.Red, Yellow: d.Risks.Yellow, Items: riskItems(d.Risks.Items)},
		Sites:        []DigestSite{},
		Probation:    DigestProbation{Count: d.Probation, ThisWeek: d.ProbationNow},
		Text:         DigestText{Summary: d.Text.Summary, Themes: d.Text.Themes, Changes: d.Text.Changes, Source: d.Text.Source, Rejected: nonNil(d.Text.Rejected)},
		Message:      DigestMessage{Text: d.MaxText, Length: len([]rune(d.MaxText)), Link: d.Link},
	}
	if d.Site != "" {
		s := d.Site
		r.Site = &s
	}
	for _, t := range d.Themes {
		th := DigestTheme{
			Key: t.Key, Label: t.Label, Newcomers: t.Newcomers, Mentions: t.Mentions, Sites: nonNil(t.Sites),
			Owner: t.Owner, Keywords: nonNil(t.Keywords), Examples: nonNil(t.Examples),
			PrevCount: t.PrevCount, PrevHidden: t.PrevHidden, Delta: t.Delta,
		}
		if t.Category != "" {
			c := t.Category
			th.Category = &c
		}
		r.Themes = append(r.Themes, th)
	}
	for _, s := range d.Sites {
		r.Sites = append(r.Sites, DigestSite{Site: s.Site, OnPulse: s.OnPulse, Answered: s.Answered, WAvg: s.WAvg, Problems: s.Problems, TopTheme: s.TopTheme})
	}
	return r
}

func digestTotals(t services.DigestTotals) DigestTotals {
	return DigestTotals{OnPulse: t.OnPulse, Answered: t.Answered, ResponseRatePct: t.ResponseRatePct, WAvg: t.WAvg,
		PulseAvg: t.PulseAvg, Respondents: t.Respondents, Problems: t.Problems, Critical: t.Critical}
}

func riskItems(list []services.RiskZone) []RiskZone {
	out := []RiskZone{}
	for _, z := range list {
		reasons := nonNil(z.Reasons)
		out = append(out, RiskZone{NewcomerID: z.NewcomerID, FullName: z.FullName, Site: z.Site, Day: z.Day, Stage: z.Stage,
			Zone: z.Zone, Reason: joinReasons(reasons), Reasons: reasons, Overdue: z.Overdue, LastPulse: z.LastPulse, OpenTickets: z.OpenTickets})
	}
	return out
}

func joinReasons(r []string) string {
	s := ""
	for i, x := range r {
		if i > 0 {
			s += " + "
		}
		s += x
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// DigestSent — POST /api/dev/digest/send.
type DigestSent struct {
	Week   string `json:"week"`
	SentTo int    `json:"sent_to"`
	Logged bool   `json:"logged"`
	Text   string `json:"text"`
}

// --- руководитель ---

// ManagerInvite — POST /api/newcomers/{id}/manager-invite.
type ManagerInvite struct {
	ManagerName string `json:"manager_name"`
	Link        string `json:"link"`
	ExpiresAt   string `json:"expires_at"`
	Connected   bool   `json:"connected"`
}

type ManagerSummary struct {
	ManagerName string `json:"manager_name"`
	Newcomers   int    `json:"newcomers"`
	Connected   bool   `json:"connected"`
}

// ManagerView — GET /api/manager/newcomers?name=.
type ManagerView struct {
	ManagerName string        `json:"manager_name"`
	Connected   bool          `json:"connected"`
	Items       []ManagerCard `json:"items"`
	Total       int           `json:"total"`
}

type ManagerCard struct {
	NewcomerID    uuid.UUID     `json:"newcomer_id"`
	FullName      string        `json:"full_name"`
	Site          string        `json:"site"`
	Department    string        `json:"department"`
	Position      string        `json:"position"`
	StartDate     string        `json:"start_date"`
	Day           int           `json:"day"`
	Stage         string        `json:"stage"`
	Connection    string        `json:"connection"`
	LastPulse     *int          `json:"last_pulse"`
	ProbationDate string        `json:"probation_date"`
	Attention     []string      `json:"attention"`
	Meetings      []ManagerTask `json:"meetings"`
}

type ManagerTask struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Text      string    `json:"text"`
	CreatedAt string    `json:"created_at"`
}

func ManagerViewFrom(v *services.ManagerView) ManagerView {
	r := ManagerView{ManagerName: v.ManagerName, Connected: v.Connected, Items: []ManagerCard{}, Total: len(v.Items)}
	for _, c := range v.Items {
		card := ManagerCard{NewcomerID: c.NewcomerID, FullName: c.FullName, Site: c.Site, Department: c.Department,
			Position: c.Position, StartDate: c.StartDate.Format(overviewDate), Day: c.Day, Stage: c.Stage,
			Connection: c.Connection, LastPulse: c.LastPulse, ProbationDate: c.ProbationDate.Format(overviewDate),
			Attention: nonNil(c.Attention), Meetings: []ManagerTask{}}
		for _, m := range c.Meetings {
			card.Meetings = append(card.Meetings, ManagerTask{ID: m.ID, Kind: m.Kind, Status: m.Status, Text: m.Text,
				CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
		}
		r.Items = append(r.Items, card)
	}
	return r
}

// ManagerPush — POST /api/dev/manager/push.
type ManagerPush struct {
	NudgeID    uuid.UUID `json:"nudge_id"`
	NewcomerID uuid.UUID `json:"newcomer_id"`
	Manager    string    `json:"manager_name"`
	Kind       string    `json:"kind"`
	Text       string    `json:"text"`
	Delivered  bool      `json:"delivered"`
}
