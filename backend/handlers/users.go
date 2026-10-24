package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"main/domain"
	"main/out"
	"main/pkg/errs"
	"main/repositories"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Пользователи, команда, переназначение, дашборды ролей (FEATURE_AUTH; api_contract_auth.md).
// Роль проверяет policy (auth.go); здесь — разбор запроса и фильтры «только свои».

// ListUsers — GET /api/users?role=&active=1.
func (h *Handler) ListUsers(c *fiber.Ctx) error {
	f := repositories.UserFilter{Role: strings.TrimSpace(c.Query("role"))}
	if a := c.Query("active"); a != "" {
		v := a == "1" || a == "true"
		f.Active = &v
	}
	list, err := h.services.Users.List(c.UserContext(), f)
	if err != nil {
		return err
	}
	return c.JSON(out.NewList(out.UsersFrom(list)))
}

type userCreateBody struct {
	Login       string  `json:"login"`
	FullName    string  `json:"full_name"`
	Password    string  `json:"password"`
	Role        string  `json:"role"`
	TZ          *string `json:"tz"`
	ManagerName *string `json:"manager_name"`
}

// CreateUser — POST /api/users → 201 пользователь.
func (h *Handler) CreateUser(c *fiber.Ctx) error {
	var b userCreateBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	u, err := h.services.Users.Create(c.UserContext(), services.UserInput{Login: b.Login, FullName: b.FullName,
		Password: b.Password, Role: b.Role, TZ: b.TZ, ManagerName: b.ManagerName, Active: true})
	if err != nil {
		return err
	}
	return c.Status(http.StatusCreated).JSON(out.UserFrom(*u))
}

type userPatchBody struct {
	Active      *bool   `json:"active"`
	Role        *string `json:"role"`
	FullName    *string `json:"full_name"`
	Password    *string `json:"password"`
	ManagerName *string `json:"manager_name"`
	Login       *string `json:"login"`
}

// PatchUser — PATCH /api/users/{id} {"active"?,"role"?,"full_name"?,"password"?,"manager_name"?,
// "login"?}. login (+ password, если пароля нет) — «выдать вход» руководителю из справочника.
func (h *Handler) PatchUser(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var b userPatchBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	u, err := h.services.Users.Patch(c.UserContext(), id, services.UserPatch{Active: b.Active, Role: b.Role,
		FullName: b.FullName, Password: b.Password, ManagerName: b.ManagerName, Login: b.Login})
	if err != nil {
		return err
	}
	return c.JSON(out.UserFrom(*u))
}

type reassignBody struct {
	NewcomerIDs []string `json:"newcomer_ids"`
	RecruiterID *string  `json:"recruiter_id"`
}

// parseRecruiterID — "uuid" | null | "" → *uuid.
func parseRecruiterID(raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "recruiter_id", Code: errs.CodeInvalidFormat, Message: "Ожидается UUID"})
	}
	return &id, nil
}

// Reassign — POST /api/newcomers/reassign {"newcomer_ids","recruiter_id"} → {"updated"}.
func (h *Handler) Reassign(c *fiber.Ctx) error {
	var b reassignBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	if len(b.NewcomerIDs) == 0 {
		return errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "newcomer_ids", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	list := make([]uuid.UUID, 0, len(b.NewcomerIDs))
	for _, s := range b.NewcomerIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return errs.NewValidationError(errs.MsgInvalidFields).
				WithDetails(errs.FieldError{Field: "newcomer_ids", Code: errs.CodeInvalidFormat, Message: "Ожидается UUID"})
		}
		list = append(list, id)
	}
	rec, err := parseRecruiterID(b.RecruiterID)
	if err != nil {
		return err
	}
	n, err := h.services.Users.Reassign(c.UserContext(), list, rec)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"updated": n})
}

// Team — GET /api/team (hr_lead).
func (h *Handler) Team(c *fiber.Ctx) error {
	t, err := h.services.Users.Team(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(out.TeamFrom(t))
}

// dashboardSite — ?site= (пусто — все площадки).
func dashboardSite(c *fiber.Ctx) (string, error) {
	site := strings.TrimSpace(c.Query("site"))
	if site == "" {
		return "", nil
	}
	if _, ok := domain.Sites[site]; !ok {
		return "", errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "site", Code: errs.CodeOutOfRange, Message: "Площадка: GrPP, Luzhniki или GTP"})
	}
	return site, nil
}

// RecruiterDashboard — GET /api/recruiter/dashboard?site=&recruiter_id=. Рекрутер — свои
// новички; hr_lead — все или ?recruiter_id=<uuid>|none.
func (h *Handler) RecruiterDashboard(c *fiber.Ctx) error {
	site, err := dashboardSite(c)
	if err != nil {
		return err
	}
	sc := services.DashboardScope{RecruiterID: recruiterScope(c)}
	if sc.RecruiterID == nil {
		switch raw := strings.TrimSpace(c.Query("recruiter_id")); raw {
		case "":
		case "none":
			sc.Unassigned = true
		default:
			if sc.RecruiterID, err = parseRecruiterID(&raw); err != nil {
				return err
			}
		}
	}
	d, err := h.services.Users.RecruiterDashboard(c.UserContext(), sc, site, h.services.Overview())
	if err != nil {
		return err
	}
	return c.JSON(out.RecruiterDashboardFrom(d))
}

