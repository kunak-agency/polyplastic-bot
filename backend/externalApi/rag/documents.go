package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// Загрузка документов HR (services/rag/README.md, «Загрузка документов HR»): POST /documents и
// GET /documents. У rag нет авторизации — права HR проверяет backend до вызова.

// Uploaded — ответ POST /documents.
type Uploaded struct {
	DocID              string `json:"doc_id"`
	Chunks             int    `json:"chunks"`
	Clauses            int    `json:"clauses"`
	LowConfidencePages []int  `json:"low_confidence_pages"`
}

// Document — элемент GET /documents.
type Document struct {
	DocID              string  `json:"doc_id"`
	Doc                string  `json:"doc"`
	Filename           *string `json:"filename"`
	Kind               string  `json:"kind"`   // corpus | pdf | pptx | xlsx | text
	Source             string  `json:"source"` // corpus | upload
	Chunks             int     `json:"chunks"`
	Clauses            int     `json:"clauses"`
	LowConfidencePages []int   `json:"low_confidence_pages"`
	UploadedAt         string  `json:"uploaded_at"`
}

// Значения Document.Source.
const (
	SourceCorpus = "corpus"
	SourceUpload = "upload"
)

// RejectedError — rag отказал в разборе файла по смыслу: 415 (формат), 422 (пустой, битый, нет
// текста), 413 (больше лимита). Это не сбой rag, а ответ про файл: сервис превращает его в 422
// со своим текстом. Detail — `detail` FastAPI, только для лога.
type RejectedError struct {
	Status int
	Detail string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("rag POST /documents: файл отклонён, статус %d: %s", e.Status, e.Detail)
}

// maxDocumentsBody — предел ответа GET /documents: метаданные, не тексты.
const maxDocumentsBody = 4 << 20

// UploadDocument — POST /documents (multipart: file, doc). Срок — из ctx: индекс пересобирается
// синхронно, с hybrid это десятки секунд, общий askTimeout тут не подходит.
func (c *Client) UploadDocument(ctx context.Context, doc, filename string, data []byte) (*Uploaded, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if doc != "" {
		if err := mw.WriteField("doc", doc); err != nil {
			return nil, err
		}
	}
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/documents", &body)
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
	switch {
	case resp.StatusCode == http.StatusUnsupportedMediaType, resp.StatusCode == http.StatusUnprocessableEntity,
		resp.StatusCode == http.StatusRequestEntityTooLarge:
		var e struct {
			Detail any `json:"detail"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&e)
		return nil, &RejectedError{Status: resp.StatusCode, Detail: fmt.Sprint(e.Detail)}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &Error{Status: resp.StatusCode, Op: "POST /documents"}
	}
	var out Uploaded
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag POST /documents: тело не разобрано: %w", err)
	}
	if out.DocID == "" {
		return nil, fmt.Errorf("rag POST /documents: нет doc_id")
	}
	if out.LowConfidencePages == nil {
		out.LowConfidencePages = []int{}
	}
	return &out, nil
}

// ListDocuments — GET /documents: что сейчас в индексе (корпус и загруженные).
func (c *Client) ListDocuments(ctx context.Context) ([]Document, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("rag: адрес не задан (RAG_BASE_URL)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/documents", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &Error{Status: resp.StatusCode, Op: "GET /documents"}
	}
	var out struct {
		Items []Document `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDocumentsBody)).Decode(&out); err != nil {
		return nil, fmt.Errorf("rag GET /documents: тело не разобрано: %w", err)
	}
	return out.Items, nil
}
