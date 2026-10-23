package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"main/out"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

type stubPinger struct{ err error }

func (p stubPinger) Ping(context.Context) error { return p.err }

type stubRAG struct{ err error }

func (r stubRAG) Health(context.Context, time.Duration) error { return r.err }

func newTestApp(db, rag error) *fiber.App {
	svc := &services.Services{Health: services.NewHealthService(stubPinger{db}, stubRAG{rag}, time.Second)}
	h := NewHandlerManager(svc)
	app := fiber.New(ServerConfig(DefaultTimeouts))
	app.Use(RequestID)
	app.Get("/api/health", h.Health)
	return app
}

func TestHealthShape(t *testing.T) {
	app := newTestApp(nil, errors.New("rag лежит"))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/health", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("статус %d: health отвечает 200 и при лежащем rag", resp.StatusCode)
	}
	var got out.Health
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != (out.Health{Status: "ok", DB: "ok", RAG: "down"}) {
		t.Fatalf("тело %+v", got)
	}
}

func TestErrorEnvelope(t *testing.T) {
	app := newTestApp(nil, nil)
	resp, err := app.Test(httptest.NewRequest("GET", "/api/nope", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 404 {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	var env out.ErrorBody
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("не конверт: %s", body)
	}
	if env.Error.Code != "NOT_FOUND" || env.Error.Message == "" || env.Error.TraceID == "" {
		t.Fatalf("конверт %+v", env)
	}
	if env.Error.TraceID != resp.Header.Get("X-Request-Id") {
		t.Fatalf("traceId %q не совпадает с X-Request-Id %q", env.Error.TraceID, resp.Header.Get("X-Request-Id"))
	}
}
