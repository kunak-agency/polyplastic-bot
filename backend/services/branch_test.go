package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"

	"github.com/google/uuid"
)

// Исполнение блока, ветки, действия, повопросные опросы (api_contract.md v4). Синтетика,
// подделка MAX, TEST_DATABASE_URL.

func (e *env) launchSteps(t *testing.T, start time.Time, steps []domain.ScenarioStep, cmds ...domain.Command) (*NewcomerCard, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	d, err := e.svc.Scenarios.Create(ctx, "Ветки", []string{domain.SiteGTP}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) > 0 {
		list := domain.CommandList(cmds)
		if _, err := e.svc.Scenarios.Patch(ctx, d.Scenario.ID, &list, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Newcomers.Create(ctx, NewcomerInput{
		FullName: "Веткин Иван (синтетика)", Site: domain.SiteGTP, StartDate: start, ScenarioID: d.Scenario.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, d.Scenario.ID
}

func txt(day int, hhmm, title, text string, btns ...domain.StepButton) domain.ScenarioStep {
	return domain.ScenarioStep{DayOffset: day, Time: hhmm, Kind: domain.KindText, Title: title, TextVy: text, TextTy: text, Buttons: btns}
}

func sentTexts(f *max.Fake) []string {
	var out []string
	for _, m := range f.Sent() {
		out = append(out, m.Text)
	}
	return out
}

func hasText(f *max.Fake, s string) bool {
	for _, t := range sentTexts(f) {
		if strings.Contains(t, s) {
			return true
		}
	}
	return false
}

func findPayload(m max.OutMessage, label string) string {
	for _, row := range m.Buttons {
		for _, b := range row {
			if b.Text == label {
				return b.Payload
			}
		}
	}
	return ""
}

// Вторник 3 ноября 2026 — день выхода; 10:05 — блок 10:00 созрел.
var (
	flowStart = date(2026, 11, 3)
	flowNow   = msk(2026, 11, 3, 10, 5)
)

// Вопрос держит поток; ответ в 23:00 — следующая реплика сразу (тихие часы не держат реакцию).
func TestBlockQuestionHoldsFlowAnswerAtNight(t *testing.T) {
	e := newEnv(t, flowNow)
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Готов к первому дню?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"}),
		txt(0, "10:01", "Реплика", "Следующая реплика"), // «10:01» — тот же блок
	})
	e.connect(t, c, domain.AddressVy, true)
	if !hasText(e.fake, "Готов к первому дню?") || hasText(e.fake, "Следующая реплика") {
		t.Fatalf("вопрос должен держать поток: %q", sentTexts(e.fake))
	}
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
	if q.Waiting != domain.WaitAnswer || q.Block != eventByTitle(t, e.events(t, c.Employee.ID), "Реплика").Block {
		t.Fatalf("ожидание/блок: %q %q", q.Waiting, q.Block)
	}
	// Такты не отпускают вопрос.
	if n, _ := e.svc.Scheduler.Tick(context.Background()); n != 0 {
		t.Fatalf("такт отправил %d при открытом вопросе", n)
	}
	*e.clock = msk(2026, 11, 3, 23, 0)
	e.press(preStepButton + q.ID.String() + ":1")
	if a := lastAnswer(t, e.fake); a.Answer.Message == nil || !strings.HasSuffix(a.Answer.Message.Text, "✅ Нет") {
		t.Fatalf("правка вопроса: %+v", a.Answer)
	}
	if m := lastSent(t, e.fake); m.Text != "Следующая реплика" {
		t.Fatalf("после ответа в 23:00 — реплика сразу: %q", m.Text)
	}
}

// Нет ответа, пришло время следующего блока: необязательные — skipped, обязательные — «Что вы
// пропустили» перед новым блоком.
func TestBlockNoAnswerNextBlock(t *testing.T) {
	e := newEnv(t, flowNow)
	req := txt(0, "10:00", "Обязательно", "Возьмите паспорт")
	req.Required = true
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Всё понятно?", domain.StepButton{Label: "Да"}),
		txt(0, "10:00", "Необязательно", "Факт дня"),
		req,
		txt(0, "15:00", "Вечер", "Как прошёл обед?"),
	})
	e.connect(t, c, domain.AddressVy, true)
	if hasText(e.fake, "Факт дня") {
		t.Fatal("реплика ушла до ответа")
	}
	e.fake.Reset()
	*e.clock = msk(2026, 11, 3, 15, 1)
	if _, err := e.svc.Scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	texts := sentTexts(e.fake)
	if len(texts) != 2 || !strings.HasPrefix(texts[0], textMissedHead) || !strings.Contains(texts[0], "Возьмите паспорт") || texts[1] != "Как прошёл обед?" {
		t.Fatalf("сводка и новый блок: %q", texts)
	}
	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Необязательно"); ev.Status != domain.EventSkipped || *ev.Answer != skipNoAnswer {
		t.Fatalf("необязательный: %s %v", ev.Status, deref(ev.Answer))
	}
	if ev := eventByTitle(t, evs, "Обязательно"); ev.Status != domain.EventSent || *ev.Answer != sentInMissed {
		t.Fatalf("обязательный: %s %v", ev.Status, deref(ev.Answer))
	}
}

