package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Обращения (итерация 3): создание из бота, маршрутизация F-кодов, статусы и переходы, прокси
// ответа HR, пауза RAG в human_active. Интеграционные: Postgres + подделка MAX.

// chatClient — та же подделка, но не *max.Fake: TicketService шлёт карточки в чаты, как в
// настоящем MAX (на голой подделке — только лог).
type chatClient struct{ *max.Fake }

const hrChatID, sbChatID = 555001, 555002

// withChats — карточки обращений уходят в HR-чат и чат СБ через подделку.
func (e *env) withChats() {
	e.svc.Tickets.max = chatClient{e.fake}
	e.svc.Tickets.hrChat, e.svc.Tickets.sbChat = hrChatID, sbChatID
}

// sentTo — сообщения в чат.
func sentTo(f *max.Fake, chat int64) []max.OutMessage {
	var out []max.OutMessage
	for _, m := range f.Sent() {
		if m.ChatID == chat {
			out = append(out, m)
		}
	}
	return out
}

func (e *env) tickets(t *testing.T, emp uuid.UUID) []domain.Ticket {
	t.Helper()
	list, err := e.repos.Tickets.List(context.Background(), repositories.TicketFilter{Newcomer: &emp})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (e *env) onlyTicket(t *testing.T, emp uuid.UUID) domain.Ticket {
	t.Helper()
	list := e.tickets(t, emp)
	if len(list) != 1 {
		t.Fatalf("обращений %d: %+v", len(list), list)
	}
	return list[0]
}

func appCode(err error) (errs.Type, string) {
	var e *errs.Error
	if !errors.As(err, &e) {
		return "", ""
	}
	return e.Type, e.Code
}

// «Спросить человека» → обращение «вопрос» к HR, OPEN с номером; повтор — «уже передано».
// Затем весь путь статусов: assign → reply (human_active, SLA остановлен) → текст новичка в
// обращение, не в RAG → «Вопрос закрыт» → 👍.
func TestTicketLifecycle(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	f.set(ragAnswer)
	e.withChats()
	ctx := context.Background()

	e.press(payHuman)
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Number != domain.TicketNumberStart || tk.Category != taxonomy.CategoryQuestion || tk.Route != taxonomy.RouteHR ||
		tk.Priority != taxonomy.P1 || tk.Status != domain.TicketOpen || tk.SLADueAt == nil || tk.Day != -1 || tk.Restricted {
		t.Fatalf("обращение %+v", tk)
	}
	// 2 ноября 2026 12:00 (пн) + 1 рабочий день → вт 12:00.
	if !tk.SLADueAt.Equal(msk(2026, 11, 3, 12, 0)) {
		t.Fatalf("SLA %v", tk.SLADueAt)
	}
	open := lastSent(t, e.fake)
	if !strings.HasPrefix(open.Text, "🕓 **Передал ваш вопрос HR.**") || !strings.Contains(open.Text, "Обращение #1001.") ||
		!strings.Contains(open.Text, "в течение рабочего дня") ||
		!sameRows(labels(open), []string{btnTicketDetails, btnTicketCancel}) {
		t.Fatalf("OPEN %q %v", open.Text, labels(open))
	}
	if strings.Contains(open.Text, "без моего имени") {
		t.Fatal("кнопка анонимности")
	}
	if cards := sentTo(e.fake, hrChatID); len(cards) != 1 || !strings.Contains(cards[0].Text, "#1001") {
		t.Fatalf("карточка в HR-чат: %+v", cards)
	}
	e.press(payHuman)
	if len(e.tickets(t, c.Employee.ID)) != 1 || !strings.Contains(lastSent(t, e.fake).Text, "уже передано") {
		t.Fatalf("повтор: %q", lastSent(t, e.fake).Text)
	}

	// reply без исполнителя — 409 ticket_not_assigned.
	if _, err := e.svc.Tickets.Reply(ctx, tk.ID, "Здравствуйте", false); err == nil {
		t.Fatal("reply без assign")
	} else if typ, code := appCode(err); typ != errs.ErrTypeConflict || code != CodeTicketNotAssigned {
		t.Fatalf("%v %v", typ, code)
	}

	// assign → assigned, статус правкой OPEN.
	v, err := e.svc.Tickets.Assign(ctx, tk.ID, "Анна Смирнова", "")
	if err != nil || v.Ticket.Status != domain.TicketAssigned {
		t.Fatalf("assign %+v %v", v, err)
	}
	edits := e.fake.Edits()
	if len(edits) == 0 || !strings.Contains(edits[len(edits)-1].Message.Text, "Вашим вопросом занимается Анна Смирнова, HR") {
		t.Fatalf("ASSIGNED %+v", edits)
	}
	// Переназначение без комментария — 422, с комментарием — ок.
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, "Олег Петров", ""); err == nil {
		t.Fatal("переназначение без комментария")
	} else if typ, _ := appCode(err); typ != errs.ErrTypeValidation {
		t.Fatalf("%v", err)
	}

	// reply → human_active, подпись, кнопки, SLA остановлен.
	e.fake.Reset()
	v, err = e.svc.Tickets.Reply(ctx, tk.ID, "Здравствуйте, Пётр! Это Анна из HR. Пропуск будет завтра.", false)
	if err != nil || v.Ticket.Status != domain.TicketHumanActive || v.Ticket.SLADueAt != nil {
		t.Fatalf("reply %+v %v", v, err)
	}
	r := lastSent(t, e.fake)
	if !strings.HasPrefix(r.Text, "**Анна Смирнова, HR** (по обращению #1001):\n") || !sameRows(labels(r), []string{btnTicketReply, btnTicketClose}) {
		t.Fatalf("ответ HR %q %v", r.Text, labels(r))
	}
	// Переназначение в human_active статус не понижает.
	if v, err := e.svc.Tickets.Assign(ctx, tk.ID, "Олег Петров", "Анна в отпуске"); err != nil || v.Ticket.Status != domain.TicketHumanActive {
		t.Fatalf("assign в human_active: %+v %v", v, err)
	}

	// Текст новичка — в обращение, не в RAG; подтверждение только на первое сообщение.
	e.fake.Reset()
	e.ask("А во сколько подойти?")
	e.ask("И куда именно?")
	if n := len(f.requests()); n != 0 {
		t.Fatalf("в rag ушло %d", n)
	}
	if s := e.fake.Sent(); len(s) != 1 || !strings.Contains(s[0].Text, "обращение #1001") {
		t.Fatalf("подтверждения: %+v", s)
	}
	msgs, _ := e.repos.Tickets.Messages(ctx, tk.ID)
	var fromNewcomer int
	for _, m := range msgs {
		if m.From == domain.TicketMsgNewcomer {
			fromNewcomer++
		}
	}
	if fromNewcomer != 2 {
		t.Fatalf("сообщений новичка %d", fromNewcomer)
	}
	// /help работает и напоминает про переписку.
	e.ask("/help")
	if h := lastSent(t, e.fake); !strings.Contains(h.Text, "Я Корсик") || !strings.Contains(h.Text, "#1001") {
		t.Fatalf("/help %q", h.Text)
	}

	// Новичок: «Вопрос закрыт» → resolved, RESOLVED с 👍/👎.
	e.fake.Reset()
	e.press(preTicketClose + tk.ID.String())
	cur, _ := e.repos.Tickets.ByID(ctx, tk.ID)
	if cur.Status != domain.TicketResolved || cur.ResolvedAt == nil {
		t.Fatalf("resolved %+v", cur)
	}
	res := lastSent(t, e.fake)
	if !strings.HasPrefix(res.Text, "✅ **Вопрос закрыт.** Вернулся 🐾") || !sameRows(labels(res), []string{btnTicketUp, btnTicketDown}) {
		t.Fatalf("RESOLVED %q %v", res.Text, labels(res))
	}
	e.press(preTicketUp + tk.ID.String())
	if cur, _ := e.repos.Tickets.ByID(ctx, tk.ID); cur.Feedback != "up" {
		t.Fatalf("👍 %+v", cur.Feedback)
	}
	// После закрытия текст снова идёт в RAG; reply/assign/resolve — 409.
	e.ask("когда отпуск")
	if n := len(f.requests()); n != 1 {
		t.Fatalf("rag после закрытия: %d", n)
	}
	for _, call := range []func() error{
		func() error { _, err := e.svc.Tickets.Reply(ctx, tk.ID, "ещё", false); return err },
		func() error { _, err := e.svc.Tickets.Assign(ctx, tk.ID, "Анна", "x"); return err },
		func() error { _, err := e.svc.Tickets.Resolve(ctx, tk.ID, ""); return err },
	} {
		if typ, _ := appCode(call()); typ != errs.ErrTypeConflict {
			t.Fatal("действие над закрытым — не 409")
		}
	}
	// Заметка в закрытое — можно, в MAX не уходит.
	e.fake.Reset()
	if v, err := e.svc.Tickets.Note(ctx, tk.ID, "Проверить через неделю"); err != nil || !v.Messages[len(v.Messages)-1].Internal {
		t.Fatalf("note %+v %v", v, err)
	}
	if len(e.fake.Sent()) != 0 {
		t.Fatal("заметка ушла в MAX")
	}
}

