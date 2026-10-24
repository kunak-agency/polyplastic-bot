package services

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// TicketService — обращения (итерация 3, api_contract.md v2): создание из бота, маршрут,
// приоритет и SLA по data/taxonomy.yaml, ответ HR новичку через бота, уведомления в HR-чат и
// чат СБ.
//
// ПДн: в лог идут только номер, категория, маршрут и id — ни текстов новичка, ни текстов HR.
// Цитата хранится только при согласии новичка и только у категорий со store_quote (F10 — без
// цитаты). Сводку пишет код, текста новичка в ней нет.
type TicketService struct {
	repos  *repositories.Repos
	max    max.Client // nil — бот выключен: ответы HR не уходят (409)
	tax    *taxonomy.Taxonomy
	now    func() time.Time
	hrChat int64
	sbChat int64
	// replies — FEATURE_REPLIES: подтверждение обращения и ответ HR — реплаем на сообщение новичка.
	replies bool
}

// TicketView — обращение для кабинета: с именем и площадкой новичка, переписка — в карточке.
type TicketView struct {
	Ticket       domain.Ticket
	NewcomerName string
	Site         string
	Messages     []domain.TicketMessage
	// CategoryName — заголовок категории из data/taxonomy.yaml («Нет доступов, техники…»).
	CategoryName string
	// Tone — тон исходного текста новичка (pos | neu | neg), только в карточке; nil — не размечен.
	Tone *string
	// SummaryModel — однострочная сводка модели (/flags v2, агент H) — заголовок карточки; nil —
	// модели нет или она промолчала, тогда заголовок — категория.
	SummaryModel *string
}

// TicketFilter — GET /api/tickets.
type TicketFilter = repositories.TicketFilter

// Отказы кабинету.
const (
	msgTicketNotFound   = "Обращение не найдено"
	msgTicketResolved   = "Обращение уже закрыто"
	msgReassignComment  = "Переназначение — с комментарием: почему меняется исполнитель"
	msgTicketBotOff     = "Бот выключен — сообщение в MAX не уйдёт"
	msgTicketNoChat     = "Новичок не подключён к боту — ответ некуда отправить"
	msgTicketSendFailed = "MAX не принял сообщение — повторите позже"
	msgNotAssigned      = "Сначала назначьте исполнителя: его имя станет подписью ответа"
	msgManagerHidden    = "Эта категория руководителю не показывается — уведомить его нельзя"
)

// CodeTicketNotAssigned — 409 на reply без исполнителя («Уточнения v2»).
const CodeTicketNotAssigned = "ticket_not_assigned"

// maxQuoteLen — потолок цитаты новичка в карточке.
const maxQuoteLen = 1000

// contextWindow — «последние сообщения» новичка боту, которые попадают в переписку обращения.
const (
	contextWindow = 24 * time.Hour
	contextLimit  = 3
)

func (s *TicketService) ready() error {
	if !s.repos.Enabled() {
		return repositories.ErrNoDatabase
	}
	return nil
}

// category — категория обращения: question — по маршруту, F-коды — из таксономии.
func (s *TicketService) category(code, route string) taxonomy.Category {
	if code == taxonomy.CategoryQuestion {
		return taxonomy.Question(route)
	}
	if c, ok := s.tax.Get(code); ok {
		return c
	}
	return taxonomy.Question(route)
}

// --- создание из бота ---

// ticketDraft — что бот знает об обращении в момент передачи.
type ticketDraft struct {
	cat            taxonomy.Category
	source         string
	what           string // для сводки: что случилось, своими словами кода
	quote          string // текст новичка — цитата (фильтруется согласием и store_quote)
	humanRequestID *uuid.UUID
	questionID     *uuid.UUID
	eventID        *uuid.UUID
	withContext    bool // приложить последние вопросы новичка боту
}

// consented — новичок согласился на обработку сообщений.
func consented(e *domain.Employee) bool {
	return e.Consent != nil && *e.Consent == domain.ConsentGiven
}

