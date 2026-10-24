package handlers

import (
	"encoding/json"
	"main/pkg/audio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"main/config"
	"main/externalApi/rag"
	"main/services"

	fws "github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
)

// Звонок /api/call против фейкового rag-пода: WS /stream (события распознавания шлёт тест),
// POST /tts (тишина PCM нужной длины), POST /ask (ответ, отказ, медленный ответ). Без сети и
// без Postgres.

const testKey = "test-key"

type fakePod struct {
	t   *testing.T
	srv *httptest.Server

	mu      sync.Mutex
	asks    []rag.AskRequest
	ttsText []string
	audio   int // байт звука микрофона пришло в /stream
	conns   []*fws.Conn
	closed  int // сокетов /stream закрыто клиентом
	ttsMs   int // длина звука одной фразы
	authBad int
	connCh  chan *fws.Conn
}

func newFakePod(t *testing.T) *fakePod {
	p := &fakePod{t: t, ttsMs: 200, connCh: make(chan *fws.Conn, 4)}
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", p.stream)
	mux.HandleFunc("/tts", p.tts)
	mux.HandleFunc("/ask", p.ask)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakePod) authorized(r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		p.mu.Lock()
		p.authBad++
		p.mu.Unlock()
		return false
	}
	return true
}

func (p *fakePod) stream(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	up := fws.Upgrader{}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.conns = append(p.conns, c)
	p.mu.Unlock()
	p.connCh <- c
	go func() {
		for {
			kind, b, err := c.ReadMessage()
			if err != nil {
				p.mu.Lock()
				p.closed++
				p.mu.Unlock()
				return
			}
			if kind == fws.BinaryMessage {
				p.mu.Lock()
				p.audio += len(b)
				p.mu.Unlock()
			}
		}
	}()
}

// emit — событие распознавания в последний открытый /stream.
func (p *fakePod) emit(ev map[string]any) {
	p.mu.Lock()
	c := p.conns[len(p.conns)-1]
	p.mu.Unlock()
	if err := c.WriteJSON(ev); err != nil {
		p.t.Fatalf("pod emit: %v", err)
	}
}

func (p *fakePod) tts(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	var body struct{ Text string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	p.mu.Lock()
	p.ttsText = append(p.ttsText, body.Text)
	ms := p.ttsMs
	p.mu.Unlock()
	w.Header().Set("Content-Type", rag.TTSFormat)
	n := ms * 48 // 24000 Гц × 2 байта = 48 байт на мс
	chunk := make([]byte, 4800)
	for sent := 0; sent < n; sent += len(chunk) {
		if _, err := w.Write(chunk[:min(len(chunk), n-sent)]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
	}
}

func (p *fakePod) ask(w http.ResponseWriter, r *http.Request) {
	if !p.authorized(r) {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	var req rag.AskRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	p.mu.Lock()
	p.asks = append(p.asks, req)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.Contains(req.Question, "отказ"):
		_, _ = w.Write([]byte(`{"answer":null,"sources":[],"confidence":"low","needs_escalation":true,"topic":"other","abstain_reason":"no_source"}`))
		return
	case strings.Contains(req.Question, "сломай"):
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	case strings.Contains(req.Question, "медленно"):
		time.Sleep(1200 * time.Millisecond)
	}
	_, _ = w.Write([]byte(`{"answer":"Аванс выплачивается 25 числа","sources":[{"doc":"ПВТР П 3.11-07","clause_id":"6.7.4","page":12,"quote":"Аванс — 25 числа."}],"confidence":"high","needs_escalation":false,"topic":"pay","abstain_reason":null}`))
}

func (p *fakePod) snapshot() (asks []rag.AskRequest, tts []string, audioBytes, closed, authBad int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]rag.AskRequest(nil), p.asks...), append([]string(nil), p.ttsText...), p.audio, p.closed, p.authBad
}

// callServer — API с одним маршрутом /api/call на настоящем порту (WebSocket через app.Test не
// пройти).
func callServer(t *testing.T, pod *fakePod, enabled bool, cfg config.Call) string {
	t.Helper()
	client := rag.New(pod.srv.URL, 5*time.Second, rag.WithAPIKey(testKey))
	svc := &services.Services{Call: services.NewCallService(client, cfg, enabled)}
	h := NewHandlerManager(svc)
	h.SetOrigins([]string{"https://call.example.ru"})
	app := fiber.New(ServerConfig(DefaultTimeouts))
	RegisterRoutes(app, h)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(time.Second) })
	return "ws://" + ln.Addr().String() + "/api/call"
}

