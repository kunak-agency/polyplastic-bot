package repositories

import (
	"context"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScenarioRepo — сценарии, шаги черновика и опубликованные версии.
type ScenarioRepo struct {
	db *gorm.DB
}

func (r *ScenarioRepo) Create(ctx context.Context, s *domain.Scenario) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *ScenarioRepo) Save(ctx context.Context, s *domain.Scenario) error {
	return r.db.WithContext(ctx).Save(s).Error
}

func (r *ScenarioRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Scenario, error) {
	var s domain.Scenario
	return ignoreNotFound(&s, r.db.WithContext(ctx).First(&s, "id = ?", id).Error)
}

func (r *ScenarioRepo) ByName(ctx context.Context, name string) (*domain.Scenario, error) {
	var s domain.Scenario
	return ignoreNotFound(&s, r.db.WithContext(ctx).First(&s, "name = ?", name).Error)
}

func (r *ScenarioRepo) List(ctx context.Context) ([]domain.Scenario, error) {
	var out []domain.Scenario
	return out, r.db.WithContext(ctx).Order("name").Find(&out).Error
}

// Steps — шаги черновика по порядку.
func (r *ScenarioRepo) Steps(ctx context.Context, scenarioID uuid.UUID) ([]domain.ScenarioStep, error) {
	var out []domain.ScenarioStep
	return out, r.db.WithContext(ctx).Where("scenario_id = ?", scenarioID).Order("position").Find(&out).Error
}

// ReplaceSteps — весь список шагов черновика заменить новым. Звать внутри транзакции: между
// удалением и вставкой черновик пуст.
func (r *ScenarioRepo) ReplaceSteps(ctx context.Context, scenarioID uuid.UUID, steps []domain.ScenarioStep) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("scenario_id = ?", scenarioID).Delete(&domain.ScenarioStep{}).Error; err != nil {
		return err
	}
	if len(steps) == 0 {
		return nil
	}
	return db.Create(&steps).Error
}

func (r *ScenarioRepo) CreateVersion(ctx context.Context, v *domain.ScenarioVersion) error {
	return r.db.WithContext(ctx).Create(v).Error
}

// Version — опубликованная версия по номеру.
func (r *ScenarioRepo) Version(ctx context.Context, scenarioID uuid.UUID, number int) (*domain.ScenarioVersion, error) {
	var v domain.ScenarioVersion
	err := r.db.WithContext(ctx).First(&v, "scenario_id = ? AND number = ?", scenarioID, number).Error
	return ignoreNotFound(&v, err)
}

// StepIDsElsewhere — какие из ids уже заняты шагами других сценариев.
func (r *ScenarioRepo) StepIDsElsewhere(ctx context.Context, scenarioID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var found []uuid.UUID
	err := r.db.WithContext(ctx).Model(&domain.ScenarioStep{}).
		Where("id IN ? AND scenario_id <> ?", ids, scenarioID).Pluck("id", &found).Error
	for _, id := range found {
		out[id] = true
	}
	return out, err
}