// keepText — текст новичка, который можно сохранить в обращении: при согласии и если категория
// разрешает цитаты.
func keepText(e *domain.Employee, cat taxonomy.Category, text string) string {
	text = strings.TrimSpace(text)
	if !consented(e) || !cat.StoreQuote || text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) > maxQuoteLen {
		text = string([]rune(text)[:maxQuoteLen-1]) + "…"
	}
	return text
}

// open — в транзакции tx: новое обращение или уже открытое той же категории (тогда к нему
// дописывается контекст). created=false — обращение уже было.
func (s *TicketService) open(ctx context.Context, tx *repositories.Repos, e *domain.Employee, d ticketDraft) (*domain.Ticket, bool, error) {
	cur, err := tx.Tickets.Active(ctx, e.ID, d.cat.Code)
	if err != nil {
		return nil, false, err
	}
	// Открытое обращение той же категории, но другому адресату (вопрос HR после ввода r34, а
	// новичок жмёт «Описать проблему» рекрутеру) — не дописываем: у рекрутера своё обращение.
	if cur != nil && d.cat.Route != "" && cur.Route != "" && cur.Route != d.cat.Route {
		cur = nil
	}
	if cur != nil {
		if q := keepText(e, d.cat, d.quote); q != "" {
			if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: cur.ID, From: domain.TicketMsgNewcomer, Text: q}); err != nil {
				return nil, false, err
			}
		}
		return cur, false, nil
	}
	now := s.now()
	site := domain.SiteOf(e.Site)
	t := &domain.Ticket{
		EmployeeID: e.ID, Day: DayNumber(e.StartDate, now, site), Category: d.cat.Code, Priority: d.cat.Priority,
		Status: domain.TicketOpen, Route: d.cat.Route, Restricted: d.cat.Restricted, NotifyManager: d.cat.NotifyManager,
		Source: d.source, HumanRequestID: d.humanRequestID, QuestionID: d.questionID, EventID: d.eventID, IsTest: e.IsTest,
	}
	t.CreatedAt = now
	due := SLADue(now, t.Priority, site)
	t.SLADueAt = &due
	if q := keepText(e, d.cat, d.quote); q != "" {
		t.Quote = &q
	}

	// Последние вопросы новичка боту — в переписку (кроме того, из которого передали: он — цитата).
	var context []domain.TicketMessage
	if d.withContext && consented(e) && d.cat.StoreQuote {
		recent, err := tx.Questions.Recent(ctx, e.ID, now.Add(-contextWindow), contextLimit)
		if err != nil {
			return nil, false, err
		}
		for _, q := range recent {
			if q.Question == "" || (d.questionID != nil && q.ID == *d.questionID) {
				continue
			}
			context = append(context, domain.TicketMessage{From: domain.TicketMsgNewcomer, Text: q.Question})
		}
	}
	t.Summary = ticketSummary(d.cat, d.what, t.Day, len(context))
	if err := tx.Tickets.Create(ctx, t); err != nil {
		return nil, false, err
	}
	if t.Quote != nil {
		context = append(context, domain.TicketMessage{From: domain.TicketMsgNewcomer, Text: *t.Quote})
	}
	// автор строки — бот («Корсик» в кабинете), поэтому без «Корсик:» в тексте
	context = append(context, domain.TicketMessage{From: domain.TicketMsgBot, Text: "Передал обращение: " + d.what})
	for i := range context {
		context[i].TicketID = t.ID
		if err := tx.Tickets.AddMessage(ctx, &context[i]); err != nil {
			return nil, false, err
		}
	}
	logger.Info("обращение #%d создано: %s %s %s, новичок %s, источник %s", t.Number, t.Category, t.Priority, t.Route, e.ID, t.Source)
	return t, true, nil
}