// testCall — браузер: события копятся, на audio.end сам шлёт playback.mark (если autoMark).
type testCall struct {
	t        *testing.T
	conn     *fws.Conn
	wmu      sync.Mutex
	events   chan map[string]any
	mu       sync.Mutex
	binary   int
	closed   chan struct{}
	autoMark bool
}

func dialCall(t *testing.T, url, origin string) (*testCall, *http.Response, error) {
	t.Helper()
	hdr := http.Header{}
	if origin != "" {
		hdr.Set("Origin", origin)
	}
	conn, resp, err := fws.DefaultDialer.Dial(url, hdr)
	if err != nil {
		return nil, resp, err
	}
	c := &testCall{t: t, conn: conn, events: make(chan map[string]any, 1024), closed: make(chan struct{}), autoMark: true}
	t.Cleanup(func() { _ = conn.Close() })
	go c.read()
	return c, resp, nil
}

func (c *testCall) read() {
	defer close(c.closed)
	for {
		kind, b, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if kind == fws.BinaryMessage {
			c.mu.Lock()
			c.binary += len(b)
			c.mu.Unlock()
			continue
		}
		var ev map[string]any
		if json.Unmarshal(b, &ev) != nil {
			continue
		}
		if ev["type"] == "audio.end" && c.autoMark {
			c.send(map[string]any{"type": "playback.mark", "turn": ev["turn"], "complete": true})
		}
		c.events <- ev
	}
}

func (c *testCall) send(m map[string]any) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.WriteJSON(m)
}

func (c *testCall) sendAudio(n int) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.conn.WriteMessage(fws.BinaryMessage, make([]byte, n))
}

func (c *testCall) binaryBytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.binary
}

// waitFor — первое событие типа typ, подходящее под match (nil — любое); прочие пропускаются.
func (c *testCall) waitFor(typ string, match func(map[string]any) bool) map[string]any {
	c.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-c.events:
			if ev["type"] == typ && (match == nil || match(ev)) {
				return ev
			}
		case <-deadline:
			c.t.Fatalf("не дождались %s", typ)
			return nil
		}
	}
}

// none — за d не пришло событие typ.
func (c *testCall) none(typ string, d time.Duration) {
	c.t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-c.events:
			if ev["type"] == typ {
				c.t.Fatalf("неожиданное %s: %v", typ, ev)
			}
		case <-deadline:
			return
		}
	}
}

func turnIs(n int) func(map[string]any) bool {
	return func(ev map[string]any) bool { return ev["turn"] == float64(n) }
}

func textIs(s string) func(map[string]any) bool {
	return func(ev map[string]any) bool { return ev["text"] == s }
}

var defaultCallCfg = config.Call{MaxConcurrent: 2, MaxDuration: time.Minute, BargeIn: true, SpokenGreeting: true}

// startCall — звонок со стороны браузера: start и приветствие доиграно.
func startCall(t *testing.T, url string, input string) *testCall {
	t.Helper()
	c, _, err := dialCall(t, url, "https://call.example.ru")
	if err != nil {
		t.Fatal(err)
	}
	c.send(map[string]any{"type": "start", "input": input, "site": "grpp"})
	st := c.waitFor("call.started", nil)
	if st["input"] != input || st["site"] != "GrPP" || st["callId"] == "" {
		t.Fatalf("call.started %v", st)
	}
	c.waitFor("bot.say", textIs(services.VoiceGreeting))
	return c
}

