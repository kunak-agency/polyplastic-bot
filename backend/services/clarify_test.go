package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/repositories"

	"github.com/google/uuid"
)

// Уточняющий вопрос (FEATURE_CLARIFY, clarify.go): чистые правила и путь на подделке MAX.

func TestClarifyRules(t *testing.T) {
	cases := map[string]string{
		"Люди и общение": "F4", "Задачи и нагрузка": "F6", "Условия и место": "F2", "Другое": "",
		"Не отвечает": "", "Зарплата не та": "F8", "Ничего непонятно": "F1", "Редко на месте": "",
	}
	for reason, want := range cases {
		if got := reasonCategory(reason); got != want {
			t.Errorf("reasonCategory(%q) = %q, want %q", reason, got, want)
		}
	}
	// Особый флаг варианта главнее причины; обычный — уступает причине.
	if c := clarifyCode("F5", "Люди и общение"); c != "F5" {
		t.Fatalf("F5: %q", c)
	}
	if c := clarifyCode("F6", "Люди и общение"); c != "F4" {
		t.Fatalf("причина главнее: %q", c)
	}
	if c := clarifyCode("F8", "Другое"); c != "F8" {
		t.Fatalf("флаг без причины: %q", c)
	}

	sv := &domain.Survey{Scale: domain.Scale5, Threshold: 3}
	q := domain.SurveyQuestion{Text: "Как день?", OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F2"}}}
	two, four := 2, 4
	low, ok := domain.SurveyResult{Value: &two}, domain.SurveyResult{Value: &four}
	if c := textClarify(sv, q, low, "нет пропуска", nil); c != "F2" {
		t.Fatalf("короткий текст после 2/5: %q", c)
	}
	if c := textClarify(sv, q, low, "нет пропуска уже третий день подряд", nil); c != "" {
		t.Fatalf("длинный текст: %q", c)
	}
	if c := textClarify(sv, q, ok, "всё норм", nil); c != "" {
		t.Fatalf("не негативный: %q", c)
	}
	if c := textClarify(sv, q, ok, "уволюсь", []string{"F7"}); c != "" {
		t.Fatalf("regex F7: %q", c)
	}
	if c := answerClarify(sv, q, 2, "", false); c != "F2" {
		t.Fatalf("оценка с флагом без экрана: %q", c)
	}
	if c := answerClarify(sv, q, 2, domain.ScreenReasons, false); c != "" {
		t.Fatalf("экран причин — ждём причину: %q", c)
	}

	if r := withClarify("Люди и общение", "С кем сложно", "Наставник"); r != "Люди и общение → Наставник" {
		t.Fatalf("withClarify: %q", r)
	}
	if r := withClarify("", "С кем сложно", "Коллеги"); r != "С кем сложно → Коллеги" {
		t.Fatalf("withClarify без причины: %q", r)
	}
}

// Дайджест: уточнение — своя тема-причина, основа считается как обычная кнопка.
func TestClarifyDigestTheme(t *testing.T) {
	var rows []repositories.DigestText
	for i := 0; i < 5; i++ {
		rows = append(rows, repositories.DigestText{EmployeeID: uuid.New(), Site: domain.SiteGTP, Reason: "Люди и общение → Наставник"})
	}
	set := ClusterThemes(rows, testTax(t))
	labels := map[string]int{}
	for _, th := range set.Visible {
		labels[th.Label] = th.Newcomers
	}
	if labels["причина: люди и общение"] != 5 || labels["причина: люди и общение — наставник"] != 5 {
		t.Fatalf("темы: %+v", labels)
	}
}

// clarifySurveySteps — один пульс 1–5, порог 3 (D0 18:00); flags — option_flags вопроса.
func clarifySurveySteps(flags map[string]domain.OptionFlag) []domain.ScenarioStep {
	return []domain.ScenarioStep{
		{DayOffset: 0, Time: "18:00", Kind: domain.KindSurvey, Title: "Итог дня", TextVy: "Как день?", WorkHoursOnly: true,
			Survey: &domain.Survey{Scale: domain.Scale5, Threshold: 3, Questions: []domain.SurveyQuestion{{Text: "Как прошёл день?", OptionFlags: flags}}}},
		{DayOffset: 14, Time: "10:00", Kind: domain.KindSurvey, Title: "Чек двух недель", TextVy: "Две недели", WorkHoursOnly: true,
			Survey: &domain.Survey{Scale: domain.Scale10, Threshold: 8, Questions: []domain.SurveyQuestion{
				{Text: "Задачи понятны?"}, {Text: "Руководитель доступен?"}, {Text: "Наставник помогает?"},
			}}},
	}
}

// clarifyEnv — новичок на «ты» с согласием, сценарий steps, пульс D0 отправлен машиной времени.
func clarifyEnv(t *testing.T, steps []domain.ScenarioStep, on bool) (*env, uuid.UUID, string) {
	t.Helper()
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.svc.Bot.clarify = on
	ctx := context.Background()
	d, err := e.svc.Scenarios.Create(ctx, "Уточнения", []string{domain.SiteGTP}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Newcomers.Create(ctx, NewcomerInput{FullName: "Тестов Пётр (синтетика)", Site: domain.SiteGTP, StartDate: date(2026, 11, 3), ScenarioID: d.Scenario.ID})
	if err != nil {
		t.Fatal(err)
	}
	e.connect(t, c, domain.AddressTy, true)
	if _, err := e.svc.Newcomers.TimeTravel(ctx, c.Employee.ID, 0); err != nil {
		t.Fatal(err)
	}
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	if ev.Status != domain.EventSent {
		t.Fatalf("пульс не отправлен: %s", ev.Status)
	}
	e.fake.Reset()
	return e, c.Employee.ID, ev.ID.String()
}

// clarifyMsgs — отправленные уточнения (по тексту-лиду).
func clarifyMsgs(f *max.Fake) []max.OutMessage {
	var out []max.OutMessage
	for _, m := range f.Sent() {
		if strings.Contains(m.Text, "уточни") || strings.Contains(m.Text, "Могу передать HR") {
			out = append(out, m)
		}
	}
	return out
}

func answerOf(t *testing.T, e *env, evID string, qn int) *domain.SurveyAnswer {
	t.Helper()
	a, err := e.repos.Events.Answer(context.Background(), uuid.MustParse(evID), qn)
	if err != nil || a == nil {
		t.Fatalf("ответ %d: %v %v", qn, a, err)
	}
	return a
}

// 2/5 + «Люди и общение» → «С кем именно сложно?» → «Наставник» → в ответе, в траектории, в дайджесте.
func TestClarifyPeopleMentor(t *testing.T) {
	e, empID, id := clarifyEnv(t, clarifySurveySteps(nil), true)
	e.press(preAnswer + id + ":0:2")
	e.press(preReason + id + ":0:1") // «Люди и общение»
	msgs := clarifyMsgs(e.fake)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "С кем именно сложно?") || !strings.Contains(msgs[0].Text, textClarifyLeadTy) {
		t.Fatalf("уточнение: %+v", msgs)
	}
	m := msgs[0]
	var labels []string
	for _, row := range m.Buttons {
		for _, b := range row {
			labels = append(labels, b.Text)
		}
	}
	if got := strings.Join(labels, "|"); got != "Наставник|Руководитель|Коллеги|Не хочу говорить|"+btnHuman {
		t.Fatalf("кнопки: %s", got)
	}
	lastRowIsHuman(t, m)

	e.press(m.Buttons[0][0].Payload) // «Наставник»
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.Contains(a.Answer.Message.Text, "✅ Наставник") || !strings.Contains(a.Answer.Message.Text, textClarifyThanks) || len(a.Answer.Message.Buttons) != 0 {
		t.Fatalf("после выбора: %+v", a.Answer)
	}
	if ans := answerOf(t, e, id, 1); ans.Reason != "Люди и общение → Наставник" || *ans.Value != 2 {
		t.Fatalf("ответ: %+v", ans)
	}
	ev := eventByTitle(t, e.events(t, empID), "Итог дня")
	if ev.Answer == nil || *ev.Answer != "2/5 (Люди и общение → Наставник)" {
		t.Fatalf("траектория: %q", deref(ev.Answer))
	}
	// Повторное нажатие ничего не меняет.
	e.press(m.Buttons[0][1].Payload)
	if a := lastAnswer(t, e.fake); a.Answer.Notification != "Уже записал" {
		t.Fatalf("повтор: %+v", a.Answer)
	}
	if ans := answerOf(t, e, id, 1); ans.Reason != "Люди и общение → Наставник" {
		t.Fatalf("повтор перезаписал: %q", ans.Reason)
	}
}

// «Не хочу отвечать» (у F3/F4 — «Не хочу говорить») → «Понял, не буду», причина как была.
func TestClarifyDecline(t *testing.T) {
	e, _, id := clarifyEnv(t, clarifySurveySteps(nil), true)
	e.press(preAnswer + id + ":0:1")
	e.press(preReason + id + ":0:0") // «Задачи и нагрузка» → F6
	msgs := clarifyMsgs(e.fake)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "Что перегружает?") {
		t.Fatalf("уточнение F6: %+v", msgs)
	}
	e.press(preClarifyNo + id + ":0")
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.HasSuffix(a.Answer.Message.Text, textClarifyNo) {
		t.Fatalf("отказ: %+v", a.Answer)
	}
	if ans := answerOf(t, e, id, 1); ans.Reason != "Задачи и нагрузка" {
		t.Fatalf("причина после отказа: %q", ans.Reason)
	}
}