// reply с resolve:true — сразу resolved и «Вопрос закрыт».
func TestTicketReplyAndResolve(t *testing.T) {
	e, _, c := askEnv(t, time.Second)
	ctx := context.Background()
	e.press(payHuman)
	tk := e.onlyTicket(t, c.Employee.ID)
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, "Анна", ""); err != nil {
		t.Fatal(err)
	}
	e.fake.Reset()
	v, err := e.svc.Tickets.Reply(ctx, tk.ID, "Готово", true)
	if err != nil || v.Ticket.Status != domain.TicketResolved {
		t.Fatalf("%+v %v", v, err)
	}
	if s := e.fake.Sent(); len(s) != 2 || !strings.HasPrefix(s[1].Text, "✅") {
		t.Fatalf("%+v", s)
	}
}

// Маршрутизация F-кодов: F9 и F5 — сразу обращение, карточка в чат СБ (не в HR-чат),
// руководителю не видно; F7 — только после «Передать рекрутеру», в HR-чат.
func TestTicketFlagRouting(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	e.withChats()

	e.ask("мастер берёт откаты с поставщиков")
	if n := len(f.requests()); n != 0 {
		t.Fatalf("в rag %d", n)
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F9" || tk.Route != taxonomy.RouteSecurity || !tk.Restricted || tk.NotifyManager || tk.Priority != taxonomy.P0 {
		t.Fatalf("F9 %+v", tk)
	}
	if tk.Quote == nil || !strings.Contains(*tk.Quote, "откаты") {
		t.Fatalf("цитата F9 (согласие есть): %v", tk.Quote)
	}
	if len(sentTo(e.fake, hrChatID)) != 0 || len(sentTo(e.fake, sbChatID)) != 1 {
		t.Fatalf("F9: HR-чат %d, СБ %d", len(sentTo(e.fake, hrChatID)), len(sentTo(e.fake, sbChatID)))
	}
	if card := sentTo(e.fake, sbChatID)[0].Text; strings.Contains(card, "откаты") {
		t.Fatalf("цитата в карточке: %q", card)
	}
	m := lastSent(t, e.fake)
	if !strings.Contains(m.Text, "передаю в службу безопасности") || !strings.Contains(m.Text, "Обращение #1001.") ||
		!sameRows(labels(m), []string{btnFlagMore, btnTicketCancel}) {
		t.Fatalf("сигнал F9 %q %v", m.Text, labels(m))
	}
	// «Напишу подробнее» → детали в то же обращение.
	qid := questionID(t, m)
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, qid).MessageID)
	e.ask("каждую пятницу, наличными")
	if d := lastSent(t, e.fake); !strings.Contains(d.Text, "приложил к обращению #1001") {
		t.Fatalf("детали %q", d.Text)
	}

	e.ask("бригадир ударил новичка")
	list := e.tickets(t, c.Employee.ID)
	if len(list) != 2 || list[0].Category != "F5" || list[0].Route == taxonomy.RouteManager || list[0].NotifyManager || !list[0].Restricted {
		t.Fatalf("F5 %+v", list)
	}
	if len(sentTo(e.fake, hrChatID)) != 0 || len(sentTo(e.fake, sbChatID)) != 2 {
		t.Fatal("F5 ушёл не в чат СБ")
	}

	// F7: сначала выбор, обращения нет; «Передать рекрутеру» → обращение F7 в HR-чат.
	e.ask("думаю уволиться")
	if n := len(e.tickets(t, c.Employee.ID)); n != 2 {
		t.Fatalf("F7 без согласия на передачу: %d", n)
	}
	m = lastSent(t, e.fake)
	qid = questionID(t, m)
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, qid).MessageID)
	list = e.tickets(t, c.Employee.ID)
	if len(list) != 3 || list[0].Category != "F7" || list[0].Route != taxonomy.RouteRecruiter || list[0].Restricted {
		t.Fatalf("F7 %+v", list[0])
	}
	if len(sentTo(e.fake, hrChatID)) != 1 {
		t.Fatal("F7 не в HR-чат")
	}
	if open := lastSent(t, e.fake); !strings.Contains(open.Text, "Передал ваш вопрос рекрутеру") {
		t.Fatalf("OPEN F7 %q", open.Text)
	}
}