// Ветка: 2 сообщения, терминальные кнопки; set_field, set_flag; open_free_text → текст →
// обращение; handoff → human_active; затем следующая реплика.
func TestBranchTerminalButtonsAndActions(t *testing.T) {
	e := newEnv(t, flowNow)
	yes := &domain.Branch{
		Messages: []domain.BranchMessage{
			{TextVy: "Отлично, {Имя}!", TextTy: "Отлично, {Имя}!"},
			{TextVy: "Если что-то не так — скажи", TextTy: "Если что-то не так — скажи", Buttons: []domain.StepButton{
				{Label: "Передать через Корсика", Branch: &domain.Branch{Actions: []domain.Action{
					{Type: domain.ActionOpenFreeText, Prompt: "Опиши проблему"},
					{Type: domain.ActionEscalate, Category: "F2", Route: "hr", NotifyManager: true},
				}}},
				{Label: "Связаться с рекрутером", Branch: &domain.Branch{Actions: []domain.Action{{Type: domain.ActionHandoff, To: "recruiter"}}}},
			}},
		},
		Actions: []domain.Action{
			{Type: domain.ActionSetField, Field: "address_form", Value: domain.AddressTy},
			{Type: domain.ActionSetFlag, Flag: "said_yes"},
		},
	}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Готов?", domain.StepButton{Label: "Да", Branch: yes}, domain.StepButton{Label: "Нет"}),
		txt(0, "10:00", "Реплика", "Весь день проведём вместе"),
	})
	e.connect(t, c, domain.AddressVy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
	e.fake.Reset()
	e.press(preStepButton + q.ID.String() + ":0")
	texts := sentTexts(e.fake)
	if len(texts) != 2 || texts[0] != "Отлично, Иван!" || texts[1] != "Если что-то не так — скажи" {
		t.Fatalf("сообщения ветки: %q", texts)
	}
	term := e.fake.Sent()[1]
	pay := findPayload(term, "Передать через Корсика")
	if !strings.HasPrefix(pay, preTerminal+q.ID.String()+":0:1:0") || len(pay) > 1024 {
		t.Fatalf("payload терминальной: %q", pay)
	}
	emp := e.employee(t, c.Employee.ID)
	if emp.AddressForm == nil || *emp.AddressForm != domain.AddressTy || !emp.Flags["said_yes"] {
		t.Fatalf("set_field/set_flag: %+v %v", emp.AddressForm, emp.Flags)
	}
	if hasText(e.fake, "Весь день") {
		t.Fatal("терминальные кнопки держат поток, реплика не должна уйти")
	}

	e.press(pay)
	if m := lastSent(t, e.fake); m.Text != "Опиши проблему" {
		t.Fatalf("open_free_text: %q", m.Text)
	}
	e.say("Пропуск не выдали, охрана не пускает")
	tks := e.tickets(t, c.Employee.ID)
	if len(tks) != 1 || tks[0].Category != "F2" || tks[0].Quote == nil || !tks[0].NotifyManager {
		t.Fatalf("обращение из своего текста: %+v", tks)
	}
	if !hasText(e.fake, "Весь день проведём вместе") {
		t.Fatalf("после текста — следующая реплика: %q", sentTexts(e.fake))
	}

	e.press(findPayload(term, "Связаться с рекрутером"))
	tks = e.tickets(t, c.Employee.ID)
	var handoff *domain.Ticket
	for i := range tks {
		if tks[i].Route == "recruiter" {
			handoff = &tks[i]
		}
	}
	if handoff == nil || handoff.Status != domain.TicketHumanActive {
		t.Fatalf("handoff: %+v", tks)
	}
}

