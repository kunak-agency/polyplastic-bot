// Package rag — клиент Python-сервиса services/rag (agency/system/api_contract.md): GET /health
// и POST /ask (ask.go), POST /flags (flags.go).
package rag

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client — клиент rag-сервиса. Пустой адрес — клиент есть, но каждый вызов отказывает: сервис
// обязан подниматься без rag.
type Client struct {
	baseURL    string
	http       *http.Client
	askTimeout time.Duration
}

// DefaultAskTimeout — срок POST /ask, если RAG_ASK_TIMEOUT не задан.
const DefaultAskTimeout = 12 * time.Second

// New — клиент. Срок health передаёт вызывающий, срок /ask — askTimeout (≤ 0 — по умолчанию).
func New(baseURL string, askTimeout time.Duration) *Client {
	if askTimeout <= 0 {
		askTimeout = DefaultAskTimeout
	}
	return &Client{baseURL: baseURL, http: &http.Client{}, askTimeout: askTimeout}
}

// Error — rag ответил не 2xx. Реализует errs.External: статус нашего ответа выбирает ErrorHandler.
type Error struct {
	Status int
	Op     string
}

func (e *Error) Error() string { return fmt.Sprintf("rag %s: статус %d", e.Op, e.Status) }

// ExternalStatus — сбой rag для клиента выглядит как 502: ответил, но не по договорённости.
func (e *Error) ExternalStatus() int { return http.StatusBadGateway }

// Health — GET /health отвечает 2xx. Тело не разбираем: живость сервиса — это статус.
func (c *Client) Health(ctx context.Context, timeout time.Duration) error {
	if c.baseURL == "" {
		return fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Op: "GET /health"}
	}
	return nil
}
