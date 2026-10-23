package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFlagsRequestAndAnswer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/flags" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"flags":[{"code":"F7","severity":3,"confidence":0.9},{"code":"F4","severity":5,"confidence":0.8}],
			"sentiment":"neg","quote":"резюме обновил","degraded":false}`))
	}))
	defer srv.Close()

	q := "Как дела с наставником?"
	resp, err := New(srv.URL, time.Second).Flags(context.Background(), FlagsRequest{Text: "резюме обновил", Context: FlagsContextPulse(5), SurveyQuestion: &q})
	if err != nil {
		t.Fatal(err)
	}
	if got["text"] != "резюме обновил" || got["context"] != "pulse_d5" || got["survey_question"] != q {
		t.Fatalf("тело запроса %v", got)
	}
	// severity 5 вне контракта — отброшен страховкой клиента
	if len(resp.Flags) != 1 || resp.Flags[0].Code != "F7" || resp.Quote != "резюме обновил" || resp.Degraded {
		t.Fatalf("%+v", resp)
	}

	// Без вопроса опроса поля нет, контекст по умолчанию — free_text.
	if _, err := New(srv.URL, time.Second).Flags(context.Background(), FlagsRequest{Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["survey_question"]; ok || got["context"] != "free_text" {
		t.Fatalf("тело запроса %v", got)
	}
}

func TestFlagsErrors(t *testing.T) {
	if _, err := New("", time.Second).Flags(context.Background(), FlagsRequest{Text: "x"}); err == nil {
		t.Fatal("без адреса — ошибка")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, time.Second).Flags(context.Background(), FlagsRequest{Text: "x"}); err == nil {
		t.Fatal("503 — ошибка")
	}
	if FlagsContextPulse(-3) != "pulse_d0" {
		t.Fatal("отрицательный день — pulse_d0")
	}
}
