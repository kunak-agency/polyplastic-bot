package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"main/config"
	"main/externalApi"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/repositories"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// База знаний через таблицу маршрутов (api_contract.md, «v3 — база знаний»): права HR, проверки
// формы загрузки до rag, прокси в rag (httptest-подделка), версии, проверочный вопрос.

type docsRAG struct {
	mu    sync.Mutex
	docs  map[string]map[string]any
	calls int
}

func newDocsAPI(t *testing.T, token string) (*api, *docsRAG) {
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
	ctx := context.Background()
	if err := repos.Truncate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repos.Documents.TruncateKB(ctx); err != nil {
		t.Fatal(err)
	}
	f := &docsRAG{docs: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/documents":
			items := []any{}
			for _, d := range f.docs {
				items = append(items, d)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
		case r.Method == http.MethodPost && r.URL.Path == "/documents":
			f.calls++
			doc := r.FormValue("doc")
			_, fh, _ := r.FormFile("file")
			id := "doc" + string(rune('0'+f.calls))
			f.docs[doc] = map[string]any{"doc_id": id, "doc": doc, "filename": fh.Filename, "kind": "pdf", "source": "upload",
				"chunks": 1, "clauses": 2, "low_confidence_pages": []int{}, "uploaded_at": "2026-10-24T08:00:00+00:00"}
			_ = json.NewEncoder(w).Encode(map[string]any{"doc_id": id, "chunks": 1, "clauses": 2, "low_confidence_pages": []int{}})
		case r.URL.Path == "/ask":
			_, _ = w.Write([]byte(`{"answer":"Пропуск выдаёт бюро пропусков в день выхода.","sources":[{"doc":"Памятка (синтетика)","clause_id":"2.1","page":1,"quote":"Пропуск выдаёт бюро пропусков в день выхода."}],"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	fake := max.NewFake()
	svc := services.NewServices(repos, &externalApi.ExternalAPI{Max: fake, RAG: rag.New(srv.URL, time.Second)}, services.Options{
		Bot:  config.Bot{Username: "korsik_test_bot", InviteLinkFormat: config.DefaultInviteLinkFormat},
		Docs: services.DocsOptions{MaxUploadMB: 1, UploadTimeout: 2 * time.Second, OperatorToken: token},
	})
	app := fiber.New(ServerConfig(DefaultTimeouts))
	app.Use(RequestID)
	RegisterRoutes(app, NewHandlerManager(svc))
	return &api{t: t, app: app, svc: svc, fake: fake}, f
}

// docForm — multipart POST /api/documents.
func docForm(filename string, content []byte, fields map[string]string, header map[string]string) *http.Request {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	req := httptest.NewRequest("POST", "/api/documents", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return req
}

// Вне development без X-Operator-Token — 403 на всех маршрутах раздела; с токеном — пускает.
// Неизвестный путь вне development остаётся 404.
func TestDocumentsAccess(t *testing.T) {
	a, _ := newDocsAPI(t, "s3cret-operator")
	t.Setenv("APP_ENV", "production")
	a.json("GET", "/api/documents", nil, 403)
	a.json("POST", "/api/documents/ask", map[string]any{"question": "где пропуск"}, 403)
	a.json("GET", "/api/knowledge/unanswered", nil, 403)
	a.call(docForm("memo.pdf", []byte("%PDF-1.4"), nil, nil), 403)
	a.call(docForm("memo.pdf", []byte("%PDF-1.4"), nil, map[string]string{HeaderOperatorToken: "wrong"}), 403)
	a.json("GET", "/api/nothing-here", nil, 404)

	req := httptest.NewRequest("GET", "/api/documents", nil)
	req.Header.Set(HeaderOperatorToken, "s3cret-operator")
	if l := a.call(req, 200); l["rag"] != "ok" {
		t.Fatalf("список с токеном: %v", l)
	}

	t.Setenv("APP_ENV", "development")
	a.json("GET", "/api/documents", nil, 200)
}

// Пустой KB_OPERATOR_TOKEN не открывает раздел пустым заголовком.
func TestDocumentsEmptyToken(t *testing.T) {
	a, _ := newDocsAPI(t, "")
	t.Setenv("APP_ENV", "production")
	req := httptest.NewRequest("GET", "/api/documents", nil)
	req.Header.Set(HeaderOperatorToken, "")
	a.call(req, 403)
}

// Загрузка: проверки формы до rag (формат, размер, подмена расширения, площадка), прокси в rag,
// версии, проверочный вопрос с цитатой.
func TestDocumentsUploadFlow(t *testing.T) {
	a, f := newDocsAPI(t, "")
	t.Setenv("APP_ENV", "development")

	a.call(docForm("photo.jpg", []byte("JFIF"), nil, nil), 422)
	a.call(docForm("memo.pdf", []byte("PK\x03\x04 это zip"), nil, nil), 422)                   // расширение врёт
	a.call(docForm("big.pdf", append([]byte("%PDF-"), make([]byte, 1<<20)...), nil, nil), 422) // > 1 МБ
	a.call(docForm("memo.pdf", []byte("%PDF-1.4"), map[string]string{"sites": "Марс"}, nil), 422)
	if f.calls != 0 {
		t.Fatalf("отказы формы дошли до rag: %d", f.calls)
	}

	d := a.call(docForm("memo.pdf", []byte("%PDF-1.4 синтетика"), map[string]string{
		"doc": "Памятка (синтетика)", "sites": "GTP,GrPP", "uploaded_by": "Анна HR (синтетика)",
	}, nil), 201)
	if d["version"] != 1.0 || d["status"] != "ready" || d["indexed"] != true || d["clauses"] != 2.0 || d["filename"] != "memo.pdf" ||
		d["uploaded_by"] != "Анна HR (синтетика)" || len(d["sites"].([]any)) != 2 || d["source"] != "upload" {
		t.Fatalf("документ %v", d)
	}
	id := d["id"].(string)
	d2 := a.call(docForm("memo-v2.pdf", []byte("%PDF-1.4 синтетика v2"), map[string]string{"doc": "Памятка (синтетика)"}, nil), 201)
	if d2["id"] != id || d2["version"] != 2.0 {
		t.Fatalf("v2 %v", d2)
	}
	// Без doc — имя файла без расширения.
	d3 := a.call(docForm("Охрана труда.pdf", []byte("%PDF-1.4"), nil, nil), 201)
	if d3["doc"] != "Охрана труда" {
		t.Fatalf("имя по файлу %v", d3)
	}

	l := a.json("GET", "/api/documents", nil, 200)
	if l["total"] != 2.0 || l["rag"] != "ok" {
		t.Fatalf("список %v", l)
	}
	v := a.json("GET", "/api/documents/"+id+"/versions", nil, 200)
	vs := items(t, v)
	if len(vs) != 2 || vs[0].(map[string]any)["current"] != true || vs[1].(map[string]any)["filename"] != "memo.pdf" {
		t.Fatalf("версии %v", v)
	}
	a.json("GET", "/api/documents/не-uuid/versions", nil, 422)
	a.json("GET", "/api/documents/0199a000-0000-7000-8000-00000000ffff/versions", nil, 404)

	a.json("POST", "/api/documents/ask", map[string]any{"question": "?"}, 422)
	r := a.json("POST", "/api/documents/ask", map[string]any{"question": "где получить пропуск", "doc": "Памятка (синтетика)"}, 200)
	src := r["sources"].([]any)
	if r["answer"] == nil || len(src) != 1 || src[0].(map[string]any)["clause_id"] != "2.1" || r["from_doc"] != true {
		t.Fatalf("ответ %v", r)
	}

	u := a.json("GET", "/api/knowledge/unanswered?days=7", nil, 200)
	if u["total"] != 0.0 || u["questions"] != 0.0 {
		t.Fatalf("без отказов %v", u)
	}
	a.json("GET", "/api/knowledge/unanswered?days=0", nil, 422)
}
