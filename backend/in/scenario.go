package in

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"

	"github.com/google/uuid"
)

// Потолки шага: MAX режет сообщение на 4000 символов, кнопку — на ~18 видимых.
const (
	maxStepText    = 4000
	maxButtonLabel = 64
	maxButtons     = 12
	maxQuestions   = 20
	maxSteps       = 300
)

// StepIn — шаг сценария в теле PUT /api/scenarios/{id}/steps.
type StepIn struct {
	ID        string   `json:"id"`
	DayOffset *int     `json:"day_offset"`
	Time      string   `json:"time"`
	Stage     string   `json:"stage"`
	Sites     []string `json:"sites"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	TextTy    string   `json:"text_ty"`
	TextVy    string   `json:"text_vy"`
	Buttons   []struct {
		Label  string `json:"label"`
		Action string `json:"action"`
	} `json:"buttons"`
	Survey        *domain.Survey `json:"survey"`
	Condition     *string        `json:"condition"`
	WorkHoursOnly *bool          `json:"work_hours_only"` // нет поля — true
}

// ParseSteps — тело PUT: {"steps":[…]} по контракту («Уточнения v1»); голый массив тоже
// принимается — тому, кто пишет руками в curl.
func ParseSteps(body []byte) ([]StepIn, error) {
	body = bytes.TrimSpace(body)
	var steps []StepIn
	if len(body) > 0 && body[0] == '[' {
		if err := json.Unmarshal(body, &steps); err != nil {
			return nil, errs.NewValidationError(errs.MsgInvalidJSON)
		}
		return steps, nil
	}
	var wrap struct {
		Steps []StepIn `json:"steps"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, errs.NewValidationError(errs.MsgInvalidJSON)
	}
	return wrap.Steps, nil
}

// ValidateSteps — шаги в модель черновика.
func ValidateSteps(list []StepIn) ([]domain.ScenarioStep, error) {
	var f fields
	if len(list) > maxSteps {
		f.add("steps", errs.CodeOutOfRange, fmt.Sprintf("Не больше %d шагов", maxSteps))
		return nil, f.err()
	}
	out := make([]domain.ScenarioStep, 0, len(list))
	for i, s := range list {
		p := fmt.Sprintf("steps[%d].", i)
		st := domain.ScenarioStep{
			Time:      strings.TrimSpace(s.Time),
			Stage:     strings.TrimSpace(s.Stage),
			Kind:      strings.TrimSpace(s.Kind),
			Title:     strings.TrimSpace(s.Title),
			TextTy:    s.TextTy,
			TextVy:    s.TextVy,
			Sites:     domain.StringList{},
			Buttons:   domain.ButtonList{},
			Survey:    s.Survey,
			Condition: s.Condition,
		}
		if s.ID != "" {
			if id, err := uuid.Parse(s.ID); err == nil {
				st.ID = id
			}
		}
		if s.DayOffset == nil {
			f.add(p+"day_offset", errs.CodeRequired, "Обязательное поле")
		} else if *s.DayOffset < -60 || *s.DayOffset > 365 {
			f.add(p+"day_offset", errs.CodeOutOfRange, "День от −60 до 365")
		} else {
			st.DayOffset = *s.DayOffset
		}
		if st.Time != "" && !validHHMM(st.Time) {
			f.add(p+"time", errs.CodeInvalidFormat, "Время в формате HH:MM")
		}
		if st.Stage != "" && !slices.Contains(domain.StageCodes, st.Stage) {
			f.add(p+"stage", errs.CodeOutOfRange, "Этап: "+strings.Join(domain.StageCodes, ", "))
		}
		if st.Kind == "" {
			st.Kind = domain.KindText
		} else if !slices.Contains(domain.KindCodes, st.Kind) {
			f.add(p+"kind", errs.CodeOutOfRange, "Тип: "+strings.Join(domain.KindCodes, ", "))
		}
		if st.Title == "" {
			f.add(p+"title", errs.CodeRequired, "Обязательное поле")
		} else if utf8.RuneCountInString(st.Title) > maxField {
			f.add(p+"title", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxField))
		}
		if utf8.RuneCountInString(st.TextTy) > maxStepText || utf8.RuneCountInString(st.TextVy) > maxStepText {
			f.add(p+"text_ty", errs.CodeOutOfRange, fmt.Sprintf("Текст не длиннее %d символов", maxStepText))
		}
		for _, code := range s.Sites {
			if _, ok := domain.Sites[code]; !ok {
				f.add(p+"sites", errs.CodeOutOfRange, "Площадки: GrPP, Luzhniki, GTP")
				break
			}
			if !st.Sites.Has(code) {
				st.Sites = append(st.Sites, code)
			}
		}
		if len(s.Buttons) > maxButtons {
			f.add(p+"buttons", errs.CodeOutOfRange, fmt.Sprintf("Не больше %d кнопок", maxButtons))
		}
		for j, b := range s.Buttons {
			label := strings.TrimSpace(b.Label)
			if label == "" || utf8.RuneCountInString(label) > maxButtonLabel {
				f.add(fmt.Sprintf("%sbuttons[%d].label", p, j), errs.CodeInvalidFormat, fmt.Sprintf("Подпись от 1 до %d символов", maxButtonLabel))
			}
			st.Buttons = append(st.Buttons, domain.StepButton{Label: label, Action: strings.TrimSpace(b.Action)})
		}
		validateSurvey(&f, p, st.Kind, st.Survey)
		st.WorkHoursOnly = s.WorkHoursOnly == nil || *s.WorkHoursOnly
		out = append(out, st)
	}
	return out, f.err()
}

