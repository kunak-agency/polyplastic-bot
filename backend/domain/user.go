package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// Роли пользователей кабинета (матрица ролей — research/2026-10-24-iteration-plan.md, п. 4).
const (
	RoleHRLead    = "hr_lead"   // руководитель HR / подбора: всё по всем площадкам, пользователи
	RoleRecruiter = "recruiter" // рекрутер: свои новички (Employee.RecruiterID)
	RoleManager   = "manager"   // руководитель подразделения: только подчинённые, без текстов ответов
)

// Roles — допустимые роли.
var Roles = []string{RoleHRLead, RoleRecruiter, RoleManager}

// User — пользователь кабинета. Вход — логин и пароль (хэш argon2id или bcrypt), сессии — Session.
type User struct {
	UUIDModel
	UpdatedAt time.Time

	Login        string `gorm:"uniqueIndex;size:100"`
	PasswordHash string
	FullName     string
	Role         string // hr_lead | recruiter | manager
	// Active — false: деактивирован, войти не может. default:true: GORM на Create подменяет
	// нулевое значение дефолтом, поэтому выключенного создаёт только UserRepo.Create.
	Active bool `gorm:"not null;default:true"`
	// TZ — IANA-пояс пользователя, только для отображения; nil — пояс браузера/площадки.
	TZ *string
	// ManagerName — для роли manager: имя, как в Employee.ManagerName; по нему руководитель
	// видит своих подчинённых. nil — у пользователя нет подчинённых.
	ManagerName *string
}

// SessionTTL — срок жизни сессии кабинета (cookie HttpOnly; Secure; SameSite=Lax).
const SessionTTL = 12 * time.Hour

// Session — сессия кабинета. ID — случайные 32 байта в hex (64 символа), он же значение cookie.
type Session struct {
	ID        string    `gorm:"primaryKey;size:64"`
	UserID    uuid.UUID `gorm:"type:uuid"`
	ExpiresAt time.Time
	CreatedIP string
	CreatedAt time.Time
}

// NewSessionID — 32 случайных байта из crypto/rand в hex.
func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