// notify — карточка в HR-чат (F5/F9 — в чат СБ). На подделке MAX и без chat_id — только строка
// в лог: номер, категория, куда. Текстов новичка там нет.
func (s *TicketService) notify(ctx context.Context, t *domain.Ticket, e *domain.Employee) {
	cat := s.category(t.Category, t.Route)
	chat, where := s.hrChat, "HR-чат"
	if cat.SecurityChat {
		chat, where = s.sbChat, "чат СБ"
	}
	_, fake := s.max.(*max.Fake)
	switch {
	case s.max == nil || fake:
		logger.Info("обращение #%d %s → %s: подделка MAX, карточка только в лог", t.Number, t.Category, where)
	case chat == 0:
		logger.Warn("обращение #%d %s → %s: chat_id не задан (MAX_HR_CHAT_ID / MAX_SB_CHAT_ID), карточка не ушла", t.Number, t.Category, where)
		return
	default:
		text := ticketCard(t, cat, e, domain.SiteOf(e.Site))
		if _, err := s.max.Send(ctx, max.OutMessage{ChatID: chat, Text: text}); err != nil {
			logger.Warn("обращение #%d → %s: карточка не ушла: %v", t.Number, where, err)
			return
		}
		logger.Info("обращение #%d %s → %s: карточка ушла", t.Number, t.Category, where)
	}
	if t.NotifyManager {
		logger.Info("обращение #%d: руководителю — уведомление без текста (канала к руководителю в MAX пока нет)", t.Number)
	}
	if err := s.repos.Tickets.MarkNotified(ctx, t.ID, s.now()); err != nil {
		logger.Warn("обращение #%d: notified_at не записан: %v", t.Number, err)
	}
}

// sendOpen — OPEN новичку новым сообщением; его mid — для правки в ASSIGNED.
// replyTo — сообщение новичка, на которое OPEN уходит реплаем (FEATURE_REPLIES); пусто — без.
func (s *TicketService) sendOpen(ctx context.Context, e *domain.Employee, t *domain.Ticket, lead, replyTo string) {
	if s.max == nil {
		return
	}
	cat := s.category(t.Category, t.Route)
	if t.Route != "" && !cat.SecurityChat && !cat.PartnerOnly {
		cat.Route = t.Route // «Описать проблему» рекрутеру с флагом F2: «Передал рекрутеру», не «HR»
	}
	off := !InWorkHours(s.now(), domain.SiteOf(e.Site))
	m := recipientOf(e).msg(ticketOpenText(e, t, cat, off, lead), formatMarkdown, ticketOpenButtons(t))
	m.ReplyTo = replyTo
	s.sendStatus(ctx, e, t, m, false)
}

// sendStatus — статус новичку: правкой сообщения статуса, пока ему меньше суток (edit=true), иначе
// новым сообщением. Текст статуса пишется в переписку от бота.
func (s *TicketService) sendStatus(ctx context.Context, e *domain.Employee, t *domain.Ticket, m max.OutMessage, edit bool) {
	if s.max == nil || (m.ChatID == 0 && m.UserID == 0) {
		return
	}
	now := s.now()
	if edit && t.StatusMessageID != "" && t.StatusSentAt != nil && now.Sub(*t.StatusSentAt) < editWindow {
		if err := s.max.Edit(ctx, t.StatusMessageID, m); err != nil {
			logger.Warn("обращение #%d: правка статуса не ушла: %v", t.Number, err)
		}
		return
	}
	mid, err := sendReply(ctx, s.max, s.replies, m)
	if err != nil {
		logger.Warn("обращение #%d: статус новичку не ушёл: %v", t.Number, err)
		return
	}
	t.StatusMessageID, t.StatusSentAt = mid, &now
	if err := s.repos.Tickets.SetStatusMessage(ctx, t.ID, mid, now); err != nil {
		logger.Warn("обращение #%d: mid статуса не записан: %v", t.Number, err)
	}
}

// --- кабинет (REST v2) ---

// List — обращения по фильтру.
func (s *TicketService) List(ctx context.Context, f TicketFilter) ([]TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	list, err := s.repos.Tickets.List(ctx, f)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]*domain.Employee{}
	out := make([]TicketView, 0, len(list))
	for _, t := range list {
		e, ok := names[t.EmployeeID]
		if !ok {
			if e, err = s.repos.Employees.ByID(ctx, t.EmployeeID); err != nil {
				return nil, err
			}
			names[t.EmployeeID] = e
		}
		v := TicketView{Ticket: t, CategoryName: s.category(t.Category, t.Route).Name}
		if e != nil {
			v.NewcomerName, v.Site = e.FullName, e.Site
		}
		out = append(out, v)
	}
	return out, nil
}

