package domain

import (
	"database/sql/driver"

	"github.com/google/uuid"
)

// Исполнение блока и веток (api_contract.md v4, «Исполнение блока»).

// EventHeld — шаг «по отмашке» (flag_required) ждёт флага новичка: не отправлен и поток блока
// не держит. Флаг включили — событие снова scheduled.
const EventHeld = "held"

// Чего ждёт отправленное событие (ScheduledEvent.Waiting). Пока ждёт — следующие реплики блока
// не уходят.
const (
	WaitAnswer   = "answer"    // вопрос: кнопки шага или опрос
	WaitContinue = "continue"  // «Дальше →»
	WaitBranch   = "branch"    // терминальные кнопки ветки
	WaitFreeText = "free_text" // «Другое» / open_free_text: следующий текст новичка
	WaitInput    = "input"     // шаг kind=input: текст и/или фото новичка → карточка и HR
)

// Значения ScenarioStep.Advance.
const (
	AdvanceButton = "button"
	AdvanceAuto   = "auto"
)

// Типы действий ветки (Action.Type) и then ветки.
const (
	ActionEscalate     = "escalate"
	ActionHandoff      = "handoff"
	ActionSetField     = "set_field"
	ActionSetFlag      = "set_flag"
	ActionSendFile     = "send_file"
	ActionOpenFreeText = "open_free_text"

	ThenContinue = "continue"
	ThenEndBlock = "end_block"
	ThenGoto     = "goto_block"
)

// BoolMap — jsonb-объект флагов новичка {"recruiter_enabled": true}.
type BoolMap map[string]bool

func (m BoolMap) Value() (driver.Value, error) {
	if m == nil {
		m = BoolMap{}
	}
	return jsonValue(map[string]bool(m))
}

func (m *BoolMap) Scan(src any) error { return jsonScan(src, (*map[string]bool)(m)) }

// FreeTextWait — бот ждёт свободный текст новичка («Другое», open_free_text): куда его
// передать обращением.
type FreeTextWait struct {
	EventID       uuid.UUID `json:"event_id"`
	Label         string    `json:"label"`
	Category      string    `json:"category,omitempty"`
	Route         string    `json:"route,omitempty"`
	NotifyManager bool      `json:"notify_manager,omitempty"`
	Input         bool      `json:"input,omitempty"` // ответ на шаг kind=input
	Then          string    `json:"then,omitempty"`  // then ветки — после текста
	Goto          string    `json:"goto,omitempty"`
}

func (w *FreeTextWait) Value() (driver.Value, error) {
	if w == nil {
		return nil, nil
	}
	return jsonValue(*w)
}

func (w *FreeTextWait) Scan(src any) error { return jsonScan(src, w) }

// StepSnap — снимок шага в событии «Отправить себе» (send-test): шаг черновика, которого нет ни
// в одной версии. Отдельный тип, чтобы ScenarioStep оставался моделью таблицы.
type StepSnap ScenarioStep

func (s *StepSnap) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return jsonValue(*s)
}

func (s *StepSnap) Scan(src any) error { return jsonScan(src, s) }
