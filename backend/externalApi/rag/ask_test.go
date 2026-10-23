package rag

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAskRequestAndAnswer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ask" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"answer":"Отпуск после 6 месяцев.","sources":[{"doc":"ПВТР","clause_id":"6.7.8","page":14,"quote":"отпуск"}],
			"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`))
	}))
	defer srv.Close()

	site, form := "GTP", "ty"
	ctx := WithTraceID(context.Background(), "trace-1")
	resp, err := New(srv.URL, time.Second).Ask(ctx, AskRequest{Question: "когда отпуск", Site: &site, AddressForm: &form})
	if err != nil {
		t.Fatal(err)
	}
	if got["question"] != "когда отпуск" || got["site"] != "GTP" || got["address_form"] != "ty" || got["trace_id"] != "trace-1" {
		t.Fatalf("тело запроса %v", got)
	}
	if resp.Answer == nil || len(resp.Sources) != 1 || resp.Sources[0].ClauseID != "6.7.8" || *resp.Sources[0].Page != 14 {
		t.Fatalf("%+v", resp)
	}

	// Без site и address_form — null, как в контракте.
	if _, err := New(srv.URL, time.Second).Ask(context.Background(), AskRequest{Question: "q"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["address_form"]; !ok || v != nil {
		t.Fatalf("address_form: %v", got)
	}
	if _, ok := got["trace_id"]; ok {
		t.Fatalf("пустой trace_id не отправляется: %v", got)
	}
}

func TestAskFailures(t *testing.T) {
	cases := map[string]struct {
		handler func(w http.ResponseWriter, r *http.Request)
		check   func(error) bool
	}{
		"таймаут": {
			func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			},
			func(err error) bool {
				return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline")
			},
		},
		"500": {
			func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
			func(err error) bool { var e *Error; return errors.As(err, &e) && e.Status == 500 },
		},
		"ответ без источника": {
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"answer":"выдумка","sources":[],"confidence":"high","needs_escalation":false,"topic":"hr","abstain_reason":null}`))
			},
			func(err error) bool { return errors.Is(err, ErrBadAnswer) },
		},
		"мусор": {
			func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`<html>`)) },
			func(err error) bool { return err != nil },
		},
	}
	for name, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(c.handler))
		_, err := New(srv.URL, 100*time.Millisecond).Ask(context.Background(), AskRequest{Question: "q"})
		srv.Close()
		if err == nil || !c.check(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := New("", 0).Ask(context.Background(), AskRequest{Question: "q"}); err == nil {
		t.Error("пустой адрес")
	}
}

func TestAskEscalationDropsAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answer":"лишнее","sources":[],"confidence":"low","needs_escalation":true,"topic":"hr","abstain_reason":"no_source"}`))
	}))
	defer srv.Close()
	resp, err := New(srv.URL, time.Second).Ask(context.Background(), AskRequest{Question: "q"})
	if err != nil || resp.Answer != nil || resp.Reason() != AbstainNoSource {
		t.Fatalf("%+v %v", resp, err)
	}
}
