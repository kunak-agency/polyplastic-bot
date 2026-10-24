package services

import (
	"testing"

	"main/domain"

	"github.com/google/uuid"
)

// ManagerOwns: manager_id решает, строка имени — только для записей без manager_id.
func TestManagerOwns(t *testing.T) {
	legacy := "Руководитель Цеха"
	v := &domain.User{FullName: "Иванов И.", ManagerName: &legacy}
	v.ID = uuid.New()
	other := uuid.New()
	cases := []struct {
		name string
		e    domain.Employee
		want bool
	}{
		{"по id, имя с опечаткой", domain.Employee{ManagerID: &v.ID, ManagerName: "Ивонов"}, true},
		{"тёзка с чужим id", domain.Employee{ManagerID: &other, ManagerName: legacy}, false},
		{"старая запись по manager_name", domain.Employee{ManagerName: " " + legacy + " "}, true},
		{"старая запись, чужое имя", domain.Employee{ManagerName: "Иванов И."}, false},
		{"пусто", domain.Employee{}, false},
	}
	for _, c := range cases {
		if got := ManagerOwns(v, &c.e); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// Без users.manager_name запасное имя — full_name.
	plain := &domain.User{FullName: "Петров П."}
	if !ManagerOwns(plain, &domain.Employee{ManagerName: "Петров П."}) {
		t.Error("запасное имя — full_name")
	}
	if managerKey(&domain.Employee{ManagerID: &other, ManagerName: "X"}) == managerKey(&domain.Employee{ManagerName: "X"}) {
		t.Error("ключ лимитов: id и имя различаются")
	}
}
