package services

import (
	"context"
	"testing"

	"main/domain"
)

// Развёртка, идемпотентность и перенос даты выхода.
func TestSchedulerBuildAndReschedule(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	ctx := context.Background()
	evs := e.events(t, c.Employee.ID)
	if len(evs) != 5 { // 6 шагов, один — только для Лужников
		t.Fatalf("событий %d, want 5", len(evs))
	}
	if ev := eventByTitle(t, evs, "Документы"); !ev.FireAt.Equal(msk(2026, 11, 2, 10, 0)) {
		t.Fatalf("D−3 в субботу → понедельник: %v", ev.FireAt)
	}

	// Повторная развёртка той же версии дублей не даёт: event_id детерминирован.
	emp := e.employee(t, c.Employee.ID)
	v, _ := e.repos.Scenarios.Version(ctx, emp.ScenarioID, emp.ScenarioVersion)
	if err := e.repos.Events.CreateMissing(ctx, e.svc.Scheduler.BuildEvents(emp, v, nil)); err != nil {
		t.Fatal(err)
	}
	if n := len(e.events(t, c.Employee.ID)); n != 5 {
		t.Fatalf("после повторной развёртки %d", n)
	}

	// Пакет D−3 ушёл; переносим выход на неделю: отправленные не трогаем, остальные — заново.
	e.connect(t, c, domain.AddressVy, true)
	if _, err := e.svc.Newcomers.Reschedule(ctx, c.Employee.ID, date(2026, 11, 10)); err != nil {
		t.Fatal(err)
	}
	evs = e.events(t, c.Employee.ID)
	if len(evs) != 5 {
		t.Fatalf("после переноса %d событий", len(evs))
	}
	docs := eventByTitle(t, evs, "Документы")
	if docs.Status != domain.EventSent || !docs.FireAt.Equal(msk(2026, 11, 2, 10, 0)) {
		t.Fatalf("отправленное поменялось: %s %v", docs.Status, docs.FireAt)
	}
	if d0 := eventByTitle(t, evs, "С первым днём"); d0.Status != domain.EventScheduled || !d0.FireAt.Equal(msk(2026, 11, 10, 10, 0)) {
		t.Fatalf("D0 после переноса: %s %v", d0.Status, d0.FireAt)
	}
	// Пятница: итог дня 18:00 → 17:00.
	if _, err := e.svc.Newcomers.Reschedule(ctx, c.Employee.ID, date(2026, 11, 6)); err != nil {
		t.Fatal(err)
	}
	if p := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня"); !p.FireAt.Equal(msk(2026, 11, 6, 17, 0)) {
		t.Fatalf("пятница: %v", p.FireAt)
	}
}

// Поллер отправляет созревшее один раз; две параллельные отправки не дублируют событие.
func TestSchedulerTickIdempotent(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()

	*e.clock = msk(2026, 11, 3, 11, 0) // D0 после 10:00
	e.fake.Reset()
	done := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() { n, _ := e.svc.Scheduler.Tick(ctx); done <- n }()
	}
	if total := <-done + <-done; total != 1 {
		t.Fatalf("D0 10:00 отправлен %d раз", total)
	}
	if sent := e.fake.Sent(); len(sent) != 1 || sent[0].Text != "С первым днём" {
		t.Fatalf("%+v", sent)
	}
	// Кнопка «Позже» с action skip — событие пропущено, сообщение правится в итог.
	d0 := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	e.press(preStepButton + d0.ID.String() + ":1")
	if d0 = eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём"); d0.Status != domain.EventSkipped || *d0.Answer != "Позже" {
		t.Fatalf("кнопка шага: %s %v", d0.Status, d0.Answer)
	}
}

// Публикация «к активным»: новый шаг доезжает, отправленные остаются.
func TestPublishApplyActive(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()

	d, err := e.svc.Scenarios.Get(ctx, c.Employee.ScenarioID)
	if err != nil {
		t.Fatal(err)
	}
	steps := append(d.Steps, domain.ScenarioStep{DayOffset: 1, Time: "10:00", Kind: domain.KindText, Title: "Новый шаг", TextVy: "Новое", WorkHoursOnly: true})
	if _, err := e.svc.Scenarios.PutSteps(ctx, d.Scenario.ID, steps); err != nil {
		t.Fatal(err)
	}
	pub, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "добавили шаг", ApplyActive)
	if err != nil || pub.Scenario.Version != 2 || pub.Scenario.Status != domain.ScenarioPublished {
		t.Fatalf("публикация: %+v %v", pub, err)
	}
	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Новый шаг"); ev.ScenarioVersion != 2 || ev.Status != domain.EventScheduled {
		t.Fatalf("новый шаг: %+v", ev)
	}
	if ev := eventByTitle(t, evs, "Документы"); ev.ScenarioVersion != 1 || ev.Status != domain.EventSent {
		t.Fatalf("отправленный шаг: %+v", ev)
	}
	if emp := e.employee(t, c.Employee.ID); emp.ScenarioVersion != 2 {
		t.Fatalf("версия новичка %d", emp.ScenarioVersion)
	}
}
