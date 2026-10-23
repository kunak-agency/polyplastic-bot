package max

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"main/pkg/logger"
)

// Fake — подделка Client для тестов бизнес-кода: запоминает исходящие, отдаёт события из
// очереди Push. Лимитов и сети нет. Живёт не в _test.go, чтобы её брали тесты других пакетов.
type Fake struct {
	mu      sync.Mutex
	sent    []OutMessage
	edits   []FakeEdit
	answers []FakeAnswer
	uploads []string
	typing  []int64
	nextMid int
	// commands — последний PATCH /me/commands (commands.go).
	commands []BotCommand

	// Журнал ленты для демо-чата (fake_journal.go).
	journal     []JournalEntry
	seq         int
	presses     map[string]pressRef
	typingUntil map[int64]time.Time

	batches chan fakeBatch
	marker  int64

	// Verbose — писать в лог факт отправки (адресат и длина, без текста): режим «бот на
	// подделке» в development, чтобы машина времени была видна без MAX.
	Verbose bool
}

// FakeEdit, FakeAnswer — записанные Edit и AnswerCallback.
type FakeEdit struct {
	MessageID string
	Message   OutMessage
}

type FakeAnswer struct {
	CallbackID string
	Answer     Answer
}

type fakeBatch struct {
	updates []Update
	err     error
}

var _ Client = (*Fake)(nil)

func NewFake() *Fake { return &Fake{batches: make(chan fakeBatch, 64)} }

// Push — пачка событий для следующего GetUpdates.
func (f *Fake) Push(updates ...Update) { f.batches <- fakeBatch{updates: updates} }

// PushError — следующий GetUpdates вернёт ошибку.
func (f *Fake) PushError(err error) { f.batches <- fakeBatch{err: err} }

func (f *Fake) Send(_ context.Context, m OutMessage) (string, error) {
	if m.ChatID == 0 && m.UserID == 0 {
		return "", errNoRecipient
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextMid++
	f.sent = append(f.sent, m)
	mid := fmt.Sprintf("mid.fake.%d", f.nextMid)
	f.journalSent(mid, m)
	if f.Verbose {
		logger.Info("подделка MAX: %s user=%d chat=%d, %d символов, рядов кнопок %d", mid, m.UserID, m.ChatID, len([]rune(m.Text)), len(m.Buttons))
	}
	return mid, nil
}

func (f *Fake) Edit(_ context.Context, messageID string, m OutMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, FakeEdit{MessageID: messageID, Message: m})
	f.journalEdit(messageID, m)
	if f.Verbose {
		logger.Info("подделка MAX: правка %s", messageID)
	}
	return nil
}

func (f *Fake) AnswerCallback(_ context.Context, callbackID string, a Answer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, FakeAnswer{CallbackID: callbackID, Answer: a})
	f.journalAnswer(callbackID, a)
	return nil
}

func (f *Fake) Upload(_ context.Context, kind UploadKind, r io.Reader, name string, _ int64) (string, error) {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, name)
	return fmt.Sprintf("token.fake.%s.%d", kind, len(f.uploads)), nil
}

// GetUpdates — следующая пачка из Push; пусто — ждёт пачку или отмену ctx, как long polling.
// marker растёт на размер пачки.
func (f *Fake) GetUpdates(ctx context.Context, _ int64) ([]Update, int64, error) {
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case b := <-f.batches:
		if b.err != nil {
			return nil, 0, b.err
		}
		f.mu.Lock()
		f.marker += int64(len(b.updates))
		m := f.marker
		f.mu.Unlock()
		return b.updates, m, nil
	}
}

// Reset — забыть записанные отправки, правки и ответы (между шагами теста).
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent, f.edits, f.answers, f.typing = nil, nil, nil, nil
}

// Sent — копия отправленных сообщений.
func (f *Fake) Sent() []OutMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]OutMessage(nil), f.sent...)
}

func (f *Fake) Edits() []FakeEdit {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeEdit(nil), f.edits...)
}

func (f *Fake) Answers() []FakeAnswer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeAnswer(nil), f.answers...)
}
