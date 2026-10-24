package handlers

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"main/domain"
	"main/out"
	"main/pkg/errs"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Вход в кабинет и роли (FEATURE_AUTH; agency/system/api_contract_auth.md). Флаг выключен —
// маршрутов /api/auth/* нет (GET /api/auth/me → 404) и мидлвара нет: всё как раньше.

// SessionCookie — имя cookie сессии.
const SessionCookie = "korsik_session"

// viewerKey — ключ текущего пользователя в c.Locals.
const viewerKey = "viewer"

// Viewer — текущий пользователь запроса; nil — входа нет (флаг выключен или открытый маршрут).
func Viewer(c *fiber.Ctx) *domain.User {
	u, _ := c.Locals(viewerKey).(*domain.User)
	return u
}

// authOn — вход включён.
func (h *Handler) authOn() bool { return h.services.Auth.Enabled() }

// clientIP — адрес клиента для лимита входов. За Caddy (reverse_proxy) X-Forwarded-For ставит
// прокси, последний элемент — адрес, который видел он; без прокси — адрес соединения.
func clientIP(c *fiber.Ctx) string {
	if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	return c.IP()
}

// secureCookie — Secure вне development и при https (в том числе за прокси).
func secureCookie(c *fiber.Ctx) bool { return !isDev() || c.Protocol() == "https" }

func setSessionCookie(c *fiber.Ctx, value string, expires time.Time) {
	ck := &fiber.Cookie{Name: SessionCookie, Value: value, Path: "/", HTTPOnly: true,
		Secure: secureCookie(c), SameSite: fiber.CookieSameSiteLaxMode, Expires: expires}
	if value == "" {
		ck.MaxAge = -1
		ck.Expires = time.Unix(0, 0)
	} else {
		ck.MaxAge = int(time.Until(expires).Seconds())
	}
	c.Cookie(ck)
}

