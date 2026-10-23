package handlers

import (
	"testing"
)

// POST /api/dev/simulate-start (api_contract.md, раздел «dev»): только в development; играет
// за новичка открытие приглашения, знакомство и согласие — после этого машина времени не 409.
func TestDevSimulateStart(t *testing.T) {
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Девов Олег (синтетика)", "site": "GTP", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)
	body := map[string]any{"newcomer_id": nid, "address_form": "ty", "consent": "given"}

	t.Setenv("APP_ENV", "production")
	a.json("POST", "/api/dev/simulate-start", body, 404)

	t.Setenv("APP_ENV", "development")
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": "нет"}, 422)
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid, "consent": "может быть"}, 422)
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": "0199a000-0000-7000-8000-00000000ffff"}, 404)

	c := a.json("POST", "/api/dev/simulate-start", body, 200)
	if c["connection"] != "connected" || c["address_form"] != "ty" || c["consent"] != "given" {
		t.Fatalf("после simulate-start: %v", c)
	}
	a.svc.Bot.Wait() // пакет после согласия уходит в фоне
	// Повтор — тот же синтетический аккаунт: приглашение не «занято другим».
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid}, 200)
	a.json("POST", "/api/newcomers/"+nid+"/time-travel", map[string]any{"day": 0}, 200)
}

func TestInvitePayload(t *testing.T) {
	cases := map[string]string{
		"https://max.ru/bot?start=inv_abc-DEF_1": "inv_abc-DEF_1",
		"https://max.ru/bot/start/inv_abc":       "inv_abc",
		"https://max.ru/bot?start=inv_abc&utm=x": "inv_abc",
		"https://max.ru/bot?start=":              "",
	}
	for link, want := range cases {
		if got := invitePayload(link); got != want {
			t.Errorf("invitePayload(%q) = %q, want %q", link, got, want)
		}
	}
}

// POST /api/dev/simulate-message с flag — разметка /flags за новичка. Поправка оркестратора
// (24.10): F4 скрыт только от руководителя — HR видит цитату и переписку, restricted=false,
// PATCH notify_manager → 422; F9 по regex — restricted, текст скрыт.
func TestDevSimulateFlagVisibility(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Флагов Иван (синтетика)", "site": "GTP", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "text": "привет"}, 409)
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid, "address_form": "ty", "consent": "given"}, 200)
	a.svc.Bot.Wait()

	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "payload": "h", "flag": "F4"}, 422)
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "text": "x", "flag": "F42"}, 422)
	const said = "наставник третью смену орёт и ничего не объясняет"
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "text": said, "flag": "F4"}, 200)
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "text": "мастер предлагает откат"}, 200)

	byCat := map[string]map[string]any{}
	for _, x := range items(t, a.json("GET", "/api/tickets?newcomer_id="+nid, nil, 200)) {
		m := x.(map[string]any)
		byCat[m["category"].(string)] = m
	}
	f4, f9 := byCat["F4"], byCat["F9"]
	if f4 == nil || f9 == nil {
		t.Fatalf("обращения: %v", byCat)
	}
	if f4["restricted"] != false || f4["quote"] != said || f4["notify_manager"] != false || f4["route"] != "hr" {
		t.Fatalf("F4: %v", f4)
	}
	if f9["restricted"] != true || f9["quote"] != nil {
		t.Fatalf("F9: %v", f9)
	}
	seen := false
	for _, x := range a.json("GET", "/api/tickets/"+f4["id"].(string), nil, 200)["messages"].([]any) {
		m := x.(map[string]any)
		if m["from"] == "newcomer" {
			if m["hidden"] != false || m["text"] != said {
				t.Fatalf("F4: текст новичка скрыт: %v", m)
			}
			seen = true
		}
	}
	if !seen {
		t.Fatal("F4: нет сообщения новичка")
	}
	a.json("PATCH", "/api/tickets/"+f4["id"].(string), map[string]any{"notify_manager": true}, 422)
	a.json("PATCH", "/api/tickets/"+f4["id"].(string), map[string]any{"notify_manager": false}, 200)
}
