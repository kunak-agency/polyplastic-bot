package services

import (
	"context"
	"fmt"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// Наджи — напоминания новичку о вопросе без ответа (план итерации 24.10 §3.4).
//
// FEATURE_NUDGES_DEFAULT — один проход вместо remindNoAnswer и remindSurveys:
//   - у шага-вопроса без своего no_answer и у опроса без правила r70 — 4 ч и 24 ч от отправки, в
//     рабочее окно новичка (SiteFor); свои правила шага и r70 — со своими сроками;
//   - не больше 1 напоминания в день на диалог; висит несколько вопросов — одно общее «У вас 2
//     вопроса без ответа — вот первый»;
//   - вместе с догоняющими блоками — не больше 2 проактивных сообщений в день (шаги сценария
//     бюджет не тратят);
//   - после второго автонапоминания без ответа — Employee.UnresponsiveSince; автонаджи этому
//     новичку выключены, пока он что-нибудь не нажмёт или не напишет (LastActivityAt позже);
//   - каждое напоминание — строка в nudges (kind=auto, n — номер по этому вопросу).
//
// Ручной надж (NudgeNow, кнопка «Напомнить» в карточке) работает и без флага: в рабочее окно —
// сразу, в тихие часы — откладывается на начало окна (строка nudges с пустым message_id и
// плановым sent_at), такт отправляет его, когда время придёт.

// Сроки и лимиты наджей.
var nudgeDefaultHours = []int{4, 24}

const (
	nudgeDailyLimit     = 1 // напоминаний в день на диалог
	proactiveDailyLimit = 2 // наджи + догоняющие блоки в день на диалог
	unresponsiveAfter   = 2 // автонапоминаний подряд без ответа → «не отвечает»
)

// Тексты наджа. Коротко, без «вы не ответили» и восклицаний.
const (
	textNudgeOne    = "🔔 Напомню про этот вопрос — займёт 10 секунд."
	textNudgeManyVy = "🔔 У вас %d %s без ответа — вот первый. Займёт 10 секунд."
	textNudgeManyTy = "🔔 У тебя %d %s без ответа — вот первый. Займёт 10 секунд."
	// Ответы кабинету на «Напомнить».
	msgNudgeNothing  = "Вопросов без ответа нет — напоминать не о чем"
	msgNudgeBotOff   = "Бот выключен — напоминание отправить некому"
	msgNudgeNotReady = "Новичок ещё не подключился к боту — напоминать некому"
	msgNudgeSent     = "Напоминание отправлено"
	msgNudgeFailed   = "MAX не принял напоминание — попробуйте позже"
	msgNudgePlanned  = "Отправится в %s по времени новичка"
)

// questionsWord — «вопрос», «вопроса», «вопросов» для n.
func questionsWord(n int) string {
	switch m10, m100 := n%10, n%100; {
	case m10 == 1 && m100 != 11:
		return "вопрос"
	case m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14):
		return "вопроса"
	}
	return "вопросов"
}

// nudgeHead — первая строка наджа: один вопрос или «у вас N вопросов — вот первый».
func nudgeHead(e *domain.Employee, open int) string {
	if open <= 1 {
		return textNudgeOne
	}
	return fmt.Sprintf(byForm(e, textNudgeManyTy, textNudgeManyVy), open, questionsWord(open))
}

// Виды правил напоминания вопроса.
const (
	ruleDefault = iota // 4 ч / 24 ч (FEATURE_NUDGES_DEFAULT)
	ruleStep           // свой no_answer шага
	ruleSurvey         // правило сценария r70 (survey_reminder)
)

// nudgeCand — открытый вопрос новичка и его правило напоминаний.
type nudgeCand struct {
	ev    *domain.ScheduledEvent
	step  domain.ScenarioStep
	kind  int
	hours []int
	limit int
	rule  *domain.ScenarioRule // r70
}

// due — подошёл срок очередного напоминания (по одному за раз: пропущенные за ночь или выходные
// сроки не сливаются, иначе второе напоминание пропало бы).
func (c nudgeCand) due(now time.Time) bool {
	if c.ev.SentAt == nil || c.ev.Reminded >= c.limit || c.ev.Reminded >= len(c.hours) {
		return false
	}
	return !now.Before(c.ev.SentAt.Add(time.Duration(c.hours[c.ev.Reminded]) * time.Hour))
}