// «Чего-то нет» → причины → F2 к HR с уведомлением руководителю, новичку «уточните у руководителя».
func TestTicketAccessMissing(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 12, 5))
	e.withChats()
	ctx := context.Background()
	steps := []domain.ScenarioStep{
		{DayOffset: 0, Time: "12:00", Kind: domain.KindText, Title: "Проверка доступов", TextVy: "Всё выдали?",
			Buttons: domain.ButtonList{{Label: "Да, всё есть"}, {Label: "Чего-то нет"}}, WorkHoursOnly: true},
	}
	d, err := e.svc.Scenarios.Create(ctx, "Доступы", []string{domain.SiteGTP}, steps)
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
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Проверка доступов")
	if ev.Status != domain.EventSent {
		t.Fatalf("шаг не ушёл: %+v", ev)
	}
	e.press(preStepButton + ev.ID.String() + ":1")
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || !strings.Contains(a.Answer.Message.Text, textAccessWhich) ||
		!sameRows(labels(*a.Answer.Message), []string{"Техника", "Учётка"}, []string{"Пропуск", "Другое"}, []string{btnHuman}) {
		t.Fatalf("причины %+v", a.Answer.Message)
	}
	if len(e.tickets(t, c.Employee.ID)) != 0 {
		t.Fatal("обращение до выбора причины")
	}
	e.press(preAccessReason + ev.ID.String() + ":1")
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F2" || tk.Route != taxonomy.RouteHR || !tk.NotifyManager || tk.Priority != taxonomy.P1 ||
		tk.Day != 0 || !strings.Contains(tk.Summary, "«Учётка»") || tk.Quote != nil || tk.EventID == nil {
		t.Fatalf("F2 %+v", tk)
	}
	if open := lastSent(t, e.fake); !strings.Contains(open.Text, "уточните у руководителя") || !strings.Contains(open.Text, "#1001") {
		t.Fatalf("OPEN F2 %q", open.Text)
	}
	if got := eventByTitle(t, e.events(t, c.Employee.ID), "Проверка доступов"); got.Status != domain.EventAnswered || deref(got.Answer) != "Чего-то нет: Учётка" {
		t.Fatalf("событие %+v", got)
	}
	if cards := sentTo(e.fake, hrChatID); len(cards) != 1 || !strings.Contains(cards[0].Text, "Руководителю — уведомление без текста") {
		t.Fatalf("карточка F2 %+v", cards)
	}
	if c, _ := e.svc.Newcomers.Get(ctx, c.Employee.ID); c.OpenTickets != 1 {
		t.Fatalf("open_tickets %d", c.OpenTickets)
	}
}

