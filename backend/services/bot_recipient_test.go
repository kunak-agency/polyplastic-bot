package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Пустой получатель (прод 24.10, «max: у сообщения не указан ни ChatID, ни UserID»): бот не
// должен собирать сообщение без адресата, а если собрал — не звать MAX.

// payloadsOf — payload всех callback-кнопок в отправленных сообщениях и правках.
func payloadsOf(f *max.Fake) []string {
	var out []string
	add := func(m max.OutMessage) {
		for _, r := range m.Buttons {
			for _, b := range r {
				if b.Payload != "" {
					out = append(out, b.Payload)
				}
			}
		}
	}
	for _, m := range f.Sent() {
		add(m)
	}
	for _, a := range f.Answers() {
		if a.Answer.Message != nil {
			add(*a.Answer.Message)
		}
	}
	return out
}

// rawDB — прямой доступ к тестовой базе: удалить карточку целиком или снять привязку, как
// руками в проде.
func rawDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(os.Getenv("TEST_DATABASE_URL")))
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return db
}

// deleteEmployee — новичок удалён из базы вместе с вопросами, обращениями, событиями и
// приглашением.
func deleteEmployee(t *testing.T, db *gorm.DB, id uuid.UUID) {
	t.Helper()
	for _, q := range []string{
		"DELETE FROM ticket_messages WHERE ticket_id IN (SELECT id FROM tickets WHERE employee_id = ?)",
		"DELETE FROM bot_questions WHERE employee_id = ?",
		"DELETE FROM tickets WHERE employee_id = ?",
		"DELETE FROM human_requests WHERE employee_id = ?",
		"DELETE FROM survey_answers WHERE employee_id = ?",
		"DELETE FROM scheduled_events WHERE employee_id = ?",
		"DELETE FROM invite_tokens WHERE employee_id = ?",
		"DELETE FROM employees WHERE id = ?",
	} {
		if err := db.Exec(q, id).Error; err != nil {
			t.Fatal(q, err)
		}
	}
}

// Сценарий из прода: ответ rag, отказ, «Спросить человека», «Добавить детали», ответ HR с
// закрытием; затем новичка удаляют, тот же телефон входит новым новичком и жмёт все старые
// кнопки — в чате с нормальным апдейтом и в апдейте без сообщения (chat 0). Ни одного
// сообщения без адресата, на кнопки удалённых записей — «Это сообщение устарело».
func TestStaleButtonsAfterNewcomerDeleted(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	ctx := context.Background()
	f.set(ragAnswer)
	e.ask("когда можно в отпуск?")
	f.set(ragAbstain)
	e.ask("а что с дмс?")
	qid := questionID(t, lastSent(t, e.fake))
	e.press(qPayload(preQHR, qid))
	tk := e.onlyTicket(t, c.Employee.ID)
	e.press(preTicketDetails + tk.ID.String())
	e.say("детали")
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, "Анна HR", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Tickets.Reply(ctx, tk.ID, "ответ HR", true); err != nil {
		t.Fatal(err)
	}
	e.svc.Bot.Wait()
	payloads := payloadsOf(e.fake)
	if len(payloads) < 10 {
		t.Fatalf("кнопок мало: %v", payloads)
	}

	deleteEmployee(t, rawDB(t), c.Employee.ID)
	c2, err := e.svc.Newcomers.Create(ctx, NewcomerInput{
		FullName: "Второй Пётр (синтетика)", Site: domain.SiteGTP, StartDate: date(2026, 11, 3), ScenarioID: c.Employee.ScenarioID,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.connect(t, c2, domain.AddressVy, true)
	e.fake.Reset()

	for _, chat := range []int64{chatID, 0} {
		for _, p := range payloads {
			e.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateMessageCallback, UserID: userID, ChatID: chat, Payload: p, CallbackID: "cb-" + p})
			e.svc.Bot.Wait()
			if r := e.fake.Refused(); len(r) > 0 {
				t.Fatalf("chat=%d, кнопка %q: сообщение без получателя %+v", chat, p, r)
			}
			a := lastAnswer(t, e.fake)
			if (isQuestionPayload(p) || isTicketPayload(p)) && a.Answer.Notification != textButtonStale {
				t.Fatalf("chat=%d, кнопка %q: %q", chat, p, a.Answer.Notification)
			}
		}
	}
	for _, m := range e.fake.Sent() {
		if m.ChatID != chatID && m.UserID != userID {
			t.Fatalf("сообщение не тому: %+v", m)
		}
	}
}

