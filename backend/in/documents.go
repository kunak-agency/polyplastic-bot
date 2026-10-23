package in

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"main/pkg/errs"
)

// База знаний (api_contract.md, «v3 — база знаний»).

// Форматы загрузки — те же, что принимает rag (services/rag/README.md). Ключ — расширение,
// значение — kind документа.
var docKinds = map[string]string{
	".pdf":  "pdf",
	".pptx": "pptx",
	".xlsx": "xlsx",
	".txt":  "text",
	".md":   "text",
}

// MsgDocFormat — текст отказа по формату: его же показывает кабинет.
const MsgDocFormat = "Нужен файл PDF, PPTX, XLSX, TXT или MD"

// maxDocName — длина имени документа (поле doc у rag — до 200 символов).
const maxDocName = 200

// DocumentUpload — поля формы POST /api/documents (multipart). Файл — отдельно: его байты читает
// хендлер.
type DocumentUpload struct {
	Doc        string // имя документа; пусто — имя файла без расширения
	Sites      string // площадки через запятую; пусто — все
	UploadedBy string // кто загрузил: ролей нет, кабинет присылает имя оператора
	Filename   string
	Size       int64
	MaxBytes   int64
}

// DocumentUploadValid — проверенная загрузка.
type DocumentUploadValid struct {
	Doc        string
	Kind       string
	Filename   string
	Sites      []string
	UploadedBy string
}

// Validate — поля формы без байтов файла: формат по расширению, размер, имя, площадки.
func (u DocumentUpload) Validate() (DocumentUploadValid, error) {
	var f fields
	var out DocumentUploadValid
	out.Filename = filepath.Base(strings.ReplaceAll(strings.TrimSpace(u.Filename), `\`, "/"))
	ext := strings.ToLower(filepath.Ext(out.Filename))
	kind, ok := docKinds[ext]
	switch {
	case u.Filename == "":
		f.add("file", errs.CodeRequired, "Обязательное поле")
	case !ok:
		f.add("file", errs.CodeInvalidFormat, MsgDocFormat)
	case u.Size <= 0:
		f.add("file", errs.CodeInvalidFormat, "Файл пустой")
	case u.Size > u.MaxBytes:
		f.add("file", errs.CodeOutOfRange, fmt.Sprintf("Файл больше %d МБ", u.MaxBytes>>20))
	}
	out.Kind = kind
	doc := strings.TrimSpace(u.Doc)
	if doc == "" {
		doc = strings.TrimSpace(strings.TrimSuffix(out.Filename, filepath.Ext(out.Filename)))
	}
	switch {
	case doc == "":
		f.add("doc", errs.CodeRequired, "Обязательное поле")
	case utf8.RuneCountInString(doc) > maxDocName:
		f.add("doc", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxDocName))
	case strings.IndexFunc(doc, unicode.IsControl) >= 0:
		f.add("doc", errs.CodeInvalidFormat, "Без переводов строк и служебных символов")
	}
	out.Doc = doc
	out.Sites = []string{}
	for _, s := range strings.Split(u.Sites, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		out.Sites = append(out.Sites, f.site("sites", s))
	}
	out.UploadedBy = f.text("uploaded_by", u.UploadedBy, false)
	return out, f.err()
}

// CheckDocumentBytes — начало файла соответствует расширению: PDF — «%PDF-», PPTX и XLSX —
// zip-архив, текст — UTF-8. Ловит переименованные файлы раньше rag и понятным текстом.
func CheckDocumentBytes(kind string, data []byte) error {
	ok := true
	switch kind {
	case "pdf":
		ok = bytes.HasPrefix(data, []byte("%PDF-"))
	case "pptx", "xlsx":
		ok = bytes.HasPrefix(data, []byte("PK\x03\x04"))
	case "text":
		ok = utf8.Valid(data)
	}
	if ok {
		return nil
	}
	msg := "Содержимое файла не совпадает с расширением: файл переименован или повреждён"
	return errs.NewValidationError(msg).
		WithDetails(errs.FieldError{Field: "file", Code: errs.CodeInvalidFormat, Message: msg})
}

// maxAskQuestion — потолок проверочного вопроса: новичок в MAX пишет не длиннее.
const maxAskQuestion = 1000

// DocumentAsk — тело POST /api/documents/ask.
type DocumentAsk struct {
	Question string  `json:"question"`
	Doc      *string `json:"doc"`
	Site     *string `json:"site"`
}

// Validate — вопрос обязателен; site — площадка или null.
func (a DocumentAsk) Validate() (question string, doc, site *string, err error) {
	var f fields
	question = strings.TrimSpace(a.Question)
	switch {
	case utf8.RuneCountInString(question) < 3:
		f.add("question", errs.CodeRequired, "Задайте вопрос: хотя бы несколько слов")
	case utf8.RuneCountInString(question) > maxAskQuestion:
		f.add("question", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxAskQuestion))
	}
	if a.Doc != nil {
		if d := strings.TrimSpace(*a.Doc); d != "" {
			doc = &d
		}
	}
	if a.Site != nil && strings.TrimSpace(*a.Site) != "" {
		s := f.site("site", *a.Site)
		site = &s
	}
	return question, doc, site, f.err()
}

// UnansweredQuery — GET /api/knowledge/unanswered?site=&days=&include_test=.
type UnansweredQuery struct {
	Site        string `query:"site"`
	Days        string `query:"days"`
	IncludeTest string `query:"include_test"`
}

// UnansweredQueryValid — проверенный фильтр.
type UnansweredQueryValid struct {
	Site        string
	Days        int
	IncludeTest bool
}

// Validate — days от 1 до 365 (по умолчанию 30), include_test — true|1.
func (q UnansweredQuery) Validate() (UnansweredQueryValid, error) {
	var f fields
	out := UnansweredQueryValid{Days: 30}
	if s := strings.TrimSpace(q.Site); s != "" {
		out.Site = f.site("site", s)
	}
	if d := strings.TrimSpace(q.Days); d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 1 || n > 365 {
			f.add("days", errs.CodeOutOfRange, "От 1 до 365 дней")
		} else {
			out.Days = n
		}
	}
	switch strings.ToLower(strings.TrimSpace(q.IncludeTest)) {
	case "true", "1":
		out.IncludeTest = true
	}
	return out, f.err()
}
