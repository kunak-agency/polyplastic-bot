package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
)

// Сверка с Excel-ТЗ (steps/07-tz-conformance.md): правила сценария r69/r70, каноническая форма
// терминальных кнопок, шаги, покрытые знакомством, запасной текст пустой реплики, блоки одного
// момента. Синтетика, подделка MAX, TEST_DATABASE_URL.

// launchRules — сценарий с правилами (опубликован) и новичок GTP.
func (e *env) launchRules(t *testing.T, start time.Time, steps []domain.ScenarioStep, rules ...domain.ScenarioRule) *NewcomerCard {
	t.Helper()
	ctx := context.Background()
	d, err := e.svc.Scenarios.Create(ctx, "Правила", []string{domain.SiteGTP}, steps)
	if err != nil {
		t.Fatal(err)
	}
	list := domain.RuleList(rules)
	if _, err := e.svc.Scenarios.Patch(ctx, d.Scenario.ID, nil, &list); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Newcomers.Create(ctx, NewcomerInput{
		FullName: "Правилов Иван (синтетика)", Site: domain.SiteGTP, StartDate: start, ScenarioID: d.Scenario.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func canonicalPair() []domain.StepButton {
	return []domain.StepButton{escalateButton("Описать проблему", "recruiter"), handoffButton("Чат с рекрутером", "recruiter")}
}

// Импорт пишет действие кнопки в branch.actions (как редактор): escalate + opens_free_text — бот
// ждёт текст и заводит обращение по route; handoff — переписка.
func TestCanonicalTerminalActions(t *testing.T) {
	e := newEnv(t, flowNow)
	br := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Понимаю", TextTy: "Понимаю", Buttons: canonicalPair()}}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Вопрос", "Документы в порядке?", domain.StepButton{Label: "Нет", Branch: br}),
		txt(0, "10:00", "Дальше", "Идём дальше"),
	})
	e.connect(t, c, domain.AddressVy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
	e.press(preStepButton + q.ID.String() + ":0")
	m := lastSent(t, e.fake)
	e.press(findPayload(m, "Описать проблему"))
	if !hasText(e.fake, textFreeTextAsk) {
		t.Fatalf("бот не попросил текст: %q", sentTexts(e.fake))
	}
	e.say("не прислали договор на подпись")
	tks := e.tickets(t, c.Employee.ID)
	if len(tks) != 1 || tks[0].Route != "recruiter" || tks[0].Quote == nil {
		t.Fatalf("escalate recruiter после текста: %+v", tks)
	}
	if !hasText(e.fake, "Идём дальше") {
		t.Fatalf("поток после текста: %q", sentTexts(e.fake))
	}
	// handoff при открытом обращении того же новичка дописывается в него (tickets.open) —
	// здесь важно, что кнопка исполнила действие, а не «Эта кнопка больше не работает».
	e.press(findPayload(m, "Чат с рекрутером"))
	if a := lastAnswer(t, e.fake); a.Answer.Notification == "Эта кнопка больше не работает" {
		t.Fatalf("handoff: %+v", a)
	}
}

// r5–r7: форму обращения спросило знакомство — вопрос ТЗ и «очень приятно» не уходят.
func TestOnboardingCoveredSteps(t *testing.T) {
	e := newEnv(t, flowNow)
	cond := func() *string { c := domain.ConditionAddressUnset; return &c }
	ask := txt(0, "10:00", "Форма", "Как мы будем общаться?",
		domain.StepButton{Label: "На ТЫ", Branch: addressFormBranch("На ТЫ", nil)},
		domain.StepButton{Label: "На ВЫ", Branch: addressFormBranch("На ВЫ", nil)})
	ask.Condition = cond()
	nice := txt(0, "10:00", "Приятно", "Очень приятно с тобой познакомиться!")
	nice.Condition = cond()
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{txt(0, "10:00", "Привет", "Добро пожаловать!"), ask, nice, txt(0, "10:00", "Факт", "Три факта")})
	e.connect(t, c, domain.AddressTy, true)
	if hasText(e.fake, "Как мы будем общаться?") || hasText(e.fake, "Очень приятно с тобой") {
		t.Fatalf("покрытые знакомством шаги ушли: %q", sentTexts(e.fake))
	}
	// «Добро пожаловать!» не последний — «Дальше →» отпускает сразу «Три факта».
	w := eventByTitle(t, e.events(t, c.Employee.ID), "Привет")
	e.press(preContinue + w.ID.String())
	if m := lastSent(t, e.fake); m.Text != "Три факта" {
		t.Fatalf("после «Дальше»: %q", sentTexts(e.fake))
	}
	for _, title := range []string{"Форма", "Приятно"} {
		if ev := eventByTitle(t, e.events(t, c.Employee.ID), title); ev.Status != domain.EventSkipped || deref(ev.Answer) != skipOnboarding {
			t.Errorf("%s: %s %q", title, ev.Status, deref(ev.Answer))
		}
	}
}

