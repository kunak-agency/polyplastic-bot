package max

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeClock — часы, которые двигает только sleep: проверка очереди без настоящих пауз.
type fakeClock struct {
	t      time.Time
	slept  []time.Duration
	sleepE error
}

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	if c.sleepE != nil {
		return c.sleepE
	}
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
	return nil
}

func newFakePacer(interval time.Duration) (*pacer, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)}
	p := newPacer(interval)
	p.now, p.sleep = clk.now, clk.sleep
	return p, clk
}

func TestPacerWaitsWithinChat(t *testing.T) {
	p, clk := newFakePacer(600 * time.Millisecond)
	ctx := context.Background()
	noop := func() error { return nil }

	for i := 0; i < 3; i++ {
		_ = p.Do(ctx, "c1", noop)
	}
	if len(clk.slept) != 2 || clk.slept[0] != 600*time.Millisecond || clk.slept[1] != 600*time.Millisecond {
		t.Fatalf("паузы %v, want две по 600ms", clk.slept)
	}

	// Другой чат своей очереди не ждёт.
	_ = p.Do(ctx, "c2", noop)
	if len(clk.slept) != 2 {
		t.Fatalf("чат c2 ждал: паузы %v", clk.slept)
	}

	// Прошло больше интервала — пауза не нужна.
	clk.t = clk.t.Add(time.Second)
	_ = p.Do(ctx, "c1", noop)
	if len(clk.slept) != 2 {
		t.Fatalf("лишняя пауза: %v", clk.slept)
	}
}

func TestPacerErrorStillTakesSlot(t *testing.T) {
	p, clk := newFakePacer(600 * time.Millisecond)
	boom := errors.New("сеть")
	if err := p.Do(context.Background(), "c1", func() error { return boom }); err != boom {
		t.Fatalf("err = %v", err)
	}
	_ = p.Do(context.Background(), "c1", func() error { return nil })
	if len(clk.slept) != 1 {
		t.Fatalf("после ошибки следующая отправка должна ждать: паузы %v", clk.slept)
	}
}

func TestPacerCancelledWhileWaiting(t *testing.T) {
	p, clk := newFakePacer(600 * time.Millisecond)
	_ = p.Do(context.Background(), "c1", func() error { return nil })
	clk.sleepE = context.Canceled
	called := false
	err := p.Do(context.Background(), "c1", func() error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("err=%v, called=%v: отменённая отправка не должна уйти", err, called)
	}
}

func TestChatKey(t *testing.T) {
	if chatKey(5, 7) != "c5" || chatKey(0, 7) != "u7" {
		t.Fatal(chatKey(5, 7), chatKey(0, 7))
	}
}

func TestRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{"": time.Second, "2": 2 * time.Second, "abc": time.Second, "0": time.Second, "999": max429Pause}
	for h, want := range cases {
		if got := retryAfter(h, time.Second); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", h, got, want)
		}
	}
}

// TestCertPool — самоподписанный сертификат, выпущенный в тесте: PEM и DER принимаются,
// мусор — нет.
func TestCertPool(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Synthetic Test Root CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "ca.pem")
	derPath := filepath.Join(dir, "ca.cer")
	junkPath := filepath.Join(dir, "junk.txt")
	_ = os.WriteFile(pemPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(derPath, der, 0o600)
	_ = os.WriteFile(junkPath, []byte("not a certificate"), 0o600)

	for _, p := range []string{pemPath, derPath} {
		if pool, err := certPool(p); err != nil || pool == nil {
			t.Errorf("certPool(%s): pool=%v err=%v", filepath.Base(p), pool, err)
		}
	}
	if _, err := certPool(junkPath); err == nil {
		t.Error("мусор принят как сертификат")
	}
	if pool, err := certPool(""); err != nil || pool != nil {
		t.Errorf("пустой путь: pool=%v err=%v, want системный (nil)", pool, err)
	}
	if _, err := New(Config{Token: testToken, CAFile: filepath.Join(dir, "missing.pem")}); err == nil {
		t.Error("New с несуществующим MAX_CA_FILE должен отказать")
	}
}
