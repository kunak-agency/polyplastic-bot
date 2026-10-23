package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Медиатека (api_contract.md, «v4», вердикт ресерчера §Q5): картинки и файлы, которые шаги и
// ветки сценария прикладывают к сообщениям. Шаг ссылается на запись по id или по имени
// (MediaRef): импорт ТЗ создаёт ссылку «Изображение 14» раньше, чем HR загрузит файл, — тогда
// запись-заглушка живёт со статусом missing.
//
// У записи до четырёх файлов: общий (Site "") и по одному на площадку. Бот берёт файл площадки
// новичка, иначе общий. Байты — в Postgres (MediaFileData, bytea), как у базы знаний: бэкап базы
// уносит и файлы, контейнеру API не нужен том.

// Вид записи медиатеки: image — картинка (в MAX до 12 в одном сообщении), file — документ (в MAX
// одно вложение на сообщение).
const (
	MediaKindImage = "image"
	MediaKindFile  = "file"
)

// Статус записи: missing — ни одного файла (заглушка импорта), ready — есть хотя бы один.
const (
	MediaMissing = "missing"
	MediaReady   = "ready"
)

// Media — запись медиатеки. NameKey — имя без регистра и лишних пробелов, уникально: «Карта
// ГТП» и «карта гтп» — одна запись. Aliases — прежние имена (в том же виде, что NameKey):
// переименование правит ссылки в черновиках, а опубликованные версии неизменяемы и по-прежнему
// ссылаются на старое имя.
type Media struct {
	UUIDModel
	UpdatedAt time.Time
	Name      string
	NameKey   string `gorm:"uniqueIndex"`
	Kind      string
	Aliases   StringList `gorm:"type:jsonb"`
	UpdatedBy string
}

func (Media) TableName() string { return "media" }

// MediaFile — файл записи для площадки (Site "" — общий). MaxToken — токен вложения после
// загрузки в MAX (POST /uploads); пусто — ещё не загружали или файл заменили.
type MediaFile struct {
	UUIDModel
	UpdatedAt  time.Time
	MediaID    uuid.UUID `gorm:"type:uuid;uniqueIndex:ux_media_file_site"`
	Site       string    `gorm:"uniqueIndex:ux_media_file_site"`
	Filename   string
	Mime       string
	Size       int64
	SHA256     string
	MaxToken   string
	MaxTokenAt *time.Time
}

func (MediaFile) TableName() string { return "media_files" }

// MediaFileData — байты файла. ID = MediaFile.ID: список медиатеки байты не тянет.
type MediaFileData struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Data []byte    `gorm:"type:bytea"`
}

func (MediaFileData) TableName() string { return "media_file_data" }

// MediaNameKey — ключ имени для сравнения: без регистра, пробелы схлопнуты.
func MediaNameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// MatchesName — имя совпадает с текущим или прежним (без регистра).
func (m Media) MatchesName(name string) bool {
	k := MediaNameKey(name)
	if k == "" {
		return false
	}
	return k == m.NameKey || m.Aliases.Has(k)
}

// Refers — ссылка указывает на запись: по id, а без id — по имени (текущему или прежнему).
func (m Media) Refers(ref MediaRef) bool {
	if ref.MediaID != nil {
		return *ref.MediaID == m.ID
	}
	return m.MatchesName(ref.Name)
}