// end_block и снимок версии: ветка, удалённая в v2, срабатывает у сообщения v1.
func TestBranchEndBlockFromSnapshot(t *testing.T) {
	e := newEnv(t, flowNow)
	ctx := context.Background()
	stop := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Хорошо, вернёмся позже", TextTy: "Хорошо, вернёмся позже"}}, Then: domain.ThenEndBlock}
	c, scID := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Продолжим?", domain.StepButton{Label: "Позже", Branch: stop}, domain.StepButton{Label: "Да"}),
		txt(0, "10:00", "Два", "Реплика два"),
		txt(0, "10:00", "Три", "Реплика три"),
	})
	e.connect(t, c, domain.AddressVy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")

	// v2: ветку удалили.
	d, err := e.svc.Scenarios.Get(ctx, scID)
	if err != nil {
		t.Fatal(err)
	}
	steps := d.Steps
	steps[0].Buttons = domain.ButtonList{{Label: "Позже"}, {Label: "Да"}}
	if _, err := e.svc.Scenarios.PutSteps(ctx, scID, steps); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, scID, "v2 без ветки", ApplyNew); err != nil {
		t.Fatal(err)
	}

	e.press(preStepButton + q.ID.String() + ":0")
	if m := lastSent(t, e.fake); m.Text != "Хорошо, вернёмся позже" {
		t.Fatalf("ветка из снимка v1: %q", m.Text)
	}
	evs := e.events(t, c.Employee.ID)
	for _, title := range []string{"Два", "Три"} {
		if ev := eventByTitle(t, evs, title); ev.Status != domain.EventSkipped || *ev.Answer != skipEndBlock {
			t.Fatalf("%s после end_block: %s %v", title, ev.Status, deref(ev.Answer))
		}
	}
}

// hr_task не отправляется; flag_required — held до флага, флаг отпускает шаг.
func TestHRTaskAndFlagRequired(t *testing.T) {
	e := newEnv(t, flowNow)
	ctx := context.Background()
	hr := txt(0, "10:00", "Письмо HR", "Письмо кандидату")
	hr.HRTask = true
	flag := "recruiter_enabled"
	byFlag := txt(0, "10:00", "По отмашке", "Расскажи о своём опыте")
	byFlag.FlagRequired = &flag
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{hr, byFlag, txt(0, "10:00", "Последний", "До встречи")})
	e.connect(t, c, domain.AddressVy, true)
	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Письмо HR"); ev.Status != domain.EventSkipped || *ev.Answer != skipHRTask {
		t.Fatalf("hr_task: %s", ev.Status)
	}
	if ev := eventByTitle(t, evs, "По отмашке"); ev.Status != domain.EventHeld {
		t.Fatalf("flag_required без флага: %s", ev.Status)
	}
	if hasText(e.fake, "Письмо кандидату") || hasText(e.fake, "Расскажи") || !hasText(e.fake, "До встречи") {
		t.Fatalf("отправлено: %q", sentTexts(e.fake))
	}
	if _, err := e.svc.SetNewcomerFlag(ctx, c.Employee.ID, flag, true); err != nil {
		t.Fatal(err)
	}
	if ev := eventByTitle(t, e.events(t, c.Employee.ID), "По отмашке"); ev.Status != domain.EventSent || !hasText(e.fake, "Расскажи о своём опыте") {
		t.Fatalf("после флага: %s", ev.Status)
	}
}

