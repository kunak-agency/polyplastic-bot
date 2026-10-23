package max

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// DefaultSendInterval — пауза между сообщениями в один чат. Лимит MAX — 2 сообщения/с на
// диалог; 600 мс, а не 500, дают запас на дрожание сети: два запроса, ушедшие с разницей
// 500 мс, могут прийти на сервер ближе друг к другу.
const DefaultSendInterval = 600 * time.Millisecond

// pacer — очередь отправки по чатам. На каждый чат свой замок: пока одна отправка ждёт паузы
// и идёт в сеть, следующие в тот же чат стоят за ней, а другие чаты не ждут вовсе.
//
// Записи чатов не удаляются: на масштабе пилота (сотни новичков) это килобайты. Если чатов
// станет много — чистить записи, у которых next давно в прошлом.
type pacer struct {
	interval time.Duration
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error

	mu    sync.Mutex
	chats map[string]*chatSlot
}

type chatSlot struct {
	mu   sync.Mutex
	next time.Time // раньше этого момента в чат не отправляем
}

func newPacer(interval time.Duration) *pacer {
	if interval <= 0 {
		interval = DefaultSendInterval
	}
	return &pacer{interval: interval, now: time.Now, sleep: sleepCtx, chats: map[string]*chatSlot{}}
}

// Do — выполнить fn в очереди чата key. Пауза отсчитывается от конца предыдущей отправки, а не
// от начала: медленный ответ сервера не съедает интервал. Отправка с ошибкой тоже занимает
// слот — 429 и сетевой сбой не повод сразу слать следующее.
func (p *pacer) Do(ctx context.Context, key string, fn func() error) error {
	slot := p.slot(key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if wait := slot.next.Sub(p.now()); wait > 0 {
		if err := p.sleep(ctx, wait); err != nil {
			return err
		}
	}
	err := fn()
	slot.next = p.now().Add(p.interval)
	return err
}

func (p *pacer) slot(key string) *chatSlot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.chats[key]
	if !ok {
		s = &chatSlot{}
		p.chats[key] = s
	}
	return s
}

// chatKey — ключ очереди: чат, а для отправки по user_id — пользователь. Диалог с ботом и
// user_id одного человека — один и тот же чат на стороне MAX, но chat_id до первого апдейта
// неизвестен; ключи разные, и в худшем случае два сообщения уйдут с паузой меньше интервала —
// тогда сработает пауза на 429.
func chatKey(chatID, userID int64) string {
	if chatID != 0 {
		return "c" + strconv.FormatInt(chatID, 10)
	}
	return "u" + strconv.FormatInt(userID, 10)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
