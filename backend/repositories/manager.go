package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ManagerRepo — руководитель подразделения: ссылки-приглашения, привязка MAX user_id к
// новичкам с тем же manager_name, push и задачи «Встреча 1:1» (services/manager.go).
type ManagerRepo struct {
	db *gorm.DB
}

// Manager — репозиторий руководителя. Поля в Repos нет нарочно (как у Overview): a_init.go
// правят параллельно.
func (r *Repos) Manager() *ManagerRepo {
	if !r.Enabled() {
		return nil
	}
	return &ManagerRepo{db: r.db}
}

// ManagerSummary — руководитель в списке кабинета: имя, сколько новичков, подключён ли к боту.
type ManagerSummary struct {
	ManagerName string
	Newcomers   int
	Connected   int // новичков, у которых привязан ManagerUserID
}

func (r *ManagerRepo) CreateInvite(ctx context.Context, inv *domain.ManagerInvite) error {
	if r == nil {
		return ErrNoDatabase
	}
	return r.db.WithContext(ctx).Create(inv).Error
}

// ActiveInvite — последняя неиспользованная и непросроченная ссылка для этого имени.
func (r *ManagerRepo) ActiveInvite(ctx context.Context, name string, now time.Time) (*domain.ManagerInvite, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var inv domain.ManagerInvite
	err := r.db.WithContext(ctx).Where("manager_name = ? AND used_at IS NULL AND expires_at > ?", name, now).
		Order("created_at DESC").First(&inv).Error
	return ignoreNotFound(&inv, err)
}

func (r *ManagerRepo) InviteByToken(ctx context.Context, token string) (*domain.ManagerInvite, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var inv domain.ManagerInvite
	return ignoreNotFound(&inv, r.db.WithContext(ctx).First(&inv, "token = ?", token).Error)
}

// Bind — пометить ссылку использованной и привязать user_id ко всем новичкам руководителя.
// Возвращает, скольких новичков привязали.
func (r *ManagerRepo) Bind(ctx context.Context, inv *domain.ManagerInvite, userID int64, at time.Time) (int, error) {
	if r == nil {
		return 0, ErrNoDatabase
	}
	var n int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&domain.ManagerInvite{}).Where("id = ?", inv.ID).
			Updates(map[string]any{"used_at": at, "used_by": userID}).Error; err != nil {
			return err
		}
		// Руководитель карточки, с которой выдана ссылка: из справочника (manager_id) — его
		// подчинённые по manager_id плюс старые карточки с тем же именем без связи, и MAX
		// запоминается у пользователя (users.max_user_id) для карточек, назначенных позже.
		var rows []struct{ ManagerID *uuid.UUID }
		if err := tx.Model(&domain.Employee{}).Select("manager_id").Where("id = ?", inv.EmployeeID).Limit(1).Scan(&rows).Error; err != nil {
			return err
		}
		q := tx.Model(&domain.Employee{})
		if len(rows) == 1 && rows[0].ManagerID != nil {
			mid := *rows[0].ManagerID
			q = q.Where("manager_id = ? OR (manager_id IS NULL AND manager_name = ? AND manager_name <> '') OR id = ?", mid, inv.ManagerName, inv.EmployeeID)
			if err := tx.Model(&domain.User{}).Where("id = ?", mid).Update("max_user_id", userID).Error; err != nil {
				return err
			}
		} else {
			q = q.Where("(manager_name = ? AND manager_name <> '') OR id = ?", inv.ManagerName, inv.EmployeeID)
		}
		res := q.Update("manager_user_id", userID)
		n = res.RowsAffected
		return res.Error
	})
	return int(n), err
}

// Managers — все руководители из карточек новичков (непустой manager_name).
func (r *ManagerRepo) Managers(ctx context.Context) ([]ManagerSummary, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var out []ManagerSummary
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Select("manager_name, count(*) AS newcomers, count(manager_user_id) AS connected").
		Where("manager_name <> ''").Group("manager_name").Order("manager_name").Scan(&out).Error
	return out, err
}

// CreateNudge — записать push; false — такой повод уже был (уникальный Key).
func (r *ManagerRepo) CreateNudge(ctx context.Context, n *domain.ManagerNudge) (bool, error) {
	if r == nil {
		return false, ErrNoDatabase
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).Create(n)
	return res.RowsAffected == 1, res.Error
}

// NudgeKeys — какие поводы уже отправлены (ключи).
func (r *ManagerRepo) NudgeKeys(ctx context.Context) (map[string]bool, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var keys []string
	if err := r.db.WithContext(ctx).Model(&domain.ManagerNudge{}).Pluck("key", &keys).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out, nil
}

func (r *ManagerRepo) Nudge(ctx context.Context, id uuid.UUID) (*domain.ManagerNudge, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var n domain.ManagerNudge
	return ignoreNotFound(&n, r.db.WithContext(ctx).First(&n, "id = ?", id).Error)
}

// SaveNudge — записать push целиком (статус, message_id).
func (r *ManagerRepo) SaveNudge(ctx context.Context, n *domain.ManagerNudge) error {
	if r == nil {
		return ErrNoDatabase
	}
	return r.db.WithContext(ctx).Save(n).Error
}

// NudgesOf — push по новичкам, новые первыми.
func (r *ManagerRepo) NudgesOf(ctx context.Context, ids []uuid.UUID) ([]domain.ManagerNudge, error) {
	if r == nil {
		return nil, ErrNoDatabase
	}
	var out []domain.ManagerNudge
	if len(ids) == 0 {
		return out, nil
	}
	err := r.db.WithContext(ctx).Where("employee_id IN ?", ids).Order("created_at DESC").Find(&out).Error
	return out, err
}

// TruncateManager — очистить таблицы руководителя (тесты: общий Truncate о них не знает).
func (r *ManagerRepo) TruncateManager(ctx context.Context) error {
	if r == nil {
		return ErrNoDatabase
	}
	return r.db.WithContext(ctx).Exec(`TRUNCATE manager_invites, manager_nudges`).Error
}
