package handlers

import (
	"context"
	"testing"

	"main/domain"
	"main/repositories"
	"main/services"

	"github.com/google/uuid"
)

// Справочник людей (api_contract_auth.md, «Люди»): руководитель видит подчинённых по
// manager_id, вход без пароля отвергается, права POST /api/people, перенос строк идемпотентен.

// staff — сотрудник с руководителем из справочника (managerID) и строкой имени (name).
func (a *authAPI) staff(fullName, name string, managerID *uuid.UUID) domain.Employee {
	a.t.Helper()
	e := domain.Employee{FullName: fullName, Site: domain.SiteGTP, ManagerName: name, ManagerID: managerID,
		StartDate: a.now.AddDate(0, 0, -2), Connection: domain.ConnInvited, Risk: domain.RiskGreen, ScenarioID: uuid.New()}
	if err := a.repos.Employees.Create(context.Background(), &e); err != nil {
		a.t.Fatal(err)
	}
	return e
}

func managerItems(t *testing.T, m map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	items, _ := m["items"].([]any)
	for _, it := range items {
		out[it.(map[string]any)["full_name"].(string)] = true
	}
	return out
}

func TestPeopleManagerSeesByID(t *testing.T) {
	a := newAuthAPI(t, true)
	ctx := context.Background()
	a.user("anna", domain.RoleHRLead, nil)
	boss := a.user("boss", domain.RoleManager, nil) // full_name «Синтетик boss», без manager_name
	other := a.user("other", domain.RoleManager, nil)
	byID := a.staff("По id (синтетика)", "Опечатка Босс", &boss.ID)
	a.staff("Тёзка чужого (синтетика)", "Синтетик boss", &other.ID)
	a.staff("Старая запись (синтетика)", "Синтетик boss", nil)
	a.staff("Чужая старая (синтетика)", "Кто-то ещё", nil)
	sid := a.login("boss")

	got, _ := a.do("GET", "/api/manager/newcomers", sid, nil, 200)
	seen := managerItems(t, got)
	if len(seen) != 2 || !seen["По id (синтетика)"] || !seen["Старая запись (синтетика)"] {
		t.Fatalf("руководитель видит по manager_id и старые записи по имени, не тёзку чужого: %v", seen)
	}

	// Задача «Встреча 1:1»: своя по manager_id — можно, чужая (тёзка) — 404.
	mine := &domain.ManagerNudge{EmployeeID: byID.ID, Kind: domain.NudgeMonth, Key: "t:mine:" + uuid.NewString(), Text: "1:1", Status: domain.NudgeOpen}
	if _, err := a.repos.Manager().CreateNudge(ctx, mine); err != nil {
		t.Fatal(err)
	}
	a.do("POST", "/api/manager/tasks/"+mine.ID.String(), sid, map[string]string{"status": "done"}, 204)

	// Переименование руководителя: доступ по manager_id остаётся, копия имени в карточке — новая.
	hrSID := a.login("anna")
	a.do("PATCH", "/api/users/"+boss.ID.String(), hrSID, map[string]any{"full_name": "Иванов И. (синтетика)"}, 200)
	got, _ = a.do("GET", "/api/manager/newcomers", sid, nil, 200)
	if seen = managerItems(t, got); !seen["По id (синтетика)"] {
		t.Fatalf("после переименования подчинённый по manager_id пропал: %v", seen)
	}
	e, _ := a.repos.Employees.ByID(ctx, byID.ID)
	if e.ManagerName != "Иванов И. (синтетика)" {
		t.Fatalf("manager_name карточки не синхронизирован: %q", e.ManagerName)
	}
}

func TestPeopleLoginWithoutPassword(t *testing.T) {
	a := newAuthAPI(t, true)
	ctx := context.Background()
	a.user("anna", domain.RoleHRLead, nil)
	hrSID := a.login("anna")

	// Логин есть, пароля нет — тот же отказ, что неверный пароль.
	login := "nopass"
	if err := a.repos.Users.Create(ctx, &domain.User{Login: &login, FullName: "Без пароля", Role: domain.RoleManager, Active: true}); err != nil {
		t.Fatal(err)
	}
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "nopass", "password": "anything-long"}, 401)
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "nopass", "password": ""}, 422)

	// Наставник не входит, даже если пароль каким-то образом задан.
	hash, _ := services.HashPassword("password-mentor")
	ml := "mentor1"
	if err := a.repos.Users.Create(ctx, &domain.User{Login: &ml, PasswordHash: hash, FullName: "Наставник", Role: domain.RoleMentor, Active: true}); err != nil {
		t.Fatal(err)
	}
	a.do("POST", "/api/auth/login", "", map[string]string{"login": "mentor1", "password": "password-mentor"}, 401)

	// Руководитель без входа → «выдать вход»: без пароля — 422, с паролем — входит.
	p, _ := a.do("POST", "/api/people", hrSID, map[string]any{"full_name": "Петров П. (синтетика)", "role": "manager"}, 201)
	id := p["id"].(string)
	if p["has_login"] != false {
		t.Fatalf("без входа: %v", p)
	}
	a.do("PATCH", "/api/users/"+id, hrSID, map[string]any{"login": "petrov"}, 422)
	u, _ := a.do("PATCH", "/api/users/"+id, hrSID, map[string]any{"login": "petrov", "password": "password-petrov"}, 200)
	if u["has_login"] != true || u["login"] != "petrov" {
		t.Fatalf("вход не выдан: %v", u)
	}
	a.login("petrov")
}

