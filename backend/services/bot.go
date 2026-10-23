package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/flags"
	"main/pkg/logger"
	"main/repositories"

	"github.com/google/uuid"
)

// EchoReply — ответ, когда бот запущен без базы: транспорт проверяется и без сценариев.
const EchoReply = "Корсик на связи"

// editWindow — сообщение старше суток MAX править не даёт (UX-спецификация п. 0, правило 2):
// продолжение опроса приходит новым сообщением.
const editWindow = 24 * time.Hour

// BotService — обработка событий MAX: привязка по приглашению, знакомство, согласие, кнопки
// шагов, живые опросы, свободный текст. Тексты сообщений и payload в лог не пишем: это
// переписка сотрудника и его invite-токен; в лог идут только тип события, id и коды флагов.
type BotService struct {
	max       max.Client
	repos     *repositories.Repos
	scheduler *SchedulerService
	rag       RAGAsker // nil — на вопросы бот отвечает «не могу проверить по документам»
	tickets   *TicketService
	media     MediaAttacher // nil — вложения веток не уходят (branch_media.go)
	now       func() time.Time

	wg sync.WaitGroup // фоновые отправки пакета после согласия
}

func NewBotService(c max.Client, r *repositories.Repos, sch *SchedulerService, now func() time.Time) *BotService {
	if now == nil {
		now = time.Now
	}
	return &BotService{max: c, repos: r, scheduler: sch, now: now}
}

// Wait — дождаться фоновых отправок (тесты, остановка).
func (b *BotService) Wait() { b.wg.Wait() }

// Handle — один апдейт; подходит как max.Handler для Poller.
func (b *BotService) Handle(ctx context.Context, u max.Update) {
	if !b.repos.Enabled() {
		b.echo(ctx, u)
		return
	}
	var err error
	switch u.Type {
	case max.UpdateBotStarted:
		err = b.started(ctx, u)
	case max.UpdateMessageCallback:
		err = b.callback(ctx, u)
	case max.UpdateMessageCreated:
		err = b.message(ctx, u)
	case max.UpdateBotStopped:
		err = b.stopped(ctx, u)
	case max.UpdateBotAdded, max.UpdateBotRemoved:
		err = b.botAdded(ctx, u, u.Type == max.UpdateBotRemoved)
	default:
		logger.Info("бот: событие %s пропущено (обработки ещё нет)", u.Type)
	}
	if err != nil {
		logger.Warn("бот: %s user=%d не обработано: %v", u.Type, u.UserID, err)
	}
}

// echo — режим без базы: ответить заглушкой, чтобы было видно, что транспорт жив.
func (b *BotService) echo(ctx context.Context, u max.Update) {
	if u.Type != max.UpdateMessageCreated || u.FromBot {
		logger.Info("бот: %s без базы пропущено", u.Type)
		return
	}
	if _, err := b.max.Send(ctx, max.OutMessage{ChatID: u.ChatID, UserID: u.UserID, Text: EchoReply}); err != nil {
		logger.Warn("бот: ответ в chat=%d не ушёл: %v", u.ChatID, err)
	}
}

func (b *BotService) send(ctx context.Context, m max.OutMessage) {
	if _, err := b.max.Send(ctx, m); err != nil {
		logger.Warn("бот: сообщение user=%d chat=%d не ушло: %v", m.UserID, m.ChatID, err)
	}
}

func (b *BotService) answer(ctx context.Context, callbackID string, a max.Answer) {
	if callbackID == "" { // команда, а не нажатие
		return
	}
	if err := b.max.AnswerCallback(ctx, callbackID, a); err != nil {
		logger.Warn("бот: ответ на нажатие не ушёл: %v", err)
	}
}

// --- bot_started ---

