package rag

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranscribeSendsAudioPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transcribe" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		f, fh, err := r.FormFile("audio")
		if err != nil {
			t.Fatalf("нет части audio: %v", err)
		}
		data, _ := io.ReadAll(f)
		if fh.Filename != "voice.ogg" || string(data) != "OggS..." {
			t.Errorf("часть %q %q", fh.Filename, data)
		}
		_, _ = w.Write([]byte(`{"text":"где столовая","lang":"ru","duration_s":2.5,"confidence":null}`))
	}))
	defer srv.Close()
	got, err := New(srv.URL, 0).Transcribe(context.Background(), "voice.ogg", []byte("OggS..."))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "где столовая" || got.Lang != "ru" || got.DurationS != 2.5 || got.Confidence != nil {
		t.Fatalf("%+v", got)
	}
}

func TestTranscribeFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	_, err := New(srv.URL, 0).Transcribe(context.Background(), "v.ogg", []byte("x"))
	var e *Error
	if !errors.As(err, &e) || e.Status != 503 {
		t.Fatalf("ждали Error 503, получили %v", err)
	}
	if _, err := New("", 0).Transcribe(context.Background(), "v.ogg", []byte("x")); err == nil {
		t.Fatal("без адреса — ошибка")
	}
}
