package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NudgeRepo — журнал напоминаний новичку (domain.Nudge). Заготовка волны 0.
type NudgeRepo struct {
	db *gorm.DB
}

// Create — записать напоминание.
func (r *NudgeRepo) Create(ctx context.Context, n *domain.Nudge) error {
	return r.db.WithContext(ctx).Create(n).Error
}

// ListByEmployee — напоминания новичку по времени отправки.
func (r *NudgeRepo) ListByEmployee(ctx context.Context, employeeID uuid.UUID) ([]domain.Nudge, error) {
	var out []domain.Nudge
	err := r.db.WithContext(ctx).Where("employee_id = ?", employeeID).Order("sent_at, id").Find(&out).Error
	return out, err
}

// HolidayRepo — праздничные дни (domain.Holiday). Заготовка волны 0.
type HolidayRepo struct {
	db *gorm.DB
}

// Create — добавить праздник; дата — день без времени (колонка date).
func (r *HolidayRepo) Create(ctx context.Context, h *domain.Holiday) error {
	return r.db.WithContext(ctx).Create(h).Error
}

// List — праздники в [from, to] по дате; нулевые границы — без ограничения.
func (r *HolidayRepo) List(ctx context.Context, from, to time.Time) ([]domain.Holiday, error) {
	q := r.db.WithContext(ctx).Order("date")
	if !from.IsZero() {
		q = q.Where("date >= ?", from.Format(time.DateOnly))
	}
	if !to.IsZero() {
		q = q.Where("date <= ?", to.Format(time.DateOnly))
	}
	var out []domain.Holiday
	return out, q.Find(&out).Error
}
