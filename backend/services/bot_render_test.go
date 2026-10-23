package services

import (
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"

	"github.com/google/uuid"
)

func TestEscapeMarkdown(t *testing.T) {
	got := escapeMarkdown(`Анна *жирно* _курсив_ [ссылка](x) <b>`)
	want := `Анна \*жирно\* \_курсив\_ \[ссылка\](x) \<b\>`
	if got != want {
		t.Fatalf("%q, want %q", got, want)
	}
}

func TestFirstName(t *testing.T) {
	for in, want := range map[string]string{
		"Иванов Пётр (синтетика)": "Пётр",
		"Иванов Пётр Сергеевич":   "Пётр",
		"Пётр": "Пётр",
		"":     "",
	} {
		if got := FirstName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestParseScore(t *testing.T) {
	for in, want := range map[string]int{"7": 7, " 8/10 ": 8, "семь": 7, "Десять!": 10} {
		if got, ok := parseScore(in); !ok || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, ok, want)
		}
	}
	if _, ok := parseScore("норм, но есть вопрос"); ok {
		t.Error("свободный текст не оценка")
	}
}

// Имя из карточки вставляется в markdown-сообщение экранированным.
func TestAddressChosenEscapesName(t *testing.T) {
	got := addressChosenText(domain.AddressTy, "Иванов *Пётр*")
	if !strings.Contains(got, `\*Пётр\*`) || !strings.Contains(got, "на «ты»") {
		t.Fatalf("%q", got)
	}
}

func lastRowIsHuman(t *testing.T, m max.OutMessage) {
	t.Helper()
	if len(m.Buttons) == 0 {
		t.Fatal("нет клавиатуры")
	}
	last := m.Buttons[len(m.Buttons)-1]
	if len(last) != 1 || last[0].Text != btnHuman {
		t.Fatalf("последний ряд %+v, want «Спросить человека»", last)
	}
}

func TestSurveyKeyboard10(t *testing.T) {
	step := domain.ScenarioStep{Kind: domain.KindSurvey, TextVy: "Чек *двух* недель", Survey: &domain.Survey{
		Scale: domain.Scale10, Threshold: 8, Questions: []domain.SurveyQuestion{{Text: "Задачи понятны?"}, {Text: "Руководитель доступен?"}},
	}}
	e := &domain.Employee{FullName: "Тестов Тест"}
	ev := &domain.ScheduledEvent{ID: uuid.New(), Survey: &domain.SurveyState{Screen: domain.ScreenQuestion}}
	m := surveyMessage(recipient{UserID: 1}, step, e, ev, time.Time{})
	if len(m.Buttons) != 4 || len(m.Buttons[0]) != 5 || len(m.Buttons[1]) != 5 {
		t.Fatalf("шкала 1–10 — два ряда по пять, ряд пропуска и человек: %+v", m.Buttons)
	}
	if m.Buttons[1][4].Text != "10" || m.Buttons[2][0].Text != btnSkip || m.Buttons[2][1].Text != btnRemind {
		t.Fatalf("кнопки %+v", m.Buttons)
	}
	lastRowIsHuman(t, m)
	if !strings.Contains(m.Text, "Вопрос 1 из 2") || !strings.Contains(m.Text, `Чек \*двух\* недель`) {
		t.Fatalf("текст %q", m.Text)
	}

	// Ответ ниже порога → экран причин в том же сообщении; готовый опрос — без клавиатуры.
	v := 6
	ev.Survey = &domain.SurveyState{Screen: domain.ScreenReasons, Results: []domain.SurveyResult{{Value: &v}}}
	m = surveyMessage(recipient{UserID: 1}, step, e, ev, time.Time{})
	if !strings.Contains(m.Text, "6/10") || !strings.Contains(m.Text, "Что именно не так?") {
		t.Fatalf("экран причин %q", m.Text)
	}
	lastRowIsHuman(t, m)
	ev.Survey = &domain.SurveyState{Q: 2, Results: []domain.SurveyResult{{Value: &v}, {Skipped: true}}}
	m = surveyMessage(recipient{UserID: 1}, step, e, ev, time.Time{})
	if len(m.Buttons) != 0 || !strings.Contains(m.Text, textSurveyDone) || !strings.Contains(m.Text, "пропущено") {
		t.Fatalf("итог %q %+v", m.Text, m.Buttons)
	}
}

