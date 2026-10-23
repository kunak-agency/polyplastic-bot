package repositories

import (
	"context"
	"time"

	"main/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BotStateRepo — служебное состояние бота (marker long polling).
type BotStateRepo struct {
	db *gorm.DB
}

// Marker — сохранённый marker; нет строки — 0 (читать с начала очереди сервера).
func (r *BotStateRepo) Marker(ctx context.Context, key string) (int64, error) {
	if r == nil {
		return 0, ErrNoDatabase
	}
	var m domain.MarkerState
	got, err := ignoreNotFound(&m, r.db.WithContext(ctx).First(&m, "key = ?", key).Error)
	if err != nil || got == nil {
		return 0, err
	}
	return got.Marker, nil
}

func (r *BotStateRepo) SaveMarker(ctx context.Context, key string, marker int64) error {
	if r == nil {
		return ErrNoDatabase
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"marker", "updated_at"}),
	}).Create(&domain.MarkerState{Key: key, Marker: marker, UpdatedAt: time.Now()}).Error
}
