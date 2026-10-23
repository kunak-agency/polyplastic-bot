package taxonomy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRealFile(t *testing.T) {
	tx, err := LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tx.Codes()) != 11 || tx.Codes()[0] != "F0" || tx.Codes()[10] != "F10" {
		t.Fatalf("коды %v", tx.Codes())
	}
	want := map[string]struct {
		route, prio                                string
		hidden, restricted, notify, sb, storeQuote bool
	}{
		// restricted (текст новичка скрыт) — только F5 и F9; F0, F4, F10 скрыты лишь от руководителя.
		"F0":  {RouteDuty, P0, true, false, false, false, true},
		"F2":  {RouteHR, P1, false, false, true, false, true},
		"F4":  {RouteHR, P1, true, false, false, false, true},
		"F5":  {RouteHR, P0, true, true, false, true, true},
		"F7":  {RouteRecruiter, P0, false, false, false, false, true},
		"F9":  {RouteSecurity, P0, true, true, false, true, true},
		"F10": {RouteHR, P2, true, false, false, false, false},
	}
	for code, w := range want {
		c, ok := tx.Get(code)
		if !ok {
			t.Fatalf("нет %s", code)
		}
		if c.Route != w.route || c.Priority != w.prio || c.ManagerHidden != w.hidden || c.Restricted != w.restricted || c.NotifyManager != w.notify ||
			c.SecurityChat != w.sb || c.StoreQuote != w.storeQuote {
			t.Errorf("%s: %+v", code, c)
		}
	}
	// Ни одна закрытая категория не уходит руководителю.
	for _, code := range tx.Codes() {
		c, _ := tx.Get(code)
		if c.ManagerHidden && (c.Route == RouteManager || c.NotifyManager) {
			t.Errorf("%s видна руководителю: %+v", code, c)
		}
		if c.Restricted && !c.ManagerHidden {
			t.Errorf("%s видна руководителю: %+v", code, c)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) string {
		p := filepath.Join(dir, "t.yaml")
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := Load(filepath.Join(dir, "нет.yaml")); err == nil {
		t.Fatal("нет файла — ошибка")
	}
	if _, err := Load(write("::: не yaml")); err == nil {
		t.Fatal("мусор — ошибка")
	}
	_, err := Load(write("categories:\n  F5: {route: [mars], urgency: критическая, manager_visibility: none}\n"))
	if err == nil || !strings.Contains(err.Error(), "F5") {
		t.Fatalf("неизвестный получатель: %v", err)
	}
}

func TestLLMThresholds(t *testing.T) {
	tx, err := LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	if tx.ManualReviewFloor != 0.4 || tx.EscalateSeverityGte != 2 {
		t.Fatalf("правила: floor %v, severity %d", tx.ManualReviewFloor, tx.EscalateSeverityGte)
	}
	for code, want := range map[string]float64{"F0": 0.5, "F1": 0.6, "F7": 0.5, "F9": 0.4, "F10": 0.6} {
		if c, _ := tx.Get(code); c.AutoThreshold != want {
			t.Errorf("%s: порог %v, want %v", code, c.AutoThreshold, want)
		}
	}
	// escalate_if: severity ≥ 2 или критический код.
	if !tx.Escalates("F1", 2) || tx.Escalates("F1", 1) || !tx.Escalates("F7", 1) || !tx.Escalates("F0", 1) {
		t.Fatal("escalate_if разобран неверно")
	}
}
