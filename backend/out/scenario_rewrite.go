package out

import "main/services"

// ScenarioRewrite — ответ POST /api/scenarios/{id}/rewrite-vy (api_contract.md, v4).
type ScenarioRewrite struct {
	Updated         int              `json:"updated"`
	Failed          int              `json:"failed"`
	Items           []RewriteItem    `json:"items"`
	ClearedWarnings []ClearedWarning `json:"cleared_warnings"`
	Scenario        ScenarioDetail   `json:"scenario"`
}

type RewriteItem struct {
	StepID *string `json:"step_id"` // null — команда или правило сценария
	Field  string  `json:"field"`
	Before string  `json:"before"`
	After  string  `json:"after"`
	Source string  `json:"source"` // llm | rule
}

type ClearedWarning struct {
	StepID string `json:"step_id"`
	Code   string `json:"code"`
}

func ScenarioRewriteFrom(r services.RewriteReport) ScenarioRewrite {
	items := make([]RewriteItem, len(r.Items))
	for i, it := range r.Items {
		items[i] = RewriteItem{Field: it.Field, Before: it.Before, After: it.After, Source: it.Source}
		if it.StepID != "" {
			id := it.StepID
			items[i].StepID = &id
		}
	}
	cleared := make([]ClearedWarning, len(r.Cleared))
	for i, c := range r.Cleared {
		cleared[i] = ClearedWarning{StepID: c.StepID, Code: c.Code}
	}
	return ScenarioRewrite{Updated: r.Updated, Failed: r.Failed, Items: items, ClearedWarnings: cleared,
		Scenario: ScenarioDetailFrom(*r.Detail)}
}
