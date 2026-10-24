package services

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/externalApi/max"
	"main/repositories"

	"github.com/google/uuid"
)

// Интеграционные тесты: настоящий Postgres (TEST_DATABASE_URL, отдельная база — таблицы
// очищаются) и подделка MAX. Без TEST_DATABASE_URL пропускаются.
//
//	docker compose up -d postgres
//	TEST_DATABASE_URL=postgres://polyplastic:polyplastic@localhost:5437/polyplastic_test?sslmode=disable go test -p 1 ./...

type env struct {
	svc   *Services
	repos *repositories.Repos
	fake  *max.Fake
	clock *time.Time
}

func newEnv(t *testing.T, now time.Time) *env {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	repos, err := repositories.InitRepos(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := repos.MigrateDB(); err != nil {
		t.Fatal(err)
	}
	if err := repos.Truncate(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock := now
	fake := max.NewFake()
	svc := NewServices(repos, &externalApi.ExternalAPI{Max: fake}, Options{
		Bot: config.Bot{Username: "korsik_test_bot", InviteLinkFormat: config.DefaultInviteLinkFormat},
		Now: func() time.Time { return clock },
	})
	t.Cleanup(func() {
		if svc.Bot != nil {
			svc.Bot.Wait() // фоновые ответы ещё пишут в базу
		}
		_ = repos.Close()
	})
	return &env{svc: svc, repos: repos, fake: fake, clock: &clock}
}

// testSteps — короткий синтетический сценарий: пакет D−3, шаг D0 с кнопками, пульс D0 (1–5),
// чек D+14 (1–10, порог 8, три вопроса).
func testSteps() []domain.ScenarioStep {
	return []domain.ScenarioStep{
		{DayOffset: -3, Time: "10:00", Kind: domain.KindText, Title: "Документы", TextTy: "Возьми паспорт, {Имя}", TextVy: "Возьмите паспорт", Buttons: domain.ButtonList{{Label: "Всё понятно"}}, WorkHoursOnly: true},
		{DayOffset: -3, Time: "10:00", Kind: domain.KindText, Title: "Факты", TextTy: "Три факта", TextVy: "Три факта", WorkHoursOnly: true},
		{DayOffset: 0, Time: "10:00", Kind: domain.KindText, Title: "С первым днём", TextTy: "С первым днём", TextVy: "С первым днём", Buttons: domain.ButtonList{{Label: "Спасибо!"}, {Label: "Позже", Action: "skip"}}, WorkHoursOnly: true},
		{DayOffset: 0, Time: "18:00", Kind: domain.KindSurvey, Title: "Итог дня", TextVy: "Как день?", WorkHoursOnly: true,
			Survey: &domain.Survey{Scale: domain.Scale5, Threshold: 3, Questions: []domain.SurveyQuestion{{Text: "Как прошёл день?"}}}},
		{DayOffset: 14, Time: "10:00", Kind: domain.KindSurvey, Title: "Чек двух недель", TextVy: "Две недели", WorkHoursOnly: true,
			Survey: &domain.Survey{Scale: domain.Scale10, Threshold: 8, Questions: []domain.SurveyQuestion{
				{Text: "Задачи понятны?"}, {Text: "Руководитель доступен?", Options: []string{"Редко на месте", "Не отвечает"}}, {Text: "Наставник помогает?"},
			}}},
		{DayOffset: 1, Time: "10:00", Kind: domain.KindText, Title: "Только Лужники", TextVy: "Лужники", Sites: domain.StringList{domain.SiteLuzhniki}, WorkHoursOnly: true},
	}
}

// launch — опубликованный сценарий и новичок GTP с выходом в start.
func (e *env) launch(t *testing.T, start time.Time) *NewcomerCard {
	t.Helper()
	ctx := context.Background()
	d, err := e.svc.Scenarios.Create(ctx, "Тестовый", []string{domain.SiteGTP}, testSteps())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Newcomers.Create(ctx, NewcomerInput{
		FullName: "Тестов Пётр (синтетика)", Site: domain.SiteGTP, StartDate: start, ScenarioID: d.Scenario.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e *env) token(t *testing.T, c *NewcomerCard) string {
	t.Helper()
	i := strings.Index(c.InviteLink, "start=")
	if i < 0 {
		t.Fatalf("ссылка %q", c.InviteLink)
	}
	return c.InviteLink[i+len("start="):]
}

func (e *env) employee(t *testing.T, id uuid.UUID) *domain.Employee {
	t.Helper()
	emp, err := e.repos.Employees.ByID(context.Background(), id)
	if err != nil || emp == nil {
		t.Fatalf("сотрудник %v %v", emp, err)
	}
	return emp
}

func (e *env) events(t *testing.T, id uuid.UUID) []domain.ScheduledEvent {
	t.Helper()
	evs, err := e.repos.Events.ByEmployee(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func eventByTitle(t *testing.T, evs []domain.ScheduledEvent, title string) domain.ScheduledEvent {
	t.Helper()
	for _, ev := range evs {
		if ev.Title == title {
			return ev
		}
	}
	t.Fatalf("нет события %q", title)
	return domain.ScheduledEvent{}
}

const userID, chatID = 700001, 800001

func (e *env) press(payload string) {
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCallback, UserID: userID, ChatID: chatID, Payload: payload, CallbackID: "cb-" + payload})
}

func (e *env) say(text string) {
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, UserID: userID, ChatID: chatID, Text: text})
}

// connect — новичок открыл ссылку, выбрал форму и ответил на согласие.
func (e *env) connect(t *testing.T, c *NewcomerCard, form string, consent bool) {
	t.Helper()
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateBotStarted, UserID: userID, ChatID: chatID, Payload: e.token(t, c)})
	e.press("a:" + form)
	if consent {
		e.press(payConsentYes)
	} else {
		e.press(payConsentNo)
	}
	e.svc.Bot.Wait()
}

