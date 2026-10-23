package domain

import (
	"database/sql/driver"
	"time"

	"github.com/google/uuid"
)

// Статусы сценария: draft — есть неопубликованные правки, published — шаги совпадают с
// последней версией.
const (
	ScenarioDraft     = "draft"
	ScenarioPublished = "published"
)

// Этапы адаптации (stage сотрудника и шага).
const (
	StageBefore    = "before"
	StageDay0      = "day0"
	StageWeek1     = "week1"
	StageMonth1    = "month1"
	StageProbation = "probation"
	StageDone      = "done"
)

// StageCodes — допустимые этапы.
var StageCodes = []string{StageBefore, StageDay0, StageWeek1, StageMonth1, StageProbation, StageDone}

// StageForDay — этап по дню от D0: до выхода, день выхода, первая неделя, первый месяц,
// испытательный срок (до D+90), завершено.
func StageForDay(day int) string {
	switch {
	case day < 0:
		return StageBefore
	case day == 0:
		return StageDay0
	case day <= 7:
		return StageWeek1
	case day <= 30:
		return StageMonth1
	case day <= 90:
		return StageProbation
	}
	return StageDone
}

// Типы шага.
const (
	KindText   = "text"
	KindCard   = "card"
	KindFile   = "file"
	KindSurvey = "survey"
	KindTask   = "task"
)

var KindCodes = []string{KindText, KindCard, KindFile, KindSurvey, KindTask}

// Шкалы опроса.
const (
	Scale10     = "1-10"
	Scale5      = "1-5"
	ScaleYesNo  = "yesno"
	ScaleChoice = "choice"
)

var ScaleCodes = []string{Scale10, Scale5, ScaleYesNo, ScaleChoice}

// Scenario — сценарий адаптации. Шаги черновика — ScenarioStep; опубликованные — снимки
// ScenarioVersion. Version — номер последней опубликованной версии (0 — не публиковался).
type Scenario struct {
	UUIDModel
	UpdatedAt time.Time
	Name      string
	Sites     StringList `gorm:"type:jsonb"`
	Version   int
	Status    string
}

// ScenarioVersion — опубликованная версия: неизменяемый снимок шагов. События таймлайна
// ссылаются на (step_id, номер версии) и рендерятся из снимка, а не из черновика.
type ScenarioVersion struct {
	UUIDModel
	ScenarioID  uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_scenario_version"`
	Number      int       `gorm:"uniqueIndex:ux_scenario_version"`
	Comment     string
	Apply       string   // new | active
	Steps       StepList `gorm:"type:jsonb"`
	PublishedAt time.Time
}

// Step — шаг ScenarioVersion.Steps по id.
func (v ScenarioVersion) Step(id uuid.UUID) (ScenarioStep, bool) {
	for _, s := range v.Steps {
		if s.ID == id {
			return s, true
		}
	}
	return ScenarioStep{}, false
}

// ScenarioStep — шаг черновика. id шага стабилен между версиями: по нему таймлайн узнаёт,
// что шаг уже отправлен, когда публикация пересобирает недоставленные события.
type ScenarioStep struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	ScenarioID    uuid.UUID  `gorm:"type:uuid" json:"scenario_id"`
	Position      int        `json:"position"`
	DayOffset     int        `json:"day_offset"`
	Time          string     `json:"time"` // HH:MM по времени площадки; пусто — 10:00
	Stage         string     `json:"stage"`
	Sites         StringList `gorm:"type:jsonb" json:"sites"` // пусто — все площадки сценария
	Kind          string     `json:"kind"`
	Title         string     `json:"title"`
	TextTy        string     `json:"text_ty"`
	TextVy        string     `json:"text_vy"`
	Buttons       ButtonList `gorm:"type:jsonb" json:"buttons"`
	Survey        *Survey    `gorm:"type:jsonb" json:"survey"`
	Condition     *string    `json:"condition"`
	WorkHoursOnly bool       `json:"work_hours_only"`
}

// AppliesTo — шаг для площадки: без меток — для всех.
func (s ScenarioStep) AppliesTo(site string) bool { return len(s.Sites) == 0 || s.Sites.Has(site) }

// StepButton — кнопка шага. Action — что сделать по нажатию (api_contract.md, заметка backend):
// "" или произвольный код — записать ответ; "link:<url>" или "http…" — кнопка-ссылка;
// "human" — «Спросить человека»; "skip" — отметить шаг пропущенным.
type StepButton struct {
	Label  string `json:"label"`
	Action string `json:"action"`
}

// ButtonList — jsonb-массив кнопок.
type ButtonList []StepButton

func (l ButtonList) Value() (driver.Value, error) {
	if l == nil {
		l = ButtonList{}
	}
	return jsonValue([]StepButton(l))
}

func (l *ButtonList) Scan(src any) error { return jsonScan(src, (*[]StepButton)(l)) }

// Survey — опрос шага. Threshold — «ниже порога → экран причин» для шкал 1-10 и 1-5 (0 — не
// спрашивать). Options вопроса: для choice — варианты ответа, для шкал — кнопки причин.
type Survey struct {
	Scale     string           `json:"scale"`
	Threshold int              `json:"threshold"`
	Questions []SurveyQuestion `json:"questions"`
}

type SurveyQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

func (s *Survey) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return jsonValue(*s)
}

func (s *Survey) Scan(src any) error { return jsonScan(src, s) }

// StepList — jsonb-снимок шагов версии.
type StepList []ScenarioStep

func (l StepList) Value() (driver.Value, error) {
	if l == nil {
		l = StepList{}
	}
	return jsonValue([]ScenarioStep(l))
}

func (l *StepList) Scan(src any) error { return jsonScan(src, (*[]ScenarioStep)(l)) }
