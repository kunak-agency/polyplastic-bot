// Package in — DTO запросов API и их валидация. Валидный запрос превращается в типизированный
// вход слоя services; ошибки — пополевые (errs.FieldError) в одном отказе 422.
package in

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"
	"main/services"

	"github.com/google/uuid"
)

// DateLayout — даты API (api_contract.md: YYYY-MM-DD).
const DateLayout = "2006-01-02"

// maxField — потолок длины текстовых полей карточки.
const maxField = 200

// fields — сборщик пополевых ошибок.
type fields []errs.FieldError

func (f *fields) add(field, code, msg string) {
	*f = append(*f, errs.FieldError{Field: field, Code: code, Message: msg})
}

func (f fields) err() error {
	if len(f) == 0 {
		return nil
	}
	return errs.NewValidationError(errs.MsgInvalidFields).WithDetails(f...)
}

// text — обрезанная строка с проверкой длины и обязательности.
func (f *fields) text(name, v string, required bool) string {
	v = strings.TrimSpace(v)
	switch {
	case required && v == "":
		f.add(name, errs.CodeRequired, "Обязательное поле")
	case utf8.RuneCountInString(v) > maxField:
		f.add(name, errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxField))
	}
	return v
}

func (f *fields) site(name, v string) string {
	v = strings.TrimSpace(v)
	if _, ok := domain.Sites[v]; !ok {
		f.add(name, errs.CodeOutOfRange, "Площадка: GrPP, Luzhniki или GTP")
	}
	return v
}

func (f *fields) date(name, v string) time.Time {
	d, err := time.Parse(DateLayout, strings.TrimSpace(v))
	if err != nil {
		f.add(name, errs.CodeInvalidFormat, "Дата в формате YYYY-MM-DD")
	}
	return d
}

func (f *fields) uuid(name, v string) uuid.UUID {
	id, err := uuid.Parse(strings.TrimSpace(v))
	if err != nil {
		f.add(name, errs.CodeInvalidFormat, "Ожидается UUID")
	}
	return id
}

// rePhone — телефон: +, цифры, пробелы, скобки, дефисы; 10–15 цифр.
var rePhone = regexp.MustCompile(`^\+?[\d\s()\-]{10,20}$`)

// NewcomerCreate — тело POST /api/newcomers: поля карточки без вычисляемых.
type NewcomerCreate struct {
	FullName      string          `json:"full_name"`
	Phone         string          `json:"phone"`
	Site          string          `json:"site"`
	Department    string          `json:"department"`
	Position      string          `json:"position"`
	ManagerName   string          `json:"manager_name"`
	MentorName    string          `json:"mentor_name"`
	RecruiterName string          `json:"recruiter_name"`
	StartDate     string          `json:"start_date"`
	Shift         json.RawMessage `json:"shift"` // "1" | 1 | null
	ScenarioID    string          `json:"scenario_id"`
	TZ            json.RawMessage `json:"tz"` // IANA-пояс новичка; null/нет — пояс площадки
}

// Validate — карточка в вход сервиса.
func (n NewcomerCreate) Validate() (services.NewcomerInput, error) {
	var f fields
	out := services.NewcomerInput{
		FullName:      f.text("full_name", n.FullName, true),
		Phone:         f.text("phone", n.Phone, false),
		Site:          f.site("site", n.Site),
		Department:    f.text("department", n.Department, false),
		Position:      f.text("position", n.Position, false),
		ManagerName:   f.text("manager_name", n.ManagerName, false),
		MentorName:    f.text("mentor_name", n.MentorName, false),
		RecruiterName: f.text("recruiter_name", n.RecruiterName, false),
		StartDate:     f.date("start_date", n.StartDate),
		ScenarioID:    f.uuid("scenario_id", n.ScenarioID),
	}
	if out.Phone != "" && !rePhone.MatchString(out.Phone) {
		f.add("phone", errs.CodeInvalidFormat, "Телефон: +7 и 10 цифр")
	}
	out.Shift = parseShift(&f, n.Shift)
	if tz := parseTZ(&f, n.TZ); tz != nil && *tz != "" {
		out.TZ = tz
	}
	return out, f.err()
}

// parseShift — смена строкой или числом; пусто и null — смены нет.
func parseShift(f *fields, raw json.RawMessage) *string {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	switch s {
	case "", "null":
		return nil
	case "1", "2", "3":
		return &s
	}
	f.add("shift", errs.CodeOutOfRange, "Смена: 1, 2, 3 или null")
	return nil
}

