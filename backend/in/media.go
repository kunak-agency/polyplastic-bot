package in

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"
)

// Медиатека (api_contract.md, «v4»).

// maxMediaName — длина имени записи медиатеки.
const maxMediaName = 120

// maxEnsureNames — потолок имён в одном POST /api/media/ensure (в ТЗ — 19).
const maxEnsureNames = 500

// MsgMediaFormat — отказ по формату: его же показывает кабинет.
const MsgMediaFormat = "Нужна картинка PNG, JPG, GIF, WEBP или документ PDF, DOCX, XLSX, PPTX"

// mediaFormat — формат по расширению: MIME, вид записи и проверка начала файла.
type mediaFormat struct {
	mime  string
	kind  string
	magic func([]byte) bool
}

func prefix(p string) func([]byte) bool {
	return func(b []byte) bool { return bytes.HasPrefix(b, []byte(p)) }
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
}

var mediaFormats = map[string]mediaFormat{
	".png":  {"image/png", domain.MediaKindImage, prefix("\x89PNG\r\n\x1a\n")},
	".jpg":  {"image/jpeg", domain.MediaKindImage, prefix("\xff\xd8\xff")},
	".jpeg": {"image/jpeg", domain.MediaKindImage, prefix("\xff\xd8\xff")},
	".gif":  {"image/gif", domain.MediaKindImage, func(b []byte) bool { return prefix("GIF87a")(b) || prefix("GIF89a")(b) }},
	".webp": {"image/webp", domain.MediaKindImage, isWebP},
	".pdf":  {"application/pdf", domain.MediaKindFile, prefix("%PDF-")},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", domain.MediaKindFile, prefix("PK\x03\x04")},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", domain.MediaKindFile, prefix("PK\x03\x04")},
	".pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", domain.MediaKindFile, prefix("PK\x03\x04")},
}

// MediaUpload — поля формы POST /api/media и PUT /api/media/{id} (multipart). Байты — отдельно.
type MediaUpload struct {
	Name      string // только POST; пусто — имя файла без расширения
	Site      string // GrPP|Luzhniki|GTP; пусто или common — общий файл
	UpdatedBy string
	Filename  string
	Size      int64
	MaxBytes  int64
	NeedName  bool // POST: имя обязательно (или из файла)
}

// MediaUploadValid — проверенная форма.
type MediaUploadValid struct {
	Name      string
	Site      string
	Filename  string
	Mime      string
	Kind      string
	UpdatedBy string
}

// Validate — поля формы без байтов: формат по расширению, размер, имя, площадка.
func (u MediaUpload) Validate() (MediaUploadValid, error) {
	var f fields
	var out MediaUploadValid
	out.Filename = filepath.Base(strings.ReplaceAll(strings.TrimSpace(u.Filename), `\`, "/"))
	ext := strings.ToLower(filepath.Ext(out.Filename))
	fm, ok := mediaFormats[ext]
	switch {
	case u.Filename == "":
		f.add("file", errs.CodeRequired, "Обязательное поле")
	case !ok:
		f.add("file", errs.CodeInvalidFormat, MsgMediaFormat)
	case u.Size <= 0:
		f.add("file", errs.CodeInvalidFormat, "Файл пустой")
	case u.Size > u.MaxBytes:
		f.add("file", errs.CodeOutOfRange, fmt.Sprintf("Файл больше %d МБ", u.MaxBytes>>20))
	}
	out.Mime, out.Kind = fm.mime, fm.kind
	if u.NeedName {
		name := strings.TrimSpace(u.Name)
		if name == "" {
			name = strings.TrimSpace(strings.TrimSuffix(out.Filename, filepath.Ext(out.Filename)))
		}
		out.Name = f.mediaName("name", name)
	}
	out.Site = f.mediaSite("site", u.Site)
	out.UpdatedBy = f.text("updated_by", u.UpdatedBy, false)
	return out, f.err()
}

// CheckMediaBytes — начало файла совпадает с расширением (сигнатура), а не только имя.
func CheckMediaBytes(filename string, data []byte) error {
	fm, ok := mediaFormats[strings.ToLower(filepath.Ext(filename))]
	if ok && fm.magic(data) {
		return nil
	}
	msg := "Содержимое файла не совпадает с расширением: файл переименован или повреждён"
	return errs.NewValidationError(msg).
		WithDetails(errs.FieldError{Field: "file", Code: errs.CodeInvalidFormat, Message: msg})
}

// ParseMediaSite — площадка файла из пути или запроса: GrPP|Luzhniki|GTP, «common» или пусто —
// общий ("").
func ParseMediaSite(v string) (string, error) {
	var f fields
	s := f.mediaSite("site", v)
	return s, f.err()
}

func (f *fields) mediaSite(name, v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "common") || v == "null" {
		return ""
	}
	return f.site(name, v)
}

func (f *fields) mediaName(field, v string) string {
	v = strings.Join(strings.Fields(v), " ")
	switch {
	case v == "":
		f.add(field, errs.CodeRequired, "Обязательное поле")
	case utf8.RuneCountInString(v) > maxMediaName:
		f.add(field, errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxMediaName))
	case strings.IndexFunc(v, unicode.IsControl) >= 0:
		f.add(field, errs.CodeInvalidFormat, "Без служебных символов")
	}
	return v
}

// MediaRename — тело PATCH /api/media/{id}.
type MediaRename struct {
	Name      string `json:"name"`
	UpdatedBy string `json:"updated_by"`
}

// Validate — новое имя.
func (r MediaRename) Validate() (name, by string, err error) {
	var f fields
	name = f.mediaName("name", r.Name)
	by = f.text("updated_by", r.UpdatedBy, false)
	return name, by, f.err()
}

// MediaEnsure — тело POST /api/media/ensure.
type MediaEnsure struct {
	Names     []string `json:"names"`
	Kind      string   `json:"kind"`
	UpdatedBy string   `json:"updated_by"`
}

// MediaEnsureValid — проверенные имена.
type MediaEnsureValid struct {
	Names     []string
	Kind      string
	UpdatedBy string
}

// Validate — имена (пустые пропускаются), kind — image|file|пусто (по имени).
func (e MediaEnsure) Validate() (MediaEnsureValid, error) {
	var f fields
	out := MediaEnsureValid{Names: []string{}}
	if len(e.Names) > maxEnsureNames {
		f.add("names", errs.CodeOutOfRange, fmt.Sprintf("Не больше %d имён за раз", maxEnsureNames))
	}
	for i, n := range e.Names {
		if strings.TrimSpace(n) == "" {
			continue
		}
		out.Names = append(out.Names, f.mediaName(fmt.Sprintf("names[%d]", i), n))
	}
	switch k := strings.TrimSpace(e.Kind); k {
	case "", domain.MediaKindImage, domain.MediaKindFile:
		out.Kind = k
	default:
		f.add("kind", errs.CodeOutOfRange, "Вид: image или file")
	}
	out.UpdatedBy = f.text("updated_by", e.UpdatedBy, false)
	return out, f.err()
}

// MediaListQuery — GET /api/media?status=.
type MediaListQuery struct {
	Status string `query:"status"`
}

// Validate — status: missing|ready|пусто.
func (q MediaListQuery) Validate() (string, error) {
	var f fields
	s := strings.TrimSpace(q.Status)
	switch s {
	case "", domain.MediaMissing, domain.MediaReady:
	default:
		f.add("status", errs.CodeOutOfRange, "Статус: missing или ready")
	}
	return s, f.err()
}