// F5 у варианта — уточнения нет (сигнал варианта сам ведёт к «Передать»).
func TestClarifyF5Silent(t *testing.T) {
	e, _, id := clarifyEnv(t, clarifySurveySteps(map[string]domain.OptionFlag{"2": {Code: "F5", Severity: "high"}}), true)
	e.press(preAnswer + id + ":0:2")
	e.press(preReason + id + ":0:1")
	if msgs := clarifyMsgs(e.fake); len(msgs) != 0 {
		t.Fatalf("уточнение при F5: %+v", msgs)
	}
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "С кем именно сложно?") {
			t.Fatalf("вопрос при F5: %q", m.Text)
		}
	}
}

// Без флага — поведение прежнее.
func TestClarifyOff(t *testing.T) {
	e, empID, id := clarifyEnv(t, clarifySurveySteps(nil), false)
	e.press(preAnswer + id + ":0:2")
	e.press(preReason + id + ":0:1")
	if msgs := clarifyMsgs(e.fake); len(msgs) != 0 {
		t.Fatalf("уточнение без флага: %+v", msgs)
	}
	ev := eventByTitle(t, e.events(t, empID), "Итог дня")
	if *ev.Answer != "2/5 (Люди и общение)" || ev.Survey.Results[0].Clarify != "" {
		t.Fatalf("без флага: %q %+v", deref(ev.Answer), ev.Survey.Results)
	}
}

