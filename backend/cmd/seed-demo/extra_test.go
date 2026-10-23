package main

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"main/domain"
)

// Добор: те же люди при том же -count, имена уникальны и помечены, пропорции флагов на весь
// поток — в коридорах ресерча (блок C).
func TestExtraPersonas(t *testing.T) {
	base, err := loadPersonas()
	if err != nil {
		t.Fatal(err)
	}
	extra := extraPersonas(base, 150-len(base))
	if again := extraPersonas(base, 150-len(base)); !reflect.DeepEqual(extra, again) {
		t.Fatal("добор не детерминирован")
	}
	if len(extraPersonas(base, 0)) != 0 {
		t.Fatal("-count ≤ 50 — без добора")
	}
	all := append(append([]persona{}, base...), extra...)
	names := map[string]bool{}
	flagged := map[string]int{}
	f2 := map[string]int{}
	none := 0
	for _, p := range all {
		if names[p.FullName] || !strings.HasSuffix(p.FullName, "(синтетика)") {
			t.Fatalf("имя %q повторяется или без пометки", p.FullName)
		}
		names[p.FullName] = true
		if len(p.HiddenFlags) == 0 {
			none++
		}
		seen := map[string]bool{}
		for _, f := range p.HiddenFlags {
			if seen[f.Code] {
				t.Fatalf("%s: код %s дважды", p.ID, f.Code)
			}
			seen[f.Code] = true
			flagged[f.Code]++
			if f.Code == "F2" {
				f2[p.Site]++
			}
		}
	}
	for _, p := range extra {
		if p.Day == nil {
			t.Fatalf("%s: нет дня", p.ID)
		}
		for _, f := range p.HiddenFlags {
			on := dayOf(f.Onset)
			if on > *p.Day || *p.Day-on > 25 {
				t.Fatalf("%s %s: начало D+%d вне окна обзора при D+%d", p.ID, f.Code, on, *p.Day)
			}
			// Решённый P0 — между опросами: иначе ответ на опрос даст красную зону.
			if p0(f.Code) && f.Resolved != nil {
				res := dayOf(*f.Resolved)
				if on <= 14 || res > *p.Day {
					t.Fatalf("%s %s: решённый P0 D+%d…D+%d задевает опрос или не решён к D+%d", p.ID, f.Code, on, res, *p.Day)
				}
			}
		}
	}
	n := float64(len(all))
	pct := func(k int) float64 { return float64(k) * 100 / n }
	want := map[string][2]float64{
		"F1": {22, 28}, "F2": {12, 18}, "F6": {12, 18}, "F7": {6, 10}, "F8": {6, 10},
		"F5": {2, 4}, "F9": {1, 3}, "F10": {3, 5},
	}
	for c, r := range want {
		if p := pct(flagged[c]); p < r[0] || p > r[1] {
			t.Errorf("%s: %.1f %%, коридор %.0f–%.0f %%", c, p, r[0], r[1])
		}
	}
	if p := pct(flagged["F3"] + flagged["F4"]); math.Abs(p-10) > 2 {
		t.Errorf("F3+F4: %.1f %%, цель ≈ 10 %%", p)
	}
	if p := pct(none); p < 45 || p > 50 {
		t.Errorf("без флагов %.1f %%, цель 45–50 %%", p)
	}
	if f2[domain.SiteGTP] <= f2[domain.SiteGrPP] || f2[domain.SiteGTP] <= f2[domain.SiteLuzhniki] {
		t.Errorf("F2 должен быть выше на GTP: %v", f2)
	}
}
