package handlers

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"

	"main/in"
	"main/out"
	"main/pkg/errs"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// Медиатека (api_contract.md, «v4»). Права — как у базы знаний (hrOnly).

// ListMedia — GET /api/media?status=missing|ready.
func (h *Handler) ListMedia(c *fiber.Ctx) error {
	var q in.MediaListQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	status, err := q.Validate()
	if err != nil {
		return err
	}
	list, err := h.services.Media.List(c.UserContext(), status)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaListFrom(list))
}

// GetMedia — GET /api/media/{id}.
func (h *Handler) GetMedia(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	it, err := h.services.Media.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaFrom(*it))
}

// readMediaUpload — форма загрузки (file, name?, site?, updated_by?) → проверенный файл.
func (h *Handler) readMediaUpload(c *fiber.Ctx, needName bool) (services.MediaUpload, error) {
	limit := h.services.Media.MaxUploadBytes()
	fh, err := c.FormFile("file")
	if err != nil {
		return services.MediaUpload{}, errs.NewValidationError("Нужен файл в поле file").
			WithDetails(errs.FieldError{Field: "file", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	form := in.MediaUpload{
		Name: c.FormValue("name"), Site: c.FormValue("site"), UpdatedBy: c.FormValue("updated_by"),
		Filename: fh.Filename, Size: fh.Size, MaxBytes: limit, NeedName: needName,
	}
	v, err := form.Validate()
	if err != nil {
		return services.MediaUpload{}, err
	}
	data, err := readPart(fh, limit)
	if err != nil {
		return services.MediaUpload{}, err
	}
	if int64(len(data)) > limit {
		form.Size = int64(len(data)) // заголовок части соврал о размере
		_, err := form.Validate()
		return services.MediaUpload{}, err
	}
	if err := in.CheckMediaBytes(v.Filename, data); err != nil {
		return services.MediaUpload{}, err
	}
	return services.MediaUpload{
		Name: v.Name, Site: v.Site, Filename: v.Filename, Mime: v.Mime, Kind: v.Kind, By: v.UpdatedBy, Data: data,
	}, nil
}

func readPart(fh *multipart.FileHeader, limit int64) ([]byte, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, errs.NewValidationError("Файл не читается").WithCause(err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errs.NewValidationError("Файл не читается").WithCause(err)
	}
	return data, nil
}

// CreateMedia — POST /api/media (multipart: file, name?, site?, updated_by?) → 201 + запись.
func (h *Handler) CreateMedia(c *fiber.Ctx) error {
	u, err := h.readMediaUpload(c, true)
	if err != nil {
		return err
	}
	it, err := h.services.Media.Create(c.UserContext(), u)
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.MediaFrom(*it))
}

// PutMediaFile — PUT /api/media/{id} (multipart: file, site?, updated_by?) → запись: добавить
// или заменить файл площадки (без site — общий).
func (h *Handler) PutMediaFile(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	u, err := h.readMediaUpload(c, false)
	if err != nil {
		return err
	}
	it, err := h.services.Media.PutFile(c.UserContext(), id, u)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaFrom(*it))
}

// DeleteMediaFile — DELETE /api/media/{id}/files/{site} (site — GrPP|Luzhniki|GTP|common).
func (h *Handler) DeleteMediaFile(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	site, err := in.ParseMediaSite(c.Params("site"))
	if err != nil {
		return err
	}
	it, err := h.services.Media.DeleteFile(c.UserContext(), id, site)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaFrom(*it))
}

// RenameMedia — PATCH /api/media/{id} {"name"} → запись + updated_refs.
func (h *Handler) RenameMedia(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.MediaRename
	if err := bindBody(c, &body); err != nil {
		return err
	}
	name, by, err := body.Validate()
	if err != nil {
		return err
	}
	r, err := h.services.Media.Rename(c.UserContext(), id, name, by)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaRenamed{Media: out.MediaFrom(*r.Item), UpdatedRefs: r.Updated})
}

// DeleteMedia — DELETE /api/media/{id} → 204; используется — 409.
func (h *Handler) DeleteMedia(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := h.services.Media.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(http.StatusNoContent)
}

// EnsureMedia — POST /api/media/ensure {"names":[…],"kind"?} → записи для имён, недостающие —
// заглушки missing (для импорта ТЗ).
func (h *Handler) EnsureMedia(c *fiber.Ctx) error {
	var body in.MediaEnsure
	if err := bindBody(c, &body); err != nil {
		return err
	}
	v, err := body.Validate()
	if err != nil {
		return err
	}
	r, err := h.services.Media.Ensure(c.UserContext(), v.Names, v.Kind, v.UpdatedBy)
	if err != nil {
		return err
	}
	return c.JSON(out.MediaEnsuredFrom(*r))
}

// MediaContent — GET /api/media/{id}/content?site= → байты файла площадки (иначе общего).
func (h *Handler) MediaContent(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	site, err := in.ParseMediaSite(c.Query("site"))
	if err != nil {
		return err
	}
	f, data, err := h.services.Media.Content(c.UserContext(), id, site)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, f.Mime)
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf("inline; filename*=UTF-8''%s", url.PathEscape(f.Filename)))
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set(fiber.HeaderCacheControl, "private, max-age=60")
	return c.Send(data)
}