// tickIteration — часть такта итерации 24.10: отложенные ручные наджи, автонаджи
// (FEATURE_NUDGES_DEFAULT), отметка «Догнал ✓» (FEATURE_CATCHUP).
func (s *SchedulerService) tickIteration(ctx context.Context) (int, error) {
	if s.max == nil || !s.repos.Enabled() {
		return 0, nil
	}
	n, err := s.sendPlannedNudges(ctx)
	if err != nil {
		return n, err
	}
	if s.features.NudgesDefault {
		a, err := s.autoNudges(ctx)
		n += a
		if err != nil {
			return n, err
		}
	}
	if s.features.Catchup {
		if err := s.finishCatchups(ctx); err != nil {
			return n, err
		}
	}
	return n, nil
}

// autoNudges — проход автонапоминаний: по новичку в своей транзакции.
func (s *SchedulerService) autoNudges(ctx context.Context) (int, error) {
	now := s.now()
	ids, err := s.repos.Events.AwaitingEmployees(ctx, now.Add(-time.Hour), 500)
	if err != nil {
		return 0, err
	}
	sent := 0
	var release []domain.ScheduledEvent
	s.mu.Lock()
	for _, id := range ids {
		err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
			n, rel, err := s.nudgeEmployee(ctx, tx, id, now)
			sent += n
			release = append(release, rel...)
			return err
		})
		if err != nil {
			s.mu.Unlock()
			return sent, err
		}
	}
	s.mu.Unlock()
	for _, ev := range release {
		if err := s.Release(ctx, ev.EmployeeID, ev.ID); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// openFlow — новичок, его открытые вопросы и шаги к ним.
func (s *SchedulerService) openFlow(ctx context.Context, tx *repositories.Repos, e *domain.Employee) (*flow, error) {
	open, err := tx.Events.OpenQuestions(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	f := &flow{e: e, events: open, versions: map[int]*domain.ScenarioVersion{}}
	for i := range f.events {
		ev := &f.events[i]
		if ev.Snapshot != nil {
			continue
		}
		if _, ok := f.versions[ev.ScenarioVersion]; !ok {
			v, err := tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion)
			if err != nil {
				return nil, err
			}
			f.versions[ev.ScenarioVersion] = v
		}
	}
	return f, nil
}

// candidates — правила напоминаний открытых вопросов.
func (s *SchedulerService) candidates(ctx context.Context, tx *repositories.Repos, f *flow) []nudgeCand {
	var r70 *domain.ScenarioRule
	r70Loaded := false
	var out []nudgeCand
	for i := range f.events {
		ev := &f.events[i]
		st, ok := f.step(ev)
		if !ok || ev.SentAt == nil {
			continue
		}
		c := nudgeCand{ev: ev, step: st, kind: ruleDefault, hours: nudgeDefaultHours, limit: len(nudgeDefaultHours)}
		switch {
		case isSurvey(st):
			if ev.Snapshot != nil {
				continue // «Отправить себе» — без напоминаний, как у r70
			}
			if !r70Loaded {
				r70, r70Loaded = scenarioRule(ctx, tx, f.e, domain.RuleSurveyReminder), true
			}
			if r70 != nil {
				c.kind, c.rule, c.hours = ruleSurvey, r70, r70.RemindAfterH
				if len(c.hours) == 0 {
					c.hours = []int{24, 48}
				}
				c.limit = len(c.hours)
			}
		case ev.Waiting != domain.WaitAnswer:
			continue
		case st.NoAnswer != nil && len(st.NoAnswer.RemindAfterH) > 0:
			c.kind, c.hours = ruleStep, st.NoAnswer.RemindAfterH
			c.limit = st.NoAnswer.Max
			if c.limit <= 0 || c.limit > len(c.hours) {
				c.limit = len(c.hours)
			}
		case ev.Snapshot != nil:
			continue
		}
		out = append(out, c)
	}
	return out
}

// nudgeEmployee — автонапоминание одному новичку (не больше одного сообщения). release — вопросы
// со своим no_answer, у которых вышли напоминания: «не отвечен», поток блока идёт дальше.
func (s *SchedulerService) nudgeEmployee(ctx context.Context, tx *repositories.Repos, id uuid.UUID, now time.Time) (int, []domain.ScheduledEvent, error) {
	e, err := tx.Employees.ByIDForUpdate(ctx, id)
	if err != nil || e == nil {
		return 0, nil, err
	}
	if e.Connection != domain.ConnConnected || !reachable(e) {
		return 0, nil, nil
	}
	if e.UnresponsiveSince != nil && e.LastActivityAt != nil && e.LastActivityAt.After(*e.UnresponsiveSince) {
		// ответил после статуса «не отвечает» — автонаджи снова включены
		e.UnresponsiveSince = nil
		if err := tx.Employees.SetUnresponsive(ctx, e.ID, nil); err != nil {
			return 0, nil, err
		}
	}
	f, err := s.openFlow(ctx, tx, e)
	if err != nil {
		return 0, nil, err
	}
	cands := s.candidates(ctx, tx, f)
	var release []domain.ScheduledEvent
	var due []nudgeCand
	closed, held := 0, false
	for _, c := range cands {
		if c.kind != ruleStep && !c.ev.Catchup && !heldOut(c.ev) && !now.Before(c.ev.SentAt.Add(questionHoldMax)) {
			// 48 ч без ответа: поток блока идёт дальше, вопрос остаётся открытым (наджи и ручное
			// «Напомнить» — по нему же, в карточке «не отвечен»), рекрутеру — «не отвечает»
			c.ev.Answer = strPtr(answerHeldOut)
			if err := tx.Events.Save(ctx, c.ev); err != nil {
				return 0, nil, err
			}
			release = append(release, *c.ev)
			held = true
		}
		if c.kind == ruleStep && c.ev.Reminded >= c.limit {
			// после последнего напоминания — ещё сутки, затем «не отвечен» (как remindNoAnswer)
			last := c.hours[c.limit-1] + 24
			if now.Before(c.ev.SentAt.Add(time.Duration(last) * time.Hour)) {
				continue
			}
			c.ev.Status, c.ev.Answer, c.ev.Waiting = domain.EventSkipped, strPtr(answerUnanswer), ""
			if err := tx.Events.Save(ctx, c.ev); err != nil {
				return 0, nil, err
			}
			release = append(release, *c.ev)
			closed++
			continue
		}
		if c.due(now) {
			due = append(due, c)
		}
	}
	// вопрос 48 ч без ответа — «не отвечает» (строка и «Связаться» у рекрутера); ставится после
	// попытки наджа, чтобы созревшее напоминание по нему всё же ушло
	markHeld := func() error {
		if !held || e.UnresponsiveSince != nil {
			return nil
		}
		e.UnresponsiveSince = &now
		logger.Info("планировщик: новичок %s — «не отвечает»: вопрос без ответа 48 ч", e.ID)
		return tx.Employees.SetUnresponsive(ctx, e.ID, &now)
	}
	if len(due) == 0 || e.UnresponsiveSince != nil {
		return 0, release, markHeld()
	}
	site := s.SiteFor(e)
	if !InWorkHours(now, site) {
		return 0, release, markHeld()
	}
	if ok, err := s.budgetLeft(ctx, tx, e, site, now, false); err != nil || !ok {
		if err == nil {
			err = markHeld()
		}
		return 0, release, err
	}
	openCount := len(cands) - closed
	target := due[0]
	msg := s.nudgeMessage(ctx, e, target, openCount, now)
	mid, err := s.sendNudge(ctx, msg, target.ev)
	if err != nil {
		logger.Warn("планировщик: автонапоминание новичку %s не ушло: %v", e.ID, err)
		return 0, release, markHeld()
	}
	// все созревшие вопросы упомянуты общим напоминанием — каждый тратит свой срок
	for _, c := range due {
		c.ev.Reminded++
		if err := tx.Events.Save(ctx, c.ev); err != nil {
			return 0, nil, err
		}
	}
	evID := target.ev.ID
	if err := tx.Nudges.Create(ctx, &domain.Nudge{EmployeeID: e.ID, EventID: &evID, Kind: domain.NudgeAuto, N: target.ev.Reminded, SentAt: now, MessageID: mid}); err != nil {
		return 0, nil, err
	}
	logger.Info("планировщик: автонапоминание %d по событию %s (открытых вопросов %d)", target.ev.Reminded, evID, openCount)
	var since time.Time
	if e.LastActivityAt != nil {
		since = *e.LastActivityAt
	}
	n, err := tx.Nudges.CountSince(ctx, e.ID, domain.NudgeAuto, since)
	if err != nil {
		return 0, nil, err
	}
	if n >= unresponsiveAfter {
		if err := tx.Employees.SetUnresponsive(ctx, e.ID, &now); err != nil {
			return 0, nil, err
		}
		e.UnresponsiveSince = &now
		logger.Info("планировщик: новичок %s — «не отвечает» после %d напоминаний", e.ID, n)
	}
	return 1, release, markHeld()
}

// budgetLeft — можно ли сегодня ещё одно проактивное сообщение новичку: надж (не больше одного
// в день) или догоняющий блок (не больше одного в день). Вместе — не больше двух.
func (s *SchedulerService) budgetLeft(ctx context.Context, tx *repositories.Repos, e *domain.Employee, site domain.Site, now time.Time, catchup bool) (bool, error) {
	day := StartOfDay(now, site)
	nudges, err := tx.Nudges.CountSince(ctx, e.ID, "", day)
	if err != nil {
		return false, err
	}
	blocks, err := tx.Events.CatchupBlocksSince(ctx, e.ID, day, catchupMissedKey)
	if err != nil {
		return false, err
	}
	if catchup && blocks >= 1 || !catchup && nudges >= nudgeDailyLimit {
		return false, nil
	}
	return nudges+blocks < proactiveDailyLimit, nil
}

// nudgeMessage — надж: мягкая первая строка и сам вопрос с теми же кнопками. Опрос — текущий
// экран живого опроса (на необязательном вопросе там уже есть «Пропустить»); правило r70 — его
// текст. Необязательный шаг-вопрос без своей кнопки skip получает «Пропустить» (nudge_skip.go).
func (s *SchedulerService) nudgeMessage(ctx context.Context, e *domain.Employee, c nudgeCand, open int, now time.Time) max.OutMessage {
	to := s.recipient(e)
	head := nudgeHead(e, open)
	if c.kind == ruleSurvey && c.rule != nil {
		text := stepTextIn(domain.ScenarioStep{TextTy: c.rule.TextTy, TextVy: c.rule.TextVy}, e, now, to.siteFor(e))
		if c.ev.Title != "" {
			text += "\n\n" + fmt.Sprintf(textSurveyReminderTail, c.ev.Title)
		}
		if open > 1 {
			text = head + "\n\n" + text
		}
		if c.ev.IsTest {
			text = testPrefix + text
		}
		msg := to.msg(text, "", [][]max.Button{humanRow(&c.ev.ID)})
		atts, _ := s.attachFor(ctx, e, c.rule.Media, c.ev.Title)
		if groups := GroupAttachments(atts); len(groups) > 0 {
			msg.Attachments = groups[len(groups)-1]
		}
		return msg
	}
	if isSurvey(c.step) {
		ev := *c.ev
		if ev.Survey == nil {
			ev.Survey = &domain.SurveyState{Screen: domain.ScreenQuestion}
		}
		msg := surveyMessage(to, c.step, e, &ev, sentAt(c.ev, now))
		msg.Text = head + "\n\n" + msg.Text
		return msg
	}
	msg := stepMessage(to, c.step, e, c.ev.ID, c.ev.IsTest, sentAt(c.ev, now))
	msg.Text = head + "\n\n" + msg.Text
	if nudgeSkippable(c.step) {
		msg = withNudgeSkip(msg, c.ev.ID) // необязательный вопрос без своей кнопки skip (nudge_skip.go)
	}
	return msg
}

// sendNudge — отправить надж; с FEATURE_REPLIES — реплаем на сам вопрос, MAX отказал — ещё раз
// без реплая.
func (s *SchedulerService) sendNudge(ctx context.Context, msg max.OutMessage, ev *domain.ScheduledEvent) (string, error) {
	if s.features.Replies && ev != nil && ev.MessageID != "" {
		r := msg
		r.ReplyTo = ev.MessageID
		if mid, err := s.max.Send(ctx, r); err == nil {
			return mid, nil
		}
	}
	return s.max.Send(ctx, msg)
}

// NudgeResult — итог «Напомнить» для кабинета.
type NudgeResult struct {
	Nudge   domain.Nudge
	Sent    bool      // ушло сейчас
	At      time.Time // когда ушло или уйдёт
	TZ      string    // пояс новичка, в котором показано время
	Message string    // «Напоминание отправлено» / «Отправится в 08:00 по времени новичка»
}

// NudgeNow — ручное напоминание: текущий незакрытый вопрос новичка ещё раз, с мягкой первой
// строкой. В тихие часы новичка — откладывается на начало его рабочего окна. userID — кто нажал
// (User.ID), uuid.Nil — неизвестно (кабинет без входа).
func (s *SchedulerService) NudgeNow(ctx context.Context, empID, userID uuid.UUID) (*NudgeResult, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	if s.max == nil {
		return nil, errs.NewConflictError(msgNudgeBotOff)
	}
	var by *uuid.UUID
	if userID != uuid.Nil {
		by = &userID
	}
	now := s.now()
	var res *NudgeResult
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, empID)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError(msgNewcomerNotFound)
		}
		if e.Connection != domain.ConnConnected || !reachable(e) {
			return errs.NewConflictError(msgNudgeNotReady)
		}
		f, err := s.openFlow(ctx, tx, e)
		if err != nil {
			return err
		}
		cands := s.candidates(ctx, tx, f)
		if len(cands) == 0 {
			return errs.NewConflictError(msgNudgeNothing)
		}
		site := s.SiteFor(e)
		target := cands[0]
		evID := target.ev.ID
		n, err := s.nudgeNo(ctx, tx, e.ID, evID)
		if err != nil {
			return err
		}
		nd := domain.Nudge{EmployeeID: e.ID, EventID: &evID, Kind: domain.NudgeManual, N: n, CreatedBy: by}
		if !InWorkHours(now, site) {
			if planned, err := tx.Nudges.Planned(ctx, e.ID); err != nil || planned != nil {
				if planned != nil {
					res = plannedResult(*planned, site)
				}
				return err // уже отложен — второй не ставим
			}
			nd.SentAt = NextWorkMoment(now, site)
			if err := tx.Nudges.Create(ctx, &nd); err != nil {
				return err
			}
			res = plannedResult(nd, site)
			return nil
		}
		// ручной надж — кнопкой «Спросить»: «у вас N вопросов» по общей шапке, но правило r70 тут
		// не к месту — шлём сам опрос
		if target.kind == ruleSurvey {
			target.kind = ruleDefault
		}
		mid, err := s.sendNudge(ctx, s.nudgeMessage(ctx, e, target, len(cands), now), target.ev)
		nd.SentAt = now
		res = &NudgeResult{Sent: err == nil, At: now, TZ: site.TZ, Message: msgNudgeSent}
		if err != nil {
			logger.Warn("кабинет: ручное напоминание новичку %s не ушло: %v", e.ID, err)
			mid, res.Message = repositories.NudgeUndelivered, msgNudgeFailed
		}
		nd.MessageID = mid
		if err := tx.Nudges.Create(ctx, &nd); err != nil {
			return err
		}
		res.Nudge = nd
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// plannedResult — ответ кабинету про отложенный надж: время по часам новичка.
func plannedResult(n domain.Nudge, site domain.Site) *NudgeResult {
	local := n.SentAt.In(site.Loc)
	return &NudgeResult{Nudge: n, At: n.SentAt, TZ: site.TZ,
		Message: fmt.Sprintf(msgNudgePlanned, fmt.Sprintf("%02d:%02d", local.Hour(), local.Minute()))}
}