// started — открыта ссылка-приглашение (payload inv_<token>) или бот перезапущен.
func (b *BotService) started(ctx context.Context, u max.Update) error {
	if ok, err := b.managerStart(ctx, u); ok { // ссылка руководителя mgr_<token> (manager_bot.go)
		return err
	}
	to := recipient{ChatID: u.ChatID, UserID: u.UserID}
	now := b.now()
	var emp *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := b.bindByPayload(ctx, tx, u, now)
		if err != nil || e == nil {
			return err
		}
		e.Connection = domain.ConnConnected
		e.LastActivityAt = &now
		if u.ChatID != 0 {
			e.ChatID = &u.ChatID
		}
		emp = e
		return tx.Employees.Save(ctx, e)
	})
	if err != nil {
		return err
	}
	logger.Info("бот: bot_started user=%d, payload %s, привязан %v", u.UserID, presence(u.Payload), emp != nil)
	switch {
	case emp == nil:
		b.send(ctx, inviteBrokenMessage(to))
	case emp.AddressForm == nil:
		b.send(ctx, introMessage(to))
	case emp.Consent == nil:
		b.send(ctx, consentMessage(to))
	default:
		b.send(ctx, to.msg(textWelcomeBack, "", nil))
	}
	return nil
}

// bindByPayload — найти новичка по приглашению или по уже привязанному аккаунту. nil без
// ошибки — ссылка не сработала: токена нет, он просрочен или им уже вошёл другой аккаунт.
func (b *BotService) bindByPayload(ctx context.Context, tx *repositories.Repos, u max.Update, now time.Time) (*domain.Employee, error) {
	token, isInvite := strings.CutPrefix(strings.TrimSpace(u.Payload), InvitePayloadPrefix)
	if !isInvite || token == "" {
		// /start без приглашения: свои — по привязке, чужие — «ссылка не сработала».
		e, err := tx.Employees.ByMaxUser(ctx, u.UserID)
		if err != nil || e == nil {
			return nil, err
		}
		return tx.Employees.ByIDForUpdate(ctx, e.ID)
	}
	inv, err := tx.Employees.InviteByToken(ctx, token)
	if err != nil || inv == nil || inv.ExpiresAt.Before(now) {
		return nil, err
	}
	e, err := tx.Employees.ByIDForUpdate(ctx, inv.EmployeeID)
	if err != nil || e == nil {
		return nil, err
	}
	if e.MaxUserID != nil && *e.MaxUserID != u.UserID {
		return nil, nil // приглашением уже вошёл другой аккаунт
	}
	// Один аккаунт — один активный новичок: тестовый телефон, открывший приглашение другой
	// синтетической карточки, переезжает на неё.
	if err := tx.Employees.UnbindMaxUser(ctx, u.UserID, e.ID); err != nil {
		return nil, err
	}
	e.MaxUserID = &u.UserID
	if inv.UsedAt == nil {
		if err := tx.Employees.MarkInviteUsed(ctx, inv.ID, now); err != nil {
			return nil, err
		}
	}
	return e, nil
}

// stopped — новичок остановил бота: в карточке «отключил бота», без автоэскалации.
func (b *BotService) stopped(ctx context.Context, u max.Update) error {
	return b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := b.lockByUser(ctx, tx, u.UserID)
		if err != nil || e == nil {
			return err
		}
		e.Connection = domain.ConnStopped
		if e.Risk == domain.RiskGreen {
			e.Risk, e.RiskReason = domain.RiskYellow, strPtr("отключил бота")
		}
		return tx.Employees.Save(ctx, e)
	})
}

func (b *BotService) lockByUser(ctx context.Context, tx *repositories.Repos, userID int64) (*domain.Employee, error) {
	e, err := tx.Employees.ByMaxUser(ctx, userID)
	if err != nil || e == nil {
		return nil, err
	}
	return tx.Employees.ByIDForUpdate(ctx, e.ID)
}

// --- message_callback ---