func lastAnswer(t *testing.T, f *max.Fake) max.FakeAnswer {
	t.Helper()
	a := f.Answers()
	if len(a) == 0 {
		t.Fatal("ответов на нажатия нет")
	}
	return a[len(a)-1]
}

// Вход по приглашению: связка, «На ты» с правкой без клавиатуры, согласие, пакет D−3.
func TestBotStartByInvite(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0)) // понедельник, D−1: D−3 (суббота) перенесён на сегодня 10:00 — созрел
	c := e.launch(t, date(2026, 11, 3))
	if !strings.HasPrefix(c.InviteLink, "https://max.ru/korsik_test_bot?start=inv_") {
		t.Fatalf("ссылка %q", c.InviteLink)
	}
	if tok := e.token(t, c); len(tok) > 128 {
		t.Fatalf("токен длиннее 128: %d", len(tok))
	}
	ctx := context.Background()
	e.svc.Bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: userID, ChatID: chatID, Payload: e.token(t, c)})

	emp := e.employee(t, c.Employee.ID)
	if emp.Connection != domain.ConnConnected || emp.MaxUserID == nil || *emp.MaxUserID != userID {
		t.Fatalf("не привязан: %+v", emp)
	}
	sent := e.fake.Sent()
	if len(sent) != 1 || sent[0].Text != textIntro || sent[0].Buttons[0][0].Payload != payAddressTy {
		t.Fatalf("знакомство: %+v", sent)
	}
	lastRowIsHuman(t, sent[0])

	// До ответа на согласие поллер ничего не шлёт.
	if n, err := e.svc.Scheduler.Tick(ctx); err != nil || n != 0 {
		t.Fatalf("такт до согласия: %d %v", n, err)
	}

	e.press(payAddressTy)
	a := lastAnswer(t, e.fake)
	if a.Answer.Message == nil || len(a.Answer.Message.Buttons) != 0 || !strings.Contains(a.Answer.Message.Text, "на «ты»") || !strings.Contains(a.Answer.Message.Text, "Пётр") {
		t.Fatalf("правка знакомства: %+v", a.Answer.Message)
	}
	sent = e.fake.Sent()
	if sent[len(sent)-1].Text != textConsent {
		t.Fatalf("после знакомства — согласие: %+v", sent[len(sent)-1])
	}

	e.fake.Reset()
	e.press(payConsentYes)
	e.svc.Bot.Wait()
	emp = e.employee(t, c.Employee.ID)
	if emp.Consent == nil || *emp.Consent != domain.ConsentGiven || *emp.AddressForm != domain.AddressTy {
		t.Fatalf("согласие: %+v", emp)
	}
	// Блок D−3 — диалог: вопрос держит поток (api_contract v4, «Исполнение блока»).
	sent = e.fake.Sent()
	if len(sent) != 1 || sent[0].Text != "Возьми паспорт, Пётр" {
		t.Fatalf("пакет D−3 на «ты»: %+v", sent)
	}
	docs := eventByTitle(t, e.events(t, c.Employee.ID), "Документы")
	e.press(preStepButton + docs.ID.String() + ":0")
	sent = e.fake.Sent()
	if len(sent) != 2 || sent[1].Text != "Три факта" {
		t.Fatalf("после ответа — следующая реплика: %+v", sent)
	}
	// Повторный такт ничего не дублирует.
	if n, _ := e.svc.Scheduler.Tick(ctx); n != 0 {
		t.Fatalf("повтор такта отправил %d", n)
	}
}

