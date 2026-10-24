package services

import (
	"context"
	"strings"
	"sync"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// SchedulerService — таймлайн: развёртка шагов версии в scheduled_events, расчёт fire_at,
// поллер созревших событий, машина времени. Отправка идёт через max.Client — его очередь держит
// ≤ 2 сообщения/с на чат и паузу на 429.
type SchedulerService struct {
	repos   *repositories.Repos
	max     max.Client // nil — бот выключен: события копятся, отправки нет
	now     func() time.Time
	packGap time.Duration
	sleep   func(ctx context.Context, d time.Duration) error
	media   MediaAttacher  // nil — медиатеки нет: шаги уходят без вложений (branch_media.go)
	tickets *TicketService // задача HR, если личного файла новичка нет (branch_input.go)

	// features — FEATURE_TZ (SiteFor), FEATURE_NUDGES_DEFAULT (scheduler_nudges.go),
	// FEATURE_CATCHUP (scheduler_catchup.go), FEATURE_REPLIES (надж реплаем на вопрос).
	features config.Features
	hol      holidayCache // праздники для SiteFor (scheduler_tz.go)

	mu sync.Mutex // один такт отправки за раз в процессе; между процессами — SKIP LOCKED
}

// SetFeatures — фича-флаги итерации 24.10 (NewServices передаёт config.Features).
func (s *SchedulerService) SetFeatures(f config.Features) { s.features = f }

func NewSchedulerService(r *repositories.Repos, c max.Client, now func() time.Time, packGap time.Duration) *SchedulerService {
	return &SchedulerService{repos: r, max: c, now: now, packGap: packGap, sleep: sleepCtx}
}

// Причины в answer у пропущенных событий: их видит HR в таймлайне.
const (
	skipNoConsent  = "пропущено: нет согласия на опросы"
	skipMissed     = "пропущено: подключился позже даты шага"
	skipAddedLater = "пропущено: шаг добавлен после своей даты"
	failNoStep     = "шага нет в версии сценария"
)

// BuildEvents — события версии v для новичка: шаги его площадки, fire_at по календарю. skip —
// шаги, которые уже доставлены (пересборка под новую версию); их не повторяем.
func (s *SchedulerService) BuildEvents(e *domain.Employee, v *domain.ScenarioVersion, skip map[uuid.UUID]bool) []domain.ScheduledEvent {
	site := s.SiteFor(e)
	blocks := blockKeys(v.Steps)
	var out []domain.ScheduledEvent
	for _, st := range v.Steps {
		if !st.AppliesTo(e.Site) || skip[st.ID] {
			continue
		}
		out = append(out, domain.ScheduledEvent{
			ID:              domain.EventID(e.ID, st.ID, v.Number),
			EmployeeID:      e.ID,
			StepID:          st.ID,
			ScenarioVersion: v.Number,
			Position:        st.Position,
			DayOffset:       st.DayOffset,
			Title:           st.Title,
			Kind:            st.Kind,
			FireAt:          FireAt(e.StartDate, st.DayOffset, st.Time, site, st.WorkHoursOnly),
			Status:          domain.EventScheduled,
			Block:           blocks[st.ID],
		})
	}
	return out
}

// Reschedule — дата выхода или пояс новичка поменялись: пересчитать fire_at всех неотправленных
// событий. Строки те же (event_id не меняется), поэтому дублей нет; отправленные не трогаем.
// Догоняющая очередь (Catchup) живёт по своему расписанию — её не пересчитываем.
func (s *SchedulerService) Reschedule(ctx context.Context, tx *repositories.Repos, e *domain.Employee) error {
	v, err := tx.Scenarios.Version(ctx, e.ScenarioID, e.ScenarioVersion)
	if err != nil || v == nil {
		return err
	}
	events, err := tx.Events.ScheduledOf(ctx, e.ID)
	if err != nil {
		return err
	}
	site := s.SiteFor(e)
	for i := range events {
		if events[i].Catchup {
			continue
		}
		st, ok := v.Step(events[i].StepID)
		if !ok {
			continue
		}
		events[i].FireAt = FireAt(e.StartDate, st.DayOffset, st.Time, site, st.WorkHoursOnly)
		if err := tx.Events.Save(ctx, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

// Run — поллер: раз в interval отправляет созревшие события. Возвращается по отмене ctx.
func (s *SchedulerService) Run(ctx context.Context, interval time.Duration) {
	logger.Info("планировщик: запущен, такт %v", interval)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if n, err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("планировщик: такт не удался: %v", err)
		} else if n > 0 {
			logger.Info("планировщик: отправлено %d", n)
		}
		select {
		case <-ctx.Done():
			logger.Info("планировщик: остановлен")
			return
		case <-t.C:
		}
	}
}

// Tick — один такт: все созревшие события подключённых новичков.
func (s *SchedulerService) Tick(ctx context.Context) (int, error) {
	n, err := s.dispatch(ctx, repositories.DueFilter{Now: s.now()}, false)
	if err != nil {
		return n, err
	}
	// Отложенные ручные наджи; FEATURE_NUDGES_DEFAULT — все автонапоминания одним проходом с
	// бюджетом диалога; FEATURE_CATCHUP — отметка «Догнал ✓» (scheduler_nudges.go,
	// scheduler_catchup.go). Без флагов и без ручных наджей — ничего не отправляет.
	it, err := s.tickIteration(ctx)
	n += it
	if err != nil || s.features.NudgesDefault {
		return n, err
	}
	r, err := s.remindNoAnswer(ctx)
	if err != nil {
		return n + r, err
	}
	sv, err := s.remindSurveys(ctx) // правило сценария r70 (scenario_rules_exec.go)
	return n + r + sv, err
}

// DispatchEmployee — созревшие события одного новичка сразу, не дожидаясь такта (после
// согласия: «затем события D−3»).
func (s *SchedulerService) DispatchEmployee(ctx context.Context, id uuid.UUID) (int, error) {
	return s.dispatch(ctx, repositories.DueFilter{Now: s.now(), EmployeeID: &id}, false)
}

// TimeTravel — машина времени: все неотправленные события по день day включительно, с
// пометкой [ТЕСТ]. Пауза пакета не нужна: HR ждёт ответа, лимит чата держит очередь MAX.
func (s *SchedulerService) TimeTravel(ctx context.Context, id uuid.UUID, day int) (int, error) {
	return s.dispatch(ctx, repositories.DueFilter{Now: s.now(), EmployeeID: &id, MaxDay: &day}, true)
}

// dispatch — забрать созревшие события под блокировкой и отправить по блокам (branch_flow.go:
// вопрос держит поток, «Дальше →», «Что вы пропустили»). Транзакция держится, пока идут
// отправки: так событие не уйдёт дважды, даже если такт и машина времени совпали.
func (s *SchedulerService) dispatch(ctx context.Context, f repositories.DueFilter, test bool) (int, error) {
	if s.max == nil || !s.repos.Enabled() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.EmployeeID == nil {
		// такт — по новичку в своей транзакции: молчащий новичок с очередью за вопросом не
		// съедает лимит выборки остальных, сбой одного не откатывает отправки другим
		ids, err := s.repos.Events.DueEmployees(ctx, f)
		if err != nil {
			return 0, err
		}
		sent := 0
		for _, id := range ids {
			one := f
			one.EmployeeID = &id
			n, err := s.dispatchLocked(ctx, one, test)
			sent += n
			if err != nil {
				return sent, err
			}
		}
		return sent, nil
	}
	return s.dispatchLocked(ctx, f, test)
}

// dispatchLocked — dispatch под s.mu.
func (s *SchedulerService) dispatchLocked(ctx context.Context, f repositories.DueFilter, test bool) (int, error) {
	start := s.now()
	sent := 0
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		events, err := tx.Events.LockDue(ctx, f)
		if err != nil {
			return err
		}
		var order []uuid.UUID
		byEmp := map[uuid.UUID][]domain.ScheduledEvent{}
		for _, ev := range events {
			if _, ok := byEmp[ev.EmployeeID]; !ok {
				order = append(order, ev.EmployeeID)
			}
			byEmp[ev.EmployeeID] = append(byEmp[ev.EmployeeID], ev)
		}
		for _, id := range order {
			paused := false
			n, err := s.runEmployee(ctx, tx, id, byEmp[id], test, start, &paused)
			sent += n
			if err != nil {
				return err
			}
		}
		return nil
	})
	return sent, err
}

