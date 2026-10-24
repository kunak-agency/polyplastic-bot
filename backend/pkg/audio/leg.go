// Package audio — звук звонка в браузере: кадры PCM16LE 8 кГц и Pacer, который отдаёт звук бота
// в темпе воспроизведения (порт telephony из hack-ru-clinik без телефонии и DTMF).
package audio

import (
	"context"
	"time"
)

// FrameBytes — кадр звука: PCM16LE, моно, 8 кГц, 20 мс. Формат браузерного канала и WS /stream.
const FrameBytes = 320

// OutRate, OutFrameBytes — звук бота в браузер: PCM16LE моно 24 кГц, кадр 20 мс. Микрофон и
// распознавание остаются на 8 кГц, а голос Корсика идёт в полном качестве синтеза.
const (
	OutRate       = 24000
	OutFrameBytes = OutRate / 50 * 2
)

// FrameDur — длительность кадра.
const FrameDur = 20 * time.Millisecond

// Leg — сторона звонка, по которой идёт звук (WebSocket браузера, подделка в тестах).
type Leg interface {
	// Frames — входящий звук собеседника кадрами по FrameBytes.
	Frames() <-chan []byte
	// Send — кадр звука бота. Темп реального времени держит Pacer, нога только пишет.
	Send(frame []byte) error
	// Done — нога закрыта (собеседник положил трубку).
	Done() <-chan struct{}
}

// Played — сколько звука фразы реально ушло к моменту остановки.
type Played struct {
	Sent     time.Duration // длительность отправленного звука
	Total    time.Duration // длительность фразы
	Complete bool          // отправлено целиком и не перебито
	EndsAt   time.Time     // когда последний кадр прозвучит у собеседника (с запасом)
}

// Player — проигрывание фразы в ногу в темпе реального времени с возможностью перебить.
type Player interface {
	Play(ctx context.Context, leg Leg, pcm []byte) (Played, error)
	// Interrupt — собеседник перебил: неотправленное выбрасывается.
	Interrupt()
}
