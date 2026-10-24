package repositories

import (
	"context"

	"main/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TextRewriteRepo — кэш переписанных текстов (services/address_form.go).
type TextRewriteRepo struct {
	db *gorm.DB
}

// Get — текст по хэшу; ok=false — в кэше нет.
func (r *TextRewriteRepo) Get(ctx context.Context, hash string) (string, bool, error) {
	var row domain.TextRewrite
	err := r.db.WithContext(ctx).Where("hash = ?", hash).Limit(1).Find(&row).Error
	if err != nil || row.Hash == "" {
		return "", false, err
	}
	return row.TextOut, true, nil
}

// Put — записать; тот же хэш второй раз не перезаписывается.
func (r *TextRewriteRepo) Put(ctx context.Context, hash, mode, text string) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&domain.TextRewrite{Hash: hash, Mode: mode, TextOut: text}).Error
}
