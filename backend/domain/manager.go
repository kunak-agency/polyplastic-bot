package domain

import (
	"time"

	"github.com/google/uuid"
)

// Руководитель подразделения (кейс, роль 03; UX 1 и 3.7). Отдельной учётной записи нет:
// руководитель — это manager_name в карточке новичка плюс, после входа по своей ссылке,
// Employee.ManagerUserID (MAX user_id) у всех новичков с тем же manager_name.

// ManagerPayloadPrefix — payload deep link руководителя: mgr_<token>.
const ManagerPayloadPrefix = "mgr_"

// ManagerInvite — ссылка руководителя (по аналогии с InviteToken новичка): случайная строка без
// ПДн. Выдаётся с карточки новичка, привязывает всех новичков с тем же ManagerName.
type ManagerInvite struct {
	UUIDModel
	EmployeeID  uuid.UUID `gorm:"type:uuid"`
	ManagerName string
	Token       string `gorm:"uniqueIndex;size:128"`
	ExpiresAt   time.Time
	UsedAt      *time.Time
	UsedBy      *int64
}

// Поводы push руководителю (правила кода, services/manager_rules.go).
const (
	NudgeLowPulseTwice = "low_pulse_twice" // второй низкий пульс подряд
	NudgeSilent        = "silent_3d"       // не отвечает боту больше 3 дней
	NudgeProbation     = "probation_3d"    // решение по испытательному сроку через 3 дня
	NudgeMonth         = "d30_meeting"     // D+30 — встреча 1:1
	// FEATURE_RISK_V2 (services/manager_rules.go):
	NudgeRegular    = "regular_meeting" // плановая встреча: D+14 всем и нейтральный повод вместо скрытого
	NudgePlanNeeded = "plan_needed"     // D+1: адаптационный план не приложен
	NudgeIPDiscuss  = "ip_discuss"      // руководитель нажал «Обсудить с HR» под D+87 — задача HR
	// NudgeRecruiterMorning — утренняя сводка рекрутеру (services/recruiter_morning.go): та же
	// таблица как журнал push с идемпотентным Key, EmployeeID пустой.
	NudgeRecruiterMorning = "recruiter_morning"
)

// Статусы задачи «Встреча 1:1».
const (
	NudgeOpen    = "open"    // push ушёл, ответа нет
	NudgePlanned = "planned" // «Запланирую» — задача ещё не закрыта
	NudgeMet     = "met"     // «Встретился» — закрыта
)

// ManagerNudge — push руководителю и задача «Встреча 1:1» по нему. Key — идемпотентность:
// одно правило по одному поводу шлётся один раз (новичок/правило/повод).
type ManagerNudge struct {
	UUIDModel
	UpdatedAt     time.Time
	EmployeeID    uuid.UUID `gorm:"type:uuid"`
	ManagerUserID int64
	Kind          string
	Key           string `gorm:"uniqueIndex;size:200"`
	Text          string // текст push: только повод, без ответов новичка и категорий
	Status        string
	MessageID     string
	SentAt        *time.Time
	AnsweredAt    *time.Time
}
