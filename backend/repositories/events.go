package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EventRepo — события таймлайна и ответы опросов.
type EventRepo struct {
	db *gorm.DB
}

// CreateMissing — вставить события, которых ещё нет: (сотрудник, шаг, версия) уже есть — строка
// пропускается молча. Это и есть «без дублей» при повторной развёртке.
func (r *EventRepo) CreateMissing(ctx context.Context, events []domain.ScheduledEvent) error {
	if len(events) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error
}

func (r *EventRepo) Save(ctx context.Context, e *domain.ScheduledEvent) error {
	return r.db.WithContext(ctx).Save(e).Error
}

// ByEmployee — таймлайн сотрудника по времени.
func (r *EventRepo) ByEmployee(ctx context.Context, employeeID uuid.UUID) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).Where("employee_id = ?", employeeID).Order("fire_at, position").Find(&out).Error
	return out, err
}

// ByIDForUpdate — событие под блокировкой строки (ответ на кнопку живого опроса).
func (r *EventRepo) ByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.ScheduledEvent, error) {
	var e domain.ScheduledEvent
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", id).Error
	return ignoreNotFound(&e, err)
}

// DueFilter — какие события забрать на отправку.
type DueFilter struct {
	Now        time.Time
	EmployeeID *uuid.UUID // только этого сотрудника
	// MaxDay — машина времени: все неотправленные с day_offset ≤ MaxDay, без оглядки на fire_at.
	MaxDay *int
	Limit  int
}

// LockDue — созревшие события под FOR UPDATE SKIP LOCKED: второй поллер, машина времени и
// перезапуск не заберут то же событие. Только подключённые новички; без ответа на согласие —
// никаких опросов (в обычном режиме — вообще ничего: сначала согласие, потом таймлайн).
// Звать внутри транзакции — блокировки живут до её конца.
func (r *EventRepo) LockDue(ctx context.Context, f DueFilter) ([]domain.ScheduledEvent, error) {
	if f.Limit <= 0 {
		f.Limit = 100
	}
	var out []domain.ScheduledEvent
	err := r.dueQuery(ctx, f).Select("e.*").Order("e.employee_id, e.fire_at, e.position").Limit(f.Limit).
		Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "e"}, Options: "SKIP LOCKED"}).
		Find(&out).Error
	return out, err
}

// DueEmployees — сотрудники с созревшими событиями (без блокировки). Такт отправляет их по
// одному: новичок, который не отвечает и копит созревшие реплики за вопросом, не забирает лимит
// выборки у остальных.
func (r *EventRepo) DueEmployees(ctx context.Context, f DueFilter) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := r.dueQuery(ctx, f).Distinct("e.employee_id").Pluck("e.employee_id", &out).Error
	return out, err
}

func (r *EventRepo) dueQuery(ctx context.Context, f DueFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("scheduled_events AS e").
		Joins("JOIN employees m ON m.id = e.employee_id").
		Where("e.status = ? AND m.connection = ?", domain.EventScheduled, domain.ConnConnected).
		// без привязки к MAX слать некуда: такие карточки такт не берёт
		Where("(m.max_user_id IS NOT NULL OR m.chat_id IS NOT NULL)")
	if f.MaxDay != nil {
		q = q.Where("e.day_offset <= ?", *f.MaxDay).
			Where("(m.consent IS NOT NULL OR e.kind <> ?)", domain.KindSurvey)
	} else {
		q = q.Where("e.fire_at <= ? AND m.consent IS NOT NULL", f.Now)
	}
	if f.EmployeeID != nil {
		q = q.Where("e.employee_id = ?", *f.EmployeeID)
	}
	return q
}

// ScheduledOf — неотправленные события сотрудника (перенос даты, пересборка версии).
func (r *EventRepo) ScheduledOf(ctx context.Context, employeeID uuid.UUID) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).Where("employee_id = ? AND status = ?", employeeID, domain.EventScheduled).
		Order("fire_at, position").Find(&out).Error
	return out, err
}

// DeleteScheduled — убрать неотправленные события сотрудника (публикация «к активным»).
func (r *EventRepo) DeleteScheduled(ctx context.Context, employeeID uuid.UUID) error {
	return r.db.WithContext(ctx).Where("employee_id = ? AND status = ?", employeeID, domain.EventScheduled).
		Delete(&domain.ScheduledEvent{}).Error
}

// DeliveredStepIDs — шаги, которые сотрудник уже получил в любой версии.
func (r *EventRepo) DeliveredStepIDs(ctx context.Context, employeeID uuid.UUID) (map[uuid.UUID]bool, error) {
	var ids []uuid.UUID
	err := r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Where("employee_id = ? AND status <> ?", employeeID, domain.EventScheduled).Pluck("step_id", &ids).Error
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// OpenSurvey — последний отправленный и не законченный опрос сотрудника: к нему цепляются
// текстовые ответы («7», «семь»).
func (r *EventRepo) OpenSurvey(ctx context.Context, employeeID uuid.UUID) (*domain.ScheduledEvent, error) {
	var e domain.ScheduledEvent
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("employee_id = ? AND kind = ? AND status = ?", employeeID, domain.KindSurvey, domain.EventSent).
		Order("sent_at DESC, fire_at DESC, position DESC").First(&e).Error
	return ignoreNotFound(&e, err)
}

// UpsertAnswer — ответ на вопрос; повторный ответ на тот же вопрос перезаписывает поля.
func (r *EventRepo) UpsertAnswer(ctx context.Context, a *domain.SurveyAnswer) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_id"}, {Name: "question_no"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "skipped", "reason", "text", "flags", "is_test", "updated_at"}),
	}).Create(a).Error
}

// Answers — ответы на опрос события по номеру вопроса.
func (r *EventRepo) Answers(ctx context.Context, eventID uuid.UUID) ([]domain.SurveyAnswer, error) {
	var out []domain.SurveyAnswer
	return out, r.db.WithContext(ctx).Where("event_id = ?", eventID).Order("question_no").Find(&out).Error
}

// Answer — ответ на конкретный вопрос.
func (r *EventRepo) Answer(ctx context.Context, eventID uuid.UUID, questionNo int) (*domain.SurveyAnswer, error) {
	var a domain.SurveyAnswer
	err := r.db.WithContext(ctx).First(&a, "event_id = ? AND question_no = ?", eventID, questionNo).Error
	return ignoreNotFound(&a, err)
}
