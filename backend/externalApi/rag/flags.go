package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// FlagsRequest — тело POST /flags (api_contract.md). Context — pulse_dN | free_text | voice.
// SurveyQuestion — вопрос опроса, к которому относится ответ; nil — свободный текст.
type FlagsRequest struct {
	Text           string  `json:"text"`
	Context        string  `json:"context"`
	SurveyQuestion *string `json:"survey_question,omitempty"`
}

// Значения FlagsRequest.Context.
const (
	FlagsContextFreeText = "free_text"
	FlagsContextVoice    = "voice"
)

// FlagsContextPulse — контекст ответа на опрос дня day (pulse_d5). Отрицательные дни (до выхода)
// контракт не знает — это pulse_d0.
func FlagsContextPulse(day int) string {
	if day < 0 {
		day = 0
	}
	return fmt.Sprintf("pulse_d%d", day)
}

// Flag — одна категория разметки. rag-сервис уже проверил: код из таксономии, severity 1–3,
// confidence 0–1.
type Flag struct {
	Code       string  `json:"code"`
	Severity   int     `json:"severity"`
	Confidence float64 `json:"confidence"`
}

// FlagsResponse — ответ POST /flags. Degraded — LLM недоступен или ответил не по схеме: флагов
// нет, потому что не размечено, а не потому что их нет.
type FlagsResponse struct {
	Flags     []Flag `json:"flags"`
	Sentiment string `json:"sentiment"`
	Quote     string `json:"quote"`
	Degraded  bool   `json:"degraded"`
}

// DefaultFlagsTimeout — срок POST /flags: LLM до 10 с плюс один ретрай. Бот ждёт в фоне, ответ
// новичку от этого не задерживается. Переопределяется RAG_FLAGS_TIMEOUT (например, 30s).
const DefaultFlagsTimeout = 25 * time.Second

func flagsTimeout() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("RAG_FLAGS_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return DefaultFlagsTimeout
}

const maxFlagsBody = 1 << 16

// Flags — POST /flags. Ошибка — разметки нет; regex в Go от этого не зависит.
func (c *Client) Flags(ctx context.Context, req FlagsRequest) (*FlagsResponse, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	if req.Context == "" {
		req.Context = FlagsContextFreeText
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, flagsTimeout())
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/flags", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	if id := TraceID(ctx); id != "" {
		hr.Header.Set("X-Trace-Id", id)
	}
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &Error{Status: resp.StatusCode, Op: "POST /flags"}
	}
	var out FlagsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxFlagsBody)).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag POST /flags: тело не разобрано: %w", err)
	}
	// Страховка поверх проверок rag: severity 1–3, confidence 0–1, без пустых кодов.
	kept := out.Flags[:0]
	for _, f := range out.Flags {
		if f.Code == "" || f.Severity < 1 || f.Severity > 3 || f.Confidence < 0 || f.Confidence > 1 {
			continue
		}
		kept = append(kept, f)
	}
	out.Flags = kept
	return &out, nil
}
