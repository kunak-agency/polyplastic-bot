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

// GET /api/indicators («Показатели»): форма ответа, период и проверка площадки.
func TestIndicatorsRoute(t *testing.T) {
	a := newAPI(t)
	body := a.json("GET", "/api/indicators", nil, 200)
	for _, k := range []string{"site", "period", "cohort", "today", "tiles", "funnel", "sites", "topics", "hr_load"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("нет поля %s: %v", k, body)
		}
	}
	if p := body["period"].(map[string]any); p["key"] != "30d" || p["days"].(float64) != 30 {
		t.Fatalf("period %v", p)
	}
	tiles := body["tiles"].([]any)
	if len(tiles) != 6 || tiles[0].(map[string]any)["status"] != "few" || tiles[5].(map[string]any)["status"] != "no_marker" {
		t.Fatalf("tiles %v", tiles)
	}
	if rows := body["sites"].(map[string]any)["rows"].([]any); len(rows) != 6 {
		t.Fatalf("sites rows %v", rows)
	}
	if today := body["today"].(map[string]any); today["source"] != "actions" {
		t.Fatalf("today без входа: %v", today)
	}
	q := a.json("GET", "/api/indicators?period=q&site=GTP", nil, 200)
	if q["site"] != "GTP" || q["period"].(map[string]any)["key"] != "q" {
		t.Fatalf("q: %v", q)
	}
	a.json("GET", "/api/indicators?period=week", nil, 422)
	a.json("GET", "/api/indicators?site=Moon", nil, 422)
}
