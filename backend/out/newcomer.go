package out

import (
	"time"

	"main/domain"
	"main/services"
)

// List — любой список API: {"items":[...],"total":N} (api_contract.md, v1).
type List[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// NewList — список; пустой — [] в JSON, а не null.
func NewList[T any](items []T) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Items: items, Total: len(items)}
}

// dateLayout — даты API: YYYY-MM-DD.
const dateLayout = "2006-01-02"

// Newcomer — сотрудник (новичок) по контракту v1.
type Newcomer struct {
	ID              string  `json:"id"`
	FullName        string  `json:"full_name"`
	Phone           string  `json:"phone"`
	Site            string  `json:"site"`
	Department      string  `json:"department"`
	Position        string  `json:"position"`
	ManagerName     string  `json:"manager_name"`
	MentorName      string  `json:"mentor_name"`
	RecruiterName   string  `json:"recruiter_name"`
	StartDate       string  `json:"start_date"`
	Shift           *string `json:"shift"`
	ScenarioID      string  `json:"scenario_id"`
	ScenarioVersion int     `json:"scenario_version"`
	AddressForm     *string `json:"address_form"`
	Consent         *string `json:"consent"`
	Connection      string  `json:"connection"`
	Day             int     `json:"day"`
	Stage           string  `json:"stage"`
	InviteLink      string  `json:"invite_link"`
	LastActivityAt  *string `json:"last_activity_at"`
	LastPulse       *int    `json:"last_pulse"`
	Risk            string  `json:"risk"`
	RiskReason      *string `json:"risk_reason"`
	OpenTickets     int     `json:"open_tickets"`
}

// zoned — момент в ISO 8601 с зоной площадки.
func zoned(t time.Time, site string) string {
	return t.In(domain.SiteOf(site).Loc).Format(time.RFC3339)
}

func NewcomerFrom(c services.NewcomerCard) Newcomer {
	e := c.Employee
	n := Newcomer{
		ID: e.ID.String(), FullName: e.FullName, Phone: e.Phone, Site: e.Site,
		Department: e.Department, Position: e.Position, ManagerName: e.ManagerName,
		MentorName: e.MentorName, RecruiterName: e.RecruiterName,
		StartDate: e.StartDate.Format(dateLayout), Shift: e.Shift,
		ScenarioID: e.ScenarioID.String(), ScenarioVersion: e.ScenarioVersion,
		AddressForm: e.AddressForm, Consent: e.Consent, Connection: e.Connection,
		Day: c.Day, Stage: c.Stage, InviteLink: c.InviteLink,
		LastPulse: e.LastPulse, Risk: e.Risk, RiskReason: e.RiskReason, OpenTickets: c.OpenTickets,
	}
	if n.Risk == "" {
		n.Risk = domain.RiskGreen
	}
	if e.LastActivityAt != nil {
		s := zoned(*e.LastActivityAt, e.Site)
		n.LastActivityAt = &s
	}
	return n
}

func NewcomersFrom(list []services.NewcomerCard) []Newcomer {
	out := make([]Newcomer, len(list))
	for i, c := range list {
		out[i] = NewcomerFrom(c)
	}
	return out
}

// TimelineEvent — событие таймлайна.
type TimelineEvent struct {
	EventID   string  `json:"event_id"`
	StepID    string  `json:"step_id"`
	DayOffset int     `json:"day_offset"`
	Title     string  `json:"title"`
	Kind      string  `json:"kind"`
	FireAt    string  `json:"fire_at"`
	Status    string  `json:"status"`
	Answer    *string `json:"answer"`
	Text      *string `json:"text"`
}

// TimelineFrom — события в ответ; время — в зоне площадки новичка.
func TimelineFrom(events []domain.ScheduledEvent, site string) List[TimelineEvent] {
	items := make([]TimelineEvent, len(events))
	for i, e := range events {
		items[i] = TimelineEvent{
			EventID: e.ID.String(), StepID: e.StepID.String(), DayOffset: e.DayOffset,
			Title: e.Title, Kind: e.Kind, FireAt: zoned(e.FireAt, site), Status: e.Status, Answer: e.Answer, Text: e.Text,
		}
	}
	return NewList(items)
}
