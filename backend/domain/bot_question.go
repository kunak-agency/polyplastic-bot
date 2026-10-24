package domain

import (
	"database/sql/driver"
	"time"

	"github.com/google/uuid"
)

// BotQuestion — свободный вопрос новичка и то, что бот на него ответил (итерация 2,
// UX-спецификация п. 2.5). Кнопки под ответом («Подробнее», «👍», «Не то», «Передать HR»)
// несут в payload только id этой записи: текст ответа и цитаты живут здесь, а не в payload
// (у MAX payload ≤ 1024 байт).
//
// Поля для аналитики: Kind (доля ответов с источником), HelpfulAt (👍, deflection rate),
// NotThat, HumanAt.
type BotQuestion struct {
	UUIDModel
	UpdatedAt  time.Time
	EmployeeID uuid.UUID `gorm:"type:uuid"`
	TraceID    string
	// Question — текст вопроса для HR («переписку они видят»). Только при согласии на обработку
	// сообщений; без согласия — пусто, остаётся только факт вопроса.
	Question string
	Kind     string // answer | abstain | routed | error | flag
	// Flags — коды regex-триггеров (Kind=flag): вопрос в rag не уходил.
	Flags StringList `gorm:"type:jsonb"`

	Answer        string     // текст ответа rag, как пришёл (без разметки)
	Sources       SourceList `gorm:"type:jsonb"`
	Confidence    string
	Topic         string
	AbstainReason string
	// Attempt — номер отказа подряд (Kind=abstain) для лестницы ошибок: 1, 2, 3+.
	Attempt int

	MessageID string // mid сообщения с ответом
	// SourceMID — mid сообщения новичка с вопросом (текст или голосовое): на него бот отвечает
	// реплаем, на него же — ответ HR по обращению из этого вопроса. nil — старые записи.
	SourceMID *string `gorm:"column:source_mid"`

	Expanded  bool // раскрыто «Подробнее»
	HelpfulAt *time.Time
	NotThat   int        // нажатий «Не то»
	HumanAt   *time.Time // передано человеку из этого ответа
	Outcome   string     // чем кончилось: handoff | declined | rephrase | details | ""
	// AwaitDetails — после «Сначала напишу, что случилось» следующий текст новичка — детали к
	// этому сигналу, а не новый вопрос.
	AwaitDetails bool
	AwaitSince   *time.Time // когда бот начал ждать детали (часы бота)
	Details      string
	// TicketID — обращение, в которое передан этот вопрос или сигнал (итерация 3).
	TicketID     *uuid.UUID `gorm:"type:uuid"`
	TicketNumber int64      // номер для текста сигнала F5/F9 («Обращение #N»)
	IsTest       bool
	// Sentiment — тональность Question (domain.Sentiment*): pos | neu | neg; nil — не размечен.
	Sentiment *string
	// Разметка /flags v2, как у SurveyAnswer: уверенность в тоне, цитата, строка для HR. nil — не
	// размечено (regex без ответа модели, LLM недоступна, старая запись).
	ToneConfidence *float64
	Quote          *string
	Summary        *string
}

// Значения BotQuestion.Kind.
const (
	QuestionAnswer  = "answer"
	QuestionAbstain = "abstain"
	QuestionRouted  = "routed"
	QuestionError   = "error"
	QuestionFlag    = "flag"
)

// QuestionSource — источник ответа: пункт документа и дословная цитата.
type QuestionSource struct {
	Doc      string `json:"doc"`
	ClauseID string `json:"clause_id"`
	Page     *int   `json:"page,omitempty"`
	Quote    string `json:"quote"`
}

// SourceList — jsonb-массив источников.
type SourceList []QuestionSource

func (l SourceList) Value() (driver.Value, error) {
	if l == nil {
		l = SourceList{}
	}
	return jsonValue([]QuestionSource(l))
}

func (l *SourceList) Scan(src any) error { return jsonScan(src, (*[]QuestionSource)(l)) }