// Get — обращение с перепиской.
func (s *TicketService) Get(ctx context.Context, id uuid.UUID) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	t, err := s.repos.Tickets.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errs.NewNotFoundError(msgTicketNotFound)
	}
	return s.view(ctx, s.repos, t)
}

func (s *TicketService) view(ctx context.Context, r *repositories.Repos, t *domain.Ticket) (*TicketView, error) {
	v := &TicketView{Ticket: *t, CategoryName: s.category(t.Category, t.Route).Name}
	e, err := r.Employees.ByID(ctx, t.EmployeeID)
	if err != nil {
		return nil, err
	}
	if e != nil {
		v.NewcomerName, v.Site = e.FullName, e.Site
	}
	if v.Messages, err = r.Tickets.Messages(ctx, t.ID); err != nil {
		return nil, err
	}
	v.Tone = s.tone(ctx, r, t)
	v.SummaryModel = modelSummary(t)
	return v, nil
}

// modelSummary — сводка модели для заголовка карточки. Поле у обращения добавляет агент H
// (волна 4, #24); пока его нет — nil, и кабинет показывает категорию.
func modelSummary(t *domain.Ticket) *string {
	return t.SummaryModel
}

// tone — тон текста, из которого выросло обращение (подсказка в карточке, #26): свободный вопрос
// (QuestionID) или ответы опроса (EventID, самый напряжённый из размеченных). nil — не размечен.
// Тон в маршруте и приоритете не участвует.
func (s *TicketService) tone(ctx context.Context, r *repositories.Repos, t *domain.Ticket) *string {
	if t.QuestionID != nil {
		if q, err := r.Questions.ByID(ctx, *t.QuestionID); err == nil && q != nil && q.Sentiment != nil {
			return q.Sentiment
		}
	}
	if t.EventID == nil {
		return nil
	}
	answers, err := r.Events.Answers(ctx, *t.EventID)
	if err != nil {
		return nil
	}
	rank := map[string]int{domain.SentimentPos: 1, domain.SentimentNeu: 2, domain.SentimentNeg: 3}
	var best *string
	for _, a := range answers {
		if a.Sentiment != nil && rank[*a.Sentiment] > 0 && (best == nil || rank[*a.Sentiment] > rank[*best]) {
			v := *a.Sentiment
			best = &v
		}
	}
	return best
}

