package handlers

import (
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/externalApi/rag"
	"main/pkg/audio"
	"main/pkg/logger"
	"main/services"
)

// voiceCall — голос одного звонка Корсику: урезанный порт voiceCall из hack-ru-clinik
// (handlers/voice.go). Горутины: насос звука микрофона в распознавание (pump), читатель
// распознавания (readSpeech), исполнитель ходов (turns: реплика → rag /ask → фразы) и диктор
// (speak: синтез, звук в браузер через Pacer). Все живут до ctx звонка.

const (
	fillerAfter = 700 * time.Millisecond // rag думает дольше — Корсик говорит «секунду»
	ttsTimeout  = 30 * time.Second       // на синтез фразы целиком (звук дочитывается быстрее, чем звучит)
	browserLead = 300 * time.Millisecond // звук бота опережает воспроизведение в браузере
	// markGrace — браузер не прислал playback.mark: ход считается доигранным по часам отправки
	// плюс этот запас (иначе таймер тишины не взведётся никогда).
	markGrace = 3 * time.Second
	// byeWait — сколько ждать, пока браузер доиграет прощание, прежде чем call.end.
	byeWait = 15 * time.Second
)

// silenceAfter — пауза тишины: 8 с — «Вы меня слышите?», ещё 8 с — прощание. Переменная, а не
// константа, — чтобы тесты не ждали секундами.
var silenceAfter = 8 * time.Second

// voiceOpts — режим звонка.
type voiceOpts struct {
	audio   bool // голос: распознавание и синтез; false — только текст (микрофона нет)
	bargeIn bool // CALL_BARGE_IN
	greet   bool // CALL_SPOKEN_GREETING: приветствие голосом
	mute    bool // CALL_MUTE_WHILE_BOT: микрофон глушится, пока Корсик думает и говорит
	site    string
	callID  string
}

// Виды входа в очередь ходов.
const (
	inputUser = iota
	inputSilence
	inputTimeUp
)

// userInput — реплика собеседника (голосом или текстом) или служебный ход (тишина, конец времени).
type userInput struct {
	kind      int
	text      string
	speechEnd time.Time // конец речи по детектору; пусто — реплика набрана текстом
	asrMs     int64
	at        time.Time // когда реплика принята
}

// turnMetrics — разбивка задержки хода: turn.metrics и строка лога, один раз на ход.
type turnMetrics struct {
	turn      int
	typed     bool
	asrMs     int64
	ragMs     int64
	speechEnd time.Time
	at        time.Time
	once      sync.Once
}

// phrase — фраза в очереди синтеза; end — «звук хода закончился».
type phrase struct {
	turn int
	text string
	end  bool
	m    *turnMetrics // у ответа — метрики хода
}

type voiceCall struct {
	svc    *services.CallService
	opts   voiceOpts
	leg    audio.Leg // nil — текстовый звонок
	player *audio.Pacer
	send   func(string, map[string]any)
	hangup func()

	speech *rag.Stream

	inputs  chan userInput
	phrases chan phrase

	mu          sync.Mutex
	turn        int // последний выданный номер хода
	gen         int
	genCtx      context.Context
	genCancel   context.CancelFunc
	audioTurn   int // ход, чей звук сейчас идёт (audio.start отправлен, audio.end — ещё нет)
	playing     int // ход, чей звук собеседник ещё не дослушал (ждём playback.mark)
	clearedTurn int // ходы до этого включительно перебиты — их фразы не озвучиваются
	markedTurns map[int]bool
	inSpeech    bool
	speechEnd   time.Time
	queued      int // фраз в очереди и в работе
	busy        int // ходов в работе (rag думает)

	silTimer *time.Timer
	silGen   int
	silStage int
	ending   bool

	ctx    context.Context
	cancel context.CancelFunc
}

func newVoiceCall(svc *services.CallService, opts voiceOpts, leg audio.Leg, send func(string, map[string]any), hangup func()) *voiceCall {
	return &voiceCall{
		svc: svc, opts: opts, leg: leg, send: send, hangup: hangup,
		inputs:      make(chan userInput, 8),
		phrases:     make(chan phrase, 64),
		markedTurns: map[int]bool{},
	}
}

