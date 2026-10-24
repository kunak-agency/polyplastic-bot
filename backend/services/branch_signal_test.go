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

// Сигналы вариантов опроса и ветки ТЗ (e2e-прогон 24.10, находки 1, 2, 6, 7): обращений столько,
// сколько нажатий; вопросы на «вы»; свой ответ опроса не уходит в открытое обращение.

func ticketTexts(f *max.Fake) []string {
	var out []string
	for _, t := range sentTexts(f) {
		if strings.Contains(t, "Обращение #") || strings.Contains(t, "Передал") {
			out = append(out, t)
		}
	}
	return out
}

// Вариант с флагом F2 и веткой [Описать проблему][Чат с HR] (r39 В): выбор — только сигнал риска,
// обращения — по нажатиям, с категорией F2.
func TestOptionFlagWithEscalationBranch(t *testing.T) {
	e := newEnv(t, flowNow)
	pair := []domain.StepButton{escalateButton("Описать проблему", "recruiter"), handoffButton("Чат с HR", "hr")}
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{
		{Text: "Были ли вопросы при оформлении?", Type: "choice", Options: []string{"Всё отлично", "Трудности с документами"},
			Branches:    map[string]*domain.Branch{"2": {Messages: []domain.BranchMessage{{TextVy: "Жаль. Как поступим?", TextTy: "Жаль. Как поступим?", Buttons: pair}}}},
			OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F2", Severity: "medium"}}},
		{Text: "Как вас встретили?", Type: "choice", Options: []string{"Отлично", "Нечёткие задачи"},
			OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F1", Severity: "medium"}}},
		{Text: "Задачи понятны?", Type: "choice", Options: []string{"Да", "Ничего не понял"},
			Branches:    map[string]*domain.Branch{"2": {Messages: []domain.BranchMessage{{TextVy: "Обсудим с рекрутером?", TextTy: "Обсудим с рекрутером?", Buttons: pair}}}},
			OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F1", Severity: "medium"}}},
	}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос", TextVy: "Пара вопросов", Survey: sv}})
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Опрос")
	id := ev.ID.String()
	e.fake.Reset()
	e.press(preAnswer + id + ":0:2")
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 || len(ticketTexts(e.fake)) != 0 {
		t.Fatalf("выбор варианта с веткой эскалации создал обращение: %d, %q", n, sentTexts(e.fake))
	}
	emp := e.employee(t, c.Employee.ID)
	if emp.Risk != domain.RiskYellow || !strings.Contains(deref(emp.RiskReason), "F2") || !strings.Contains(deref(emp.RiskReason), "по варианту ответа") {
		t.Fatalf("сигнал риска: %v %q", emp.Risk, deref(emp.RiskReason))
	}
	var branchMsg max.OutMessage
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "Как поступим") {
			branchMsg = m
		}
	}
	e.press(findPayload(branchMsg, "Описать проблему"))
	e.say("не приняли справку")
	tks := e.tickets(t, c.Employee.ID)
	if len(tks) != 1 || tks[0].Category != "F2" || tks[0].Route != "recruiter" || !strings.Contains(tks[0].Summary, "серьёзность 2") {
		t.Fatalf("«Описать проблему» → одно обращение F2 рекрутеру: %+v", tks)
	}
	e.press(findPayload(branchMsg, "Чат с HR"))
	tks = e.tickets(t, c.Employee.ID)
	if len(tks) != 2 {
		t.Fatalf("два нажатия — два обращения: %+v", tks)
	}
	// Вариант без ветки, medium (F1) — только сигнал, обращений не прибавилось.
	e.fake.Reset()
	e.press(preAnswer + id + ":1:2")
	if n := len(e.tickets(t, c.Employee.ID)); n != 2 || len(ticketTexts(e.fake)) != 0 {
		t.Fatalf("F1 medium без ветки: обращений %d, %q", n, sentTexts(e.fake))
	}
	// «Описать проблему» в ветке другого вопроса того же опроса — новое нажатие, не «Уже записал».
	e.press(preAnswer + id + ":2:2")
	var q3 max.OutMessage
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "Обсудим с рекрутером") {
			q3 = m
		}
	}
	e.press(findPayload(q3, "Описать проблему"))
	e.say("задачи не поставили")
	tks = e.tickets(t, c.Employee.ID)
	if len(tks) != 3 {
		t.Fatalf("три нажатия — три обращения: %+v", tks)
	}
}

