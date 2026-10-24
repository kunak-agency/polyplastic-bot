package domain

import (
	"time"

	"github.com/google/uuid"
)

// Статусы обращения (api_contract.md, v2; UX-спецификация п. 2.7).
const (
	TicketOpen        = "open"
	TicketAssigned    = "assigned"
	TicketHumanActive = "human_active"
	TicketResolved    = "resolved"
)

// TicketStatuses — допустимые статусы в порядке канбана.
var TicketStatuses = []string{TicketOpen, TicketAssigned, TicketHumanActive, TicketResolved}

// Откуда обращение (Ticket.Source).
const (
	TicketFromButton = "button"  // «Спросить человека» под шагом, опросом, приветствием
	TicketFromAnswer = "answer"  // «Передать HR/рекрутеру» под ответом, отказом, маршрутом rag
	TicketFromFlag   = "flag"    // regex-флаг в свободном тексте
	TicketFromAccess = "access"  // «Чего-то нет» в шаге «Проверка доступов» → F2
	TicketFromSurvey = "survey"  // флаг в причине к оценке опроса (зарезервировано)
	TicketFromCmd    = "command" // /human
	TicketFromOther  = "other"
)

// TicketNumberStart — первый номер обращения: в кабинете «#1001», а не «#1».
const TicketNumberStart = 1001

// Ticket — обращение: эскалация новичка к человеку. Номер — по порядку (последовательность
// ticket_number_seq), его видит новичок.
//
// ПДн: Quote — слова новичка; для категорий со store_quote=false (F10) код её обнуляет и не
// сохраняет. Summary пишет код из контекста и текста новичка не содержит.
type Ticket struct {
	UUIDModel
	UpdatedAt time.Time

	Number     int64     `gorm:"uniqueIndex"`
	EmployeeID uuid.UUID `gorm:"type:uuid"`
	Day        int       // день адаптации на момент создания
	Category   string    // F0…F10 | question
	Priority   string    // P0 | P1 | P2
	Status     string
	Route      string // hr | recruiter | manager | security | duty
	// Restricted — F5 и F9: текст новичка скрыт и в кабинете (quote=null, hidden=true). Скрытые
	// лишь от руководителя F0, F4, F10 — false (taxonomy.Category.ManagerHidden).
	Restricted    bool
	NotifyManager bool
	AssigneeName  *string
	Anonymous     bool // анонимность HR-процессом не обеспечена — всегда false (UX 2.7)
	Summary       string
	Quote         *string
	// SummaryModel — строка модели для HR (/flags v2, ≤ 120 символов, без ПДн): заголовок карточки
	// в кабинете. nil — модель не размечала (degraded, F10, обращение не из текста). Summary
	// остаётся сводкой кода.
	SummaryModel *string `gorm:"column:summary_model"`
	SLADueAt     *time.Time
	Source       string

	// Связи: запрос «позвать человека», свободный вопрос, событие таймлайна.
	HumanRequestID *uuid.UUID `gorm:"type:uuid"`
	QuestionID     *uuid.UUID `gorm:"type:uuid"`
	EventID        *uuid.UUID `gorm:"type:uuid"`

	// StatusMessageID — сообщение новичку со статусом (OPEN → ASSIGNED правится в нём ≤ 24 ч).
	StatusMessageID string
	StatusSentAt    *time.Time
	// AwaitDetails — новичок нажал «Добавить детали»: следующий его текст — в обращение.
	AwaitDetails bool
	// NotifiedAt — карточка ушла в HR-чат / чат СБ (или в лог на подделке).
	NotifiedAt *time.Time
	ResolvedAt *time.Time
	Feedback   string // up | down | "" — 👍/👎 после «Вопрос закрыт»
	IsTest     bool
}

// Авторы сообщений обращения.
const (
	TicketMsgBot      = "bot"
	TicketMsgNewcomer = "newcomer"
	TicketMsgStaff    = "staff"
)

// TicketMessage — переписка обращения: новичок, бот, сотрудник; Internal — заметка, в MAX не
// уходит.
type TicketMessage struct {
	UUIDModel
	TicketID uuid.UUID `gorm:"type:uuid"`
	From     string
	Author   *string
	Text     string
	Internal bool
}
