package handlers

import (
	"encoding/json"

	"main/pkg/errs"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// bindBody — разбор JSON-тела запроса в DTO слоя in. Закрывает ТОЛЬКО шаг разбора:
// валидацию оставляем вызывающему (Validate() у каждого DTO возвращает свой тип). Пустое тело
// — пустой DTO: недостающие поля назовёт валидация.
func bindBody(c *fiber.Ctx, dst any) error {
	if len(c.Body()) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.Body(), dst); err != nil {
		return errs.NewValidationError(errs.MsgInvalidJSON)
	}
	return nil
}

// bindQuery — то же для query-параметров списков.
func bindQuery(c *fiber.Ctx, dst any) error {
	if err := c.QueryParser(dst); err != nil {
		return errs.NewValidationError(errs.MsgInvalidQuery)
	}
	return nil
}

// pathID — UUID из параметра пути :id.
func pathID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, errs.NewValidationError(errs.MsgInvalidID).
			WithDetails(errs.FieldError{Field: "id", Code: errs.CodeInvalidFormat, Message: errs.MsgInvalidID})
	}
	return id, nil
}
