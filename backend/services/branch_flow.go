package services

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// Исполнение блока (api_contract.md v4, «Исполнение блока»). Блок — шаги с одним днём и
// временем (реплика «12:01» после «12:00» — тот же блок). Первая реплика уходит по расписанию,
// каждая следующая — от предыдущей:
//   - вопрос (кнопки-ответы или опрос) держит поток до ответа; ответ → правка «✅» → ветка →
//     следующая реплика сразу, даже ночью: это реакция на действие новичка;
//   - шаг без вопроса, за которым в блоке есть ещё шаги, получает «Дальше →» (advance=button,
//     по умолчанию); явный advance=auto — следующая реплика сразу с «печатает…» и паузой по
//     длине, но подряд без участия новичка не больше двух;
//   - ответа нет, а пришло время следующего блока: необязательные реплики прошлого блока —
//     skipped, обязательные — одним сообщением «Что вы пропустили».

// blockJoin — реплика не позже стольких минут после предыдущей того же дня — тот же блок
// («12:01» после «12:00», «09.01» после «09.00» в ТЗ — «сразу после предыдущей»).
const blockJoin = 2

// continueLabel — подпись «Дальше →» (env CONTINUE_LABEL).
var continueLabel = func() string {
	if s := strings.TrimSpace(os.Getenv("CONTINUE_LABEL")); s != "" {
		return s
	}
	return "Дальше →"
}()

// Причины в answer, которые видит HR.
const (
	skipHRTask      = "пропущено: задача HR, бот не отправляет"
	skipNoAnswer    = "пропущено: не было ответа на вопрос блока"
	skipEndBlock    = "пропущено: ветка завершила блок"
	skipGoto        = "пропущено: ветка перешла к другому блоку"
	sentInMissed    = "отправлено в сводке «Что вы пропустили»"
	answerUnanswer  = "не отвечен: напоминания исчерпаны"
	textMissedHead  = "📌 Что вы пропустили:"
	textRemindHead  = "🔔 Напоминаю:"
	autoPausePer100 = time.Second
)

// blockKeys — ключ блока каждого шага версии.
func blockKeys(steps []domain.ScenarioStep) map[uuid.UUID]string {
	sorted := append([]domain.ScenarioStep(nil), steps...)
	mins := func(s domain.ScenarioStep) int {
		if m, ok := parseHHMM(s.Time); ok {
			return m
		}
		return 10 * 60
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.DayOffset != b.DayOffset {
			return a.DayOffset < b.DayOffset
		}
		if mins(a) != mins(b) {
			return mins(a) < mins(b)
		}
		return a.Position < b.Position
	})
	out := make(map[uuid.UUID]string, len(sorted))
	key, lastDay, lastMin := "", 0, 0
	for i, st := range sorted {
		m := mins(st)
		if i == 0 || st.DayOffset != lastDay || m-lastMin > blockJoin {
			key = fmt.Sprintf("d%d@%02d:%02d", st.DayOffset, m/60, m%60)
		}
		lastDay, lastMin = st.DayOffset, m
		out[st.ID] = key
	}
	return out
}

// isQuestion — шаг ждёт ответа: опрос или кнопки-ответы (не ссылки и не «Спросить человека»).
func isQuestion(s domain.ScenarioStep) bool {
	if isSurvey(s) || s.Kind == domain.KindInput {
		return true
	}
	for _, b := range s.Buttons {
		if b.Action != "human" && buttonURL(b.Action) == "" {
			return true
		}
	}
	return false
}

// flagOn — флаг новичка включён.
func flagOn(e *domain.Employee, flag string) bool { return e.Flags != nil && e.Flags[flag] }

// sendable — шаг уйдёт новичку (не задача HR, отмашка есть, согласие для опроса).
func sendable(e *domain.Employee, s domain.ScenarioStep) bool {
	switch {
	case s.HRTask:
		return false
	case s.FlagRequired != nil && *s.FlagRequired != "" && !flagOn(e, *s.FlagRequired):
		return false
	case s.Kind == domain.KindSurvey && (e.Consent == nil || *e.Consent != domain.ConsentGiven):
		return false
	}
	return true
}

