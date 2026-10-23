package handlers

import (
	"strings"
	"testing"

	"main/externalApi"
	"main/externalApi/max"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// Демо-чат (api_contract.md, раздел «dev»): лента диалога из журнала подделки MAX и нажатия за
// новичка. 404 вне development и когда бот не на подделке.
func TestDevChat(t *testing.T) {
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Чатов Пётр (синтетика)", "site": "GTP", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)

	t.Setenv("APP_ENV", "production")
	a.json("GET", "/api/dev/chat/"+nid, nil, 404)
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": "x", "payload": "h"}, 404)

	t.Setenv("APP_ENV", "development")
	a.json("GET", "/api/dev/chat/нет", nil, 422)
	a.json("GET", "/api/dev/chat/0199a000-0000-7000-8000-00000000ffff", nil, 404)
	if c := a.json("GET", "/api/dev/chat/"+nid, nil, 200); len(c["messages"].([]any)) != 0 || c["typing"] != false {
		t.Fatalf("до подключения: %v", c)
	}
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": "mid.fake.1", "payload": "a:ty"}, 409)

	// Только «Начать» — знакомство с кнопками «ты/вы».
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid}, 200)
	msgs := a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("после «Начать»: %v", msgs)
	}
	start, intro := msgs[0].(map[string]any), msgs[1].(map[string]any)
	if start["from"] != "newcomer" || start["kind"] != "start" || intro["from"] != "bot" || intro["format"] != "markdown" {
		t.Fatalf("старт: %v / %v", start, intro)
	}
	row := intro["buttons"].([]any)[0].([]any)
	ty := row[0].(map[string]any)
	if ty["type"] != "callback" || ty["payload"] != "a:ty" {
		t.Fatalf("кнопка «ты»: %v", ty)
	}

	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": intro["id"], "payload": ""}, 422)
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": "mid.нет", "payload": "a:ty"}, 404)
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": intro["id"], "payload": "a:ty"}, 200)
	msgs = a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	if e := msgs[1].(map[string]any); e["edited"] != true || msgs[2].(map[string]any)["text"] != "На «ты»" {
		t.Fatalf("нажатие «ты» из ленты: %v", msgs)
	}
}

// Нажатие через ленту правит то сообщение, на котором кнопка; simulate-start с ответами — тоже.
func TestDevChatPress(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Кнопкин Илья (синтетика)", "site": "GTP", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid, "address_form": "vy", "consent": "given"}, 200)
	a.svc.Bot.Wait()

	msgs := a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	intro := msgs[1].(map[string]any)
	if intro["edited"] != true || len(intro["buttons"].([]any)) != 0 {
		t.Fatalf("знакомство не исправлено нажатием «вы»: %v", intro)
	}
	press := msgs[2].(map[string]any)
	if press["from"] != "newcomer" || press["kind"] != "press" || press["text"] != "На «вы»" || press["reply_to"] != intro["id"] {
		t.Fatalf("нажатие «вы»: %v", press)
	}

	// Текст через simulate-message — в ленте как входящее.
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "text": "привет"}, 200)
	msgs = a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	found := false
	var withHuman map[string]any
	for _, x := range msgs {
		m := x.(map[string]any)
		if m["from"] == "newcomer" && m["kind"] == "text" && m["text"] == "привет" {
			found = true
		}
		if m["from"] == "bot" && hasPayloadPrefix(m, "qh:") {
			withHuman = m
		}
	}
	if !found {
		t.Fatalf("текст новичка не в ленте: %v", msgs)
	}
	if withHuman == nil {
		t.Fatalf("нет сообщения с «Передать HR»: %v", msgs)
	}
	handoff := withHuman["buttons"].([]any)[0].([]any)[0].(map[string]any)["payload"]
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": "mid.fake.999999", "payload": "h"}, 404)
	a.json("POST", "/api/dev/chat/"+nid+"/press", map[string]any{"message_id": withHuman["id"], "payload": handoff}, 200)
	after := a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	if len(after) <= len(msgs) {
		t.Fatalf("после нажатия лента не выросла: %v", after)
	}
	if len(items(t, a.json("GET", "/api/tickets", nil, 200))) == 0 {
		t.Fatal("«Передать HR» не открыл обращение")
	}
}

func hasPayloadPrefix(m map[string]any, prefix string) bool {
	for _, r := range m["buttons"].([]any) {
		for _, b := range r.([]any) {
			if p, _ := b.(map[string]any)["payload"].(string); strings.HasPrefix(p, prefix) {
				return true
			}
		}
	}
	return false
}

// notFake — клиент MAX, который не подделка (как настоящий SDK): журнала нет.
type notFake struct{ *max.Fake }

func TestDevChatNotOnFake(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	if testing.Short() {
		t.Skip()
	}
	svc := services.NewServices(nil, &externalApi.ExternalAPI{Max: notFake{max.NewFake()}}, services.Options{})
	app := fiber.New(ServerConfig(DefaultTimeouts))
	RegisterRoutes(app, NewHandlerManager(svc))
	b := &api{t: t, app: app, svc: svc}
	b.json("GET", "/api/dev/chat/0199a000-0000-7000-8000-00000000ffff", nil, 404)
	b.json("POST", "/api/dev/chat/0199a000-0000-7000-8000-00000000ffff/press", map[string]any{"message_id": "m", "payload": "h"}, 404)
}
