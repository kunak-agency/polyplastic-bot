package services

import (
	"context"
	"strings"
	"testing"

	"main/config"
	"main/domain"
)

// Итерация 24.10, агент L: вопрос держит поток не дольше 48 ч; «Пропустить» в надже на
// необязательный шаг-вопрос. Синтетика, подделка MAX, TEST_DATABASE_URL.

// Обязательный вопрос без ответа 48 ч: следующая реплика блока уходит, вопрос остаётся
// открытым («не отвечен» в карточке, наджи и ручное «Напомнить» — по нему), у рекрутера —
// «Не отвечает боту»; ответ новичка закрывает вопрос как обычно.
func TestQuestionHoldMax48h(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 9, 0))
	e.features(config.Features{NudgesDefault: true})
	q := txt(0, "10:00", "Доступы", "Пропуск выдали?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"})
	q.Required = true
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{q, txt(0, "10:01", "Схема", "Вот схема проезда")})
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()
	id := c.Employee.ID
	e.tick(t, msk(2026, 11, 3, 10, 0))
	if ev := eventByTitle(t, e.events(t, id), "Схема"); ev.Status != domain.EventScheduled {
		t.Fatalf("вопрос держит блок: %s", ev.Status)
	}
	// 47 ч 59 мин — ещё держит
	e.tick(t, msk(2026, 11, 5, 9, 59))
	if ev := eventByTitle(t, e.events(t, id), "Схема"); ev.Status != domain.EventScheduled {
		t.Fatalf("раньше 48 ч: %s", ev.Status)
	}
	e.fake.Reset()
	e.tick(t, msk(2026, 11, 5, 10, 0))
	if !hasText(e.fake, "Вот схема проезда") {
		t.Fatalf("48 ч — поток дальше: %q", sentTexts(e.fake))
	}
	qe := eventByTitle(t, e.events(t, id), "Доступы")
	if qe.Status != domain.EventSent || qe.Waiting != domain.WaitAnswer || qe.Answer == nil || !strings.HasPrefix(*qe.Answer, "не отвечен") {
		t.Fatalf("вопрос открыт, «не отвечен»: %s %q %v", qe.Status, qe.Waiting, qe.Answer)
	}
	if open, _ := e.repos.Events.OpenQuestions(ctx, id); len(open) != 1 || open[0].ID != qe.ID {
		t.Fatalf("открытые вопросы: %+v", open)
	}
	if emp := e.employee(t, id); emp.UnresponsiveSince == nil {
		t.Fatal("нет статуса «не отвечает»")
	}
	d, err := e.svc.Users.RecruiterDashboard(ctx, DashboardScope{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range d.Today {
		if r.NewcomerID == id && r.Type == RowSilent {
			found = strings.Contains(strings.Join(r.Actions, ","), ActContact)
		}
	}
	if !found {
		t.Fatalf("«Сегодня» рекрутера без «не отвечает» и «Связаться»: %+v", d.Today)
	}
	// ручное «Напомнить» — по этому же вопросу
	*e.clock = msk(2026, 11, 5, 11, 0)
	e.fake.Reset()
	res, err := e.svc.Newcomers.Nudge(ctx, id, [16]byte{})
	if err != nil || !res.Sent || res.Nudge.EventID == nil || *res.Nudge.EventID != qe.ID {
		t.Fatalf("ручной надж: %+v %v", res, err)
	}
	if m := lastSent(t, e.fake); !strings.Contains(m.Text, "Пропуск выдали?") || findPayload(m, btnSkip) != "" {
		t.Fatalf("надж на обязательный вопрос — без «Пропустить»: %q %v", m.Text, labels(m))
	}
	// ответ закрывает вопрос, отметка «не отвечен» не остаётся
	e.press(preStepButton + qe.ID.String() + ":0")
	qe = eventByTitle(t, e.events(t, id), "Доступы")
	if qe.Status != domain.EventAnswered || qe.Answer == nil || *qe.Answer != "Да" {
		t.Fatalf("ответ: %s %v", qe.Status, qe.Answer)
	}
}

// Без FEATURE_NUDGES_DEFAULT вопрос держит поток, как раньше.
func TestQuestionHoldWithoutFlag(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 9, 0))
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Доступы", "Пропуск выдали?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"}),
		txt(0, "10:01", "Схема", "Вот схема проезда"),
	})
	e.connect(t, c, domain.AddressVy, true)
	e.tick(t, msk(2026, 11, 3, 10, 0))
	e.tick(t, msk(2026, 11, 6, 10, 0))
	if ev := eventByTitle(t, e.events(t, c.Employee.ID), "Схема"); ev.Status != domain.EventScheduled {
		t.Fatalf("без флага: %s", ev.Status)
	}
}

