package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// Timeouts — сроки HTTP-сервера API. Без них медленный или брошенный клиент держит соединение
// сколько угодно.
type Timeouts struct {
	Read  time.Duration // чтение запроса целиком (заголовки и тело), не работа обработчика
	Write time.Duration // отправка ответа; отсчёт — после обработчика
	Idle  time.Duration // ожидание следующего запроса в keep-alive
}

var DefaultTimeouts = Timeouts{Read: 15 * time.Second, Write: 30 * time.Second, Idle: 60 * time.Second}

// ServerConfig — конфигурация fiber для API: единый конверт ошибок и сроки соединений.
func ServerConfig(t Timeouts) fiber.Config {
	return fiber.Config{
		ErrorHandler: ErrorHandler,
		ReadTimeout:  t.Read,
		WriteTimeout: t.Write,
		IdleTimeout:  t.Idle,
	}
}
