package handlers

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"main/externalApi/rag"
)

// simulate-message с голосовым (api_contract.md, «Голосовые»): файл уходит в подделку MAX, бот
// скачивает его по fake:// и распознаёт через rag /transcribe; в ленте — «🎙 Голосовое» и эхо.
func TestDevSimulateVoice(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	var mu sync.Mutex
	var heard [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/transcribe":
			f, _, err := r.FormFile("audio")
			if err != nil {
				w.WriteHeader(422)
				return
			}
			data, _ := io.ReadAll(f)
			mu.Lock()
			heard = append(heard, data)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"text":"где столовая","lang":"ru","duration_s":1.5,"confidence":null}`))
		case "/flags":
			_, _ = w.Write([]byte(`{"flags":[],"sentiment":"neu","quote":"","degraded":false}`))
		default:
			_, _ = w.Write([]byte(`{"answer":null,"sources":[],"confidence":"low","needs_escalation":true,"topic":"other","abstain_reason":"no_source"}`))
		}
	}))
	defer srv.Close()

	a := newAPIWithRAG(t, rag.New(srv.URL, 0))
	imp := a.upload("/api/scenarios/import", "t.xlsx", timelineXLSX(t), "Завод (синтетика)", 201)
	sid := imp["id"].(string)
	a.json("POST", "/api/scenarios/"+sid+"/publish", map[string]any{}, 200)
	n := a.json("POST", "/api/newcomers", map[string]any{
		"full_name": "Голосов Иван (синтетика)", "site": "GTP", "start_date": "2026-11-03", "scenario_id": sid,
	}, 201)
	nid := n["id"].(string)
	a.json("POST", "/api/dev/simulate-start", map[string]any{"newcomer_id": nid, "address_form": "vy", "consent": "given"}, 200)
	a.svc.Bot.Wait()

	// audio_path
	path := filepath.Join(t.TempDir(), "voice.ogg")
	if err := os.WriteFile(path, []byte("OggS-путь"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "audio_path": path}, 200)
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "audio_path": path, "text": "и текст"}, 422)
	a.json("POST", "/api/dev/simulate-message", map[string]any{"newcomer_id": nid, "audio_path": "/нет/такого.ogg"}, 422)

	// multipart: newcomer_id + audio
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("newcomer_id", nid)
	fw, _ := w.CreateFormFile("audio", "voice.m4a")
	_, _ = fw.Write([]byte("m4a-форма"))
	_ = w.Close()
	req := httptest.NewRequest("POST", "/api/dev/simulate-message", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	a.call(req, 200)

	mu.Lock()
	if len(heard) != 2 || string(heard[0]) != "OggS-путь" || string(heard[1]) != "m4a-форма" {
		t.Fatalf("в /transcribe: %q", heard)
	}
	mu.Unlock()
	msgs := a.json("GET", "/api/dev/chat/"+nid, nil, 200)["messages"].([]any)
	voices, echoes := 0, 0
	for _, x := range msgs {
		m := x.(map[string]any)
		if m["from"] == "newcomer" && m["kind"] == "voice" {
			voices++
		}
		if m["from"] == "bot" && strings.HasPrefix(m["text"].(string), "🎙 Распознал: Где столовая") {
			echoes++
		}
	}
	if voices != 2 || echoes != 2 {
		t.Fatalf("в ленте голосовых %d, эхо %d: %v", voices, echoes, msgs)
	}
}