// Надж на необязательный шаг-вопрос без своей кнопки skip — «Пропустить» перед «Спросить
// человека»; нажатие — skipped, следующая реплика блока.
func TestNudgeSkipButton(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 9, 0))
	e.features(config.Features{NudgesDefault: true})
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Наставник", "С наставником познакомились?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Пока нет"}),
		txt(0, "10:01", "Схема", "Вот схема проезда"),
	})
	e.connect(t, c, domain.AddressVy, true)
	id := c.Employee.ID
	e.tick(t, msk(2026, 11, 3, 10, 0))
	e.fake.Reset()
	if n := e.tick(t, msk(2026, 11, 3, 14, 0)); n != 1 {
		t.Fatalf("напоминаний %d", n)
	}
	m := lastSent(t, e.fake)
	rows := labels(m)
	if len(rows) < 3 || rows[len(rows)-2][0] != btnSkip || rows[len(rows)-1][0] != btnHuman {
		t.Fatalf("ряды наджа: %v", rows)
	}
	skip := findPayload(m, btnSkip)
	e.fake.Reset()
	e.press(skip)
	q := eventByTitle(t, e.events(t, id), "Наставник")
	if q.Status != domain.EventSkipped || q.Waiting != "" || q.Answer == nil || *q.Answer != answerNudgeSkipped {
		t.Fatalf("пропуск: %s %q %v", q.Status, q.Waiting, q.Answer)
	}
	if !hasText(e.fake, "Вот схема проезда") {
		t.Fatalf("после «Пропустить» — следующая реплика: %q", sentTexts(e.fake))
	}
	ans := e.fake.Answers()
	if len(ans) == 0 || ans[len(ans)-1].Answer.Message == nil || !strings.HasSuffix(ans[len(ans)-1].Answer.Message.Text, "✅ "+labelNudgeSkipped) {
		t.Fatalf("надж правится в итог: %+v", ans)
	}
	// второе нажатие — «Уже записал»
	e.press(skip)
	if ans = e.fake.Answers(); ans[len(ans)-1].Answer.Notification != "Уже записал" {
		t.Fatalf("повтор: %+v", ans[len(ans)-1])
	}
	if open, _ := e.repos.Events.OpenQuestions(context.Background(), id); len(open) != 0 {
		t.Fatalf("открытых вопросов %d", len(open))
	}
}

// Свой skip у шага — второй «Пропустить» не добавляется; правило nudgeSkippable.
func TestNudgeSkippable(t *testing.T) {
	q := txt(0, "10:00", "Q", "?", domain.StepButton{Label: "Да"})
	if !nudgeSkippable(q) {
		t.Fatal("необязательный вопрос")
	}
	req := q
	req.Required = true
	own := txt(0, "10:00", "Q", "?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Позже", Action: "skip"})
	plain := txt(0, "10:00", "T", "текст")
	sv := domain.ScenarioStep{Kind: domain.KindSurvey, Survey: &domain.Survey{Questions: []domain.SurveyQuestion{{Text: "?"}}}}
	for name, st := range map[string]domain.ScenarioStep{"обязательный": req, "свой skip": own, "текст": plain, "опрос": sv} {
		if nudgeSkippable(st) {
			t.Fatalf("%s: «Пропустить» не нужен", name)
		}
	}
}
