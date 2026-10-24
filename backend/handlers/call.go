package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"main/pkg/audio"
	"main/pkg/logger"
	"main/services"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/oklog/ulid/v2"
)

// Звонок Корсику в браузере: WebSocket /api/call (agency/system/api_contract.md, «Звонок
// /api/call»). Порт браузерной части hack-ru-clinik (handlers/call.go, wsLeg) без телефонии,
// DTMF и диалогового движка: одна реплика — один вызов rag /ask.

// SetOrigins — источники, которым можно открыть WebSocket звонка (ORIGINS из конфига).
func (h *Handler) SetOrigins(origins []string) {
	h.origins = map[string]bool{}
	for _, o := range origins {
		h.origins[strings.TrimRight(strings.ToLower(strings.TrimSpace(o)), "/")] = true
	}
}

// CallUpgrade — перед /api/call: флаг FEATURE_CALL (выключен — 404, как будто маршрута нет),
// только апгрейд до WebSocket и только со своего источника. CORS WebSocket не защищает: браузер
// откроет сокет с любой страницы и приложит к нему сохранённый basic auth Caddy, поэтому Origin
// проверяет сервер.
func (h *Handler) CallUpgrade(c *fiber.Ctx) error {
	if !h.services.Call.Enabled() {
		return fiber.ErrNotFound
	}
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}
	if !h.originAllowed(c.Get(fiber.HeaderOrigin), c.Hostname()) {
		logger.Warn("звонок: чужой Origin %q отклонён", c.Get(fiber.HeaderOrigin))
		return fiber.NewError(fiber.StatusForbidden, "Звонок открывается только со страницы звонка")
	}
	return c.Next()
}

