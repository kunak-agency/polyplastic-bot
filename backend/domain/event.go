package domain

import (
	"database/sql/driver"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// Статусы события таймлайна (api_contract.md: scheduled|sent|answered|skipped|failed).
const (
	EventScheduled = "scheduled"
	EventSent      = "sent"
	EventAnswered  = "answered"
	EventSkipped   = "skipped"
	EventFailed    = "failed"
)

// MaxSendAttempts — после стольких неудачных отправок событие становится failed.
const MaxSendAttempts = 3

// eventNamespace — пространство имён UUIDv5 для event_id.
var eventNamespace = uuid.MustParse("5b0f3c9e-7d8a-4c61-9a1e-2f4d8b6c0a11")

// EventID — детерминированный id события: (сотрудник, шаг, версия) → один и тот же UUID. Это
// идемпотентность «по event_id»: повторная развёртка или перенос даты не плодят дублей, а
// уникальный индекс ниже — страховка на уровне БД.
func EventID(employeeID, stepID uuid.UUID, version int) uuid.UUID {
	return uuid.NewSHA1(eventNamespace, []byte(employeeID.String()+"/"+stepID.String()+"/"+strconv.Itoa(version)))
}

// ScheduledEvent — шаг сценария, развёрнутый на конкретного новичка. Заголовок, тип и день
// скопированы из снимка версии: таймлайн кабинета читается без разбора снимков.
type ScheduledEvent struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	EmployeeID      uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_event_step"`
	StepID          uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_event_step"`
	ScenarioVersion int       `gorm:"uniqueIndex:ux_event_step"`
	Position        int
	DayOffset       int
	Title           string
	Kind            string
	FireAt          time.Time
	Status          string
	MessageID       string
	SentAt          *time.Time
	Attempts        int
	Answer          *string
	// Text — текст, который ушёл в MAX (первый экран для опроса); тестовые — с «[ТЕСТ]».
	Text   *string
	IsTest bool
	Survey *SurveyState `gorm:"type:jsonb"`

	// Block — ключ блока (день + время, «12:01» после «12:00» — тот же блок): реплики блока
	// идут по очереди, вопрос держит поток (api_contract.md v4, «Исполнение блока»).
	Block string
	// Waiting — чего ждёт отправленное событие (domain.Wait*); "" — ничего, поток идёт дальше.
	Waiting string
	// Reminded — сколько напоминаний no_answer ушло.
	Reminded int
	// Snapshot — шаг «Отправить себе» (send-test): резолвится из снимка, а не из версии.
	Snapshot *StepSnap `gorm:"type:jsonb"`
	// Catchup — событие стоит в догоняющей очереди позднего входа (обязательный вопрос из
	// пропущенного дня, 1 блок в день); false — обычный таймлайн.
	Catchup bool `gorm:"not null;default:false"`
}

// SurveyState — где новичок в «живом опросе»: одно сообщение, которое правится после каждого
// ответа (UX-спецификация п. 2.4).
type SurveyState struct {
	Q         int            `json:"q"`      // текущий вопрос, с нуля
	Screen    string         `json:"screen"` // question | reasons | write
	Results   []SurveyResult `json:"results"`
	Reminders int            `json:"reminders"` // сколько раз переносили «Напомнить завтра»
}

// Экраны живого опроса.
const (
	ScreenQuestion = "question"
	ScreenReasons  = "reasons"
	ScreenWrite    = "write"
)

// SurveyResult — итог вопроса для строки «✅ 1. …: 8/10». Текст новичка сюда не попадает:
// он живёт в SurveyAnswer.Text и в разметку сообщения не вставляется.
type SurveyResult struct {
	Value   *int   `json:"value,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"` // подпись кнопки причины (наш текст)
	Wrote   bool   `json:"wrote,omitempty"`  // причина написана своими словами
	Multi   []int  `json:"multi,omitempty"`  // multi: отмеченные варианты (с единицы)
	// Уточняющий вопрос после тревожного ответа (FEATURE_CLARIFY, services/clarify.go): код
	// категории шаблона, когда задан, и ответил ли новичок (выбор или «Не хочу отвечать»). Живёт
	// в jsonb события — схема таблиц не меняется.
	Clarify     string     `json:"clarify,omitempty"`
	ClarifyAt   *time.Time `json:"clarify_at,omitempty"`
	ClarifyDone bool       `json:"clarify_done,omitempty"`
}

func (s *SurveyState) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return jsonValue(*s)
}

func (s *SurveyState) Scan(src any) error { return jsonScan(src, s) }

// SurveyAnswer — ответ на вопрос опроса. Шкалы 1–10 и 1–5 хранятся с полем Scale и не
// смешиваются в агрегатах.
type SurveyAnswer struct {
	UUIDModel
	UpdatedAt  time.Time
	EmployeeID uuid.UUID `gorm:"type:uuid"`
	EventID    uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_answer_question"`
	QuestionNo int       `gorm:"uniqueIndex:ux_answer_question"` // с единицы
	Scale      string
	Value      *int
	Skipped    bool
	Reason     string     // кнопка причины
	Text       string     // причина своими словами (текст новичка)
	Flags      StringList `gorm:"type:jsonb"`
	IsTest     bool
	// Sentiment — тональность Text (domain.Sentiment*): pos | neu | neg; nil — не размечен или
	// текста нет.
	Sentiment *string
	// Разметка /flags v2 (все nil — не размечено или старая запись). ToneConfidence — уверенность
	// модели в тоне 0–1; Quote — дословный фрагмент Text в подтверждение самого серьёзного флага
	// (rag уже проверил: подстрока, без ПДн, при F10 пусто); Summary — строка для HR ≤ 120
	// символов, в третьем лице, без ПДн (при F10 пусто).
	ToneConfidence *float64
	Quote          *string
	Summary        *string
}
