package handlers

import (
	"os"
	"testing"
)

// Импорт синтетического ТЗ (services/testdata/tz_synthetic.xlsx): ответ несёт import_report,
// правила и команды, предупреждения ссылаются на шаги сценария.
func TestImportTZReportAPI(t *testing.T) {
	a := newAPI(t)
	b, err := os.ReadFile("../services/testdata/tz_synthetic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	imp := a.upload("/api/scenarios/import", "tz.xlsx", b, "ТЗ (синтетика)", 201)
	rep, _ := imp["import_report"].(map[string]any)
	if rep == nil || rep["blocks"] != float64(16) || rep["rules"] != float64(2) {
		t.Fatalf("import_report: %v", rep)
	}
	if rules, _ := imp["rules"].([]any); len(rules) != 2 {
		t.Fatalf("rules: %v", imp["rules"])
	}
	if cmds, _ := imp["commands"].([]any); len(cmds) != 2 {
		t.Fatalf("commands: %v", imp["commands"])
	}
	ids := map[string]bool{}
	for _, s := range imp["steps"].([]any) {
		ids[s.(map[string]any)["id"].(string)] = true
	}
	warns, _ := rep["warnings"].([]any)
	if len(warns) == 0 {
		t.Fatal("нет предупреждений")
	}
	for _, w := range warns {
		m := w.(map[string]any)
		if id, _ := m["step_id"].(string); id != "" && !ids[id] {
			t.Errorf("step_id предупреждения не из сценария: %v", m)
		}
	}
	// Повторный импорт в существующий сценарий — тот же отчёт.
	id := imp["id"].(string)
	again := a.upload("/api/scenarios/"+id+"/import", "tz.xlsx", b, "", 200)
	if r, _ := again["import_report"].(map[string]any); r == nil || r["blocks"] != float64(16) {
		t.Fatalf("импорт в сценарий: %v", again["import_report"])
	}
}
