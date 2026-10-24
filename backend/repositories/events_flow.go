package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Исполнение блока и веток (services/branch*.go, api_contract.md v4 «Исполнение блока»).

// SetBlock — ключ блока у события, созданного до появления блоков (дозаполнение на лету).
func (r *EventRepo) SetBlock(ctx context.Context, id uuid.UUID, block string) error {
	return r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).Where("id = ?", id).Update("block", block).Error
}

// HeldOf — шаги «по отмашке», ждущие флага.
func (r *EventRepo) HeldOf(ctx context.Context, employeeID uuid.UUID) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).Where("employee_id = ? AND status = ?", employeeID, domain.EventHeld).
		Order("fire_at, position").Find(&out).Error
	return out, err
}

// AwaitingAnswer — отправленные вопросы без ответа, ушедшие не позже before (напоминания
// no_answer).
func (r *EventRepo) AwaitingAnswer(ctx context.Context, before time.Time, limit int) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).
		Where("status = ? AND waiting = ? AND sent_at IS NOT NULL AND sent_at <= ?", domain.EventSent, domain.WaitAnswer, before).
		Order("sent_at").Limit(limit).Find(&out).Error
	return out, err
}

// OpenSurveys — отправленные и не законченные опросы, ушедшие не позже before (правило r70 —
// напоминание об опросе).
func (r *EventRepo) OpenSurveys(ctx context.Context, before time.Time, limit int) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).
		Where("status = ? AND kind = ? AND sent_at IS NOT NULL AND sent_at <= ?", domain.EventSent, domain.KindSurvey, before).
		Order("sent_at").Limit(limit).Find(&out).Error
	return out, err
}

// MinVersion — наименьший номер версии событий (сотрудник, шаг): «Отправить себе» пишет
// события с отрицательными номерами, чтобы не задеть уникальный индекс настоящих версий.
func (r *EventRepo) MinVersion(ctx context.Context, employeeID, stepID uuid.UUID) (int, error) {
	var v *int
	err := r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Where("employee_id = ? AND step_id = ?", employeeID, stepID).Select("MIN(scenario_version)").Scan(&v).Error
	if err != nil || v == nil {
		return 0, err
	}
	return *v, nil
}

// Create — одно событие (send-test).
func (r *EventRepo) Create(ctx context.Context, e *domain.ScheduledEvent) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// SetAwaitSurvey — бот ждёт текст к вопросу опроса (вопрос типа text). Точечное обновление:
// карточку целиком из устаревшей копии не перезаписываем.
func (r *EmployeeRepo) SetAwaitSurvey(ctx context.Context, id uuid.UUID, eventID *uuid.UUID, question int) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).
		Updates(map[string]any{"await_event_id": eventID, "await_question": question}).Error
}
