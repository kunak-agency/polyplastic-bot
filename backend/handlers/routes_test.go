package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"main/config"
	"main/externalApi"
	"main/externalApi/max"
	"main/repositories"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

// Тесты REST v1 по контракту (agency/system/api_contract.md) через настоящую таблицу маршрутов:
// Postgres из TEST_DATABASE_URL, MAX — подделка. Без базы пропускаются.

type api struct {
	t    *testing.T
	app  *fiber.App
	svc  *services.Services
	fake *max.Fake
}

func newAPI(t *testing.T) *api {
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
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, moscow) // понедельник
	fake := max.NewFake()
	svc := services.NewServices(repos, &externalApi.ExternalAPI{Max: fake}, services.Options{
		Bot: config.Bot{Username: "korsik_test_bot", InviteLinkFormat: config.DefaultInviteLinkFormat},
		Now: func() time.Time { return now },
	})
	app := fiber.New(ServerConfig(DefaultTimeouts))
	app.Use(RequestID)
	RegisterRoutes(app, NewHandlerManager(svc))
	return &api{t: t, app: app, svc: svc, fake: fake}
}

// call — запрос; ответ разбирается в map. Статус сверяется с want.
func (a *api) call(req *http.Request, want int) map[string]any {
	a.t.Helper()
	resp, err := a.app.Test(req, 10_000)
	if err != nil {
		a.t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		a.t.Fatalf("%s %s: статус %d, want %d: %s", req.Method, req.URL.Path, resp.StatusCode, want, body)
	}
	out := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			a.t.Fatalf("%s %s: не JSON: %s", req.Method, req.URL.Path, body)
		}
	}
	return out
}

func (a *api) json(method, path string, body any, want int) map[string]any {
	a.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	return a.call(req, want)
}

