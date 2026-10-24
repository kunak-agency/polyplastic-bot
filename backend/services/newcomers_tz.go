package services

import (
	"context"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Карточка новичка: пояс, «не отвечает», догоняющая очередь, наджи (итерация 24.10).

// NewcomerSchedule — поля расписания карточки.
type NewcomerSchedule struct {
	// TZ — пояс, по которому живёт расписание новичка (с FEATURE_TZ — Employee.TZ или пояс
	// площадки, без флага — пояс площадки); SiteTZ — пояс площадки. Фронт: «10:00 по времени
	// GTP (UTC+5)».
	TZ, SiteTZ string
	// Unresponsive — «не отвечает» с этого момента; nil — отвечает (ответил после статуса — тоже nil).
	Unresponsive *time.Time
	// Catchup — догоняющая очередь позднего входа; nil — не собиралась.
	Catchup *CatchupState
}

// schedule — поля без запросов в базу (Catchup дозаполняют Get и List).
func (s *NewcomerService) schedule(e *domain.Employee) NewcomerSchedule {
	var out NewcomerSchedule
	out.TZ, out.SiteTZ = s.scheduler.EffectiveTZ(e)
	if u := e.UnresponsiveSince; u != nil && (e.LastActivityAt == nil || !e.LastActivityAt.After(*u)) {
		out.Unresponsive = u
	}
	return out
}

// Patch — PATCH карточки: перенос даты выхода (start) и/или пояс новичка (tz: "" — пояс
// площадки). Недоставленные события пересчитываются под новые дату и пояс (с FEATURE_TZ; без
// флага пояс сохраняется, но расписание его не учитывает).
func (s *NewcomerService) Patch(ctx context.Context, id uuid.UUID, start *time.Time, tz *string) (*NewcomerCard, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError(msgNewcomerNotFound)
		}
		if start != nil {
			e.StartDate = *start
		}
		if tz != nil {
			if *tz == "" {
				e.TZ = nil
			} else {
				v := *tz
				e.TZ = &v
			}
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		if start != nil {
			if err := tx.Employees.ExtendInvites(ctx, e.ID, s.inviteExpiry(*start)); err != nil {
				return err
			}
		}
		return s.scheduler.Reschedule(ctx, tx, e)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Nudge — «Напомнить» из карточки (SchedulerService.NudgeNow).
func (s *NewcomerService) Nudge(ctx context.Context, id, userID uuid.UUID) (*NudgeResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.scheduler.NudgeNow(ctx, id, userID)
}

// Nudges — история напоминаний и пояс, в котором её показывать.
func (s *NewcomerService) Nudges(ctx context.Context, id uuid.UUID) ([]NudgeItem, string, error) {
	if err := s.ready(); err != nil {
		return nil, "", err
	}
	return s.scheduler.Nudges(ctx, id)
}