// Ветка-заготовка «да» без текста, а файла в медиатеке нет: пустое сообщение не уходит —
// запасной текст на форме новичка.
func TestEmptyBranchMessageFallback(t *testing.T) {
	e := newEnv(t, flowNow)
	yes := &domain.Branch{Messages: []domain.BranchMessage{{Media: domain.MediaRefList{{Name: "Материал: транспорт (GTP)"}}}}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Транспорт", "Рассказать о маршрутах?", domain.StepButton{Label: "Да, расскажи", Branch: yes}, domain.StepButton{Label: "Нет, спасибо"}),
	})
	e.connect(t, c, domain.AddressTy, true)
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Транспорт")
	e.press(preStepButton + q.ID.String() + ":0")
	for _, m := range e.fake.Sent() {
		if emptyMessage(m) {
			t.Fatalf("ушло пустое сообщение: %+v", m)
		}
	}
	if m := lastSent(t, e.fake); m.Text != textMaterialSoonTy {
		t.Fatalf("запасной текст: %q", m.Text)
	}
}

// r69: бот не нашёл ответа — текст правила, его картинка, кнопки правила; «Чат с рекрутером» —
// обращение рекрутеру сразу в переписке, «Описать проблему» — вопрос уходит рекрутеру.
func TestRuleFreeQuestion(t *testing.T) {
	e := newEnv(t, flowNow)
	f := newFakeRAG(t)
	f.set(ragAbstain)
	e.svc.Bot.rag = rag.New(f.srv.URL, 5*time.Second)
	media := &fakeMedia{}
	e.svc.Bot.media = media
	c := e.launchRules(t, flowStart, []domain.ScenarioStep{txt(0, "10:00", "Привет", "Привет!")}, domain.ScenarioRule{
		Key: "rule-free_question", Kind: domain.RuleFreeQuestion, TextTy: "Сложный вопрос — посоветуюсь с подбором и вернусь!",
		TextVy: "Сложный вопрос — посоветуюсь с подбором и вернусь!", Media: domain.MediaRefList{{Name: "Изображение 13"}}, Buttons: canonicalPair(),
	})
	e.connect(t, c, domain.AddressVy, true)
	e.ask("Сколько стоит парковка у офиса?")
	m := lastSent(t, e.fake)
	if !strings.HasPrefix(m.Text, "Сложный вопрос") || findPayload(m, "Описать проблему") == "" || findPayload(m, "Чат с рекрутером") == "" ||
		findPayload(m, btnRephrase) == "" {
		t.Fatalf("отказ по правилу: %q %v", m.Text, labels(m))
	}
	if len(media.refs) == 0 || media.refs[len(media.refs)-1].Name != "Изображение 13" {
		t.Errorf("картинка правила: %+v", media.refs)
	}
	e.press(findPayload(m, "Чат с рекрутером"))
	e.svc.Bot.Wait()
	tks := e.tickets(t, c.Employee.ID)
	if len(tks) != 1 || tks[0].Route != "recruiter" || tks[0].Status != domain.TicketHumanActive {
		t.Fatalf("handoff по правилу: %+v", tks)
	}
	if a := lastAnswer(t, e.fake); a.Answer.Message == nil || !strings.HasPrefix(a.Answer.Message.Text, "Сложный вопрос") {
		t.Errorf("правка после нажатия — с текстом правила: %+v", a.Answer.Message)
	}
}

