package rag

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// POST /documents: multipart с полями doc и file, ответ разобран; 415/422/413 — RejectedError
// (ответ про файл), 5xx — *Error (сбой rag).
func TestUploadDocument(t *testing.T) {
	var gotDoc, gotName string
	var gotData []byte
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/documents" {
			t.Errorf("запрос %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		gotDoc = r.FormValue("doc")
		f, fh, err := r.FormFile("file")
		if err == nil {
			gotName = fh.Filename
			gotData, _ = io.ReadAll(f)
		}
		w.WriteHeader(status)
		switch status {
		case http.StatusOK:
			_, _ = w.Write([]byte(`{"doc_id":"abc123","chunks":3,"clauses":5,"low_confidence_pages":[2]}`))
		case http.StatusUnprocessableEntity:
			_, _ = w.Write([]byte(`{"detail":"в документе не найден текст"}`))
		default:
			_, _ = w.Write([]byte(`oops`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, time.Second)
	ctx := context.Background()

	res, err := c.UploadDocument(ctx, "Памятка (синтетика)", "memo.pdf", []byte("%PDF-1.4 test"))
	if err != nil {
		t.Fatal(err)
	}
	if res.DocID != "abc123" || res.Chunks != 3 || res.Clauses != 5 || len(res.LowConfidencePages) != 1 {
		t.Fatalf("ответ %+v", res)
	}
	if gotDoc != "Памятка (синтетика)" || gotName != "memo.pdf" || string(gotData) != "%PDF-1.4 test" {
		t.Fatalf("rag получил doc=%q file=%q data=%q", gotDoc, gotName, gotData)
	}

	status = http.StatusUnprocessableEntity
	_, err = c.UploadDocument(ctx, "x", "x.pdf", []byte("%PDF-"))
	var rej *RejectedError
	if !errors.As(err, &rej) || rej.Status != http.StatusUnprocessableEntity || rej.Detail != "в документе не найден текст" {
		t.Fatalf("422: %v", err)
	}

	status = http.StatusInternalServerError
	_, err = c.UploadDocument(ctx, "x", "x.pdf", []byte("%PDF-"))
	var re *Error
	if !errors.As(err, &re) || re.Status != 500 || errors.As(err, &rej) {
		t.Fatalf("500: %v", err)
	}

	if _, err := New("", time.Second).UploadDocument(ctx, "x", "x.pdf", nil); err == nil {
		t.Fatal("без адреса — ошибка")
	}
}

func TestListDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/documents" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"doc_id":"c1","doc":"ПВТР-синтетика","filename":null,"kind":"corpus","source":"corpus",
			"chunks":10,"clauses":15,"low_confidence_pages":[],"uploaded_at":"2026-10-23T20:36:48+00:00"}],"total":1}`))
	}))
	defer srv.Close()
	list, err := New(srv.URL, time.Second).ListDocuments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].DocID != "c1" || list[0].Source != SourceCorpus || list[0].Filename != nil || list[0].Clauses != 15 {
		t.Fatalf("список %+v", list)
	}
}
