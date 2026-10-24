package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Запросы входа и ролей (FEATURE_AUTH): команда рекрутеров, закрепление новичков, пользователи
// по фильтру. Только чтение и точечные UPDATE; схему не трогают.

// UserFilter — GET /api/users?role=&active=. Пустое поле — без фильтра.
type UserFilter struct {
	Role   string
	Active *bool
}

// ListBy — пользователи по фильтру, по имени.
func (r *UserRepo) ListBy(ctx context.Context, f UserFilter) ([]domain.User, error) {
	q := r.db.WithContext(ctx).Model(&domain.User{})
	if f.Role != "" {
		q = q.Where("role = ?", f.Role)
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	var out []domain.User
	return out, q.Order("full_name, login").Find(&out).Error
}

// ByIDs — пользователи по id (имена рекрутеров для карточек).
func (r *UserRepo) ByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.User, error) {
	out := map[uuid.UUID]domain.User{}
	if len(ids) == 0 {
		return out, nil
	}
	var list []domain.User
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&list).Error
	for _, u := range list {
		out[u.ID] = u
	}
	return out, err
}

// Patch — точечное обновление полей пользователя (map: колонка → значение).
func (r *UserRepo) Patch(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).Updates(fields).Error
}

// LinkRecruitersByName — миграция данных: новичкам без RecruiterID проставить рекрутера, чьё
// full_name совпадает с текстовым RecruiterName карточки. Сколько карточек обновлено.
func (r *EmployeeRepo) LinkRecruitersByName(ctx context.Context) (int64, error) {
	res := r.db.WithContext(ctx).Exec(`UPDATE employees e SET recruiter_id = u.id
		FROM users u
		WHERE e.recruiter_id IS NULL AND u.role = ? AND u.active
		  AND lower(trim(e.recruiter_name)) = lower(trim(u.full_name))`, domain.RoleRecruiter)
	return res.RowsAffected, res.Error
}

// SetRecruiter — закрепить новичков за рекрутером (nil — снять). Сколько карточек обновлено.
func (r *EmployeeRepo) SetRecruiter(ctx context.Context, ids []uuid.UUID, recruiterID *uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id IN ?", ids).
		Updates(map[string]any{"recruiter_id": recruiterID, "updated_at": time.Now()})
	return res.RowsAffected, res.Error
}

// RecruiterNameCount — сколько карточек у каждого текстового RecruiterName (сид демо-аккаунтов).
type RecruiterNameCount struct {
	Name string
	N    int
}

// RecruiterNames — текстовые имена рекрутеров в карточках, частые первыми.
func (r *EmployeeRepo) RecruiterNames(ctx context.Context) ([]RecruiterNameCount, error) {
	var out []RecruiterNameCount
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Select("recruiter_name AS name, count(*) AS n").Where("trim(recruiter_name) <> ''").
		Group("recruiter_name").Order("n DESC, name").Scan(&out).Error
	return out, err
}

// ManagerNames — имена руководителей в карточках, частые первыми.
func (r *EmployeeRepo) ManagerNames(ctx context.Context) ([]RecruiterNameCount, error) {
	var out []RecruiterNameCount
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Select("manager_name AS name, count(*) AS n").Where("trim(manager_name) <> ''").
		Group("manager_name").Order("n DESC, name").Scan(&out).Error
	return out, err
}

// RecruiterOf — RecruiterID новичка и найден ли он (проверка «свой новичок» без карточки целиком).
func (r *EmployeeRepo) RecruiterOf(ctx context.Context, id uuid.UUID) (*uuid.UUID, bool, error) {
	var rows []struct{ RecruiterID *uuid.UUID }
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).Select("recruiter_id").Where("id = ?", id).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, false, err
	}
	return rows[0].RecruiterID, true, nil
}

// TicketEmployee — новичок обращения; uuid.Nil — обращения нет.
func (r *TicketRepo) TicketEmployee(ctx context.Context, ticketID uuid.UUID) (uuid.UUID, error) {
	var rows []struct{ EmployeeID uuid.UUID }
	err := r.db.WithContext(ctx).Model(&domain.Ticket{}).Select("employee_id").Where("id = ?", ticketID).Limit(1).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return uuid.Nil, err
	}
	return rows[0].EmployeeID, nil
}

// TeamRow — строка «Команды» по рекрутеру (RecruiterID nil — «без рекрутера»). Времени ответа
// по человеку нет намеренно (ресерч дашбордов §2: без рейтингов) — только медиана по команде.
type TeamRow struct {
	RecruiterID       *uuid.UUID
	Newcomers         int
	TicketsOpen       int
	TicketsSLAOverdue int
}

// Team — новички, открытые и просроченные по SLA обращения по рекрутерам. Тестовые обращения
// не считаются.
func (r *Repos) Team(ctx context.Context, now time.Time) ([]TeamRow, error) {
	if !r.Enabled() {
		return nil, ErrNoDatabase
	}
	var out []TeamRow
	err := r.db.WithContext(ctx).Raw(`
WITH emp AS (
	SELECT recruiter_id, count(*) AS newcomers FROM employees GROUP BY recruiter_id
), tk AS (
	SELECT e.recruiter_id,
		count(*) FILTER (WHERE t.status <> ?) AS tickets_open,
		count(*) FILTER (WHERE t.status IN (?, ?) AND t.sla_due_at IS NOT NULL AND t.sla_due_at < ?) AS tickets_sla_overdue
	FROM tickets t JOIN employees e ON e.id = t.employee_id
	WHERE NOT t.is_test
	GROUP BY e.recruiter_id
)
SELECT emp.recruiter_id, emp.newcomers,
	coalesce(tk.tickets_open, 0) AS tickets_open,
	coalesce(tk.tickets_sla_overdue, 0) AS tickets_sla_overdue
FROM emp
LEFT JOIN tk ON tk.recruiter_id IS NOT DISTINCT FROM emp.recruiter_id`,
		domain.TicketResolved, domain.TicketOpen, domain.TicketAssigned, now).Scan(&out).Error
	return out, err
}