func TestPeopleCreateRights(t *testing.T) {
	a := newAuthAPI(t, true)
	a.user("anna", domain.RoleHRLead, nil)
	a.user("rita", domain.RoleRecruiter, nil)
	a.user("boss", domain.RoleManager, nil)
	hrSID, recSID, bossSID := a.login("anna"), a.login("rita"), a.login("boss")

	first, _ := a.do("POST", "/api/people", recSID, map[string]any{"full_name": "Сидоров С. (синтетика)", "role": "manager"}, 201)
	again, _ := a.do("POST", "/api/people", recSID, map[string]any{"full_name": " сидоров с. (синтетика) ", "role": "manager"}, 200)
	if first["id"] != again["id"] {
		t.Fatalf("тот же человек по ФИО: %v / %v", first, again)
	}
	a.do("POST", "/api/people", recSID, map[string]any{"full_name": "Наставница Н. (синтетика)", "role": "mentor"}, 201)
	a.do("POST", "/api/people", recSID, map[string]any{"full_name": "Вход", "role": "manager", "login": "vhod", "password": "password-vhod"}, 403)
	a.do("POST", "/api/people", recSID, map[string]any{"full_name": "Рекрутер", "role": "recruiter"}, 422)
	a.do("POST", "/api/people", recSID, map[string]any{"full_name": "", "role": "manager"}, 422)
	a.do("POST", "/api/people", hrSID, map[string]any{"full_name": "Наставник со входом", "role": "mentor", "login": "m", "password": "password-m"}, 422)
	a.do("POST", "/api/people", bossSID, map[string]any{"full_name": "Кто-то", "role": "mentor"}, 403)
	a.do("GET", "/api/people", bossSID, nil, 403)

	withLogin, _ := a.do("POST", "/api/people", hrSID, map[string]any{"full_name": "Козлов К. (синтетика)", "role": "manager",
		"login": "kozlov", "password": "password-kozlov"}, 201)
	if withLogin["has_login"] != true {
		t.Fatalf("hr_lead создаёт со входом: %v", withLogin)
	}
	a.login("kozlov")

	list, _ := a.do("GET", "/api/people?role=manager&q=%D1%81%D0%B8%D0%B4", recSID, nil, 200) // q=сид
	if itemsLen(list) != 1 {
		t.Fatalf("поиск по подстроке: %v", list)
	}
	all, _ := a.do("GET", "/api/people?role=mentor", hrSID, nil, 200)
	if itemsLen(all) != 1 {
		t.Fatalf("наставники: %v", all)
	}
	a.do("GET", "/api/people?role=hr_lead", hrSID, nil, 422)
}

