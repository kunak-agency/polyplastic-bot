package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Каждый код предупреждения импорта (warn* в scenario_xlsx_tz.go и cellWarn{"…"} в
// scenario_xlsx_cell.go) имеет русскую подпись в кабинете (frontend scenarios.importReport.codes):
// иначе отчёт импорта показывает сырой код (e2e 24.10: label_long, covered_by_onboarding…).
func TestImportWarningCodesHaveLabels(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "i18n", "locales", "ru", "scenarios.json"))
	if err != nil {
		t.Skipf("нет фронта рядом: %v", err)
	}
	var loc struct {
		ImportReport struct {
			Codes map[string]string `json:"codes"`
		} `json:"importReport"`
	}
	if err := json.Unmarshal(raw, &loc); err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for file, re := range map[string]*regexp.Regexp{
		"scenario_xlsx_tz.go":   regexp.MustCompile(`warn[A-Za-z]+\s*=\s*"([a-z_]+)"`),
		"scenario_xlsx_cell.go": regexp.MustCompile(`cellWarn\{"([a-z_]+)"`),
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			codes[m[1]] = true
		}
	}
	if len(codes) < 25 {
		t.Fatalf("кодов найдено %d — разбор исходников сломался", len(codes))
	}
	for c := range codes {
		if loc.ImportReport.Codes[c] == "" {
			t.Errorf("нет подписи кода %q в scenarios.importReport.codes", c)
		}
	}
}