func TestBotUnknownInvite(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateBotStarted, UserID: userID, ChatID: chatID, Payload: "inv_nope"})
	sent := e.fake.Sent()
	if len(sent) != 1 || sent[0].Text != textInviteBroken || !sent[0].Buttons[0][0].Contact {
		t.Fatalf("%+v", sent)
	}

	// Просроченный токен — тот же ответ.
	c := e.launch(t, date(2026, 11, 3))
	*e.clock = msk(2027, 1, 1, 12, 0)
	e.fake.Reset()
	e.svc.Bot.Handle(context.Background(), max.Update{Type: max.UpdateBotStarted, UserID: userID, ChatID: chatID, Payload: e.token(t, c)})
	if sent := e.fake.Sent(); len(sent) != 1 || sent[0].Text != textInviteBroken {
		t.Fatalf("просроченный: %+v", sent)
	}
}

// Отказ от согласия: опросы снимаются, обычные шаги идут.
func TestBotConsentDeclined(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, false)

	evs := e.events(t, c.Employee.ID)
	if ev := eventByTitle(t, evs, "Итог дня"); ev.Status != domain.EventSkipped {
		t.Fatalf("опрос после отказа: %s", ev.Status)
	}
	if ev := eventByTitle(t, evs, "Документы"); ev.Status != domain.EventSent {
		t.Fatalf("обычный шаг после отказа: %s", ev.Status)
	}
	// Шаг только для Лужников новичку GTP не развёрнут.
	for _, ev := range evs {
		if ev.Title == "Только Лужники" {
			t.Fatal("шаг чужой площадки в таймлайне")
		}
	}
}

// Живой опрос: шкала 1–10, порог 8 → причины, свой текст, пропуск, итог, риск.
func TestLiveSurveyThreshold(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()

	e.fake.Reset()
	if _, err := e.svc.Newcomers.TimeTravel(ctx, c.Employee.ID, 14); err != nil {
		t.Fatal(err)
	}
	evs := e.events(t, c.Employee.ID)
	check := eventByTitle(t, evs, "Чек двух недель")
	if check.Status != domain.EventSent || check.Text == nil || !strings.HasPrefix(*check.Text, testPrefix) {
		t.Fatalf("машина времени: %+v", check)
	}
	if card, _ := e.svc.Newcomers.Get(ctx, c.Employee.ID); card.Day != DayNumber(c.Employee.StartDate, *e.clock, gtp) {
		t.Fatalf("машина времени не меняет day: %d", card.Day)
	}
	for _, m := range e.fake.Sent() {
		if !strings.HasPrefix(m.Text, testPrefix) {
			t.Fatalf("без пометки [ТЕСТ]: %q", m.Text)
		}
	}
	id := check.ID.String()

	// 6 < 8 → экран причин в том же сообщении.
	e.press(preAnswer + id + ":0:6")
	a := lastAnswer(t, e.fake)
	if a.Answer.Notification != "Записал" || !strings.Contains(a.Answer.Message.Text, "Что именно не так?") {
		t.Fatalf("экран причин: %+v", a.Answer)
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskYellow || *emp.LastPulse != 6 {
		t.Fatalf("риск после 6/10: %+v", emp)
	}
	e.press(preReason + id + ":0:1") // «Люди и общение»
	if a := lastAnswer(t, e.fake); !strings.Contains(a.Answer.Message.Text, "Вопрос 2 из 3") || !strings.Contains(a.Answer.Message.Text, "6/10 (Люди и общение)") {
		t.Fatalf("после причины: %q", a.Answer.Message.Text)
	}

	// Повторный тап по старому вопросу ничего не меняет.
	e.press(preAnswer + id + ":0:9")
	if a := lastAnswer(t, e.fake); a.Answer.Message != nil {
		t.Fatalf("старый вопрос: %+v", a.Answer)
	}

	// Вопрос 2: оценка текстом «пять» → причины → «Напишу сам(а)» → свой текст.
	e.say("пять")
	e.press(preWrite + id + ":1")
	e.say("Руководитель *всё время* на совещаниях")
	edits := e.fake.Edits()
	if len(edits) < 2 || !strings.Contains(edits[len(edits)-1].Message.Text, "Вопрос 3 из 3") {
		t.Fatalf("правки после текста: %+v", edits)
	}
	if strings.Contains(edits[len(edits)-1].Message.Text, "совещаниях") {
		t.Fatal("текст новичка попал в разметку сообщения")
	}

	// Вопрос 3: пропуск → итог без клавиатуры.
	e.press(preSkip + id + ":2")
	a = lastAnswer(t, e.fake)
	if len(a.Answer.Message.Buttons) != 0 || !strings.Contains(a.Answer.Message.Text, textSurveyDone) {
		t.Fatalf("итог: %+v", a.Answer.Message)
	}
	ev := eventByTitle(t, e.events(t, c.Employee.ID), "Чек двух недель")
	if ev.Status != domain.EventAnswered || ev.Answer == nil || *ev.Answer != "6/10 (Люди и общение); 5/10 (причина своими словами); —" {
		t.Fatalf("событие: %s %q", ev.Status, deref(ev.Answer))
	}
	answers, _ := e.repos.Events.Answers(ctx, ev.ID)
	if len(answers) != 3 || *answers[0].Value != 6 || answers[1].Text != "Руководитель *всё время* на совещаниях" || !answers[2].Skipped || answers[0].Scale != domain.Scale10 {
		t.Fatalf("ответы: %+v", answers)
	}
}

// «Напомнить завтра»: событие возвращается в расписание на следующий рабочий день.
func TestSurveyRemindTomorrow(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 18, 30)) // D0 вечер, вторник
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	// Подключился вечером: шаг 10:00 (вопрос) держит поток, пульс 18:00 — после ответа.
	first := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	e.press(preStepButton + first.ID.String() + ":0")
	pulse := eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	if pulse.Status != domain.EventSent {
		t.Fatalf("пульс D0 18:00: %s", pulse.Status)
	}
	e.press(preRemind + pulse.ID.String())
	pulse = eventByTitle(t, e.events(t, c.Employee.ID), "Итог дня")
	if pulse.Status != domain.EventScheduled || !pulse.FireAt.Equal(msk(2026, 11, 4, 18, 0)) {
		t.Fatalf("перенос: %s %v", pulse.Status, pulse.FireAt)
	}
}

