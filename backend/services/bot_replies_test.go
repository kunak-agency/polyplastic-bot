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

// Реплаи (FEATURE_REPLIES): ответ бота на сообщение новичка уходит со ссылкой на него.

// withReplies — флаг FEATURE_REPLIES в боте и обращениях.
func (e *env) withReplies() {
	e.svc.Bot.replies = true
	e.svc.Tickets.replies = true
}

// sayMID — текст новичка с mid, как из long polling, и ожидание фонового ответа.
func (e *env) sayMID(text, mid string) {
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, UserID: userID, ChatID: chatID, Text: text, MessageID: mid})
	e.svc.Bot.Wait()
}

// Ответ rag — реплаем на вопрос; «не нашёл» → «Передать HR» → OPEN реплаем на тот же вопрос; ответ
// HR из кабинета — тоже на него.
func TestRepliesAnswerHandoffAndStaff(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	e.withReplies()
	ctx := context.Background()

	f.set(ragAnswer)
	e.sayMID("когда можно в отпуск?", "mid.q1")
	if m := lastSent(t, e.fake); m.ReplyTo != "mid.q1" || !strings.Contains(m.Text, "Отпуск можно взять") {
		t.Fatalf("ответ rag: reply_to %q, %q", m.ReplyTo, m.Text)
	}
	if q := e.question(t, questionID(t, lastSent(t, e.fake))); deref(q.SourceMID) != "mid.q1" {
		t.Fatalf("source_mid %v", q.SourceMID)
	}

	f.set(ragAbstain)
	e.sayMID("сколько стоит парковка", "mid.q2")
	m := lastSent(t, e.fake)
	if m.Text != textAbstain || m.ReplyTo != "mid.q2" {
		t.Fatalf("«не нашёл»: reply_to %q, %q", m.ReplyTo, m.Text)
	}
	qid := questionID(t, m)
	e.fake.Reset()
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, qid).MessageID)
	open := lastSent(t, e.fake)
	if !strings.Contains(open.Text, "Передал") || open.ReplyTo != "mid.q2" {
		t.Fatalf("OPEN: reply_to %q, %q", open.ReplyTo, open.Text)
	}

	tk := e.onlyTicket(t, c.Employee.ID)
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, AssignInput{Name: "Анна", Comment: ""}); err != nil {
		t.Fatal(err)
	}
	e.fake.Reset()
	if _, err := e.svc.Tickets.Reply(ctx, tk.ID, "Парковка бесплатная", false); err != nil {
		t.Fatal(err)
	}
	if s := e.fake.Sent(); len(s) != 1 || s[0].ReplyTo != "mid.q2" || !strings.Contains(s[0].Text, "Парковка бесплатная") {
		t.Fatalf("ответ HR: %+v", s)
	}

	// Переписка с HR: «Передал HR…» на первое сообщение после ответа — реплаем на него.
	e.fake.Reset()
	e.sayMID("а где именно?", "mid.c1")
	if m := lastSent(t, e.fake); m.ReplyTo != "mid.c1" {
		t.Fatalf("подтверждение в переписке: reply_to %q, %q", m.ReplyTo, m.Text)
	}
	// Правки (ASSIGNED и т. п.) реплаем не становятся: ReplyTo в Edit не ставится.
	for _, ed := range e.fake.Edits() {
		if ed.Message.ReplyTo != "" {
			t.Fatalf("правка с reply_to: %+v", ed)
		}
	}
}

// Голосовое: «🎙 Распознал: …» и ответ rag — реплаем на голосовое.
func TestRepliesVoice(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	e.withReplies()
	f.set(ragAnswer)
	e.svc.Bot.asr = rag.New(newFakeASR(t, "когда можно в отпуск").srv.URL, 0)

	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, UserID: userID, ChatID: chatID,
		MessageID: "mid.v1", Audio: []max.Audio{{URL: e.fake.PutFile([]byte(voiceBytes)), Duration: 3}}})
	e.svc.Bot.Wait()

	s := e.fake.Sent()
	if len(s) < 2 || !strings.HasPrefix(s[0].Text, voicePrefix) || s[0].ReplyTo != "mid.v1" {
		t.Fatalf("эхо распознанного: %+v", s)
	}
	if last := s[len(s)-1]; last.ReplyTo != "mid.v1" || !strings.Contains(last.Text, "Отпуск") {
		t.Fatalf("ответ rag на голосовое: %+v", last)
	}
}