// locked — обращение и новичок под блокировкой; закрытое — 409.
func (s *TicketService) locked(ctx context.Context, tx *repositories.Repos, id uuid.UUID) (*domain.Ticket, *domain.Employee, error) {
	t, err := tx.Tickets.ByIDForUpdate(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return nil, nil, errs.NewNotFoundError(msgTicketNotFound)
	}
	if t.Status == domain.TicketResolved {
		return nil, nil, errs.NewConflictError(msgTicketResolved)
	}
	e, err := tx.Employees.ByID(ctx, t.EmployeeID)
	if err != nil {
		return nil, nil, err
	}
	if e == nil {
		return nil, nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	return t, e, nil
}

// AssignInput — POST /api/tickets/{id}/assign. AssigneeID — пользователь (recruiter | hr_lead,
// активный); Name — имя без входа (совместимость при выключенном FEATURE_AUTH).
type AssignInput struct {
	AssigneeID *uuid.UUID
	Name       string
	Comment    string
}

// Отказы назначения.
const (
	msgAssigneeNotFound = "Исполнитель не найден"
	msgAssigneeRole     = "Исполнитель — активный рекрутер или руководитель HR"
)

// resolveAssignee — кому назначить. С входом: assignee_id → этот пользователь; без id и имени
// («Взять в работу», тело пустое) или с именем вошедшего → он сам; другое имя — активный
// recruiter/hr_lead с таким full_name. Без входа: assignee_id → пользователь, иначе имя как есть.
func (s *TicketService) resolveAssignee(ctx context.Context, in AssignInput) (*uuid.UUID, string, error) {
	field := func(msg string) error {
		return errs.NewValidationError(msg).
			WithDetails(errs.FieldError{Field: "assignee_id", Code: errs.CodeOutOfRange, Message: msg})
	}
	if in.AssigneeID != nil {
		u, err := s.repos.Users.ByID(ctx, *in.AssigneeID)
		if err != nil {
			return nil, "", err
		}
		if u == nil {
			return nil, "", field(msgAssigneeNotFound)
		}
		if !canAssign(u) {
			return nil, "", field(msgAssigneeRole)
		}
		id := u.ID
		return &id, userName(u), nil
	}
	v := ViewerFrom(ctx)
	if v == nil {
		if strings.TrimSpace(in.Name) == "" {
			return nil, "", errs.NewValidationError(errs.MsgInvalidFields).
				WithDetails(errs.FieldError{Field: "assignee_name", Code: errs.CodeRequired, Message: "Обязательное поле"})
		}
		return nil, in.Name, nil
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || name == userName(v) {
		if !canAssign(v) {
			return nil, "", field(msgAssigneeRole)
		}
		id := v.ID
		return &id, userName(v), nil
	}
	list, err := s.repos.Users.List(ctx)
	if err != nil {
		return nil, "", err
	}
	for i := range list {
		if u := &list[i]; canAssign(u) && userName(u) == name {
			id := u.ID
			return &id, userName(u), nil
		}
	}
	return nil, "", field(msgAssigneeNotFound)
}

// userName — full_name пользователя, пустое — логин.
func userName(u *domain.User) string {
	if n := strings.TrimSpace(u.FullName); n != "" {
		return n
	}
	return u.LoginOr("")
}

// Assignees — кому можно назначить обращение: активные recruiter и hr_lead по имени.
func (s *TicketService) Assignees(ctx context.Context) ([]domain.User, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	list, err := s.repos.Users.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.User, 0, len(list))
	for _, u := range list {
		if canAssign(&u) {
			out = append(out, u)
		}
	}
	return out, nil
}

// sameAssignee — обращение уже за этим исполнителем: по id, если он есть у обоих, иначе по имени.
func sameAssignee(t *domain.Ticket, id *uuid.UUID, name string) bool {
	if t.AssigneeID != nil && id != nil {
		return *t.AssigneeID == *id
	}
	return t.AssigneeName != nil && *t.AssigneeName == name
}

// Assign — «Взять в работу» / «Назначить»: open → assigned; в human_active статус остаётся
// (диалог уже идёт). Переназначение другому — только с комментарием. Новичку — ASSIGNED правкой
// сообщения статуса. Автор записи в переписке — вошедший пользователь (без входа — исполнитель).
func (s *TicketService) Assign(ctx context.Context, id uuid.UUID, in AssignInput) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	assigneeID, name, err := s.resolveAssignee(ctx, in)
	if err != nil {
		return nil, err
	}
	comment := in.Comment
	var t *domain.Ticket
	var e *domain.Employee
	var view *TicketView
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if t, e, err = s.locked(ctx, tx, id); err != nil {
			return err
		}
		reassign := t.AssigneeName != nil && !sameAssignee(t, assigneeID, name)
		if reassign && strings.TrimSpace(comment) == "" {
			return errs.NewValidationError(msgReassignComment).
				WithDetails(errs.FieldError{Field: "comment", Code: errs.CodeRequired, Message: msgReassignComment})
		}
		note := "Исполнитель: " + name
		if reassign {
			note = "Переназначено: " + *t.AssigneeName + " → " + name
		}
		if c := strings.TrimSpace(comment); c != "" {
			note += ". " + c
		}
		t.AssigneeID, t.AssigneeName = assigneeID, &name
		if t.Status == domain.TicketOpen {
			t.Status = domain.TicketAssigned
		}
		if err := tx.Tickets.Save(ctx, t); err != nil {
			return err
		}
		author := actorOr(ctx, name)
		if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgStaff, Author: &author, Text: note, Internal: true}); err != nil {
			return err
		}
		view, err = s.view(ctx, tx, t)
		return err
	})
	if err != nil {
		return nil, err
	}
	logger.Info("обращение #%d: назначено, статус %s", t.Number, t.Status)
	if t.Status == domain.TicketAssigned {
		cat := s.category(t.Category, t.Route)
		m := recipientOf(e).msg(ticketAssignedText(e, t, cat), formatMarkdown, ticketOpenButtons(t))
		s.sendStatus(ctx, e, t, m, true)
	}
	return view, nil
}