// nudgeNo — номер следующего напоминания по вопросу (авто и ручные вместе).
func (s *SchedulerService) nudgeNo(ctx context.Context, tx *repositories.Repos, empID, evID uuid.UUID) (int, error) {
	list, err := tx.Nudges.ListByEmployee(ctx, empID)
	if err != nil {
		return 0, err
	}
	n := 1
	for _, x := range list {
		if x.Kind != repositories.NudgeContacted && x.EventID != nil && *x.EventID == evID && x.MessageID != "" && x.MessageID != repositories.NudgeUndelivered {
			n++
		}
	}
	return n, nil
}

// sendPlannedNudges — отложенные ручные наджи, чьё время пришло.
func (s *SchedulerService) sendPlannedNudges(ctx context.Context) (int, error) {
	now := s.now()
	list, err := s.repos.Nudges.DuePlanned(ctx, now, 100)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	sent := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, nd := range list {
		err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
			e, err := tx.Employees.ByIDForUpdate(ctx, nd.EmployeeID)
			if err != nil {
				return err
			}
			if e == nil || e.Connection != domain.ConnConnected || !reachable(e) {
				return tx.Nudges.MarkSent(ctx, nd.ID, repositories.NudgeUndelivered, now)
			}
			site := s.SiteFor(e)
			if !InWorkHours(now, site) { // праздник добавили после постановки — ждём окна
				return tx.Nudges.MarkSent(ctx, nd.ID, "", NextWorkMoment(now, site))
			}
			f, err := s.openFlow(ctx, tx, e)
			if err != nil {
				return err
			}
			cands := s.candidates(ctx, tx, f)
			if len(cands) == 0 { // ответил, пока надж ждал утра
				return tx.Nudges.MarkSent(ctx, nd.ID, repositories.NudgeUndelivered, now)
			}
			target := cands[0]
			for _, c := range cands {
				if nd.EventID != nil && c.ev.ID == *nd.EventID {
					target = c
				}
			}
			if target.kind == ruleSurvey {
				target.kind = ruleDefault
			}
			mid, err := s.sendNudge(ctx, s.nudgeMessage(ctx, e, target, len(cands), now), target.ev)
			if err != nil {
				logger.Warn("планировщик: отложенное напоминание %s не ушло: %v", nd.ID, err)
				mid = repositories.NudgeUndelivered
			} else {
				sent++
			}
			return tx.Nudges.MarkSent(ctx, nd.ID, mid, now)
		})
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// NudgeItem — строка истории напоминаний для кабинета.
type NudgeItem struct {
	domain.Nudge
	EventTitle string
	Planned    bool // ждёт рабочего окна новичка
	Delivered  bool
}

