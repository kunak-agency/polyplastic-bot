package repositories

import (
	"context"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserRepo — пользователи кабинета (domain.User). Заготовка волны 0: CRUD без бизнес-логики,
// пароли и роли проверяет сервис auth.
type UserRepo struct {
	db *gorm.DB
}

// Create — новый пользователь. GORM подменяет нулевое значение поля с default его дефолтом
// (Active=false → true, даже с Select("*")), поэтому выключенный создаётся вторым запросом.
func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	active := u.Active
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil || active {
			return err
		}
		u.Active = false
		return tx.Model(u).Update("active", false).Error
	})
}

// ByID — пользователь по id; nil — нет такого.
func (r *UserRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	var u domain.User
	err := r.db.WithContext(ctx).First(&u, "id = ?", id).Error
	return ignoreNotFound(&u, err)
}

// ByLogin — пользователь по логину (точное совпадение); nil — нет такого.
func (r *UserRepo) ByLogin(ctx context.Context, login string) (*domain.User, error) {
	var u domain.User
	err := r.db.WithContext(ctx).First(&u, "login = ?", login).Error
	return ignoreNotFound(&u, err)
}

// List — все пользователи по имени.
func (r *UserRepo) List(ctx context.Context) ([]domain.User, error) {
	var out []domain.User
	err := r.db.WithContext(ctx).Order("full_name, login").Find(&out).Error
	return out, err
}

// Save — записать все поля.
func (r *UserRepo) Save(ctx context.Context, u *domain.User) error {
	return r.db.WithContext(ctx).Save(u).Error
}

// SetActive — активировать или деактивировать.
func (r *UserRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).
		Updates(map[string]any{"active": active, "updated_at": time.Now()}).Error
}

// Delete — удалить пользователя (в кабинете — деактивация, Delete для тестов и сида).
func (r *UserRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.User{}, "id = ?", id).Error
}

// SessionRepo — сессии кабинета (domain.Session).
type SessionRepo struct {
	db *gorm.DB
}

// Create — новая сессия; ID задаёт вызывающий (domain.NewSessionID).
func (r *SessionRepo) Create(ctx context.Context, s *domain.Session) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// ByID — живая сессия: не истекла к now. nil — нет или истекла.
func (r *SessionRepo) ByID(ctx context.Context, id string, now time.Time) (*domain.Session, error) {
	var s domain.Session
	err := r.db.WithContext(ctx).First(&s, "id = ? AND expires_at > ?", id, now).Error
	return ignoreNotFound(&s, err)
}

// Extend — продлить сессию до expiresAt.
func (r *SessionRepo) Extend(ctx context.Context, id string, expiresAt time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.Session{}).Where("id = ?", id).Update("expires_at", expiresAt).Error
}

// Delete — выход: удалить сессию.
func (r *SessionRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&domain.Session{}, "id = ?", id).Error
}

// DeleteByUser — все сессии пользователя (деактивация, смена пароля).
func (r *SessionRepo) DeleteByUser(ctx context.Context, userID uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&domain.Session{}, "user_id = ?", userID).Error
}

// DeleteExpired — уборка истёкших к now; сколько удалено.
func (r *SessionRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Delete(&domain.Session{}, "expires_at <= ?", now)
	return res.RowsAffected, res.Error
}
