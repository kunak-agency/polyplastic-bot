package max

import (
	"context"
	"runtime/debug"
	"time"

	"main/pkg/logger"
)

// Handler — обработчик одного события. Ошибки обработчик решает сам (лог, ответ новичку):
// поллеру с ними делать нечего, а остановка цикла из-за одного сообщения уронила бы бота.
type Handler func(ctx context.Context, u Update)

// Poller — цикл long polling: GET /updates с marker, события по одному в Handle.
//
// События обрабатываются последовательно, в порядке прихода: так ответы одному новичку не
// перегоняют друг друга. Медленный обработчик задерживает следующий опрос — для пилота это
// приемлемо; распараллеливать по чатам, когда понадобится.
type Poller struct {
	Client Client
	Handle Handler
	// Marker — с какого места читать. TODO: хранить в БД (OnMarker), иначе после рестарта
	// marker теряется. [проверить]: что MAX отдаёт на опрос без marker — непрочитанные события
	// или только новые.
	Marker int64
	// OnMarker — вызывается после обработки пачки с новым marker (nil — не вызывается).
	OnMarker func(marker int64)
	// Backoff — пауза после ошибки опроса; 0 — 5 с. Свои повторы у SDK уже были (3 попытки).
	Backoff time.Duration
}

// Run — крутит цикл до отмены ctx. Возвращает ctx.Err().
func (p *Poller) Run(ctx context.Context) error {
	backoff := p.Backoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	logger.Info("бот MAX: long polling запущен")
	for {
		if err := ctx.Err(); err != nil {
			logger.Info("бот MAX: long polling остановлен")
			return err
		}
		ups, next, err := p.Client.GetUpdates(ctx, p.Marker)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			logger.Warn("бот MAX: опрос не удался: %v — пауза %v", err, backoff)
			_ = sleepCtx(ctx, backoff)
			continue
		}
		for _, u := range ups {
			p.handle(ctx, u)
		}
		if next != 0 && next != p.Marker {
			p.Marker = next
			if p.OnMarker != nil {
				p.OnMarker(next)
			}
		}
	}
}

// handle — паника в обработчике одного события не роняет цикл: событие теряется, бот живёт.
func (p *Poller) handle(ctx context.Context, u Update) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("бот MAX: паника в обработчике %s: %v\n%s", u.Type, r, debug.Stack())
		}
	}()
	p.Handle(ctx, u)
}
