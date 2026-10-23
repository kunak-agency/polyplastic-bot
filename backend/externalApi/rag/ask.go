package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AskRequest — тело POST /ask (api_contract.md). Site и AddressForm — nil, если неизвестны:
// в JSON уходят как null. TraceID не задан — берётся из ctx (WithTraceID).
type AskRequest struct {
	Question    string  `json:"question"`
	Site        *string `json:"site"`
	TraceID     string  `json:"trace_id,omitempty"`
	AddressForm *string `json:"address_form"`
}

// Source — найденный пункт документа с дословной цитатой. Цитату rag-сервис уже сверил с
// текстом чанка.
type Source struct {
	Doc      string `json:"doc"`
	ClauseID string `json:"clause_id"`
	Page     *int   `json:"page"`
	Quote    string `json:"quote"`
}

// Значения abstain_reason.
const (
	AbstainNoSource       = "no_source"
	AbstainBelowThreshold = "below_threshold"
	AbstainRoutedTopic    = "routed_topic"
)

// AskResponse — ответ POST /ask. Answer и AbstainReason — nil, если null.
type AskResponse struct {
	Answer          *string  `json:"answer"`
	Sources         []Source `json:"sources"`
	Confidence      string   `json:"confidence"`
	NeedsEscalation bool     `json:"needs_escalation"`
	Topic           string   `json:"topic"`
	AbstainReason   *string  `json:"abstain_reason"`
}

// Reason — abstain_reason или "".
func (r *AskResponse) Reason() string {
	if r.AbstainReason == nil {
		return ""
	}
	return *r.AbstainReason
}

// ErrBadAnswer — rag ответил 200, но нарушил правило контракта: ответ без эскалации обязан
// иметь текст и хотя бы один источник с цитатой. Бот такой ответ не показывает.
var ErrBadAnswer = errors.New("rag POST /ask: ответ без источника")

// maxAskBody — предел тела ответа: цитаты пунктов, а не документы целиком.
const maxAskBody = 1 << 20

// Ask — POST /ask со сроком askTimeout. Любая ошибка (нет адреса, сеть, срок, не 2xx, мусор в
// теле, ответ без источника) — повод для честного «не могу проверить по документам».
func (c *Client) Ask(ctx context.Context, req AskRequest) (*AskResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	return c.AskWithin(ctx, req, c.askTimeout)
}

// AskTimeout — срок POST /ask (RAG_ASK_TIMEOUT).
func (c *Client) AskTimeout() time.Duration { return c.askTimeout }

// AskWithin — POST /ask со сроком d: бот ждёт дольше RAG_ASK_TIMEOUT, чтобы дослать поздний ответ.
func (c *Client) AskWithin(ctx context.Context, req AskRequest, d time.Duration) (*AskResponse, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	if req.TraceID == "" {
		req.TraceID = TraceID(ctx)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if d <= 0 {
		d = c.askTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ask", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if req.TraceID != "" {
		hr.Header.Set("X-Trace-Id", req.TraceID)
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &Error{Status: resp.StatusCode, Op: "POST /ask"}
	}
	var out AskResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAskBody)).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag POST /ask: тело не разобрано: %w", err)
	}
	if !out.NeedsEscalation && !out.hasSourcedAnswer() {
		return nil, ErrBadAnswer
	}
	if out.NeedsEscalation {
		out.Answer = nil // контракт: при эскалации answer=null; не показываем, даже если пришёл
	}
	return &out, nil
}

func (r *AskResponse) hasSourcedAnswer() bool {
	if r.Answer == nil || *r.Answer == "" {
		return false
	}
	for _, s := range r.Sources {
		if s.Doc != "" && s.ClauseID != "" && s.Quote != "" {
			return true
		}
	}
	return false
}

type traceKey struct{}

// WithTraceID — номер трассировки для запросов в rag из этого ctx.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceKey{}, id)
}

// TraceID — номер из WithTraceID или "".
func TraceID(ctx context.Context) string {
	s, _ := ctx.Value(traceKey{}).(string)
	return s
}
