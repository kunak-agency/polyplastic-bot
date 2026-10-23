package out

import (
	"time"

	"main/domain"
	"main/services"
)

// Scenario — строка списка сценариев.
type Scenario struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Sites           []string `json:"sites"`
	Version         int      `json:"version"`
	Status          string   `json:"status"`
	ActiveNewcomers int      `json:"active_newcomers"`
	UpdatedAt       string   `json:"updated_at"`
}

// ScenarioDetail — сценарий со шагами.
type ScenarioDetail struct {
	Scenario
	Steps []Step `json:"steps"`
	// v4: команды бота (/food, /hr) и глобальные правила (ТЗ r69, r70) черновика.
	Commands domain.CommandList `json:"commands"`
	Rules    domain.RuleList    `json:"rules"`
}

// Step — шаг сценария по контракту v1.
type Step struct {
	ID            string         `json:"id"`
	DayOffset     int            `json:"day_offset"`
	Time          string         `json:"time"`
	Stage         string         `json:"stage"`
	Sites         []string       `json:"sites"`
	Kind          string         `json:"kind"`
	Title         string         `json:"title"`
	TextTy        string         `json:"text_ty"`
	TextVy        string         `json:"text_vy"`
	Buttons       []Button       `json:"buttons"`
	Survey        *domain.Survey `json:"survey"`
	Condition     *string        `json:"condition"`
	WorkHoursOnly bool           `json:"work_hours_only"`
	// v4
	Key          string               `json:"key"`
	Required     bool                 `json:"required"`
	Media        domain.MediaRefList  `json:"media"`
	Gallery      bool                 `json:"gallery"`
	AltText      *string              `json:"alt_text"`
	HRTask       bool                 `json:"hr_task"`
	FlagRequired *string              `json:"flag_required"`
	NoAnswer     *domain.NoAnswerRule `json:"no_answer"`
	Advance      *string              `json:"advance"`
}

type Button struct {
	Label         string         `json:"label"`
	Action        string         `json:"action"`
	Branch        *domain.Branch `json:"branch"`
	OpensFreeText bool           `json:"opens_free_text"`
}

func ScenarioFrom(s services.ScenarioSummary) Scenario {
	sites := []string(s.Scenario.Sites)
	if sites == nil {
		sites = []string{}
	}
	return Scenario{
		ID: s.Scenario.ID.String(), Name: s.Scenario.Name, Sites: sites, Version: s.Scenario.Version,
		Status: s.Scenario.Status, ActiveNewcomers: s.ActiveNewcomers,
		UpdatedAt: s.Scenario.UpdatedAt.In(domain.SiteOf("").Loc).Format(time.RFC3339),
	}
}

func ScenariosFrom(list []services.ScenarioSummary) []Scenario {
	out := make([]Scenario, len(list))
	for i, s := range list {
		out[i] = ScenarioFrom(s)
	}
	return out
}

func ScenarioDetailFrom(d services.ScenarioDetail) ScenarioDetail {
	steps := make([]Step, len(d.Steps))
	for i, s := range d.Steps {
		sites := []string(s.Sites)
		if sites == nil {
			sites = []string{}
		}
		buttons := make([]Button, len(s.Buttons))
		for j, b := range s.Buttons {
			buttons[j] = Button{Label: b.Label, Action: b.Action, Branch: b.Branch, OpensFreeText: b.OpensFreeText}
		}
		media := s.Media
		if media == nil {
			media = domain.MediaRefList{}
		}
		hhmm := s.Time
		if hhmm == "" {
			hhmm = "10:00"
		}
		steps[i] = Step{
			ID: s.ID.String(), DayOffset: s.DayOffset, Time: hhmm, Stage: s.Stage, Sites: sites,
			Kind: s.Kind, Title: s.Title, TextTy: s.TextTy, TextVy: s.TextVy, Buttons: buttons,
			Survey: surveyOut(s.Survey), Condition: s.Condition, WorkHoursOnly: s.WorkHoursOnly,
			Key: s.Key, Required: s.Required, Media: media, Gallery: s.Gallery, AltText: s.AltText,
			HRTask: s.HRTask, FlagRequired: s.FlagRequired, NoAnswer: s.NoAnswer, Advance: s.Advance,
		}
	}
	cmds, rules := d.Scenario.Commands, d.Scenario.Rules
	if cmds == nil {
		cmds = domain.CommandList{}
	}
	if rules == nil {
		rules = domain.RuleList{}
	}
	return ScenarioDetail{Scenario: ScenarioFrom(d.ScenarioSummary), Steps: steps, Commands: cmds, Rules: rules}
}

// surveyOut — копия опроса, где у каждого вопроса options — массив, а не null.
func surveyOut(sv *domain.Survey) *domain.Survey {
	if sv == nil {
		return nil
	}
	c := *sv
	c.Questions = make([]domain.SurveyQuestion, len(sv.Questions))
	for i, q := range sv.Questions {
		if q.Options == nil {
			q.Options = []string{}
		}
		c.Questions[i] = q
	}
	return &c
}