func (b *BotService) callback(ctx context.Context, u max.Update) error {
	if b.groupChat(ctx, u.ChatID) {
		return nil
	}
	if ok, err := b.managerCallback(ctx, u); ok { // кнопки push руководителю (manager_bot.go)
		return err
	}
	emp, err := b.repos.Employees.ByMaxUser(ctx, u.UserID)
	if err != nil {
		return err
	}
	if emp == nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: textInviteBroken})
		return nil
	}
	p := u.Payload
	switch {
	case isQuestionPayload(p):
		return b.questionCallback(ctx, u, emp)
	case isTicketPayload(p):
		return b.ticketCallback(ctx, u, emp)
	case strings.HasPrefix(p, preAccessReason):
		return b.accessReason(ctx, u, emp.ID)
	case p == payAddressTy || p == payAddressVy:
		return b.chooseAddress(ctx, u, emp.ID, strings.TrimPrefix(p, "a:"))
	case p == payConsentYes || p == payConsentNo:
		return b.consent(ctx, u, emp.ID, p == payConsentYes)
	case p == payConsentMore:
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Подробнее — ниже"})
		b.send(ctx, recipientOf(emp).msg(textConsentMore, "", nil))
		return nil
	case p == payHuman || strings.HasPrefix(p, preHuman):
		return b.human(ctx, u, emp, strings.TrimPrefix(p, preHuman))
	case strings.HasPrefix(p, preStepButton):
		return b.stepButton(ctx, u, emp.ID, strings.TrimPrefix(p, preStepButton))
	case strings.HasPrefix(p, preTerminal):
		return b.terminalButton(ctx, u, emp.ID, strings.TrimPrefix(p, preTerminal))
	case strings.HasPrefix(p, preContinue):
		return b.continuePress(ctx, u, emp.ID, strings.TrimPrefix(p, preContinue))
	case strings.HasPrefix(p, preReason), strings.HasPrefix(p, preWrite), strings.HasPrefix(p, preSkip),
		strings.HasPrefix(p, preRemind), strings.HasPrefix(p, preAnswer), strings.HasPrefix(p, preDone):
		return b.surveyCallback(ctx, u, emp.ID)
	}
	logger.Info("бот: неизвестная кнопка от user=%d", u.UserID)
	b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
	return nil
}

// chooseAddress — «На ты» / «На вы»: сообщение 1 правится в итог без клавиатуры, следом —
// согласие (если ещё не дано).
func (b *BotService) chooseAddress(ctx context.Context, u max.Update, id uuid.UUID, form string) error {
	var emp *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil || e == nil {
			return err
		}
		now := b.now()
		e.AddressForm, e.LastActivityAt = &form, &now
		emp = e
		return tx.Employees.Save(ctx, e)
	})
	if err != nil || emp == nil {
		return err
	}
	done := recipientOf(emp).msg(addressChosenText(form, emp.FullName), formatMarkdown, nil)
	b.answer(ctx, u.CallbackID, max.Answer{Notification: "Записал", Message: &done})
	if emp.Consent == nil {
		b.send(ctx, consentMessage(recipientOf(emp)))
	}
	return nil
}

// consent — ответ на сообщение 2. После любого ответа идут созревшие шаги таймлайна; при
// отказе — без опросов.
func (b *BotService) consent(ctx context.Context, u max.Update, id uuid.UUID, given bool) error {
	var emp *domain.Employee
	var next *time.Time // ближайшее неотправленное событие после пропусков
	now := b.now()
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil || e == nil {
			return err
		}
		first := e.Consent == nil
		v := domain.ConsentDeclined
		if given {
			v = domain.ConsentGiven
		}
		e.Consent, e.ConsentAt, e.LastActivityAt = &v, &now, &now
		emp = e
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		if !given {
			if err := b.scheduler.SkipSurveys(ctx, tx, e.ID); err != nil {
				return err
			}
		}
		if first {
			if err := b.scheduler.SkipMissed(ctx, tx, e); err != nil {
				return err
			}
		}
		evs, err := tx.Events.ScheduledOf(ctx, e.ID) // по fire_at
		if err != nil || len(evs) == 0 {
			return err
		}
		next = &evs[0].FireAt
		return nil
	})
	if err != nil || emp == nil {
		return err
	}
	text := consentAnsweredText(emp, given, next, now)
	done := recipientOf(emp).msg(text, "", nil)
	b.answer(ctx, u.CallbackID, max.Answer{Notification: "Записал", Message: &done})

	// Пакет D−3 идёт с паузами 2–3 с — в фоне, чтобы не держать чтение апдейтов остальных.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		if _, err := b.scheduler.DispatchEmployee(context.WithoutCancel(ctx), emp.ID); err != nil {
			logger.Warn("бот: пакет после согласия для %s не ушёл: %v", emp.ID, err)
		}
	}()
	return nil
}