// start — горутины звонка и приветствие.
func (v *voiceCall) start(parent context.Context) {
	v.ctx, v.cancel = context.WithCancel(parent)
	v.genCtx, v.genCancel = context.WithCancel(v.ctx)
	if v.opts.audio {
		v.player = audio.NewPacer(v.ctx, browserLead, 0)
		v.connectSpeech()
		go v.pump()
	}
	go v.speak()
	go v.turns()
	turn := v.nextTurn()
	if !v.opts.greet {
		// Звонящий говорит первым: приветствие — подсказкой на экране, Корсик сразу слушает.
		v.send("bot.say", map[string]any{"turn": turn, "text": services.VoiceHint, "sources": []services.VoiceSource{}, "silent": true})
		v.armSilence()
		return
	}
	v.send("bot.say", map[string]any{"turn": turn, "text": services.VoiceGreeting, "sources": []services.VoiceSource{}})
	v.say(phrase{turn: turn, text: services.VoiceGreeting})
	v.say(phrase{turn: turn, end: true})
}

func (v *voiceCall) nextTurn() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.turn++
	return v.turn
}

// connectSpeech — сокет распознавания на весь звонок. Без него звонок продолжается текстом.
func (v *voiceCall) connectSpeech() {
	s, err := v.svc.DialSpeech(v.ctx)
	if err != nil {
		logger.Warn("звонок %s: распознавание недоступно: %v", v.opts.callID, err)
		v.send("error", map[string]any{"code": "SPEECH_UNAVAILABLE", "message": "Распознавание речи недоступно — напишите вопрос текстом"})
		return
	}
	v.speech = s
	go v.readSpeech(s)
}

// pump — звук микрофона из ноги в распознавание.
func (v *voiceCall) pump() {
	for {
		select {
		case f := <-v.leg.Frames():
			if v.speech == nil {
				continue
			}
			if v.opts.mute && v.botActive() {
				// CALL_MUTE_WHILE_BOT: пока Корсик думает и говорит, микрофон — тишина, чтобы шум
				// и голос не перебили ответ (запись демо). Тишина, а не пропуск: VAD видит паузу.
				f = make([]byte, len(f))
			}
			if err := v.speech.SendAudio(f); err != nil && v.ctx.Err() == nil {
				logger.Warn("звонок %s: звук в распознавание не отправлен: %v", v.opts.callID, err)
			}
		case <-v.ctx.Done():
			return
		}
	}
}

func (v *voiceCall) readSpeech(s *rag.Stream) {
	for {
		ev, err := s.Next()
		if err != nil {
			if v.ctx.Err() == nil {
				logger.Warn("звонок %s: распознавание отключилось: %v", v.opts.callID, err)
				v.send("error", map[string]any{"code": "SPEECH_UNAVAILABLE", "message": "Распознавание речи отключилось — напишите вопрос текстом"})
			}
			return
		}
		switch ev.Type {
		case rag.SpeechStart:
			v.mu.Lock()
			v.inSpeech = true
			v.mu.Unlock()
			v.disarmSilence(true)
			v.send("user.speech", map[string]any{"state": "start"})
		case rag.SpeechPartial:
			v.send("user.partial", map[string]any{"text": ev.Text})
			if strings.TrimSpace(ev.Text) == "" {
				continue
			}
			v.disarmSilence(true)
			// Перебивание — по словам, а не по самому детектору: кашель не обрывает Корсика.
			if v.opts.bargeIn {
				v.interrupt()
			}
		case rag.SpeechEnd:
			v.mu.Lock()
			v.speechEnd, v.inSpeech = time.Now(), false
			v.mu.Unlock()
			v.send("user.speech", map[string]any{"state": "end"})
			v.armSilence() // шум без финала: тишина снова считается
		case rag.SpeechFinal:
			text := strings.TrimSpace(ev.Text)
			v.mu.Lock()
			end := v.speechEnd
			v.inSpeech = false
			v.mu.Unlock()
			if text == "" {
				continue
			}
			if end.IsZero() {
				end = time.Now()
			}
			v.push(userInput{kind: inputUser, text: text, speechEnd: end, asrMs: ev.AsrMs, at: time.Now()})
		}
	}
}

// botActive — Корсик думает над ходом или говорит.
func (v *voiceCall) botActive() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.queued > 0 || v.busy > 0 || v.playing != 0 || v.audioTurn != 0
}