// staffAuthor — автор записи сотрудника: вошедший пользователь; без входа — исполнитель или «HR».
func staffAuthor(ctx context.Context, t *domain.Ticket) string {
	fallback := "HR"
	if t.AssigneeName != nil && strings.TrimSpace(*t.AssigneeName) != "" {
		fallback = *t.AssigneeName
	}
	if n := actorOr(ctx, fallback); n != "" {
		return n
	}
	return fallback
}

// Reply — ответ HR новичку через бота с подписью: статус human_active, текст новичка дальше идёт
// в обращение, а не в RAG. resolve — сразу закрыть.
func (s *TicketService) Reply(ctx context.Context, id uuid.UUID, text string, resolve bool) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if s.max == nil {
		return nil, errs.NewConflictError(msgTicketBotOff)
	}
	t, e, err := s.locked(ctx, s.repos, id)
	if err != nil {
		return nil, err
	}
	if t.AssigneeName == nil || strings.TrimSpace(*t.AssigneeName) == "" {
		return nil, errs.NewConflictError(msgNotAssigned).WithCode(CodeTicketNotAssigned)
	}
	to := recipientOf(e)
	if to.ChatID == 0 && to.UserID == 0 {
		return nil, errs.NewConflictError(msgTicketNoChat)
	}
	cat := s.category(t.Category, t.Route)
	// Отправка — до записи: «отправлено» в кабинете должно значить «ушло в MAX». Реплаем — на
	// сообщение новичка, из которого выросло обращение (FEATURE_REPLIES).
	m := staffReplyMessage(to, t, cat, staffAuthor(ctx, t), text)
	m.ReplyTo = s.sourceMID(ctx, t)
	if _, err := sendReply(ctx, s.max, s.replies, m); err != nil {
		logger.Warn("обращение #%d: ответ HR не ушёл в MAX: %v", t.Number, err)
		return nil, errs.NewBadGatewayError(msgTicketSendFailed).WithCause(err)
	}
	var view *TicketView
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		cur, _, err := s.locked(ctx, tx, id)
		if err != nil {
			return err
		}
		author := staffAuthor(ctx, cur)
		if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: cur.ID, From: domain.TicketMsgStaff, Author: &author, Text: text}); err != nil {
			return err
		}
		// SLA — до первого ответа человека: в human_active таймер остановлен.
		cur.Status, cur.AwaitDetails, cur.SLADueAt = domain.TicketHumanActive, false, nil
		if resolve {
			s.markResolved(cur)
		}
		if err := tx.Tickets.Save(ctx, cur); err != nil {
			return err
		}
		t = cur
		view, err = s.view(ctx, tx, cur)
		return err
	})
	if err != nil {
		return nil, err
	}
	logger.Info("обращение #%d: ответ HR ушёл новичку, статус %s", t.Number, t.Status)
	if resolve {
		s.sendResolved(ctx, e, t)
		return s.Get(ctx, id) // с записью «Вопрос закрыт» в переписке
	}
	return view, nil
}

// sourceMID — mid сообщения новичка, из которого выросло обращение: вопрос боту (текст или
// голосовое), на который передали. Пусто — флаг выключен, обращение из кнопки или старая запись.
func (s *TicketService) sourceMID(ctx context.Context, t *domain.Ticket) string {
	if !s.replies || t.QuestionID == nil {
		return ""
	}
	q, err := s.repos.Questions.ByID(ctx, *t.QuestionID)
	if err != nil || q == nil {
		if err != nil {
			logger.Warn("обращение #%d: вопрос для реплая не прочитан: %v", t.Number, err)
		}
		return ""
	}
	return derefMID(q.SourceMID)
}

