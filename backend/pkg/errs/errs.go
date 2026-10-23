// Package errs — единственная типизированная ошибка приложения *Error с enum Type.
// Центральный обработчик (handlers.ErrorHandler, посаженный в fiber.Config.ErrorHandler)
// — ЕДИНСТВЕННОЕ место, где Type превращается в HTTP-статус и конверт
// {"error":{"code","message","traceId"}} (agency/system/api_contract.md).
package errs

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"runtime"
)

// Type категоризует доменные ошибки; HTTP-слой маппит Type в статус-код.
type Type string

const (
	ErrTypeValidation      Type = "VALIDATION"      // 422
	ErrTypeUnauthenticated Type = "UNAUTHENTICATED" // 401 — клиент не предъявил валидных учётных данных
	ErrTypePermissions     Type = "PERMISSIONS"     // 403 — предъявил, но прав нет
	ErrTypeNotFound        Type = "NOT_FOUND"       // 404
	ErrTypeConflict        Type = "CONFLICT"        // 409
	ErrTypeInternal        Type = "INTERNAL"        // 500 (default)
	ErrTypeNotImplemented  Type = "NOT_IMPLEMENTED" // 501 — функция объявлена, но ещё не написана

	// Сбои внешних систем (rag, модель, MAX): не наша неожиданность, а их состояние. Клиенту
	// важно различать «повторите позже» (503), «не дождались» (504) и «ответ не по контракту» (502).
	ErrTypeBadGateway      Type = "BAD_GATEWAY"          // 502
	ErrTypeUpstreamDown    Type = "UPSTREAM_UNAVAILABLE" // 503
	ErrTypeUpstreamTimeout Type = "UPSTREAM_TIMEOUT"     // 504
)

// Реестр машиночитаемых кодов. Канонические — по статусу (поле error.code), пополевые —
// для FieldError.Code (детализация валидации).
const (
	CodeBadRequest       = "BAD_REQUEST"
	CodeValidationFailed = "VALIDATION_FAILED"
	CodeUnauthenticated  = "UNAUTHENTICATED"
	CodePermissionDenied = "PERMISSION_DENIED"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeTimeout          = "TIMEOUT"
	CodeBadGateway       = "BAD_GATEWAY"
	CodeUnavailable      = "UPSTREAM_UNAVAILABLE"
	CodeInternal         = "INTERNAL"
	CodeNotImplemented   = "NOT_IMPLEMENTED"

	// Коды роутера и лимитов Fiber: домен их не порождает, но HTTP-слой маппит.
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	CodePayloadTooLarge  = "PAYLOAD_TOO_LARGE"
	CodeHeadersTooLarge  = "HEADERS_TOO_LARGE" // 431: слишком длинный URI/заголовки, ловит fasthttp
	CodeRequestTimeout   = "REQUEST_TIMEOUT"   // 408: клиент не дописал запрос за ReadTimeout сервера
	CodeTooManyRequests  = "TOO_MANY_REQUESTS"

	// Пополевые (FieldError.Code).
	CodeRequired      = "REQUIRED"
	CodeInvalidFormat = "INVALID_FORMAT"
	CodeOutOfRange    = "OUT_OF_RANGE"
)

// Тексты отказов разбора запроса.
const (
	MsgInvalidFields = "Проверьте данные запроса"             // тело разобрано, но поля не прошли валидацию
	MsgInvalidQuery  = "Проверьте параметры запроса"          // не разобрались query-параметры
	MsgInvalidJSON   = "Тело запроса не разбирается как JSON" // тело не разобралось как JSON
	MsgInvalidID     = "Идентификатор записан неверно"        // параметр пути не разобрался как ключ
)