// r70: опрос не пройден — напоминание текстом правила через 24 и 48 ч, в рабочие часы, не больше
// двух; законченный опрос не напоминается.
func TestRuleSurveyReminder(t *testing.T) {
	e := newEnv(t, flowNow)
	ctx := context.Background()
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{{Text: "Как дела?", Type: "yesno"}}}
	c := e.launchRules(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос дня", TextVy: "Пара вопросов", Survey: sv}},
		domain.ScenarioRule{Key: "rule-survey_reminder", Kind: domain.RuleSurveyReminder, TextTy: "Привет, {Имя}! Ты не прошёл опрос.", TextVy: "Привет, {Имя}! Вы не прошли опрос.", RemindAfterH: []int{24, 48}})
	e.connect(t, c, domain.AddressVy, true)
	reminders := func() int {
		n := 0
		for _, m := range e.fake.Sent() {
			if strings.Contains(m.Text, "Вы не прошли опрос") {
				n++
			}
		}
		return n
	}
	tick := func(at time.Time) {
		*e.clock = at
		if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	tick(msk(2026, 11, 3, 20, 0)) // в тот же день — рано
	tick(msk(2026, 11, 4, 9, 0))  // 23 ч — рано
	if n := reminders(); n != 0 {
		t.Fatalf("напоминаний до 24 ч: %d", n)
	}
	tick(msk(2026, 11, 4, 10, 30))
	m := lastSent(t, e.fake)
	if reminders() != 1 || !strings.Contains(m.Text, "Привет, Иван!") || !strings.Contains(m.Text, "«Опрос дня»") {
		t.Fatalf("первое напоминание: %q", sentTexts(e.fake))
	}
	tick(msk(2026, 11, 4, 12, 0))
	tick(msk(2026, 11, 5, 3, 0)) // 48 ч ночью — не в рабочие часы
	if reminders() != 1 {
		t.Fatalf("ночью или повторно: %d", reminders())
	}
	tick(msk(2026, 11, 5, 10, 30))
	tick(msk(2026, 11, 6, 10, 30))
	if reminders() != 2 {
		t.Fatalf("всего напоминаний %d, ждём 2", reminders())
	}
}

// Выход в понедельник: D−3 и D−1 — оба в пятницу. Блоки одного момента идут по очереди, а молчание
// на вопросе D−3 не держит поток до конца: пришёл D0 — D−3 закрыт, D−1 и D0 уходят.
func TestSameMomentBlocksAndLaterBlock(t *testing.T) {
	start := date(2026, 11, 9) // понедельник
	e := newEnv(t, msk(2026, 11, 6, 10, 5))
	c, _ := e.launchSteps(t, start, []domain.ScenarioStep{
		txt(-3, "10:00", "Вопрос D-3", "Есть вопросы?", domain.StepButton{Label: "Нет"}),
		txt(-3, "10:00", "Хвост D-3", "Отдыхай"),
		txt(-1, "10:00", "D-1", "{Start_day} выход-D1"),
		txt(0, "10:00", "D0", "С первым днём!"),
	})
	e.connect(t, c, domain.AddressTy, true)
	if hasText(e.fake, "выход-D1") {
		t.Fatalf("D−1 вперемешку с D−3: %q", sentTexts(e.fake))
	}
	q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос D-3")
	e.press(preStepButton + q.ID.String() + ":0")
	texts := sentTexts(e.fake)
	if len(texts) < 2 || texts[len(texts)-2] != "Отдыхай" || !strings.HasPrefix(texts[len(texts)-1], "В понедельник, 9 ноября выход-D1") {
		t.Fatalf("D−3 целиком, потом D−1: %q", texts)
	}

	// Второй новичок молчит: в понедельник D−3 закрывается, D−1 и D0 уходят.
	e2 := newEnv(t, msk(2026, 11, 6, 10, 5))
	c2, _ := e2.launchSteps(t, start, []domain.ScenarioStep{
		txt(-3, "10:00", "Вопрос D-3", "Есть вопросы?", domain.StepButton{Label: "Нет"}),
		txt(-3, "10:00", "Хвост D-3", "Отдыхай"),
		txt(-1, "10:00", "D-1", "{Start_day} выход-D1"),
		txt(0, "10:00", "D0", "С первым днём!"),
	})
	e2.connect(t, c2, domain.AddressTy, true)
	*e2.clock = msk(2026, 11, 9, 10, 5)
	if _, err := e2.svc.Scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasText(e2.fake, "выход-D1") || !hasText(e2.fake, "С первым днём!") {
		t.Fatalf("молчание на D−3 держит поток: %q", sentTexts(e2.fake))
	}
	if ev := eventByTitle(t, e2.events(t, c2.Employee.ID), "Хвост D-3"); ev.Status != domain.EventSkipped {
		t.Errorf("хвост D−3: %s", ev.Status)
	}
}

// Вступление к опросу (ТЗ r38/r54/r61) — опрос сразу следом, без «Дальше →»; молчание не кладёт
// опрос в «Что вы пропустили» заголовком.
func TestSurveyIntroWithoutContinue(t *testing.T) {
	e := newEnv(t, flowNow)
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{{Text: "Как дела?", Type: "yesno"}}}
	intro := txt(0, "10:00", "Вступление", "Предлагаем пройти опрос!")
	intro.Required = true
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		intro,
		{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос дня", TextVy: "", Required: true, Survey: sv},
	})
	e.connect(t, c, domain.AddressVy, true)
	sent := e.fake.Sent()
	if len(sent) < 2 || sent[len(sent)-2].Text != "Предлагаем пройти опрос!" || findPayload(sent[len(sent)-2], continueLabel) != "" ||
		!strings.Contains(sent[len(sent)-1].Text, "Как дела?") {
		t.Fatalf("вступление и опрос: %q", sentTexts(e.fake))
	}
}

