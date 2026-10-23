package handlers

import (
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"main/in"
	"main/out"
	"main/pkg/errs"

	"github.com/gofiber/fiber/v2"
)

// MaxImportSize — потолок Excel-файла импорта. Таймлайн ТЗ — десятки строк; 5 МБ с запасом,
// и fiber всё равно не примет тело больше BodyLimit (4 МБ по умолчанию).
const MaxImportSize = 5 << 20

// Тексты отказа импорта. В details — тот же текст: кабинет дописывает к сообщению первое
// пополевое, а одинаковое не повторяет.
const (
	msgImportXLS      = "Формат .xls (Excel 97–2003) пока не читается — сохраните файл как .xlsx"
	msgImportNotExcel = "Нужен файл Excel: .xlsx"
)

func fileFormatError(msg string) error {
	return errs.NewValidationError(msg).
		WithDetails(errs.FieldError{Field: "file", Code: errs.CodeInvalidFormat, Message: msg})
}

// ListScenarios — GET /api/scenarios.
func (h *Handler) ListScenarios(c *fiber.Ctx) error {
	list, err := h.services.Scenarios.List(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(out.NewList(out.ScenariosFrom(list)))
}

// GetScenario — GET /api/scenarios/{id} → сценарий + steps.
func (h *Handler) GetScenario(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	d, err := h.services.Scenarios.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioDetailFrom(*d))
}

// PutSteps — PUT /api/scenarios/{id}/steps: весь список шагов → черновик.
func (h *Handler) PutSteps(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	raw, err := in.ParseSteps(c.Body())
	if err != nil {
		return err
	}
	steps, err := in.ValidateSteps(raw)
	if err != nil {
		return err
	}
	d, err := h.services.Scenarios.PutSteps(c.UserContext(), id, steps)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioDetailFrom(*d))
}

// PublishScenario — POST /api/scenarios/{id}/publish {"comment","apply"} → новая версия.
func (h *Handler) PublishScenario(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.Publish
	if err := bindBody(c, &body); err != nil {
		return err
	}
	comment, apply, err := body.Validate()
	if err != nil {
		return err
	}
	d, err := h.services.Scenarios.Publish(c.UserContext(), id, comment, apply)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioDetailFrom(*d))
}

// excelUpload — файл Excel из поля file: размер и расширение; закрыть — вызывающему.
func excelUpload(c *fiber.Ctx) (*multipart.FileHeader, multipart.File, error) {
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, nil, errs.NewValidationError("Нужен файл Excel в поле file").
			WithDetails(errs.FieldError{Field: "file", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	if fh.Size > MaxImportSize {
		return nil, nil, errs.NewValidationError("Файл больше 5 МБ")
	}
	switch strings.ToLower(filepath.Ext(fh.Filename)) {
	case ".xlsx":
	case ".xls":
		// Контракт принимает .xls, но excelize читает только OOXML: старый двоичный формат
		// Excel 97–2003 честно отказываем с подсказкой.
		return nil, nil, fileFormatError(msgImportXLS)
	default:
		return nil, nil, fileFormatError(msgImportNotExcel)
	}
	f, err := fh.Open()
	if err != nil {
		return nil, nil, errs.NewValidationError("Файл не читается").WithCause(err)
	}
	return fh, f, nil
}

// ImportScenario — POST /api/scenarios/import (multipart: file, необязательно name) →
// 201 + сценарий-черновик с import_report.
func (h *Handler) ImportScenario(c *fiber.Ctx) error {
	fh, f, err := excelUpload(c)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename))
	}
	d, err := h.services.Scenarios.ImportExcel(c.UserContext(), name, f)
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.ScenarioImportFrom(*d))
}

// ImportSteps — POST /api/scenarios/{id}/import (multipart: file) → шаги из Excel заменяют
// черновик сценария, как PUT …/steps; ответ — сценарий со steps.
func (h *Handler) ImportSteps(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	_, f, err := excelUpload(c)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	d, err := h.services.Scenarios.ImportExcelInto(c.UserContext(), id, f)
	if err != nil {
		return err
	}
	return c.JSON(out.ScenarioImportFrom(*d))
}

// CreateScenario — POST /api/scenarios {"name","sites","from_scenario_id"} → 201 + черновик.
func (h *Handler) CreateScenario(c *fiber.Ctx) error {
	var body in.ScenarioCreate
	if err := bindBody(c, &body); err != nil {
		return err
	}
	name, sites, from, err := body.Validate()
	if err != nil {
		return err
	}
	d, err := h.services.Scenarios.CreateDraft(c.UserContext(), name, sites, from)
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.ScenarioDetailFrom(*d))
}

// DeleteScenario — DELETE /api/scenarios/{id} → 204; только черновик без публикаций и новичков.
func (h *Handler) DeleteScenario(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if err := h.services.Scenarios.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(http.StatusNoContent)
}
