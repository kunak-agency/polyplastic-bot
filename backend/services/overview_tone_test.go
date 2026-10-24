package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/rag"
	"main/repositories"
)

// Тональность: агрегат с порогом n ≥ 5 (чистые тесты) и сохранение тона ботом (Postgres +
// подделки MAX и rag). Тексты синтетические.

func toneRows(t *testing.T, at time.Time, site string, tones ...string) *repositories.OverviewRows {
	t.Helper()
	rows := &repositories.OverviewRows{}
	for i, s := range tones {
		e := ovEmp("Тонов Синтетик", site, 10+i)
		rows.Employees = append(rows.Employees, e)
		s := s
		if i%2 == 0 {
			rows.Answers = append(rows.Answers, repositories.OverviewAnswer{EmployeeID: e.ID, Sentiment: &s, CreatedAt: at})
		} else {
			rows.Questions = append(rows.Questions, repositories.OverviewQuestion{EmployeeID: e.ID, Sentiment: &s, CreatedAt: at})
		}
	}
	return rows
}

func TestToneWeekAnonymityAndShares(t *testing.T) {
	at := ovNow.Add(-time.Hour)
	// 4 текста — мало данных, доли не отдаются.
	o := BuildOverview(toneRows(t, at, domain.SiteGTP, "neg", "neg", "pos", "neu"), "", ovNow, ovTax(t))
	if o.ToneWeek.Enough || o.ToneWeek.N != 4 || o.ToneWeek.Pos != nil || o.ToneWeek.Neg != nil {
		t.Fatalf("при n=4 доли скрыты: %+v", o.ToneWeek)
	}
	// 7 текстов: 1 pos, 4 neu, 2 neg → 14/57/29 (сумма 100).
	rows := toneRows(t, at, domain.SiteGTP, "pos", "neu", "neu", "neu", "neu", "neg", "neg")
	// тестовая запись (машина времени) и прошлая неделя не считаются
	neg := domain.SentimentNeg
	rows.Answers = append(rows.Answers,
		repositories.OverviewAnswer{EmployeeID: rows.Employees[0].ID, Sentiment: &neg, CreatedAt: at, IsTest: true},
		repositories.OverviewAnswer{EmployeeID: rows.Employees[0].ID, Sentiment: &neg, CreatedAt: at.AddDate(0, 0, -7)})
	o = BuildOverview(rows, "", ovNow, ovTax(t))
	c := o.ToneWeek
	if !c.Enough || c.N != 7 || *c.Pos+*c.Neu+*c.Neg != 100 || *c.Pos != 14 || *c.Neu != 57 || *c.Neg != 29 {
		t.Fatalf("доли %+v pos=%v neu=%v neg=%v", c, deref2(c.Pos), deref2(c.Neu), deref2(c.Neg))
	}
	// другая площадка этих текстов не видит
	if g := BuildOverview(rows, domain.SiteGrPP, ovNow, ovTax(t)).ToneWeek; g.N != 0 || g.Enough {
		t.Fatalf("GrPP видит тон GTP: %+v", g)
	}
	if !strings.Contains(ToneLine(c), "57 % нейтр · 14 % поз · 29 % нег (n = 7)") {
		t.Fatalf("строка %q", ToneLine(c))
	}
	if l := ToneLine(ToneCell{N: 3}); !strings.Contains(l, "мало данных (3 текста") {
		t.Fatalf("строка %q", l)
	}
}

func deref2(p *int) int {
	if p == nil {
		return -1
	}
	return *p
}

