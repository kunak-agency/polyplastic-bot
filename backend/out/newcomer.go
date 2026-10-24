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
	ID            string `json:"id"`
	FullName      string `json:"full_name"`
	Phone         string `json:"phone"`
	Site          string `json:"site"`
	Department    string `json:"department"`
	Position      string `json:"position"`
	ManagerName   string `json:"manager_name"`
	MentorName    string `json:"mentor_name"`
	RecruiterName string `json:"recruiter_name"`
	// RecruiterID — закреплённый рекрутер (employees.recruiter_id); null — без рекрутера.
	RecruiterID *string `json:"recruiter_id"`
	// ManagerID, MentorID — руководитель и наставник из справочника людей (GET /api/people);
	// null — не выбран (у старой записи есть только manager_name / mentor_name).
	ManagerID       *string `json:"manager_id"`
	MentorID        *string `json:"mentor_id"`
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
	// Flags — флаги новичка (recruiter_enabled — «отмашка рекрутера», set_flag веток).
	Flags map[string]bool `json:"flags"`
	// IPDecision — решение по ИС: keep | extend | part | null; когда и кто записал.
	IPDecision   *string `json:"ip_decision"`
	IPDecisionAt *string `json:"ip_decision_at"`
	IPDecisionBy *string `json:"ip_decision_by"`
	// Расписание (итерация 24.10, out/nudge.go): пояс новичка и площадки, «не отвечает»,
	// догоняющая очередь позднего входа.
	NewcomerScheduleOut
	// RecentTone — тон последних 5 текстовых ответов (только GET /api/newcomers/{id}; в реестре
	// поля нет). Без текста.
	RecentTone *[]NewcomerTone `json:"recent_tone,omitempty"`
}

// NewcomerTone — тон одного текстового ответа: at — время в зоне площадки, kind — survey | question.
type NewcomerTone struct {
	At        string `json:"at"`
	Kind      string `json:"kind"`
	Sentiment string `json:"sentiment"`
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
		Flags: map[string]bool{},
	}
	for k, v := range e.Flags {
		n.Flags[k] = v
	}
	if e.RecruiterID != nil {
		id := e.RecruiterID.String()
		n.RecruiterID = &id
	}
	if e.ManagerID != nil {
		id := e.ManagerID.String()
		n.ManagerID = &id
	}
	if e.MentorID != nil {
		id := e.MentorID.String()
		n.MentorID = &id
	}
	if c.RecentTone != nil {
		tones := make([]NewcomerTone, 0, len(c.RecentTone))
		for _, t := range c.RecentTone {
			tones = append(tones, NewcomerTone{At: zoned(t.CreatedAt, e.Site), Kind: t.Kind, Sentiment: t.Sentiment})
		}
		n.RecentTone = &tones
	}
	n.NewcomerScheduleOut = scheduleOut(c)
	if n.Risk == "" {
		n.Risk = domain.RiskGreen
	}
	if e.LastActivityAt != nil {
		s := zoned(*e.LastActivityAt, e.Site)
		n.LastActivityAt = &s
	}
	n.IPDecision, n.IPDecisionBy = e.IPDecision, e.IPDecisionBy
	if e.IPDecisionAt != nil {
		s := zoned(*e.IPDecisionAt, e.Site)
		n.IPDecisionAt = &s
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
