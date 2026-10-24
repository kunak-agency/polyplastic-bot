package in

import (
	"fmt"

	"main/pkg/errs"
	"main/services"

	"github.com/google/uuid"
)

// RewriteVy — тело POST /api/scenarios/{id}/rewrite-vy (api_contract.md, v4). Всё
// необязательно: only_copied — true, neutralize — false, step_ids нет — все шаги,
// scenario_texts — true, если step_ids нет, иначе false.
type RewriteVy struct {
	OnlyCopied    *bool     `json:"only_copied"`
	Neutralize    *bool     `json:"neutralize"`
	StepIDs       *[]string `json:"step_ids"`
	ScenarioTexts *bool     `json:"scenario_texts"`
}

func (b RewriteVy) Validate() (services.RewriteOptions, error) {
	o := services.RewriteOptions{OnlyCopied: true, ScenarioTexts: b.StepIDs == nil}
	if b.OnlyCopied != nil {
		o.OnlyCopied = *b.OnlyCopied
	}
	if b.Neutralize != nil {
		o.Neutralize = *b.Neutralize
	}
	if b.ScenarioTexts != nil {
		o.ScenarioTexts = *b.ScenarioTexts
	}
	var f fields
	if b.StepIDs != nil {
		if len(*b.StepIDs) > maxSteps {
			f.add("step_ids", errs.CodeOutOfRange, fmt.Sprintf("Не больше %d шагов", maxSteps))
		}
		o.StepIDs = make([]uuid.UUID, 0, len(*b.StepIDs))
		for i, s := range *b.StepIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				f.add(fmt.Sprintf("step_ids[%d]", i), errs.CodeInvalidFormat, errs.MsgInvalidID)
				continue
			}
			o.StepIDs = append(o.StepIDs, id)
		}
	}
	return o, f.err()
}
