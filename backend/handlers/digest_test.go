package handlers

import "testing"

// v4: дайджест и руководитель через таблицу маршрутов (api_contract.md, «v4»).
func TestDigestAndManagerRoutes(t *testing.T) {
	a := newAPI(t)
	d := a.json("GET", "/api/digest?week=2026-W44", nil, 200)
	if d["week"] != "2026-W44" || d["min_group"].(float64) != 5 {
		t.Fatalf("дайджест: %v", d)
	}
	msg := d["max_message"].(map[string]any)
	if msg["text"] == "" || msg["length"].(float64) > 4000 {
		t.Fatalf("сообщение MAX: %v", msg)
	}
	a.json("GET", "/api/digest?week=2026-44", nil, 422)
	a.json("GET", "/api/digest?site=Moon", nil, 422)
	a.json("GET", "/api/digest?week=2030-W01", nil, 422)

	a.json("GET", "/api/manager/newcomers", nil, 422)
	v := a.json("GET", "/api/manager/newcomers?name=%D0%9D%D0%B5%D1%82", nil, 200)
	if v["total"].(float64) != 0 {
		t.Fatalf("вид руководителя: %v", v)
	}
	a.json("GET", "/api/manager/managers", nil, 200)
	a.json("POST", "/api/newcomers/00000000-0000-0000-0000-000000000001/manager-invite", nil, 404)
}
