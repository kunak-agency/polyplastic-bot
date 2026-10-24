package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Чтение для экрана «Показатели» (GET /api/indicators): к строкам обзора (OverviewRepo.Load)
// добавляются свободные вопросы без текста и время первого ответа сотрудника по обращениям.
// Только SELECT; агрегаты считает services/indicators.go.

// IndicatorQuestion — свободный вопрос новичка без текста: чем кончился ответ бота.
type IndicatorQuestion struct {
	EmployeeID uuid.UUID
	Kind       string
	NotThat    int
	HumanAt    *time.Time
	TicketID   *uuid.UUID
	IsTest     bool
	CreatedAt  time.Time
}

// IndicatorRows — дополнительные строки «Показателей».
type IndicatorRows struct {
	Questions []IndicatorQuestion
	// FirstStaff — обращение → время первого ответа сотрудника (не заметки).
	FirstStaff map[uuid.UUID]time.Time
}

// Indicators — вопросы и первые ответы сотрудников по обращениям, созданным не раньше since.
func (r *OverviewRepo) Indicators(ctx context.Context, since time.Time) (*IndicatorRows, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	db := r.db.WithContext(ctx)
	out := &IndicatorRows{FirstStaff: map[uuid.UUID]time.Time{}}
	if err := db.Model(&domain.BotQuestion{}).
		Select("employee_id, kind, not_that, human_at, ticket_id, is_test, created_at").
		Where("created_at >= ?", since).Order("created_at").
		Scan(&out.Questions).Error; err != nil {
		return nil, err
	}
	var first []struct {
		TicketID uuid.UUID
		FirstAt  time.Time
	}
	if err := db.Raw(`
SELECT m.ticket_id, min(m.created_at) AS first_at
FROM ticket_messages m JOIN tickets t ON t.id = m.ticket_id
WHERE m."from" = ? AND NOT m.internal AND t.created_at >= ?
GROUP BY m.ticket_id`, domain.TicketMsgStaff, since).Scan(&first).Error; err != nil {
		return nil, err
	}
	for _, f := range first {
		out.FirstStaff[f.TicketID] = f.FirstAt
	}
	return out, nil
}
