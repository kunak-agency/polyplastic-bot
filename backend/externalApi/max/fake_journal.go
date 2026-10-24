package max

import (
	"fmt"
	"time"
)

// Журнал подделки — лента диалога «как в MAX» для демо-чата кабинета (GET /api/dev/chat/{id},
// только development и только на подделке). Пишет исходящие бота (Send), их правки (Edit и
// AnswerCallback с Message), всплывающие уведомления (AnswerCallback с Notification) и входящие
// от новичка, которые dev-хуки сообщают сами (JournalIncoming, JournalPress). «Печатает» —
// не запись, а флаг чата с коротким сроком, как в MAX.

// Кто автор записи журнала.
const (
	JournalFromBot      = "bot"
	JournalFromNewcomer = "newcomer"
	JournalFromToast    = "toast" // всплывающее уведомление ответа на нажатие
)

// Виды входящих записей новичка.
const (
	JournalKindText  = "text"
	JournalKindPress = "press" // нажатая кнопка: Text — подпись, Payload — payload
	JournalKindStart = "start" // «Начать» по ссылке-приглашению (bot_started)
	JournalKindVoice = "voice" // голосовое: Text — подпись «🎙 Голосовое, N с»
)

// Типы кнопок — как type в inline_keyboard MAX.
const (
	ButtonTypeCallback = "callback"
	ButtonTypeLink     = "link"
	ButtonTypeContact  = "request_contact"
)

// typingFor — сколько MAX держит «печатает» после typing_on (бот повторяет действие раз в 4 с).
const typingFor = 5 * time.Second

// journalCap — предел записей в журнале: подделка живёт весь процесс dev-сервера.
const journalCap = 5000

// JournalEntry — запись ленты. ID — mid для сообщений бота, иначе служебный.
type JournalEntry struct {
	Seq         int
	ID          string
	ChatID      int64
	UserID      int64
	From        string
	Kind        string // для From=newcomer
	Text        string
	Format      string
	Buttons     [][]JournalButton
	Attachments []JournalAttachment
	Notify      bool
	Payload     string // нажатая кнопка
	ReplyTo     string // mid сообщения, на кнопку которого нажали
	Edited      bool
	At          time.Time
	EditedAt    *time.Time
}

// JournalButton — кнопка в журнале: тип MAX и payload или ссылка.
type JournalButton struct {
	Text    string
	Type    string
	Payload string
	URL     string
}

// JournalAttachment — вложение: вид и адрес. У подделки адреса нет — токен загрузки.
type JournalAttachment struct {
	Type string
	URL  string
}

type pressRef struct {
	chat, user int64
	mid        string
}

func journalButtons(rows [][]Button) [][]JournalButton {
	if len(rows) == 0 {
		return nil
	}
	out := make([][]JournalButton, 0, len(rows))
	for _, row := range rows {
		r := make([]JournalButton, 0, len(row))
		for _, b := range row {
			jb := JournalButton{Text: b.Text, Type: ButtonTypeCallback, Payload: b.Payload}
			switch {
			case b.Contact:
				jb = JournalButton{Text: b.Text, Type: ButtonTypeContact}
			case b.URL != "":
				jb = JournalButton{Text: b.Text, Type: ButtonTypeLink, URL: b.URL}
			}
			r = append(r, jb)
		}
		out = append(out, r)
	}
	return out
}

func journalAttachments(refs []AttachmentRef) []JournalAttachment {
	if len(refs) == 0 {
		return nil
	}
	out := make([]JournalAttachment, 0, len(refs))
	for _, a := range refs {
		out = append(out, JournalAttachment{Type: string(a.Kind), URL: a.Token})
	}
	return out
}

// journalAdd — под f.mu.
func (f *Fake) journalAdd(e JournalEntry) {
	f.seq++
	e.Seq = f.seq
	if e.At.IsZero() {
		e.At = time.Now()
	}
	f.journal = append(f.journal, e)
	if len(f.journal) > journalCap {
		f.journal = append([]JournalEntry(nil), f.journal[len(f.journal)-journalCap:]...)
	}
}

// journalSent — исходящее Send (под f.mu). Сообщение в чат гасит «печатает».
func (f *Fake) journalSent(mid string, m OutMessage) {
	f.journalAdd(JournalEntry{
		ID: mid, ChatID: m.ChatID, UserID: m.UserID, From: JournalFromBot, Text: m.Text, Format: m.Format,
		Buttons: journalButtons(m.Buttons), Attachments: journalAttachments(m.Attachments), Notify: !m.Silent,
	})
	if f.typingUntil != nil {
		delete(f.typingUntil, journalChat(m.ChatID, m.UserID))
	}
}

