package services

import (
	"context"
	"time"

	"main/domain"
	"main/repositories"
)

// Поздний вход (FEATURE_CATCHUP, план итерации 24.10 §3.5). Новичок ответил на согласие после
// даты выхода — вместо SkipMissed прошлые шаги разбираются так:
//   - необязательные — skipped, как раньше;
//   - обязательные без вопроса (материалы) — одна сводка «Что вы пропустили» в первой сессии
//     (механизм flushBlock) и до трёх сообщений с их вложениями;
//   - обязательные вопросы и опросы с обязательными вопросами — догоняющая очередь
//     (ScheduledEvent.Catchup): первый блок — в той же сессии сразу после сводки, остальные — по
//     одному в день в начале рабочего окна, раньше обычного шага того же дня;
//   - опрос старше 7 дней (пульс дня, итоги недели) — skipped «не задан (поздний вход)».
//
// Бюджет: догоняющий блок и наджи вместе — не больше 2 проактивных сообщений в день, догоняющих
// блоков — не больше одного (budgetLeft); не влез — переезжает на следующий рабочий день.
// EventID детерминирован: догоняющее событие — то же событие с новым fire_at и блоком.

// catchupMissedKey — блок сводки «Что вы пропустили» догоняющей очереди.
const catchupMissedKey = "catchup:missed"

// catchupStaleAfter — опрос старше этого не задаём при позднем входе.
const catchupStaleAfter = 7 * 24 * time.Hour

// skipLateSurvey — причина в таймлайне HR у устаревшего опроса.
const skipLateSurvey = "не задан (поздний вход)"

// catchupMaxMedia — вложений материалов сразу после сводки, не больше (сообщений).
const catchupMaxMedia = 3

// surveyRequired — у опроса есть обязательный вопрос.
func surveyRequired(st domain.ScenarioStep) bool {
	if st.Survey == nil {
		return false
	}
	for _, q := range st.Survey.Questions {
		if q.Required {
			return true
		}
	}
	return false
}

// BuildCatchup — догоняющая очередь при первом ответе на согласие. До дня выхода — как
// SkipMissed (ничего не пропущено).
func (s *SchedulerService) BuildCatchup(ctx context.Context, tx *repositories.Repos, e *domain.Employee) error {
	now := s.now()
	site := s.SiteFor(e)
	today := DayNumber(e.StartDate, now, site)
	if today < 0 {
		return nil
	}
	events, err := tx.Events.ScheduledOf(ctx, e.ID) // по fire_at, position
	if err != nil {
		return err
	}
	f := &flow{e: e, versions: map[int]*domain.ScenarioVersion{}}
	skip := func(ev *domain.ScheduledEvent, why string) error {
		ev.Status, ev.Answer = domain.EventSkipped, strPtr(why)
		return tx.Events.Save(ctx, ev)
	}
	var blocks []string                              // ключи догоняющих блоков-вопросов по порядку
	byBlock := map[string][]*domain.ScheduledEvent{} // события блока
	queued := 0
	for i := range events {
		ev := &events[i]
		if ev.FireAt.After(now) || ev.DayOffset >= today {
			continue
		}
		if ev.Snapshot == nil {
			if _, ok := f.versions[ev.ScenarioVersion]; !ok {
				v, err := tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion)
				if err != nil {
					return err
				}
				f.versions[ev.ScenarioVersion] = v
			}
		}
		st, ok := f.step(ev)
		switch {
		case !ok || !sendable(e, st):
			err = skip(ev, skipMissed)
		case isSurvey(st) && now.Sub(ev.FireAt) > catchupStaleAfter:
			err = skip(ev, skipLateSurvey)
		case isSurvey(st) && (st.Required || surveyRequired(st)), !isSurvey(st) && st.Required && isQuestion(st):
			ev.Catchup = true
			if _, seen := byBlock[ev.Block]; !seen {
				blocks = append(blocks, ev.Block)
			}
			byBlock[ev.Block] = append(byBlock[ev.Block], ev)
			queued++
			continue
		case !isSurvey(st) && st.Required:
			ev.Catchup, ev.Block, ev.FireAt = true, catchupMissedKey, now
			err = tx.Events.Save(ctx, ev)
			queued++
		default:
			err = skip(ev, skipMissed)
		}
		if err != nil {
			return err
		}
	}
	// первый блок — в этой сессии (после сводки: runEmployee ставит сводку первой), остальные — по
	// одному в рабочий день с начала окна
	day := civilDate(now.In(site.Loc), site.Loc)
	for i, key := range blocks {
		at := now
		if i > 0 {
			day = nextWorkday(day.AddDate(0, 0, 1), site)
			at = atMinutes(day, site.DayStart[day.Weekday()], site.Loc)
		}
		for _, ev := range byBlock[key] {
			ev.FireAt = at
			if err := tx.Events.Save(ctx, ev); err != nil {
				return err
			}
		}
	}
	if queued == 0 {
		return nil
	}
	e.CatchupStartedAt, e.CatchupDoneAt = &now, nil
	return tx.Employees.SetCatchupStarted(ctx, e.ID, now)
}

