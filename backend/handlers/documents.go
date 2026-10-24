package handlers

import (
	"io"
	"net/http"

	"main/in"
	"main/out"
	"main/pkg/errs"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// База знаний (api_contract.md, «v3 — база знаний»).

// MaxRequestBody — потолок тела любого запроса API (fiber BodyLimit, по умолчанию 4 МБ). Поднят
// ради загрузки документов: файл до 25 МБ (предел rag) плюс поля формы. Свой потолок файла
// (KB_MAX_UPLOAD_MB, 20) проверяет хендлер загрузки.
const MaxRequestBody = 26 << 20

// HeaderOperatorToken — заголовок «dev-оператора»: пока ролей нет, вне development база знаний
// открыта только с ним (значение — KB_OPERATOR_TOKEN). Ставит его прокси перед API, не кабинет.
const HeaderOperatorToken = "X-Operator-Token"

// hrOnly — права HR на базу знаний. Ролей нет: пускаем в development или с верным
// X-Operator-Token. rag без авторизации, поэтому кабинет ходит в него только через этот прокси.
func (h *Handler) hrOnly(c *fiber.Ctx) error {
	// FEATURE_AUTH: вошедший пользователь уже прошёл матрицу ролей (policy в auth.go).
	if isDev() || Viewer(c) != nil || h.services.Documents.OperatorAllowed(c.Get(HeaderOperatorToken)) {
		return c.Next()
	}
	return errs.NewForbiddenError("База знаний доступна только HR: нужен вход оператора")
}

// ListDocuments — GET /api/documents.
func (h *Handler) ListDocuments(c *fiber.Ctx) error {
	l, err := h.services.Documents.List(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(out.DocumentListFrom(*l))
}

// UploadDocument — POST /api/documents (multipart: file, doc?, sites?, uploaded_by?) → 201 +
// документ. Проверяет формат и размер до rag, затем проксирует файл в rag POST /documents.
func (h *Handler) UploadDocument(c *fiber.Ctx) error {
	limit := h.services.Documents.MaxUploadBytes()
	fh, err := c.FormFile("file")
	if err != nil {
		return errs.NewValidationError("Нужен файл в поле file").
			WithDetails(errs.FieldError{Field: "file", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	form := in.DocumentUpload{
		Doc: c.FormValue("doc"), Sites: c.FormValue("sites"), UploadedBy: c.FormValue("uploaded_by"),
		Filename: fh.Filename, Size: fh.Size, MaxBytes: limit,
	}
	v, err := form.Validate()
	if err != nil {
		return err
	}
	f, err := fh.Open()
	if err != nil {
		return errs.NewValidationError("Файл не читается").WithCause(err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return errs.NewValidationError("Файл не читается").WithCause(err)
	}
	if int64(len(data)) > limit {
		form.Size = int64(len(data)) // заголовок части соврал о размере
		_, err := form.Validate()
		return err
	}
	if err := in.CheckDocumentBytes(v.Kind, data); err != nil {
		return err
	}
	d, err := h.services.Documents.Upload(c.UserContext(), services.UploadInput{
		Doc: v.Doc, Kind: v.Kind, Filename: v.Filename, Sites: v.Sites, UploadedBy: v.UploadedBy, Data: data,
	})
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.DocumentFrom(*d))
}

// DocumentVersions — GET /api/documents/{id}/versions → история загрузок.
func (h *Handler) DocumentVersions(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	d, list, err := h.services.Documents.Versions(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.DocumentVersionsFrom(*d, list))
}

// AskDocuments — POST /api/documents/ask {"question","doc","site"} → ответ rag с цитатами.
func (h *Handler) AskDocuments(c *fiber.Ctx) error {
	var body in.DocumentAsk
	if err := bindBody(c, &body); err != nil {
		return err
	}
	q, doc, site, err := body.Validate()
	if err != nil {
		return err
	}
	r, err := h.services.Documents.Ask(c.UserContext(), q, doc, site)
	if err != nil {
		return err
	}
	return c.JSON(out.DocumentAskFrom(*r))
}

// Unanswered — GET /api/knowledge/unanswered?site=&days=&include_test= → группы отказов rag.
func (h *Handler) Unanswered(c *fiber.Ctx) error {
	var q in.UnansweredQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	f, err := q.Validate()
	if err != nil {
		return err
	}
	r, err := h.services.Documents.Unanswered(c.UserContext(), services.UnansweredFilter{
		Site: f.Site, Days: f.Days, IncludeTest: f.IncludeTest,
	})
	if err != nil {
		return err
	}
	return c.JSON(out.UnansweredFrom(*r))
}