// upload — multipart с файлом в поле file.
func (a *api) upload(path, filename string, content []byte, name string, want int) map[string]any {
	a.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	if name != "" {
		_ = w.WriteField("name", name)
	}
	_ = w.Close()
	req := httptest.NewRequest("POST", path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return a.call(req, want)
}

// timelineXLSX — синтетический Excel с предварительными колонками контракта.
func timelineXLSX(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	rows := [][]any{
		{"Таймлайн адаптации (синтетика)"},
		{"Когда", "Повод", "Метка", "Макет", "Текст", "Обязательный", "Изображение"},
		{"D−3", "Документы", "GTP, GrPP", "текст", "Возьмите паспорт", "да", ""},
		{"D0 10:00", "Поздравление", "", "открытка", "С первым днём!", "да", "card.png"},
		{"D+14", "Чек", "Лужники", "опрос", "Как дела?", "нет", ""},
		{"", "", "", "", "", "", ""},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func items(t *testing.T, m map[string]any) []any {
	t.Helper()
	it, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("нет items: %v", m)
	}
	if total, _ := m["total"].(float64); int(total) != len(it) {
		t.Fatalf("total %v при %d элементах", m["total"], len(it))
	}
	return it
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func TestScenariosAPI(t *testing.T) {
	a := newAPI(t)

	imp := a.upload("/api/scenarios/import", "timeline.xlsx", timelineXLSX(t), "Импорт (синтетика)", 201)
	if imp["status"] != "draft" || imp["version"] != float64(0) || imp["name"] != "Импорт (синтетика)" {
		t.Fatalf("импорт: %v", imp)
	}
	steps := imp["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("шагов %d: %v", len(steps), steps)
	}
	first := steps[0].(map[string]any)
	if first["day_offset"] != float64(-3) || first["time"] != "10:00" || first["kind"] != "text" || len(first["sites"].([]any)) != 2 {
		t.Fatalf("шаг 1: %v", first)
	}
	if k := steps[1].(map[string]any)["kind"]; k != "card" {
		t.Fatalf("открытка → card: %v", k)
	}
	// Строка «опрос» получает опрос по умолчанию: 1–10, порог 8, вопрос из текста.
	sv, _ := steps[2].(map[string]any)["survey"].(map[string]any)
	if sv == nil || sv["scale"] != "1-10" || sv["threshold"] != float64(8) ||
		sv["questions"].([]any)[0].(map[string]any)["text"] != "Как дела?" {
		t.Fatalf("опрос по умолчанию: %v", steps[2])
	}
	a.upload("/api/scenarios/import", "old.xls", []byte("x"), "", 422)
	a.upload("/api/scenarios/import", "notes.txt", []byte("x"), "", 422)
	id := imp["id"].(string)

	// PUT steps: {"steps":[…]}, id нового шага от клиента сохраняется.
	clientID := "0199a000-0000-7000-8000-000000000001"
	put := a.json("PUT", "/api/scenarios/"+id+"/steps", map[string]any{"steps": []any{
		map[string]any{"id": clientID, "day_offset": -3, "time": "10:00", "stage": "before", "sites": []string{},
			"kind": "text", "title": "Знакомство", "text_ty": "Привет", "text_vy": "Здравствуйте",
			"buttons": []any{map[string]any{"label": "Ок", "action": ""}}, "survey": nil, "condition": nil, "work_hours_only": true},
		map[string]any{"day_offset": 14, "time": "10:00", "kind": "survey", "title": "Чек", "text_vy": "Как дела?",
			"survey": map[string]any{"scale": "1-10", "threshold": 8, "questions": []any{map[string]any{"text": "Задачи понятны?"}}}},
	}}, 200)
	ps := put["steps"].([]any)
	if put["status"] != "draft" || len(ps) != 2 || ps[0].(map[string]any)["id"] != clientID {
		t.Fatalf("PUT steps: %v", put)
	}
	if q := ps[1].(map[string]any)["survey"].(map[string]any)["questions"].([]any)[0].(map[string]any); q["options"] == nil {
		t.Fatalf("options — массив, а не null: %v", q)
	}
	bad := a.json("PUT", "/api/scenarios/"+id+"/steps", map[string]any{"steps": []any{map[string]any{"title": ""}}}, 422)
	if errCode(bad) != "VALIDATION_FAILED" || bad["error"].(map[string]any)["details"] == nil {
		t.Fatalf("валидация шагов: %v", bad)
	}

	// Без публикации запустить адаптацию нельзя: 409 scenario_not_published.
	body := map[string]any{"full_name": "Тестов Пётр (синтетика)", "phone": "+7 000 000-00-01", "site": "GTP",
		"department": "Цех", "position": "Оператор", "start_date": "2026-11-03", "shift": "1", "scenario_id": id}
	if r := a.json("POST", "/api/newcomers", body, 409); errCode(r) != services.CodeScenarioNotPublished {
		t.Fatalf("неопубликованный: %v", r)
	}

	pub := a.json("POST", "/api/scenarios/"+id+"/publish", map[string]any{"comment": "первая", "apply": "new"}, 200)
	if pub["version"] != float64(1) || pub["status"] != "published" || len(pub["steps"].([]any)) != 2 {
		t.Fatalf("публикация: %v", pub)
	}
	a.json("POST", "/api/scenarios/"+id+"/publish", map[string]any{"apply": "всем"}, 422)

	a.json("POST", "/api/newcomers", body, 201)
	list := items(t, a.json("GET", "/api/scenarios", nil, 200))
	if len(list) != 1 || list[0].(map[string]any)["active_newcomers"] != float64(1) {
		t.Fatalf("список сценариев: %v", list)
	}
	got := a.json("GET", "/api/scenarios/"+id, nil, 200)
	if _, ok := got["steps"]; !ok {
		t.Fatalf("карточка без steps: %v", got)
	}
	a.json("GET", "/api/scenarios/0199a000-0000-7000-8000-00000000ffff", nil, 404)
	a.json("GET", "/api/scenarios/не-uuid", nil, 422)
}

func TestNewcomersAPI(t *testing.T) {
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	id := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+id+"/publish", map[string]any{}, 200)

	if r := a.json("POST", "/api/newcomers", map[string]any{"site": "Марс", "start_date": "03.11.2026"}, 422); len(r["error"].(map[string]any)["details"].([]any)) < 3 {
		t.Fatalf("пополевые ошибки: %v", r)
	}
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Тестов Пётр (синтетика)", "phone": "+7 000 000-00-01", "site": "GTP", "department": "Цех экструзии",
		"position": "Оператор линии", "manager_name": "Рук (синтетика)", "mentor_name": "Наст (синтетика)",
		"recruiter_name": "Рекр (синтетика)", "start_date": "2026-11-03", "shift": 1, "scenario_id": id,
	}, 201)
	for _, k := range []string{"address_form", "consent", "last_activity_at", "last_pulse", "risk_reason"} {
		if v, ok := n[k]; !ok || v != nil {
			t.Fatalf("%s: want null, got %v (ok=%v)", k, v, ok)
		}
	}
	if n["connection"] != "invited" || n["risk"] != "green" || n["shift"] != "1" || n["scenario_version"] != float64(1) ||
		n["day"] != float64(-1) || n["stage"] != "before" || n["open_tickets"] != float64(0) || n["start_date"] != "2026-11-03" {
		t.Fatalf("карточка: %v", n)
	}
	link, _ := n["invite_link"].(string)
	if !strings.HasPrefix(link, "https://max.ru/korsik_test_bot?start=inv_") {
		t.Fatalf("invite_link %q", link)
	}
	nid := n["id"].(string)

	if l := items(t, a.json("GET", "/api/newcomers?site=GTP&q=тестов&risk=green", nil, 200)); len(l) != 1 {
		t.Fatalf("фильтр: %v", l)
	}
	if l := items(t, a.json("GET", "/api/newcomers?site=GrPP", nil, 200)); len(l) != 0 {
		t.Fatalf("фильтр площадки: %v", l)
	}
	a.json("GET", "/api/newcomers?risk=фиолетовый", nil, 422)
	a.json("GET", "/api/newcomers/"+nid, nil, 200)
	a.json("GET", "/api/newcomers/0199a000-0000-7000-8000-00000000ffff", nil, 404)

	tl := items(t, a.json("GET", "/api/newcomers/"+nid+"/timeline", nil, 200))
	if len(tl) != 2 { // шаг для Лужников новичку GTP не положен
		t.Fatalf("таймлайн: %v", tl)
	}
	ev := tl[0].(map[string]any)
	for _, k := range []string{"event_id", "step_id", "day_offset", "title", "kind", "fire_at", "status", "answer", "text"} {
		if _, ok := ev[k]; !ok {
			t.Fatalf("в событии нет %s: %v", k, ev)
		}
	}
	if ev["fire_at"] != "2026-11-02T10:00:00+03:00" || ev["status"] != "scheduled" {
		t.Fatalf("событие: %v", ev)
	}

	// Машина времени без подключения — 409; подключили через бота — уходит с [ТЕСТ].
	a.json("POST", "/api/newcomers/"+nid+"/time-travel", map[string]any{"day": 0}, 409)
	a.json("POST", "/api/newcomers/"+nid+"/time-travel", map[string]any{}, 422)
	token := link[strings.Index(link, "start=")+len("start="):]
	a.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateBotStarted, UserID: 700001, ChatID: 800001, Payload: token})
	tt := items(t, a.json("POST", "/api/newcomers/"+nid+"/time-travel", map[string]any{"day": 0}, 200))
	for _, x := range tt {
		e := x.(map[string]any)
		if e["status"] != "sent" || !strings.HasPrefix(e["text"].(string), "[ТЕСТ] ") {
			t.Fatalf("после машины времени: %v", e)
		}
	}
	if c := a.json("GET", "/api/newcomers/"+nid, nil, 200); c["day"] != float64(-1) || c["start_date"] != "2026-11-03" || c["connection"] != "connected" {
		t.Fatalf("машина времени не меняет day и start_date: %v", c)
	}

	// Перенос даты выхода (расширение контракта).
	p := a.json("PATCH", "/api/newcomers/"+nid, map[string]any{"start_date": "2026-11-10"}, 200)
	if p["start_date"] != "2026-11-10" {
		t.Fatalf("перенос: %v", p)
	}
}

