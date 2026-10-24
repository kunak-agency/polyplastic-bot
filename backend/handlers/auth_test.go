package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/externalApi/max"
	"main/repositories"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Вход и роли (FEATURE_AUTH; api_contract_auth.md) через настоящую таблицу маршрутов.
// Postgres из TEST_DATABASE_URL; без базы тесты пропускаются.

type authAPI struct {
	t     *testing.T
	app   *fiber.App
	repos *repositories.Repos
	svc   *services.Services
	now   time.Time
}

func newAuthAPI(t *testing.T, auth bool) *authAPI {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	repos, err := repositories.InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	if err := repos.Truncate(context.Background()); err != nil {
		t.Fatal(err)
	}
	moscow, _ := time.LoadLocation("Europe/Moscow")
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, moscow)
	svc := services.NewServices(repos, &externalApi.ExternalAPI{Max: max.NewFake()}, services.Options{
		Bot:      config.Bot{Username: "korsik_test_bot", InviteLinkFormat: config.DefaultInviteLinkFormat},
		Now:      func() time.Time { return now },
		Features: config.Features{Auth: auth},
	})
	app := fiber.New(ServerConfig(DefaultTimeouts))
	app.Use(RequestID)
	RegisterRoutes(app, NewHandlerManager(svc))
	return &authAPI{t: t, app: app, repos: repos, svc: svc, now: now}
}

// do — запрос с cookie сессии (пусто — без неё); статус сверяется с want.
func (a *authAPI) do(method, path, cookie string, body any, want int) (map[string]any, *http.Response) {
	a.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	if cookie != "" {
		req.Header.Set("Cookie", SessionCookie+"="+cookie)
	}
	resp, err := a.app.Test(req, 20_000)
	if err != nil {
		a.t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		a.t.Fatalf("%s %s: статус %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	out := map[string]any{}
	if len(raw) > 0 && raw[0] == '{' {
		_ = json.Unmarshal(raw, &out)
	}
	return out, resp
}

func (a *authAPI) user(login, role string, managerName *string) *domain.User {
	a.t.Helper()
	u, err := a.svc.Users.Create(context.Background(), services.UserInput{Login: login, FullName: "Синтетик " + login,
		Password: "password-" + login, Role: role, ManagerName: managerName, Active: true})
	if err != nil {
		a.t.Fatal(err)
	}
	return u
}

// login — вход, значение cookie сессии.
func (a *authAPI) login(login string) string {
	a.t.Helper()
	_, resp := a.do("POST", "/api/auth/login", "", map[string]string{"login": login, "password": "password-" + login}, 200)
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			return c.Value
		}
	}
	a.t.Fatal("нет cookie сессии")
	return ""
}

func (a *authAPI) newcomer(name, manager string, recruiter *uuid.UUID) domain.Employee {
	a.t.Helper()
	e := domain.Employee{FullName: name, Site: domain.SiteGTP, ManagerName: manager, RecruiterID: recruiter,
		StartDate: a.now.AddDate(0, 0, -2), Connection: domain.ConnInvited, Risk: domain.RiskGreen, ScenarioID: uuid.New()}
	if err := a.repos.Employees.Create(context.Background(), &e); err != nil {
		a.t.Fatal(err)
	}
	return e
}

func (a *authAPI) ticket(emp uuid.UUID) domain.Ticket {
	a.t.Helper()
	tk := domain.Ticket{EmployeeID: emp, Category: "F2", Priority: "P1", Status: domain.TicketOpen, Route: "hr", Summary: "синтетика"}
	if err := a.repos.Tickets.Create(context.Background(), &tk); err != nil {
		a.t.Fatal(err)
	}
	return tk
}

func itemsLen(m map[string]any) int {
	items, _ := m["items"].([]any)
	return len(items)
}

func TestAuthFlagOffEverythingOpen(t *testing.T) {
	a := newAuthAPI(t, false)
	a.do("GET", "/api/auth/me", "", nil, 404)
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "x", "password": "y"}, 404)
	a.newcomer("Открытый Новичок (синтетика)", "Рук", nil)
	got, _ := a.do("GET", "/api/newcomers", "", nil, 200)
	if itemsLen(got) != 1 {
		t.Fatalf("без флага реестр открыт: %v", got)
	}
	a.do("GET", "/api/tickets", "", nil, 200)
	a.do("GET", "/api/recruiter/dashboard", "", nil, 404)
}