func TestMissedPartsSplit(t *testing.T) {
	long := strings.Repeat("я", 3000)
	parts := missedParts([]string{"коротко", long, long})
	if len(parts) != 2 || !strings.HasPrefix(parts[0], textMissedHead) {
		t.Fatalf("частей %d", len(parts))
	}
	for _, p := range parts {
		if n := len([]rune(p)); n > maxTextLen {
			t.Fatalf("часть %d символов", n)
		}
	}
	if huge := missedParts([]string{strings.Repeat("ы", 9000)}); len(huge) != 4 {
		t.Fatalf("реплика длиннее предела: %d частей", len(huge))
	}
}

// r70: сроки 24 и 48 ч пропущены за выходные — одно напоминание, не два подряд.
func TestRuleSurveyReminderCollapses(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 6, 10, 5)) // пятница
	ctx := context.Background()
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{{Text: "Как дела?", Type: "yesno"}}}
	c := e.launchRules(t, date(2026, 11, 6), []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос", TextVy: "Пара вопросов", Survey: sv}},
		domain.ScenarioRule{Key: "rule-survey_reminder", Kind: domain.RuleSurveyReminder, TextTy: "Напоминаю про опрос", TextVy: "Напоминаю про опрос", RemindAfterH: []int{24, 48}})
	e.connect(t, c, domain.AddressVy, true)
	for _, at := range []time.Time{msk(2026, 11, 9, 9, 0), msk(2026, 11, 9, 9, 15), msk(2026, 11, 10, 10, 0)} {
		*e.clock = at
		if _, err := e.svc.Scheduler.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "Напоминаю про опрос") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("напоминаний %d, ждём одно", n)
	}
}

// r63 Б/В: «ответь в чате с ботом либо нажми кнопку … рекрутеру. Далее — вопрос 3». Следующий
// вопрос ждёт текст или кнопку: иначе причина ушла бы ответом на него.
func TestSurveyBranchWaitsForTextOrButton(t *testing.T) {
	for _, viaButton := range []bool{false, true} {
		e := newEnv(t, flowNow)
		why := &domain.Branch{Actions: []domain.Action{{Type: domain.ActionEscalate, Route: "recruiter"}, {Type: domain.ActionOpenFreeText}}, Messages: []domain.BranchMessage{{
			TextVy: "Что повлияло?", TextTy: "Что повлияло?", Buttons: []domain.StepButton{handoffButton("Готов озвучить причину рекрутеру лично", "recruiter")}}}}
		sv := &domain.Survey{Questions: []domain.SurveyQuestion{
			{Text: "Останетесь?", Type: "choice", Options: []string{"Да", "Не уверен"}, Branches: map[string]*domain.Branch{"2": why}},
			{Text: "Что улучшить?", Type: "yesno"},
		}}
		c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Итоги", TextVy: "Опрос", Survey: sv}})
		e.connect(t, c, domain.AddressVy, true)
		ev := eventByTitle(t, e.events(t, c.Employee.ID), "Итоги")
		e.press(preAnswer + ev.ID.String() + ":0:2")
		e.svc.Bot.Wait()
		if hasText(e.fake, "Что улучшить?") {
			t.Fatalf("следующий вопрос до текста/кнопки: %q", sentTexts(e.fake))
		}
		if viaButton {
			var branchMsg max.OutMessage
			for _, m := range e.fake.Sent() {
				if strings.HasPrefix(m.Text, "Что повлияло?") {
					branchMsg = m
				}
			}
			e.press(findPayload(branchMsg, "Готов озвучить причину рекрутеру лично"))
		} else {
			e.say("сомневаюсь из-за графика")
		}
		e.svc.Bot.Wait()
		if !strings.Contains(lastSent(t, e.fake).Text, "Что улучшить?") {
			t.Fatalf("вопрос 2 после %v: %q", viaButton, sentTexts(e.fake))
		}
		if emp := e.employee(t, c.Employee.ID); emp.AwaitFreeText != nil {
			t.Fatalf("текст всё ещё ждём: %+v", emp.AwaitFreeText)
		}
		if tks := e.tickets(t, c.Employee.ID); len(tks) == 0 || tks[0].Route != "recruiter" {
			t.Fatalf("обращение рекрутеру: %+v", tks)
		}
	}
}
