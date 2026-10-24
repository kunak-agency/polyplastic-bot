package repositories

import (
	"context"
	"fmt"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TicketRepo — обращения и их переписка.
type TicketRepo struct {
	db *gorm.DB
}

// ticketSeq — последовательность номеров обращений. Создаётся в MigrateDB.
const ticketSeq = "ticket_number_seq"

// TicketFilter — фильтры GET /api/tickets. Пустое поле — без фильтра.
type TicketFilter struct {
	Statuses []string
	Site     string
	Route    string
	Assignee string
	Newcomer *uuid.UUID
	// RecruiterID — обращения новичков этого рекрутера (роль recruiter, FEATURE_AUTH).
	RecruiterID *uuid.UUID
}

// Create — новое обращение со следующим номером.
func (r *TicketRepo) Create(ctx context.Context, t *domain.Ticket) error {
	if t.Number == 0 {
		if err := r.db.WithContext(ctx).Raw("SELECT nextval('" + ticketSeq + "')").Scan(&t.Number).Error; err != nil {
			return err
		}
	}
	return r.db.WithContext(ctx).Create(t).Error
}

func (r *TicketRepo) Save(ctx context.Context, t *domain.Ticket) error {
	return r.db.WithContext(ctx).Save(t).Error
}

func (r *TicketRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Ticket, error) {
	var t domain.Ticket
	return ignoreNotFound(&t, r.db.WithContext(ctx).First(&t, "id = ?", id).Error)
}

// ByIDForUpdate — обращение под блокировкой: ответ HR и нажатие новичка не перетирают статус.
func (r *TicketRepo) ByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Ticket, error) {
	var t domain.Ticket
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, "id = ?", id).Error
	return ignoreNotFound(&t, err)
}

// List — обращения по фильтру, свежие (по номеру) первыми. Площадка — по карточке новичка.
func (r *TicketRepo) List(ctx context.Context, f TicketFilter) ([]domain.Ticket, error) {
	q := r.db.WithContext(ctx).Model(&domain.Ticket{})
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	}
	if f.Route != "" {
		q = q.Where("route = ?", f.Route)
	}
	if f.Newcomer != nil {
		q = q.Where("employee_id = ?", *f.Newcomer)
	}
	if f.Assignee != "" {
		q = q.Where("assignee_name = ?", f.Assignee)
	}
	if f.Site != "" {
		q = q.Where("employee_id IN (?)", r.db.Model(&domain.Employee{}).Select("id").Where("site = ?", f.Site))
	}
	if f.RecruiterID != nil {
		q = q.Where("employee_id IN (?)", r.db.Model(&domain.Employee{}).Select("id").Where("recruiter_id = ?", *f.RecruiterID))
	}
	var out []domain.Ticket
	return out, q.Order("number DESC").Find(&out).Error
}

// activeStatuses — обращение ещё не закрыто.
var activeStatuses = []string{domain.TicketOpen, domain.TicketAssigned, domain.TicketHumanActive}

// Active — незакрытое обращение сотрудника категории category (пусто — любой), свежее первым.
func (r *TicketRepo) Active(ctx context.Context, employeeID uuid.UUID, category string) (*domain.Ticket, error) {
	q := r.db.WithContext(ctx).Where("employee_id = ? AND status IN ?", employeeID, activeStatuses)
	if category != "" {
		q = q.Where("category = ?", category)
	}
	var t domain.Ticket
	return ignoreNotFound(&t, q.Order("number DESC").First(&t).Error)
}

// Conversation — обращение, куда идёт текст новичка: в human_active (HR ведёт диалог) или с
// «Добавить детали». Свежее первым.
func (r *TicketRepo) Conversation(ctx context.Context, employeeID uuid.UUID) (*domain.Ticket, error) {
	var t domain.Ticket
	err := r.db.WithContext(ctx).
		Where("employee_id = ? AND status IN ? AND (status = ? OR await_details)", employeeID, activeStatuses, domain.TicketHumanActive).
		Order("updated_at DESC").First(&t).Error
	return ignoreNotFound(&t, err)
}

// ByQuestion — обращение, созданное из свободного вопроса (сигнала флага).
func (r *TicketRepo) ByQuestion(ctx context.Context, questionID uuid.UUID) (*domain.Ticket, error) {
	var t domain.Ticket
	return ignoreNotFound(&t, r.db.WithContext(ctx).Where("question_id = ?", questionID).Order("number DESC").First(&t).Error)
}

// ClearAwaiting — снять «Добавить детали» у всех обращений сотрудника.
func (r *TicketRepo) ClearAwaiting(ctx context.Context, employeeID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.Ticket{}).
		Where("employee_id = ? AND await_details", employeeID).Update("await_details", false).Error
}

// CountActive — незакрытые обращения по сотрудникам (open_tickets карточки).
func (r *TicketRepo) CountActive(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		EmployeeID uuid.UUID
		N          int
	}
	err := r.db.WithContext(ctx).Model(&domain.Ticket{}).Select("employee_id, count(*) AS n").
		Where("employee_id IN ? AND status IN ?", ids, activeStatuses).Group("employee_id").Scan(&rows).Error
	for _, x := range rows {
		out[x.EmployeeID] = x.N
	}
	return out, err
}

// AddMessage — сообщение в переписку.
func (r *TicketRepo) AddMessage(ctx context.Context, m *domain.TicketMessage) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// Messages — переписка по порядку записи (id — UUIDv7, растёт со временем вставки).
func (r *TicketRepo) Messages(ctx context.Context, ticketID uuid.UUID) ([]domain.TicketMessage, error) {
	var out []domain.TicketMessage
	return out, r.db.WithContext(ctx).Where("ticket_id = ?", ticketID).Order("id").Find(&out).Error
}

// LastMessage — последнее сообщение переписки (кроме заметок).
func (r *TicketRepo) LastMessage(ctx context.Context, ticketID uuid.UUID) (*domain.TicketMessage, error) {
	var m domain.TicketMessage
	err := r.db.WithContext(ctx).Where("ticket_id = ? AND NOT internal", ticketID).Order("id DESC").First(&m).Error
	return ignoreNotFound(&m, err)
}

// SaveChat — групповой чат, куда добавили (или откуда удалили) бота.
func (r *TicketRepo) SaveChat(ctx context.Context, c *domain.BotChat) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// IsGroupChat — чат известен как групповой (bot_added).
func (r *TicketRepo) IsGroupChat(ctx context.Context, chatID int64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.BotChat{}).Where("chat_id = ?", chatID).Count(&n).Error
	return n > 0, err
}

// migrateTicketSeq — последовательность номеров (до AutoMigrate не нужна: колонка обычная).
func migrateTicketSeq(db *gorm.DB) error {
	return db.Exec(fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s START %d", ticketSeq, domain.TicketNumberStart)).Error
}

// MarkNotified — карточка обращения ушла.
func (r *TicketRepo) MarkNotified(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Ticket{}).Where("id = ?", id).Update("notified_at", at).Error
}

// SetStatusMessage — mid сообщения новичку со статусом обращения.
func (r *TicketRepo) SetStatusMessage(ctx context.Context, id uuid.UUID, mid string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Ticket{}).Where("id = ?", id).
		Updates(map[string]any{"status_message_id": mid, "status_sent_at": at}).Error
}