// sendEvent — одно событие: пропустить, отправить или отметить сбой. Ошибка — только от БД;
// сбой MAX — это статус события, а не причина откатить весь такт.
// cont — шаг без вопроса, за которым в блоке есть ещё реплики: кнопка «Дальше →».
func (s *SchedulerService) sendEvent(ctx context.Context, tx *repositories.Repos, e *domain.Employee, v *domain.ScenarioVersion, ev *domain.ScheduledEvent, test bool, cont bool) (bool, error) {
	var step domain.ScenarioStep
	found := false
	if ev.Snapshot != nil {
		step, found = domain.ScenarioStep(*ev.Snapshot), true
	} else if v != nil {
		step, found = v.Step(ev.StepID)
	}
	switch {
	case !found:
		ev.Status, ev.Answer = domain.EventFailed, strPtr(failNoStep)
		return false, tx.Events.Save(ctx, ev)
	case step.HRTask:
		ev.Status, ev.Answer = domain.EventSkipped, strPtr(skipHRTask)
		return false, tx.Events.Save(ctx, ev)
	case coveredByOnboarding(e, step):
		ev.Status, ev.Answer = domain.EventSkipped, strPtr(skipOnboarding)
		return false, tx.Events.Save(ctx, ev)
	case step.FlagRequired != nil && *step.FlagRequired != "" && !flagOn(e, *step.FlagRequired):
		ev.Status, ev.Answer = domain.EventHeld, strPtr("ждёт отмашки: флаг "+*step.FlagRequired)
		return false, tx.Events.Save(ctx, ev)
	case ev.Kind == domain.KindSurvey && (e.Consent == nil || *e.Consent != domain.ConsentGiven):
		ev.Status, ev.Answer = domain.EventSkipped, strPtr(skipNoConsent)
		return false, tx.Events.Save(ctx, ev)
	}

	if test {
		ev.IsTest = true
	}
	to := recipientOf(e)
	var msg max.OutMessage
	if isSurvey(step) {
		if ev.Survey == nil {
			ev.Survey = &domain.SurveyState{Screen: domain.ScreenQuestion}
		}
		ev.Survey.Screen = domain.ScreenQuestion // повтор после «Напомнить завтра» — с вопроса
		msg = surveyMessage(to, step, e, ev, s.now())
	} else {
		msg = stepMessage(to, step, e, ev.ID, test, s.now())
		if cont {
			msg = withContinue(msg, ev.ID)
		}
	}
	atts, planMissing := s.attachFor(ctx, e, step.Media, step.Title)
	if planMissing {
		msg.Text += "\n\n" + textPlanMissing
	}
	if groups := GroupAttachments(atts); len(groups) > 0 {
		// MAX: файл — одно вложение на сообщение; лишние группы уходят перед текстом
		for _, g := range groups[:len(groups)-1] {
			if _, err := s.max.Send(ctx, max.OutMessage{ChatID: msg.ChatID, UserID: msg.UserID, Attachments: g}); err != nil {
				logger.Warn("планировщик: вложение события %s не ушло: %v", ev.ID, err)
			}
		}
		msg.Attachments = groups[len(groups)-1]
	}

	if emptyMessage(msg) {
		logger.Warn("планировщик: шаг события %s без текста и без вложений — запасной текст", ev.ID)
		msg.Text = strings.TrimSuffix(msg.Text, " ") + byForm(e, textMaterialSoonTy, textMaterialSoonVy)
	}
	mid, err := s.max.Send(ctx, msg)
	if err != nil && len(msg.Attachments) > 0 && s.media != nil {
		// MAX мог отказать по протухшему токену: забыть токены, загрузить заново, один повтор
		var toks []string
		for _, a := range msg.Attachments {
			toks = append(toks, a.Token)
		}
		s.media.ForgetTokens(ctx, toks)
		if atts, _ := s.attachFor(ctx, e, step.Media, step.Title); len(atts) > 0 {
			groups := GroupAttachments(atts)
			msg.Attachments = groups[len(groups)-1]
			mid, err = s.max.Send(ctx, msg)
		}
	}
	if err != nil {
		ev.Attempts++
		logger.Warn("планировщик: событие %s не отправлено (попытка %d): %v", ev.ID, ev.Attempts, err)
		if ev.Attempts >= domain.MaxSendAttempts {
			ev.Status = domain.EventFailed
		} else {
			ev.FireAt = s.now().Add(time.Minute)
		}
		return false, tx.Events.Save(ctx, ev)
	}
	now := s.now()
	ev.Status, ev.MessageID, ev.SentAt, ev.Text = domain.EventSent, mid, &now, &msg.Text
	switch {
	case step.Kind == domain.KindInput:
		ev.Waiting = domain.WaitInput
		if err := tx.Employees.SetAwaitFreeText(ctx, e.ID, &domain.FreeTextWait{EventID: ev.ID, Label: step.Title, Input: true}); err != nil {
			return false, err
		}
	case isQuestion(step):
		ev.Waiting = domain.WaitAnswer
	case cont:
		ev.Waiting = domain.WaitContinue
	default:
		ev.Waiting = ""
	}
	if isSurvey(step) && ev.Survey.Q < len(step.Survey.Questions) && qType(step.Survey.Questions[ev.Survey.Q]) == qText {
		// вопрос «своими словами» — следующий текст новичка и есть ответ
		if err := tx.Employees.SetAwaitSurvey(ctx, e.ID, &ev.ID, ev.Survey.Q); err != nil {
			return false, err
		}
	}
	return true, tx.Events.Save(ctx, ev)
}

