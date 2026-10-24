package rag

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Ключ уходит заголовком Bearer в каждом запросе к rag; без ключа заголовка нет вовсе.
func TestAPIKeyHeader(t *testing.T) {
	var (
		mu  sync.Mutex
		got = map[string]string{}
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.Method+" "+r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		switch r.URL.Path {
		case "/ask":
			_, _ = w.Write([]byte(`{"answer":"a","sources":[{"doc":"d","clause_id":"1","page":1,"quote":"q"}],
				"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`))
		case "/flags":
			_, _ = w.Write([]byte(`{"flags":[],"sentiment":"neu","quote":null,"degraded":false}`))
		case "/transcribe":
			_, _ = w.Write([]byte(`{"text":"t","lang":"ru","duration_s":1,"confidence":0.9}`))
		case "/documents":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"items":[],"total":0}`))
				return
			}
			_, _ = w.Write([]byte(`{"doc_id":"x","chunks":1,"clauses":1,"low_confidence_pages":[]}`))
		default:
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()

	call := func(c *Client) {
		t.Helper()
		ctx := context.Background()
		if err := c.Health(ctx, time.Second); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Ask(ctx, AskRequest{Question: "q"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Flags(ctx, FlagsRequest{Text: "x"}); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Transcribe(ctx, "v.ogg", []byte("OggS")); err != nil {
			t.Fatal(err)
		}
		if _, err := c.UploadDocument(ctx, "d", "d.txt", []byte("1. текст")); err != nil {
			t.Fatal(err)
		}
		if _, err := c.ListDocuments(ctx); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"GET /health", "POST /ask", "POST /flags", "POST /transcribe", "POST /documents", "GET /documents"}

	call(New(srv.URL, time.Second, WithAPIKey("s3cret")))
	for _, p := range paths {
		if got[p] != "Bearer s3cret" {
			t.Errorf("%s: Authorization %q", p, got[p])
		}
	}

	got = map[string]string{}
	call(New(srv.URL, time.Second, WithAPIKey("")))
	for _, p := range paths {
		if v, ok := got[p]; !ok || v != "" {
			t.Errorf("без ключа %s: Authorization %q (запрос был: %v)", p, v, ok)
		}
	}
}