type loginBody struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// Login — POST /api/auth/login {"login","password"} → {"user"} + cookie.
func (h *Handler) Login(c *fiber.Ctx) error {
	var b loginBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	var fe []errs.FieldError
	if strings.TrimSpace(b.Login) == "" {
		fe = append(fe, errs.FieldError{Field: "login", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	if b.Password == "" {
		fe = append(fe, errs.FieldError{Field: "password", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	if len(fe) > 0 {
		return errs.NewValidationError(errs.MsgInvalidFields).WithDetails(fe...)
	}
	u, sess, err := h.services.Auth.Login(c.UserContext(), b.Login, b.Password, clientIP(c))
	if errors.Is(err, services.ErrTooManyAttempts) {
		logRefusal(c, http.StatusTooManyRequests, err)
		c.Set(fiber.HeaderRetryAfter, "60")
		return writeError(c, http.StatusTooManyRequests, out.ErrorInfo{Code: services.CodeTooManyAttempts,
			Message: "Слишком много попыток входа. Подождите минуту"})
	}
	if err != nil {
		return err
	}
	setSessionCookie(c, sess.ID, sess.ExpiresAt)
	return c.JSON(fiber.Map{"user": out.UserFrom(*u)})
}

// Logout — POST /api/auth/logout → 204. Без сессии — тоже 204.
func (h *Handler) Logout(c *fiber.Ctx) error {
	if err := h.services.Auth.Logout(c.UserContext(), c.Cookies(SessionCookie)); err != nil {
		return err
	}
	setSessionCookie(c, "", time.Time{})
	return c.SendStatus(http.StatusNoContent)
}

// Me — GET /api/auth/me → {"user"} | 401.
func (h *Handler) Me(c *fiber.Ctx) error {
	u := Viewer(c)
	if u == nil {
		return errs.NewUnauthorizedError("Нужно войти в кабинет")
	}
	return c.JSON(fiber.Map{"user": out.UserFrom(*u)})
}

// ── Мидлвар: сессия и матрица ролей ─────────────────────────────────────────

// publicPath — маршруты без сессии: готовность, вход и выход. Dev-маршруты (демо-чат, машина
// времени, имитация сообщений) тоже за сессией: стенд за ними пишет в живой MAX.
func publicPath(c *fiber.Ctx) bool {
	p := c.Path()
	switch p {
	case "/api/health", "/api/auth/login", "/api/auth/logout":
		return true
	}
	return false
}

var (
	reSendTest    = regexp.MustCompile(`^/api/scenarios/[^/]+/steps/[^/]+/send-test$`)
	reNewcomerID  = regexp.MustCompile(`^/api/newcomers/([0-9a-fA-F-]{36})(/|$)`)
	reTicketID    = regexp.MustCompile(`^/api/tickets/([0-9a-fA-F-]{36})(/|$)`)
	rePreviewPath = regexp.MustCompile(`^/api/scenarios/[^/]+/publish/preview$`)
)

// AuthRequired — сессия из cookie → текущий пользователь в c.Locals и в UserContext; без неё —
// 401 (кроме publicPath). Затем матрица ролей (policy). Ставится на /api только при FEATURE_AUTH.
func (h *Handler) AuthRequired(c *fiber.Ctx) error {
	if sid := c.Cookies(SessionCookie); sid != "" {
		u, err := h.services.Auth.UserBySession(c.UserContext(), sid)
		var e *errs.Error
		switch {
		case err == nil:
			c.Locals(viewerKey, u)
			c.SetUserContext(services.WithViewer(c.UserContext(), u))
		case errors.As(err, &e) && e.Type == errs.ErrTypeUnauthenticated:
			// истекла или пользователь выключен — как без cookie
		default:
			return err
		}
	}
	if publicPath(c) {
		return c.Next()
	}
	u := Viewer(c)
	if u == nil {
		return errs.NewUnauthorizedError("Нужно войти в кабинет")
	}
	if err := h.policy(c, u); err != nil {
		return err
	}
	return c.Next()
}

func errForbidden() error { return errs.NewForbiddenError("Недостаточно прав") }

// policy — матрица ролей (api_contract_auth.md, «Роли»). ЕДИНСТВЕННОЕ место, где роль решает
// доступ к маршруту; фильтры «только свои» — в хендлерах списков.
func (h *Handler) policy(c *fiber.Ctx, u *domain.User) error {
	p, m := c.Path(), c.Method()
	if strings.HasPrefix(p, "/api/auth/") {
		return nil
	}
	switch u.Role {
	case domain.RoleHRLead:
		return nil
	case domain.RoleManager:
		// Только «Мои новички» по своим подчинённым: без текстов ответов и обращений.
		if p == "/api/manager/newcomers" && m == fiber.MethodGet {
			return nil
		}
		if strings.HasPrefix(p, "/api/manager/tasks/") && m == fiber.MethodPost {
			return nil // свои подчинённые — проверка в UserService.ManagerTask
		}
		return errForbidden()
	case domain.RoleRecruiter:
		return h.recruiterPolicy(c, u, p, m)
	}
	return errForbidden()
}

// recruiterPolicy — рекрутер: свои новички и обращения (чужой — 404), сценарии и база знаний —
// чтение, без команды, пользователей, переназначения и вида руководителя (/api/manager/*;
// ссылка-приглашение руководителя из карточки — /api/newcomers/:id/manager-invite — остаётся).
func (h *Handler) recruiterPolicy(c *fiber.Ctx, u *domain.User, p, m string) error {
	read := m == fiber.MethodGet || m == fiber.MethodHead
	switch {
	case p == "/api/team" || strings.HasPrefix(p, "/api/users") || p == "/api/newcomers/reassign" ||
		strings.HasPrefix(p, "/api/hrlead/") || p == "/api/dev/recruiter-morning" ||
		strings.HasPrefix(p, "/api/manager/"):
		return errForbidden()
	case strings.HasPrefix(p, "/api/scenarios"):
		if read || rePreviewPath.MatchString(p) || (isDev() && reSendTest.MatchString(p)) {
			return nil
		}
		return errs.NewForbiddenError("Сценарии рекрутеру — только чтение")
	case strings.HasPrefix(p, "/api/documents") || strings.HasPrefix(p, "/api/knowledge") || strings.HasPrefix(p, "/api/media"):
		if read || p == "/api/documents/ask" {
			return nil
		}
		return errs.NewForbiddenError("База знаний рекрутеру — только чтение")
	}
	if mm := reNewcomerID.FindStringSubmatch(p); mm != nil {
		return h.ownNewcomer(c, u, mm[1])
	}
	if mm := reTicketID.FindStringSubmatch(p); mm != nil {
		id, err := uuid.Parse(mm[1])
		if err != nil {
			return nil // разберёт pathID хендлера
		}
		emp, err := h.services.Users.TicketNewcomer(c.UserContext(), id)
		if err != nil {
			return err
		}
		if emp == uuid.Nil {
			return nil // 404 отдаст хендлер
		}
		return h.ownNewcomer(c, u, emp.String())
	}
	return nil
}

// ownNewcomer — новичок закреплён за рекрутером u; чужой — 404 (существование не выдаём).
func (h *Handler) ownNewcomer(c *fiber.Ctx, u *domain.User, raw string) error {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	rec, found, err := h.services.Users.NewcomerRecruiter(c.UserContext(), id)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if rec == nil || *rec != u.ID {
		return errs.NewNotFoundError("Новичок не найден")
	}
	return nil
}

// recruiterScope — фильтр «только свои» для списков: id рекрутера для роли recruiter.
func recruiterScope(c *fiber.Ctx) *uuid.UUID { return services.RecruiterScope(Viewer(c)) }
