package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"main/config"
	"main/domain"
)

// Итерация 24.10, агент A: пояса и окно 8–18 (FEATURE_TZ), наджи по умолчанию
// (FEATURE_NUDGES_DEFAULT), поздний вход (FEATURE_CATCHUP), праздники. Даты фиксированы.

const tzEkb = "Asia/Yekaterinburg" // UTC+5

func strp(s string) *string { return &s }

func tzScheduler(on bool) *SchedulerService {
	s := NewSchedulerService(nil, nil, time.Now, 0)
	s.SetFeatures(config.Features{TZ: on})
	return s
}

// UTC+5, шаг 10:00 → 05:00Z; окно 8–18; без двойного смещения.
func TestSiteForEmployeeTZ(t *testing.T) {
	s := tzScheduler(true)
	e := &domain.Employee{Site: domain.SiteGTP, TZ: strp(tzEkb)}
	site := s.SiteFor(e)
	if site.TZ != tzEkb || site.DayStart[time.Monday] != 8*60 || site.DayEnd[time.Friday] != 18*60 {
		t.Fatalf("правила: %s %d %d", site.TZ, site.DayStart[time.Monday], site.DayEnd[time.Friday])
	}
	at := FireAt(date(2026, 11, 3), 0, "10:00", site, true)
	if want := time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC); !at.Equal(want) {
		t.Fatalf("10:00 UTC+5 → %v, want %v", at.UTC(), want)
	}
	// нет двойного смещения: момент в UTC и обратно в пояс новичка — те же 10:00
	if l := at.UTC().In(site.Loc); l.Hour() != 10 || l.Minute() != 0 {
		t.Fatalf("обратно в пояс: %v", l)
	}
	if _, off := at.In(site.Loc).Zone(); off != 5*3600 {
		t.Fatalf("смещение %d", off)
	}
	// «какой сегодня день» — по часам новичка: 23:30 МСК 2 ноября = 01:30 3 ноября в Екб → D0
	if d := DayNumber(date(2026, 11, 3), msk(2026, 11, 2, 23, 30), site); d != 0 {
		t.Fatalf("день по поясу новичка %d", d)
	}
	// рабочее окно 8–18: 7:30 → 8:00, 18:30 → 18:00 в пределах дня (шаг work_hours_only)
	if at := FireAt(date(2026, 11, 3), 0, "07:30", site, true); at.In(site.Loc).Hour() != 8 {
		t.Fatalf("7:30 → %v", at.In(site.Loc))
	}
	// пояс новичка не задан — пояс площадки (Москва), 10:00 → 07:00Z
	if at := FireAt(date(2026, 11, 3), 0, "10:00", s.SiteFor(&domain.Employee{Site: domain.SiteGTP}), true); !at.Equal(time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("пояс площадки: %v", at.UTC())
	}
	// неизвестный пояс — пояс площадки, не паника
	if got := s.SiteFor(&domain.Employee{Site: domain.SiteGTP, TZ: strp("Mars/Olympus")}).TZ; got != domain.TZMoscow {
		t.Fatalf("неизвестный пояс → %s", got)
	}
}

// Пятница 18:30 местного → понедельник 8:00 (напоминания, отложенный ручной надж).
func TestNextWorkMomentFridayEvening(t *testing.T) {
	site := tzScheduler(true).SiteFor(&domain.Employee{Site: domain.SiteGTP, TZ: strp(tzEkb)})
	fri := time.Date(2026, 11, 6, 18, 30, 0, 0, site.Loc)
	got := NextWorkMoment(fri, site)
	if want := time.Date(2026, 11, 9, 8, 0, 0, 0, site.Loc); !got.Equal(want) {
		t.Fatalf("пятница 18:30 → %v, want %v", got.In(site.Loc), want)
	}
	if InWorkHours(fri, site) || !InWorkHours(got, site) {
		t.Fatal("окно")
	}
	in := time.Date(2026, 11, 6, 17, 59, 0, 0, site.Loc)
	if !NextWorkMoment(in, site).Equal(in) {
		t.Fatal("в окне — сразу")
	}
}

