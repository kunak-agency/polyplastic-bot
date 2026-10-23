package handlers

import (
	"strings"

	"main/services"

	"github.com/gofiber/fiber/v2"
)

// Статистика шагов и кнопок сценария и последствия публикации (api_contract.md, v4).

type buttonStatsOut struct {
	Label   string   `json:"label"`
	Picked  int      `json:"picked"`
	Share   *float64 `json:"share"`
	Tickets int      `json:"tickets"`
}

type stepStatsOut struct {
	StepID        string           `json:"step_id"`
	Sent          int              `json:"sent"`
	Answered      int              `json:"answered"`
	NoAnswer      int              `json:"no_answer"`
	MedianAnswerS *int             `json:"median_answer_s"`
	Buttons       []buttonStatsOut `json:"buttons"`
}

type scenarioStatsOut struct {
	Site       *string        `json:"site"`
	MinAnswers int            `json:"min_answers"`
	Items      []stepStatsOut `json:"items"`
}

// ScenarioStats — GET /api/scenarios/{id}/stats?site= → по шагам и кнопкам.
func (h *Handler) ScenarioStats(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	st, err := h.services.Scenarios.Stats(c.UserContext(), id, strings.TrimSpace(c.Query("site")))
	if err != nil {
		return err
	}
	res := scenarioStatsOut{MinAnswers: services.StatsMinAnswers, Items: make([]stepStatsOut, len(st.Steps))}
	if st.Site != "" {
		res.Site = &st.Site
	}
	for i, s := range st.Steps {
		btns := make([]buttonStatsOut, len(s.Buttons))
		for j, b := range s.Buttons {
			btns[j] = buttonStatsOut{Label: b.Label, Picked: b.Picked, Share: b.Share, Tickets: b.Tickets}
		}
		res.Items[i] = stepStatsOut{
			StepID: s.StepID.String(), Sent: s.Sent, Answered: s.Answered, NoAnswer: s.NoAnswer,
			MedianAnswerS: s.MedianAnswerS, Buttons: btns,
		}
	}
	return c.JSON(res)
}

type stepChangeOut struct {
	StepID         string   `json:"step_id"`
	Title          string   `json:"title"`
	DayOffset      int      `json:"day_offset"`
	Time           string   `json:"time"`
	Change         string   `json:"change"`
	Fields         []string `json:"fields"`
	ButtonsRemoved []string `json:"buttons_removed"`
}

type publishPreviewOut struct {
	NextVersion         int             `json:"next_version"`
	ActiveNewcomers     int             `json:"active_newcomers"`
	InsideSurvey        int             `json:"inside_survey"`
	RemovedBranchesLive int             `json:"removed_branches_live"`
	Recommend           string          `json:"recommend"`
	Changes             []stepChangeOut `json:"changes"`
}

// PublishPreview — POST /api/scenarios/{id}/publish/preview → последствия публикации черновика.
// Ничего не меняет; считается по сохранённому черновику.
func (h *Handler) PublishPreview(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	p, err := h.services.Scenarios.PublishPreview(c.UserContext(), id)
	if err != nil {
		return err
	}
	res := publishPreviewOut{
		NextVersion: p.NextVersion, ActiveNewcomers: p.ActiveNewcomers, InsideSurvey: p.InsideSurvey,
		RemovedBranchesLive: p.RemovedBranchesLive, Recommend: p.Recommend,
		Changes: make([]stepChangeOut, len(p.Changes)),
	}
	for i, ch := range p.Changes {
		fields, removed := ch.Fields, ch.ButtonsRemoved
		if fields == nil {
			fields = []string{}
		}
		if removed == nil {
			removed = []string{}
		}
		res.Changes[i] = stepChangeOut{
			StepID: ch.StepID.String(), Title: ch.Title, DayOffset: ch.DayOffset, Time: ch.Time,
			Change: ch.Change, Fields: fields, ButtonsRemoved: removed,
		}
	}
	return c.JSON(res)
}
