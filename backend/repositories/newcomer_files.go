package repositories

import (
	"context"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Личные файлы новичка (domain.NewcomerFile) и точечные поля ожидания бота.

// UpsertFile — файл с тем же именем заменяется (токен MAX сбрасывается).
func (r *EmployeeRepo) UpsertFile(ctx context.Context, f *domain.NewcomerFile) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "employee_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"filename", "mime", "size", "data", "max_token", "updated_at"}),
	}).Create(f).Error
}

// File — личный файл по имени.
func (r *EmployeeRepo) File(ctx context.Context, employeeID uuid.UUID, name string) (*domain.NewcomerFile, error) {
	var f domain.NewcomerFile
	err := r.db.WithContext(ctx).First(&f, "employee_id = ? AND name = ?", employeeID, name).Error
	return ignoreNotFound(&f, err)
}

// SetFileToken — кэш токена вложения MAX.
func (r *EmployeeRepo) SetFileToken(ctx context.Context, id uuid.UUID, token string) error {
	return r.db.WithContext(ctx).Model(&domain.NewcomerFile{}).Where("id = ?", id).Update("max_token", token).Error
}

// SetAwaitFreeText — бот ждёт текст новичка (шаг kind=input) — точечно, без перезаписи карточки.
func (r *EmployeeRepo) SetAwaitFreeText(ctx context.Context, id uuid.UUID, w *domain.FreeTextWait) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).Update("await_free_text", w).Error
}
