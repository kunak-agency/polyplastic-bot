package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/oklog/ulid/v2"
)

// requestIDKey — ключ номера запроса в c.Locals. Пишет RequestID, читает TraceID; держим их
// рядом, иначе связь живёт только в совпадении строки.
const requestIDKey = "requestid"

// RequestID — номер запроса для логов и поля traceId в ответе об ошибке.
// Входящий X-Request-Id ИГНОРИРУЕМ и всегда генерируем свой: иначе клиент задаёт номер сам —
// любой длины, повторяющийся, чужой, — и номер перестаёт быть зацепкой в логах. Штатный
// requestid-мидлвар доверяет входящему, поэтому свой.
func RequestID(c *fiber.Ctx) error {
	id := ulid.Make().String()
	c.Locals(requestIDKey, id)
	c.Set("X-Request-Id", id)
	return c.Next()
}

// TraceID — номер запроса, проставленный RequestID (пусто, если мидлвар не отработал).
func TraceID(c *fiber.Ctx) string {
	s, _ := c.Locals(requestIDKey).(string)
	return s
}