// TestTicketsAPI — REST v2 «Обращения» и «Уточнения v2»: список и фильтры, карточка с перепиской,
// закрытые категории без цитаты и текста новичка, PATCH notify_manager, assign/reply/note/resolve.
func TestTicketsAPI(t *testing.T) {
	a := newAPI(t)
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Тестов Пётр (синтетика)", "phone": "+7 000 000-00-01", "site": "GTP", "department": "Цех",
		"position": "Оператор", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)
	link := n["invite_link"].(string)
	ctx := context.Background()
	const user = 700002
	bot := a.svc.Bot
	bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: user, ChatID: user, Payload: link[strings.Index(link, "start=")+len("start="):]})
	press := func(p string) {
		bot.Handle(ctx, max.Update{Type: max.UpdateMessageCallback, UserID: user, ChatID: user, Payload: p, CallbackID: "cb-" + p})
	}
	press("a:vy")
	press("c:yes")
	bot.Wait()
	press("h")                                                                                                            // «Спросить человека» → #1001
	bot.Handle(ctx, max.Update{Type: max.UpdateMessageCreated, UserID: user, ChatID: user, Text: "мастер просит взятку"}) // F9 → #1002
	bot.Wait()

	list := items(t, a.json("GET", "/api/tickets", nil, 200))
	if len(list) != 2 {
		t.Fatalf("список: %v", list)
	}
	f9, q := list[0].(map[string]any), list[1].(map[string]any)
	for _, k := range []string{"id", "number", "newcomer_id", "newcomer_name", "site", "day", "category", "priority", "status",
		"route", "assignee_name", "anonymous", "summary", "quote", "sla_due_at", "created_at", "updated_at", "notify_manager", "restricted"} {
		if _, ok := f9[k]; !ok {
			t.Fatalf("нет поля %s: %v", k, f9)
		}
	}
	if _, ok := f9["messages"]; ok {
		t.Fatal("messages в списке")
	}
	if f9["category"] != "F9" || f9["route"] != "security" || f9["restricted"] != true || f9["quote"] != nil ||
		f9["notify_manager"] != false || f9["number"] != float64(1002) || f9["day"] != float64(-1) || f9["anonymous"] != false {
		t.Fatalf("F9: %v", f9)
	}
	if q["category"] != "question" || q["route"] != "hr" || q["restricted"] != false || q["status"] != "open" || q["sla_due_at"] == nil {
		t.Fatalf("вопрос: %v", q)
	}
	// Фильтры.
	if l := items(t, a.json("GET", "/api/tickets?route=security&status=open,assigned", nil, 200)); len(l) != 1 {
		t.Fatalf("route: %v", l)
	}
	if l := items(t, a.json("GET", "/api/tickets?newcomer_id="+nid+"&site=GTP", nil, 200)); len(l) != 2 {
		t.Fatalf("newcomer_id: %v", l)
	}
	if l := items(t, a.json("GET", "/api/tickets?site=GrPP", nil, 200)); len(l) != 0 {
		t.Fatalf("site: %v", l)
	}
	a.json("GET", "/api/tickets?status=closed", nil, 422)
	a.json("GET", "/api/tickets?route=boss", nil, 422)
	a.json("GET", "/api/tickets?newcomer_id=x", nil, 422)
	if c := a.json("GET", "/api/newcomers/"+nid, nil, 200); c["open_tickets"] != float64(2) {
		t.Fatalf("open_tickets: %v", c["open_tickets"])
	}

	// Карточка закрытой: quote=null, текст новичка скрыт.
	card := a.json("GET", "/api/tickets/"+f9["id"].(string), nil, 200)
	if card["quote"] != nil {
		t.Fatalf("quote в карточке F9: %v", card["quote"])
	}
	hidden := false
	for _, x := range card["messages"].([]any) {
		m := x.(map[string]any)
		if m["from"] == "newcomer" {
			if m["text"] != nil || m["hidden"] != true {
				t.Fatalf("текст новичка открыт: %v", m)
			}
			hidden = true
		}
	}
	if !hidden {
		t.Fatalf("нет сообщения новичка: %v", card["messages"])
	}
	a.json("PATCH", "/api/tickets/"+f9["id"].(string), map[string]any{"notify_manager": true}, 422)
	a.json("GET", "/api/tickets/0199a000-0000-7000-8000-00000000ffff", nil, 404)
	a.json("GET", "/api/tickets/нет", nil, 422)

	// Вопрос: patch, assign, reply, note, resolve — каждое возвращает карточку с messages.
	qid := q["id"].(string)
	if p := a.json("PATCH", "/api/tickets/"+qid, map[string]any{"notify_manager": false}, 200); p["messages"] == nil {
		t.Fatalf("patch: %v", p)
	}
	a.json("PATCH", "/api/tickets/"+qid, map[string]any{}, 422)
	if r := a.json("POST", "/api/tickets/"+qid+"/reply", map[string]any{"text": "Здравствуйте"}, 409); errCode(r) != "ticket_not_assigned" {
		t.Fatalf("reply без assign: %v", r)
	}
	a.json("POST", "/api/tickets/"+qid+"/assign", map[string]any{}, 422)
	as := a.json("POST", "/api/tickets/"+qid+"/assign", map[string]any{"assignee_name": "Анна Смирнова"}, 200)
	if as["status"] != "assigned" || as["assignee_name"] != "Анна Смирнова" || as["messages"] == nil {
		t.Fatalf("assign: %v", as)
	}
	a.json("POST", "/api/tickets/"+qid+"/assign", map[string]any{"assignee_name": "Олег"}, 422)
	a.json("POST", "/api/tickets/"+qid+"/reply", map[string]any{"text": "  "}, 422)
	a.fake.Reset()
	rp := a.json("POST", "/api/tickets/"+qid+"/reply", map[string]any{"text": "Здравствуйте, Пётр! Это Анна из HR.", "resolve": false}, 200)
	if rp["status"] != "human_active" || rp["sla_due_at"] != nil {
		t.Fatalf("reply: %v", rp)
	}
	if s := a.fake.Sent(); len(s) != 1 || !strings.Contains(s[0].Text, "Анна Смирнова, HR") {
		t.Fatalf("в MAX: %+v", s)
	}
	nt := a.json("POST", "/api/tickets/"+qid+"/note", map[string]any{"text": "Проверить пропуск"}, 200)
	msgs := nt["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any); last["internal"] != true || last["from"] != "staff" {
		t.Fatalf("note: %v", last)
	}
	if r := a.json("POST", "/api/tickets/"+qid+"/resolve", nil, 200); r["status"] != "resolved" {
		t.Fatalf("resolve: %v", r)
	}
	a.json("POST", "/api/tickets/"+qid+"/resolve", nil, 409)
	a.json("POST", "/api/tickets/"+qid+"/reply", map[string]any{"text": "ещё"}, 409)
	a.json("POST", "/api/tickets/"+qid+"/assign", map[string]any{"assignee_name": "Анна Смирнова"}, 409)
	if c := a.json("GET", "/api/newcomers/"+nid, nil, 200); c["open_tickets"] != float64(1) {
		t.Fatalf("open_tickets после закрытия: %v", c["open_tickets"])
	}
}

