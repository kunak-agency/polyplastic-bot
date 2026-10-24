package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// TTSFormat — что отдаёт POST /tts: PCM16LE моно 8 кГц (audio/L16;rate=8000), чанками по
// предложениям, пока синтез идёт.
const TTSFormat = "audio/L16;rate=8000"

// Synthesize — POST /tts {text}: поток звука. Срок задаёт ctx вызывающего (на синтез, а не на
// длину фразы); тело закрывает вызывающий.
func (c *Client) Synthesize(ctx context.Context, text string) (io.ReadCloser, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tts", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Op: "POST /tts"}
	}
	return resp.Body, nil
}