// «Добавить детали» и «Отменить» под OPEN; без согласия текст не сохраняется.
func TestTicketDetailsAndCancel(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 20, 0)) // вечер понедельника — нерабочее время
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressTy, false)
	e.fake.Reset()
	e.press(payHuman)
	open := lastSent(t, e.fake)
	if !strings.Contains(open.Text, "твой вопрос HR") || !strings.Contains(open.Text, "HR на связи в будни 9:00–18:00") {
		t.Fatalf("OPEN вне рабочего времени %q", open.Text)
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	// Создано в 20:00 пн → отсчёт со вт 9:00 → ср 9:00.
	if !tk.SLADueAt.Equal(msk(2026, 11, 4, 9, 0)) {
		t.Fatalf("SLA %v", tk.SLADueAt)
	}
	e.press(preTicketDetails + tk.ID.String())
	e.say("пропуск не работает на турникете")
	if s := lastSent(t, e.fake); !strings.Contains(s.Text, "Без согласия") {
		t.Fatalf("без согласия %q", s.Text)
	}
	msgs, _ := e.repos.Tickets.Messages(context.Background(), tk.ID)
	for _, m := range msgs {
		if strings.Contains(m.Text, "турникет") {
			t.Fatal("текст без согласия сохранён")
		}
	}
	e.press(preTicketCancel + tk.ID.String())
	if cur, _ := e.repos.Tickets.ByID(context.Background(), tk.ID); cur.Status != domain.TicketResolved {
		t.Fatalf("отмена %+v", cur.Status)
	}
	if s := lastSent(t, e.fake); !strings.Contains(s.Text, "отменил обращение #1001") {
		t.Fatalf("%q", s.Text)
	}
}

