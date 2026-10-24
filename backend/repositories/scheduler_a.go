package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Запросы планировщика итерации 24.10 (services/scheduler_*.go): наджи, догоняющая очередь,
// пояс новичка. Схему не меняют — только читают и точечно обновляют поля волны 0.

// OpenQuestions — отправленные вопросы новичка без ответа (кнопки-ответы, ввод, опрос), по
// времени отправки: первым — самый старый. Это и «текущий незакрытый вопрос» ручного наджа.
func (r *EventRepo) OpenQuestions(ctx context.Context, employeeID uuid.UUID) ([]domain.ScheduledEvent, error) {
	var out []domain.ScheduledEvent
	err := r.db.WithContext(ctx).
		Where("employee_id = ? AND status = ? AND sent_at IS NOT NULL AND (waiting = ? OR kind = ?)",
			employeeID, domain.EventSent, domain.WaitAnswer, domain.KindSurvey).
		Order("sent_at, position").Find(&out).Error
	return out, err
}

// AwaitingEmployees — новички с отправленными и неотвеченными вопросами, ушедшими не позже
// before (кандидаты на автонадж).
func (r *EventRepo) AwaitingEmployees(ctx context.Context, before time.Time, limit int) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Where("status = ? AND sent_at IS NOT NULL AND sent_at <= ? AND (waiting = ? OR kind = ?)",
			domain.EventSent, before, domain.WaitAnswer, domain.KindSurvey).
		Distinct("employee_id").Limit(limit).Pluck("employee_id", &out).Error
	return out, err
}

// CatchupBlocksSince — сколько разных блоков догоняющей очереди ушло новичку с since (сводка
// «Что вы пропустили» не считается: её ключ — skipKey).
func (r *EventRepo) CatchupBlocksSince(ctx context.Context, employeeID uuid.UUID, since time.Time, skipKey string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Where("employee_id = ? AND catchup AND sent_at >= ? AND block <> ?", employeeID, since, skipKey).
		Distinct("block").Count(&n).Error
	return n, err
}

// CountSince — напоминания новичку с since; kind "" — любые.
func (r *NudgeRepo) CountSince(ctx context.Context, employeeID uuid.UUID, kind string, since time.Time) (int64, error) {
	q := r.db.WithContext(ctx).Model(&domain.Nudge{}).
		Where("employee_id = ? AND sent_at >= ? AND message_id <> '' AND message_id <> ?", employeeID, since, NudgeUndelivered)
	if kind != "" {
		q = q.Where("kind = ?", kind)
	} else {
		q = q.Where("kind IN ?", []string{domain.NudgeAuto, domain.NudgeManual}) // не «связался» (team.go)
	}
	var n int64
	return n, q.Count(&n).Error
}

// NudgeUndelivered — message_id ручного наджа, который не ушёл (MAX отказал): строка остаётся в
// истории, но в бюджет и в «ждёт отправки» не попадает.
const NudgeUndelivered = "-"

// DuePlanned — ручные наджи, отложенные до рабочего окна новичка (message_id пусто, sent_at —
// плановое время), чьё время пришло.
func (r *NudgeRepo) DuePlanned(ctx context.Context, now time.Time, limit int) ([]domain.Nudge, error) {
	var out []domain.Nudge
	err := r.db.WithContext(ctx).Where("kind = ? AND message_id = '' AND sent_at <= ?", domain.NudgeManual, now).
		Order("sent_at").Limit(limit).Find(&out).Error
	return out, err
}

// Planned — отложенный ручной надж новичка, ещё не ушедший (nil — нет).
func (r *NudgeRepo) Planned(ctx context.Context, employeeID uuid.UUID) (*domain.Nudge, error) {
	var n domain.Nudge
	err := r.db.WithContext(ctx).Where("employee_id = ? AND kind = ? AND message_id = ''", employeeID, domain.NudgeManual).
		Order("sent_at").First(&n).Error
	return ignoreNotFound(&n, err)
}

// MarkSent — надж ушёл (или не ушёл: mid = NudgeUndelivered).
func (r *NudgeRepo) MarkSent(ctx context.Context, id uuid.UUID, mid string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Nudge{}).Where("id = ?", id).
		Updates(map[string]any{"message_id": mid, "sent_at": at}).Error
}

// SetUnresponsive — статус «не отвечает» (nil — снять). Точечно: карточку целиком не пишем.
func (r *EmployeeRepo) SetUnresponsive(ctx context.Context, id uuid.UUID, since *time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).
		Update("unresponsive_since", since).Error
}

// SetCatchupStarted — догоняющая очередь собрана.
func (r *EmployeeRepo) SetCatchupStarted(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).
		Updates(map[string]any{"catchup_started_at": at, "catchup_done_at": nil}).Error
}

// SetCatchupDone — догоняющая очередь пройдена («Догнал ✓»).
func (r *EmployeeRepo) SetCatchupDone(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ? AND catchup_done_at IS NULL", id).
		Update("catchup_done_at", at).Error
}

// CatchingUp — новички, у которых очередь собрана и ещё не пройдена.
func (r *EmployeeRepo) CatchingUp(ctx context.Context, limit int) ([]uuid.UUID, error) {
	var out []uuid.UUID
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Where("catchup_started_at IS NOT NULL AND catchup_done_at IS NULL").
		Limit(limit).Pluck("id", &out).Error
	return out, err
}
