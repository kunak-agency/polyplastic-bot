// Package domain — GORM-модели: новички, приглашения, сценарии с версиями, события таймлайна,
// ответы опросов, состояние бота.
package domain

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UUIDModel — общая база «крупных» сущностей: UUIDv7 PK (в BeforeCreate) + created_at.
// Обычных индексов схема не заводит: индекс — оптимизация, и без измеренной проблемы он
// стоит записью в лишнее дерево. Уникальные индексы — ограничение целостности, приезжают
// тегами моделей.
type UUIDModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time
}

func (m *UUIDModel) BeforeCreate(*gorm.DB) error {
	if m.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		m.ID = id
	}
	return nil
}

// AllModels — список для AutoMigrate. ЕДИНСТВЕННОЕ место, где модель включается в схему:
// файл в domain/ без строки здесь таблицы не создаёт.
func AllModels() []any {
	return []any{
		&Employee{},
		&InviteToken{},
		&HumanRequest{},
		&Scenario{},
		&ScenarioVersion{},
		&ScenarioStep{},
		&ScheduledEvent{},
		&SurveyAnswer{},
		&MarkerState{},
		&BotQuestion{},
		&Ticket{},
		&TicketMessage{},
		&BotChat{},
		&KBDocument{},
		&KBDocumentVersion{},
		&KBDocumentFile{},
		&ManagerInvite{},
		&ManagerNudge{},
		&Media{},
		&MediaFile{},
		&MediaFileData{},
		&NewcomerFile{},
	}
}

// jsonValue / jsonScan — общая часть jsonb-колонок. Свои типы с Valuer/Scanner, а не тег
// serializer: срез структур с тегом GORM иначе может принять за связь.
func jsonValue(v any) (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func jsonScan(src any, dst any) error {
	switch v := src.(type) {
	case nil:
		return nil
	case []byte:
		return json.Unmarshal(v, dst)
	case string:
		return json.Unmarshal([]byte(v), dst)
	}
	return fmt.Errorf("jsonb: неожиданный тип %T", src)
}

// StringList — jsonb-массив строк (метки площадок).
type StringList []string

func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		l = StringList{}
	}
	return jsonValue([]string(l))
}

func (l *StringList) Scan(src any) error { return jsonScan(src, (*[]string)(l)) }

// Has — есть ли значение в списке.
func (l StringList) Has(v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
