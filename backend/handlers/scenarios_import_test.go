package handlers

import (
	"encoding/json"
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

// Всё, что создаёт импорт, проходит валидацию PUT без изменений: импорт → GET → PUT тем же телом
// → 200, и шаги после PUT не меняются. Синтетика и (если задан TZ_XLSX) настоящий файл заказчика.
func TestImportedStepsRoundTrip(t *testing.T) {
	files := map[string]string{"синтетика": "../services/testdata/tz_synthetic.xlsx"}
	if p := os.Getenv("TZ_XLSX"); p != "" {
		files["ТЗ заказчика"] = p
	}
	for name, path := range files {
		t.Run(name, func(t *testing.T) {
			a := newAPI(t)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			imp := a.upload("/api/scenarios/import", "tz.xlsx", b, "ТЗ (сверка)", 201)
			id := imp["id"].(string)
			got := a.json("GET", "/api/scenarios/"+id, nil, 200)
			steps := got["steps"]
			put := a.json("PUT", "/api/scenarios/"+id+"/steps", map[string]any{"steps": steps}, 200)
			before, _ := json.Marshal(steps)
			after, _ := json.Marshal(put["steps"])
			if string(before) != string(after) {
				t.Errorf("шаги изменились после PUT тем же телом")
			}
			a.json("PATCH", "/api/scenarios/"+id, map[string]any{"commands": got["commands"], "rules": got["rules"]}, 200)
		})
	}
}