// Без флага — прежние правила площадки: Москва, пн–чт 9–18, пт 9–17, без праздников; Employee.TZ
// не учитывается.
func TestSiteForWithoutFlag(t *testing.T) {
	s := tzScheduler(false)
	site := s.SiteFor(&domain.Employee{Site: domain.SiteGTP, TZ: strp(tzEkb)})
	if site.TZ != domain.TZMoscow || site.DayStart[time.Monday] != 9*60 || site.DayEnd[time.Friday] != 17*60 || site.Holidays != nil {
		t.Fatalf("без флага: %+v", site)
	}
	// 4 ноября (среда, праздник) без флага — рабочий
	if at := FireAt(date(2026, 11, 3), 1, "10:00", site, true); !at.Equal(msk(2026, 11, 4, 10, 0)) {
		t.Fatalf("без праздников: %v", at)
	}
}

// Праздники РФ (с флагом): шаг на 4 ноября → 5 ноября; январские каникулы → 12 января.
func TestHolidaysShiftStep(t *testing.T) {
	site := tzScheduler(true).SiteFor(&domain.Employee{Site: domain.SiteGTP})
	if at := FireAt(date(2026, 11, 3), 1, "10:00", site, true); !at.Equal(msk(2026, 11, 5, 10, 0)) {
		t.Fatalf("4 ноября → %v", at)
	}
	if at := FireAt(date(2025, 12, 29), 3, "10:00", site, true); !at.Equal(msk(2026, 1, 12, 10, 0)) {
		t.Fatalf("1 января → %v", at)
	}
	if site.IsWorkDate(time.Date(2026, 11, 4, 12, 0, 0, 0, site.Loc)) {
		t.Fatal("4 ноября рабочий")
	}
	if NextWorkMoment(time.Date(2026, 11, 3, 19, 0, 0, 0, site.Loc), site) != time.Date(2026, 11, 5, 8, 0, 0, 0, site.Loc) {
		t.Fatal("напоминание после праздника")
	}
}

func TestQuestionsWord(t *testing.T) {
	for n, want := range map[int]string{1: "вопрос", 2: "вопроса", 4: "вопроса", 5: "вопросов", 11: "вопросов", 12: "вопросов", 21: "вопрос", 22: "вопроса"} {
		if got := questionsWord(n); got != want {
			t.Fatalf("%d: %s", n, got)
		}
	}
	vy := domain.AddressVy
	ty := domain.AddressTy
	if h := nudgeHead(&domain.Employee{AddressForm: &vy}, 2); h != "🔔 У вас 2 вопроса без ответа — вот первый. Займёт 10 секунд." {
		t.Fatal(h)
	}
	if h := nudgeHead(&domain.Employee{AddressForm: &ty}, 3); !strings.HasPrefix(h, "🔔 У тебя 3 вопроса") {
		t.Fatal(h)
	}
}

// --- база (TEST_DATABASE_URL) ---

func (e *env) features(f config.Features) {
	e.svc.Scheduler.SetFeatures(f)
	e.svc.Scheduler.ResetHolidays()
}

func (e *env) tick(t *testing.T, at time.Time) int {
	t.Helper()
	*e.clock = at
	n, err := e.svc.Scheduler.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Пояс новичка через PATCH: недоставленные события переезжают, карточка отдаёт tz и site_tz.
func TestPatchTZReschedules(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.features(config.Features{TZ: true})
	c := e.launch(t, date(2026, 11, 3))
	ctx := context.Background()
	d0 := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	if !d0.FireAt.Equal(time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("D0 по Москве: %v", d0.FireAt.UTC())
	}
	card, err := e.svc.Newcomers.Patch(ctx, c.Employee.ID, nil, strp(tzEkb))
	if err != nil {
		t.Fatal(err)
	}
	if card.TZ != tzEkb || card.SiteTZ != domain.TZMoscow {
		t.Fatalf("карточка: %s / %s", card.TZ, card.SiteTZ)
	}
	d0 = eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём")
	if !d0.FireAt.Equal(time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("D0 по Екатеринбургу: %v", d0.FireAt.UTC())
	}
	// дата выхода та же
	if !e.employee(t, c.Employee.ID).StartDate.Equal(date(2026, 11, 3)) {
		t.Fatal("дата выхода поменялась")
	}
	// снять пояс — снова Москва
	if card, err = e.svc.Newcomers.Patch(ctx, c.Employee.ID, nil, strp("")); err != nil || card.TZ != domain.TZMoscow {
		t.Fatalf("снять пояс: %v %v", card, err)
	}
	if d0 = eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём"); !d0.FireAt.Equal(time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("D0 снова по Москве: %v", d0.FireAt.UTC())
	}
}

