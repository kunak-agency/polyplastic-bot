package repositories

import (
	"context"

	"main/domain"

	"github.com/google/uuid"
)

// CatchupProgress — «догоняет done из total»: события догоняющей очереди новичка
// (ScheduledEvent.Catchup). Пройдено — отвечено, пропущено или не ушло, либо отправлено и ничего
// не ждёт. Заготовка волны 0: правило «пройдено» может уточнить планировщик.
func (r *EventRepo) CatchupProgress(ctx context.Context, employeeID uuid.UUID) (done, total int64, err error) {
	var row struct{ Done, Total int64 }
	err = r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Select(`COUNT(*) FILTER (WHERE status IN (?, ?, ?) OR (status = ? AND waiting = '')) AS done,
			COUNT(*) AS total`, domain.EventAnswered, domain.EventSkipped, domain.EventFailed, domain.EventSent).
		Where("employee_id = ? AND catchup", employeeID).Scan(&row).Error
	return row.Done, row.Total, err
}