func (v *voiceCall) push(in userInput) {
	select {
	case v.inputs <- in:
	case <-v.ctx.Done():
	}
}

// typed — реплика текстом: перебивает Корсика, как голос.
func (v *voiceCall) typed(text string) {
	v.disarmSilence(true)
	v.interrupt()
	v.push(userInput{kind: inputUser, text: text, at: time.Now()})
}

// timeUp — CALL_MAX_SECONDS: Корсик прощается и кладёт трубку.
func (v *voiceCall) timeUp() {
	select {
	case v.inputs <- userInput{kind: inputTimeUp}:
	case <-v.ctx.Done():
	default:
		v.hangup() // очередь забита — прощаться некогда
	}
}

// turns — ходы по очереди: реплика → rag → фразы в синтез; тишина и конец времени — прощание.
func (v *voiceCall) turns() {
	for {
		var in userInput
		select {
		case in = <-v.inputs:
		case <-v.ctx.Done():
			return
		}
		switch in.kind {
		case inputUser:
			v.answer(in)
		case inputSilence:
			v.mu.Lock()
			stage := v.silStage
			v.mu.Unlock()
			if stage <= 1 {
				turn := v.nextTurn()
				v.send("bot.say", map[string]any{"turn": turn, "text": services.VoiceSilence, "silence": true, "sources": []services.VoiceSource{}})
				v.say(phrase{turn: turn, text: services.VoiceSilence})
				v.say(phrase{turn: turn, end: true})
				continue
			}
			v.goodbye(services.VoiceBye, "silence", "Собеседник молчит")
			return
		case inputTimeUp:
			v.goodbye(services.VoiceTimeUp, "time_limit", "Время звонка вышло")
			return
		}
	}
}

// answer — реплика собеседника: rag /ask; пока думает дольше fillerAfter — «Секунду, смотрю в
// регламентах»; затем ответ с пунктом или честный отказ.
func (v *voiceCall) answer(in userInput) {
	v.setBusy(1)
	defer v.setBusy(-1)
	turn := v.nextTurn()
	v.send("user.final", map[string]any{"text": in.text})
	m := &turnMetrics{turn: turn, typed: in.speechEnd.IsZero(), asrMs: in.asrMs, speechEnd: in.speechEnd, at: in.at}

	res := make(chan services.VoiceAnswer, 1)
	go func() { res <- v.svc.Answer(v.ctx, in.text, v.opts.site) }()
	timer := time.NewTimer(fillerAfter)
	var a services.VoiceAnswer
	select {
	case a = <-res:
	case <-timer.C:
		v.send("bot.say", map[string]any{"turn": turn, "text": services.VoiceFiller, "filler": true, "sources": []services.VoiceSource{}})
		v.say(phrase{turn: turn, text: services.VoiceFiller})
		a = <-res
	}
	timer.Stop()
	if v.ctx.Err() != nil {
		return
	}
	if a.Err != nil {
		logger.Warn("звонок %s: rag /ask: %v", v.opts.callID, a.Err)
	}
	m.ragMs = a.RagMs
	sources := a.Sources
	if sources == nil {
		sources = []services.VoiceSource{}
	}
	v.send("bot.say", map[string]any{"turn": turn, "text": a.Text, "kind": a.Kind, "sources": sources})
	v.say(phrase{turn: turn, text: a.Text, m: m})
	v.say(phrase{turn: turn, end: true})
}

// goodbye — последняя фраза, дождаться, пока доиграет, call.end и положить трубку.
func (v *voiceCall) goodbye(text, reason, summary string) {
	v.stopSilence()
	turn := v.nextTurn()
	v.send("bot.say", map[string]any{"turn": turn, "text": text, "silence": reason == "silence", "sources": []services.VoiceSource{}})
	v.say(phrase{turn: turn, text: text})
	v.say(phrase{turn: turn, end: true})
	v.waitIdle(byeWait)
	if v.ctx.Err() != nil {
		return
	}
	v.send("call.end", map[string]any{"reason": reason, "summary": summary})
	logger.Info("звонок %s: Корсик положил трубку (%s)", v.opts.callID, reason)
	v.hangup()
}