// Без FEATURE_TZ пояс сохраняется, но расписание прежнее.
func TestPatchTZWithoutFlag(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c := e.launch(t, date(2026, 11, 3))
	card, err := e.svc.Newcomers.Patch(context.Background(), c.Employee.ID, nil, strp(tzEkb))
	if err != nil {
		t.Fatal(err)
	}
	if card.TZ != domain.TZMoscow || card.Employee.TZ == nil || *card.Employee.TZ != tzEkb {
		t.Fatalf("карточка: %s %v", card.TZ, card.Employee.TZ)
	}
	if d0 := eventByTitle(t, e.events(t, c.Employee.ID), "С первым днём"); !d0.FireAt.Equal(msk(2026, 11, 3, 10, 0)) {
		t.Fatalf("без флага: %v", d0.FireAt)
	}
}

// Наджи по умолчанию: 4 ч и 24 ч, не больше одного в день, общее при двух вопросах, после
// второго — «не отвечает», ответ снимает статус.
func TestNudgesDefault(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 9, 0))
	e.features(config.Features{NudgesDefault: true})
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Доступы", "Пропуск выдали?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"}),
		txt(0, "11:00", "Наставник", "С наставником познакомились?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Пока нет"}),
	})
	e.connect(t, c, domain.AddressVy, true)
	ctx := context.Background()
	id := c.Employee.ID
	e.tick(t, msk(2026, 11, 3, 10, 0))
	e.tick(t, msk(2026, 11, 3, 11, 0))
	if q := eventByTitle(t, e.events(t, id), "Наставник"); q.Status != domain.EventSent {
		t.Fatalf("второй вопрос: %s", q.Status)
	}
	e.fake.Reset()
	if n := e.tick(t, msk(2026, 11, 3, 13, 59)); n != 0 || len(e.fake.Sent()) != 0 {
		t.Fatalf("раньше 4 ч: %d", n)
	}
	// 14:00 — первый вопрос созрел, висят два: одно общее напоминание
	if n := e.tick(t, msk(2026, 11, 3, 15, 0)); n != 1 {
		t.Fatalf("напоминаний %d", n)
	}
	m := lastSent(t, e.fake)
	if !strings.HasPrefix(m.Text, "🔔 У вас 2 вопроса без ответа — вот первый") || !strings.Contains(m.Text, "Пропуск выдали?") || findPayload(m, "Да") == "" {
		t.Fatalf("общее напоминание: %q %v", m.Text, labels(m))
	}
	nudges, _ := e.repos.Nudges.ListByEmployee(ctx, id)
	if len(nudges) != 1 || nudges[0].Kind != domain.NudgeAuto || nudges[0].N != 1 || nudges[0].MessageID == "" {
		t.Fatalf("журнал: %+v", nudges)
	}
	// в тот же день — больше ни одного
	e.fake.Reset()
	if n := e.tick(t, msk(2026, 11, 3, 17, 30)); n != 0 {
		t.Fatalf("второй в день: %d", n)
	}
	// ночью — нет
	if n := e.tick(t, msk(2026, 11, 4, 7, 0)); n != 0 {
		t.Fatalf("ночью: %d", n)
	}
	// 24 ч у первого вопроса: второе напоминание → «не отвечает»
	if n := e.tick(t, msk(2026, 11, 4, 10, 30)); n != 1 {
		t.Fatalf("второе: %d", n)
	}
	emp := e.employee(t, id)
	if emp.UnresponsiveSince == nil {
		t.Fatal("нет статуса «не отвечает»")
	}
	card, _ := e.svc.Newcomers.Get(ctx, id)
	if card.Unresponsive == nil {
		t.Fatal("карточка без «не отвечает»")
	}
	// дальше автонаджи выключены
	e.fake.Reset()
	if n := e.tick(t, msk(2026, 11, 5, 11, 0)); n != 0 {
		t.Fatalf("после «не отвечает»: %d", n)
	}
	// ответ снимает статус
	*e.clock = msk(2026, 11, 5, 12, 0)
	q := eventByTitle(t, e.events(t, id), "Наставник")
	e.press(preStepButton + q.ID.String() + ":0")
	if card, _ = e.svc.Newcomers.Get(ctx, id); card.Unresponsive != nil {
		t.Fatal("ответил — статус остался в карточке")
	}
	e.tick(t, msk(2026, 11, 5, 12, 1))
	if emp = e.employee(t, id); emp.UnresponsiveSince != nil {
		t.Fatal("ответил — статус не снят")
	}
}

