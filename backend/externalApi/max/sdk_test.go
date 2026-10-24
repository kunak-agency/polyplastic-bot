package max

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Токен и данные синтетические.
const testToken = "test-token-not-real"

// updatesBody — ответ GET /updates: голосовое с расшифровкой на уровне вложения (как в
// schema.yaml v0.0.30), голосовое с расшифровкой внутри payload, запуск по deep link и нажатие.
const updatesBody = `{
  "marker": 42,
  "updates": [
    {"update_type": "message_created", "timestamp": 1000,
     "message": {"sender": {"user_id": 7, "is_bot": false}, "recipient": {"chat_id": 100, "chat_type": "dialog"},
       "body": {"mid": "mid.a", "text": "", "attachments": [
         {"type": "audio", "payload": {"url": "https://example.invalid/a.ogg", "token": "tok-a"}, "transcription": "где взять пропуск"}
       ]}}},
    {"update_type": "message_created", "timestamp": 1001,
     "message": {"sender": {"user_id": 8}, "recipient": {"chat_id": 101, "chat_type": "dialog"},
       "body": {"mid": "mid.b", "text": "привет", "attachments": [
         {"type": "audio", "payload": {"url": "https://example.invalid/b.ogg", "transcription": "во сколько обед"}}
       ]}}},
    {"update_type": "bot_started", "timestamp": 1002, "chat_id": 102, "user": {"user_id": 9}, "payload": "inv-synthetic"},
    {"update_type": "message_callback", "timestamp": 1003,
     "callback": {"callback_id": "cb.1", "payload": "pulse:5", "user": {"user_id": 10}},
     "message": {"recipient": {"chat_id": 103, "user_id": 99}, "body": {"mid": "mid.c", "text": "Как прошёл день?"}}}
  ]
}`

type stubAPI struct {
	mu       sync.Mutex
	sends    []string    // тексты POST /messages, которые сервер принял
	sendAt   []time.Time // когда принял
	auth     []string
	fail429  int // сколько первых POST /messages ответить 429
	posts    int
	srv      *httptest.Server
	editBody string
	sendBody string // тело последнего принятого POST /messages
}

func newStubAPI(t *testing.T) *stubAPI {
	s := &stubAPI{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/updates":
			_, _ = io.WriteString(w, updatesBody)
		case r.Method == http.MethodPost && r.URL.Path == "/messages":
			s.posts++
			body, _ := io.ReadAll(r.Body)
			if s.posts <= s.fail429 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"code":"too.many.requests","message":"slow down"}`)
				return
			}
			var m struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(body, &m)
			s.sendBody = string(body)
			s.sends = append(s.sends, m.Text)
			s.sendAt = append(s.sendAt, time.Now())
			_, _ = io.WriteString(w, `{"message":{"body":{"mid":"mid.sent"}}}`)
		case r.Method == http.MethodPut && r.URL.Path == "/messages":
			body, _ := io.ReadAll(r.Body)
			s.editBody = string(body)
			_, _ = io.WriteString(w, `{"success":true}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"not.found","message":"no"}`)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func newTestClient(t *testing.T, s *stubAPI, interval time.Duration) *SDKClient {
	t.Helper()
	c, err := New(Config{Token: testToken, BaseURL: s.srv.URL, PollTimeout: time.Second, SendInterval: interval})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestGetUpdatesParsesTranscriptionAndPayload(t *testing.T) {
	s := newStubAPI(t)
	c := newTestClient(t, s, time.Millisecond)

	ups, marker, err := c.GetUpdates(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if marker != 42 || len(ups) != 4 {
		t.Fatalf("marker=%d, событий %d; want 42 и 4", marker, len(ups))
	}

	a := ups[0]
	if a.Type != UpdateMessageCreated || a.ChatID != 100 || a.UserID != 7 || a.MessageID != "mid.a" {
		t.Errorf("голосовое: %+v", a)
	}
	if len(a.Audio) != 1 || a.Audio[0].Transcription != "где взять пропуск" || a.Audio[0].Token != "tok-a" {
		t.Errorf("расшифровка на уровне вложения: %+v", a.Audio)
	}
	if b := ups[1]; len(b.Audio) != 1 || b.Audio[0].Transcription != "во сколько обед" || b.Text != "привет" {
		t.Errorf("расшифровка внутри payload: %+v", b)
	}
	if st := ups[2]; st.Type != UpdateBotStarted || st.Payload != "inv-synthetic" || st.UserID != 9 || st.ChatID != 102 {
		t.Errorf("bot_started: %+v", st)
	}
	cb := ups[3]
	if cb.Type != UpdateMessageCallback || cb.CallbackID != "cb.1" || cb.Payload != "pulse:5" || cb.UserID != 10 || cb.ChatID != 103 {
		t.Errorf("message_callback (UserID — нажавший, а не получатель): %+v", cb)
	}
	if s.auth[0] != testToken {
		t.Errorf("Authorization = %q, want токен без Bearer", s.auth[0])
	}
}

func TestSendRetriesOn429(t *testing.T) {
	s := newStubAPI(t)
	s.fail429 = 2
	c := newTestClient(t, s, time.Millisecond)
	var pauses []time.Duration
	c.doer.sleep = func(_ context.Context, d time.Duration) error { pauses = append(pauses, d); return nil }

	mid, err := c.Send(context.Background(), OutMessage{ChatID: 100, Text: "Корсик на связи"})
	if err != nil {
		t.Fatal(err)
	}
	if mid != "mid.sent" {
		t.Errorf("mid = %q", mid)
	}
	if len(s.sends) != 1 || s.sends[0] != "Корсик на связи" {
		t.Errorf("сервер принял %v: тело при повторе должно уйти заново", s.sends)
	}
	if len(pauses) != 2 || pauses[0] != time.Second {
		t.Errorf("паузы %v, want две по Retry-After = 1s", pauses)
	}
}

func TestSendGivesUpAfter429Attempts(t *testing.T) {
	s := newStubAPI(t)
	s.fail429 = 100
	c := newTestClient(t, s, time.Millisecond)
	c.doer.sleep = func(context.Context, time.Duration) error { return nil }

	if _, err := c.Send(context.Background(), OutMessage{ChatID: 100, Text: "x"}); err == nil {
		t.Fatal("ждали ошибку после исчерпания попыток на 429")
	}
	if s.posts != max429Attempts {
		t.Errorf("запросов %d, want %d", s.posts, max429Attempts)
	}
}

func TestSendPacesPerChat(t *testing.T) {
	s := newStubAPI(t)
	const interval = 60 * time.Millisecond
	c := newTestClient(t, s, interval)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Send(context.Background(), OutMessage{ChatID: 100, Text: "x"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(s.sendAt) != 3 {
		t.Fatalf("отправок %d", len(s.sendAt))
	}
	for i := 1; i < 3; i++ {
		// Пауза — от конца предыдущей отправки, значит между приёмами на сервере не меньше
		// интервала за вычетом дрожания часов.
		if gap := s.sendAt[i].Sub(s.sendAt[i-1]); gap < interval-5*time.Millisecond {
			t.Errorf("между отправками %d и %d прошло %v, want ≥ %v", i-1, i, gap, interval)
		}
	}
}

func TestEditSendsKeyboard(t *testing.T) {
	s := newStubAPI(t)
	c := newTestClient(t, s, time.Millisecond)
	err := c.Edit(context.Background(), "mid.c", OutMessage{ChatID: 103, Text: "Спасибо!", Buttons: [][]Button{
		{{Text: "Ещё вопрос", Payload: "ask"}, {Text: "Памятка", URL: "https://example.invalid/memo"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"inline_keyboard"`, `"callback"`, `"payload":"ask"`, `"link"`} {
		if !strings.Contains(s.editBody, want) {
			t.Errorf("тело правки без %s: %s", want, s.editBody)
		}
	}
}

