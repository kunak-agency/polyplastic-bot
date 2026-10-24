package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Распознавание голосовых (api_contract.md, раздел «Голосовые»; services/rag/README.md, «Голос»):
// POST /transcribe, multipart audio → {text, lang, duration_s, confidence}. Пустой text — тишина
// или ничего не распознано, это не ошибка.

// TranscribeTimeout — срок POST /transcribe: T-one разбирает 30 с звука примерно за 1 с, на
// голосовом до 120 с и холодной модели — несколько секунд.
const TranscribeTimeout = 20 * time.Second

// Transcript — ответ POST /transcribe. Confidence — nil, если движок вероятностей не даёт (T-one).
type Transcript struct {
	Text       string   `json:"text"`
	Lang       string   `json:"lang"`
	DurationS  float64  `json:"duration_s"`
	Confidence *float64 `json:"confidence"`
}

// Transcribe — распознать голосовое. filename нужен только как имя части формы.
func (c *Client) Transcribe(ctx context.Context, filename string, data []byte) (*Transcript, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	ctx, cancel := context.WithTimeout(ctx, TranscribeTimeout)
	defer cancel()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("audio", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transcribe", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if id := TraceID(ctx); id != "" {
		req.Header.Set("X-Trace-Id", id)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &Error{Status: resp.StatusCode, Op: "POST /transcribe"}
	}
	var out Transcript
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag POST /transcribe: тело не разобрано: %w", err)
	}
	return &out, nil
}
