package max

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Downloader — необязательная возможность клиента: скачать входящее вложение (голосовое) по
// payload.url из MAX. Отдельный интерфейс, как Typer: подделки других пакетов не обязаны его
// реализовывать, бизнес-код спрашивает приведением типа.
type Downloader interface {
	// Download — тело файла не длиннее limit байт; длиннее — ErrTooLarge.
	Download(ctx context.Context, url string, limit int64) ([]byte, error)
}

var (
	_ Downloader = (*SDKClient)(nil)
	_ Downloader = (*Fake)(nil)
)

// ErrTooLarge — вложение больше лимита вызывающего.
var ErrTooLarge = errors.New("max: вложение больше лимита")

// downloadTimeout — срок скачивания одного вложения: голосовое до 2 минут — сотни КБ.
const downloadTimeout = 30 * time.Second

// Download идёт тем же http.Client, что и API (корневой сертификат Минцифры), но без токена
// бота: ссылка на файл уже подписана, а токен на хост CDN не отдаём. Без повторов на 429 —
// это не API. [проверить]: отдаёт ли CDN MAX голосовое без заголовка Authorization.
func (c *SDKClient) Download(ctx context.Context, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("max: ссылка на вложение не https")
	}
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.doer.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("max: вложение не скачалось, статус %d", resp.StatusCode)
	}
	return readLimited(resp.Body, limit)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// fakeFiles — файлы подделки для Download: dev-хук кладёт голосовое (PutFile) и шлёт апдейт
// со ссылкой fake://.
type fakeFiles struct {
	mu   sync.Mutex
	data map[string][]byte
	n    int
}

// PutFile — положить файл в подделку, вернуть его ссылку для Audio.URL.
func (f *Fake) PutFile(data []byte) string {
	f.files.mu.Lock()
	defer f.files.mu.Unlock()
	if f.files.data == nil {
		f.files.data = map[string][]byte{}
	}
	f.files.n++
	url := fmt.Sprintf("fake://file/%d", f.files.n)
	f.files.data[url] = append([]byte(nil), data...)
	return url
}

func (f *Fake) Download(_ context.Context, url string, limit int64) ([]byte, error) {
	f.files.mu.Lock()
	data, ok := f.files.data[url]
	f.files.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("подделка MAX: файла %s нет", url)
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return append([]byte(nil), data...), nil
}
