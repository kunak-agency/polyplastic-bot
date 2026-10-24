package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/repositories"
)

// Закрытие петли (#31): «что сделано» при решении → новичку «По вашему сигналу: …» реплаем на
// исходное сообщение; анонимным и F5/F9/F10 — не уходит; заметка — в переписке и в дайджесте.

func doneNotes(t *testing.T, e *env, id domain.Ticket) []domain.TicketMessage {
	t.Helper()
	msgs, err := e.repos.Tickets.Messages(context.Background(), id.ID)
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.TicketMessage
	for _, m := range msgs {
		if m.Internal && strings.HasPrefix(m.Text, DoneNotePrefix) {
			out = append(out, m)
		}
	}
	return out
}

func TestResolveDoneNoteToNewcomer(t *testing.T) {
	e, f, c := askEnv(t, time.Second)
	e.withReplies()
	ctx := context.Background()

	f.set(ragAbstain)
	e.sayMID("сколько стоит парковка", "mid.q2")
	m := lastSent(t, e.fake)
	e.pressOn(m.Buttons[0][0].Payload, e.question(t, questionID(t, m)).MessageID)
	tk := e.onlyTicket(t, c.Employee.ID)
	if !DoneNoteReachesNewcomer(&tk) {
		t.Fatalf("адресное обращение %+v", tk)
	}
	// Тон карточки (#26) — тон вопроса, из которого выросло обращение.
	if tk.QuestionID == nil {
		t.Fatal("обращение без вопроса")
	}
	if _, err := e.repos.Questions.SetSentiment(ctx, *tk.QuestionID, domain.SentimentNeg); err != nil {
		t.Fatal(err)
	}
	if v, err := e.svc.Tickets.Get(ctx, tk.ID); err != nil || v.Tone == nil || *v.Tone != domain.SentimentNeg {
		t.Fatalf("тон карточки %+v %v", v, err)
	}

	e.fake.Reset()
	v, err := e.svc.Tickets.Resolve(ctx, tk.ID, "  Договорились с охраной:\nпарковка бесплатная  ")
	if err != nil || v.Ticket.Status != domain.TicketResolved {
		t.Fatalf("%+v %v", v, err)
	}
	s := e.fake.Sent()
	if len(s) != 2 {
		t.Fatalf("ушло %d: %+v", len(s), s)
	}
	if !strings.Contains(s[0].Text, "По вашему сигналу: Договорились с охраной: парковка бесплатная") || s[0].ReplyTo != "mid.q2" {
		t.Fatalf("«что сделано»: reply_to %q, %q", s[0].ReplyTo, s[0].Text)
	}
	if !strings.HasPrefix(s[1].Text, "✅") {
		t.Fatalf("«Вопрос закрыт» вторым: %q", s[1].Text)
	}
	notes := doneNotes(t, e, tk)
	if len(notes) != 1 || notes[0].Text != DoneNotePrefix+"Договорились с охраной: парковка бесплатная" {
		t.Fatalf("заметка %+v", notes)
	}

	// Дайджест: заметка за период — в «Сделано по темам», без имени новичка.
	rows, err := e.repos.Overview().DigestDone(ctx, "", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(rows) != 1 || rows[0].Category != tk.Category {
		t.Fatalf("строки дайджеста %+v %v", rows, err)
	}
	done := BuildDigestDone(rows, ovTax(t))
	if len(done) != 1 || done[0].Count != 1 || len(done[0].Notes) != 1 || !strings.Contains(done[0].Notes[0], "парковка бесплатная") {
		t.Fatalf("сделано %+v", done)
	}
	// Решённое обращение повторно — 409, вторая заметка не пишется.
	if _, err := e.svc.Tickets.Resolve(ctx, tk.ID, "ещё раз"); err == nil {
		t.Fatal("resolve закрытого")
	}
}

// Без «что сделано» — только «Вопрос закрыт»; F5 и анонимное — заметка есть, новичку не уходит.
func TestResolveDoneNoteSilent(t *testing.T) {
	e, _, c := askEnv(t, time.Second)
	ctx := context.Background()

	e.ask("бригадир ударил новичка")
	f5 := e.onlyTicket(t, c.Employee.ID)
	if f5.Category != "F5" || DoneNoteReachesNewcomer(&f5) {
		t.Fatalf("F5 %+v", f5)
	}
	e.fake.Reset()
	if _, err := e.svc.Tickets.Resolve(ctx, f5.ID, "Провели разбор со сменой"); err != nil {
		t.Fatal(err)
	}
	for _, m := range e.fake.Sent() {
		if strings.Contains(m.Text, "сигналу") {
			t.Fatalf("F5: «что сделано» ушло новичку: %q", m.Text)
		}
	}
	if len(doneNotes(t, e, f5)) != 1 {
		t.Fatal("F5: заметки нет")
	}

	e.press(payHuman)
	var q domain.Ticket
	for _, x := range e.tickets(t, c.Employee.ID) {
		if x.Status != domain.TicketResolved {
			q = x
		}
	}
	q.Anonymous = true
	if err := e.repos.Tickets.Save(ctx, &q); err != nil {
		t.Fatal(err)
	}
	e.fake.Reset()
	if _, err := e.svc.Tickets.Resolve(ctx, q.ID, "Ответили в общем чате"); err != nil {
		t.Fatal(err)
	}
	s := e.fake.Sent()
	if len(s) != 1 || !strings.HasPrefix(s[0].Text, "✅") {
		t.Fatalf("анонимное: ушло %+v", s)
	}

	// В дайджест F5 не идёт: из двух заметок — одна, у вопроса.
	rows, err := e.repos.Overview().DigestDone(ctx, "", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(rows) != 2 {
		t.Fatalf("строки %+v %v", rows, err)
	}
	if done := BuildDigestDone(rows, ovTax(t)); len(done) != 1 || done[0].Category != q.Category {
		t.Fatalf("сделано %+v", done)
	}
}

// Группировка без базы: по категории, по убыванию, до трёх строк, имя замаскировано;
// блок MAX-сообщения — числа из кода.
func TestBuildDigestDone(t *testing.T) {
	tax := ovTax(t)
	row := func(cat, text string) repositories.DoneNoteRow {
		return repositories.DoneNoteRow{Category: cat, FullName: "Тестов Пётр Иванович", Text: DoneNotePrefix + text}
	}
	rows := []repositories.DoneNoteRow{
		row("F2", "Выдали пропуск Тестову, кабинет 214"),
		row("F2", "Завели учётку в 1С"),
		row("F2", "Починили принтер"),
		row("F2", "Выдали спецовку"),
		row("question", "Объяснили график"),
		row("F5", "Разбор со сменой"),
		row("F10", "Перевели на дневные смены"),
		{Category: "F1", Restricted: true, Text: DoneNotePrefix + "скрытое"},
		row("F3", "   "),
	}
	done := BuildDigestDone(rows, tax)
	if len(done) != 2 || done[0].Category != "F2" || done[0].Count != 4 || len(done[0].Notes) != 3 || done[1].Category != "question" {
		t.Fatalf("%+v", done)
	}
	if strings.Contains(done[0].Notes[0], "Тестов") {
		t.Fatalf("не обезличено: %q", done[0].Notes[0])
	}
	block := digestDoneBlock(done)
	if !strings.HasPrefix(block, "Сделано по темам:\n") || !strings.Contains(block, "— 4 обращения: ") || !strings.Contains(block, "Вопросы — 1 обращение") {
		t.Fatalf("%q", block)
	}
	if digestDoneBlock(nil) != "" {
		t.Fatal("пустой блок")
	}
	d := BuildDigest(nil, nil, "", time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), tax)
	d.Done = done
	if msg := DigestMaxMessage(d); !strings.Contains(msg, "Сделано по темам:") {
		t.Fatalf("нет в MAX: %q", msg)
	}
}