// metrics — turn.metrics и строка лога. ttsFirst < 0 — звука не было (текстовый звонок, сбой
// синтеза, перебили до первого кадра).
func (v *voiceCall) metrics(m *turnMetrics, ttsFirst time.Duration, firstAudio time.Time) {
	m.once.Do(func() {
		from := m.speechEnd
		if from.IsZero() {
			from = m.at
		}
		if firstAudio.IsZero() {
			firstAudio = time.Now()
		}
		total := max(0, firstAudio.Sub(from).Milliseconds())
		data := map[string]any{"turn": m.turn, "asr_ms": nil, "rag_ms": m.ragMs, "tts_first_ms": nil, "total_ms": total}
		asr, tts := "—", "—"
		if !m.typed {
			data["asr_ms"] = m.asrMs
			asr = strconv.FormatInt(m.asrMs, 10)
		}
		if ttsFirst >= 0 {
			data["tts_first_ms"] = ttsFirst.Milliseconds()
			tts = strconv.FormatInt(ttsFirst.Milliseconds(), 10)
		}
		v.send("turn.metrics", data)
		logger.Info("звонок %s: ход %d asr_ms=%s rag_ms=%d tts_first_ms=%s total_ms=%d", v.opts.callID, m.turn, asr, m.ragMs, tts, total)
	})
}

// interrupt — собеседник заговорил поверх Корсика: звук сбрасывается, недоговорённое не
// озвучивается, синтез отменяется.
func (v *voiceCall) interrupt() {
	v.mu.Lock()
	if v.playing == 0 && v.audioTurn == 0 {
		v.mu.Unlock()
		return
	}
	turn := max(v.playing, v.audioTurn)
	v.clearedTurn = max(v.clearedTurn, turn)
	v.genCancel()
	if v.player != nil {
		v.player.Interrupt()
	}
	v.gen++
	v.genCtx, v.genCancel = context.WithCancel(v.ctx)
	v.audioTurn, v.playing = 0, 0
	gen := v.gen
	v.mu.Unlock()
	v.send("playback.clear", map[string]any{"gen": gen})
}

// marked — браузер доиграл (или сбросил) звук хода: можно считать тишину. Повтор безвреден.
func (v *voiceCall) marked(turn int) {
	v.mu.Lock()
	if v.markedTurns[turn] {
		v.mu.Unlock()
		return
	}
	v.markedTurns[turn] = true
	if v.playing == turn {
		v.playing = 0
	}
	v.mu.Unlock()
	v.armSilence()
}

func (v *voiceCall) say(p phrase) {
	v.mu.Lock()
	v.queued++
	v.mu.Unlock()
	if !p.end {
		v.disarmSilence(false)
	}
	select {
	case v.phrases <- p:
	case <-v.ctx.Done():
	}
}

// setBusy — rag думает над ходом: тишину в это время не считаем.
func (v *voiceCall) setBusy(d int) {
	v.mu.Lock()
	v.busy += d
	v.mu.Unlock()
	if d > 0 {
		v.disarmSilence(false)
	}
}

func (v *voiceCall) idle() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.queued == 0 && v.playing == 0 && v.audioTurn == 0
}

// waitIdle — дать договорить последнюю фразу (до limit).
func (v *voiceCall) waitIdle(limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) && !v.idle() && v.ctx.Err() == nil {
		select {
		case <-time.After(50 * time.Millisecond):
		case <-v.ctx.Done():
			return
		}
	}
}

func (v *voiceCall) stopSilence() {
	v.mu.Lock()
	v.ending = true
	v.mu.Unlock()
	v.disarmSilence(false)
}

// silentNow — Корсик договорил, ход не думается, собеседник молчит. Под v.mu.
func (v *voiceCall) silentNow() bool {
	return v.opts.audio && !v.ending && v.queued == 0 && v.busy == 0 && v.playing == 0 && v.audioTurn == 0 && !v.inSpeech
}

// armSilence — через silenceAfter молчания — служебный ход тишины.
func (v *voiceCall) armSilence() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.silentNow() {
		return
	}
	if v.silTimer != nil {
		v.silTimer.Stop()
	}
	v.silGen++
	gen := v.silGen
	v.silTimer = time.AfterFunc(silenceAfter, func() { v.silenceFired(gen) })
}

func (v *voiceCall) disarmSilence(reset bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.silTimer != nil {
		v.silTimer.Stop()
		v.silTimer = nil
	}
	v.silGen++
	if reset {
		v.silStage = 0
	}
}