// advance: по умолчанию «Дальше →»; явный auto — не больше двух подряд и пауза «печатает».
func TestAdvanceContinueAndAuto(t *testing.T) {
	e := newEnv(t, flowNow)
	var steps []domain.ScenarioStep
	for _, s := range []string{"Раз", "Два", "Три", "Четыре", "Пять"} {
		steps = append(steps, txt(0, "10:00", s, "Сообщение "+s))
	}
	c, _ := e.launchSteps(t, flowStart, steps)
	e.connect(t, c, domain.AddressVy, true)
	m := lastSent(t, e.fake)
	if m.Text != "Сообщение Раз" || findPayload(m, continueLabel) == "" || hasText(e.fake, "Сообщение Два") {
		t.Fatalf("первое — с «Дальше →», остальные ждут: %q", sentTexts(e.fake))
	}
	e.press(findPayload(m, continueLabel))
	if a := lastAnswer(t, e.fake); a.Answer.Message == nil || len(a.Answer.Message.Buttons) != 0 || strings.Contains(a.Answer.Message.Text, "✅") {
		t.Fatalf("«Дальше» убирает клавиатуру без ✅: %+v", a.Answer.Message)
	}
	if m := lastSent(t, e.fake); m.Text != "Сообщение Два" {
		t.Fatalf("после «Дальше»: %q", m.Text)
	}

	// auto: пачка из пяти — не больше двух подряд, вторая получает «Дальше →».
	e2 := newEnv(t, flowNow)
	var slept []time.Duration
	e2.svc.Scheduler.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	auto := domain.AdvanceAuto
	var autos []domain.ScenarioStep
	for _, s := range []string{"А", "Б", "В", "Г", "Д"} {
		st := txt(0, "10:00", s, "Авто "+s)
		st.Advance = &auto
		autos = append(autos, st)
	}
	c2, _ := e2.launchSteps(t, flowStart, autos)
	e2.connect(t, c2, domain.AddressVy, true)
	var got []max.OutMessage
	for _, m := range e2.fake.Sent() {
		if strings.HasPrefix(m.Text, "Авто ") {
			got = append(got, m)
		}
	}
	if len(got) != 2 || findPayload(got[0], continueLabel) != "" || findPayload(got[1], continueLabel) == "" {
		t.Fatalf("auto: %d сообщений", len(got))
	}
	if len(slept) == 0 || slept[0] < time.Second {
		t.Fatalf("пауза «печатает»: %v", slept)
	}
}

type fakeMedia struct{ refs []domain.MediaRef }

func (f *fakeMedia) Attachments(_ context.Context, refs []domain.MediaRef, site string) ([]max.AttachmentRef, []string) {
	f.refs = append(f.refs, refs...)
	return []max.AttachmentRef{{Kind: max.UploadFile, Token: "tok-" + refs[0].Name + "-" + site}}, nil
}
func (f *fakeMedia) ForgetTokens(context.Context, []string) {}