// Nudges — история напоминаний новичку (траектория: «🔔 напоминание 1/2 авто · 2/2 вручную»).
func (s *SchedulerService) Nudges(ctx context.Context, empID uuid.UUID) ([]NudgeItem, string, error) {
	if !s.repos.Enabled() {
		return nil, "", repositories.ErrNoDatabase
	}
	e, err := s.repos.Employees.ByID(ctx, empID)
	if err != nil {
		return nil, "", err
	}
	if e == nil {
		return nil, "", errs.NewNotFoundError(msgNewcomerNotFound)
	}
	list, err := s.repos.Nudges.ListByEmployee(ctx, empID)
	if err != nil {
		return nil, "", err
	}
	evs, err := s.repos.Events.ByEmployee(ctx, empID)
	if err != nil {
		return nil, "", err
	}
	titles := make(map[uuid.UUID]string, len(evs))
	for _, ev := range evs {
		titles[ev.ID] = ev.Title
	}
	out := make([]NudgeItem, 0, len(list))
	for _, n := range list {
		if n.Kind != domain.NudgeAuto && n.Kind != domain.NudgeManual {
			continue // «связался» рекрутера (repositories.NudgeContacted) — не напоминание
		}
		out = append(out, NudgeItem{Nudge: n, Planned: n.MessageID == "", Delivered: n.MessageID != "" && n.MessageID != repositories.NudgeUndelivered})
		if n.EventID != nil {
			out[len(out)-1].EventTitle = titles[*n.EventID]
		}
	}
	return out, s.SiteFor(e).TZ, nil
}
