package domain

import (
	"time"

	"github.com/google/uuid"
)

// NewcomerFile — личный файл новичка в карточке («adaptation_plan» — адаптационный план от
// руководителя, ТЗ r36). Шаг ссылается на него media {"personal":"adaptation_plan"}. MaxToken —
// кэш токена вложения MAX: файл загружается один раз.
type NewcomerFile struct {
	UUIDModel
	UpdatedAt  time.Time
	EmployeeID uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_newcomer_file"`
	Name       string    `gorm:"uniqueIndex:ux_newcomer_file"`
	Filename   string
	Mime       string
	Size       int64
	Data       []byte
	MaxToken   string
}

// PersonalAdaptationPlan — имя личного файла «адаптационный план».
const PersonalAdaptationPlan = "adaptation_plan"