func TestPeopleNewcomerFields(t *testing.T) {
	a := newAuthAPI(t, true)
	ctx := context.Background()
	a.user("anna", domain.RoleHRLead, nil)
	rita := a.user("rita", domain.RoleRecruiter, nil)
	hrSID, recSID := a.login("anna"), a.login("rita")
	sc, err := a.svc.Scenarios.Create(ctx, "Сценарий (синтетика)", []string{domain.SiteGTP},
		[]domain.ScenarioStep{{DayOffset: 1, Time: "10:00", Kind: domain.KindText, Title: "Шаг", TextVy: "Текст", TextTy: "Текст"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.svc.Scenarios.Publish(ctx, sc.Scenario.ID, "тест", services.ApplyNew); err != nil {
		t.Fatal(err)
	}
	mgr, _ := a.do("POST", "/api/people", hrSID, map[string]any{"full_name": "Руководитель А. (синтетика)", "role": "manager"}, 201)

	// manager_id из справочника; наставник строкой — найдётся или создастся.
	n, _ := a.do("POST", "/api/newcomers", recSID, map[string]any{"full_name": "Сотрудник (синтетика)", "site": "GTP",
		"start_date": "2026-11-09", "scenario_id": sc.Scenario.ID.String(), "manager_id": mgr["id"], "manager_name": "что угодно",
		"mentor_name": "Наставник Б. (синтетика)"}, 201)
	if n["manager_id"] != mgr["id"] || n["manager_name"] != "Руководитель А. (синтетика)" || n["mentor_id"] == nil {
		t.Fatalf("POST /api/newcomers: %v", n)
	}
	id := n["id"].(string)
	// Совместимость: только строки — тот же человек по точному ФИО, без дублей.
	n2, _ := a.do("POST", "/api/newcomers", recSID, map[string]any{"full_name": "Второй (синтетика)", "site": "GTP",
		"start_date": "2026-11-09", "scenario_id": sc.Scenario.ID.String(), "manager_name": "Руководитель А. (синтетика)",
		"mentor_name": "Наставник Б. (синтетика)"}, 201)
	if n2["manager_id"] != mgr["id"] || n2["mentor_id"] != n["mentor_id"] {
		t.Fatalf("по имени — тот же человек: %v", n2)
	}
	a.do("POST", "/api/newcomers", recSID, map[string]any{"full_name": "Третий", "site": "GTP", "start_date": "2026-11-09",
		"scenario_id": sc.Scenario.ID.String(), "manager_id": rita.ID.String()}, 422) // не руководитель

	// PATCH: сменить наставника, снять руководителя; рекрутер — только свои.
	mentor2, _ := a.do("POST", "/api/people", recSID, map[string]any{"full_name": "Наставник В. (синтетика)", "role": "mentor"}, 201)
	p, _ := a.do("PATCH", "/api/newcomers/"+id, recSID, map[string]any{"mentor_id": mentor2["id"], "manager_id": nil}, 200)
	if p["mentor_id"] != mentor2["id"] || p["mentor_name"] != "Наставник В. (синтетика)" || p["manager_id"] != nil || p["manager_name"] != "" {
		t.Fatalf("PATCH people: %v", p)
	}
	a.do("PATCH", "/api/newcomers/"+id, recSID, map[string]any{"manager_id": "not-a-uuid"}, 422)
	a.do("PATCH", "/api/newcomers/"+id, recSID, map[string]any{"manager_id": mentor2["id"]}, 422)
	foreign := a.staff("Чужой (синтетика)", "", nil)
	a.do("PATCH", "/api/newcomers/"+foreign.ID.String(), recSID, map[string]any{"manager_id": mgr["id"]}, 404)
	a.do("PATCH", "/api/newcomers/"+foreign.ID.String(), hrSID, map[string]any{"manager_id": mgr["id"]}, 200)
}

func TestPeopleLinkIdempotent(t *testing.T) {
	a := newAuthAPI(t, false)
	ctx := context.Background()
	demo := "Руководитель Цех экструзии (синтетика)"
	acc := a.user("manager", domain.RoleManager, &demo) // full_name другой, связь по старому manager_name
	a.staff("Первый (синтетика)", demo, nil)
	a.staff("Второй (синтетика)", demo, nil)
	a.staff("Третий (синтетика)", "Новый Руководитель (синтетика)", nil)
	linked := a.staff("Уже связан (синтетика)", "Новый Руководитель (синтетика)", &acc.ID)
	m := a.staff("С наставником (синтетика)", "", nil)
	if err := a.repos.Employees.Save(ctx, func() *domain.Employee { m.MentorName = "Наставник Г. (синтетика)"; return &m }()); err != nil {
		t.Fatal(err)
	}

	r, err := a.svc.Users.LinkPeople(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.ManagersCreated != 1 || r.ManagersLinked != 3 || r.MentorsCreated != 1 || r.MentorsLinked != 1 {
		t.Fatalf("первый проход: %+v", r)
	}
	r, err = a.svc.Users.LinkPeople(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if *r != (services.PeopleLinkResult{}) {
		t.Fatalf("второй проход ничего не меняет: %+v", r)
	}
	list, _ := a.repos.Employees.List(ctx, repositories.EmployeeFilter{})
	for _, e := range list {
		switch {
		case e.ManagerName == demo && (e.ManagerID == nil || *e.ManagerID != acc.ID):
			t.Fatalf("демо-руководитель не связан: %+v", e)
		case e.ID == linked.ID && *e.ManagerID != acc.ID:
			t.Fatalf("связанная карточка перезаписана: %+v", e)
		case e.ManagerName != "" && e.ManagerID == nil, e.MentorName != "" && e.MentorID == nil:
			t.Fatalf("без связи: %+v", e)
		}
	}
}
