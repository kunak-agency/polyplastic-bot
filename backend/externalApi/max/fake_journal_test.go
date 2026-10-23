package max

import (
	"context"
	"testing"
)

// Журнал подделки — лента демо-чата: исходящие, правки через Edit и AnswerCallback, тосты,
// входящие новичка и «печатает»; чужие чаты в ленту не попадают.
func TestFakeJournal(t *testing.T) {
	ctx := context.Background()
	f := NewFake()
	const chat, user, other = 501, 501, 777

	if err := f.Typing(ctx, chat); err != nil {
		t.Fatal(err)
	}
	if _, typing := f.Chat(chat, user); !typing {
		t.Fatal("после Typing нет «печатает»")
	}
	f.JournalIncoming(chat, user, JournalKindText, "привет")
	mid, _ := f.Send(ctx, OutMessage{ChatID: chat, UserID: user, Text: "**Привет**", Format: "markdown", Buttons: [][]Button{
		{{Text: "Да", Payload: "y"}, {Text: "Сайт", URL: "https://example.org"}},
		{{Text: "Телефон", Contact: true}},
	}, Attachments: []AttachmentRef{{Kind: UploadImage, Token: "tok"}}, Silent: true})
	if _, typing := f.Chat(chat, user); typing {
		t.Fatal("сообщение не погасило «печатает»")
	}
	_, _ = f.Send(ctx, OutMessage{ChatID: other, Text: "чужое"})

	msgs, _ := f.Chat(chat, user)
	if len(msgs) != 2 || msgs[0].From != JournalFromNewcomer || msgs[0].Text != "привет" || msgs[1].ID != mid {
		t.Fatalf("лента: %+v", msgs)
	}
	m := msgs[1]
	if m.Format != "markdown" || m.Notify || len(m.Attachments) != 1 || m.Attachments[0].Type != "image" || m.Attachments[0].URL != "tok" {
		t.Fatalf("сообщение: %+v", m)
	}
	want := [][]JournalButton{
		{{Text: "Да", Type: ButtonTypeCallback, Payload: "y"}, {Text: "Сайт", Type: ButtonTypeLink, URL: "https://example.org"}},
		{{Text: "Телефон", Type: ButtonTypeContact}},
	}
	if len(m.Buttons) != 2 || len(m.Buttons[0]) != 2 || m.Buttons[0][0] != want[0][0] || m.Buttons[0][1] != want[0][1] || m.Buttons[1][0] != want[1][0] {
		t.Fatalf("кнопки: %+v", m.Buttons)
	}

	// Нажатие без mid находит последнее сообщение с этой кнопкой; ответ правит его и даёт тост.
	cb := f.JournalPress(chat, user, "", "y")
	if err := f.AnswerCallback(ctx, cb, Answer{Notification: "Записал", Message: &OutMessage{Text: "✅ Да"}}); err != nil {
		t.Fatal(err)
	}
	msgs, _ = f.Chat(chat, user)
	if len(msgs) != 4 {
		t.Fatalf("лента после нажатия: %+v", msgs)
	}
	if p := msgs[2]; p.From != JournalFromNewcomer || p.Kind != JournalKindPress || p.Text != "Да" || p.ReplyTo != mid {
		t.Fatalf("нажатие: %+v", p)
	}
	if e := msgs[1]; !e.Edited || e.Text != "✅ Да" || e.Buttons != nil || e.EditedAt == nil || e.Format != "" {
		t.Fatalf("правка ответом: %+v", e)
	}
	if ts := msgs[3]; ts.From != JournalFromToast || ts.Text != "Записал" || ts.ReplyTo != mid {
		t.Fatalf("тост: %+v", ts)
	}

	// Edit по mid; неизвестный callback — без следов.
	_ = f.Edit(ctx, mid, OutMessage{ChatID: chat, Text: "итог", Buttons: [][]Button{{{Text: "Ещё", Payload: "m"}}}})
	_ = f.AnswerCallback(ctx, "чужой", Answer{Notification: "x"})
	msgs, _ = f.Chat(chat, user)
	if len(msgs) != 4 || msgs[1].Text != "итог" || len(msgs[1].Buttons) != 1 {
		t.Fatalf("Edit: %+v", msgs)
	}
	if !f.HasMessage(chat, user, mid) || f.HasMessage(other, other, mid) {
		t.Fatal("HasMessage")
	}
	// Кнопки нет — в ленте payload.
	f.JournalPress(chat, user, mid, "zzz")
	msgs, _ = f.Chat(chat, user)
	if last := msgs[len(msgs)-1]; last.Text != "zzz" || last.ReplyTo != mid {
		t.Fatalf("нажатие без кнопки: %+v", last)
	}
}