// Повопросный опрос: ветка варианта между вопросами, сигнал варианта, multi с «Готово»,
// обязательный вопрос, причина при «нет», правила после опроса.
func TestSurveyBranchesMultiOutcomes(t *testing.T) {
	e := newEnv(t, flowNow)
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{
		{Text: "Как настроение?", Type: "choice", Options: []string{"Отлично", "Безразлично", "Устал"},
			Branches:    map[string]*domain.Branch{"2": {Messages: []domain.BranchMessage{{TextVy: "Жаль это слышать", TextTy: "Жаль это слышать"}}}},
			OptionFlags: map[string]domain.OptionFlag{"3": {Code: "F6", Severity: "medium"}}},
		{Text: "Кто помогает?", Type: "multi", Options: []string{"Наставник", "Никто!"}, Other: true},
		{Text: "Задачи понятны?", Type: "yesno", Comment: "on_no", Required: true},
	}, Outcomes: []domain.Outcome{
		{If: []domain.OutcomeCond{{Q: 1, In: []int{2}}}, Group: "g1", Messages: []domain.BranchMessage{{TextVy: "Итог: безразлично"}}},
		{If: []domain.OutcomeCond{{Q: 1, In: []int{2, 3}}}, Group: "g1", Messages: []domain.BranchMessage{{TextVy: "Итог: не выводится (ЛИБО)"}}},
		{If: []domain.OutcomeCond{{Q: 2, Contains: []int{2}}}, Messages: []domain.BranchMessage{{TextVy: "Итог: никто не помогает"}}},
	}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос", TextVy: "Пара вопросов", Survey: sv},
		txt(0, "10:00", "После", "Спасибо, дальше по плану"),
	})
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Опрос")
	id := ev.ID.String()
	if hasText(e.fake, "Спасибо, дальше") {
		t.Fatal("опрос держит поток")
	}

	e.fake.Reset()
	e.press(preAnswer + id + ":0:2")
	a := lastAnswer(t, e.fake)
	if strings.Contains(a.Answer.Message.Text, "Вопрос 2") || len(a.Answer.Message.Buttons) != 0 {
		t.Fatalf("перед веткой — итоги без вопроса: %q", a.Answer.Message.Text)
	}
	texts := sentTexts(e.fake)
	if len(texts) != 2 || texts[0] != "Жаль это слышать" || !strings.Contains(texts[1], "Вопрос 2 из 3") {
		t.Fatalf("ветка варианта, затем вопрос новым сообщением: %q", texts)
	}
	q2 := lastSent(t, e.fake)
	if findPayload(q2, btnDone) == "" || findPayload(q2, btnOther) == "" {
		t.Fatalf("multi: «Готово» и «свой вариант»: %+v", q2.Buttons)
	}

	e.press(preAnswer + id + ":1:2")
	if a := lastAnswer(t, e.fake); findPayload(*a.Answer.Message, "✓ Никто!") == "" {
		t.Fatalf("переключатель «✓»: %+v", a.Answer.Message.Buttons)
	}
	e.press(preDone + id + ":1")
	a = lastAnswer(t, e.fake)
	if !strings.Contains(a.Answer.Message.Text, "Вопрос 3 из 3") || findPayload(*a.Answer.Message, btnSkip) != "" {
		t.Fatalf("обязательный вопрос без «Пропустить»: %q", a.Answer.Message.Text)
	}
	e.press(preSkip + id + ":2")
	if a := lastAnswer(t, e.fake); a.Answer.Notification != "Этот вопрос обязательный" {
		t.Fatalf("пропуск обязательного: %+v", a.Answer)
	}
	e.press(preAnswer + id + ":2:0") // «Нет» → причина обязательна
	if a := lastAnswer(t, e.fake); !strings.Contains(a.Answer.Message.Text, textComment) || findPayload(*a.Answer.Message, btnSkip) != "" {
		t.Fatalf("причина при «нет»: %+v", a.Answer.Message)
	}
	e.say("не объяснили, что делать")
	if !hasText(e.fake, "Итог: безразлично") || !hasText(e.fake, "Итог: никто не помогает") || hasText(e.fake, "ЛИБО") {
		t.Fatalf("правила после опроса: %q", sentTexts(e.fake))
	}
	if !hasText(e.fake, "Спасибо, дальше по плану") {
		t.Fatalf("после опроса — следующая реплика: %q", sentTexts(e.fake))
	}
	answers, _ := e.repos.Events.Answers(context.Background(), ev.ID)
	if len(answers) != 3 || answers[1].Reason != "Никто!" || answers[2].Text != "не объяснили, что делать" {
		t.Fatalf("ответы: %+v", answers)
	}
}

// Сигнал варианта (option_flags) — риск без LLM.
func TestSurveyOptionFlag(t *testing.T) {
	e := newEnv(t, flowNow)
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{{Text: "Как ты?", Type: "choice", Options: []string{"Хорошо", "Устал"},
		OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F6", Severity: "high"}}}}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Пульс", TextVy: "Вопрос", Survey: sv}})
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Пульс")
	e.press(preAnswer + ev.ID.String() + ":0:2")
	emp := e.employee(t, c.Employee.ID)
	if emp.Risk == domain.RiskGreen || emp.RiskReason == nil || !strings.Contains(*emp.RiskReason, "F6") || !strings.Contains(*emp.RiskReason, "по варианту ответа") {
		t.Fatalf("риск по варианту: %v %v", emp.Risk, deref(emp.RiskReason))
	}
}

