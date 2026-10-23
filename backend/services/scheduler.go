package services

import (
	"context"
	"strconv"
	"sync"
	"time"

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

	mu sync.Mutex // один такт отправки за раз в процессе; между процессами — SKIP LOCKED
}

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
	site := domain.SiteOf(e.Site)
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
		})
	}
	return out
}

// Reschedule — дата выхода перенесена: пересчитать fire_at всех неотправленных событий.
// Строки те же (event_id не меняется), поэтому дублей нет; отправленные не трогаем.
func (s *SchedulerService) Reschedule(ctx context.Context, tx *repositories.Repos, e *domain.Employee) error {
	v, err := tx.Scenarios.Version(ctx, e.ScenarioID, e.ScenarioVersion)
	if err != nil || v == nil {
		return err
	}
	events, err := tx.Events.ScheduledOf(ctx, e.ID)
	if err != nil {
		return err
	}
	site := domain.SiteOf(e.Site)
	for i := range events {
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
	return s.dispatch(ctx, repositories.DueFilter{Now: s.now()}, false)
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

// dispatch — забрать события под блокировкой и отправить. Транзакция держится, пока идут
// отправки: так событие не уйдёт дважды, даже если такт и машина времени совпали.
func (s *SchedulerService) dispatch(ctx context.Context, f repositories.DueFilter, test bool) (int, error) {
	if s.max == nil || !s.repos.Enabled() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sent := 0
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		events, err := tx.Events.LockDue(ctx, f)
		if err != nil {
			return err
		}
		employees := map[uuid.UUID]*domain.Employee{}
		versions := map[string]*domain.ScenarioVersion{}
		var last uuid.UUID
		for i := range events {
			ev := &events[i]
			e, ok := employees[ev.EmployeeID]
			if !ok {
				if e, err = tx.Employees.ByID(ctx, ev.EmployeeID); err != nil {
					return err
				}
				employees[ev.EmployeeID] = e
			}
			if e == nil {
				continue
			}
			key := e.ScenarioID.String() + "/" + strconv.Itoa(ev.ScenarioVersion)
			v, ok := versions[key]
			if !ok {
				if v, err = tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion); err != nil {
					return err
				}
				versions[key] = v
			}
			// Пакет одному новичку — с паузой между сообщениями (не в машине времени).
			if !test && last == ev.EmployeeID && s.packGap > 0 {
				if err := s.sleep(ctx, s.packGap); err != nil {
					return err
				}
			}
			ok, err = s.sendEvent(ctx, tx, e, v, ev, test)
			if err != nil {
				return err
			}
			if ok {
				sent++
				last = ev.EmployeeID
			}
		}
		return nil
	})
	return sent, err
}

// sendEvent — одно событие: пропустить, отправить или отметить сбой. Ошибка — только от БД;
// сбой MAX — это статус события, а не причина откатить весь такт.
func (s *SchedulerService) sendEvent(ctx context.Context, tx *repositories.Repos, e *domain.Employee, v *domain.ScenarioVersion, ev *domain.ScheduledEvent, test bool) (bool, error) {
	var step domain.ScenarioStep
	found := false
	if v != nil {
		step, found = v.Step(ev.StepID)
	}
	switch {
	case !found:
		ev.Status, ev.Answer = domain.EventFailed, strPtr(failNoStep)
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
	}

	mid, err := s.max.Send(ctx, msg)
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
	return true, tx.Events.Save(ctx, ev)
}

// SkipMissed — новичок подключился (дал ответ на согласие) позже части шагов. Политика
// пропущенных событий, упрощённая для итерации 1: до выхода созревшие шаги идут сразу (пакет
// D−3 нужен и в D−1); после выхода — только шаги сегодняшнего дня, прошлые помечаются
// пропущенными. TODO: «Что вы пропустили» одним сообщением с обязательными материалами.
func (s *SchedulerService) SkipMissed(ctx context.Context, tx *repositories.Repos, e *domain.Employee) error {
	now := s.now()
	today := DayNumber(e.StartDate, now, domain.SiteOf(e.Site))
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