// advanceOf — как уходит следующая реплика после шага без вопроса.
func advanceOf(s domain.ScenarioStep) string {
	if s.Advance != nil && *s.Advance == domain.AdvanceAuto {
		return domain.AdvanceAuto
	}
	return domain.AdvanceButton
}

// autoPause — «печатает…» перед репликой после auto: ≈1 с на 100 символов, 1–4 с.
func autoPause(text string) time.Duration {
	d := time.Duration(utf8.RuneCountInString(text)) * autoPausePer100 / 100
	if d < time.Second {
		d = time.Second
	}
	if d > 4*time.Second {
		d = 4 * time.Second
	}
	return d
}

// flow — события новичка и шаги их версий (снимки).
type flow struct {
	e        *domain.Employee
	events   []domain.ScheduledEvent
	versions map[int]*domain.ScenarioVersion
}

func (f *flow) step(ev *domain.ScheduledEvent) (domain.ScenarioStep, bool) {
	if ev.Snapshot != nil {
		return domain.ScenarioStep(*ev.Snapshot), true
	}
	v := f.versions[ev.ScenarioVersion]
	if v == nil {
		return domain.ScenarioStep{}, false
	}
	return v.Step(ev.StepID)
}

// block — события блока по position.
func (f *flow) block(key string) []*domain.ScheduledEvent {
	var out []*domain.ScheduledEvent
	for i := range f.events {
		if f.events[i].Block == key {
			out = append(out, &f.events[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (f *flow) byID(id uuid.UUID) *domain.ScheduledEvent {
	for i := range f.events {
		if f.events[i].ID == id {
			return &f.events[i]
		}
	}
	return nil
}

// loadFlow — все события новичка с версиями; у старых событий без блока ключ дозаполняется.
func (s *SchedulerService) loadFlow(ctx context.Context, tx *repositories.Repos, e *domain.Employee) (*flow, error) {
	evs, err := tx.Events.ByEmployee(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	f := &flow{e: e, events: evs, versions: map[int]*domain.ScenarioVersion{}}
	keys := map[int]map[uuid.UUID]string{}
	for i := range f.events {
		ev := &f.events[i]
		if ev.Snapshot != nil {
			if ev.Block == "" {
				ev.Block = "test:" + ev.ID.String()
			}
			continue
		}
		v, ok := f.versions[ev.ScenarioVersion]
		if !ok {
			if v, err = tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion); err != nil {
				return nil, err
			}
			f.versions[ev.ScenarioVersion] = v
		}
		if ev.Block != "" || v == nil {
			continue
		}
		if keys[ev.ScenarioVersion] == nil {
			keys[ev.ScenarioVersion] = blockKeys(v.Steps)
		}
		ev.Block = keys[ev.ScenarioVersion][ev.StepID]
		if ev.Block == "" {
			ev.Block = fmt.Sprintf("d%d", ev.DayOffset)
		}
		if err := tx.Events.SetBlock(ctx, ev.ID, ev.Block); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// blockRun — итог прогона блока.
type blockRun struct {
	sent int
	halt bool // блок ждёт новичка: следующие блоки тоже ждут
}

// runOpts — прогон блока: test — машина времени; start — начало такта; later — ранний fire_at
// следующих созревших блоков (nil — их нет).
type runOpts struct {
	test     bool
	start    time.Time
	later    *time.Time
	laterDay *int  // машина времени: самый поздний день следующих созревших блоков
	paused   *bool // уже было отправлено в этом такте (пауза пакета перед следующим)
}

// holder — событие блока, которое держит поток: отправлено и чего-то ждёт. Перенесённый
// «Напомнить завтра» опрос держит поток, пока не созрел.
func holderOf(evs []*domain.ScheduledEvent, now time.Time) *domain.ScheduledEvent {
	var h *domain.ScheduledEvent
	for _, ev := range evs {
		if ev.Waiting == "" {
			continue
		}
		if ev.Status == domain.EventScheduled && !ev.FireAt.After(now) {
			continue // созревший повтор опроса — сам уйдёт следующей репликой
		}
		h = ev // отправлен и ждёт: ответа, терминальной кнопки ветки, текста, «Дальше»
	}
	return h
}

// runBlock — отправить реплики блока по очереди до первого вопроса или «Дальше».
func (s *SchedulerService) runBlock(ctx context.Context, tx *repositories.Repos, f *flow, key string, o runOpts) (blockRun, error) {
	evs := f.block(key)
	holder := holderOf(evs, s.now())
	var pending []*domain.ScheduledEvent
	for _, ev := range evs {
		if ev.Status != domain.EventScheduled || ev == holder {
			continue
		}
		if holder != nil && ev.Position < holder.Position {
			continue
		}
		pending = append(pending, ev)
	}
	if len(pending) == 0 {
		return blockRun{}, nil
	}
	if holder != nil {
		arrived := false
		if o.later != nil {
			if o.test {
				// машина времени: «время следующего блока» — следующий виртуальный день или
				// повторный вызов машины
				arrived = holder.SentAt == nil || holder.SentAt.Before(o.start) ||
					(o.laterDay != nil && *o.laterDay > holder.DayOffset)
			} else {
				arrived = holder.SentAt == nil || o.later.After(*holder.SentAt)
			}
		}
		if !arrived {
			return blockRun{halt: true}, nil
		}
		n, err := s.flushBlock(ctx, tx, f, pending, o.test)
		return blockRun{sent: n}, err
	}
	return s.sendRun(ctx, tx, f, pending, o)
}

// sendRun — реплики подряд: вопрос или «Дальше →» останавливают поток.
func (s *SchedulerService) sendRun(ctx context.Context, tx *repositories.Repos, f *flow, pending []*domain.ScheduledEvent, o runOpts) (blockRun, error) {
	var res blockRun
	autoRun := 0
	prevAuto := false
	for i, ev := range pending {
		step, found := f.step(ev)
		hasNext := false
		for _, nx := range pending[i+1:] {
			if st, ok := f.step(nx); ok && sendable(f.e, st) {
				hasNext = true
				break
			}
		}
		cont := false
		auto := false
		if found && sendable(f.e, step) && !isQuestion(step) && hasNext {
			if advanceOf(step) == domain.AdvanceAuto {
				autoRun++
				auto = true
				if autoRun >= 2 { // не больше двух подряд без участия новичка
					cont, auto = true, false
				}
			} else {
				cont = true
			}
		}
		if found && sendable(f.e, step) {
			switch {
			case prevAuto:
				s.typingPause(ctx, f.e, stepText(step, f.e, s.now()))
			case !o.test && o.paused != nil && *o.paused && s.packGap > 0:
				if err := s.sleep(ctx, s.packGap); err != nil {
					return res, err
				}
			}
		}
		locked, err := tx.Events.ByIDForUpdate(ctx, ev.ID)
		if err != nil {
			return res, err
		}
		if locked == nil || locked.Status != domain.EventScheduled {
			continue
		}
		var v *domain.ScenarioVersion
		if locked.Snapshot == nil {
			v = f.versions[locked.ScenarioVersion]
		}
		ok, err := s.sendEvent(ctx, tx, f.e, v, locked, o.test, cont)
		if err != nil {
			return res, err
		}
		*ev = *locked
		if !ok {
			continue
		}
		res.sent++
		if o.paused != nil {
			*o.paused = true
		}
		if locked.Waiting != "" {
			res.halt = true
			return res, nil
		}
		prevAuto = auto
	}
	return res, nil
}

// typingPause — «печатает…» и пауза по длине следующей реплики.
func (s *SchedulerService) typingPause(ctx context.Context, e *domain.Employee, text string) {
	if t, ok := s.max.(max.Typer); ok && e.ChatID != nil {
		if err := t.Typing(ctx, *e.ChatID); err != nil {
			logger.Info("планировщик: «печатает» не показан: %v", err)
		}
	}
	_ = s.sleep(ctx, autoPause(text))
}

// flushBlock — пришло время следующего блока, а этот ждёт ответа: необязательные реплики —
// skipped, обязательные — одним сообщением «Что вы пропустили».
func (s *SchedulerService) flushBlock(ctx context.Context, tx *repositories.Repos, f *flow, pending []*domain.ScheduledEvent, test bool) (int, error) {
	var missed []*domain.ScheduledEvent
	var texts []string
	for _, ev := range pending {
		locked, err := tx.Events.ByIDForUpdate(ctx, ev.ID)
		if err != nil {
			return 0, err
		}
		if locked == nil || locked.Status != domain.EventScheduled {
			continue
		}
		step, ok := f.step(locked)
		if ok && step.Required && sendable(f.e, step) {
			missed = append(missed, locked)
			texts = append(texts, stepText(step, f.e, s.now()))
			continue
		}
		locked.Status, locked.Answer = domain.EventSkipped, strPtr(skipNoAnswer)
		if ok && step.HRTask {
			locked.Answer = strPtr(skipHRTask)
		}
		if err := tx.Events.Save(ctx, locked); err != nil {
			return 0, err
		}
		*ev = *locked
	}
	if len(missed) == 0 {
		return 0, nil
	}
	text := textMissedHead + "\n\n" + strings.Join(texts, "\n\n— — —\n\n")
	if test {
		text = testPrefix + text
	}
	msg := recipientOf(f.e).msg(text, "", [][]max.Button{humanRow(nil)})
	mid, err := s.max.Send(ctx, msg)
	now := s.now()
	for _, ev := range missed {
		if err != nil {
			ev.Status, ev.Answer = domain.EventFailed, strPtr("сводка «Что вы пропустили» не ушла")
		} else {
			ev.Status, ev.MessageID, ev.SentAt, ev.Answer, ev.Text = domain.EventSent, mid, &now, strPtr(sentInMissed), &text
		}
		ev.IsTest = ev.IsTest || test
		if err := tx.Events.Save(ctx, ev); err != nil {
			return 0, err
		}
	}
	if err != nil {
		logger.Warn("планировщик: «Что вы пропустили» новичку %s не ушло: %v", f.e.ID, err)
		return 0, nil
	}
	return 1, nil
}

// runEmployee — созревшие события новичка по блокам в порядке fire_at.
func (s *SchedulerService) runEmployee(ctx context.Context, tx *repositories.Repos, empID uuid.UUID, due []domain.ScheduledEvent, test bool, start time.Time, paused *bool) (int, error) {
	e, err := tx.Employees.ByID(ctx, empID)
	if err != nil || e == nil {
		return 0, err
	}
	f, err := s.loadFlow(ctx, tx, e)
	if err != nil {
		return 0, err
	}
	var keys []string
	first := map[string]time.Time{}
	for _, d := range due {
		ev := f.byID(d.ID)
		if ev == nil {
			continue
		}
		if _, ok := first[ev.Block]; !ok {
			keys = append(keys, ev.Block)
			first[ev.Block] = ev.FireAt
		}
	}
	sent := 0
	for i, key := range keys {
		o := runOpts{test: test, start: start, paused: paused}
		for _, k := range keys[i+1:] {
			t := first[k]
			if o.later == nil || t.Before(*o.later) {
				o.later = &t
			}
			if d := blockDay(f, k); o.laterDay == nil || d > *o.laterDay {
				o.laterDay = &d
			}
		}
		res, err := s.runBlock(ctx, tx, f, key, o)
		if err != nil {
			return sent, err
		}
		sent += res.sent
		if res.halt && test && o.laterDay != nil && *o.laterDay > blockDay(f, key) {
			// машина времени ушла дальше этого дня: остаток блока — как без ответа
			res, err = s.runBlock(ctx, tx, f, key, o)
			if err != nil {
				return sent, err
			}
			sent += res.sent
		}
		if res.halt {
			break
		}
	}
	return sent, nil
}

// blockDay — день блока (по первому событию).
func blockDay(f *flow, key string) int {
	if evs := f.block(key); len(evs) > 0 {
		return evs[0].DayOffset
	}
	return 0
}

// Release — новичок ответил («✅», ветка, «Дальше», свой текст): следующие реплики блока сразу,
// без тихих часов, затем всё созревшее. Событие «Отправить себе» продолжения не имеет.
func (s *SchedulerService) Release(ctx context.Context, empID, evID uuid.UUID) error {
	if s.max == nil || !s.repos.Enabled() {
		return nil
	}
	var halt, snapshot, test bool
	var maxDay int
	s.mu.Lock()
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByID(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		f, err := s.loadFlow(ctx, tx, e)
		if err != nil {
			return err
		}
		ev := f.byID(evID)
		if ev == nil {
			return nil
		}
		snapshot, test = ev.Snapshot != nil, ev.IsTest
		maxDay = ev.DayOffset
		if e.VirtualDay != nil && *e.VirtualDay > maxDay {
			maxDay = *e.VirtualDay
		}
		res, err := s.runBlock(ctx, tx, f, ev.Block, runOpts{test: test, start: s.now()})
		halt = res.halt
		return err
	})
	s.mu.Unlock()
	if err != nil || halt || snapshot {
		return err
	}
	if test {
		_, err = s.dispatch(ctx, repositories.DueFilter{Now: s.now(), EmployeeID: &empID, MaxDay: &maxDay}, true)
		return err
	}
	_, err = s.DispatchEmployee(ctx, empID)
	return err
}

// EndBlock — then: end_block: оставшиеся реплики блока события — skipped.
func (s *SchedulerService) EndBlock(ctx context.Context, empID, evID uuid.UUID, reason string) error {
	return s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByID(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		f, err := s.loadFlow(ctx, tx, e)
		if err != nil {
			return err
		}
		ev := f.byID(evID)
		if ev == nil {
			return nil
		}
		for _, x := range f.block(ev.Block) {
			if x.Status != domain.EventScheduled || x.Position < ev.Position {
				continue
			}
			x.Status, x.Answer = domain.EventSkipped, strPtr(reason)
			if err := tx.Events.Save(ctx, x); err != nil {
				return err
			}
		}
		return nil
	})
}

// GotoBlock — then: goto_block: остаток текущего блока — skipped, блок с ключом шага key
// уходит сейчас. Ключа нет — как continue.
func (s *SchedulerService) GotoBlock(ctx context.Context, empID, evID uuid.UUID, key string) error {
	var target uuid.UUID
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByID(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		f, err := s.loadFlow(ctx, tx, e)
		if err != nil {
			return err
		}
		ev := f.byID(evID)
		if ev == nil {
			return nil
		}
		var block string
		for i := range f.events {
			x := &f.events[i]
			if st, ok := f.step(x); ok && st.Key == key && x.ScenarioVersion == ev.ScenarioVersion {
				block = x.Block
				break
			}
		}
		if block == "" || block == ev.Block {
			logger.Warn("бот: goto_block «%s» не найден у новичка %s — продолжаю поток", key, empID)
			target = evID
			return nil
		}
		for _, x := range f.block(ev.Block) {
			if x.Status == domain.EventScheduled && x.Position > ev.Position {
				x.Status, x.Answer = domain.EventSkipped, strPtr(skipGoto)
				if err := tx.Events.Save(ctx, x); err != nil {
					return err
				}
			}
		}
		now := s.now()
		for _, x := range f.block(block) {
			if x.Status == domain.EventScheduled {
				x.FireAt = now
				if err := tx.Events.Save(ctx, x); err != nil {
					return err
				}
				if target == uuid.Nil {
					target = x.ID
				}
			}
		}
		if target == uuid.Nil {
			target = evID
		}
		return nil
	})
	if err != nil || target == uuid.Nil {
		return err
	}
	return s.Release(ctx, empID, target)
}

// FlagChanged — флаг новичка включён: шаги «по отмашке» с этим флагом снова в очереди и уходят,
// если их время пришло.
func (s *SchedulerService) FlagChanged(ctx context.Context, empID uuid.UUID) error {
	var test bool
	var maxDay int
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByID(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		held, err := tx.Events.HeldOf(ctx, empID)
		if err != nil {
			return err
		}
		f := &flow{e: e, versions: map[int]*domain.ScenarioVersion{}}
		for i := range held {
			ev := &held[i]
			v, ok := f.versions[ev.ScenarioVersion]
			if !ok {
				if v, err = tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion); err != nil {
					return err
				}
				f.versions[ev.ScenarioVersion] = v
			}
			st, ok := f.step(ev)
			if !ok || (st.FlagRequired != nil && !flagOn(e, *st.FlagRequired)) {
				continue
			}
			ev.Status, ev.Answer = domain.EventScheduled, nil
			if ev.IsTest {
				test = true
			}
			if err := tx.Events.Save(ctx, ev); err != nil {
				return err
			}
		}
		if e.VirtualDay != nil {
			test, maxDay = true, *e.VirtualDay
		}
		return nil
	})
	if err != nil {
		return err
	}
	if test {
		_, err = s.dispatch(ctx, repositories.DueFilter{Now: s.now(), EmployeeID: &empID, MaxDay: &maxDay}, true)
		return err
	}
	_, err = s.DispatchEmployee(ctx, empID)
	return err
}

// remindNoAnswer — no_answer шага: напоминание через remind_after_h[k] часов после отправки, в
// рабочие часы, не больше max раз; потом вопрос «не отвечен», поток блока идёт дальше.
func (s *SchedulerService) remindNoAnswer(ctx context.Context) (int, error) {
	if s.max == nil || !s.repos.Enabled() {
		return 0, nil
	}
	now := s.now()
	var release []domain.ScheduledEvent
	n := 0
	s.mu.Lock()
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		evs, err := tx.Events.AwaitingAnswer(ctx, now.Add(-time.Hour), 200)
		if err != nil {
			return err
		}
		for i := range evs {
			ev, err := tx.Events.ByIDForUpdate(ctx, evs[i].ID)
			if err != nil {
				return err
			}
			if ev == nil || ev.Status != domain.EventSent || ev.Waiting != domain.WaitAnswer || ev.SentAt == nil {
				continue
			}
			e, err := tx.Employees.ByID(ctx, ev.EmployeeID)
			if err != nil || e == nil {
				return err
			}
			f := &flow{e: e, versions: map[int]*domain.ScenarioVersion{}}
			if ev.Snapshot == nil {
				v, err := tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion)
				if err != nil {
					return err
				}
				f.versions[ev.ScenarioVersion] = v
			}
			st, ok := f.step(ev)
			if !ok || st.NoAnswer == nil || len(st.NoAnswer.RemindAfterH) == 0 || isSurvey(st) {
				continue
			}
			rule := st.NoAnswer
			limit := rule.Max
			if limit <= 0 || limit > len(rule.RemindAfterH) {
				limit = len(rule.RemindAfterH)
			}
			site := domain.SiteOf(e.Site)
			if ev.Reminded >= limit {
				// после последнего напоминания — ещё сутки, затем «не отвечен»
				last := rule.RemindAfterH[limit-1] + 24
				if now.Before(ev.SentAt.Add(time.Duration(last) * time.Hour)) {
					continue
				}
				ev.Status, ev.Answer, ev.Waiting = domain.EventSkipped, strPtr(answerUnanswer), ""
				if err := tx.Events.Save(ctx, ev); err != nil {
					return err
				}
				release = append(release, *ev)
				continue
			}
			if now.Before(ev.SentAt.Add(time.Duration(rule.RemindAfterH[ev.Reminded])*time.Hour)) || !InWorkHours(now, site) {
				continue
			}
			msg := stepMessage(recipientOf(e), st, e, ev.ID, ev.IsTest, now)
			msg.Text = textRemindHead + "\n" + msg.Text
			mid, err := s.max.Send(ctx, msg)
			if err != nil {
				logger.Warn("планировщик: напоминание %s не ушло: %v", ev.ID, err)
				continue
			}
			// интервалы — от первой отправки; кнопки обоих сообщений ведут в то же событие
			ev.Reminded++
			logger.Info("планировщик: напоминание %d по событию %s ушло (mid %s)", ev.Reminded, ev.ID, mid)
			if err := tx.Events.Save(ctx, ev); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return n, err
	}
	for _, ev := range release {
		if err := s.Release(ctx, ev.EmployeeID, ev.ID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// withContinue — «Дальше →» перед рядом «Спросить человека».
func withContinue(m max.OutMessage, eventID uuid.UUID) max.OutMessage {
	cont := []max.Button{{Text: continueLabel, Payload: preContinue + eventID.String()}}
	rows := m.Buttons
	if n := len(rows); n > 0 && len(rows[n-1]) == 1 && rows[n-1][0].Text == btnHuman {
		out := append([][]max.Button{}, rows[:n-1]...)
		m.Buttons = append(out, cont, rows[n-1])
		return m
	}
	m.Buttons = append(append([][]max.Button{}, rows...), cont, humanRow(&eventID))
	return m
}
