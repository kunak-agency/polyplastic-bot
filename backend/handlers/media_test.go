package handlers

import (
	"context"
	"os"

	"main/repositories"

	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Медиатека через таблицу маршрутов (api_contract.md, «v4»): права HR, проверка формата по
// сигнатуре и размера, площадки, content, ensure, переименование, удаление.

func newMediaAPI(t *testing.T) *api {
	t.Helper()
	a, _ := newDocsAPI(t, "s3cret-operator")
	repos, err := repositories.InitRepos(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.Media.TruncateMedia(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

func mediaForm(method, path, filename string, content []byte, fields map[string]string) *http.Request {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", filename)
	_, _ = fw.Write(content)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

var (
	pngData = []byte("\x89PNG\r\n\x1a\n синтетика")
	pdfData = []byte("%PDF-1.4 синтетика")
)

func TestMediaAccess(t *testing.T) {
	a := newMediaAPI(t)
	t.Setenv("APP_ENV", "production")
	a.json("GET", "/api/media", nil, 403)
	a.json("POST", "/api/media/ensure", map[string]any{"names": []string{"x"}}, 403)
	a.call(mediaForm("POST", "/api/media", "a.png", pngData, nil), 403)
	req := httptest.NewRequest("GET", "/api/media", nil)
	req.Header.Set(HeaderOperatorToken, "s3cret-operator")
	a.call(req, 200)
}

func TestMediaFlow(t *testing.T) {
	a := newMediaAPI(t)
	t.Setenv("APP_ENV", "development")

	a.call(mediaForm("POST", "/api/media", "a.png", pdfData, nil), 422)      // сигнатура не PNG
	a.call(mediaForm("POST", "/api/media", "a.exe", []byte("MZ"), nil), 422) // формат
	a.call(mediaForm("POST", "/api/media", "a.png", []byte{}, nil), 422)     // пустой
	a.call(mediaForm("POST", "/api/media", "a.png", pngData, map[string]string{"site": "Марс"}), 422)

	m := a.call(mediaForm("POST", "/api/media", "Карта ГТП.png", pngData, map[string]string{"updated_by": "Анна HR (синтетика)"}), 201)
	if m["name"] != "Карта ГТП" || m["kind"] != "image" || m["status"] != "ready" || m["used_in"] != 0.0 {
		t.Fatalf("создание %v", m)
	}
	files := m["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["site"] != nil || files[0].(map[string]any)["mime"] != "image/png" {
		t.Fatalf("файлы %v", files)
	}
	id := m["id"].(string)
	a.call(mediaForm("POST", "/api/media", "x.png", pngData, map[string]string{"name": "карта гтп"}), 409)

	p := a.call(mediaForm("PUT", "/api/media/"+id, "gtp.png", pngData, map[string]string{"site": "GTP"}), 200)
	if fs := p["files"].([]any); len(fs) != 2 || fs[1].(map[string]any)["site"] != "GTP" {
		t.Fatalf("PUT площадки %v", p)
	}
	a.call(mediaForm("PUT", "/api/media/"+id, "memo.pdf", pdfData, map[string]string{"site": "GrPP"}), 422) // вид

	resp, err := a.app.Test(httptest.NewRequest("GET", "/api/media/"+id+"/content?site=Luzhniki", nil), 5000)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngData) {
		t.Fatalf("content %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	e := a.json("POST", "/api/media/ensure", map[string]any{"names": []string{"Карта ГТП", "Изображение 14", "изображение 14"}}, 200)
	if e["created"] != 1.0 || e["total"] != 2.0 {
		t.Fatalf("ensure %v", e)
	}
	stub := e["items"].([]any)[1].(map[string]any)
	if stub["status"] != "missing" || len(stub["files"].([]any)) != 0 {
		t.Fatalf("заглушка %v", stub)
	}
	l := a.json("GET", "/api/media?status=missing", nil, 200)
	if l["total"] != 1.0 {
		t.Fatalf("фильтр %v", l)
	}
	a.json("GET", "/api/media?status=другое", nil, 422)

	r := a.json("PATCH", "/api/media/"+id, map[string]any{"name": "Схема проезда"}, 200)
	if r["name"] != "Схема проезда" || r["updated_refs"] != 0.0 {
		t.Fatalf("rename %v", r)
	}
	a.json("PATCH", "/api/media/"+id, map[string]any{"name": " "}, 422)
	a.json("PATCH", "/api/media/"+id, map[string]any{"name": "Изображение 14"}, 409)

	a.json("DELETE", "/api/media/"+id+"/files/GTP", nil, 200)
	a.json("DELETE", "/api/media/"+id+"/files/GTP", nil, 404)
	a.json("DELETE", "/api/media/"+id, nil, 204)
	a.json("GET", "/api/media/"+id, nil, 404)
	a.json("GET", "/api/media/не-uuid", nil, 422)
}
