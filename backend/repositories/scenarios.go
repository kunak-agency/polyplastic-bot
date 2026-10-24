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

// SaveStep — записать один шаг черновика целиком (переписывание текстов, rewrite-vy).
func (r *ScenarioRepo) SaveStep(ctx context.Context, st *domain.ScenarioStep) error {
	return r.db.WithContext(ctx).Save(st).Error
}

// MarkDraft — сценарий снова черновик (правка шагов без записи всего сценария: параллельный
// вызов мог поменять команды и правила).
func (r *ScenarioRepo) MarkDraft(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.Scenario{}).Where("id = ?", id).
		Updates(map[string]any{"status": domain.ScenarioDraft, "updated_at": gorm.Expr("now()")}).Error
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

// ByNameFold — сценарий с тем же именем без учёта регистра и крайних пробелов.
func (r *ScenarioRepo) ByNameFold(ctx context.Context, name string) (*domain.Scenario, error) {
	var s domain.Scenario
	err := r.db.WithContext(ctx).First(&s, "lower(btrim(name)) = lower(btrim(?))", name).Error
	return ignoreNotFound(&s, err)
}

// CountVersions — сколько версий сценария опубликовано.
func (r *ScenarioRepo) CountVersions(ctx context.Context, scenarioID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.ScenarioVersion{}).Where("scenario_id = ?", scenarioID).Count(&n).Error
	return n, err
}

// CountEmployees — сколько новичков (в любом статусе) запущено по сценарию.
func (r *ScenarioRepo) CountEmployees(ctx context.Context, scenarioID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).Where("scenario_id = ?", scenarioID).Count(&n).Error
	return n, err
}

// Delete — сценарий вместе с шагами черновика. Версии и новичков проверяет сервис.
func (r *ScenarioRepo) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("scenario_id = ?", id).Delete(&domain.ScenarioStep{}).Error; err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(&domain.Scenario{}).Error
}

// LockNames — замок транзакции на имена сценариев: две параллельные «Создать» с одним
// именем не проскочат проверку уникальности обе.
func (r *ScenarioRepo) LockNames(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtext('scenario_names'))").Error
}