// Ручной надж: в рабочее окно — сразу (manual), в тихие часы — на начало окна по поясу новичка.
func TestNudgeNow(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 3, 9, 0))
	e.features(config.Features{TZ: true})
	c, _ := e.launchSteps(t, flowStart, []domain.ScenarioStep{
		txt(0, "10:00", "Доступы", "Пропуск выдали?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"}),
	})
	ctx := context.Background()
	id := c.Employee.ID
	if _, err := e.svc.Newcomers.Nudge(ctx, id, [16]byte{}); err == nil {
		t.Fatal("не подключён — должно быть 409")
	}
	e.connect(t, c, domain.AddressTy, true)
	if _, err := e.svc.Newcomers.Nudge(ctx, id, [16]byte{}); err == nil {
		t.Fatal("вопросов нет — должно быть 409")
	}
	e.tick(t, msk(2026, 11, 3, 10, 0))
	*e.clock = msk(2026, 11, 3, 10, 30)
	e.fake.Reset()
	res, err := e.svc.Newcomers.Nudge(ctx, id, [16]byte{})
	if err != nil || !res.Sent || res.Nudge.Kind != domain.NudgeManual || res.Nudge.N != 1 {
		t.Fatalf("сразу: %+v %v", res, err)
	}
	if m := lastSent(t, e.fake); !strings.HasPrefix(m.Text, textNudgeOne) || findPayload(m, "Нет") == "" {
		t.Fatalf("текст: %q", m.Text)
	}
	// тихие часы новичка (Екатеринбург): 17:30 МСК = 19:30 Екб → завтра 8:00 Екб = 06:00 МСК
	if _, err := e.svc.Newcomers.Patch(ctx, id, nil, strp(tzEkb)); err != nil {
		t.Fatal(err)
	}
	*e.clock = msk(2026, 11, 3, 17, 30)
	e.fake.Reset()
	res, err = e.svc.Newcomers.Nudge(ctx, id, [16]byte{})
	if err != nil || res.Sent || res.Message != "Отправится в 08:00 по времени новичка" || !res.At.Equal(msk(2026, 11, 5, 6, 0)) {
		// 4 ноября — праздник: окно — 5 ноября
		t.Fatalf("отложенный: %+v %v", res, err)
	}
	if len(e.fake.Sent()) != 0 {
		t.Fatal("ушло ночью")
	}
	// повторное нажатие — тот же отложенный
	if again, _ := e.svc.Newcomers.Nudge(ctx, id, [16]byte{}); again == nil || again.Nudge.ID != res.Nudge.ID {
		t.Fatalf("второй отложенный: %+v", again)
	}
	if n := e.tick(t, msk(2026, 11, 5, 5, 59)); n != 0 {
		t.Fatal("раньше окна")
	}
	if n := e.tick(t, msk(2026, 11, 5, 6, 0)); n != 1 {
		t.Fatalf("в начале окна: %d", n)
	}
	items, tz, err := e.svc.Newcomers.Nudges(ctx, id)
	if err != nil || len(items) != 2 || tz != tzEkb || !items[1].Delivered || items[1].Planned || items[1].EventTitle != "Доступы" {
		t.Fatalf("история: %+v %s %v", items, tz, err)
	}
}

// --- поздний вход ---

// lateSteps — сценарий для позднего входа: материалы, вопросы и опросы прошлых дней.
func lateSteps() []domain.ScenarioStep {
	docs := txt(-3, "10:00", "Документы", "Возьмите паспорт")
	docs.Required = true
	access := txt(0, "10:00", "Доступы", "Пропуск выдали?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"})
	access.Required = true
	clear := txt(1, "10:00", "Понятно", "Документы понятны?", domain.StepButton{Label: "Да"}, domain.StepButton{Label: "Нет"})
	clear.Required = true
	return []domain.ScenarioStep{
		docs,
		txt(-3, "10:00", "Факты", "Три факта"),
		access,
		{DayOffset: 0, Time: "18:00", Kind: domain.KindSurvey, Title: "Итог дня", TextVy: "Как день?",
			Survey: &domain.Survey{Scale: domain.Scale5, Threshold: 3, Questions: []domain.SurveyQuestion{{Text: "Как прошёл день?"}}}},
		clear,
		{DayOffset: 2, Time: "10:00", Kind: domain.KindSurvey, Title: "Пульс", TextVy: "Пульс",
			Survey: &domain.Survey{Scale: domain.Scale5, Threshold: 3, Questions: []domain.SurveyQuestion{{Text: "Как вы?", Required: true}}}},
		txt(5, "10:00", "Сегодня", "Сегодня — обход цеха"),
		txt(7, "10:00", "Потом", "Через два дня"),
	}
}

