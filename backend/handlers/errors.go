package handlers

import (
	"context"
	"errors"
	"net/http"
	"os"

	"main/out"
	"main/pkg/errs"
	"main/pkg/logger"

	"github.com/gofiber/fiber/v2"
)

// isDev — режим разработки. ЕДИНСТВЕННОЕ место, где HTTP-слой читает APP_ENV. Читаем на каждый
// запрос, а не в init пакета: .env грузится в main() уже после инициализации пакетов.
func isDev() bool { return os.Getenv("APP_ENV") == "development" }

// logRefusal — ЛЮБОЙ отказ клиенту попадает в лог с тем же traceId, что отдан клиенту, иначе
// номер бесполезен. Уровень по классу: сбой сервера — ошибка, «не найдено» — рядовое событие,
// остальное — предупреждение. Тела запросов в *errs.Error не кладутся и сюда не попадают.
func logRefusal(c *fiber.Ctx, status int, err error) {
	write := logger.Warn
	switch {
	case status >= 500:
		write = logger.Error
	case status == http.StatusNotFound:
		write = logger.Info
	}
	write("[%s] %s %s: %v", TraceID(c), c.Method(), c.Path(), err)
}

func writeError(c *fiber.Ctx, status int, info out.ErrorInfo) error {
	info.TraceID = TraceID(c)
	return c.Status(status).JSON(out.ErrorBody{Error: info})
}

// visibleMessage — 5xx, текст которых клиенту полезен и безопасен (наши фиксированные errs.Msg*).
func visibleMessage(status int) bool {
	switch status {
	case http.StatusNotImplemented, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// ErrorHandler — центральный обработчик (fiber.Config.ErrorHandler): ЕДИНСТВЕННОЕ место, где
// ошибка становится HTTP-статусом и конвертом out.ErrorBody.
// Порядок веток: nil → Canceled → *errs.Error → *fiber.Error → фолбэк 500.
func ErrorHandler(c *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}

	// Клиент отменил запрос — тело не пишем. Это не сбой сервера.
	if errors.Is(err, context.Canceled) {
		logger.Info("[%s] клиент отменил запрос %s %s", TraceID(c), c.Method(), c.Path())
		return nil
	}

	// Сбой внешней системы в цепочке (rag, MAX) побеждает «внутреннюю» обёртку хендлера: 500 —
	// только настоящая неожиданность, а отказ rag — 502, даже завёрнутый в NewInternalError.
	var e *errs.Error
	isApp := errors.As(err, &e)
	if !isApp || e.Type == errs.ErrTypeInternal {
		if x := errs.FromExternal(err); x != nil {
			e, isApp = x, true
		}
	}
	if isApp {
		status := e.HTTPStatus()
		logRefusal(c, status, e)
		msg := e.Message
		if status >= 500 && !visibleMessage(status) {
			msg = errs.MessageForStatus(http.StatusInternalServerError) // причина осталась в логе
		}
		info := out.ErrorInfo{Code: e.DefaultCode(), Message: msg, Details: e.Details}
		if isDev() {
			info.Caller = e.Caller
		}
		return writeError(c, status, info)
	}

	// Ошибка роутера/лимитов Fiber: статус из неё, текст — наш по статусу. Сырой текст роутера
	// («Cannot GET /path») наружу не эхоим.
	var fe *fiber.Error
	if errors.As(err, &fe) {
		logRefusal(c, fe.Code, fe)
		return writeError(c, fe.Code, out.ErrorInfo{Code: errs.CodeForStatus(fe.Code), Message: errs.MessageForStatus(fe.Code)})
	}

	logger.Error("[%s] unhandled %s %s: %v", TraceID(c), c.Method(), c.Path(), err)
	return writeError(c, http.StatusInternalServerError, out.ErrorInfo{
		Code:    errs.CodeInternal,
		Message: errs.MessageForStatus(http.StatusInternalServerError),
	})
}