// FieldError — одна ошибка валидации на уровне поля (наполняет Details).
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// Error — внутренняя ошибка приложения. Клиенту НЕ сериализуется: HTTP-слой собирает из неё
// out.ErrorBody. cause уходит только в лог, наружу не протекает.
type Error struct {
	Type    Type         // класс ошибки, отсюда выводится HTTP-статус
	Message string       // безопасный для клиента текст
	Details []FieldError // пополевые ошибки валидации
	Caller  string       // "<func> at <file>:<line>", наружу только в dev
	// Code — свой машинный код вместо канонического по статусу (контракт называет некоторые
	// отказы по имени, например scenario_not_published). Пусто — код по статусу.
	Code  string
	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Type, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithCause — причина уходит только в лог, клиенту не отдаётся.
func (e *Error) WithCause(err error) *Error { e.cause = err; return e }

func (e *Error) WithDetails(d ...FieldError) *Error { e.Details = append(e.Details, d...); return e }

// WithCode — свой машинный код отказа (поле error.code).
func (e *Error) WithCode(code string) *Error { e.Code = code; return e }

// HTTPStatus — общепринятый статус для этой ошибки.
func (e *Error) HTTPStatus() int {
	switch e.Type {
	case ErrTypeValidation:
		return http.StatusUnprocessableEntity
	case ErrTypeUnauthenticated:
		return http.StatusUnauthorized
	case ErrTypePermissions:
		return http.StatusForbidden
	case ErrTypeNotFound:
		return http.StatusNotFound
	case ErrTypeConflict:
		return http.StatusConflict
	case ErrTypeNotImplemented:
		return http.StatusNotImplemented
	case ErrTypeBadGateway:
		return http.StatusBadGateway
	case ErrTypeUpstreamDown:
		return http.StatusServiceUnavailable
	case ErrTypeUpstreamTimeout:
		return http.StatusGatewayTimeout
	}
	return http.StatusInternalServerError // ErrTypeInternal + неизвестный Type
}

// DefaultCode — машиночитаемый код: канон по статусу.
func (e *Error) DefaultCode() string {
	if e.Code != "" {
		return e.Code
	}
	return CodeForStatus(e.HTTPStatus())
}

// statusMeta — ЕДИНЫЙ перечень известных статусов: машинный код и пояснение для отказов, у
// которых своего текста нет (роутер и лимиты Fiber). Тексты по-русски: их видит HR в кабинете.
var statusMeta = map[int]struct{ code, message string }{
	http.StatusBadRequest:                  {CodeBadRequest, "Запрос не разобран: проверьте адрес и параметры."},
	http.StatusUnauthorized:                {CodeUnauthenticated, "Для этого действия нужно войти."},
	http.StatusForbidden:                   {CodePermissionDenied, "Недостаточно прав для этого действия."},
	http.StatusNotFound:                    {CodeNotFound, "Такого адреса или объекта нет."},
	http.StatusMethodNotAllowed:            {CodeMethodNotAllowed, "Этот метод для адреса не поддерживается."},
	http.StatusRequestTimeout:              {CodeRequestTimeout, "Сервер не дождался запроса целиком. Повторите запрос."},
	http.StatusConflict:                    {CodeConflict, "Действие противоречит текущему состоянию. Обновите данные."},
	http.StatusRequestEntityTooLarge:       {CodePayloadTooLarge, "Тело запроса больше допустимого."},
	http.StatusRequestHeaderFieldsTooLarge: {CodeHeadersTooLarge, "Адрес или заголовки запроса длиннее допустимого."},
	http.StatusUnprocessableEntity:         {CodeValidationFailed, "Проверьте введённые данные."},
	http.StatusTooManyRequests:             {CodeTooManyRequests, "Подождите немного и повторите."},
	http.StatusInternalServerError:         {CodeInternal, "Непредвиденная ошибка на сервере. Повторите запрос."},
	http.StatusNotImplemented:              {CodeNotImplemented, "Эта возможность ещё не подключена."},
	http.StatusBadGateway:                  {CodeBadGateway, "Внешняя система ответила не по договорённости."},
	http.StatusServiceUnavailable:          {CodeUnavailable, "Сервис или внешняя система сейчас недоступны. Повторите позже."},
	http.StatusGatewayTimeout:              {CodeTimeout, "Внешняя система не ответила вовремя. Повторите позже."},
}

// CodeForStatus — машинный код по статусу; неизвестный статус — сбой сервера.
func CodeForStatus(status int) string {
	if meta, ok := statusMeta[status]; ok {
		return meta.code
	}
	return CodeInternal
}

// MessageForStatus — пояснение для отказа без собственного текста (роутер, лимиты).
func MessageForStatus(status int) string {
	if meta, ok := statusMeta[status]; ok {
		return meta.message
	}
	if status >= 500 {
		return statusMeta[http.StatusInternalServerError].message
	}
	return "Запрос отклонён."
}

// --- Конструкторы ---
//
// Каждый конструктор зовёт build() НАПРЯМУЮ, без делегирующих обёрток: иначе
// runtime.Caller(2) съедет с места вызова на пакет errs и Caller укажет не туда.

func NewValidationError(msg string) *Error    { return build(ErrTypeValidation, msg) }
func NewUnauthorizedError(msg string) *Error  { return build(ErrTypeUnauthenticated, msg) }
func NewForbiddenError(msg string) *Error     { return build(ErrTypePermissions, msg) }
func NewNotFoundError(resource string) *Error { return build(ErrTypeNotFound, resource) }
func NewConflictError(msg string) *Error      { return build(ErrTypeConflict, msg) }
func NewInternalError(msg string) *Error      { return build(ErrTypeInternal, msg) }
func NewBadGatewayError(msg string) *Error    { return build(ErrTypeBadGateway, msg) }

// NewNotImplementedError — 501: честный отказ заглушки. «Ещё не написано» и «сломалось» клиент
// обязан различать, иначе на заглушку заводят баг.
func NewNotImplementedError(msg string) *Error { return build(ErrTypeNotImplemented, msg) }

// External — сбой внешней системы, который знает, каким статусом на него отвечать. Интерфейс
// здесь, а не импорт клиентов: pkg не зависит от externalApi.
type External interface {
	error
	ExternalStatus() int
}

// Тексты внешних сбоев: свои, фиксированные — тело чужого отказа наружу не идёт.
const (
	MsgUpstreamTimeout = "Внешняя система не ответила вовремя. Повторите запрос."
	MsgUpstreamDown    = "Внешняя система временно недоступна. Повторите запрос позже."
	MsgBadGateway      = "Внешняя система ответила с ошибкой."
	MsgUpstreamRefused = "Внешняя система отказала в операции."
)

// FromExternal — сбой внешней системы из цепочки err как *Error; nil — внешнего сбоя в цепочке нет.
// Отказ по смыслу (404, 409 той стороны) остаётся 4xx: «записи нет» — не сбой нашего сервера.
func FromExternal(err error) *Error {
	var x External
	if !errors.As(err, &x) {
		return nil
	}
	var e *Error
	switch x.ExternalStatus() {
	case http.StatusGatewayTimeout:
		e = build(ErrTypeUpstreamTimeout, MsgUpstreamTimeout)
	case http.StatusServiceUnavailable:
		e = build(ErrTypeUpstreamDown, MsgUpstreamDown)
	case http.StatusNotFound:
		e = build(ErrTypeNotFound, MsgUpstreamRefused)
	case http.StatusConflict:
		e = build(ErrTypeConflict, MsgUpstreamRefused)
	default:
		e = build(ErrTypeBadGateway, MsgBadGateway)
	}
	return e.WithCause(err)
}

// build конструирует *Error и захватывает место ВЫЗОВА конструктора как Caller.
// Кадры с точки зрения runtime.Caller: 0 = build, 1 = NewXError, 2 = пользовательский код.
func build(t Type, msg string) *Error {
	caller := ""
	if pc, file, line, ok := runtime.Caller(2); ok {
		fname := "?"
		if fn := runtime.FuncForPC(pc); fn != nil {
			fname = fn.Name()
		}
		caller = fmt.Sprintf("%s at %s:%d", fname, path.Base(file), line)
	}
	return &Error{Type: t, Message: msg, Caller: caller}
}
