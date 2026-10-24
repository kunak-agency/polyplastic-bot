package main

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"testing"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/pkg/taxonomy"
	"main/repositories"
	"main/services"
)

// Зоны риска v2 на синтетике (FEATURE_RISK_V2): полный демо-поток (сид + seed-demo, 150 человек)
// в отдельной базе TEST_DATABASE_URL (таблицы очищаются), доли красных и жёлтых среди новичков
// в адаптации. Ориентир ресерча — красная 5–10 %, жёлтая 10–15 %; тест держит коридор с запасом
// на дату запуска (поток строится от сегодняшнего дня): красная ≤ 12 %, красная + жёлтая ≤ 30 %.
// Без TEST_DATABASE_URL пропускается.
func TestRiskV2SyntheticShares(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	repos, err := repositories.InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repos.Close()
	if err := repos.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := repos.Truncate(ctx); err != nil {
		t.Fatal(err)
	}
	// Базовые сценарии — настоящим сидом (seed-demo копирует шаги «Офис GrPP»).
	cmd := exec.Command("go", "run", "../seed")
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn, "APP_ENV=development")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("сид: %v\n%s", err, b)
	}
	tax, err := taxonomy.LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	svc := services.NewServices(repos, &externalApi.ExternalAPI{}, services.Options{
		Bot: config.Bot{Username: "korsik_test_bot", InviteLinkFormat: config.DefaultInviteLinkFormat}, Taxonomy: tax,
	})
	people, err := loadPersonas()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	scID, surveys := demoScenarioID(ctx, repos, svc)
	g := &gen{repos: repos, svc: svc, tax: tax, rng: rand.New(rand.NewSource(genSeed)), now: now, scenario: scID, surveys: surveys}
	days := g.dayPlan(people)
	for _, p := range extraPersonas(people, 150-len(people)) {
		people = append(people, p)
		days = append(days, *p.Day)
	}
	for i, p := range people {
		g.person(ctx, i, p, days[i])
	}

	rows, err := repos.Overview().Load(ctx, "", now.Add(-60*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v1 := services.BuildOverview(rows, "", now, tax)
	v2 := services.BuildOverview(rows, "", now, tax, services.OverviewOptions{RiskV2: true})
	n := v2.Tiles.InAdaptation
	if n == 0 {
		t.Fatal("в адаптации никого")
	}
	count := func(o services.Overview) (red, yellow int, why map[string]int) {
		why = map[string]int{}
		for _, r := range o.Risks {
			if r.Zone == domain.RiskRed {
				red++
			} else {
				yellow++
			}
			if len(r.Reasons) > 0 {
				why[r.Zone+": "+reasonKind(r.Reasons[0])]++
			}
		}
		return
	}
	r1, y1, _ := count(v1)
	r2, y2, why := count(v2)
	pct := func(k int) float64 { return float64(k) * 100 / float64(n) }
	t.Logf("в адаптации %d; v1: красных %d (%.1f%%), жёлтых %d (%.1f%%); v2: красных %d (%.1f%%), жёлтых %d (%.1f%%)",
		n, r1, pct(r1), y1, pct(y1), r2, pct(r2), y2, pct(y2))
	for k, c := range why {
		t.Logf("  v2 %s — %d", k, c)
	}
	for _, r := range v2.Risks {
		if len(r.Reasons) != 1 {
			t.Fatalf("v2: у новичка %s причин %d, нужна одна", r.FullName, len(r.Reasons))
		}
	}
	if pct(r2) > 12 || pct(r2+y2) > 30 {
		t.Fatalf("v2: красных %.1f%%, в зонах %.1f%% — вне коридора", pct(r2), pct(r2+y2))
	}
	if r2+y2 >= r1+y1 {
		t.Fatalf("v2 не сузил зоны: %d против %d", r2+y2, r1+y1)
	}
}

// reasonKind — причина без чисел и названий: для сводки в логе теста.
func reasonKind(s string) string {
	out := []rune{}
	for _, r := range s {
		if r == '«' || (r >= '0' && r <= '9') {
			break
		}
		out = append(out, r)
	}
	return string(out)
}