// originAllowed — пустой Origin (не браузер: тесты, консольный клиент), источник из ORIGINS или
// тот же хост, что в запросе (страница и API за одним Caddy).
func (h *Handler) originAllowed(origin, host string) bool {
	if origin == "" {
		return true
	}
	o := strings.TrimRight(strings.ToLower(origin), "/")
	if h.origins[o] {
		return true
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

type callClientMsg struct {
	Type     string `json:"type"`
	Input    string `json:"input"`
	Site     string `json:"site"`
	Text     string `json:"text"`
	Turn     int    `json:"turn"`
	Complete bool   `json:"complete"`
}

// wsLeg — нога браузерного звонка: звук микрофона из бинарных сообщений, звук бота — в тот же
// сокет через единственного писателя Call.
type wsLeg struct {
	id      string
	frames  chan []byte
	buf     []byte
	sendBin func([]byte) bool
	done    chan struct{}
	once    sync.Once
}

func newWSLeg(id string, sendBin func([]byte) bool) *wsLeg {
	return &wsLeg{id: id, frames: make(chan []byte, 256), sendBin: sendBin, done: make(chan struct{})}
}

func (l *wsLeg) Frames() <-chan []byte { return l.frames }
func (l *wsLeg) Done() <-chan struct{} { return l.done }

// push — бинарное сообщение браузера: кадры любой длины, кратной 2 байтам, склеиваются в 320.
func (l *wsLeg) push(b []byte) {
	if len(b) == 0 || len(b)%2 != 0 {
		return
	}
	l.buf = append(l.buf, b...)
	for len(l.buf) >= audio.FrameBytes {
		f := append([]byte(nil), l.buf[:audio.FrameBytes]...)
		l.buf = l.buf[audio.FrameBytes:]
		select {
		case l.frames <- f:
		default:
			logger.Warn("звонок %s: распознавание не успевает, кадр микрофона потерян", l.id)
		}
	}
}

func (l *wsLeg) Send(frame []byte) error {
	if !l.sendBin(frame) {
		return errors.New("сокет браузера закрыт")
	}
	return nil
}

func (l *wsLeg) closed() { l.once.Do(func() { close(l.done) }) }

// callGrace — сколько сверх CALL_MAX_SECONDS ждать прощания, прежде чем закрыть сокет силой.
const callGrace = 20 * time.Second

// Call — один звонок на одно соединение. Пишет в сокет ровно одна горутина: соединение
// fasthttp/websocket не допускает конкурентной записи, а события летят из хода, диктора и
// распознавания.
func (h *Handler) Call(conn *websocket.Conn) {
	svc := h.services.Call
	release, ok := svc.Acquire()
	if !ok {
		b, _ := json.Marshal(map[string]any{"type": "error", "code": "busy", "message": "Линия занята, позвоните чуть позже"})
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_ = conn.WriteMessage(websocket.TextMessage, b)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "busy"))
		logger.Info("звонок: линия занята (CALL_MAX_CONCURRENT=%d)", svc.Config().MaxConcurrent)
		return
	}
	defer release()

	type outMsg struct {
		kind int
		b    []byte
	}
	out := make(chan outMsg, 512)
	done := make(chan struct{})
	var once sync.Once
	closeAll := func() { once.Do(func() { close(done) }) }

	// ws — соединение под обёрткой: обёртку fiber возвращает в пул, как только Call вернулся,
	// а таймер предела живёт в своей горутине.
	ws := conn.Conn
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		defer closeAll()
		for {
			select {
			case <-done:
				return
			case m := <-out:
				_ = ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if m.kind == websocket.CloseMessage {
					// Сервер кладёт трубку: всё, что было в очереди, уже ушло; кадр закрытия —
					// и соединение, чтобы читатель ниже вышел, не дожидаясь браузера.
					_ = ws.WriteMessage(websocket.CloseMessage, m.b)
					_ = ws.Close()
					return
				}
				if err := ws.WriteMessage(m.kind, m.b); err != nil {
					return
				}
			}
		}
	}()
	send := func(typ string, data map[string]any) {
		if data == nil {
			data = map[string]any{}
		}
		data["type"] = typ
		b, _ := json.Marshal(data)
		select {
		case out <- outMsg{websocket.TextMessage, b}:
		case <-done:
		default:
			logger.Warn("звонок: очередь отправки переполнена, событие %s потеряно", typ)
		}
	}
	// Звук бота не теряется, а ждёт очереди: Pacer и так отдаёт его в темпе воспроизведения.
	sendBin := func(b []byte) bool {
		select {
		case out <- outMsg{websocket.BinaryMessage, b}:
			return true
		case <-done:
			return false
		}
	}
	hangup := func() {
		select {
		case out <- outMsg{websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")}:
		case <-done:
		}
	}

	var (
		v      *voiceCall
		cur    atomic.Pointer[voiceCall] // v для таймера предела: он в своей горутине
		leg    *wsLeg
		callID string
	)
	started := time.Now()
	limit := svc.Config().MaxDuration
	// Предел длины: Корсик прощается сам; не вышло (звонок не начат, ход завис) — сокет закрывается.
	timeUp := time.AfterFunc(limit, func() {
		if cv := cur.Load(); cv != nil {
			cv.timeUp()
		} else {
			hangup()
		}
	})
	hard := time.AfterFunc(limit+callGrace, func() { _ = ws.Close() })
	defer func() {
		timeUp.Stop()
		hard.Stop()
		if leg != nil {
			leg.closed()
		}
		if v != nil {
			v.close()
			logger.Info("звонок %s завершён за %v", callID, time.Since(started).Round(time.Second))
		}
		closeAll()
		<-writerDone // писатель не трогает соединение после возврата
	}()

	for {
		kind, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if kind == websocket.BinaryMessage {
			if leg != nil {
				leg.push(raw)
			}
			continue
		}
		var m callClientMsg
		if json.Unmarshal(raw, &m) != nil {
			send("error", map[string]any{"code": "BAD_MESSAGE", "message": "сообщение не разбирается как JSON"})
			continue
		}
		switch m.Type {
		case "start":
			if v != nil {
				continue
			}
			callID = ulid.Make().String()
			input := "voice"
			if m.Input == "text" {
				input = "text"
			}
			site := services.CallSite(m.Site)
			opts := voiceOpts{audio: input == "voice", bargeIn: svc.Config().BargeIn, site: site, callID: callID}
			var l audio.Leg
			if opts.audio {
				leg = newWSLeg(callID, sendBin)
				l = leg
			}
			v = newVoiceCall(svc, opts, l, send, hangup)
			send("call.started", map[string]any{"callId": callID, "input": input, "site": site, "maxSeconds": int(limit / time.Second)})
			logger.Info("звонок %s начат: %s, площадка %q", callID, input, site)
			v.start(context.Background())
			cur.Store(v)
		case "user.text":
			if v == nil || strings.TrimSpace(m.Text) == "" {
				continue
			}
			v.typed(strings.TrimSpace(m.Text))
		case "playback.mark":
			if v != nil {
				v.marked(m.Turn)
			}
		case "hangup":
			return
		default:
			send("error", map[string]any{"code": "BAD_MESSAGE", "message": "неизвестный тип " + m.Type})
		}
	}
}