// «Отправить себе» + «Пройти этот путь»: шаг черновика, нажатие и ветка по снимку.
func TestSendStepTestPress(t *testing.T) {
	e := newEnv(t, flowNow)
	br := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Ответ ветки", TextTy: "Ответ ветки"}}}
	c, scID := e.launchSteps(t, flowStart, []domain.ScenarioStep{txt(5, "10:00", "Тестовый", "Проверка?", domain.StepButton{Label: "Да", Branch: br})})
	e.connect(t, c, domain.AddressVy, true)
	d, _ := e.svc.Scenarios.Get(context.Background(), scID)
	e.fake.Reset()
	press := 0
	res, err := e.svc.SendStepTest(context.Background(), scID, d.Steps[0].ID, c.Employee.ID, &press)
	if err != nil {
		t.Fatal(err)
	}
	texts := sentTexts(e.fake)
	if res.Event.Status != domain.EventAnswered || res.Event.ScenarioVersion >= 0 || len(texts) != 2 ||
		texts[0] != testPrefix+"Проверка?" || texts[1] != testPrefix+"Ответ ветки" || res.Messages != 2 {
		t.Fatalf("send-test: %+v %q", res.Event, texts)
	}
	if edits := e.fake.Edits(); len(edits) != 1 || !strings.HasSuffix(edits[0].Message.Text, "✅ Да") {
		t.Fatalf("правка без callback: %+v", edits)
	}
}

// Шаг-ввод (текст и фото → карточка и HR), команда сценария /food, личный файл плана.
func TestInputCommandPersonalFile(t *testing.T) {
	e := newEnv(t, flowNow)
	ctx := context.Background()
	fm := &fakeMedia{}
	e.svc.Bot.media = fm
	plan := txt(0, "10:00", "План", "Твой адаптационный план")
	plan.Media = domain.MediaRefList{{Personal: domain.PersonalAdaptationPlan}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		{DayOffset: 0, Time: "10:00", Kind: domain.KindInput, Title: "Резюме", TextVy: "Расскажите о себе и пришлите фото"},
		plan,
	}, domain.Command{Name: "/Food", Actions: []domain.Action{{Type: domain.ActionSendFile, Media: "Где пообедать", PerTag: true}}})
	cmds := e.fake.Commands()
	if len(cmds) != 5 || cmds[4].Name != "food" || cmds[0].Name != "start" {
		t.Fatalf("меню бота PATCH /me/commands: %+v", cmds)
	}
	e.connect(t, c, domain.AddressVy, true)
	if hasText(e.fake, "адаптационный план") {
		t.Fatal("ввод держит поток")
	}
	e.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateMessageCreated, UserID: userID, ChatID: chatID, Text: "Работал инженером",
		Images: []max.Image{{URL: "https://example.invalid/photo.jpg", Token: "img"}}})
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Резюме")
	if ev.Status != domain.EventAnswered || !strings.Contains(*ev.Answer, "Работал инженером") || !strings.Contains(*ev.Answer, "фото: 1") {
		t.Fatalf("ответ в карточке: %s %v", ev.Status, deref(ev.Answer))
	}
	if tks := e.tickets(t, c.Employee.ID); len(tks) < 1 {
		t.Fatal("HR не получил ответ")
	}
	// Плана нет → «Плана ещё нет» и задача HR.
	if m := lastSent(t, e.fake); !strings.Contains(m.Text, textPlanMissing) {
		t.Fatalf("личного файла нет: %q", m.Text)
	}
	planTask := false
	for _, tk := range e.tickets(t, c.Employee.ID) {
		msgs, _ := e.repos.Tickets.Messages(ctx, tk.ID)
		for _, m := range msgs {
			planTask = planTask || strings.Contains(m.Text, domain.PersonalAdaptationPlan)
		}
	}
	if !planTask {
		t.Fatal("задача HR о плане не создана")
	}
	if _, err := e.svc.UploadNewcomerFile(ctx, c.Employee.ID, domain.PersonalAdaptationPlan, "plan.pdf", "application/pdf", []byte("%PDF-1.4 синтетика")); err != nil {
		t.Fatal(err)
	}
	emp := e.employee(t, c.Employee.ID)
	if atts, missing := attachRefs(ctx, attachDeps{repos: e.repos, max: e.fake}, emp, plan.Media, ""); len(atts) != 1 || len(missing) != 0 {
		t.Fatalf("личный файл после загрузки: %v %v", atts, missing)
	}

	e.say("/food")
	m := lastSent(t, e.fake)
	if m.Text != "📎 Где пообедать" || len(m.Attachments) != 1 || m.Attachments[0].Token != "tok-Где пообедать-GTP" {
		t.Fatalf("/food: %+v", m)
	}
}