// loadEvent — событие сотрудника под блокировкой, карточка под блокировкой и шаг из снимка
// версии. ev == nil — события нет или оно чужое.
func (b *BotService) loadEvent(ctx context.Context, tx *repositories.Repos, evID, empID uuid.UUID) (*domain.ScheduledEvent, *domain.Employee, domain.ScenarioStep, error) {
	var step domain.ScenarioStep
	e, err := tx.Employees.ByIDForUpdate(ctx, empID)
	if err != nil || e == nil {
		return nil, nil, step, err
	}
	ev, err := tx.Events.ByIDForUpdate(ctx, evID)
	if err != nil || ev == nil || ev.EmployeeID != empID {
		return nil, e, step, err
	}
	if ev.Snapshot != nil { // «Отправить себе»: шаг черновика в самом событии
		return ev, e, domain.ScenarioStep(*ev.Snapshot), nil
	}
	v, err := tx.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion)
	if err != nil || v == nil {
		return nil, e, step, err
	}
	step, ok := v.Step(ev.StepID)
	if !ok {
		return nil, e, step, nil
	}
	return ev, e, step, nil
}

// --- живой опрос ---

// surveyAction — что сделал новичок в опросе.
type surveyAction struct {
	kind  string // answer | reason | write | skip | remind | text
	q     int
	value int
	text  string // свободный текст причины (kind=text)
}

// parseSurveyPayload — payload кнопки опроса в действие и id события.
func parseSurveyPayload(p string) (uuid.UUID, surveyAction, bool) {
	var a surveyAction
	var rest string
	switch {
	case strings.HasPrefix(p, preReason):
		a.kind, rest = "reason", strings.TrimPrefix(p, preReason)
	case strings.HasPrefix(p, preWrite):
		a.kind, rest = "write", strings.TrimPrefix(p, preWrite)
	case strings.HasPrefix(p, preSkip):
		a.kind, rest = "skip", strings.TrimPrefix(p, preSkip)
	case strings.HasPrefix(p, preRemind):
		a.kind, rest = "remind", strings.TrimPrefix(p, preRemind)
	case strings.HasPrefix(p, preAnswer):
		a.kind, rest = "answer", strings.TrimPrefix(p, preAnswer)
	case strings.HasPrefix(p, preDone):
		a.kind, rest = "done", strings.TrimPrefix(p, preDone)
	default:
		return uuid.Nil, a, false
	}
	parts := strings.Split(rest, ":")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, a, false
	}
	a.q = -1
	if len(parts) > 1 {
		if a.q, err = strconv.Atoi(parts[1]); err != nil {
			return uuid.Nil, a, false
		}
	}
	if len(parts) > 2 {
		if a.value, err = strconv.Atoi(parts[2]); err != nil {
			return uuid.Nil, a, false
		}
	}
	return id, a, true
}

// surveyOutcome — что показать после шага опроса.
type surveyOutcome struct {
	notification string
	msg          *max.OutMessage
	messageID    string
	stale        bool // сообщение старше суток: продолжение — новым сообщением
	eventID      uuid.UUID
	flag         *flagInput // текст причины без regex-флагов — на LLM-разметку (bot_flags.go)
	release      bool       // опрос закрыт: следующие реплики блока (branch_flow.go)
	// ветки и сигналы вариантов, правила после опроса (branch_survey.go)
	branch    *domain.Branch
	branchSrc string
	flagged   bool
	freeText  bool
	next      *max.OutMessage // экран опроса после сообщений ветки — новым сообщением
	optFlags  []domain.OptionFlag
	optLabel  string
	flagQ     int
	outcomes  []int
	emp       *domain.Employee
	ev        *domain.ScheduledEvent
	step      domain.ScenarioStep
}

