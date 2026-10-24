package handlers

import (
	"testing"

	"main/domain"
)

// Исполнитель обращения при FEATURE_AUTH: «Взять» без тела — на вошедшего, назначение по
// assignee_id, список исполнителей открыт рекрутеру, mine — по assignee_id.
func TestAuthTicketAssignee(t *testing.T) {
	a := newAuthAPI(t, true)
	hr := a.user("anna", domain.RoleHRLead, nil)
	r1 := a.user("maria", domain.RoleRecruiter, nil)
	a.user("smirnov", domain.RoleManager, nil)
	emp := a.newcomer("Новичок (синтетика)", "Рук А", &r1.ID)
	tk := a.ticket(emp.ID)
	path := "/api/tickets/" + tk.ID.String() + "/assign"

	mariaSID := a.login("maria")
	list, _ := a.do("GET", "/api/tickets/assignees", mariaSID, nil, 200)
	if itemsLen(list) != 2 {
		t.Fatalf("исполнители — hr_lead и recruiter: %v", list)
	}

	// «Взять» без тела — на себя.
	got, _ := a.do("POST", path, mariaSID, nil, 200)
	if got["assignee_id"] != r1.ID.String() || got["assignee_name"] != r1.FullName {
		t.Fatalf("взять: %v", got)
	}
	mine, _ := a.do("GET", "/api/tickets?mine=1", mariaSID, nil, 200)
	if itemsLen(mine) != 1 {
		t.Fatalf("mine: %v", mine)
	}

	// hr_lead переназначает на себя по id — с комментарием.
	hrSID := a.login("anna")
	a.do("POST", path, hrSID, map[string]any{"assignee_id": hr.ID.String()}, 422)
	got, _ = a.do("POST", path, hrSID, map[string]any{"assignee_id": hr.ID.String(), "comment": "беру сам"}, 200)
	if got["assignee_id"] != hr.ID.String() || got["assignee_name"] != hr.FullName {
		t.Fatalf("по id: %v", got)
	}
	if mine, _ := a.do("GET", "/api/tickets?mine=1", mariaSID, nil, 200); itemsLen(mine) != 0 {
		t.Fatalf("mine после переназначения: %v", mine)
	}
	a.do("POST", path, hrSID, map[string]any{"assignee_id": "не-uuid"}, 422)
}

// Без FEATURE_AUTH — как раньше: assignee_name обязателен, assignee_id в ответе null.
func TestTicketAssignNoAuth(t *testing.T) {
	a := newAuthAPI(t, false)
	emp := a.newcomer("Новичок (синтетика)", "Рук А", nil)
	tk := a.ticket(emp.ID)
	path := "/api/tickets/" + tk.ID.String() + "/assign"
	a.do("POST", path, "", nil, 422)
	got, _ := a.do("POST", path, "", map[string]any{"assignee_name": "Анна К."}, 200)
	if got["assignee_name"] != "Анна К." || got["assignee_id"] != nil {
		t.Fatalf("без входа: %v", got)
	}
}
