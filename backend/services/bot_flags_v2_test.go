package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
)

// /flags v2: слияние regex и модели, строка модели и цитата в обращение, контекст voice. Тексты и
// разметка синтетические.

func TestMergeFlags(t *testing.T) {
	got := mergeFlags([]string{"F7"}, []rag.Flag{{Code: "F7", Severity: 3, Confidence: 0.8}, {Code: "F6", Severity: 2, Confidence: 0.6}})
	if len(got) != 2 || got[0].Code != "F7" || got[0].Severity != 3 || got[0].Confidence != 1 || got[1].Code != "F6" {
		t.Fatalf("слияние %+v", got)
	}
	// модель выше по серьёзности — первым её код
	got = mergeFlags([]string{"F7"}, []rag.Flag{{Code: "F0", Severity: 3, Confidence: 0.9}})
	if got[0].Code != "F0" || got[1] != (rag.Flag{Code: "F7", Severity: regexSeverity, Confidence: regexConfidence}) {
		t.Fatalf("порядок %+v", got)
	}
	if ex := extraFlags(got, []string{"F7"}); len(ex) != 1 || ex[0].Code != "F0" {
		t.Fatalf("новые коды %+v", ex)
	}
}

func TestNoReescalationKeepsF0(t *testing.T) {
	ds := noReescalation([]flagDecision{{code: "F0", act: flagActTicket}, {code: "F4", act: flagActTicket}, {code: "F7", act: flagActOffer}, {code: "F6", act: flagActRisk}})
	if acts(ds) != "F0:ticket,F4:risk,F7:risk,F6:risk" {
		t.Fatalf("%s", acts(ds))
	}
}

func TestSetTicketNotes(t *testing.T) {
	tax := testTax(t)
	given := domain.ConsentGiven
	e := &domain.Employee{Consent: &given}
	full := "полный текст"
	tk := &domain.Ticket{Quote: &full}
	sum, q := "Новичок сообщает, что нет пропуска.", "нет пропуска"
	f2, _ := tax.Get("F2")
	if !setTicketNotes(e, f2, tk, &sum, &q) || tk.SummaryModel == nil || *tk.SummaryModel != sum || *tk.Quote != q {
		t.Fatalf("F2 %+v", tk)
	}
	f10, _ := tax.Get("F10")
	tk = &domain.Ticket{}
	if setTicketNotes(e, f10, tk, &sum, &q) || tk.SummaryModel != nil || tk.Quote != nil {
		t.Fatalf("F10 без цитаты и строки: %+v", tk)
	}
	tk = &domain.Ticket{Quote: &full}
	if setTicketNotes(&domain.Employee{}, f2, tk, &sum, &q) || *tk.Quote != full {
		t.Fatalf("без согласия: %+v", tk)
	}
}