func (b *BotService) surveyCallback(ctx context.Context, u max.Update, empID uuid.UUID) error {
	evID, act, ok := parseSurveyPayload(u.Payload)
	if !ok {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	out, err := b.applySurvey(ctx, empID, evID, act)
	if err != nil {
		return err
	}
	if out.msg == nil || out.stale {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: orDefault(out.notification, "Записал")})
		if out.msg != nil {
			b.resend(ctx, out)
		}
	} else {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: orDefault(out.notification, "Записал"), Message: out.msg})
	}
	b.afterSurvey(ctx, empID, out)
	return nil
}

// resend — продолжение опроса новым сообщением (старое старше суток); id нового — в событие.
func (b *BotService) resend(ctx context.Context, out surveyOutcome) {
	mid, err := b.max.Send(ctx, *out.msg)
	if err != nil {
		logger.Warn("бот: продолжение опроса %s не ушло: %v", out.eventID, err)
		return
	}
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, err := tx.Events.ByIDForUpdate(ctx, out.eventID)
		if err != nil || ev == nil {
			return err
		}
		now := b.now()
		ev.MessageID, ev.SentAt = mid, &now
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		logger.Warn("бот: id сообщения опроса %s не сохранён: %v", out.eventID, err)
	}
}

// applySurvey — один шаг живого опроса под блокировками события и карточки.
func (b *BotService) applySurvey(ctx context.Context, empID, evID uuid.UUID, act surveyAction) (surveyOutcome, error) {
	out := surveyOutcome{eventID: evID}
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		if ev == nil || ev.Status != domain.EventSent || ev.Survey == nil || !isSurvey(step) {
			out.notification = "Этот опрос уже закрыт"
			return nil
		}
		st := ev.Survey
		if act.q >= 0 && act.q != st.Q {
			out.notification = "Этот вопрос уже позади"
			return nil
		}
		sv := step.Survey
		q := sv.Questions[st.Q]
		qi := st.Q
		now := b.now()
		e.LastActivityAt = &now
		answer := domain.SurveyAnswer{
			EmployeeID: e.ID, EventID: ev.ID, QuestionNo: st.Q + 1, Scale: qScale(sv, q),
			Flags: domain.StringList{}, IsTest: ev.IsTest,
		}
		if prev, err := tx.Events.Answer(ctx, ev.ID, st.Q+1); err != nil {
			return err
		} else if prev != nil {
			answer = *prev
		}
		saveAnswer := true

		switch act.kind {
		case "answer":
			if st.Screen != domain.ScreenQuestion || !validValue(sv, q, act.value) {
				out.notification = "Выберите вариант на кнопках"
				return nil
			}
			v := act.value
			if qScale(sv, q) == qMulti { // переключатель: вопрос остаётся на экране до «Готово»
				for len(st.Results) <= st.Q {
					st.Results = append(st.Results, domain.SurveyResult{})
				}
				st.Results[st.Q].Multi = toggle(st.Results[st.Q].Multi, v)
				saveAnswer = false
				out.notification = "Отмечено"
				break
			}
			setResult(st, domain.SurveyResult{Value: &v})
			answer.Value, answer.Skipped = &v, false
			if scaleMax(qScale(sv, q)) > 0 {
				e.LastPulse = &v
			}
			if lowValue(sv, q, v) {
				markRisk(e, domain.RiskYellow, fmt.Sprintf("оценка %s в «%s»", valueLabel(sv, q, v), step.Title))
			}
			switch scr := commentScreen(sv, q, v); scr {
			case domain.ScreenReasons, screenComment:
				st.Screen = scr
			case domain.ScreenWrite:
				st.Screen = domain.ScreenWrite
				e.AwaitEventID, e.AwaitQuestion = &ev.ID, st.Q
			default:
				advance(st)
			}
			// ветка и сигнал варианта (06 §1.1, §1.3)
			if br := optionBranch(q, v); br != nil {
				out.branch, out.branchSrc = br, branchSrcOption(qi, v)
				out.flagged, out.freeText = applySilent(e, br.Actions, ev.ID, valueLabel(sv, q, v), nil)
			}
			if f, ok := q.OptionFlags[strconv.Itoa(v)]; ok && f.Code != "" {
				out.optFlags, out.flagQ = append(out.optFlags, f), qi+1
				out.optLabel = valueLabel(sv, q, v)
			}
		case "done":
			if st.Screen != domain.ScreenQuestion || qScale(sv, q) != qMulti {
				out.notification = "Этот вопрос уже позади"
				return nil
			}
			var picks []int
			if st.Q < len(st.Results) {
				picks = st.Results[st.Q].Multi
			}
			if len(picks) == 0 {
				out.notification = "Отметьте хотя бы один вариант"
				return nil
			}
			answer.Reason, answer.Skipped = strings.Join(multiLabels(q, picks), "; "), false
			for _, v := range picks {
				if f, ok := q.OptionFlags[strconv.Itoa(v)]; ok && f.Code != "" {
					out.optFlags, out.flagQ = append(out.optFlags, f), qi+1
					out.optLabel = strings.Join(multiLabels(q, []int{v}), "")
				}
			}
			advance(st)
		case "reason":
			reasons := q.Options
			if len(reasons) == 0 || sv.Scale == domain.ScaleChoice {
				reasons = defaultReasons
			}
			if st.Screen != domain.ScreenReasons || act.value < 0 || act.value >= len(reasons) {
				out.notification = "Выберите вариант на кнопках"
				return nil
			}
			answer.Reason = reasons[act.value]
			if len(st.Results) > st.Q {
				st.Results[st.Q].Reason = reasons[act.value]
			}
			advance(st)
		case "write":
			if st.Screen == domain.ScreenWrite {
				return nil
			}
			st.Screen = domain.ScreenWrite
			e.AwaitEventID, e.AwaitQuestion = &ev.ID, st.Q
			saveAnswer = false
		case "text":
			if st.Screen != domain.ScreenWrite && st.Screen != domain.ScreenReasons && st.Screen != screenComment && qType(q) != qText {
				return nil
			}
			for len(st.Results) <= st.Q {
				st.Results = append(st.Results, domain.SurveyResult{})
			}
			answer.Text = act.text
			codes := flags.Codes(act.text)
			if codes != nil {
				answer.Flags = codes
				markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в ответе на опрос")
			} else {
				evID := ev.ID
				out.flag = &flagInput{text: act.text, context: rag.FlagsContextPulse(ev.DayOffset), surveyQuestion: q.Text,
					eventID: &evID, questionNo: st.Q + 1, where: "в ответе на опрос «" + step.Title + "»"}
			}
			if len(st.Results) > st.Q {
				st.Results[st.Q].Wrote = true
			}
			advance(st)
		case "skip":
			mandatory := (st.Screen == domain.ScreenQuestion && q.Required) ||
				(st.Screen == domain.ScreenWrite && (q.Comment == "below" || q.Comment == "on_no"))
			if mandatory {
				out.notification = "Этот вопрос обязательный"
				return nil
			}
			if st.Screen == domain.ScreenQuestion {
				setResult(st, domain.SurveyResult{Skipped: true})
				answer.Skipped = true
			} else {
				saveAnswer = false // причину пропустили, оценка остаётся
			}
			advance(st)
		case "remind":
			saveAnswer = false
			st.Reminders++
			if st.Reminders > maxReminders {
				ev.Status, ev.Answer = domain.EventSkipped, strPtr("не отвечен: напоминания исчерпаны")
				m := recipientOf(e).msg(textRemindStopped, "", nil)
				out.msg = &m
			} else {
				site := domain.SiteOf(e.Site)
				ev.Status = domain.EventScheduled
				ev.FireAt = NextWorkdayAt(now, step.Time, site, step.WorkHoursOnly)
				m := recipientOf(e).msg(textRemindLater, "", nil)
				out.msg = &m
			}
		}
		if act.kind != "write" && st.Screen != domain.ScreenWrite {
			e.AwaitEventID, e.AwaitQuestion = nil, 0
		}
		if st.Q < len(sv.Questions) && st.Screen == domain.ScreenQuestion && qType(sv.Questions[st.Q]) == qText && ev.Status == domain.EventSent {
			e.AwaitEventID, e.AwaitQuestion = &ev.ID, st.Q // вопрос «своими словами»: ждём текст
		}
		if saveAnswer {
			if err := tx.Events.UpsertAnswer(ctx, &answer); err != nil {
				return err
			}
		}
		if ev.Status == domain.EventSent && st.Q >= len(sv.Questions) {
			ev.Status, ev.Answer = domain.EventAnswered, strPtr(surveySummary(sv, st))
		}
		if ev.Status == domain.EventAnswered || ev.Status == domain.EventSkipped {
			out.release = ev.Waiting != ""
			ev.Waiting = ""
		}
		if ev.Status == domain.EventAnswered {
			out.outcomes = evalOutcomes(sv, st)
		}
		if out.msg == nil {
			m := surveyMessage(recipientOf(e), step, e, ev, sentAt(ev, now))
			out.msg = &m
			if out.branch != nil && ev.Status == domain.EventSent {
				// ветка варианта: итоги без вопроса → сообщения ветки → вопрос новым сообщением
				out.next = out.msg
				frozen := *ev
				fs := *st
				fs.Screen = screenFrozen
				frozen.Survey = &fs
				f := surveyMessage(recipientOf(e), step, e, &frozen, sentAt(ev, now))
				out.msg = &f
			}
		}
		ec, evc := *e, *ev
		out.emp, out.ev, out.step = &ec, &evc, step
		out.messageID = ev.MessageID
		out.stale = ev.SentAt != nil && now.Sub(*ev.SentAt) > editWindow
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		return tx.Events.Save(ctx, ev)
	})
	return out, err
}

