package services

import (
	"testing"
	"time"

	"main/domain"
	"main/repositories"
)

// Ранние сигналы зон v2: падение пульса ≥ 3 между соседними вехами; neg в 2 из 3 последних
// текстов и флаг. Жёлтая, только если других причин нет.
func TestRiskZonesV2EarlySignals(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	add := func(name string) domain.Employee {
		e := ovEmp(name, domain.SiteGTP, 20)
		rows.Employees = append(rows.Employees, e)
		return e
	}
	ago := func(days float64) time.Time { return ovNow.Add(-time.Duration(days * 24 * float64(time.Hour))) }
	text := func(e domain.Employee, tone string, days float64, flags ...string) {
		s := tone
		rows.Answers = append(rows.Answers, repositories.OverviewAnswer{EmployeeID: e.ID, QuestionNo: 2, Scale: domain.Scale10, Sentiment: &s, Flags: flags, CreatedAt: ago(days)})
	}
	question := func(e domain.Employee, tone string, days float64, flags ...string) {
		s := tone
		rows.Questions = append(rows.Questions, repositories.OverviewQuestion{EmployeeID: e.ID, Sentiment: &s, Flags: flags, CreatedAt: ago(days)})
	}

	drop := add("Упал на 3")
	rows.Answers = append(rows.Answers, ovAnswer(drop.ID, domain.Scale10, 9, ago(9)), ovAnswer(drop.ID, domain.Scale10, 6, ago(2)))
	drop5 := add("Упал на 2 по пятибалльной")
	rows.Answers = append(rows.Answers, ovAnswer(drop5.ID, domain.Scale5, 5, ago(9)), ovAnswer(drop5.ID, domain.Scale5, 4, ago(2)))
	small := add("Упал на 2")
	rows.Answers = append(rows.Answers, ovAnswer(small.ID, domain.Scale10, 9, ago(9)), ovAnswer(small.ID, domain.Scale10, 7, ago(2)))
	oldDrop := add("Упал давно")
	rows.Answers = append(rows.Answers, ovAnswer(oldDrop.ID, domain.Scale10, 10, ago(30)), ovAnswer(oldDrop.ID, domain.Scale10, 6, ago(20)))

	tone := add("Негатив и флаг")
	text(tone, domain.SentimentNeg, 5)
	question(tone, domain.SentimentNeg, 3, "F2")
	text(tone, domain.SentimentPos, 1)
	noFlag := add("Негатив без флага")
	text(noFlag, domain.SentimentNeg, 5)
	question(noFlag, domain.SentimentNeg, 3)
	text(noFlag, domain.SentimentNeu, 1)
	older := add("Негатив раньше трёх последних")
	text(older, domain.SentimentNeg, 9, "F3")
	text(older, domain.SentimentNeg, 7)
	text(older, domain.SentimentPos, 3)
	question(older, domain.SentimentNeu, 1)

	// другая причина важнее раннего сигнала
	marked := ovEmp("Отметка бота", domain.SiteGTP, 20)
	marked.Risk, marked.RiskReason = domain.RiskRed, strPtr("сигнал F7 в ответе на опрос")
	rows.Employees = append(rows.Employees, marked)
	rows.Answers = append(rows.Answers, ovAnswer(marked.ID, domain.Scale10, 9, ago(9)), ovAnswer(marked.ID, domain.Scale10, 5, ago(2)))

	got := map[string]RiskZone{}
	for _, r := range RiskZonesV2(rows, ovNow, tax) {
		got[r.FullName] = r
	}
	want := map[string]struct{ zone, reason string }{
		"Упал на 3":      {domain.RiskYellow, "ранний сигнал: пульс упал с 9 до 6 из 10"},
		"Негатив и флаг": {domain.RiskYellow, "ранний сигнал: негативный тон в 2 из 3 последних ответов и тревожная отметка"},
		"Отметка бота":   {domain.RiskRed, "сигнал F7 в ответе на опрос"},
	}
	for name, w := range want {
		r, ok := got[name]
		if !ok {
			t.Errorf("%s: нет в зонах", name)
			continue
		}
		if r.Zone != w.zone || r.Reasons[0] != w.reason {
			t.Errorf("%s: %s «%s», ждали %s «%s»", name, r.Zone, r.Reasons[0], w.zone, w.reason)
		}
	}
	if len(got) != len(want) {
		for n, r := range got {
			if _, ok := want[n]; !ok {
				t.Errorf("%s: не должен быть в зоне, а %s «%s»", n, r.Zone, r.Reasons[0])
			}
		}
	}
}

// Дайджест с FEATURE_RISK_V2 считает зоны по v2 — те же цифры, что в обзоре.
func TestDigestRiskV2MatchesOverview(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	score := ovEmp("Оценка 7/10", domain.SiteGTP, 20) // старые правила — жёлтая, v2 — не зона
	score.Risk, score.RiskReason = domain.RiskYellow, strPtr("оценка 7/10 в «Чек недели»")
	drop := ovEmp("Упал на 4", domain.SiteGTP, 20) // v2 — ранний сигнал
	rows.Employees = append(rows.Employees, score, drop)
	rows.Answers = append(rows.Answers, ovAnswer(drop.ID, domain.Scale10, 10, ovNow.AddDate(0, 0, -9)), ovAnswer(drop.ID, domain.Scale10, 6, ovNow.AddDate(0, 0, -2)))
	mon := time.Date(2026, 10, 19, 0, 0, 0, 0, ovMoscow)
	v2 := OverviewOptions{RiskV2: true}

	o := BuildOverview(rows, "", ovNow, tax, v2)
	d := BuildDigest(rows, nil, "", mon, ovNow, tax, v2)
	if d.Risks.Red+d.Risks.Yellow != len(o.Risks) || len(d.Risks.Items) != 1 || d.Risks.Items[0].FullName != "Упал на 4" {
		t.Fatalf("v2: дайджест %+v, обзор %d", d.Risks, len(o.Risks))
	}
	old := BuildDigest(rows, nil, "", mon, ovNow, tax)
	if len(old.Risks.Items) != len(BuildOverview(rows, "", ovNow, tax).Risks) {
		t.Fatalf("без флага: дайджест и обзор разошлись")
	}
	for _, r := range old.Risks.Items {
		if r.FullName == "Упал на 4" {
			t.Fatal("ранний сигнал без флага")
		}
	}
}