func (v *voiceCall) silenceFired(gen int) {
	v.mu.Lock()
	if gen != v.silGen || !v.silentNow() {
		v.mu.Unlock()
		return
	}
	v.silStage++
	v.mu.Unlock()
	select {
	case v.inputs <- userInput{kind: inputSilence}:
	case <-v.ctx.Done():
	default:
	}
}

// speak — диктор: фразы по очереди через синтез, звук — в браузер через Pacer в темпе
// воспроизведения, чтобы перебивание сбрасывало доли секунды, а не всю фразу.
func (v *voiceCall) speak() {
	for {
		var p phrase
		select {
		case p = <-v.phrases:
		case <-v.ctx.Done():
			return
		}
		v.speakOne(p)
		if p.m != nil {
			v.metrics(p.m, -1, time.Time{}) // звука не было — метрики без синтеза
		}
		v.mu.Lock()
		v.queued--
		v.mu.Unlock()
		if p.end {
			v.armSilence() // ход без звука — тишина считается сразу
		}
	}
}

func (v *voiceCall) speakOne(p phrase) {
	if !v.opts.audio {
		return
	}
	v.mu.Lock()
	if p.turn <= v.clearedTurn {
		v.mu.Unlock()
		return
	}
	ctx, gen := v.genCtx, v.gen
	if p.end {
		ended := v.audioTurn == p.turn
		if ended {
			v.audioTurn = 0
		}
		v.mu.Unlock()
		if ended {
			v.send("audio.end", map[string]any{"turn": p.turn})
			// Браузер шлёт playback.mark, когда доиграет; не прислал — считаем по часам отправки.
			time.AfterFunc(time.Until(v.player.EndsAt())+markGrace, func() { v.marked(p.turn) })
		}
		return
	}
	start := v.audioTurn != p.turn
	if start {
		v.audioTurn, v.playing = p.turn, p.turn
	}
	v.mu.Unlock()
	if start {
		v.send("audio.start", map[string]any{"turn": p.turn, "gen": gen})
	}

	requested := time.Now()
	ttsCtx, cancel := context.WithTimeout(ctx, ttsTimeout)
	body, err := v.svc.Synthesize(ttsCtx, p.text)
	if err != nil {
		cancel()
		if ctx.Err() == nil {
			logger.Warn("звонок %s: синтез: %v", v.opts.callID, err)
			v.send("error", map[string]any{"code": "TTS_UNAVAILABLE", "message": "Синтез речи недоступен — ответ только текстом"})
		}
		return
	}
	// Синтез дочитывается своей горутиной: HTTP-поток не ждёт воспроизведения, а звук уходит
	// в темпе по мере готовности.
	chunks := make(chan []byte, 256)
	go func() {
		defer close(chunks)
		defer cancel()
		defer body.Close()
		for {
			b := make([]byte, 4096)
			n, rerr := body.Read(b)
			if n > 0 {
				select {
				case chunks <- b[:n]:
				case <-ttsCtx.Done():
					return
				}
			}
			if rerr != nil {
				if rerr != io.EOF && ctx.Err() == nil {
					logger.Warn("звонок %s: синтез: поток оборван: %v", v.opts.callID, rerr)
				}
				return
			}
		}
	}()
	buf := make([]byte, 0, 8*audio.OutFrameBytes)
	first := true
	play := func(pcm []byte) bool {
		if first {
			first = false
			if p.m != nil {
				v.metrics(p.m, time.Since(requested), v.player.StartsAt())
			}
		}
		pl, err := v.player.Play(ctx, v.leg, pcm)
		return err == nil && pl.Complete
	}
	for c := range chunks {
		buf = append(buf, c...)
		if whole := len(buf) / audio.OutFrameBytes * audio.OutFrameBytes; whole > 0 {
			if !play(buf[:whole]) {
				for range chunks { // дочитать, чтобы горутина синтеза вышла
				}
				return
			}
			buf = append(buf[:0], buf[whole:]...)
		}
	}
	if ctx.Err() == nil && len(buf) > 0 {
		play(buf)
	}
}

// close — звонок кончился: таймеры, контексты, сокет распознавания.
func (v *voiceCall) close() {
	v.stopSilence()
	if v.cancel != nil {
		v.cancel()
	}
	if v.speech != nil {
		_ = v.speech.Close()
	}
}