// setResult — итог текущего вопроса (повторный ответ перезаписывает).
func setResult(st *domain.SurveyState, r domain.SurveyResult) {
	for len(st.Results) <= st.Q {
		st.Results = append(st.Results, domain.SurveyResult{})
	}
	st.Results[st.Q] = r
}

func advance(st *domain.SurveyState) {
	st.Q++
	st.Screen = domain.ScreenQuestion
}

// markRisk — поднять риск (вниз не опускаем: это решает HR).
func markRisk(e *domain.Employee, level, reason string) {
	rank := map[string]int{domain.RiskGreen: 0, "": 0, domain.RiskYellow: 1, domain.RiskRed: 2}
	if rank[level] >= rank[e.Risk] {
		e.Risk, e.RiskReason = level, &reason
	}
}

// surveySummary — ответ опроса одной строкой для таймлайна кабинета.
func surveySummary(sv *domain.Survey, st *domain.SurveyState) string {
	var parts []string
	for i, r := range st.Results {
		switch {
		case r.Skipped:
			parts = append(parts, "—")
		case len(r.Multi) > 0 && i < len(sv.Questions):
			parts = append(parts, strings.Join(multiLabels(sv.Questions[i], r.Multi), ", "))
		case r.Value == nil && r.Wrote:
			parts = append(parts, "свой ответ")
		case r.Value != nil:
			s := strconv.Itoa(*r.Value)
			sc := sv.Scale
			if i < len(sv.Questions) {
				sc = qScale(sv, sv.Questions[i])
			}
			switch sc {
			case domain.Scale10, domain.Scale5:
				s = fmt.Sprintf("%d/%d", *r.Value, scaleMax(sc))
			case domain.ScaleYesNo:
				s = map[int]string{1: "да", 0: "нет"}[*r.Value]
			case domain.ScaleChoice:
				if i < len(sv.Questions) && *r.Value >= 1 && *r.Value <= len(sv.Questions[i].Options) {
					s = sv.Questions[i].Options[*r.Value-1]
				}
			}
			if r.Reason != "" {
				s += " (" + r.Reason + ")"
			} else if r.Wrote {
				s += " (причина своими словами)"
			}
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "; ")
}

// --- message_created ---

// message — свободный текст. Порядок: команды (/help, /menu, /human) → ждём причину к опросу →
// переписка обращения (human_active, «Добавить детали») → оценка текстом в открытый опрос → ждём
// детали к сигналу флага → вопрос (bot_ask.go: regex-флаги, иначе rag).
func (b *BotService) message(ctx context.Context, u max.Update) error {
	if u.FromBot || b.groupChat(ctx, u.ChatID) {
		return nil
	}
	emp, err := b.repos.Employees.ByMaxUser(ctx, u.UserID)
	if err != nil {
		return err
	}
	to := recipient{ChatID: u.ChatID, UserID: u.UserID}
	if emp == nil {
		if strings.TrimSpace(u.Text) == "" {
			b.send(ctx, to.msg(textContactNoted, "", nil)) // контакт по кнопке «Проверить по телефону»
		} else {
			b.send(ctx, inviteBrokenMessage(to))
		}
		return nil
	}
	codes := flags.Codes(u.Text)
	logger.Info("бот: текст от новичка %s, флаги %v, аудио %d", emp.ID, codes, len(u.Audio))

	// Команды работают всегда, и в переписке с HR тоже (UX 2.7).
	if ok, err := b.command(ctx, u, emp); ok || err != nil {
		return err
	}

	// «Другое» / open_free_text ветки: текст → флаги → обращение (branch.go).
	if w := emp.AwaitFreeText; w != nil && w.Input && (strings.TrimSpace(u.Text) != "" || len(u.Images) > 0) {
		return b.inputAnswer(ctx, emp, u)
	}
	if emp.AwaitFreeText != nil && strings.TrimSpace(u.Text) != "" {
		return b.freeText(ctx, emp, u.Text)
	}
	if emp.AwaitEventID != nil && strings.TrimSpace(u.Text) != "" {
		out, err := b.applySurvey(ctx, emp.ID, *emp.AwaitEventID, surveyAction{kind: "text", q: emp.AwaitQuestion, text: u.Text})
		if err != nil {
			return err
		}
		b.deliverEdit(ctx, out)
		if out.flag != nil {
			b.classifyAsync(ctx, emp, *out.flag)
		}
		b.afterSurvey(ctx, emp.ID, out)
		return nil
	}
	// Обращение в human_active или «Добавить детали»: текст — человеку, не в RAG (UX 2.7).
	if ok, err := b.ticketConversation(ctx, u, emp); ok || err != nil {
		return err
	}
	if v, ok := parseScore(u.Text); ok {
		open, err := b.repos.Events.OpenSurvey(ctx, emp.ID)
		if err != nil {
			return err
		}
		if open != nil && open.Survey != nil && open.Survey.Screen == domain.ScreenQuestion {
			out, err := b.applySurvey(ctx, emp.ID, open.ID, surveyAction{kind: "answer", q: open.Survey.Q, value: v})
			if err != nil {
				return err
			}
			if out.msg != nil {
				b.deliverEdit(ctx, out)
				b.afterSurvey(ctx, emp.ID, out)
				return nil
			}
		}
	}

	if ok, err := b.flagDetails(ctx, u, emp); ok || err != nil {
		return err
	}
	return b.question(ctx, u, emp)
}

// deliverEdit — показать новый экран опроса без нажатия кнопки: правкой сообщения опроса, а
// если оно старше суток — новым сообщением.
func (b *BotService) deliverEdit(ctx context.Context, out surveyOutcome) {
	if out.msg == nil {
		return
	}
	if out.stale || out.messageID == "" {
		b.resend(ctx, out)
		return
	}
	if err := b.max.Edit(ctx, out.messageID, *out.msg); err != nil {
		logger.Warn("бот: правка опроса %s не ушла: %v", out.eventID, err)
	}
}

func presence(s string) string {
	if s == "" {
		return "пуст"
	}
	return "есть"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
