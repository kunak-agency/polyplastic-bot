package services

import (
	"encoding/json"
	"os"
	"testing"
)

// TestImportCustomerTZ — ручная проверка на файле заказчика (конфиденциален, в репозиторий не
// кладём): TZ_XLSX=/путь/к/файлу.xlsx go test ./services -run TestImportCustomerTZ -v.
// Без переменной пропускается. Печатает отчёт и сводку шагов (тексты не печатает).
func TestImportCustomerTZ(t *testing.T) {
	path := os.Getenv("TZ_XLSX")
	if path == "" {
		t.Skip("TZ_XLSX не задан")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	steps, rules, cmds, rep, err := ParseScenarioImport(f)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	t.Logf("report: %s", b)
	for _, s := range steps {
		nb, sq := 0, 0
		for _, x := range s.Buttons {
			if x.Branch != nil {
				nb++
			}
		}
		if s.Survey != nil {
			sq = len(s.Survey.Questions)
		}
		t.Logf("%-18s %-6s sites=%v kind=%-6s btn=%d br=%d sq=%d media=%d req=%v hr=%v flag=%v", s.Key, s.Time, s.Sites, s.Kind, len(s.Buttons), nb, sq, len(s.Media), s.Required, s.HRTask, s.FlagRequired != nil)
	}
	t.Logf("rules=%d commands=%d", len(rules), len(cmds))
	if os.Getenv("TZ_DUMP") != "" {
		b, _ := json.MarshalIndent(map[string]any{"steps": steps, "rules": rules, "commands": cmds}, "", "  ")
		_ = os.WriteFile(os.Getenv("TZ_DUMP"), b, 0o600)
	}
}