// HRLeadDashboard — GET /api/hrlead/dashboard?site= (hr_lead).
func (h *Handler) HRLeadDashboard(c *fiber.Ctx) error {
	site, err := dashboardSite(c)
	if err != nil {
		return err
	}
	d, err := h.services.Users.HRLeadDashboard(c.UserContext(), site, h.services.Digest(), h.services.Overview())
	if err != nil {
		return err
	}
	return c.JSON(out.HRLeadDashboardFrom(d))
}

// MarkContacted — POST /api/newcomers/{id}/contacted {"note"?} → 204 (note не хранится).
func (h *Handler) MarkContacted(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	if _, _, err := h.services.Users.MarkContacted(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(http.StatusNoContent)
}

// newcomerRecruiterField — recruiter_id из тела POST/PATCH /api/newcomers: present — ключ есть.
func newcomerRecruiterField(c *fiber.Ctx) (raw *string, present bool, err error) {
	var m map[string]json.RawMessage
	if len(c.Body()) == 0 || json.Unmarshal(c.Body(), &m) != nil {
		return nil, false, nil // разбор тела — забота основного DTO
	}
	v, ok := m["recruiter_id"]
	if !ok {
		return nil, false, nil
	}
	if string(v) == "null" {
		return nil, true, nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return nil, true, errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "recruiter_id", Code: errs.CodeInvalidFormat, Message: "Ожидается UUID или null"})
	}
	return &s, true, nil
}

// ── Новички: «только свои», закрепление рекрутера (вставки в newcomers.go) ──────────

// scopeNewcomers — фильтр реестра: роль recruiter — только свои (всегда); ?mine=1 — свои;
// ?recruiter_id=<uuid>|none — для hr_lead (none — без рекрутера).
func (h *Handler) scopeNewcomers(c *fiber.Ctx, f *repositories.EmployeeFilter) error {
	if id := recruiterScope(c); id != nil {
		f.RecruiterID = id
		return nil
	}
	if v := Viewer(c); v != nil && (c.Query("mine") == "1" || c.Query("mine") == "true") {
		id := v.ID
		f.RecruiterID = &id
		return nil
	}
	switch raw := strings.TrimSpace(c.Query("recruiter_id")); raw {
	case "":
	case "none":
		f.NoRecruiter = true
	default:
		id, err := parseRecruiterID(&raw)
		if err != nil {
			return err
		}
		f.RecruiterID = id
	}
	return nil
}

// fillRecruiterNames — recruiter_name карточки = full_name закреплённого рекрутера.
func (h *Handler) fillRecruiterNames(c *fiber.Ctx, cards []services.NewcomerCard) {
	var list []uuid.UUID
	for _, x := range cards {
		if x.Employee.RecruiterID != nil {
			list = append(list, *x.Employee.RecruiterID)
		}
	}
	if len(list) == 0 {
		return
	}
	names, err := h.services.Users.RecruiterNames(c.UserContext(), list)
	if err != nil {
		return // имя — украшение: старое текстовое поле остаётся
	}
	for i := range cards {
		if id := cards[i].Employee.RecruiterID; id != nil && names[*id] != "" {
			cards[i].Employee.RecruiterName = names[*id]
		}
	}
}

// createRecruiter — recruiter_id для «Запустить адаптацию»: роль recruiter — всегда он сам;
// иначе из тела (hr_lead или вход выключен).
func (h *Handler) createRecruiter(c *fiber.Ctx) (*uuid.UUID, error) {
	if id := recruiterScope(c); id != nil {
		return id, nil
	}
	raw, _, err := newcomerRecruiterField(c)
	if err != nil {
		return nil, err
	}
	return parseRecruiterID(raw)
}

// patchRecruiter — recruiter_id в PATCH /api/newcomers/{id}: меняет только hr_lead (при входе),
// иначе 403. done — в теле больше ничего не было, ответ уже отправлен.
func (h *Handler) patchRecruiter(c *fiber.Ctx, id uuid.UUID) (bool, error) {
	raw, present, err := newcomerRecruiterField(c)
	if err != nil || !present {
		return false, err
	}
	if v := Viewer(c); h.authOn() && (v == nil || v.Role != domain.RoleHRLead) {
		return true, errs.NewForbiddenError("Рекрутера меняет руководитель HR")
	}
	rec, err := parseRecruiterID(raw)
	if err != nil {
		return true, err
	}
	n, err := h.services.Users.Reassign(c.UserContext(), []uuid.UUID{id}, rec)
	if err != nil {
		return true, err
	}
	if n == 0 {
		return true, errs.NewNotFoundError("Новичок не найден")
	}
	return false, nil // ответ — в PatchNewcomer (onlyPeoplePatch) или после start_date/tz
}

// NewcomerFiles — GET /api/newcomers/{id}/files → {"items":[{name,filename,mime,size,updated_at}]}.
func (h *Handler) NewcomerFiles(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	list, err := h.services.Users.NewcomerFiles(c.UserContext(), id)
	if err != nil {
		return err
	}
	items := make([]fiber.Map, 0, len(list))
	for _, f := range list {
		items = append(items, fiber.Map{"name": f.Name, "filename": f.Filename, "mime": f.Mime, "size": f.Size,
			"updated_at": f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	return c.JSON(fiber.Map{"items": items, "total": len(items)})
}

// ManagerTask — POST /api/manager/tasks/{id} {"status":"done|planned"} → 204.
func (h *Handler) ManagerTask(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var b struct {
		Status string `json:"status"`
	}
	if err := bindBody(c, &b); err != nil {
		return err
	}
	if err := h.services.Users.ManagerTask(c.UserContext(), id, strings.TrimSpace(b.Status)); err != nil {
		return err
	}
	return c.SendStatus(http.StatusNoContent)
}