func validateSurvey(f *fields, p, kind string, sv *domain.Survey) {
	if sv == nil {
		if kind == domain.KindSurvey {
			f.add(p+"survey", errs.CodeRequired, "У опроса нужны шкала и вопросы")
		}
		return
	}
	if !slices.Contains(domain.ScaleCodes, sv.Scale) {
		f.add(p+"survey.scale", errs.CodeOutOfRange, "Шкала: "+strings.Join(domain.ScaleCodes, ", "))
	}
	if sv.Threshold < 0 || sv.Threshold > 10 {
		f.add(p+"survey.threshold", errs.CodeOutOfRange, "Порог от 0 до 10")
	}
	if len(sv.Questions) == 0 || len(sv.Questions) > maxQuestions {
		f.add(p+"survey.questions", errs.CodeOutOfRange, fmt.Sprintf("От 1 до %d вопросов", maxQuestions))
	}
	for j, q := range sv.Questions {
		if strings.TrimSpace(q.Text) == "" {
			f.add(fmt.Sprintf("%ssurvey.questions[%d].text", p, j), errs.CodeRequired, "Обязательное поле")
		}
		if sv.Scale == domain.ScaleChoice && len(q.Options) < 2 {
			f.add(fmt.Sprintf("%ssurvey.questions[%d].options", p, j), errs.CodeOutOfRange, "У выбора нужны хотя бы два варианта")
		}
	}
}

func validHHMM(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h := (int(s[0]-'0'))*10 + int(s[1]-'0')
	m := (int(s[3]-'0'))*10 + int(s[4]-'0')
	for _, c := range []byte{s[0], s[1], s[3], s[4]} {
		if c < '0' || c > '9' {
			return false
		}
	}
	return h <= 23 && m <= 59
}

// Publish — тело POST /api/scenarios/{id}/publish.
type Publish struct {
	Comment string `json:"comment"`
	Apply   string `json:"apply"` // new | active; пусто — new
}

func (p Publish) Validate() (string, string, error) {
	var f fields
	comment := strings.TrimSpace(p.Comment)
	if utf8.RuneCountInString(comment) > 1000 {
		f.add("comment", errs.CodeOutOfRange, "Не длиннее 1000 символов")
	}
	apply := strings.TrimSpace(p.Apply)
	switch apply {
	case "":
		apply = "new"
	case "new", "active":
	default:
		f.add("apply", errs.CodeOutOfRange, "apply: new или active")
	}
	return comment, apply, f.err()
}

// ScenarioCreate — тело POST /api/scenarios: новый черновик пустым или копией шагов.
type ScenarioCreate struct {
	Name           string   `json:"name"`
	Sites          []string `json:"sites"`
	FromScenarioID *string  `json:"from_scenario_id"`
}

// Validate — имя, площадки (пусто — все) и необязательный источник копии.
func (b ScenarioCreate) Validate() (string, []string, *uuid.UUID, error) {
	var f fields
	name := f.text("name", b.Name, true)
	sites := []string{}
	for _, code := range b.Sites {
		code = strings.TrimSpace(code)
		if _, ok := domain.Sites[code]; !ok {
			f.add("sites", errs.CodeOutOfRange, "Площадки: GrPP, Luzhniki, GTP")
			break
		}
		if !slices.Contains(sites, code) {
			sites = append(sites, code)
		}
	}
	var from *uuid.UUID
	if b.FromScenarioID != nil && strings.TrimSpace(*b.FromScenarioID) != "" {
		id := f.uuid("from_scenario_id", *b.FromScenarioID)
		from = &id
	}
	return name, sites, from, f.err()
}