// SetNotifyManager — PATCH {"notify_manager"}: для категорий, скрытых от руководителя, — 422.
func (s *TicketService) SetNotifyManager(ctx context.Context, id uuid.UUID, notify bool) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var view *TicketView
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		t, err := tx.Tickets.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if t == nil {
			return errs.NewNotFoundError(msgTicketNotFound)
		}
		hidden := t.Restricted || s.category(t.Category, t.Route).ManagerHidden
		if notify && hidden {
			return errs.NewValidationError(msgManagerHidden).
				WithDetails(errs.FieldError{Field: "notify_manager", Code: errs.CodeOutOfRange, Message: msgManagerHidden})
		}
		t.NotifyManager = notify && !hidden
		if err := tx.Tickets.Save(ctx, t); err != nil {
			return err
		}
		view, err = s.view(ctx, tx, t)
		return err
	})
	return view, err
}

// Note — внутренняя заметка: в MAX не уходит.
func (s *TicketService) Note(ctx context.Context, id uuid.UUID, text string) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var view *TicketView
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		t, err := tx.Tickets.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if t == nil {
			return errs.NewNotFoundError(msgTicketNotFound)
		}
		author := staffAuthor(ctx, t)
		if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgStaff, Author: &author, Text: text, Internal: true}); err != nil {
			return err
		}
		if err := tx.Tickets.Save(ctx, t); err != nil { // updated_at
			return err
		}
		view, err = s.view(ctx, tx, t)
		return err
	})
	return view, err
}

// Resolve — «Решить» / «Вернуть боту»: resolved, новичку «✅ Вопрос закрыт…». doneNote — «что
// сделано» (необязательно, одна строка): заметкой с DoneNotePrefix в переписку, адресному новичку
// (не анонимно, не F5/F9/F10) — «По вашему сигналу: …» перед «Вопрос закрыт».
func (s *TicketService) Resolve(ctx context.Context, id uuid.UUID, doneNote string) (*TicketView, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var t *domain.Ticket
	var e *domain.Employee
	var view *TicketView
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if t, e, err = s.locked(ctx, tx, id); err != nil {
			return err
		}
		s.markResolved(t)
		if err := tx.Tickets.Save(ctx, t); err != nil {
			return err
		}
		if doneNote = normDoneNote(doneNote); doneNote != "" {
			author := staffAuthor(ctx, t)
			if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgStaff, Author: &author, Text: DoneNotePrefix + doneNote, Internal: true}); err != nil {
				return err
			}
		}
		view, err = s.view(ctx, tx, t)
		return err
	})
	if err != nil {
		return nil, err
	}
	logger.Info("обращение #%d закрыто из кабинета", t.Number)
	s.sendDoneNote(ctx, e, t, doneNote)
	s.sendResolved(ctx, e, t)
	if s.max == nil {
		return view, nil
	}
	return s.Get(ctx, id) // с записью «Вопрос закрыт» в переписке
}

func (s *TicketService) markResolved(t *domain.Ticket) {
	now := s.now()
	t.Status, t.ResolvedAt, t.AwaitDetails = domain.TicketResolved, &now, false
}

// sendResolved — RESOLVED новичку и строка в переписку.
func (s *TicketService) sendResolved(ctx context.Context, e *domain.Employee, t *domain.Ticket) {
	if s.max == nil {
		return
	}
	to := recipientOf(e)
	if to.ChatID == 0 && to.UserID == 0 {
		return
	}
	if _, err := s.max.Send(ctx, ticketResolvedMessage(to, e, t)); err != nil {
		logger.Warn("обращение #%d: «Вопрос закрыт» не ушёл: %v", t.Number, err)
	}
	_ = s.repos.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgBot, Text: "«Вопрос закрыт», сотруднику предложено оценить помощь."})
}
