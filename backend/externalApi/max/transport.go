package max

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"main/pkg/logger"
)

// Повторы на 429. SDK о статусе ответа не знает (parseResponseError читает только тело), так
// что ловим его здесь, на уровне HTTP, до того как SDK превратит ответ в ошибку.
const (
	max429Attempts    = 3
	default429Pause   = time.Second
	max429Pause       = 30 * time.Second
	pathUpdates       = "/updates"
	headerSlackOnPoll = 15 * time.Second // запас сверх timeout long polling на заголовки ответа
)

// doer — http-клиент, который SDK получает через maxbot.WithHTTPClient. Поверх http.Client:
//   - пауза и повтор на 429 (Retry-After или default429Pause);
//   - копия тела успешного GET /updates для разбора полей, которых нет в SDK-модели (audio.go).
type doer struct {
	client *http.Client
	pause  time.Duration
	sleep  func(ctx context.Context, d time.Duration) error

	mu          sync.Mutex
	lastUpdates []byte // тело последнего успешного GET /updates
}

// newHTTPClient — клиент с корневым сертификатом Минцифры. Общий срок запроса не ставим:
// long polling держит соединение до timeout, загрузка видео идёт дольше — сроки задаёт ctx
// вызова, а транспорт ограничивает только ожидание заголовков ответа.
func newHTTPClient(caFile string, pollTimeout time.Duration) (*http.Client, error) {
	pool, err := certPool(caFile)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	tr.ResponseHeaderTimeout = pollTimeout + headerSlackOnPoll
	return &http.Client{Transport: tr}, nil
}

// certPool — системный пул плюс сертификат из файла. Пустой путь — nil: Go возьмёт системный
// пул сам. Файл с Госуслуг бывает и PEM, и DER — принимаем оба.
func certPool(caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("MAX_CA_FILE: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if pool.AppendCertsFromPEM(data) {
		return pool, nil
	}
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, fmt.Errorf("MAX_CA_FILE %s: ни PEM, ни DER-сертификат: %w", caFile, err)
	}
	pool.AddCert(cert)
	return pool, nil
}

// Do — запрос с повтором на 429. Запрос с телом, которое нельзя перечитать (загрузка файла
// идёт через io.Pipe), не повторяется: 429 уходит в SDK как есть.
func (d *doer) Do(req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := d.client.Do(req)
		if err != nil {
			return nil, err
		}
		replayable := req.Body == nil || req.GetBody != nil
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= max429Attempts || !replayable {
			return d.tap(req, resp)
		}

		wait := retryAfter(resp.Header.Get("Retry-After"), d.pause)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		// В строке только метод и путь: токен ходит в заголовке, а query с chat_id не нужен.
		logger.Warn("MAX: 429 на %s %s — пауза %v, попытка %d из %d", req.Method, req.URL.Path, wait, attempt, max429Attempts)
		if err := d.sleep(req.Context(), wait); err != nil {
			return nil, err
		}
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req = req.Clone(req.Context())
			req.Body = body
		}
	}
}

// tap — копия тела успешного GET /updates. SDK разбирает его в свою модель и теряет поля,
// которых в ней нет; мы читаем то же тело ещё раз (audio.go).
func (d *doer) tap(req *http.Request, resp *http.Response) (*http.Response, error) {
	if req.Method != http.MethodGet || req.URL.Path != pathUpdates || resp.StatusCode != http.StatusOK {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.lastUpdates = body
	d.mu.Unlock()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// takeUpdates — тело последнего GET /updates; сбрасывает его, чтобы следующий опрос не увидел
// старое тело, если его запрос упал.
func (d *doer) takeUpdates() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	b := d.lastUpdates
	d.lastUpdates = nil
	return b
}

// retryAfter — пауза из Retry-After (секунды). Дата вместо секунд и мусор — пауза по умолчанию.
func retryAfter(h string, def time.Duration) time.Duration {
	if def <= 0 {
		def = default429Pause
	}
	n, err := strconv.Atoi(h)
	if err != nil || n <= 0 {
		return def
	}
	return min(time.Duration(n)*time.Second, max429Pause)
}

// errNoRecipient — у сообщения нет ни чата, ни пользователя.
var errNoRecipient = errors.New("max: у сообщения не указан ни ChatID, ни UserID")
