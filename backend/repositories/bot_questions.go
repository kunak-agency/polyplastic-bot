package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QuestionRepo — свободные вопросы новичков и ответы бота (domain.BotQuestion).
type QuestionRepo struct {
	db *gorm.DB
}

func (r *QuestionRepo) Create(ctx context.Context, q *domain.BotQuestion) error {
	return r.db.WithContext(ctx).Create(q).Error
}

func (r *QuestionRepo) Save(ctx context.Context, q *domain.BotQuestion) error {
	return r.db.WithContext(ctx).Save(q).Error
}

// SetSentiment — тональность вопроса (разметка /flags приходит позже записи). false — записи нет.
func (r *QuestionRepo) SetSentiment(ctx context.Context, id uuid.UUID, s string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&domain.BotQuestion{}).Where("id = ?", id).Update("sentiment", s)
	return res.RowsAffected > 0, res.Error
}

// SetMarkup — разметка /flags v2 в запись вопроса: тон, уверенность в тоне, цитата, строка для HR.
// nil-поля не трогаются. false — записи нет.
func (r *QuestionRepo) SetMarkup(ctx context.Context, id uuid.UUID, sentiment string, toneConf *float64, quote, summary *string) (bool, error) {
	upd := map[string]any{"sentiment": sentiment}
	if toneConf != nil {
		upd["tone_confidence"] = *toneConf
	}
	if quote != nil {
		upd["quote"] = *quote
	}
	if summary != nil {
		upd["summary"] = *summary
	}
	res := r.db.WithContext(ctx).Model(&domain.BotQuestion{}).Where("id = ?", id).Updates(upd)
	return res.RowsAffected > 0, res.Error
}

func (r *QuestionRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.BotQuestion, error) {
	var q domain.BotQuestion
	return ignoreNotFound(&q, r.db.WithContext(ctx).First(&q, "id = ?", id).Error)
}

// ByIDForUpdate — запись под блокировкой: двойное нажатие не считает «Не то» дважды.
func (r *QuestionRepo) ByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.BotQuestion, error) {
	var q domain.BotQuestion
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&q, "id = ?", id).Error
	return ignoreNotFound(&q, err)
}

// AwaitingDetails — последний сигнал сотрудника, к которому бот ждёт детали, не старше since.
func (r *QuestionRepo) AwaitingDetails(ctx context.Context, employeeID uuid.UUID, since time.Time) (*domain.BotQuestion, error) {
	var q domain.BotQuestion
	err := r.db.WithContext(ctx).Where("employee_id = ? AND await_details AND await_since >= ?", employeeID, since).
		Order("await_since DESC").First(&q).Error
	return ignoreNotFound(&q, err)
}

// ClearAwaiting — снять ожидание деталей у всех записей сотрудника.
func (r *QuestionRepo) ClearAwaiting(ctx context.Context, employeeID uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.BotQuestion{}).
		Where("employee_id = ? AND await_details", employeeID).Update("await_details", false).Error
}

// RecentKinds — виды последних n вопросов сотрудника не старше since, свежие первыми: для
// лестницы ошибок (сколько отказов подряд).
func (r *QuestionRepo) RecentKinds(ctx context.Context, employeeID uuid.UUID, since time.Time, n int) ([]string, error) {
	var kinds []string
	err := r.db.WithContext(ctx).Model(&domain.BotQuestion{}).
		Where("employee_id = ? AND created_at >= ?", employeeID, since).
		Order("created_at DESC").Limit(n).Pluck("kind", &kinds).Error
	return kinds, err
}

// Recent — последние n вопросов сотрудника не старше since, по порядку (старые первыми): «последние
// сообщения» в контексте обращения.
func (r *QuestionRepo) Recent(ctx context.Context, employeeID uuid.UUID, since time.Time, n int) ([]domain.BotQuestion, error) {
	var out []domain.BotQuestion
	err := r.db.WithContext(ctx).Where("employee_id = ? AND created_at >= ?", employeeID, since).
		Order("created_at DESC").Limit(n).Find(&out).Error
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}
