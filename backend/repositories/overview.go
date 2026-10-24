package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OverviewRepo — чтение для раздела «Обзор» (api_contract.md, v3). Только SELECT: агрегаты
// считает код в services/overview.go, база отдаёт строки.
type OverviewRepo struct {
	db *gorm.DB
}

// Overview — репозиторий обзора. Поля в Repos нет нарочно: обзор читает чужие таблицы и
// ничего не пишет, а a_init.go правят параллельно.
func (r *Repos) Overview() *OverviewRepo {
	if !r.Enabled() {
		return nil
	}
	return &OverviewRepo{db: r.db}
}

// OverviewEvent — событие таймлайна без снимка опроса и текста: обзору нужны статус и время.
type OverviewEvent struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	DayOffset  int
	Title      string
	Kind       string
	FireAt     time.Time
	Status     string
	SentAt     *time.Time
	IsTest     bool
	// Attempts — неудачные попытки отправки; Catchup — в догоняющей очереди (зоны риска v2).
	Attempts int
	Catchup  bool
}

// OverviewAnswer — ответ опроса без текста новичка: текст в агрегаты не попадает никогда.
type OverviewAnswer struct {
	EmployeeID uuid.UUID
	EventID    uuid.UUID
	QuestionNo int
	Scale      string
	Value      *int
	Skipped    bool
	Flags      domain.StringList
	// Sentiment — тональность текста ответа (pos | neu | neg); nil — текста нет или не размечен.
	Sentiment *string
	IsTest    bool
	CreatedAt time.Time
}

// OverviewQuestion — размеченный по тональности свободный вопрос: без текста, только тон и время.
type OverviewQuestion struct {
	EmployeeID uuid.UUID
	Sentiment  *string
	// Flags — коды regex-триггеров вопроса (ранний сигнал зон v2: тон + флаг).
	Flags     domain.StringList
	IsTest    bool
	CreatedAt time.Time
}

// OverviewTicket — обращение без сводки, цитаты и переписки.
type OverviewTicket struct {
	ID           uuid.UUID
	EmployeeID   uuid.UUID
	Category     string
	Priority     string
	Status       string
	AssigneeName *string
	SLADueAt     *time.Time
	IsTest       bool
	CreatedAt    time.Time
}

// OverviewRows — всё, из чего считается обзор по площадке.
type OverviewRows struct {
	Employees []domain.Employee
	Events    []OverviewEvent
	Answers   []OverviewAnswer
	Tickets   []OverviewTicket
	// Questions — свободные вопросы с тональностью (не старше since): агрегат тона.
	Questions []OverviewQuestion
}

// Load — новички площадки (пусто — все) и их события, ответы и обращения. Ответы и
// обращения — не старше since: тренд и heat map дальше не смотрят.
func (r *OverviewRepo) Load(ctx context.Context, site string, since time.Time) (*OverviewRows, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	db := r.db.WithContext(ctx)
	var out OverviewRows
	q := db.Model(&domain.Employee{})
	if site != "" {
		q = q.Where("site = ?", site)
	}
	if err := q.Order("start_date, full_name").Find(&out.Employees).Error; err != nil {
		return nil, err
	}
	if len(out.Employees) == 0 {
		return &out, nil
	}
	ids := make([]uuid.UUID, len(out.Employees))
	for i, e := range out.Employees {
		ids[i] = e.ID
	}
	if err := db.Model(&domain.ScheduledEvent{}).
		Select("id, employee_id, day_offset, title, kind, fire_at, status, sent_at, is_test, attempts, catchup").
		Where("employee_id IN ?", ids).Order("fire_at, position").
		Scan(&out.Events).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&domain.SurveyAnswer{}).
		Select("employee_id, event_id, question_no, scale, value, skipped, flags, sentiment, is_test, created_at").
		Where("employee_id IN ? AND created_at >= ?", ids, since).Order("created_at").
		Scan(&out.Answers).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&domain.BotQuestion{}).
		Select("employee_id, sentiment, flags, is_test, created_at").
		Where("employee_id IN ? AND created_at >= ? AND sentiment IS NOT NULL", ids, since).Order("created_at").
		Scan(&out.Questions).Error; err != nil {
		return nil, err
	}
	// Открытые — все, закрытые — только за окно (для heat map).
	if err := db.Model(&domain.Ticket{}).
		Select("id, employee_id, category, priority, status, assignee_name, sla_due_at, is_test, created_at").
		Where("employee_id IN ? AND (status <> ? OR created_at >= ?)", ids, domain.TicketResolved, since).
		Order("created_at").Scan(&out.Tickets).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

// ToneMark — тональность одного текстового ответа новичка без текста: для карточки.
type ToneMark struct {
	Kind      string // survey | question
	Sentiment string
	CreatedAt time.Time
}

// RecentTone — последние n размеченных текстов новичка (ответы опросов и свободные вопросы),
// свежие первыми. Текст не читается.
func (r *OverviewRepo) RecentTone(ctx context.Context, employeeID uuid.UUID, n int) ([]ToneMark, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var out []ToneMark
	err := r.db.WithContext(ctx).Raw(`
SELECT kind, sentiment, created_at FROM (
  SELECT 'survey' AS kind, sentiment, created_at FROM survey_answers WHERE employee_id = ? AND sentiment IS NOT NULL
  UNION ALL
  SELECT 'question' AS kind, sentiment, created_at FROM bot_questions WHERE employee_id = ? AND sentiment IS NOT NULL
) t ORDER BY created_at DESC LIMIT ?`, employeeID, employeeID, n).Scan(&out).Error
	return out, err
}
