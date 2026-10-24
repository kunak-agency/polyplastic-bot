package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	fws "github.com/fasthttp/websocket"
)

// Потоковое распознавание речи звонка: WS /stream rag-сервиса (services/rag/stream.py, порт
// speech.go из hack-ru-clinik). Вход — бинарные кадры PCM16LE 8 кГц; управляющие сообщения —
// {"type":"config","silence_ms":N} и {"type":"flush"}. Выход — SpeechEvent.

// Типы событий распознавания.
const (
	SpeechStart   = "speech_start"
	SpeechPartial = "partial"
	SpeechEnd     = "speech_end"
	SpeechFinal   = "final"
)

// SpeechEvent — событие WS /stream: speech_start, partial{text}, speech_end, final{text, asr_ms}.
type SpeechEvent struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	AsrMs int64  `json:"asr_ms"`
}

// Stream — сокет распознавания одного звонка. Писать можно из разных горутин (звук и flush),
// читать — из одной (Next).
type Stream struct {
	conn *fws.Conn
	mu   sync.Mutex
}

// streamURL — RAG_BASE_URL со схемой ws(s) и путём /stream.
func streamURL(base string) (string, error) {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/stream", nil
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/stream", nil
	}
	return "", fmt.Errorf("rag: адрес %q без схемы http(s)", base)
}

// DialStream — сокет распознавания на весь звонок; ключ — заголовком Bearer, как у HTTP.
func (c *Client) DialStream(ctx context.Context) (*Stream, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	u, err := streamURL(c.baseURL)
	if err != nil {
		return nil, err
	}
	hdr := http.Header{}
	if c.apiKey != "" {
		hdr.Set("Authorization", "Bearer "+c.apiKey)
	}
	d := fws.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := d.DialContext(ctx, u, hdr)
	if err != nil {
		if resp != nil {
			return nil, &Error{Status: resp.StatusCode, Op: "WS /stream"}
		}
		return nil, fmt.Errorf("rag WS /stream: %w", err)
	}
	return &Stream{conn: conn}, nil
}

func (s *Stream) write(kind int, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return s.conn.WriteMessage(kind, b)
}

// SendAudio — кадр звука собеседника (PCM16LE 8 кГц).
func (s *Stream) SendAudio(pcm []byte) error { return s.write(fws.BinaryMessage, pcm) }

// Configure — пауза конца реплики, мс (0 — по умолчанию сервиса).
func (s *Stream) Configure(silenceMs int) error {
	return s.write(fws.TextMessage, []byte(fmt.Sprintf(`{"type":"config","silence_ms":%d}`, silenceMs)))
}

// Flush — закрыть начатую реплику немедленно.
func (s *Stream) Flush() error { return s.write(fws.TextMessage, []byte(`{"type":"flush"}`)) }

// Next — следующее событие; мусор пропускается. Ошибка — сокет закрыт.
func (s *Stream) Next() (SpeechEvent, error) {
	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			return SpeechEvent{}, err
		}
		var ev SpeechEvent
		if json.Unmarshal(raw, &ev) == nil && ev.Type != "" {
			return ev, nil
		}
	}
}

// Close — закрыть сокет (вежливо: кадр закрытия, затем соединение).
func (s *Stream) Close() error {
	s.mu.Lock()
	_ = s.conn.WriteControl(fws.CloseMessage, fws.FormatCloseMessage(fws.CloseNormalClosure, ""), time.Now().Add(time.Second))
	s.mu.Unlock()
	return s.conn.Close()
}