// SkipMissed — новичок подключился (дал ответ на согласие) позже части шагов. Политика
// пропущенных событий, упрощённая для итерации 1: до выхода созревшие шаги идут сразу (пакет
// D−3 нужен и в D−1); после выхода — только шаги сегодняшнего дня, прошлые помечаются
// пропущенными. С FEATURE_CATCHUP вместо пропуска — догоняющая очередь (scheduler_catchup.go).
func (s *SchedulerService) SkipMissed(ctx context.Context, tx *repositories.Repos, e *domain.Employee) error {
	if s.features.Catchup {
		return s.BuildCatchup(ctx, tx, e)
	}
	now := s.now()
	today := DayNumber(e.StartDate, now, s.SiteFor(e))
	if today < 0 {
		return nil
	}
	events, err := tx.Events.ScheduledOf(ctx, e.ID)
	if err != nil {
		return err
	}
	for i := range events {
		ev := &events[i]
		if ev.FireAt.After(now) || ev.DayOffset >= today {
			continue
		}
		ev.Status, ev.Answer = domain.EventSkipped, strPtr(skipMissed)
		if err := tx.Events.Save(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

// SkipSurveys — отказ от согласия: неотправленные опросы снимаются сразу, чтобы таймлайн HR
// показывал правду, а не «запланировано».
func (s *SchedulerService) SkipSurveys(ctx context.Context, tx *repositories.Repos, employeeID uuid.UUID) error {
	events, err := tx.Events.ScheduledOf(ctx, employeeID)
	if err != nil {
		return err
	}
	for i := range events {
		if events[i].Kind != domain.KindSurvey {
			continue
		}
		events[i].Status, events[i].Answer = domain.EventSkipped, strPtr(skipNoConsent)
		if err := tx.Events.Save(ctx, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

func isSurvey(s domain.ScenarioStep) bool {
	return s.Kind == domain.KindSurvey && s.Survey != nil && len(s.Survey.Questions) > 0
}

func strPtr(s string) *string { return &s }

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
