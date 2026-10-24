package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
)

// fakeASR — httptest-подделка POST /transcribe: отдаёт заданный текст (или статус) и запоминает
// присланные файлы.
type fakeASR struct {
	srv    *httptest.Server
	mu     sync.Mutex
	text   string
	status int
	got    [][]byte
}

func newFakeASR(t *testing.T, text string) *fakeASR {
	f := &fakeASR{text: text}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcribe" {
			t.Errorf("путь %s", r.URL.Path)
		}
		file, _, err := r.FormFile("audio")
		if err != nil {
			t.Errorf("нет части audio: %v", err)
			w.WriteHeader(422)
			return
		}
		data, _ := io.ReadAll(file)
		f.mu.Lock()
		f.got = append(f.got, data)
		status, text := f.status, f.text
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"` + text + `","lang":"ru","duration_s":2.1,"confidence":null}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeASR) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

const voiceBytes = "OggS-синтетика"

// voice — голосовое новичка: файл в подделке MAX, апдейт со ссылкой, как из long polling.
func (e *env) voice(a max.Audio) {
	if a.URL == "" && a.Transcription == "" {
		a.URL = e.fake.PutFile([]byte(voiceBytes))
	}
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, UserID: userID, ChatID: chatID, Audio: []max.Audio{a}})
	e.svc.Bot.Wait()
}

// Голосовой вопрос: скачан у MAX, распознан, эхо «🎙 Распознал: …», затем ответ rag по тексту.
func TestVoiceQuestionGoesToRAG(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(ragAnswer)
	asr := newFakeASR(t, "когда можно в отпуск")
	e.svc.Bot.asr = rag.New(asr.srv.URL, 0)

	e.voice(max.Audio{Duration: 3})

	if asr.calls() != 1 || string(asr.got[0]) != voiceBytes {
		t.Fatalf("в /transcribe ушло %d файлов", asr.calls())
	}
	texts := sentTexts(e.fake)
	if len(texts) < 2 || texts[0] != "🎙 Распознал: Когда можно в отпуск" {
		t.Fatalf("эхо распознанного: %q", texts)
	}
	if !strings.Contains(lastSent(t, e.fake).Text, "Отпуск можно взять после 6 месяцев работы") {
		t.Fatalf("ответ rag: %q", texts)
	}
	if reqs := f.requests(); len(reqs) != 1 || reqs[0]["question"] != "когда можно в отпуск" {
		t.Fatalf("вопрос в rag: %v", reqs)
	}
}

// Неудачи: пусто, сбой rag, нет распознавания, длиннее 120 с — «Не разобрал голосовое…», в rag
// /ask ничего не уходит. Расшифровка от MAX берётся без своего распознавания.
func TestVoiceFailuresAndMaxTranscription(t *testing.T) {
	e, f, _ := askEnv(t, time.Second)
	f.set(ragAnswer)
	asr := newFakeASR(t, "")
	e.svc.Bot.asr = rag.New(asr.srv.URL, 0)

	check := func(name string) {
		t.Helper()
		if m := lastSent(t, e.fake); m.Text != textVoiceVy {
			t.Fatalf("%s: %q", name, m.Text)
		}
		if len(f.requests()) != 0 {
			t.Fatalf("%s: ушло в rag %v", name, f.requests())
		}
		e.fake.Reset()
	}
	e.voice(max.Audio{})
	check("пустое распознавание")

	asr.mu.Lock()
	asr.status = 503
	asr.mu.Unlock()
	e.voice(max.Audio{})
	check("rag 503")

	e.voice(max.Audio{Duration: 121})
	check("длиннее 120 с")

	e.voice(max.Audio{URL: "fake://file/nope"})
	check("файл не скачался")

	e.svc.Bot.asr = nil
	e.voice(max.Audio{})
	check("распознавание не подключено")

	before := asr.calls()
	e.voice(max.Audio{Transcription: "где столовая"})
	if texts := sentTexts(e.fake); len(texts) == 0 || texts[0] != "🎙 Распознал: Где столовая" {
		t.Fatalf("расшифровка MAX: %q", texts)
	}
	if asr.calls() != before {
		t.Fatal("с расшифровкой MAX своё распознавание не нужно")
	}
	if reqs := f.requests(); len(reqs) != 1 || reqs[0]["question"] != "где столовая" {
		t.Fatalf("вопрос в rag: %v", reqs)
	}
}

// Голосом — ответ на шаг-ввод (r34): текст попадает в карточку, как набранный.
func TestVoiceInputStep(t *testing.T) {
	e := newEnv(t, flowNow)
	asr := newFakeASR(t, "работал инженером на экструзии")
	e.svc.Bot.asr = rag.New(asr.srv.URL, 0)
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		{DayOffset: 0, Time: "10:00", Kind: domain.KindInput, Title: "Резюме", TextVy: "Расскажите о себе"},
	})
	e.connect(t, c, domain.AddressVy, true)
	e.voice(max.Audio{})
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Резюме")
	if ev.Status != domain.EventAnswered || !strings.Contains(deref(ev.Answer), "работал инженером на экструзии") {
		t.Fatalf("ответ в карточке: %s %q", ev.Status, deref(ev.Answer))
	}
	if !hasText(e.fake, "🎙 Распознал: Работал инженером на экструзии") {
		t.Fatalf("эхо: %q", sentTexts(e.fake))
	}
}

// Голосом — «своими словами» после ветки опроса (open_free_text): опрос идёт дальше.
func TestVoiceSurveyFreeText(t *testing.T) {
	e := newEnv(t, flowNow)
	asr := newFakeASR(t, "сомневаюсь из за графика")
	e.svc.Bot.asr = rag.New(asr.srv.URL, 0)
	why := &domain.Branch{Actions: []domain.Action{{Type: domain.ActionOpenFreeText}}, Messages: []domain.BranchMessage{{TextVy: "Что повлияло?", TextTy: "Что повлияло?"}}}
	sv := &domain.Survey{Questions: []domain.SurveyQuestion{
		{Text: "Останетесь?", Type: "choice", Options: []string{"Да", "Не уверен"}, Branches: map[string]*domain.Branch{"2": why}},
		{Text: "Что улучшить?", Type: "yesno"},
	}}
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{{DayOffset: 0, Time: "10:00", Kind: domain.KindSurvey, Title: "Итоги", TextVy: "Опрос", Survey: sv}})
	e.connect(t, c, domain.AddressVy, true)
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Итоги")
	e.press(preAnswer + ev.ID.String() + ":0:2")
	e.svc.Bot.Wait()
	if emp := e.employee(t, c.Employee.ID); emp.AwaitFreeText == nil {
		t.Fatal("бот не ждёт текст после ветки")
	}
	e.voice(max.Audio{})
	if !hasText(e.fake, "🎙 Распознал: Сомневаюсь из за графика") || !strings.Contains(lastSent(t, e.fake).Text, "Что улучшить?") {
		t.Fatalf("после голосового: %q", sentTexts(e.fake))
	}
	if emp := e.employee(t, c.Employee.ID); emp.AwaitFreeText != nil {
		t.Fatalf("текст всё ещё ждём: %+v", emp.AwaitFreeText)
	}
}