// DashboardEvent — событие для дашборда рекрутера: статус, ожидание ответа, шаг версии (по нему
// сервис узнаёт «обязательный этап»), догоняющая очередь. Без текстов.
type DashboardEvent struct {
	ID              uuid.UUID
	EmployeeID      uuid.UUID
	StepID          uuid.UUID
	ScenarioVersion int
	Kind            string
	Title           string
	Status          string
	Waiting         string
	FireAt          time.Time
	SentAt          *time.Time
	IsTest          bool
	Catchup         bool
}

// DashboardEvents — события новичков ids по времени.
func (r *EventRepo) DashboardEvents(ctx context.Context, ids []uuid.UUID) ([]DashboardEvent, error) {
	var out []DashboardEvent
	if len(ids) == 0 {
		return out, nil
	}
	err := r.db.WithContext(ctx).Model(&domain.ScheduledEvent{}).
		Select("id, employee_id, step_id, scenario_version, kind, title, status, waiting, fire_at, sent_at, is_test, catchup").
		Where("employee_id IN ?", ids).Order("fire_at, position").Scan(&out).Error
	return out, err
}

// NudgeContacted — Nudge.Kind отметки «связался» из дашборда рекрутера. Не напоминание: MessageID
// пустой, в бюджет наджей (CountSince) и в отложенные (DuePlanned, kind=manual) не попадает.
const NudgeContacted = "contacted"

// DashboardSignals — что сделал бот и люди по новичкам ids: авто-напоминания с since, последняя
// отметка «связался», есть ли у новичка план руководителя («Запланирую» / «Встретился»).
type DashboardSignals struct {
	AutoNudges int
	Contacted  map[uuid.UUID]time.Time
	Planned    map[uuid.UUID]bool
}

// DashboardSignals — сигналы для дашборда рекрутера.
func (r *Repos) DashboardSignals(ctx context.Context, ids []uuid.UUID, since time.Time) (*DashboardSignals, error) {
	if !r.Enabled() {
		return nil, ErrNoDatabase
	}
	out := &DashboardSignals{Contacted: map[uuid.UUID]time.Time{}, Planned: map[uuid.UUID]bool{}}
	if len(ids) == 0 {
		return out, nil
	}
	db := r.db.WithContext(ctx)
	var n int64
	if err := db.Model(&domain.Nudge{}).
		Where("employee_id IN ? AND kind = ? AND sent_at >= ? AND message_id <> '' AND message_id <> ?", ids, domain.NudgeAuto, since, NudgeUndelivered).
		Count(&n).Error; err != nil {
		return nil, err
	}
	out.AutoNudges = int(n)
	var contacted []struct {
		EmployeeID uuid.UUID
		At         time.Time
	}
	if err := db.Model(&domain.Nudge{}).Select("employee_id, max(sent_at) AS at").
		Where("employee_id IN ? AND kind = ?", ids, NudgeContacted).Group("employee_id").Scan(&contacted).Error; err != nil {
		return nil, err
	}
	for _, c := range contacted {
		out.Contacted[c.EmployeeID] = c.At
	}
	var planned []uuid.UUID
	if err := db.Model(&domain.ManagerNudge{}).Distinct("employee_id").
		Where("employee_id IN ? AND status IN ?", ids, []string{domain.NudgePlanned, domain.NudgeMet}).
		Pluck("employee_id", &planned).Error; err != nil {
		return nil, err
	}
	for _, id := range planned {
		out.Planned[id] = true
	}
	return out, nil
}

// TeamMedianFirstResponse — медиана времени первого ответа сотрудника (не заметки) в обращениях
// с since, минуты; nil — ответов не было. По команде целиком, не по человеку.
func (r *Repos) TeamMedianFirstResponse(ctx context.Context, since time.Time) (*float64, int, error) {
	if !r.Enabled() {
		return nil, 0, ErrNoDatabase
	}
	var row struct {
		Median *float64
		N      int
	}
	err := r.db.WithContext(ctx).Raw(`
SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (m.first_at - t.created_at)) / 60) AS median,
	count(*) AS n
FROM tickets t
JOIN (SELECT ticket_id, min(created_at) AS first_at FROM ticket_messages
      WHERE "from" = ? AND NOT internal GROUP BY ticket_id) m ON m.ticket_id = t.id
WHERE NOT t.is_test AND t.created_at >= ?`, domain.TicketMsgStaff, since).Scan(&row).Error
	return row.Median, row.N, err
}

// NewcomerFileInfo — личный файл новичка без содержимого (список в карточке).
type NewcomerFileInfo struct {
	Name      string
	Filename  string
	Mime      string
	Size      int64
	UpdatedAt time.Time
}

// Files — личные файлы новичка без данных, по имени.
func (r *EmployeeRepo) Files(ctx context.Context, employeeID uuid.UUID) ([]NewcomerFileInfo, error) {
	var out []NewcomerFileInfo
	err := r.db.WithContext(ctx).Model(&domain.NewcomerFile{}).Select("name, filename, mime, size, updated_at").
		Where("employee_id = ?", employeeID).Order("name").Scan(&out).Error
	return out, err
}