func TestSendWithoutRecipient(t *testing.T) {
	c, err := New(Config{Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), OutMessage{Text: "x"}); err != errNoRecipient {
		t.Fatalf("err = %v", err)
	}
}

func TestAudioByMidBrokenBody(t *testing.T) {
	if got := audioByMid([]byte("{не json")); len(got) != 0 {
		t.Fatalf("мусор дал %v", got)
	}
}

// Правка в итог без клавиатуры: attachments уходит пустым списком, а не null — по схеме MAX
// null означает «вложения не менять», и клавиатура осталась бы.
func TestEditWithoutButtonsClearsKeyboard(t *testing.T) {
	s := newStubAPI(t)
	c := newTestClient(t, s, time.Millisecond)
	if err := c.Edit(context.Background(), "mid.c", OutMessage{ChatID: 103, Text: "✅ Записал"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.editBody, `"attachments":[]`) {
		t.Fatalf("тело правки: %s", s.editBody)
	}
}

func TestContactButton(t *testing.T) {
	s := newStubAPI(t)
	c := newTestClient(t, s, time.Millisecond)
	if err := c.Edit(context.Background(), "mid.c", OutMessage{ChatID: 103, Text: "x", Buttons: [][]Button{{{Text: "Проверить по телефону", Contact: true}}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.editBody, `"request_contact"`) {
		t.Fatalf("тело правки: %s", s.editBody)
	}
}

// Реплай: ReplyTo уходит в link {type: reply, mid}; в правке link нет — правка не меняет, на что
// сообщение отвечает.
func TestSendReplyLink(t *testing.T) {
	s := newStubAPI(t)
	c := newTestClient(t, s, time.Millisecond)
	if _, err := c.Send(context.Background(), OutMessage{ChatID: 103, Text: "Ответ", ReplyTo: "mid.q"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"link":{`, `"type":"reply"`, `"mid":"mid.q"`, `"text":"Ответ"`} {
		if !strings.Contains(s.sendBody, want) {
			t.Errorf("тело отправки без %s: %s", want, s.sendBody)
		}
	}
	if _, err := c.Send(context.Background(), OutMessage{ChatID: 103, Text: "Без реплая"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.sendBody, `"reply"`) {
		t.Fatalf("link без ReplyTo: %s", s.sendBody)
	}
	if err := c.Edit(context.Background(), "mid.c", OutMessage{ChatID: 103, Text: "x", ReplyTo: "mid.q"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.editBody, `"reply"`) {
		t.Fatalf("link в правке: %s", s.editBody)
	}
}
