package audio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// Кадры уходят не быстрее реального времени: после опережения lead каждый следующий — не
// раньше чем через 20 мс после предыдущего по часам отправки.
func TestPacerKeepsRealTime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const lead = 60 * time.Millisecond
	p := NewPacer(ctx, lead, 200*time.Millisecond)
	leg := NewMemLeg("x")
	pcm := make([]byte, 25*FrameBytes) // 0,5 с
	start := time.Now()
	pl, err := p.Play(ctx, leg, pcm)
	if err != nil || !pl.Complete || pl.Sent != 500*time.Millisecond || pl.Total != pl.Sent {
		t.Fatalf("played %+v, err %v", pl, err)
	}
	sent := leg.Sent()
	if len(sent) != 25 {
		t.Fatalf("кадров %d", len(sent))
	}
	for i, f := range sent {
		// i-й кадр должен прозвучать в start+i·20 мс; отправить его можно не раньше чем за lead.
		earliest := start.Add(time.Duration(i)*FrameDur - lead - 2*time.Millisecond)
		if f.At.Before(earliest) {
			t.Fatalf("кадр %d ушёл на %v раньше темпа", i, earliest.Sub(f.At))
		}
	}
	if el := time.Since(start); el < 500*time.Millisecond-lead-FrameDur {
		t.Fatalf("0,5 с звука отправлены за %v — быстрее реального времени", el)
	}
	// EndsAt — конец звука по часам плюс запас.
	if d := pl.EndsAt.Sub(start); d < 650*time.Millisecond || d > 800*time.Millisecond {
		t.Fatalf("EndsAt через %v после начала", d)
	}
}

// Две фразы подряд — общие часы: вторая не догоняет первую пачкой.
func TestPacerContinuousAcrossCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacer(ctx, 40*time.Millisecond, 0)
	leg := NewMemLeg("x")
	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, err := p.Play(ctx, leg, make([]byte, 10*FrameBytes)); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 400*time.Millisecond-40*time.Millisecond-FrameDur {
		t.Fatalf("0,4 с звука за %v", el)
	}
}

// Перебивание: неотправленное выбрасывается, Played честно говорит, сколько ушло.
func TestPacerInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacer(ctx, 60*time.Millisecond, 200*time.Millisecond)
	leg := NewMemLeg("x")
	go func() {
		time.Sleep(200 * time.Millisecond)
		p.Interrupt()
	}()
	pl, err := p.Play(ctx, leg, make([]byte, 100*FrameBytes)) // 2 с
	if !errors.Is(err, ErrInterrupted) || pl.Complete {
		t.Fatalf("played %+v, err %v", pl, err)
	}
	if pl.Sent >= time.Second || pl.Sent < 150*time.Millisecond || pl.Total != 2*time.Second {
		t.Fatalf("отправлено %v из %v", pl.Sent, pl.Total)
	}
	n := len(leg.Sent())
	time.Sleep(100 * time.Millisecond)
	if len(leg.Sent()) != n {
		t.Fatal("после перебивания кадры продолжают уходить")
	}
	// Следующая фраза после перебивания играет как обычно.
	if pl, err := p.Play(ctx, leg, make([]byte, 2*FrameBytes)); err != nil || !pl.Complete {
		t.Fatalf("после перебивания: %+v %v", pl, err)
	}
}

// Пациент положил трубку посреди фразы.
func TestPacerLegClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacer(ctx, 60*time.Millisecond, 0)
	leg := NewMemLeg("x")
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = leg.Close()
	}()
	pl, err := p.Play(ctx, leg, make([]byte, 100*FrameBytes))
	if err == nil || pl.Complete || pl.Sent >= time.Second {
		t.Fatalf("played %+v, err %v", pl, err)
	}
}

// Хвост короче кадра дополняется тишиной до 320 байт.
func TestPacerPadsTail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPacer(ctx, 60*time.Millisecond, 0)
	leg := NewMemLeg("x")
	if _, err := p.Play(ctx, leg, make([]byte, FrameBytes+10)); err != nil {
		t.Fatal(err)
	}
	s := leg.Sent()
	if len(s) != 2 || len(s[1].Data) != FrameBytes {
		t.Fatalf("кадры: %d, хвост %d байт", len(s), len(s[len(s)-1].Data))
	}
}

// memLeg — нога в памяти для тестов Pacer: исходящие кадры копятся с метками времени.
type memLeg struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	sent []sentFrame
}

type sentFrame struct {
	At   time.Time
	Data []byte
}

func NewMemLeg(string) *memLeg { return &memLeg{done: make(chan struct{})} }

func (m *memLeg) Frames() <-chan []byte { return nil }
func (m *memLeg) Done() <-chan struct{} { return m.done }
func (m *memLeg) Close() error          { m.once.Do(func() { close(m.done) }); return nil }

func (m *memLeg) Send(frame []byte) error {
	select {
	case <-m.done:
		return errors.New("audio: нога закрыта")
	default:
	}
	m.mu.Lock()
	m.sent = append(m.sent, sentFrame{At: time.Now(), Data: append([]byte(nil), frame...)})
	m.mu.Unlock()
	return nil
}

func (m *memLeg) Sent() []sentFrame {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentFrame(nil), m.sent...)
}