func TestCallGreeting(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	st := c.waitFor("audio.start", turnIs(1))
	if st["gen"] != float64(0) {
		t.Fatalf("audio.start %v", st)
	}
	c.waitFor("audio.end", turnIs(1))
	if got := c.binaryBytes(); got < 200*48 {
		t.Fatalf("звука приветствия %d байт", got)
	}
	if got := c.binaryBytes(); got%audio.OutFrameBytes != 0 {
		t.Fatalf("звук не кадрами по %d байт: %d", audio.OutFrameBytes, got)
	}
	_, tts, _, _, bad := pod.snapshot()
	if len(tts) != 1 || tts[0] != services.VoiceGreeting || bad != 0 {
		t.Fatalf("tts %v, без ключа %d", tts, bad)
	}
	// Звук микрофона уходит в /stream кадрами.
	c.sendAudio(640)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, _, a, _, _ := pod.snapshot(); a == 640 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("звук микрофона не дошёл до /stream")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// hangup: сокет пода закрыт.
	c.send(map[string]any{"type": "hangup"})
	<-c.closed
	deadline = time.Now().Add(2 * time.Second)
	for {
		if _, _, _, closed, _ := pod.snapshot(); closed == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("WS /stream пода не закрыт после hangup")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Реплика голосом, rag медленный: филлер, ответ с пунктом, звук, метрики.
func TestCallVoiceTurnWithFiller(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	c.waitFor("audio.end", turnIs(1))

	pod.emit(map[string]any{"type": "speech_start"})
	c.waitFor("user.speech", func(ev map[string]any) bool { return ev["state"] == "start" })
	pod.emit(map[string]any{"type": "partial", "text": "медленно когда"})
	c.waitFor("user.partial", textIs("медленно когда"))
	pod.emit(map[string]any{"type": "speech_end"})
	c.waitFor("user.speech", func(ev map[string]any) bool { return ev["state"] == "end" })
	pod.emit(map[string]any{"type": "final", "text": "медленно когда аванс", "asr_ms": 140})
	c.waitFor("user.final", textIs("медленно когда аванс"))

	filler := c.waitFor("bot.say", turnIs(2))
	if filler["filler"] != true || filler["text"] != services.VoiceFiller {
		t.Fatalf("филлер %v", filler)
	}
	ans := c.waitFor("bot.say", func(ev map[string]any) bool { return ev["turn"] == float64(2) && ev["filler"] == nil })
	want := "Аванс выплачивается 25 числа. Это пункт 6.7.4 правил внутреннего трудового распорядка."
	if ans["text"] != want || ans["kind"] != "answer" {
		t.Fatalf("ответ %v", ans)
	}
	srcs, _ := ans["sources"].([]any)
	if len(srcs) != 1 {
		t.Fatalf("sources %v", ans["sources"])
	}
	s := srcs[0].(map[string]any)
	if s["label"] != "ПВТР 6.7.4" || s["doc"] != "ПВТР П 3.11-07" || s["clause_id"] != "6.7.4" || s["page"] != float64(12) {
		t.Fatalf("источник %v", s)
	}
	m := c.waitFor("turn.metrics", turnIs(2))
	if m["asr_ms"] != float64(140) || m["rag_ms"].(float64) < 1000 || m["tts_first_ms"] == nil || m["total_ms"].(float64) < 1000 {
		t.Fatalf("метрики %v", m)
	}
	c.waitFor("audio.end", turnIs(2))

	asks, tts, _, _, bad := pod.snapshot()
	if len(asks) != 1 || asks[0].Question != "медленно когда аванс" || asks[0].Site == nil || *asks[0].Site != "GrPP" ||
		asks[0].AddressForm == nil || *asks[0].AddressForm != "vy" {
		t.Fatalf("/ask %+v", asks)
	}
	if len(tts) != 3 || tts[1] != services.VoiceFiller || tts[2] != want || bad != 0 {
		t.Fatalf("tts %v, без ключа %d", tts, bad)
	}
}

// Отказ rag: честное «в регламентах нет», без источников; ошибка rag — просьба переспросить.
func TestCallAbstainAndError(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	c.waitFor("audio.end", turnIs(1))

	c.send(map[string]any{"type": "user.text", "text": "отказ: сколько стоит обед"})
	ans := c.waitFor("bot.say", turnIs(2))
	if ans["text"] != services.VoiceAbstain || ans["kind"] != "abstain" {
		t.Fatalf("отказ %v", ans)
	}
	if srcs, ok := ans["sources"].([]any); !ok || len(srcs) != 0 {
		t.Fatalf("sources отказа %v", ans["sources"])
	}
	c.waitFor("audio.end", turnIs(2))

	c.send(map[string]any{"type": "user.text", "text": "сломай"})
	ans = c.waitFor("bot.say", turnIs(3))
	if ans["text"] != services.VoiceError || ans["kind"] != "error" {
		t.Fatalf("ошибка rag %v", ans)
	}
}

// user.text: эхо user.final и ответ без филлера; метрики без asr. Текстовый звонок — без звука.
func TestCallUserText(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "text")
	c.send(map[string]any{"type": "user.text", "text": "  когда аванс  "})
	c.waitFor("user.final", textIs("когда аванс"))
	ans := c.waitFor("bot.say", turnIs(2))
	if ans["filler"] != nil || !strings.HasPrefix(ans["text"].(string), "Аванс выплачивается") {
		t.Fatalf("ответ %v", ans)
	}
	m := c.waitFor("turn.metrics", turnIs(2))
	if m["asr_ms"] != nil || m["tts_first_ms"] != nil {
		t.Fatalf("метрики текста %v", m)
	}
	c.none("audio.start", 200*time.Millisecond)
	_, tts, _, _, _ := pod.snapshot()
	if c.binaryBytes() != 0 || len(tts) != 0 {
		t.Fatalf("в текстовом звонке звук: %d байт, tts %v", c.binaryBytes(), tts)
	}
}

// Перебивание: первый непустой partial во время речи — playback.clear, недоговорённое не звучит.
func TestCallBargeIn(t *testing.T) {
	pod := newFakePod(t)
	pod.ttsMs = 3000
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	c.waitFor("audio.start", turnIs(1))
	pod.emit(map[string]any{"type": "partial", "text": ""}) // пустой — не перебивание
	c.waitFor("user.partial", textIs(""))
	pod.emit(map[string]any{"type": "partial", "text": "подождите"})
	clr := c.waitFor("playback.clear", nil)
	if clr["gen"] != float64(1) {
		t.Fatalf("playback.clear %v", clr)
	}
	time.Sleep(100 * time.Millisecond) // кадры, уже стоявшие в очереди писателя
	got := c.binaryBytes()
	c.none("audio.end", 500*time.Millisecond)
	if after := c.binaryBytes(); after != got {
		t.Fatalf("звук после перебивания: %d → %d байт", got, after)
	}
	if got >= 3000*16 {
		t.Fatalf("приветствие доиграло целиком (%d байт)", got)
	}
	// Следующий ход звучит с новым поколением.
	c.send(map[string]any{"type": "user.text", "text": "когда аванс"})
	st := c.waitFor("audio.start", turnIs(2))
	if st["gen"] != float64(1) {
		t.Fatalf("audio.start после перебивания %v", st)
	}
}

func TestCallBargeInOff(t *testing.T) {
	pod := newFakePod(t)
	pod.ttsMs = 1500
	cfg := defaultCallCfg
	cfg.BargeIn = false
	url := callServer(t, pod, true, cfg)
	c := startCall(t, url, "voice")
	c.waitFor("audio.start", turnIs(1))
	pod.emit(map[string]any{"type": "partial", "text": "подождите"})
	c.waitFor("user.partial", nil)
	c.none("playback.clear", 300*time.Millisecond)
	c.waitFor("audio.end", turnIs(1))
}

// Лимит одновременных звонков: лишний — error{code:"busy"} и закрытие.
func TestCallBusy(t *testing.T) {
	pod := newFakePod(t)
	cfg := defaultCallCfg
	cfg.MaxConcurrent = 1
	url := callServer(t, pod, true, cfg)
	first := startCall(t, url, "voice")

	second, _, err := dialCall(t, url, "")
	if err != nil {
		t.Fatal(err)
	}
	ev := second.waitFor("error", nil)
	if ev["code"] != "busy" {
		t.Fatalf("занято: %v", ev)
	}
	select {
	case <-second.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("лишний звонок не закрыт")
	}

	// Первый положил трубку — линия свободна.
	first.send(map[string]any{"type": "hangup"})
	<-first.closed
	deadline := time.Now().Add(2 * time.Second)
	for {
		third, _, err := dialCall(t, url, "")
		if err != nil {
			t.Fatal(err)
		}
		third.send(map[string]any{"type": "start", "input": "text"})
		select {
		case ev := <-third.events:
			if ev["type"] == "call.started" {
				return
			}
		case <-time.After(2 * time.Second):
		}
		if time.Now().After(deadline) {
			t.Fatal("линия не освободилась после hangup")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Тишина: «Вы меня слышите?», затем прощание, call.end и закрытие сокета.
func TestCallSilenceEnds(t *testing.T) {
	old := silenceAfter
	silenceAfter = 150 * time.Millisecond
	t.Cleanup(func() { silenceAfter = old })
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	q := c.waitFor("bot.say", textIs(services.VoiceSilence))
	if q["silence"] != true || q["turn"] != float64(2) {
		t.Fatalf("тишина 1 %v", q)
	}
	bye := c.waitFor("bot.say", textIs(services.VoiceBye))
	if bye["turn"] != float64(3) {
		t.Fatalf("прощание %v", bye)
	}
	c.waitFor("audio.end", turnIs(3))
	end := c.waitFor("call.end", nil)
	if end["reason"] != "silence" {
		t.Fatalf("call.end %v", end)
	}
	select {
	case <-c.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("сокет не закрыт после call.end")
	}
}

// Речь собеседника сбрасывает отсчёт тишины.
func TestCallSilenceResetBySpeech(t *testing.T) {
	old := silenceAfter
	silenceAfter = 300 * time.Millisecond
	t.Cleanup(func() { silenceAfter = old })
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	c := startCall(t, url, "voice")
	c.waitFor("audio.end", turnIs(1))
	time.Sleep(150 * time.Millisecond)
	pod.emit(map[string]any{"type": "speech_start"})
	c.none("bot.say", 500*time.Millisecond)
}

// Предел длины звонка: прощание и call.end{reason: time_limit}.
func TestCallTimeLimit(t *testing.T) {
	pod := newFakePod(t)
	cfg := defaultCallCfg
	cfg.MaxDuration = 600 * time.Millisecond
	url := callServer(t, pod, true, cfg)
	c := startCall(t, url, "voice")
	c.waitFor("bot.say", textIs(services.VoiceTimeUp))
	end := c.waitFor("call.end", nil)
	if end["reason"] != "time_limit" {
		t.Fatalf("call.end %v", end)
	}
	<-c.closed
}

func TestCallFeatureOff(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, false, defaultCallCfg)
	_, resp, err := dialCall(t, url, "")
	if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("FEATURE_CALL выключен: err %v, resp %v", err, resp)
	}
}

func TestCallForeignOrigin(t *testing.T) {
	pod := newFakePod(t)
	url := callServer(t, pod, true, defaultCallCfg)
	_, resp, err := dialCall(t, url, "https://evil.example.com")
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("чужой Origin: err %v, resp %v", err, resp)
	}
	// Источник из ORIGINS пускается.
	if _, _, err := dialCall(t, url, "https://call.example.ru"); err != nil {
		t.Fatal(err)
	}
}

// Обычный GET без апгрейда — 426, а не паника в websocket.New.
func TestCallNotUpgrade(t *testing.T) {
	svc := &services.Services{Call: services.NewCallService(nil, defaultCallCfg, true)}
	app := fiber.New(ServerConfig(DefaultTimeouts))
	RegisterRoutes(app, NewHandlerManager(svc))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/call", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("статус %d", resp.StatusCode)
	}
}

func TestCallNoSpokenGreeting(t *testing.T) {
	pod := newFakePod(t)
	cfg := defaultCallCfg
	cfg.SpokenGreeting = false
	url := callServer(t, pod, true, cfg)
	c, _, err := dialCall(t, url, "https://call.example.ru")
	if err != nil {
		t.Fatal(err)
	}
	c.send(map[string]any{"type": "start", "input": "voice", "site": "grpp"})
	hint := c.waitFor("bot.say", turnIs(1))
	if hint["silent"] != true || hint["text"] != services.VoiceHint {
		t.Fatalf("подсказка вместо приветствия %v", hint)
	}
	c.none("audio.start", 400*time.Millisecond)
	c.send(map[string]any{"type": "user.text", "text": "когда аванс"})
	c.waitFor("audio.start", turnIs(2))
}