func TestAuthLoginLogout(t *testing.T) {
	a := newAuthAPI(t, true)
	a.user("anna", domain.RoleHRLead, nil)

	a.do("GET", "/api/newcomers", "", nil, 401)
	a.do("GET", "/api/auth/me", "", nil, 401)

	got, _ := a.do("POST", "/api/auth/login", "", map[string]string{"login": "anna", "password": "wrong-password"}, 401)
	if e, _ := got["error"].(map[string]any); e["code"] != services.CodeInvalidCredentials {
		t.Fatalf("код: %v", got)
	}
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "", "password": ""}, 422)

	_, resp := a.do("POST", "/api/auth/login", "", map[string]string{"login": " Anna ", "password": "password-anna"}, 200)
	var sid string
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			sid = c.Value
			if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
				t.Fatalf("cookie: %+v", c)
			}
		}
	}
	if len(sid) != 64 {
		t.Fatalf("сессия: %q", sid)
	}
	me, _ := a.do("GET", "/api/auth/me", sid, nil, 200)
	if u, _ := me["user"].(map[string]any); u["role"] != domain.RoleHRLead || u["login"] != "anna" {
		t.Fatalf("me: %v", me)
	}
	a.do("GET", "/api/newcomers", sid, nil, 200)
	a.do("POST", "/api/auth/logout", sid, nil, 204)
	a.do("GET", "/api/auth/me", sid, nil, 401)
	a.do("POST", "/api/auth/logout", "", nil, 204)
}

func TestAuthRateLimit(t *testing.T) {
	a := newAuthAPI(t, true)
	a.user("anna", domain.RoleHRLead, nil)
	for i := 0; i < services.LoginMaxFailures; i++ {
		a.do("POST", "/api/auth/login", "", map[string]string{"login": "anna", "password": "wrong-password"}, 401)
	}
	got, _ := a.do("POST", "/api/auth/login", "", map[string]string{"login": "anna", "password": "password-anna"}, 429)
	if e, _ := got["error"].(map[string]any); e["code"] != services.CodeTooManyAttempts {
		t.Fatalf("код: %v", got)
	}
}

func TestAuthRecruiterSeesOnlyOwn(t *testing.T) {
	a := newAuthAPI(t, true)
	r1 := a.user("maria", domain.RoleRecruiter, nil)
	r2 := a.user("olga", domain.RoleRecruiter, nil)
	mine := a.newcomer("Свой Новичок (синтетика)", "Рук А", &r1.ID)
	other := a.newcomer("Чужой Новичок (синтетика)", "Рук Б", &r2.ID)
	myTicket := a.ticket(mine.ID)
	otherTicket := a.ticket(other.ID)
	sid := a.login("maria")

	got, _ := a.do("GET", "/api/newcomers", sid, nil, 200)
	if itemsLen(got) != 1 {
		t.Fatalf("рекрутер видит только своих: %v", got)
	}
	card, _ := a.do("GET", "/api/newcomers/"+mine.ID.String(), sid, nil, 200)
	if card["recruiter_id"] != r1.ID.String() || card["recruiter_name"] != r1.FullName {
		t.Fatalf("recruiter_id/name: %v", card)
	}
	a.do("GET", "/api/newcomers/"+other.ID.String(), sid, nil, 404)
	a.do("GET", "/api/newcomers/"+other.ID.String()+"/timeline", sid, nil, 404)
	a.do("GET", "/api/tickets/"+otherTicket.ID.String(), sid, nil, 404)
	a.do("GET", "/api/tickets/"+myTicket.ID.String(), sid, nil, 200)
	tk, _ := a.do("GET", "/api/tickets", sid, nil, 200)
	if itemsLen(tk) != 1 {
		t.Fatalf("обращения только свои: %v", tk)
	}
	a.do("GET", "/api/scenarios", sid, nil, 200)
	a.do("POST", "/api/scenarios/"+uuid.NewString()+"/publish", sid, map[string]any{}, 403)
	a.do("POST", "/api/scenarios", sid, map[string]any{}, 403)
	a.do("GET", "/api/team", sid, nil, 403)
	a.do("POST", "/api/users", sid, map[string]any{}, 403)
	a.do("POST", "/api/newcomers/reassign", sid, map[string]any{}, 403)
	a.do("PATCH", "/api/newcomers/"+mine.ID.String(), sid, map[string]any{"recruiter_id": r2.ID.String()}, 403)

	d, _ := a.do("GET", "/api/recruiter/dashboard", sid, nil, 200)
	tiles, _ := d["tiles"].(map[string]any)
	for _, k := range services.RecruiterTiles {
		if _, ok := tiles[k]; !ok {
			t.Fatalf("нет плитки %s: %v", k, d)
		}
	}
	// Свой новичок вышел, приглашение не открыто → красная строка «Сегодня».
	today, _ := d["today"].([]any)
	if len(today) == 0 || today[0].(map[string]any)["kind"] != services.RowInviteNotOpened {
		t.Fatalf("today: %v", today)
	}
	for _, row := range today {
		if row.(map[string]any)["newcomer_id"] == other.ID.String() {
			t.Fatal("чужой новичок в «Сегодня»")
		}
	}
	a.do("POST", "/api/newcomers/"+mine.ID.String()+"/contacted", sid, nil, 204)
	d, _ = a.do("GET", "/api/recruiter/dashboard", sid, nil, 200)
	if today, _ := d["today"].([]any); len(today) != 0 {
		t.Fatalf("после «связался» строка скрыта: %v", today)
	}
	a.do("POST", "/api/newcomers/"+other.ID.String()+"/contacted", sid, nil, 404)
	a.do("GET", "/api/overview", sid, nil, 200)
	a.do("GET", "/api/hrlead/dashboard", sid, nil, 403)
	t.Setenv("APP_ENV", "development")
	a.do("POST", "/api/dev/recruiter-morning", sid, nil, 403) // утренняя сводка — только hr_lead
}

