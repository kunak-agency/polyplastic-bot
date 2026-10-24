package services

import (
	"testing"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Зоны риска v2 (FEATURE_RISK_V2): одна зона и одна причина, обращения в SLA и опросы < 48 ч —
// не зона. ovNow — среда 21.10.2026 12:00 МСК.
func TestRiskZonesV2(t *testing.T) {
	tax := ovTax(t)
	rows := &repositories.OverviewRows{}
	add := func(e domain.Employee) domain.Employee {
		rows.Employees = append(rows.Employees, e)
		return e
	}
	evID := func() uuid.UUID { id, _ := uuid.NewV7(); return id }
	at := func(days float64) *time.Time {
		t := ovNow.Add(-time.Duration(days * 24 * float64(time.Hour)))
		return &t
	}
	survey := func(emp uuid.UUID, sent *time.Time) {
		rows.Events = append(rows.Events, repositories.OverviewEvent{ID: evID(), EmployeeID: emp, Title: "Пульс", Kind: domain.KindSurvey, Status: domain.EventSent, SentAt: sent})
	}
	ticket := func(emp uuid.UUID, prio, status string, due *time.Time) {
		rows.Tickets = append(rows.Tickets, repositories.OverviewTicket{ID: evID(), EmployeeID: emp, Category: "F2", Priority: prio, Status: status, SLADueAt: due})
	}

	calm := add(ovEmp("Спокойный", domain.SiteGTP, 10))
	ticket(calm.ID, taxonomy.P1, domain.TicketOpen, at(-1))     // P1 в пределах SLA
	ticket(calm.ID, taxonomy.P2, domain.TicketHumanActive, nil) // у человека
	survey(calm.ID, at(1))                                      // опрос без ответа сутки
	p0 := add(ovEmp("Срочное", domain.SiteGTP, 10))
	ticket(p0.ID, taxonomy.P0, domain.TicketOpen, at(0.1))
	slaOver := add(ovEmp("SLA горит", domain.SiteGTP, 10))
	ticket(slaOver.ID, taxonomy.P1, domain.TicketAssigned, at(0.5))
	lowTwice := add(ovEmp("Два низких", domain.SiteGTP, 20))
	lowOnce := add(ovEmp("Один низкий", domain.SiteGTP, 20))
	lowOld := add(ovEmp("Давний низкий", domain.SiteGTP, 40))
	rows.Answers = append(rows.Answers,
		ovAnswer(lowTwice.ID, domain.Scale5, 2, *at(9)), ovAnswer(lowTwice.ID, domain.Scale10, 5, *at(2)),
		ovAnswer(lowOnce.ID, domain.Scale10, 9, *at(9)), ovAnswer(lowOnce.ID, domain.Scale10, 4, *at(2)),
		ovAnswer(lowOld.ID, domain.Scale10, 3, *at(30)), ovAnswer(lowOld.ID, domain.Scale10, 3, *at(20)),
	)
	inviteTomorrow := ovEmp("Выход завтра", domain.SiteGTP, -1)
	inviteTomorrow.Connection = domain.ConnInvited
	add(inviteTomorrow)
	inviteLater := ovEmp("Выход через 3 дня", domain.SiteGTP, -3)
	inviteLater.Connection = domain.ConnInvited
	add(inviteLater)
	// Молчит с пятницы 16.10 (пн, вт, ср — 3 рабочих дня): жёлтая.
	silent3 := ovEmp("Молчит 3 дня", domain.SiteGTP, 30)
	silent3.LastActivityAt = at(6)
	add(silent3)
	survey(silent3.ID, at(5))
	// Молчит 8 рабочих дней после двух напоминаний: красная.
	silentRed := ovEmp("Молчит после наджей", domain.SiteGTP, 30)
	silentRed.LastActivityAt = at(13)
	silentRed.UnresponsiveSince = at(11)
	add(silentRed)
	survey(silentRed.ID, at(12))
	// Пропустил старый опрос, но потом отвечал — на связи.
	skipped := ovEmp("Пропустил опрос", domain.SiteGTP, 30)
	skipped.LastActivityAt = at(1)
	add(skipped)
	survey(skipped.ID, at(10))
	// Старая отметка «оценка» — не зона; флаг ниже порога — жёлтая; F7 — красная.
	score := ovEmp("Оценка 7/10", domain.SiteGTP, 40)
	score.Risk, score.RiskReason = domain.RiskYellow, strPtr("оценка 7/10 в «Чек недели»")
	add(score)
	flag := ovEmp("Флаг ниже порога", domain.SiteGTP, 40)
	flag.Risk, flag.RiskReason = domain.RiskYellow, strPtr("возможный сигнал F2 в ответе (LLM-разметка, на ручной просмотр HR)")
	add(flag)
	f7 := ovEmp("Сигнал F7", domain.SiteGTP, 40)
	f7.Risk, f7.RiskReason = domain.RiskRed, strPtr("сигнал F7 в ответе на опрос")
	add(f7)
	failed := add(ovEmp("Не доставлено", domain.SiteGTP, 5))
	rows.Events = append(rows.Events, repositories.OverviewEvent{ID: evID(), EmployeeID: failed.ID, Title: "Карта", Kind: domain.KindCard, Status: domain.EventFailed, Attempts: 3})
	catchup := ovEmp("Догоняет", domain.SiteGTP, 12)
	catchup.CatchupStartedAt = at(4)
	add(catchup)
	for i := 0; i < 5; i++ {
		st := domain.EventScheduled
		if i == 0 {
			st = domain.EventAnswered
		}
		rows.Events = append(rows.Events, repositories.OverviewEvent{ID: evID(), EmployeeID: catchup.ID, Title: "Вопрос", Kind: domain.KindSurvey, Status: st, Catchup: true})
	}
	// Красная и жёлтая сразу — одна зона, красная причина.
	both := ovEmp("Всё сразу", domain.SiteGTP, 20)
	both.Risk, both.RiskReason = domain.RiskYellow, strPtr("возможный сигнал F3")
	add(both)
	ticket(both.ID, taxonomy.P0, domain.TicketAssigned, at(0.1))

	got := map[string]RiskZone{}
	for _, r := range RiskZonesV2(rows, ovNow, tax) {
		if len(r.Reasons) != 1 {
			t.Fatalf("%s: причин %d", r.FullName, len(r.Reasons))
		}
		got[r.FullName] = r
	}
	want := map[string]struct{ zone, reason string }{
		"Срочное":             {domain.RiskRed, "срочное обращение «" + ticketTopic("F2", tax) + "» ждёт ответа"},
		"SLA горит":           {domain.RiskYellow, "обращение «" + ticketTopic("F2", tax) + "» просрочено по SLA"},
		"Два низких":          {domain.RiskRed, "два низких пульса подряд"},
		"Один низкий":         {domain.RiskYellow, "низкий пульс 4/10"},
		"Выход завтра":        {domain.RiskRed, "не открыл(а) приглашение, выход завтра"},
		"Молчит 3 дня":        {domain.RiskYellow, "не отвечает 3 рабочих дня"},
		"Молчит после наджей": {domain.RiskRed, "не отвечает 8 рабочих дней после двух напоминаний"},
		"Флаг ниже порога":    {domain.RiskYellow, "возможный сигнал F2 в ответе (LLM-разметка, на ручной просмотр HR)"},
		"Сигнал F7":           {domain.RiskRed, "сигнал F7 в ответе на опрос"},
		"Не доставлено":       {domain.RiskYellow, "сообщение «Карта» не доставлено"},
		"Догоняет":            {domain.RiskYellow, "догоняет 1 из 5, отстаёт на 4 дня"},
		"Всё сразу":           {domain.RiskRed, "срочное обращение «" + ticketTopic("F2", tax) + "» ждёт ответа"},
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
	for _, name := range []string{"Спокойный", "Давний низкий", "Выход через 3 дня", "Пропустил опрос", "Оценка 7/10"} {
		if r, ok := got[name]; ok {
			t.Errorf("%s: не должен быть в зоне, а %s «%s»", name, r.Zone, r.Reasons[0])
		}
	}
	if len(got) != len(want) {
		t.Errorf("зон %d, ждали %d", len(got), len(want))
	}

	// Без флага — прежние правила: «Спокойный» с открытым P1 — жёлтая.
	o := BuildOverview(rows, "", ovNow, tax)
	found := false
	for _, r := range o.Risks {
		if r.FullName == "Спокойный" && r.Zone == domain.RiskYellow {
			found = true
		}
	}
	if !found {
		t.Fatal("без флага зоны изменились")
	}
	// С флагом обзор берёт зоны v2, красные первыми.
	o2 := BuildOverview(rows, "", ovNow, tax, OverviewOptions{RiskV2: true})
	if len(o2.Risks) != len(want) || o2.Risks[0].Zone != domain.RiskRed {
		t.Fatalf("обзор v2: %d зон, первая %s", len(o2.Risks), o2.Risks[0].Zone)
	}
}