// Свободный текст с F7: в rag не идёт (rag не подключён), ответ по правилу трёх шагов, флаг в
// карточку, запись намерения.
func TestFreeTextFlagsWithoutRAG(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	e.connect(t, c, domain.AddressVy, true)
	e.fake.Reset()
	e.say("думаю уволиться")
	e.svc.Bot.Wait()
	sent := e.fake.Sent()
	if len(sent) != 1 || !strings.HasPrefix(sent[0].Text, "Слышу вас, Пётр.") {
		t.Fatalf("%+v", sent)
	}
	if emp := e.employee(t, c.Employee.ID); emp.Risk != domain.RiskRed {
		t.Fatalf("флаг F7: %+v", emp)
	}
	e.press(payHuman)
	if a := lastAnswer(t, e.fake); a.Answer.Notification != "Передаю" {
		t.Fatalf("%+v", a)
	}
}

// Без базы бот остаётся эхом: транспорт проверяется до сценариев.
func TestBotEchoWithoutDB(t *testing.T) {
	f := max.NewFake()
	b := NewBotService(f, nil, nil, nil)
	b.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, ChatID: 100, UserID: 7, Text: "привет"})
	b.Handle(context.Background(), max.Update{Type: max.UpdateMessageCreated, ChatID: 100, UserID: 8, FromBot: true, Text: "я бот"})
	if sent := f.Sent(); len(sent) != 1 || sent[0].Text != EchoReply {
		t.Fatalf("%+v", sent)
	}
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

type fakeRAG struct{ err error }

func (r fakeRAG) Health(context.Context, time.Duration) error { return r.err }

func TestHealthCheck(t *testing.T) {
	down := errors.New("down")
	cases := []struct {
		db      Pinger
		rag     RAGHealth
		want    Readiness
		comment string
	}{
		{fakePinger{}, fakeRAG{}, Readiness{DB: true, RAG: true}, "всё живо"},
		{fakePinger{down}, fakeRAG{}, Readiness{RAG: true}, "база лежит"},
		{fakePinger{}, fakeRAG{down}, Readiness{DB: true}, "rag лежит"},
		{nil, nil, Readiness{}, "ничего не подключено"},
	}
	for _, c := range cases {
		if got := NewHealthService(c.db, c.rag, time.Second).Check(context.Background()); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.comment, got, c.want)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