// Групповой чат: бот не отвечает «ссылка не сработала», bot_added запоминается.
func TestBotGroupChatSilent(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateBotAdded, ChatID: 777, UserID: 1})
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, ChatID: 777, UserID: 2, Text: "всем привет"})
	if len(e.fake.Sent()) != 0 {
		t.Fatalf("бот ответил в группе: %+v", e.fake.Sent())
	}
}

// Карточка закрытой категории в HR-чате — без имени, категории и сводки.
func TestTicketCardRestricted(t *testing.T) {
	tx, err := taxonomy.LoadDefault("")
	if err != nil {
		t.Fatal(err)
	}
	f10, _ := tx.Get("F10")
	tk := &domain.Ticket{Number: 1050, Category: "F10", Priority: "P2", Summary: "F10 · Личные обстоятельства и здоровье."}
	card := ticketCard(tk, f10, &domain.Employee{FullName: "Тестов Пётр (синтетика)", Site: domain.SiteGTP}, domain.SiteOf(domain.SiteGTP))
	if strings.Contains(card, "Тестов") || strings.Contains(card, "здоровье") || !strings.Contains(card, "конфиденциальное") {
		t.Fatalf("%q", card)
	}
}

func TestSLADue(t *testing.T) {
	site := domain.SiteOf(domain.SiteGTP)
	cases := []struct {
		at   time.Time
		prio string
		want time.Time
	}{
		{msk(2026, 11, 2, 12, 0), taxonomy.P0, msk(2026, 11, 2, 13, 0)},
		{msk(2026, 11, 2, 17, 30), taxonomy.P0, msk(2026, 11, 3, 9, 30)}, // через вечер
		{msk(2026, 11, 6, 16, 30), taxonomy.P0, msk(2026, 11, 9, 9, 30)}, // пятница до 17
		{msk(2026, 11, 7, 11, 0), taxonomy.P1, msk(2026, 11, 10, 9, 0)},  // суббота → пн 9:00 + 1 день
		{msk(2026, 11, 5, 17, 30), taxonomy.P1, msk(2026, 11, 6, 17, 0)}, // в пятницу не позже 17:00
		{msk(2026, 11, 2, 12, 0), taxonomy.P2, msk(2026, 11, 5, 12, 0)},
	}
	for _, c := range cases {
		if got := SLADue(c.at, c.prio, site); !got.Equal(c.want) {
			t.Errorf("%v %s: %v, want %v", c.at, c.prio, got, c.want)
		}
	}
}