// catchupBlock — блок догоняющей очереди (кроме сводки).
func catchupBlock(evs []*domain.ScheduledEvent) bool {
	for _, ev := range evs {
		if ev.Catchup {
			return true
		}
	}
	return false
}

// catchupStarted — у блока уже ушло хоть одно событие (продолжение, а не новый блок).
func catchupStarted(evs []*domain.ScheduledEvent) bool {
	for _, ev := range evs {
		if ev.SentAt != nil {
			return true
		}
	}
	return false
}

// postponeCatchup — догоняющий блок не влез в бюджет дня: на начало окна следующего рабочего дня.
func (s *SchedulerService) postponeCatchup(ctx context.Context, tx *repositories.Repos, f *flow, key string) error {
	site := s.SiteFor(f.e)
	now := s.now()
	day := nextWorkday(civilDate(now.In(site.Loc), site.Loc).AddDate(0, 0, 1), site)
	at := atMinutes(day, site.DayStart[day.Weekday()], site.Loc)
	for _, ev := range f.block(key) {
		if ev.Status != domain.EventScheduled {
			continue
		}
		ev.FireAt = at
		if err := tx.Events.Save(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

// finishCatchups — «Догнал ✓»: очередь пройдена (CatchupProgress: done == total).
func (s *SchedulerService) finishCatchups(ctx context.Context) error {
	ids, err := s.repos.Employees.CatchingUp(ctx, 500)
	if err != nil {
		return err
	}
	now := s.now()
	for _, id := range ids {
		done, total, err := s.repos.Events.CatchupProgress(ctx, id)
		if err != nil {
			return err
		}
		if total > 0 && done >= total {
			if err := s.repos.Employees.SetCatchupDone(ctx, id, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// CatchupState — прогресс догоняющей очереди для карточки: «Догоняет: done из total», «Догнал ✓».
type CatchupState struct {
	Done, Total int
	Finished    bool
}

// Catchup — прогресс новичка; nil — очередь не собиралась.
func (s *SchedulerService) Catchup(ctx context.Context, e *domain.Employee) (*CatchupState, error) {
	if e == nil || e.CatchupStartedAt == nil {
		return nil, nil
	}
	done, total, err := s.repos.Events.CatchupProgress(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	return &CatchupState{Done: int(done), Total: int(total), Finished: e.CatchupDoneAt != nil || (total > 0 && done >= total)}, nil
}

// catchupFirstSession — ни один догоняющий вопрос ещё не уходил: первая сессия позднего входа
// (реакция на подключение — и ночью, и в выходной).
func catchupFirstSession(f *flow) bool {
	for i := range f.events {
		ev := &f.events[i]
		if ev.Catchup && ev.Block != catchupMissedKey && ev.SentAt != nil {
			return false
		}
	}
	return true
}