func TestStepMessage(t *testing.T) {
	step := domain.ScenarioStep{TextTy: "Привет, {Имя}!", TextVy: "Здравствуйте, {Имя}!", Buttons: domain.ButtonList{
		{Label: "Да"}, {Label: "Нет"}, {Label: "Ссылка", Action: "link:https://example.org"}, {Label: "Человек", Action: "human"},
	}}
	ty := domain.AddressTy
	e := &domain.Employee{FullName: "Тестов Тест", AddressForm: &ty}
	m := stepMessage(recipient{UserID: 1}, step, e, uuid.New(), true, time.Time{})
	if m.Text != "[ТЕСТ] Привет, Тест!" || m.Format != "" {
		t.Fatalf("текст %q формат %q", m.Text, m.Format)
	}
	if len(m.Buttons) != 3 || len(m.Buttons[0]) != 2 || m.Buttons[1][0].URL != "https://example.org" {
		t.Fatalf("кнопки %+v", m.Buttons)
	}
	lastRowIsHuman(t, m)
	if got := stepMessage(recipient{UserID: 1}, domain.ScenarioStep{TextVy: "Просто текст"}, e, uuid.New(), false, time.Time{}); len(got.Buttons) != 0 {
		t.Fatalf("шаг без кнопок — без клавиатуры: %+v", got.Buttons)
	}
}

func TestInviteBrokenHasContactButton(t *testing.T) {
	m := inviteBrokenMessage(recipient{UserID: 1})
	if m.Text != textInviteBroken || len(m.Buttons) != 1 || !m.Buttons[0][0].Contact {
		t.Fatalf("%+v", m)
	}
}

func TestStartDayPhrase(t *testing.T) {
	site := domain.SiteOf(domain.SiteGTP)
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC) // понедельник (колонка date)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 25, 10, 0, 0, 0, site.Loc), "завтра"},                    // воскресенье накануне
		{time.Date(2026, 10, 23, 10, 0, 0, 0, site.Loc), "в понедельник, 26 октября"}, // D−1 перенесён на пятницу
		{time.Date(2026, 10, 26, 9, 0, 0, 0, site.Loc), "сегодня"},
		{time.Time{}, "в понедельник, 26 октября"},
	}
	for _, c := range cases {
		if got := StartDayPhrase(start, c.at, site); got != c.want {
			t.Errorf("at %v: %q, want %q", c.at, got, c.want)
		}
	}
	e := &domain.Employee{FullName: "Синтетов Пётр", Site: domain.SiteGTP, StartDate: start}
	s := domain.ScenarioStep{TextVy: "{Start_day} — ваш первый день, {Имя}."}
	if got := stepText(s, e, cases[1].at); got != "В понедельник, 26 октября — ваш первый день, Пётр." {
		t.Fatalf("подстановка: %q", got)
	}
	// Вторник — «во вторник».
	if got := StartDayPhrase(time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), time.Time{}, site); got != "во вторник, 27 октября" {
		t.Fatalf("вторник: %q", got)
	}
}

// Согласие и отказ — в выбранной форме обращения («Если передумаешь» на «ты»).
func TestConsentTextsByForm(t *testing.T) {
	for _, form := range []string{domain.AddressTy, domain.AddressVy} {
		e := newEnv(t, msk(2026, 11, 2, 12, 0))
		c := e.launch(t, date(2026, 11, 3))
		e.connect(t, c, form, false)
		var got string
		for _, a := range e.fake.Answers() {
			if a.Answer.Message != nil && strings.HasPrefix(a.Answer.Message.Text, "Понял. Опросы") {
				got = a.Answer.Message.Text
			}
		}
		want := map[string]string{domain.AddressTy: "Если передумаешь, напиши HR-команде.", domain.AddressVy: "Если передумаете, напишите HR-команде."}[form]
		if !strings.HasSuffix(got, want) {
			t.Fatalf("%s: %q", form, got)
		}
	}
}