// Лимиты: 1 в сутки, 2 на опрос, не во время диалога с HR.
func TestClarifyLimits(t *testing.T) {
	e, empID, id := clarifyEnv(t, clarifySurveySteps(nil), true)
	ctx := context.Background()
	e.press(preAnswer + id + ":0:2")
	e.press(preReason + id + ":0:1")
	if n := len(clarifyMsgs(e.fake)); n != 1 {
		t.Fatalf("первое уточнение: %d", n)
	}

	// Чек D+14 в тот же день: в сутки уже было.
	e.fake.Reset()
	if _, err := e.svc.Newcomers.TimeTravel(ctx, empID, 14); err != nil {
		t.Fatal(err)
	}
	check := eventByTitle(t, e.events(t, empID), "Чек двух недель").ID.String()
	e.press(preAnswer + check + ":0:5")
	e.press(preReason + check + ":0:1")
	if n := len(clarifyMsgs(e.fake)); n != 0 {
		t.Fatalf("второе за сутки: %d", n)
	}

	// Через сутки — можно; ещё через сутки — уже два в этом опросе.
	*e.clock = e.clock.Add(25 * time.Hour)
	e.press(preAnswer + check + ":1:5")
	e.press(preReason + check + ":1:2") // «Условия и место» → F2
	if msgs := clarifyMsgs(e.fake); len(msgs) != 1 || !strings.Contains(msgs[0].Text, "Чего не хватает?") {
		t.Fatalf("через сутки: %+v", msgs)
	}
	ev := eventByTitle(t, e.events(t, empID), "Чек двух недель")
	if ev.Survey.Results[0].Clarify != "" || ev.Survey.Results[1].Clarify != "F2" {
		t.Fatalf("отметки: %+v", ev.Survey.Results)
	}

	// Живой диалог с HR: не спрашиваем, даже когда сутки прошли.
	*e.clock = e.clock.Add(25 * time.Hour)
	if err := e.repos.Tickets.Create(ctx, &domain.Ticket{EmployeeID: empID, Category: "question", Priority: "P2", Status: domain.TicketHumanActive, Route: "hr", Source: "button"}); err != nil {
		t.Fatal(err)
	}
	e.fake.Reset()
	e.press(preAnswer + check + ":2:5")
	e.press(preReason + check + ":2:1")
	if n := len(clarifyMsgs(e.fake)); n != 0 {
		t.Fatalf("при human_active: %d", n)
	}
}

// Лимит на опрос: третьего уточнения в одном опросе нет и через сутки.
func TestClarifyPerSurvey(t *testing.T) {
	e, empID, _ := clarifyEnv(t, clarifySurveySteps(nil), true)
	ctx := context.Background()
	if _, err := e.svc.Newcomers.TimeTravel(ctx, empID, 14); err != nil {
		t.Fatal(err)
	}
	check := eventByTitle(t, e.events(t, empID), "Чек двух недель").ID.String()
	for q, want := range []int{1, 1, 0} {
		e.fake.Reset()
		qs := string(rune('0' + q))
		e.press(preAnswer + check + ":" + qs + ":5")
		e.press(preReason + check + ":" + qs + ":1")
		if n := len(clarifyMsgs(e.fake)); n != want {
			t.Fatalf("вопрос %d: уточнений %d, want %d", q+1, n, want)
		}
		*e.clock = e.clock.Add(25 * time.Hour)
	}
}