// Regex F5 — обращение сразу; модель в фоне пишет в него строку и цитату, полный текст остаётся
// в переписке, второго обращения нет.
func TestRegexTicketGetsModelSummaryAndQuote(t *testing.T) {
	text := "сегодня мастер ударил меня в раздевалке"
	e, f, c := flagsEnv(t, `{"flags":[{"code":"F5","severity":3,"confidence":0.9}],"sentiment":"neg","tone_confidence":0.9,"quote":"мастер ударил меня","summary":"Новичок сообщает, что мастер его ударил.","clarify_hint":null}`)
	e.withChats()
	e.ask(text)
	e.svc.Bot.Wait()
	if n := len(f.flagRequests()); n != 1 {
		t.Fatalf("/flags %d", n)
	}
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.Category != "F5" || tk.SummaryModel == nil || *tk.SummaryModel != "Новичок сообщает, что мастер его ударил." ||
		tk.Quote == nil || *tk.Quote != "мастер ударил меня" {
		t.Fatalf("обращение %+v", tk)
	}
	msgs, err := e.repos.Tickets.Messages(context.Background(), tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range msgs {
		found = found || m.Text == text
	}
	if !found {
		t.Fatalf("полного текста нет в переписке: %+v", msgs)
	}
}

// LLM-обращение (без regex): строка модели и цитата модели — сразу при создании.
func TestLLMTicketHasModelSummary(t *testing.T) {
	e, _, c := flagsEnv(t, `{"flags":[{"code":"F4","severity":2,"confidence":0.85}],"sentiment":"neg","tone_confidence":0.8,"quote":"наставник ничего не объясняет","summary":"Новичок сообщает, что наставник не объясняет работу.","clarify_hint":null}`)
	e.withChats()
	e.ask("наставник ничего не объясняет, говорит разбирайся сам")
	e.svc.Bot.Wait()
	tk := e.onlyTicket(t, c.Employee.ID)
	if tk.SummaryModel == nil || !strings.HasPrefix(*tk.SummaryModel, "Новичок сообщает") || tk.Quote == nil || *tk.Quote != "наставник ничего не объясняет" {
		t.Fatalf("обращение %+v", tk)
	}
	if !strings.Contains(tk.Summary, "LLM") {
		t.Fatalf("сводка кода затёрта: %q", tk.Summary)
	}
}

// Голосовой вопрос — в /flags уходит context=voice.
func TestVoiceQuestionFlagsContext(t *testing.T) {
	e, f, _ := flagsEnv(t, `{"flags":[],"sentiment":"neu","quote":""}`)
	asr := newFakeASR(t, "э-э где тут столовая")
	e.svc.Bot.asr = rag.New(asr.srv.URL, 0)
	e.voice(max.Audio{Duration: 3})
	fr := f.flagRequests()
	if len(fr) != 1 || fr[0]["context"] != rag.FlagsContextVoice {
		t.Fatalf("/flags %v", fr)
	}
}

// Комментарий к опросу с regex F7: тон модели заменяет neg кода, строка и цитата — в ответ.
func TestSurveyRegexCommentGetsModelMarkup(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 18, 30))
	f := newFakeRAG(t)
	e.svc.Bot.rag = rag.New(f.srv.URL, time.Second)
	f.flagsReply = `{"flags":[{"code":"F7","severity":2,"confidence":0.8},{"code":"F6","severity":2,"confidence":0.7}],"sentiment":"neg","tone_confidence":0.8,"quote":"если так дальше, уволюсь","summary":"Новичок перегружен и думает об уходе.","clarify_hint":null}`
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	first := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	e.press(preStepButton + first.ID.String() + ":0")
	pulse := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	id := pulse.ID.String()
	e.press(preAnswer + id + ":0:2")
	e.press(preWrite + id + ":0")
	e.say("смены по 12 часов, если так дальше, уволюсь")
	e.svc.Bot.Wait()
	answers, _ := e.repos.Events.Answers(context.Background(), pulse.ID)
	if len(answers) != 1 {
		t.Fatalf("ответы %+v", answers)
	}
	a := answers[0]
	if a.Sentiment == nil || *a.Sentiment != domain.SentimentNeg || a.Summary == nil || a.Quote == nil || a.ToneConfidence == nil ||
		!a.Flags.Has("F7") || !a.Flags.Has("F6") {
		t.Fatalf("ответ %+v", a)
	}
	if n := len(e.tickets(t, c.Employee.ID)); n != 0 {
		t.Fatalf("обращений %d", n)
	}
}

// Голосом — «свой вариант» ветки шага (open_free_text): в /flags уходит context=voice, набранный
// текст — без него (free_text по умолчанию).
func TestVoiceFreeTextFlagsContext(t *testing.T) {
	for _, voice := range []bool{true, false} {
		e := newEnv(t, flowNow)
		f := newFakeRAG(t)
		f.flagsReply = `{"flags":[],"sentiment":"neu","quote":""}`
		e.svc.Bot.rag = rag.New(f.srv.URL, time.Second)
		asr := newFakeASR(t, "ну это самое не прислали договор")
		e.svc.Bot.asr = rag.New(asr.srv.URL, 0)
		br := &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Понимаю", TextTy: "Понимаю", Buttons: []domain.StepButton{
			{Label: "Описать проблему", Action: "escalate:recruiter", OpensFreeText: true},
		}}}}
		c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
			txt(0, "10:00", "Вопрос", "Документы в порядке?", domain.StepButton{Label: "Нет", Branch: br}),
		})
		e.connect(t, c, domain.AddressVy, true)
		q := eventByTitle(t, e.events(t, c.Employee.ID), "Вопрос")
		e.press(preStepButton + q.ID.String() + ":0")
		e.press(findPayload(lastSent(t, e.fake), "Описать проблему"))
		if voice {
			e.voice(max.Audio{})
		} else {
			e.say("не прислали договор")
		}
		e.svc.Bot.Wait()
		want := rag.FlagsContextFreeText
		if voice {
			want = rag.FlagsContextVoice
		}
		fr := f.flagRequests()
		if len(fr) != 1 || fr[0]["context"] != want {
			t.Fatalf("voice=%v: /flags %v", voice, fr)
		}
		if emp := e.employee(t, c.Employee.ID); emp.AwaitFreeText != nil {
			t.Fatalf("voice=%v: текст всё ещё ждём", voice)
		}
	}
}
