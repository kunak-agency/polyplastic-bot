package repositories

import (
	"context"
	"encoding/json"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MediaRepo — медиатека (domain/media.go): записи, файлы по площадкам, байты, токены MAX, плюс
// чтение шагов сценариев для «используется в N» и правки ссылок при переименовании.
type MediaRepo struct {
	db *gorm.DB
}

// List — все записи по имени.
func (r *MediaRepo) List(ctx context.Context) ([]domain.Media, error) {
	var out []domain.Media
	err := r.db.WithContext(ctx).Order("name_key").Find(&out).Error
	return out, err
}

func (r *MediaRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Media, error) {
	var m domain.Media
	return ignoreNotFound(&m, r.db.WithContext(ctx).First(&m, "id = ?", id).Error)
}

// ByIDForUpdate — запись под блокировкой: загрузка файла и переименование не гоняются.
func (r *MediaRepo) ByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Media, error) {
	var m domain.Media
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&m, "id = ?", id).Error
	return ignoreNotFound(&m, err)
}

// ByNameKey — запись с этим текущим именем (ключ — domain.MediaNameKey).
func (r *MediaRepo) ByNameKey(ctx context.Context, key string) (*domain.Media, error) {
	var m domain.Media
	return ignoreNotFound(&m, r.db.WithContext(ctx).First(&m, "name_key = ?", key).Error)
}

// ByAlias — запись, у которой это имя было раньше.
func (r *MediaRepo) ByAlias(ctx context.Context, key string) (*domain.Media, error) {
	b, _ := json.Marshal([]string{key})
	var m domain.Media
	return ignoreNotFound(&m, r.db.WithContext(ctx).Where("aliases @> ?::jsonb", string(b)).Order("updated_at DESC").First(&m).Error)
}

func (r *MediaRepo) Create(ctx context.Context, m *domain.Media) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *MediaRepo) Save(ctx context.Context, m *domain.Media) error {
	return r.db.WithContext(ctx).Save(m).Error
}

// Delete — запись, её файлы и байты.
func (r *MediaRepo) Delete(ctx context.Context, id uuid.UUID) error {
	db := r.db.WithContext(ctx)
	if err := db.Exec(`DELETE FROM media_file_data WHERE id IN (SELECT id FROM media_files WHERE media_id = ?)`, id).Error; err != nil {
		return err
	}
	if err := db.Where("media_id = ?", id).Delete(&domain.MediaFile{}).Error; err != nil {
		return err
	}
	return db.Where("id = ?", id).Delete(&domain.Media{}).Error
}

// AllFiles — файлы всех записей (без байтов).
func (r *MediaRepo) AllFiles(ctx context.Context) ([]domain.MediaFile, error) {
	var out []domain.MediaFile
	err := r.db.WithContext(ctx).Order("site").Find(&out).Error
	return out, err
}

// Files — файлы записи (без байтов), общий первым.
func (r *MediaRepo) Files(ctx context.Context, mediaID uuid.UUID) ([]domain.MediaFile, error) {
	var out []domain.MediaFile
	err := r.db.WithContext(ctx).Where("media_id = ?", mediaID).Order("site").Find(&out).Error
	return out, err
}

// PutFile — файл площадки: новый или замена прежнего (байты тоже, токен MAX сбрасывается).
func (r *MediaRepo) PutFile(ctx context.Context, f *domain.MediaFile, data []byte) error {
	db := r.db.WithContext(ctx)
	var prev domain.MediaFile
	old, err := ignoreNotFound(&prev, db.First(&prev, "media_id = ? AND site = ?", f.MediaID, f.Site).Error)
	if err != nil {
		return err
	}
	if old != nil {
		f.ID, f.CreatedAt = old.ID, old.CreatedAt
		f.MaxToken, f.MaxTokenAt = "", nil
		if err := db.Save(f).Error; err != nil {
			return err
		}
		return db.Save(&domain.MediaFileData{ID: f.ID, Data: data}).Error
	}
	if err := db.Create(f).Error; err != nil {
		return err
	}
	return db.Create(&domain.MediaFileData{ID: f.ID, Data: data}).Error
}

// DeleteFile — файл площадки и его байты; false — такого не было.
func (r *MediaRepo) DeleteFile(ctx context.Context, mediaID uuid.UUID, site string) (bool, error) {
	db := r.db.WithContext(ctx)
	var f domain.MediaFile
	got, err := ignoreNotFound(&f, db.First(&f, "media_id = ? AND site = ?", mediaID, site).Error)
	if err != nil || got == nil {
		return false, err
	}
	if err := db.Where("id = ?", f.ID).Delete(&domain.MediaFileData{}).Error; err != nil {
		return false, err
	}
	return true, db.Where("id = ?", f.ID).Delete(&domain.MediaFile{}).Error
}

// FileData — байты файла; nil — нет.
func (r *MediaRepo) FileData(ctx context.Context, fileID uuid.UUID) ([]byte, error) {
	var d domain.MediaFileData
	got, err := ignoreNotFound(&d, r.db.WithContext(ctx).First(&d, "id = ?", fileID).Error)
	if err != nil || got == nil {
		return nil, err
	}
	return got.Data, nil
}

// SetToken — токен вложения MAX для файла. Только если файл с тех пор не заменили (sha256 тот
// же): иначе токен старых байтов прилип бы к новым.
func (r *MediaRepo) SetToken(ctx context.Context, fileID uuid.UUID, sha, token string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.MediaFile{}).Where("id = ? AND sha256 = ?", fileID, sha).
		UpdateColumns(map[string]any{"max_token": token, "max_token_at": at}).Error
}

// ClearTokens — забыть токены (MAX их не принял): следующая отправка загрузит файл заново.
func (r *MediaRepo) ClearTokens(ctx context.Context, tokens []string) (int64, error) {
	if len(tokens) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&domain.MediaFile{}).Where("max_token IN ?", tokens).
		UpdateColumns(map[string]any{"max_token": "", "max_token_at": nil})
	return res.RowsAffected, res.Error
}

// DraftSteps — шаги черновиков всех сценариев.
func (r *MediaRepo) DraftSteps(ctx context.Context) ([]domain.ScenarioStep, error) {
	var out []domain.ScenarioStep
	err := r.db.WithContext(ctx).Order("scenario_id, position").Find(&out).Error
	return out, err
}

// LatestVersions — последняя опубликованная версия каждого сценария.
func (r *MediaRepo) LatestVersions(ctx context.Context) ([]domain.ScenarioVersion, error) {
	var out []domain.ScenarioVersion
	err := r.db.WithContext(ctx).Table("scenario_versions AS v").Select("v.*").
		Joins("JOIN scenarios s ON s.id = v.scenario_id AND s.version = v.number").Scan(&out).Error
	return out, err
}

// UpdateStepRefs — ссылки шага черновика на медиатеку (свои и в ветках кнопок), без остальных
// полей: редактор мог как раз сохранять текст.
func (r *MediaRepo) UpdateStepRefs(ctx context.Context, stepID uuid.UUID, media domain.MediaRefList, buttons domain.ButtonList) error {
	return r.db.WithContext(ctx).Model(&domain.ScenarioStep{}).Where("id = ?", stepID).
		UpdateColumns(map[string]any{"media": media, "buttons": buttons}).Error
}

// TruncateMedia — очистить медиатеку. Только для тестов.
func (r *MediaRepo) TruncateMedia(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec(`TRUNCATE media, media_files, media_file_data`).Error
}