// POST /api/scenarios: пустой черновик, копия опубликованной версии с новыми id шагов, 409 на
// занятое имя; DELETE — только черновик без публикаций и новичков.
func TestScenarioCreateDeleteAPI(t *testing.T) {
	a := newAPI(t)

	empty := a.json("POST", "/api/scenarios", map[string]any{"name": "  Офис (синтетика) ", "sites": []string{"GrPP", "GrPP"}}, 201)
	if empty["name"] != "Офис (синтетика)" || empty["version"] != float64(0) || empty["status"] != "draft" ||
		len(empty["steps"].([]any)) != 0 || len(empty["sites"].([]any)) != 1 {
		t.Fatalf("пустой: %v", empty)
	}
	if r := a.json("POST", "/api/scenarios", map[string]any{"name": "офис (СИНТЕТИКА)"}, 409); errCode(r) != services.CodeScenarioNameTaken {
		t.Fatalf("занятое имя: %v", r)
	}
	// Пустой редактор → «Загрузить Excel»: шаги файла ложатся в черновик этого сценария.
	into := a.upload("/api/scenarios/"+empty["id"].(string)+"/import", "t.xlsx", timelineXLSX(t), "", 200)
	if into["id"] != empty["id"] || len(into["steps"].([]any)) != 3 || into["name"] != "Офис (синтетика)" {
		t.Fatalf("импорт в черновик: %v", into)
	}
	a.upload("/api/scenarios/"+empty["id"].(string)+"/import", "old.xls", []byte("x"), "", 422)
	a.upload("/api/scenarios/0199a000-0000-7000-8000-00000000ffff/import", "t.xlsx", timelineXLSX(t), "", 404)
	a.json("PUT", "/api/scenarios/"+empty["id"].(string)+"/steps", map[string]any{"steps": []any{}}, 200)
	a.json("POST", "/api/scenarios", map[string]any{"name": " "}, 422)
	a.json("POST", "/api/scenarios", map[string]any{"name": "X", "sites": []string{"Марс"}}, 422)
	a.json("POST", "/api/scenarios", map[string]any{"name": "X", "from_scenario_id": "не-uuid"}, 422)
	a.json("POST", "/api/scenarios", map[string]any{"name": "X", "from_scenario_id": "0199a000-0000-7000-8000-00000000ffff"}, 422)

	// Источник: импорт → публикация v1 (3 шага) → правка черновика до 1 шага. Копия берёт v1.
	src := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := src["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{"comment": "v1"}, 200)
	a.json("PUT", "/api/scenarios/"+sid+"/steps", map[string]any{"steps": []any{
		map[string]any{"day_offset": 1, "title": "Только в черновике", "text_vy": "…"},
	}}, 200)
	srcIDs := map[string]bool{}
	for _, s := range src["steps"].([]any) {
		srcIDs[s.(map[string]any)["id"].(string)] = true
	}
	cp := a.json("POST", "/api/scenarios", map[string]any{"name": "Завод — копия", "sites": []string{"GTP"}, "from_scenario_id": sid}, 201)
	cs := cp["steps"].([]any)
	if cp["version"] != float64(0) || cp["status"] != "draft" || len(cs) != 3 {
		t.Fatalf("копия: %v", cp)
	}
	for i, s := range cs {
		st := s.(map[string]any)
		if srcIDs[st["id"].(string)] {
			t.Fatalf("шаг %d копии с id источника: %v", i, st)
		}
		if want := src["steps"].([]any)[i].(map[string]any)["title"]; st["title"] != want {
			t.Fatalf("шаг %d: %v, want %v", i, st["title"], want)
		}
	}
	// Копия публикуется отдельно от источника.
	pub := a.json("POST", "/api/scenarios/"+cp["id"].(string)+"/publish", map[string]any{"comment": "первая"}, 200)
	if pub["version"] != float64(1) {
		t.Fatalf("публикация копии: %v", pub)
	}
	// Копия неопубликованного: шаги черновика.
	cp2 := a.json("POST", "/api/scenarios", map[string]any{"name": "Копия пустого", "from_scenario_id": empty["id"]}, 201)
	if len(cp2["steps"].([]any)) != 0 {
		t.Fatalf("копия пустого: %v", cp2)
	}

	// DELETE: опубликованный — 409 (новички бывают только у опубликованных); черновик — 204, потом 404.
	if r := a.json("DELETE", "/api/scenarios/"+sid, nil, 409); errCode(r) != services.CodeScenarioNotDeletable {
		t.Fatalf("удаление опубликованного: %v", r)
	}
	a.json("DELETE", "/api/scenarios/"+cp2["id"].(string), nil, 204)
	a.json("GET", "/api/scenarios/"+cp2["id"].(string), nil, 404)
	a.json("DELETE", "/api/scenarios/"+cp2["id"].(string), nil, 404)
	if n := len(items(t, a.json("GET", "/api/scenarios", nil, 200))); n != 3 {
		t.Fatalf("после удаления сценариев %d", n)
	}
}