// journalEdit — правка сообщения mid (под f.mu): текст, формат и кнопки заменяются целиком,
// как в MAX. Неизвестный mid — пропуск.
func (f *Fake) journalEdit(mid string, m OutMessage) {
	for i := len(f.journal) - 1; i >= 0; i-- {
		e := &f.journal[i]
		if e.ID != mid || e.From != JournalFromBot {
			continue
		}
		now := time.Now()
		e.Text, e.Format = m.Text, m.Format
		e.Buttons = journalButtons(m.Buttons)
		if m.Attachments != nil {
			e.Attachments = journalAttachments(m.Attachments)
		}
		e.Edited, e.EditedAt = true, &now
		return
	}
}

// journalAnswer — ответ на нажатие (под f.mu): правка сообщения, на кнопку которого нажали,
// и/или всплывающее уведомление. Нажатие не через JournalPress — адресат неизвестен, пропуск.
func (f *Fake) journalAnswer(callbackID string, a Answer) {
	ref, ok := f.presses[callbackID]
	if !ok {
		return
	}
	if a.Message != nil && ref.mid != "" {
		f.journalEdit(ref.mid, *a.Message)
	}
	if a.Notification != "" {
		f.journalAdd(JournalEntry{
			ID: fmt.Sprintf("toast.%d", f.seq+1), ChatID: ref.chat, UserID: ref.user,
			From: JournalFromToast, Text: a.Notification, ReplyTo: ref.mid,
		})
	}
}

func journalChat(chat, user int64) int64 {
	if chat != 0 {
		return chat
	}
	return user
}

// journalTyping — «печатает» в чате на typingFor (под f.mu).
func (f *Fake) journalTyping(chat int64) {
	if f.typingUntil == nil {
		f.typingUntil = map[int64]time.Time{}
	}
	f.typingUntil[chat] = time.Now().Add(typingFor)
}

// JournalIncoming — входящее от новичка (текст или «Начать»), как его увидел бы MAX.
func (f *Fake) JournalIncoming(chat, user int64, kind, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.journalAdd(JournalEntry{
		ID: fmt.Sprintf("in.%d", f.seq+1), ChatID: chat, UserID: user, From: JournalFromNewcomer, Kind: kind, Text: text,
	})
}

// JournalPress — новичок нажал кнопку payload на сообщении mid. mid пуст — последнее
// сообщение бота этому новичку с такой кнопкой (так играют dev-хуки, не знающие mid).
// Возвращает callback_id для Update: ответ бота на него (AnswerCallback) правит именно это
// сообщение. Подпись кнопки — в ленту; кнопки нет — в ленте payload.
func (f *Fake) JournalPress(chat, user int64, mid, payload string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	label := ""
	for i := len(f.journal) - 1; i >= 0; i-- {
		e := f.journal[i]
		if e.From != JournalFromBot || !e.belongs(chat, user) || (mid != "" && e.ID != mid) {
			continue
		}
		if l, ok := findButton(e.Buttons, payload); ok {
			mid, label = e.ID, l
			break
		}
		if mid != "" {
			break
		}
	}
	if label == "" {
		label = payload
	}
	f.journalAdd(JournalEntry{
		ID: fmt.Sprintf("in.%d", f.seq+1), ChatID: chat, UserID: user, From: JournalFromNewcomer,
		Kind: JournalKindPress, Text: label, Payload: payload, ReplyTo: mid,
	})
	cb := fmt.Sprintf("dev-press.%d", f.seq)
	if f.presses == nil {
		f.presses = map[string]pressRef{}
	}
	f.presses[cb] = pressRef{chat: chat, user: user, mid: mid}
	return cb
}

func findButton(rows [][]JournalButton, payload string) (string, bool) {
	for _, row := range rows {
		for _, b := range row {
			if b.Type == ButtonTypeCallback && b.Payload == payload {
				return b.Text, true
			}
		}
	}
	return "", false
}

func (e JournalEntry) belongs(chat, user int64) bool {
	return (chat != 0 && e.ChatID == chat) || (user != 0 && e.UserID == user)
}

// Chat — лента диалога с новичком (его chat или user id) в порядке записи и флаг «печатает».
func (f *Fake) Chat(chat, user int64) ([]JournalEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []JournalEntry
	for _, e := range f.journal {
		if e.belongs(chat, user) {
			e.Buttons = cloneRows(e.Buttons)
			e.Attachments = append([]JournalAttachment(nil), e.Attachments...)
			out = append(out, e)
		}
	}
	typing := false
	for _, k := range []int64{chat, user} {
		if until, ok := f.typingUntil[k]; ok && k != 0 && time.Now().Before(until) {
			typing = true
		}
	}
	return out, typing
}

// HasMessage — есть ли сообщение бота mid в диалоге с новичком.
func (f *Fake) HasMessage(chat, user int64, mid string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.journal {
		if e.ID == mid && e.From == JournalFromBot && e.belongs(chat, user) {
			return true
		}
	}
	return false
}

func cloneRows(rows [][]JournalButton) [][]JournalButton {
	if rows == nil {
		return nil
	}
	out := make([][]JournalButton, len(rows))
	for i, r := range rows {
		out[i] = append([]JournalButton(nil), r...)
	}
	return out
}