// Подключение на D+5: сводка «Что вы пропустили», первый обязательный вопрос в той же сессии,
// остальные — по одному в день, прогресс и «Догнал ✓».
func TestCatchupD5(t *testing.T) {
	// выход в среду 21 октября; D+5 — понедельник 26 октября (праздников рядом нет)
	e := newEnv(t, msk(2026, 10, 26, 12, 0))
	e.features(config.Features{Catchup: true})
	c, _ := e.launchSteps(t, date(2026, 10, 21), lateSteps())
	ctx := context.Background()
	id := c.Employee.ID
	e.connect(t, c, domain.AddressVy, true)

	texts := sentTexts(e.fake)
	iSum, iQ := -1, -1
	for i, s := range texts {
		if strings.HasPrefix(s, textMissedHead) {
			iSum = i
		}
		if strings.Contains(s, "Пропуск выдали?") && iQ < 0 {
			iQ = i
		}
	}
	if iSum < 0 || iQ < iSum || !strings.Contains(texts[iSum], "Возьмите паспорт") || strings.Contains(texts[iSum], "Три факта") {
		t.Fatalf("сводка и вопрос: %q", texts)
	}
	evs := e.events(t, id)
	check := func(title, status string, catchup bool) domain.ScheduledEvent {
		t.Helper()
		ev := eventByTitle(t, evs, title)
		if ev.Status != status || ev.Catchup != catchup {
			t.Fatalf("%s: %s catchup=%v", title, ev.Status, ev.Catchup)
		}
		return ev
	}
	check("Документы", domain.EventSent, true)
	if f := check("Факты", domain.EventSkipped, false); *f.Answer != skipMissed {
		t.Fatal(*f.Answer)
	}
	check("Доступы", domain.EventSent, true)
	check("Итог дня", domain.EventSkipped, false)
	if ev := check("Понятно", domain.EventScheduled, true); !ev.FireAt.Equal(msk(2026, 10, 27, 9, 0)) {
		t.Fatalf("второй блок: %v", ev.FireAt)
	}
	if ev := check("Пульс", domain.EventScheduled, true); !ev.FireAt.Equal(msk(2026, 10, 28, 9, 0)) {
		t.Fatalf("третий блок: %v", ev.FireAt)
	}
	emp := e.employee(t, id)
	if emp.CatchupStartedAt == nil || emp.CatchupDoneAt != nil {
		t.Fatal("очередь не отмечена")
	}
	card, _ := e.svc.Newcomers.Get(ctx, id)
	if card.Catchup == nil || card.Catchup.Done != 1 || card.Catchup.Total != 4 || card.Catchup.Finished {
		t.Fatalf("прогресс: %+v", card.Catchup)
	}
	// обычный шаг сегодняшнего дня — следующим тактом
	e.tick(t, msk(2026, 10, 26, 12, 1))
	check = func(title, status string, catchup bool) domain.ScheduledEvent {
		t.Helper()
		ev := eventByTitle(t, e.events(t, id), title)
		if ev.Status != status || ev.Catchup != catchup {
			t.Fatalf("%s: %s catchup=%v", title, ev.Status, ev.Catchup)
		}
		return ev
	}
	check("Сегодня", domain.EventSent, false)
	q := check("Доступы", domain.EventSent, true)
	e.press(preStepButton + q.ID.String() + ":0")

	// 27 октября не было такта: 28-го созрели оба блока — уходит один (вопрос держит такт),
	// следующий такт переносит второй на завтра: не больше одного догоняющего блока в день
	e.fake.Reset()
	e.tick(t, msk(2026, 10, 28, 9, 5))
	e.tick(t, msk(2026, 10, 28, 9, 6))
	check("Понятно", domain.EventSent, true)
	if p := check("Пульс", domain.EventScheduled, true); !p.FireAt.Equal(msk(2026, 10, 29, 9, 0)) {
		t.Fatalf("бюджет дня: %v", p.FireAt)
	}
	q = check("Понятно", domain.EventSent, true)
	e.press(preStepButton + q.ID.String() + ":0")
	e.tick(t, msk(2026, 10, 29, 9, 5))
	p := check("Пульс", domain.EventSent, true)
	e.press(preAnswer + p.ID.String() + ":0:4")
	e.svc.Bot.Wait()
	e.tick(t, msk(2026, 10, 29, 9, 10))
	if emp = e.employee(t, id); emp.CatchupDoneAt == nil {
		done, total, _ := e.repos.Events.CatchupProgress(ctx, id)
		t.Fatalf("не догнал: %d/%d", done, total)
	}
	if card, _ = e.svc.Newcomers.Get(ctx, id); !card.Catchup.Finished || card.Catchup.Done != 4 {
		t.Fatalf("финиш: %+v", card.Catchup)
	}
}

