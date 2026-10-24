// Package out — DTO ответов API.
package out

import "main/pkg/errs"

// ErrorBody — конверт любой ошибки API: {"error":{"code","message","traceId"}}
// (agency/system/api_contract.md). Фронт разбирает его в одном месте — форма меняется только
// вместе с контрактом.
type ErrorBody struct {
	Error ErrorInfo `json:"error"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	TraceID string `json:"traceId"`
	// Details — пополевые ошибки валидации; нет — поля нет.
	Details []errs.FieldError `json:"details,omitempty"`
	// Caller — место создания ошибки, только при APP_ENV=development.
	Caller string `json:"caller,omitempty"`
}

// Health — GET /api/health. status — живость процесса (всегда ok, раз ответил); db и rag —
// готовность зависимостей: ok | down.
type Health struct {
	Status   string         `json:"status"`
	DB       string         `json:"db"`
	RAG      string         `json:"rag"`
	Features HealthFeatures `json:"features"`
}

// HealthFeatures — фича-флаги, от которых зависит вид кабинета (не секреты): nudges_default —
// FEATURE_NUDGES_DEFAULT, конструктор показывает правило напоминаний по умолчанию.
type HealthFeatures struct {
	NudgesDefault bool `json:"nudges_default"`
}

// OkDown — true → "ok", false → "down".
func OkDown(up bool) string {
	if up {
		return "ok"
	}
	return "down"
}