// MAX отказал в реплае (сообщение удалено) — тот же текст уходит без link, новичок ответ получает.
func TestRepliesRetryWithoutLink(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	e.withReplies()
	f.set(ragAnswer)
	e.fake.FailReplies(1)

	e.sayMID("когда можно в отпуск?", "mid.gone")

	failed := e.fake.ReplyFailed()
	if len(failed) != 1 || failed[0].ReplyTo != "mid.gone" {
		t.Fatalf("попытка с link: %+v", failed)
	}
	s := e.fake.Sent()
	if len(s) != 1 || s[0].ReplyTo != "" || s[0].Text != failed[0].Text {
		t.Fatalf("повтор без link: %+v", s)
	}
	// mid повтора записан: кнопки «Подробнее» и т. п. правят именно его.
	if q := e.question(t, questionID(t, s[0])); q.MessageID == "" {
		t.Fatalf("mid ответа не записан: %+v", q)
	}
}

// Флаг выключен — поведение прежнее: ReplyTo нигде нет, даже если mid известен.
func TestRepliesOff(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	ctx := context.Background()
	f.set(ragAbstain)
	e.sayMID("сколько стоит парковка", "mid.q1")
	m := lastSent(t, e.fake)
	qid := questionID(t, m)
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, qid).MessageID)
	tk := e.onlyTicket(t, c.Employee.ID)
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, AssignInput{Name: "Анна", Comment: ""}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Tickets.Reply(ctx, tk.ID, "Бесплатная", false); err != nil {
		t.Fatal(err)
	}
	for _, s := range e.fake.Sent() {
		if s.ReplyTo != "" {
			t.Fatalf("реплай без флага: %+v", s)
		}
	}
	if q := e.question(t, qid); q.Kind != domain.QuestionAbstain || deref(q.SourceMID) != "mid.q1" {
		t.Fatalf("mid вопроса пишется и без флага: %+v", q)
	}
}

// sendReply без сети: флаг выключен — ReplyTo стёрт; ошибка без link — без повтора.
func TestSendReplyUnit(t *testing.T) {
	f := max.NewFake()
	f.FailReplies(5)
	ctx := context.Background()
	if _, err := sendReply(ctx, f, false, max.OutMessage{UserID: 1, Text: "a", ReplyTo: "mid.x"}); err != nil {
		t.Fatal(err)
	}
	if s := f.Sent(); len(s) != 1 || s[0].ReplyTo != "" || len(f.ReplyFailed()) != 0 {
		t.Fatalf("флаг выключен: %+v", s)
	}
	if _, err := sendReply(ctx, f, true, max.OutMessage{UserID: 1, Text: "b", ReplyTo: "mid.x"}); err != nil {
		t.Fatal(err)
	}
	if s := f.Sent(); len(s) != 2 || s[1].Text != "b" || s[1].ReplyTo != "" || len(f.ReplyFailed()) != 1 {
		t.Fatalf("повтор: %+v", s)
	}
	f.FailReplies(0)
	if _, err := sendReply(ctx, f, true, max.OutMessage{UserID: 1, Text: "c", ReplyTo: "mid.y"}); err != nil {
		t.Fatal(err)
	}
	if s := f.Sent(); s[2].ReplyTo != "mid.y" {
		t.Fatalf("реплай: %+v", s[2])
	}
	j, _ := f.Chat(0, 1)
	if j[len(j)-1].ReplyTo != "mid.y" {
		t.Fatalf("журнал демо-чата без reply_to: %+v", j[len(j)-1])
	}
}
