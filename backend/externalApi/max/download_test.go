package max

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Download идёт клиентом с нашим корневым сертификатом (как MAX_CA_FILE с Минцифры), без
// токена бота, и режет файл по лимиту.
func TestDownloadUsesCAAndLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("токен бота ушёл на хост файла")
		}
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte("OggS-голосовое"))
	}))
	defer srv.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Token: "secret", CAFile: ca, BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data, err := c.Download(ctx, srv.URL+"/v.ogg", 1<<20)
	if err != nil || string(data) != "OggS-голосовое" {
		t.Fatalf("%q %v", data, err)
	}
	if _, err := c.Download(ctx, srv.URL+"/v.ogg", 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("лимит: %v", err)
	}
	if _, err := c.Download(ctx, srv.URL+"/missing", 1<<20); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
	if _, err := c.Download(ctx, "http://example.invalid/v.ogg", 1<<20); err == nil {
		t.Fatal("http без TLS — отказ")
	}
	// без нашего CA сертификат сервера не признаётся
	plain, _ := New(Config{Token: "secret", BaseURL: srv.URL})
	if _, err := plain.Download(ctx, srv.URL+"/v.ogg", 1<<20); err == nil {
		t.Fatal("чужой сертификат принят без CA")
	}
}

func TestFakeDownload(t *testing.T) {
	f := NewFake()
	url := f.PutFile([]byte("abc"))
	if data, err := f.Download(context.Background(), url, 10); err != nil || string(data) != "abc" {
		t.Fatalf("%q %v", data, err)
	}
	if _, err := f.Download(context.Background(), url, 2); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("лимит: %v", err)
	}
	if _, err := f.Download(context.Background(), "fake://file/99", 10); err == nil {
		t.Fatal("нет файла — ошибка")
	}
}