// D+20: опросы старше 7 дней — «не задан (поздний вход)», вопросы — в очередь.
func TestCatchupD20(t *testing.T) {
	// выход во вторник 13 октября; D+20 — понедельник 2 ноября
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.features(config.Features{Catchup: true})
	c, _ := e.launchSteps(t, date(2026, 10, 13), lateSteps())
	e.connect(t, c, domain.AddressVy, true)
	evs := e.events(t, c.Employee.ID)
	for _, title := range []string{"Итог дня", "Пульс"} {
		if ev := eventByTitle(t, evs, title); ev.Status != domain.EventSkipped || *ev.Answer != skipLateSurvey || ev.Catchup {
			t.Fatalf("%s: %s %v", title, ev.Status, ev.Answer)
		}
	}
	if ev := eventByTitle(t, evs, "Сегодня"); ev.Status != domain.EventSkipped || *ev.Answer != skipMissed {
		t.Fatalf("необязательный прошлый шаг: %s", ev.Status)
	}
	if ev := eventByTitle(t, evs, "Понятно"); !ev.Catchup || !ev.FireAt.Equal(msk(2026, 11, 3, 9, 0)) {
		t.Fatalf("вопрос в очереди: %+v", ev)
	}
	if !hasText(e.fake, "Возьмите паспорт") {
		t.Fatal("нет сводки")
	}
	if done, total, _ := e.repos.Events.CatchupProgress(context.Background(), c.Employee.ID); done != 1 || total != 3 {
		t.Fatalf("прогресс %d/%d", done, total)
	}
}

// До дня выхода поведение прежнее: пакет D−3 уходит сразу, очереди нет.
func TestCatchupBeforeD0(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	e.features(config.Features{Catchup: true})
	c, _ := e.launchSteps(t, date(2026, 11, 3), lateSteps())
	e.connect(t, c, domain.AddressVy, true)
	evs := e.events(t, c.Employee.ID)
	for _, ev := range evs {
		if ev.Catchup {
			t.Fatalf("догоняющее до D0: %s", ev.Title)
		}
	}
	if ev := eventByTitle(t, evs, "Документы"); ev.Status != domain.EventSent || ev.Answer != nil {
		t.Fatalf("D−3: %s %v", ev.Status, ev.Answer)
	}
	if e.employee(t, c.Employee.ID).CatchupStartedAt != nil {
		t.Fatal("очередь до D0")
	}
}

// Без флага — прежний SkipMissed: прошлые шаги пропущены, сводки нет.
func TestCatchupFlagOff(t *testing.T) {
	e := newEnv(t, msk(2026, 11, 2, 12, 0))
	c, _ := e.launchSteps(t, date(2026, 10, 28), lateSteps())
	e.connect(t, c, domain.AddressVy, true)
	evs := e.events(t, c.Employee.ID)
	for _, title := range []string{"Документы", "Доступы", "Понятно", "Пульс", "Итог дня"} {
		if ev := eventByTitle(t, evs, title); ev.Status != domain.EventSkipped || *ev.Answer != skipMissed || ev.Catchup {
			t.Fatalf("%s: %s %v", title, ev.Status, ev.Answer)
		}
	}
	if hasText(e.fake, textMissedHead) {
		t.Fatal("сводка без флага")
	}
	card, _ := e.svc.Newcomers.Get(context.Background(), c.Employee.ID)
	if card.Catchup != nil {
		t.Fatal("прогресс без флага")
	}
}