// F7 high («задумываюсь об уходе») со своей веткой ТЗ: говорит только ветка — без общего диалога
// F7 и без «Напишите одним сообщением»; красная зона; текст → обращение F7 рекрутеру.
func TestOptionF7WithOwnBranch(t *testing.T) {
	e := newEnv(t, flowNow)
	br := &domain.Branch{
		Messages: []domain.BranchMessage{{TextVy: "Очень жаль. Что на это повлияло?", TextTy: "Очень жаль. Что на это повлияло?",
			Buttons: []domain.StepButton{handoffButton("Готов озвучить причину рекрутеру", "recruiter")}}},
		Actions: []domain.Action{{Type: domain.ActionEscalate, Route: "recruiter"}, {Type: domain.ActionOpenFreeText}},
	}
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{
		{Text: "Планируешь остаться?", TextVy: "Планируете остаться?", Type: "choice", Options: []string{"Да", "Нет, задумываюсь об уходе"},
			Branches: map[string]*domain.Branch{"2": br}, OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F7", Severity: "high"}}},
		{Text: "Оцени нагрузку", TextVy: "Оцените нагрузку", Type: "yesno"},
	}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Итог", TextVy: "Итоги", Survey: sv}})
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Итог")
	e.fake.Reset()
	e.press(preAnswer + ev.ID.String() + ":0:2")
	e.svc.Bot.Wait()
	texts := sentTexts(e.fake)
	if len(texts) != 1 || !strings.Contains(texts[0], "Что на это повлияло") {
		t.Fatalf("говорит только ветка ТЗ: %q", texts)
	}
	if len(e.tickets(t, c.Employee.ID)) != 0 {
		t.Fatal("обращение без согласия новичка")
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskRed {
		t.Fatalf("F7 high — красная зона: %v %q", emp.Risk, deref(emp.RiskReason))
	}
	e.say("не устраивает зарплата")
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F7" || tk.Route != "recruiter" || tk.Quote == nil {
		t.Fatalf("текст после ветки → F7 рекрутеру: %+v", tk)
	}
	if !hasText(e.fake, "Вопрос 2 из 2") || !hasText(e.fake, "Оцените нагрузку") {
		t.Fatalf("после текста — следующий вопрос на «вы»: %q", sentTexts(e.fake))
	}
}

// Вопросы и подписи опроса на «вы»: новичку на «вы» — text_vy/options_vy, в итогах тоже; на «ты» — text.
func TestSurveyQuestionAddressForm(t *testing.T) {
	sv := func() *domain.Survey {
		return &domain.Survey{Questions: []domain.SurveyQuestion{
			{Text: "Как тебя встретили коллеги?", TextVy: "Как вас встретили коллеги?", Type: "choice",
				Options: []string{"Отлично", "Тебе виднее"}, OptionsVy: []string{"", "Вам виднее"}},
			{Text: "Понятны ли тебе задачи?", TextVy: "Понятны ли вам задачи?", Type: "yesno"},
		}}
	}
	for form, want := range map[string][2]string{
		domain.AddressVy: {"Как вас встретили коллеги?", "Вам виднее"},
		domain.AddressTy: {"Как тебя встретили коллеги?", "Тебе виднее"},
	} {
		e := newEnv(t, flowNow)
		c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос", TextVy: "Опрос", TextTy: "Опрос", Survey: sv()}})
		e.connect(t, c, form, true)
		m := lastSent(t, e.fake)
		if !strings.Contains(m.Text, want[0]) || findPayload(m, want[1]) == "" || findPayload(m, "Отлично") == "" {
			t.Fatalf("%s: вопрос %q, кнопки %v", form, m.Text, labels(m))
		}
		ev := eventByTitle(t, e.events(t, c.Employee.ID), "Опрос")
		e.press(preAnswer + ev.ID.String() + ":0:2")
		a := lastAnswer(t, e.fake)
		if !strings.Contains(a.Answer.Message.Text, "✅ 1. "+want[0]+": "+want[1]) {
			t.Fatalf("%s: итог %q", form, a.Answer.Message.Text)
		}
		if form == domain.AddressVy && strings.Contains(a.Answer.Message.Text, "тебе") {
			t.Fatalf("«ты» в опросе на «вы»: %q", a.Answer.Message.Text)
		}
	}
}

// Свой ответ опроса при открытом обращении той же категории (находка 6): текст — ответ опроса,
// без «Обращение #N уже передано» и без строки в обращении.
func TestSurveyOwnAnswerWithOpenTicket(t *testing.T) {
	e := newEnv(t, flowNow)
	f := newFakeRAG(t)
	e.svc.Bot.rag = rag.New(f.srv.URL, time.Second)
	f.set(ragAbstain)
	f.mu.Lock()
	f.flagsReply = `{"flags":[{"code":"F2","severity":2,"confidence":0.9}],"sentiment":"neg","quote":""}`
	f.mu.Unlock()
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{
		{Text: "Что помогло бы?", Type: "choice", Options: []string{"Доступы", "Техника"}, Other: true},
	}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Опрос", TextVy: "Опрос", Survey: sv}})
	e.connect(t, c, domain.AddressVy, true)
	e.ask("до сих пор нет ноутбука и доступа к почте")
	tk := e.onlyTicket(t, c.Employee.ID)
	msgs, _ := e.repos.Tickets.Messages(context.Background(), tk.ID)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Опрос")
	e.fake.Reset()
	e.press(preWrite + ev.ID.String() + ":0")
	e.say("быстрее выдавать технику")
	e.svc.Bot.Wait()
	for _, s := range sentTexts(e.fake) {
		if strings.Contains(s, "уже передано") || strings.Contains(s, "Обращение #") {
			t.Fatalf("свой ответ опроса ушёл в обращение: %q", sentTexts(e.fake))
		}
	}
	if done := eventByTitle(t, e.events(t, c.Employee.ID), "Опрос"); done.Status != domain.EventAnswered {
		t.Fatalf("опрос не закончен ответом: %s", done.Status)
	}
	after, _ := e.repos.Tickets.Messages(context.Background(), tk.ID)
	if len(after) != len(msgs) || len(e.tickets(t, c.Employee.ID)) != 1 {
		t.Fatalf("строк в обращении было %d, стало %d", len(msgs), len(after))
	}
}
