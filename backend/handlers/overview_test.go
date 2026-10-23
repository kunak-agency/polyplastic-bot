package handlers

import "testing"

// GET /api/overview (api_contract.md, v3): форма ответа и проверка площадки.
func TestOverviewRoute(t *testing.T) {
	a := newAPI(t)
	body := a.json("GET", "/api/overview", nil, 200)
	for _, k := range []string{"site", "generated_at", "week", "tiles", "stages", "risk_zones", "action_today", "heatmap", "trend"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("нет поля %s: %v", k, body)
		}
	}
	if body["site"] != nil {
		t.Fatalf("site без фильтра — null: %v", body["site"])
	}
	tiles := body["tiles"].(map[string]any)
	pr := tiles["pulse_response"].(map[string]any)
	if pr["percent"] != nil || tiles["in_adaptation"].(float64) != 0 {
		t.Fatalf("пустая база: %v", tiles)
	}
	hm := body["heatmap"].(map[string]any)
	if hm["min_group"].(float64) != 5 || len(hm["cells"].([]any)) != 30 {
		t.Fatalf("heatmap %v", hm)
	}
	if items := body["risk_zones"].(map[string]any)["items"].([]any); len(items) != 0 {
		t.Fatalf("risk_zones %v", items)
	}
	gtp := a.json("GET", "/api/overview?site=GTP", nil, 200)
	if gtp["site"] != "GTP" || len(gtp["heatmap"].(map[string]any)["sites"].([]any)) != 1 {
		t.Fatalf("site=GTP: %v", gtp["site"])
	}
	a.json("GET", "/api/overview?site=Moon", nil, 422)
}
