package domain

import (
	"time"

	"github.com/google/uuid"
)

// Значения полей сотрудника (api_contract.md, v1).
const (
	AddressTy = "ty"
	AddressVy = "vy"

	ConsentGiven    = "given"
	ConsentDeclined = "declined"

	ConnInvited   = "invited"
	ConnConnected = "connected"
	ConnStopped   = "stopped"

	RiskGreen  = "green"
	RiskYellow = "yellow"
	RiskRed    = "red"
)

// Employee — новичок. Данные демо синтетические; телефон хранится как ввёл HR, потому что
// контракт v1 его отдаёт (архитектура предлагала только хэш — см. отчёт итерации 1).
type Employee struct {
	UUIDModel
	UpdatedAt time.Time

	FullName      string
	Phone         string
	Site          string
	Department    string
	Position      string
	ManagerName   string
	MentorName    string
	RecruiterName string
	StartDate     time.Time `gorm:"type:date"`
	Shift         *string   // "1" | "2" | "3" | nil

	ScenarioID      uuid.UUID `gorm:"type:uuid"`
	ScenarioVersion int

	AddressForm *string // ty | vy | nil — ещё не выбрал
	Consent     *string // given | declined | nil
	ConsentAt   *time.Time
	Connection  string // invited | connected | stopped

	// MaxUserID, ChatID — связка с MAX после bot_started. Уникальность: один аккаунт MAX — один
	// активный новичок (при новом приглашении старая связка снимается).
	MaxUserID *int64 `gorm:"uniqueIndex"`
	ChatID    *int64
	// ManagerUserID — MAX user_id руководителя подразделения (вход по ссылке mgr_<token>,
	// services/manager.go); nil — руководитель к боту не подключён, push только в лог.
	ManagerUserID *int64

	LastActivityAt *time.Time
	LastPulse      *int
	Risk           string
	RiskReason     *string

	// VirtualDay — до какого дня HR «промотал» машиной времени; nil — не мотал. Только для
	// справки: day и start_date от него не зависят.
	VirtualDay *int
	// IsTest — были тестовые отправки (машина времени): в аналитику не попадает.
	IsTest bool

	// AwaitEventID, AwaitQuestion — бот ждёт свободный текст причины к вопросу опроса
	// («Напишу сам(а)»). nil — не ждёт.
	AwaitEventID  *uuid.UUID `gorm:"type:uuid"`
	AwaitQuestion int

	// Flags — булевы флаги новичка (set_flag ветки, «по отмашке рекрутера» — flag_required шага).
	Flags BoolMap `gorm:"type:jsonb"`
	// AwaitFreeText — бот ждёт свободный текст («Другое», open_free_text) → обращение.
	AwaitFreeText *FreeTextWait `gorm:"type:jsonb"`
}

// InviteToken — приглашение: случайная строка (≤ 128 символов, без ПДн) для deep link.
type InviteToken struct {
	UUIDModel
	EmployeeID uuid.UUID `gorm:"type:uuid"`
	Token      string    `gorm:"uniqueIndex;size:128"`
	ExpiresAt  time.Time
	UsedAt     *time.Time
}

// HumanRequest — новичок попросил человека («Спросить человека», свободный вопрос). В итерации 1
// это только запись намерения; обращения с маршрутами — итерация 3. Текста вопроса здесь нет.
type HumanRequest struct {
	UUIDModel
	EmployeeID uuid.UUID  `gorm:"type:uuid"`
	EventID    *uuid.UUID `gorm:"type:uuid"`
	Source     string     // button | free_text | answer
	Flags      StringList `gorm:"type:jsonb"` // коды regex-триггеров, если были
	// QuestionID — свободный вопрос (BotQuestion), из-под ответа на который попросили человека.
	QuestionID *uuid.UUID `gorm:"type:uuid"`
	// Target — кому передать, как выбрал новичок или определил флаг: hr | recruiter |
	// security | hr_partner; пусто — не выбран (итерация 1). Маршрут решает итерация 3.
	Target string
}

// Источники HumanRequest.
const (
	HumanFromButton   = "button"
	HumanFromFreeText = "free_text"
	HumanFromAnswer   = "answer" // кнопка под ответом, отказом или сигналом флага
)

// Адресаты HumanRequest.Target.
const (
	TargetHR        = "hr"
	TargetRecruiter = "recruiter"
	TargetSecurity  = "security"
	TargetHRPartner = "hr_partner"
)

// MarkerState — marker long polling MAX: после рестарта бот читает с того же места.
type MarkerState struct {
	Key       string `gorm:"primaryKey"`
	Marker    int64
	UpdatedAt time.Time
}

// MarkerKeyMax — ключ строки marker бота.
const MarkerKeyMax = "max_updates"

// BotChat — групповой чат, куда добавили бота (bot_added). Только для справки: куда слать
// карточки обращений, решает env (MAX_HR_CHAT_ID / MAX_SB_CHAT_ID), а не чат сам себе — иначе
// любой, кто добавит бота в свою группу, мог бы перехватить карточки СБ.
type BotChat struct {
	ChatID    int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Removed   bool
}