// NewcomerPatch — тело PATCH /api/newcomers/{id}: перенос даты выхода и/или пояс новичка
// (api_contract_auth.md, «Пояс»). tz — IANA-имя («Asia/Yekaterinburg»); null или "" — снять,
// вернуть пояс площадки; поля нет — не менять.
type NewcomerPatch struct {
	StartDate string          `json:"start_date"`
	TZ        json.RawMessage `json:"tz"`
}

func (p NewcomerPatch) Validate() (time.Time, error) {
	var f fields
	d := f.date("start_date", p.StartDate)
	return d, f.err()
}

// ValidateChange — что меняем: дата (nil — не меняем) и пояс (nil — не меняем). Нужно хотя бы одно.
func (p NewcomerPatch) ValidateChange() (*time.Time, *string, error) {
	var f fields
	tz := parseTZ(&f, p.TZ)
	var start *time.Time
	if strings.TrimSpace(p.StartDate) != "" || tz == nil {
		d := f.date("start_date", p.StartDate)
		start = &d
	}
	return start, tz, f.err()
}

// parseTZ — поле tz: нет поля — nil (не менять); null или "" — "" (пояс площадки); иначе
// IANA-имя зоны с «/» (Asia/Yekaterinburg). Неизвестная зона — ошибка поля.
func parseTZ(f *fields, raw json.RawMessage) *string {
	if len(raw) == 0 {
		return nil
	}
	empty := ""
	if strings.TrimSpace(string(raw)) == "null" {
		return &empty
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		f.add("tz", errs.CodeInvalidFormat, "Пояс IANA строкой, например Asia/Yekaterinburg")
		return nil
	}
	if v = strings.TrimSpace(v); v == "" {
		return &empty
	}
	if _, err := time.LoadLocation(v); err != nil || !strings.Contains(v, "/") {
		f.add("tz", errs.CodeInvalidFormat, "Неизвестный пояс: нужно IANA-имя, например Asia/Yekaterinburg")
		return nil
	}
	return &v
}

// TimeTravel — тело POST /api/newcomers/{id}/time-travel.
type TimeTravel struct {
	Day *int `json:"day"`
}

// Границы машины времени: от D−30 до D+120 — весь таймлайн с запасом.
const (
	minTravelDay = -30
	maxTravelDay = 120
)

func (t TimeTravel) Validate() (int, error) {
	var f fields
	switch {
	case t.Day == nil:
		f.add("day", errs.CodeRequired, "Обязательное поле")
	case *t.Day < minTravelDay || *t.Day > maxTravelDay:
		f.add("day", errs.CodeOutOfRange, fmt.Sprintf("День от %d до %d", minTravelDay, maxTravelDay))
	default:
		return *t.Day, nil
	}
	return 0, f.err()
}

// NewcomerQuery — фильтры GET /api/newcomers.
type NewcomerQuery struct {
	Site string `query:"site"`
	Q    string `query:"q"`
	Risk string `query:"risk"`
}

func (q NewcomerQuery) Validate() (repositories.EmployeeFilter, error) {
	var f fields
	out := repositories.EmployeeFilter{Site: strings.TrimSpace(q.Site), Q: strings.TrimSpace(q.Q), Risk: strings.TrimSpace(q.Risk)}
	if out.Site != "" {
		f.site("site", out.Site)
	}
	switch out.Risk {
	case "", domain.RiskGreen, domain.RiskYellow, domain.RiskRed:
	default:
		f.add("risk", errs.CodeOutOfRange, "Риск: green, yellow или red")
	}
	if utf8.RuneCountInString(out.Q) > maxField {
		f.add("q", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxField))
	}
	return out, f.err()
}

// IPDecision — POST /api/newcomers/{id}/ip-decision {"decision": "keep"|"extend"|"part"|null}.
// Поле обязательно: null сбрасывает решение в «не задано».
type IPDecision struct {
	Decision *string `json:"decision"`
}

// Validate — допустимое значение или null.
func (b IPDecision) Validate() (*string, error) {
	if b.Decision == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*b.Decision)
	for _, d := range domain.IPDecisions {
		if v == d {
			return &v, nil
		}
	}
	var f fields
	f.add("decision", errs.CodeOutOfRange, "Решение: keep, extend, part или null")
	return nil, f.err()
}
