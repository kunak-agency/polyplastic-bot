package audio

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrInterrupted — фраза оборвана перебиванием.
var ErrInterrupted = errors.New("audio: фраза перебита")

// Pacer — Player с собственными часами отправки. Буфера ни у Asterisk, ни у телефона нет: что
// отправлено быстрее реального времени, то либо выброшено джиттер-буфером, либо уже не отзывается
// при перебивании. Поэтому кадры уходят не раньше, чем за lead до момента, когда они должны
// прозвучать, а «доиграно» считается по этим же часам плюс margin на джиттер-буфер телефона.
//
// Отправкой занимается своя горутина: в ней нет ни синтеза, ни распознавания, только часы и
// Leg.Send, — блокирующий сетевой вызов не сбивает темп (рваный звук в трубке).
type Pacer struct {
	lead, margin time.Duration
	jobs         chan *playJob

	mu    sync.Mutex
	stop  chan struct{} // закрывается перебиванием; задания, взятые до него, обрываются
	clock time.Time     // когда у пациента прозвучит уже отправленное
}

type playJob struct {
	ctx  context.Context
	leg  Leg
	pcm  []byte
	stop chan struct{}
	done chan Played
}

// NewPacer — lead: насколько звук опережает воспроизведение (браузер 300 мс, телефон 40–60 мс);
// margin: запас к часам отправки для EndsAt (телефон 150–250 мс). Горутина живёт до ctx.
func NewPacer(ctx context.Context, lead, margin time.Duration) *Pacer {
	p := &Pacer{lead: lead, margin: margin, jobs: make(chan *playJob), stop: make(chan struct{})}
	go p.run(ctx)
	return p
}

// Play — отправить pcm (кадрами OutFrameBytes, хвост дополняется тишиной) в темпе реального
// времени. Блокирует, пока всё не отправлено или не перебито. Часы общие для подряд идущих
// вызовов: фраза, синтезированная кусками, звучит без щелчков на стыках.
func (p *Pacer) Play(ctx context.Context, leg Leg, pcm []byte) (Played, error) {
	total := time.Duration((len(pcm)+OutFrameBytes-1)/OutFrameBytes) * FrameDur
	p.mu.Lock()
	stop := p.stop
	p.mu.Unlock()
	j := &playJob{ctx: ctx, leg: leg, pcm: pcm, stop: stop, done: make(chan Played, 1)}
	select {
	case p.jobs <- j:
	case <-ctx.Done():
		return Played{Total: total, EndsAt: p.EndsAt()}, ctx.Err()
	case <-stop:
		return Played{Total: total, EndsAt: p.EndsAt()}, ErrInterrupted
	}
	pl := <-j.done
	switch {
	case pl.Complete:
		return pl, nil
	case ctx.Err() != nil:
		return pl, ctx.Err()
	case isClosed(stop):
		return pl, ErrInterrupted
	}
	select {
	case <-leg.Done():
		return pl, errors.New("audio: нога закрыта")
	default:
	}
	return pl, errors.New("audio: кадр не отправлен")
}

// Interrupt — пациент перебил: текущее и стоящее в очереди не отправляется. Уже отправленное
// (не больше lead) отозвать нельзя — оно доиграет.
func (p *Pacer) Interrupt() {
	p.mu.Lock()
	close(p.stop)
	p.stop = make(chan struct{})
	p.mu.Unlock()
}

// EndsAt — когда у пациента закончится уже отправленный звук (с запасом margin).
func (p *Pacer) EndsAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clock.Add(p.margin)
}

// StartsAt — когда прозвучит кадр, отправленный сейчас: начало следующей фразы.
func (p *Pacer) StartsAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now := time.Now(); p.clock.Before(now) {
		return now
	}
	return p.clock
}

func (p *Pacer) run(ctx context.Context) {
	for {
		select {
		case j := <-p.jobs:
			j.done <- p.play(j)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pacer) play(j *playJob) (pl Played) {
	pl.Total = time.Duration((len(j.pcm)+OutFrameBytes-1)/OutFrameBytes) * FrameDur
	defer func() { pl.EndsAt = p.EndsAt() }()
	timer := time.NewTimer(0)
	<-timer.C
	defer timer.Stop()
	for off := 0; off < len(j.pcm); off += OutFrameBytes {
		now := time.Now()
		p.mu.Lock()
		if p.clock.Before(now) {
			p.clock = now
		}
		wait := p.clock.Sub(now) - p.lead
		p.mu.Unlock()
		if wait > 0 {
			timer.Reset(wait)
			select {
			case <-timer.C:
			case <-j.stop:
				return pl
			case <-j.ctx.Done():
				return pl
			case <-j.leg.Done():
				return pl
			}
		}
		if isClosed(j.stop) || j.ctx.Err() != nil {
			return pl
		}
		// Своя копия кадра: нога может отдать его писателю позже, а буфер вызывающего уже
		// переиспользован под следующий кусок синтеза. Хвост короче кадра — тишина.
		frame := make([]byte, OutFrameBytes)
		copy(frame, j.pcm[off:min(off+OutFrameBytes, len(j.pcm))])
		if err := j.leg.Send(frame); err != nil {
			return pl
		}
		p.mu.Lock()
		p.clock = p.clock.Add(FrameDur)
		p.mu.Unlock()
		pl.Sent += FrameDur
	}
	pl.Complete = true
	return pl
}

func isClosed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
