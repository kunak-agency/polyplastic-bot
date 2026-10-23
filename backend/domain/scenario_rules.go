package domain

import "database/sql/driver"

// Глобальные правила сценария (ТЗ r69, r70): не шаги таймлайна, а реакции на события.
const (
	RuleFreeQuestion   = "free_question"   // новичок пишет боту «просто так», вопрос сложный
	RuleSurveyReminder = "survey_reminder" // опрос не пройден — напомнить через remind_after_h
)

// ScenarioRule — правило сценария. Trigger — исходная формулировка ТЗ («когда»), Row — строка
// файла импорта (0 — создано в кабинете). Buttons — терминальные кнопки («Описать проблему»,
// «Чат с рекрутером»), RemindAfterH — часы от запуска опроса для survey_reminder.
type ScenarioRule struct {
	Key          string       `json:"key"`
	Kind         string       `json:"kind"`
	Trigger      string       `json:"trigger"`
	TextTy       string       `json:"text_ty"`
	TextVy       string       `json:"text_vy"`
	Media        MediaRefList `json:"media"`
	Buttons      []StepButton `json:"buttons"`
	RemindAfterH []int        `json:"remind_after_h,omitempty"`
	Row          int          `json:"row,omitempty"`
}

// RuleList — jsonb-массив правил сценария.
type RuleList []ScenarioRule

func (l RuleList) Value() (driver.Value, error) {
	if l == nil {
		l = RuleList{}
	}
	return jsonValue([]ScenarioRule(l))
}

func (l *RuleList) Scan(src any) error { return jsonScan(src, (*[]ScenarioRule)(l)) }
