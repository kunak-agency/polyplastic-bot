package services

import (
	"context"
	"testing"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Статистика кнопок и последствия публикации (api_contract.md, v4): чистые тесты подсчёта и
// diff, интеграционный — через Postgres (TEST_DATABASE_URL).

func statsEvent(step uuid.UUID, status string, answer string, sentAt time.Time, wait time.Duration) domain.ScheduledEvent {
	id, _ := uuid.NewV7()
	ev := domain.ScheduledEvent{ID: id, StepID: step, Status: status, SentAt: &sentAt, UpdatedAt: sentAt.Add(wait)}
	if answer != "" {
		ev.Answer = &answer
	}
	return ev
}

func TestBuildStepStatsThreshold(t *testing.T) {
	step := domain.ScenarioStep{ID: uuid.New(), Buttons: domain.ButtonList{{Label: "Всё есть"}, {Label: "Чего-то нет"}}}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	var evs []domain.ScheduledEvent
	// 4 ответа — меньше порога: долей нет.
	for i := 0; i < 3; i++ {
		evs = append(evs, statsEvent(step.ID, domain.EventAnswered, "Всё есть", at, time.Duration(i+1)*time.Minute))
	}
	missing := statsEvent(step.ID, domain.EventAnswered, "Чего-то нет", at, 10*time.Minute)
	evs = append(evs, missing, statsEvent(step.ID, domain.EventSent, "", at, 0))
	evs = append(evs, domain.ScheduledEvent{StepID: step.ID, Status: domain.EventScheduled})

	got := BuildStepStats([]domain.ScenarioStep{step}, evs, map[uuid.UUID]int{missing.ID: 1})[0]
	if got.Sent != 5 || got.Answered != 4 || got.NoAnswer != 1 {
		t.Fatalf("sent/answered/no_answer = %d/%d/%d", got.Sent, got.Answered, got.NoAnswer)
	}
	if got.Buttons[1].Picked != 1 || got.Buttons[1].Tickets != 1 || got.Buttons[1].Share != nil {
		t.Fatalf("кнопка «Чего-то нет»: %+v", got.Buttons[1])
	}
	if got.MedianAnswerS == nil || *got.MedianAnswerS != 150 { // 1, 2, 3, 10 мин → (2+3)/2
		t.Fatalf("медиана: %v", got.MedianAnswerS)
	}

	// Пятый ответ — доли появляются.
	evs = append(evs, statsEvent(step.ID, domain.EventAnswered, "Чего-то нет", at, time.Minute))
	got = BuildStepStats([]domain.ScenarioStep{step}, evs, nil)[0]
	if got.Buttons[1].Share == nil || *got.Buttons[1].Share != 0.4 {
		t.Fatalf("доля «Чего-то нет»: %v", got.Buttons[1].Share)
	}
}

func TestDiffStepsAndRemovedBranches(t *testing.T) {
	branch := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Передадим HR"}}, Then: "continue"}
	a := domain.ScenarioStep{ID: uuid.New(), Title: "Доступы", TextVy: "Всё есть?",
		Buttons: domain.ButtonList{{Label: "Да"}, {Label: "Чего-то нет", Branch: branch}}}
	b := domain.ScenarioStep{ID: uuid.New(), Title: "Факты", TextVy: "Три факта"}
	c := domain.ScenarioStep{ID: uuid.New(), Title: "Новый", TextVy: "Новый шаг"}

	a2 := a
	a2.Buttons = domain.ButtonList{{Label: "Да"}, {Label: "Нет доступа"}}
	ch := DiffSteps([]domain.ScenarioStep{a, b}, []domain.ScenarioStep{a2, c})
	if len(ch) != 3 {
		t.Fatalf("изменений %d: %+v", len(ch), ch)
	}
	if ch[0].Change != ChangeChanged || len(ch[0].ButtonsRemoved) != 1 || ch[0].ButtonsRemoved[0] != "Чего-то нет" {
		t.Fatalf("a: %+v", ch[0])
	}
	if ch[1].Change != ChangeAdded || ch[2].Change != ChangeRemoved {
		t.Fatalf("порядок: %+v", ch)
	}
	if n := removedBranches(a, a2, true); n != 1 {
		t.Fatalf("удалённых веток %d", n)
	}
	if n := removedBranches(a, domain.ScenarioStep{}, false); n != 1 {
		t.Fatalf("шаг удалён: %d", n)
	}
	// Правка только текста ветки: кнопки на месте, ветка есть — не удалена.
	a3 := a
	a3.Buttons = domain.ButtonList{{Label: "Да"}, {Label: "Чего-то нет", Branch: &domain.Branch{Then: "end_block"}}}
	if n := removedBranches(a, a3, true); n != 0 {
		t.Fatalf("ветка на месте: %d", n)
	}
	if f := changedFields(a, a3); len(f) != 1 || f[0] != "branches" {
		t.Fatalf("поля: %v", f)
	}
}

func TestPublishPreviewAndStatsDB(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, domain.SiteOf(domain.SiteGTP).Loc)
	e := newEnv(t, now)
	ctx := context.Background()
	branch := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Передадим HR"}},
		Actions: []domain.Action{{Type: "escalate", Category: "F2", Route: "hr"}}, Then: "continue"}
	steps := []domain.ScenarioStep{
		{DayOffset: 1, Time: "10:00", Kind: domain.KindText, Title: "Доступы", TextTy: "Всё есть?", TextVy: "Всё есть?", WorkHoursOnly: true,
			Buttons: domain.ButtonList{{Label: "Всё есть"}, {Label: "Чего-то нет", Branch: branch}}},
		{DayOffset: 2, Time: "10:00", Kind: domain.KindSurvey, Title: "Пульс", TextVy: "Как дела?", WorkHoursOnly: true,
			Survey: &domain.Survey{Scale: domain.Scale5, Questions: []domain.SurveyQuestion{{Text: "Как дела?"}}}},
	}
	d, err := e.svc.Scenarios.Create(ctx, "Статистика", nil, steps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	pub, _ := e.svc.Scenarios.Get(ctx, d.Scenario.ID)
	access, pulse := pub.Steps[0], pub.Steps[1]

	// Двое новичков: у первого «Доступы» ждёт ответа, а опрос открыт; второй нажал «Чего-то нет».
	sent := now.Add(-time.Hour)
	for i, answer := range []string{"", "Чего-то нет"} {
		emp := domain.Employee{FullName: "Синтетика " + string(rune('A'+i)), Site: domain.SiteGTP,
			StartDate: now, ScenarioID: d.Scenario.ID, ScenarioVersion: 1, Connection: domain.ConnConnected,
			Risk: domain.RiskGreen}
		if err := e.repos.Employees.Create(ctx, &emp); err != nil {
			t.Fatal(err)
		}
		ev := domain.ScheduledEvent{ID: domain.EventID(emp.ID, access.ID, 1), EmployeeID: emp.ID, StepID: access.ID,
			ScenarioVersion: 1, Kind: domain.KindText, FireAt: sent, SentAt: &sent, Status: domain.EventSent}
		if answer != "" {
			ev.Status, ev.Answer = domain.EventAnswered, &answer
		}
		evs := []domain.ScheduledEvent{ev}
		if i == 0 {
			evs = append(evs, domain.ScheduledEvent{ID: domain.EventID(emp.ID, pulse.ID, 1), EmployeeID: emp.ID,
				StepID: pulse.ID, ScenarioVersion: 1, Kind: domain.KindSurvey, FireAt: sent, SentAt: &sent, Status: domain.EventSent})
		}
		if err := e.repos.Events.CreateMissing(ctx, evs); err != nil {
			t.Fatal(err)
		}
	}

	// Черновик: у «Доступов» кнопку с веткой переименовали.
	draft := pub.Steps
	draft[0].Buttons = domain.ButtonList{{Label: "Всё есть"}, {Label: "Нет доступа"}}
	if _, err := e.svc.Scenarios.PutSteps(ctx, d.Scenario.ID, draft); err != nil {
		t.Fatal(err)
	}
	p, err := e.svc.Scenarios.PublishPreview(ctx, d.Scenario.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActiveNewcomers != 2 || p.InsideSurvey != 1 || p.RemovedBranchesLive != 1 || p.Recommend != ApplyNew || p.NextVersion != 2 {
		t.Fatalf("preview: %+v", p)
	}
	if len(p.Changes) != 1 || p.Changes[0].StepID != access.ID {
		t.Fatalf("changes: %+v", p.Changes)
	}

	st, err := e.svc.Scenarios.Stats(ctx, d.Scenario.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if st.Steps[0].Sent != 2 || st.Steps[0].Answered != 1 {
		t.Fatalf("stats: %+v", st.Steps[0])
	}
	if st, _ := e.svc.Scenarios.Stats(ctx, d.Scenario.ID, domain.SiteGrPP); st.Steps[0].Sent != 0 {
		t.Fatalf("фильтр площадки: %+v", st.Steps[0])
	}
	if _, err := e.svc.Scenarios.Stats(ctx, d.Scenario.ID, "Mars"); err == nil {
		t.Fatal("чужая площадка должна давать 422")
	}
}