func TestDigestToneTrendAndMessage(t *testing.T) {
	tax := ovTax(t)
	rows, texts := digestRows(t)
	mon := weekMon()
	// 6 размеченных текстов в отчётную неделю, 2 — неделей раньше
	for i, e := range rows.Employees[:8] {
		s := domain.SentimentNeu
		at := mon.Add(12 * time.Hour)
		if i >= 6 {
			at, s = mon.AddDate(0, 0, -7).Add(12*time.Hour), domain.SentimentNeg
		}
		rows.Questions = append(rows.Questions, repositories.OverviewQuestion{EmployeeID: e.ID, Sentiment: &s, CreatedAt: at})
	}
	d := BuildDigest(rows, texts, "", mon, ovNow, tax)
	if !d.Tone.Enough || d.Tone.N != 6 || *d.Tone.Neu != 100 {
		t.Fatalf("тон недели %+v", d.Tone)
	}
	if len(d.ToneTrend.Weeks) != TrendWeeks || !d.ToneTrend.Weeks[TrendWeeks-1].Equal(mon) {
		t.Fatalf("недели %v", d.ToneTrend.Weeks)
	}
	var all *ToneSeries
	for i := range d.ToneTrend.Series {
		if d.ToneTrend.Series[i].Site == TrendAll {
			all = &d.ToneTrend.Series[i]
		}
	}
	if all == nil || all.Points[TrendWeeks-2].N != 2 || all.Points[TrendWeeks-2].Enough || all.Points[TrendWeeks-1].N != 6 {
		t.Fatalf("динамика %+v", all)
	}
	if !strings.Contains(d.MaxText, "Тон текстовых ответов: 100 % нейтр · 0 % поз · 0 % нег (n = 6).") {
		t.Fatalf("строки тона нет:\n%s", d.MaxText)
	}
	if extra := ExtraNumbers(d.MaxText, DigestFacts(d)); len(extra) > 0 {
		t.Fatalf("в сообщении числа не из входа %v", extra)
	}
}

// Свободный вопрос: тон из /flags пишется в запись вопроса; карточка отдаёт его без текста.
func TestToneSavedForFreeQuestion(t *testing.T) {
	e, _, c := flagsEnv(t, `{"flags":[],"sentiment":"pos","quote":""}`)
	e.ask("спасибо, наставник всё показал")
	qs, err := e.repos.Questions.Recent(context.Background(), c.Employee.ID, time.Time{}, 5)
	if err != nil || len(qs) != 1 || qs[0].Sentiment == nil || *qs[0].Sentiment != domain.SentimentPos {
		t.Fatalf("тон вопроса: %+v %v", qs, err)
	}
	card, err := e.svc.Newcomers.Get(context.Background(), c.Employee.ID)
	if err != nil || len(card.RecentTone) != 1 || card.RecentTone[0].Kind != "question" || card.RecentTone[0].Sentiment != domain.SentimentPos {
		t.Fatalf("карточка: %+v %v", card, err)
	}
}

// Regex F7: модель не вызывается, тон ставит код — neg.
func TestToneRegexIsNegative(t *testing.T) {
	e, f, c := flagsEnv(t, `{"flags":[],"sentiment":"pos","quote":""}`)
	e.ask("думаю уволиться")
	if n := len(f.flagRequests()); n != 0 {
		t.Fatalf("/flags при regex: %d", n)
	}
	qs, _ := e.repos.Questions.Recent(context.Background(), c.Employee.ID, time.Time{}, 5)
	if len(qs) != 1 || qs[0].Kind != domain.QuestionFlag || qs[0].Sentiment == nil || *qs[0].Sentiment != domain.SentimentNeg {
		t.Fatalf("запись сигнала: %+v", qs)
	}
}

// Комментарий к опросу: тон — в ответ опроса.
func TestToneSavedForSurveyComment(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 18, 30))
	f := newFakeRAG(t)
	e.svc.Bot.rag = rag.New(f.srv.URL, time.Second)
	f.flagsReply = `{"flags":[],"sentiment":"neg","quote":""}`
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	first := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	e.press(preStepButton + first.ID.String() + ":0")
	pulse := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	id := pulse.ID.String()
	e.press(preAnswer + id + ":0:2")
	e.press(preWrite + id + ":0")
	e.say("день тяжёлый, устал")
	e.svc.Bot.Wait()
	answers, _ := e.repos.Events.Answers(context.Background(), pulse.ID)
	if len(answers) != 1 || answers[0].Sentiment == nil || *answers[0].Sentiment != domain.SentimentNeg {
		t.Fatalf("тон ответа опроса: %+v", answers)
	}
}