func TestAuthManagerForbidden(t *testing.T) {
	a := newAuthAPI(t, true)
	boss := "Руководитель Цеха (синтетика)"
	a.user("smirnov", domain.RoleManager, &boss)
	mine := a.newcomer("Подчинённый (синтетика)", boss, nil)
	a.newcomer("Чужой (синтетика)", "Другой руководитель", nil)
	a.ticket(mine.ID)
	sid := a.login("smirnov")

	a.do("GET", "/api/tickets", sid, nil, 403)
	a.do("GET", "/api/newcomers", sid, nil, 403)
	a.do("GET", "/api/newcomers/"+mine.ID.String(), sid, nil, 403)
	a.do("GET", "/api/newcomers/"+mine.ID.String()+"/timeline", sid, nil, 403)
	a.do("GET", "/api/overview", sid, nil, 403)
	a.do("GET", "/api/manager/managers", sid, nil, 403)
	a.do("GET", "/api/recruiter/dashboard", sid, nil, 403)
	a.do("GET", "/api/auth/me", sid, nil, 200)
	a.do("POST", "/api/manager/tasks/"+uuid.NewString(), sid, map[string]string{"status": "done"}, 404)
	a.do("POST", "/api/manager/tasks/"+uuid.NewString(), sid, map[string]string{"status": "bogus"}, 422)

	got, _ := a.do("GET", "/api/manager/newcomers?name="+strings.ReplaceAll("Другой руководитель", " ", "%20"), sid, nil, 200)
	items, _ := got["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["full_name"] != mine.FullName {
		t.Fatalf("руководитель видит только своих, имя из запроса игнорируется: %v", got)
	}
	raw, _ := json.Marshal(got)
	for _, banned := range []string{"answer", "ticket", "quote"} {
		if strings.Contains(string(raw), `"`+banned) {
			t.Fatalf("в ответе руководителю поле %s: %s", banned, raw)
		}
	}
}

func TestAuthDeactivateAndUsers(t *testing.T) {
	a := newAuthAPI(t, true)
	hr := a.user("anna", domain.RoleHRLead, nil)
	hrSID := a.login("anna")

	created, _ := a.do("POST", "/api/users", hrSID, map[string]any{"login": "petr", "full_name": "Пётр (синтетика)",
		"password": "password-petr", "role": "recruiter"}, 201)
	id, _ := created["id"].(string)
	a.do("POST", "/api/users", hrSID, map[string]any{"login": "petr", "full_name": "Ещё", "password": "password-x", "role": "recruiter"}, 409)
	a.do("POST", "/api/users", hrSID, map[string]any{"login": "short", "full_name": "К", "password": "123", "role": "recruiter"}, 422)

	petrSID := a.login("petr")
	a.do("GET", "/api/auth/me", petrSID, nil, 200)

	n := a.newcomer("Без рекрутера (синтетика)", "Рук", nil)
	team, _ := a.do("GET", "/api/team", hrSID, nil, 200)
	if team["unassigned"] != float64(1) {
		t.Fatalf("без рекрутера: %v", team)
	}
	upd, _ := a.do("POST", "/api/newcomers/reassign", hrSID, map[string]any{"newcomer_ids": []string{n.ID.String()}, "recruiter_id": id}, 200)
	if upd["updated"] != float64(1) {
		t.Fatalf("reassign: %v", upd)
	}
	mine, _ := a.do("GET", "/api/newcomers?mine=1", petrSID, nil, 200)
	if itemsLen(mine) != 1 {
		t.Fatalf("mine: %v", mine)
	}
	a.do("PATCH", "/api/newcomers/"+n.ID.String(), hrSID, map[string]any{"recruiter_id": nil}, 200)
	hd, _ := a.do("GET", "/api/hrlead/dashboard", hrSID, nil, 200)
	for _, k := range services.HRLeadTiles {
		if _, ok := hd["tiles"].(map[string]any)[k]; !ok {
			t.Fatalf("нет плитки %s: %v", k, hd)
		}
	}
	if load, _ := hd["load"].([]any); len(load) != 1 {
		t.Fatalf("load — активные рекрутеры: %v", hd["load"])
	}
	a.do("GET", "/api/newcomers/"+n.ID.String()+"/files", hrSID, nil, 200)
	t.Setenv("APP_ENV", "development")
	if m, _ := a.do("POST", "/api/dev/recruiter-morning", hrSID, nil, 200); m["total"] == nil {
		t.Fatalf("recruiter-morning: %v", m)
	}
	t.Setenv("APP_ENV", "production")
	a.do("POST", "/api/dev/recruiter-morning", hrSID, nil, 404)

	a.do("PATCH", "/api/users/"+hr.ID.String(), hrSID, map[string]any{"active": false}, 409)
	a.do("PATCH", "/api/users/"+id, hrSID, map[string]any{"active": false}, 200)
	a.do("GET", "/api/auth/me", petrSID, nil, 401)
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "petr", "password": "password-petr"}, 401)
}