// Кнопки удалённого события — «Это сообщение устарело», а не «Уже записал» и не пустой ответ.
func TestStaleStepButton(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	e.fake.Reset()
	gone := uuid.New().String()
	for _, p := range []string{preStepButton + gone + ":0", preContinue + gone, preAccessReason + gone + ":0", preTerminal + gone + ":b0:0:0", preAnswer + gone + ":0:3"} {
		e.press(p)
		if a := lastAnswer(t, e.fake); a.Answer.Notification != textButtonStale || a.Answer.Message != nil {
			t.Fatalf("%q: %+v", p, a.Answer)
		}
	}
	if len(e.fake.Sent()) != 0 || len(e.fake.Refused()) != 0 {
		t.Fatalf("ушло %+v, без адресата %+v", e.fake.Sent(), e.fake.Refused())
	}
}

// Апдейт без user и chat: раньше message() не находил карточку (ByMaxUser(0)) и слал «ссылка
// не сработала» по пустому адресу — ровно строка из прода. Теперь событие пропускается.
func TestUpdateWithoutRecipientSkipped(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	ctx := context.Background()
	for _, u := range []max.Update{
		{Type: max.UpdateMessageCreated, Text: "привет"},
		{Type: max.UpdateMessageCreated},
		{Type: max.UpdateBotStarted, Payload: "inv_нет"},
		{Type: max.UpdateMessageCallback, Payload: payConsentMore, CallbackID: "cb"},
		{Type: max.UpdateMessageCallback, ChatID: chatID, Payload: payConsentMore, CallbackID: "cb2"},
	} {
		e.svc.Bot.Handle(ctx, u)
	}
	e.svc.Bot.Wait()
	if len(e.fake.Sent()) != 0 || len(e.fake.Refused()) != 0 {
		t.Fatalf("ушло %+v, без адресата %+v", e.fake.Sent(), e.fake.Refused())
	}
}

// Нажатие отвечает в чат нажатия: чат, которого не было в карточке (bot_started без chat),
// запоминается, и по нему уходят следующие сообщения.
func TestCallbackRecipientFromUpdate(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	ctx := context.Background()
	e.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: userID, Payload: e.token(t, c)})
	if emp := e.employee(t, c.Employee.ID); emp.ChatID != nil {
		t.Fatalf("chat %v", *emp.ChatID)
	}
	e.fake.Reset()
	e.press(payConsentMore)
	m := lastSent(t, e.fake)
	if m.ChatID != chatID || m.UserID != userID || m.Text != textConsentMore {
		t.Fatalf("%+v", m)
	}
	if emp := e.employee(t, c.Employee.ID); emp.ChatID == nil || *emp.ChatID != chatID {
		t.Fatalf("chat не запомнен: %v", emp.ChatID)
	}
}

// Такт планировщика не берёт карточку без привязки к MAX, даже если она числится подключённой:
// события ждут, сообщений без адресата нет.
func TestSchedulerSkipsUnbound(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()
	if err := rawDB(t).Exec("UPDATE employees SET max_user_id = NULL, chat_id = NULL WHERE id = ?", c.Employee.ID).Error; err != nil {
		t.Fatal(err)
	}
	*e.clock = msk(2026, 11, 3, 19, 0) // D0: шаг 10:00 и пульс 18:00 созрели
	e.fake.Reset()
	if n, err := e.svc.Scheduler.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("отправлено %d, %v", n, err)
	}
	if len(e.fake.Sent()) != 0 || len(e.fake.Refused()) != 0 {
		t.Fatalf("ушло %+v, без адресата %+v", e.fake.Sent(), e.fake.Refused())
	}
	if d0 := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём"); d0.Status != domain.EventScheduled {
		t.Fatalf("шаг D0: %s", d0.Status)
	}
}

// send и resend без адресата MAX не зовут (без базы: только подделка).
func TestSendWithoutRecipient(t *testing.T) {
	f := max.NewFake()
	b := NewBotService(f, nil, nil, nil)
	b.send(context.Background(), max.OutMessage{Text: "текст"})
	b.resend(context.Background(), surveyOutcome{msg: &max.OutMessage{Text: "опрос"}, eventID: uuid.New()})
	if len(f.Sent()) != 0 || len(f.Refused()) != 0 {
		t.Fatalf("ушло %+v, без адресата %+v", f.Sent(), f.Refused())
	}
	if got := callerOf(1); !strings.Contains(got, "TestSendWithoutRecipient") {
		t.Fatalf("вызывающий %q", got)
	}
}
