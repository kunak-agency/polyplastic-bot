package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Решение по испытательному сроку, наличие личного файла и пользователи с MAX для push
// (FEATURE_RISK_V2: services/manager*.go, services/recruiter_morning.go).

// SetIPDecision — решение по ИС точечно, без перезаписи карточки (бот параллельно пишет свои поля).
// decision nil — сбросить.
func (r *EmployeeRepo) SetIPDecision(ctx context.Context, id uuid.UUID, decision *string, at time.Time, by string) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).
		Updates(map[string]any{"ip_decision": decision, "ip_decision_at": at, "ip_decision_by": by}).Error
}

// WithFile — у каких новичков из ids есть личный файл name (например, адаптационный план).
func (r *EmployeeRepo) WithFile(ctx context.Context, ids []uuid.UUID, name string) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var have []uuid.UUID
	if err := r.db.WithContext(ctx).Model(&domain.NewcomerFile{}).
		Where("employee_id IN ? AND name = ?", ids, name).Pluck("employee_id", &have).Error; err != nil {
		return nil, err
	}
	for _, id := range have {
		out[id] = true
	}
	return out, nil
}

// WithMax — активные пользователи кабинета роли role с привязкой к MAX (users.max_user_id).
func (r *UserRepo) WithMax(ctx context.Context, role string) ([]domain.User, error) {
	var out []domain.User
	err := r.db.WithContext(ctx).Where("active AND max_user_id IS NOT NULL AND role = ?", role).
		Order("full_name").Find(&out).Error
	return out, err
}