// goto_block: остаток блока — skipped, блок с ключом уходит сейчас.
func TestBranchGotoBlock(t *testing.T) {
	e := newEnv(t, flowNow)
	jump := &domain.Branch{Then: domain.ThenGoto, Goto: "lunch"}
	lunch := txt(0, "13:00", "Обед", "Где пообедать")
	lunch.Key = "lunch"
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Сразу к обеду?", domain.StepButton{Label: "Да", Branch: jump}),
		txt(0, "10:00", "Остаток", "Пропустим"),
		lunch,
	})
	e.connect(t, c, domain.AddressVy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
	e.press(preStepButton + q.ID.String() + ":0")
	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Остаток"); ev.Status != domain.EventSkipped {
		t.Fatalf("остаток блока: %s", ev.Status)
	}
	if ev := eventByTitle(t, evs, "Обед"); ev.Status != domain.EventSent || lastSent(t, e.fake).Text != "Где пообедать" {
		t.Fatalf("goto: %s", ev.Status)
	}
}

// no_answer: напоминание через remind_after_h в рабочие часы, после max — «не отвечен» и
// поток дальше.
func TestNoAnswerReminders(t *testing.T) {
	e := newEnv(t, flowNow)
	ctx := context.Background()
	q := txt(0, "10:00", "Вопрос", "Всё получилось?", domain.StepButton{Label: "Да"})
	q.NoAnswer = &domain.NoAnswerRule{RemindAfterH: []int{24}, Max: 1}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{q, txt(0, "10:00", "Реплика", "Идём дальше")})
	e.connect(t, c, domain.AddressVy, true)
	*e.clock = msk(2026, 11, 4, 10, 30) // +24 ч, рабочее время
	if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if m := lastSent(t, e.fake); !strings.HasPrefix(m.Text, textRemindHead) || findPayload(m, "Да") == "" {
		t.Fatalf("напоминание: %q", m.Text)
	}
	*e.clock = msk(2026, 11, 5, 11, 0) // ещё сутки
	if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Вопрос"); ev.Status != domain.EventSkipped || *ev.Answer != answerUnanswer {
		t.Fatalf("не отвечен: %s %v", ev.Status, deref(ev.Answer))
	}
	if lastSent(t, e.fake).Text != "Идём дальше" {
		t.Fatalf("поток после «не отвечен»: %q", sentTexts(e.fake))
	}
}

// Кодировка импорта ТЗ: терминальные кнопки «escalate:<route>» + opens_free_text и «handoff:<to>».
func TestImportedTerminalActions(t *testing.T) {
	e := newEnv(t, flowNow)
	br := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Понимаю", TextTy: "Понимаю", Buttons: []domain.StepButton{
		{Label: "Описать проблему", Action: "escalate:recruiter", OpensFreeText: true},
		{Label: "Чат с рекрутером", Action: "handoff:recruiter"},
	}}}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Документы в порядке?", domain.StepButton{Label: "Нет", Branch: br}),
		txt(0, "10:00", "Дальше", "Идём дальше"),
	})
	e.connect(t, c, domain.AddressVy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
	e.press(preStepButton + q.ID.String() + ":0")
	m := lastSent(t, e.fake)
	e.press(findPayload(m, "Описать проблему"))
	e.say("не прислали договор на подпись")
	tks := e.tickets(t, c.Employee.ID)
	if len(tks) != 1 || tks[0].Route != "recruiter" || tks[0].Quote == nil {
		t.Fatalf("escalate:recruiter после текста: %+v", tks)
	}
	if !hasText(e.fake, "Идём дальше") {
		t.Fatalf("поток после текста: %q", sentTexts(e.fake))
	}
	e.press(findPayload(m, "Чат с рекрутером"))
	if tks := e.tickets(t, c.Employee.ID); len(tks) < 1 {
		t.Fatal("handoff")
	}
}
