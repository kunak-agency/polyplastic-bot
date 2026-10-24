package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/flags"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Свободный вопрос новичка → rag-сервис → ответ с источником или честный отказ (итерация 2,
// UX-спецификация п. 2.5). Порядок: regex-флаги (pkg/flags) — если сработали, в rag не идём и
// отвечаем по правилу трёх шагов (п. 2.7); иначе POST /ask.
//
// Текст вопроса и ответа в лог не пишем: только id записи, вид ответа и trace_id.

// RAGAsker — что боту нужно от rag-сервиса. Интерфейс здесь, чтобы тесты шли на подделке.
type RAGAsker interface {
	Ask(ctx context.Context, req rag.AskRequest) (*rag.AskResponse, error)
}

// abstainWindow — отказы подряд в пределах этого окна считаются одной «лестницей ошибок».
const abstainWindow = time.Hour

// detailsWindow — сколько бот ждёт детали после «Сначала напишу, что случилось».
const detailsWindow = 24 * time.Hour

// errNoRAG — бот собран без клиента rag.
var errNoRAG = errors.New("rag: клиент не подключён")

// question — текст новичка, который не ответ на опрос. emp — карточка без блокировки.
func (b *BotService) question(ctx context.Context, u max.Update, emp *domain.Employee) error {
	text := strings.TrimSpace(u.Text)
	to := recipientOf(emp)
	if to.ChatID == 0 && to.UserID == 0 {
		to = recipient{ChatID: u.ChatID, UserID: u.UserID}
	}
	if text == "" {
		if len(u.Audio) > 0 {
			b.replyOn(ctx, to.msg(byForm(emp, textVoiceTy, textVoiceVy), "", nil), u.MessageID)
		}
		return nil
	}
	q := &domain.BotQuestion{
		EmployeeID: emp.ID, TraceID: newTraceID(), Flags: domain.StringList{}, Sources: domain.SourceList{}, IsTest: emp.IsTest,
	}
	q.CreatedAt = b.now() // окно правки 24 ч и лестница ошибок считаются по часам бота
	// ответ — реплаем на вопрос (FEATURE_REPLIES), ответ HR по обращению из него — тоже
	q.SourceMID = midOf(u.MessageID)
	if emp.Consent != nil && *emp.Consent == domain.ConsentGiven {
		q.Question = text
	}

	if codes := flags.Codes(text); codes != nil {
		// regex F5/F7/F9 — модель не вызывается, тональность ставим кодом: текст с «уволюсь»,
		// «ударил», «взятка» честно негативный (без отдельного вызова /flags ради тона).
		q.Sentiment = strPtr(domain.SentimentNeg)
		return b.flagSignal(ctx, to, emp, q, codes, flagViaRegex)
	}
	if err := b.touch(ctx, emp.ID, nil); err != nil {
		return err
	}
	// regex не сработал — LLM-разметка (bot_flags.go) в фоне, параллельно с rag: ответ не ждёт её.
	// id записи — заранее: тональность из /flags пишется в неё, когда ask её создаст (ready).
	if id, err := uuid.NewV7(); err == nil {
		q.ID = id
	}
	ready := make(chan struct{})
	qid := q.ID
	b.classifyAsync(ctx, emp, flagInput{text: text, context: rag.FlagsContextFreeText, where: "в сообщении новичка", mid: u.MessageID,
		questionID: &qid, questionReady: ready})
	// rag отвечает до RAG_ASK_TIMEOUT: ждём в фоне, чтобы не держать чтение апдейтов остальных.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer close(ready)
		if err := b.ask(context.WithoutCancel(ctx), to, emp, q, text); err != nil {
			logger.Warn("бот: вопрос %s (trace %s) не обработан: %v", q.ID, q.TraceID, err)
		}
	}()
	return nil
}

func newTraceID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// touch — отметить активность новичка; codes — поднять риск по сигналу флага.
func (b *BotService) touch(ctx context.Context, empID uuid.UUID, codes []string) error {
	return b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, empID)
		if err != nil || e == nil {
			return err
		}
		now := b.now()
		e.LastActivityAt = &now
		if codes != nil {
			markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в сообщении новичка")
		}
		return tx.Employees.Save(ctx, e)
	})
}

// flagSignal — сработал regex-флаг: риск в карточку, HumanRequest, ответ по правилу трёх
// шагов. В rag вопрос не уходит. F5 и F9 (критические, «Передаю…» по UX 2.7) — сразу
// обращение и карточка в чат СБ; F7 — обращение, когда новичок выберет «Передать рекрутеру» или
// напишет детали (UX 2.7 даёт ему выбор).
// via — откуда сигнал: flagViaRegex или LLM-разметка /flags (для сводки обращения).
func (b *BotService) flagSignal(ctx context.Context, to recipient, emp *domain.Employee, q *domain.BotQuestion, codes []string, via string) error {
	if done, err := b.quitAlreadyOffered(ctx, to, emp, codes, via); done || err != nil {
		return err
	}
	q.Kind, q.Flags = domain.QuestionFlag, codes
	cat := b.flagCategory(q.Flags)
	immediate := cat.SecurityChat
	var res handoffResult
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, emp.ID)
		if err != nil || e == nil {
			return err
		}
		now := b.now()
		e.LastActivityAt = &now
		markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в сообщении новичка")
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		if err := tx.Questions.ClearAwaiting(ctx, e.ID); err != nil {
			return err
		}
		if err := tx.Questions.Create(ctx, q); err != nil {
			return err
		}
		h := domain.HumanRequest{EmployeeID: e.ID, Source: domain.HumanFromFreeText, Flags: codes, QuestionID: &q.ID, Target: flagTarget(q.Flags)}
		if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
			return err
		}
		if !immediate {
			return nil
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: cat, source: domain.TicketFromFlag, quote: q.Question, questionID: &q.ID, humanRequestID: &h.ID,
			what: fmt.Sprintf("Сработал сигнал %s %s, передано сразу.", strings.Join(codes, ", "), via),
		})
		if err != nil {
			return err
		}
		res = handoffResult{ticket: t, created: created}
		q.TicketID, q.HumanAt, q.Outcome = &t.ID, &now, "handoff"
		q.TicketNumber = t.Number
		return tx.Questions.Save(ctx, q)
	})
	if err != nil {
		return err
	}
	logger.Info("бот: сигнал %v от новичка %s, запись %s (%s)", codes, emp.ID, q.ID, via)
	if res.ticket != nil && res.created {
		b.tickets.notify(ctx, res.ticket, emp)
	}
	b.sendQuestion(ctx, to, emp, q)
	return nil
}

// quitOfferWindow — сколько предложение F7 «Передать рекрутеру?» считается открытым, если
// новичок на него не ответил (UX 2.3: непройденное — сутки).
const quitOfferWindow = 24 * time.Hour

const (
	textQuitOfferedVy = "Слышу вас. Я уже предложил передать это рекрутеру — выберите вариант в сообщении выше: " +
		"передать, сначала написать подробнее или пока не надо."
	textQuitOfferedTy = "Слышу тебя. Я уже предложил передать это рекрутеру — выбери вариант в сообщении выше: " +
		"передать, сначала написать подробнее или пока не надо."
	textQuitTicketVy = "Слышу вас. Я уже передал это рекрутеру — обращение #%d, ответ придёт сюда же. Если хотите " +
		"что-то добавить, нажмите «Добавить детали» в сообщении об обращении."
	textQuitTicketTy = "Слышу тебя. Я уже передал это рекрутеру — обращение #%d, ответ придёт сюда же. Если хочешь " +
		"что-то добавить, нажми «Добавить детали» в сообщении об обращении."
)

// quitAlreadyOffered — сигнал только F7 (без F5/F9), а у новичка уже есть открытое предложение
// F7 (не отвечено, не старше суток) или незакрытое обращение F7: второе предложение не
// показываем. Риск всё равно отмечается. Новичку, который сам написал «уволюсь» (regex), —
// короткое напоминание, где уже идёт дело; на LLM-сигнал — тишина: ответ rag уже ушёл.
func (b *BotService) quitAlreadyOffered(ctx context.Context, to recipient, emp *domain.Employee, codes []string, via string) (bool, error) {
	list := domain.StringList(codes)
	if !list.Has(flags.CodeQuit) || list.Has(flags.CodeViolence) || list.Has(flags.CodeCorruption) {
		return false, nil
	}
	t, offered, err := b.quitPending(ctx, emp.ID)
	if err != nil || (t == nil && !offered) {
		return false, err
	}
	if err := b.touch(ctx, emp.ID, codes); err != nil {
		return true, err
	}
	logger.Info("бот: сигнал F7 от новичка %s (%s) — предложение уже открыто, повтор не показан", emp.ID, via)
	if via != flagViaRegex {
		return true, nil
	}
	text := byForm(emp, textQuitOfferedTy, textQuitOfferedVy)
	if t != nil {
		text = fmt.Sprintf(byForm(emp, textQuitTicketTy, textQuitTicketVy), t.Number)
	}
	b.send(ctx, to.msg(text, "", nil))
	return true, nil
}

// quitPending — незакрытое обращение F7 новичка или открытое предложение F7: запись сигнала
// F7 за последние сутки, на которую новичок ещё не ответил (не передано, не «Пока не надо»).
func (b *BotService) quitPending(ctx context.Context, empID uuid.UUID) (*domain.Ticket, bool, error) {
	t, err := b.repos.Tickets.Active(ctx, empID, flags.CodeQuit)
	if err != nil || t != nil {
		return t, false, err
	}
	qs, err := b.repos.Questions.Recent(ctx, empID, b.now().Add(-quitOfferWindow), 50)
	if err != nil {
		return nil, false, err
	}
	for _, q := range qs {
		if q.Kind == domain.QuestionFlag && q.Flags.Has(flags.CodeQuit) && flagCopyFor(q.Flags).code == flags.CodeQuit &&
			q.HumanAt == nil && q.TicketID == nil && q.Outcome == "" {
			return nil, true, nil
		}
	}
	return nil, false, nil
}

// ask — POST /ask и ответ новичку. Ошибка rag не ошибка обработки: новичок получает честное
// «не могу проверить по документам».
func (b *BotService) ask(ctx context.Context, to recipient, emp *domain.Employee, q *domain.BotQuestion, text string) error {
	ctx = rag.WithTraceID(ctx, q.TraceID)
	req := rag.AskRequest{Question: text, AddressForm: emp.AddressForm}
	if emp.Site != "" {
		site := emp.Site
		req.Site = &site
	}
	var resp *rag.AskResponse
	err := errNoRAG
	if b.rag != nil {
		stop := b.typing(ctx, to.ChatID)
		resp, err = b.askPatiently(ctx, to, emp, req)
		stop()
	}
	switch {
	case err != nil:
		logger.Warn("бот: rag /ask (trace %s) не ответил: %v", q.TraceID, err)
		q.Kind = domain.QuestionError
	case !resp.NeedsEscalation:
		q.Kind = domain.QuestionAnswer
		q.Answer = strOr(resp.Answer)
		for _, s := range resp.Sources {
			q.Sources = append(q.Sources, domain.QuestionSource{Doc: s.Doc, ClauseID: s.ClauseID, Page: s.Page, Quote: s.Quote})
		}
	case resp.Reason() == rag.AbstainRoutedTopic:
		q.Kind = domain.QuestionRouted
	default:
		q.Kind = domain.QuestionAbstain
	}
	if resp != nil {
		q.Confidence, q.Topic, q.AbstainReason = resp.Confidence, resp.Topic, resp.Reason()
	}
	if ruleRefusal(q.Kind) {
		// правило сценария r69: отказ — текстом правила (он же в правке после нажатия)
		if rule := scenarioRule(ctx, b.repos, emp, domain.RuleFreeQuestion); rule != nil {
			q.Answer = byForm(emp, rule.TextTy, rule.TextVy)
		}
	}
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		if q.Kind == domain.QuestionAbstain {
			kinds, err := tx.Questions.RecentKinds(ctx, emp.ID, b.now().Add(-abstainWindow), 2)
			if err != nil {
				return err
			}
			q.Attempt = 1
			for _, k := range kinds {
				if k != domain.QuestionAbstain {
					break
				}
				q.Attempt++
			}
		}
		if err := tx.Questions.ClearAwaiting(ctx, emp.ID); err != nil {
			return err
		}
		return tx.Questions.Create(ctx, q)
	})
	if err != nil {
		return err
	}
	logger.Info("бот: вопрос %s новичка %s → %s %s (trace %s)", q.ID, emp.ID, q.Kind, q.AbstainReason, q.TraceID)
	b.sendQuestion(ctx, to, emp, q)
	return nil
}

// lateAskWindow — сколько бот ещё ждёт rag после RAG_ASK_TIMEOUT: под LLM бывает медленным, и
// поздний ответ по документам лучше «справочная база не отвечает».
var lateAskWindow = 20 * time.Second

// patientAsker — rag-клиент, у которого можно задать срок запроса (rag.Client).
type patientAsker interface {
	AskWithin(ctx context.Context, req rag.AskRequest, d time.Duration) (*rag.AskResponse, error)
	AskTimeout() time.Duration
}

// askPatiently — POST /ask. Не уложился в RAG_ASK_TIMEOUT — новичку «ищу дольше обычного», и
// бот ждёт ещё lateAskWindow: пришёл ответ — он и уходит; нет — честный отказ, как раньше.
func (b *BotService) askPatiently(ctx context.Context, to recipient, emp *domain.Employee, req rag.AskRequest) (*rag.AskResponse, error) {
	pa, ok := b.rag.(patientAsker)
	if !ok || lateAskWindow <= 0 {
		return b.rag.Ask(ctx, req)
	}
	type result struct {
		resp *rag.AskResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		r, err := pa.AskWithin(ctx, req, pa.AskTimeout()+lateAskWindow)
		ch <- result{r, err}
	}()
	timer := time.NewTimer(pa.AskTimeout())
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.resp, r.err
	case <-timer.C:
	}
	b.send(ctx, to.msg(byForm(emp, textAskLateTy, textAskLateVy), "", nil))
	r := <-ch
	return r.resp, r.err
}

const (
	textAskLateVy = "Ищу в документах дольше обычного — пришлю ответ сюда же, подождите немного."
	textAskLateTy = "Ищу в документах дольше обычного — пришлю ответ сюда же, подожди немного."
)

// typingEvery — «печатает» в MAX гаснет через несколько секунд; rag думает 4–8 с, поэтому
// действие повторяется, пока ждём ответ.
const typingEvery = 4 * time.Second

// typing — показывать «печатает», пока не вызван stop. Клиент без Typer или чат неизвестен —
// ничего не делает. Ошибка действия не мешает ответу.
func (b *BotService) typing(ctx context.Context, chatID int64) (stop func()) {
	t, ok := b.max.(max.Typer)
	if !ok || chatID == 0 {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(typingEvery)
		defer tick.Stop()
		for {
			if err := t.Typing(ctx, chatID); err != nil && ctx.Err() == nil {
				logger.Info("бот: «печатает» не показан: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}

// sendQuestion — первое сообщение по записи; mid — в запись (для правок и разбора).
func (b *BotService) sendQuestion(ctx context.Context, to recipient, emp *domain.Employee, q *domain.BotQuestion) {
	msg := questionMessage(to, emp, q)
	if ruleRefusal(q.Kind) && q.Answer != "" {
		if rule := scenarioRule(ctx, b.repos, emp, domain.RuleFreeQuestion); rule != nil {
			msg = ruleRefusalMessage(to, emp, q, rule)
			msg.Attachments = b.attach(ctx, emp, rule.Media)
		}
	}
	if noRecipient(msg, "ответ на вопрос "+q.ID.String()) {
		return
	}
	msg.ReplyTo = derefMID(q.SourceMID) // ответ rag, «не нашёл», сигнал — реплаем на вопрос
	mid, err := sendReply(ctx, b.max, b.replies, msg)
	if err != nil {
		logger.Warn("бот: ответ на вопрос %s не ушёл: %v", q.ID, err)
		return
	}
	if err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		cur, err := tx.Questions.ByIDForUpdate(ctx, q.ID)
		if err != nil || cur == nil {
			return err
		}
		cur.MessageID = mid
		if cur.TicketID != nil { // F5/F9: само сообщение и есть статус обращения (правится в ASSIGNED)
			if err := tx.Tickets.SetStatusMessage(ctx, *cur.TicketID, mid, b.now()); err != nil {
				return err
			}
		}
		return tx.Questions.Save(ctx, cur)
	}); err != nil {
		logger.Warn("бот: mid ответа %s не сохранён: %v", q.ID, err)
	}
}

// flagDetails — текст после «Сначала напишу, что случилось»: детали к сигналу, не вопрос.
// Возвращает false, если бот деталей не ждёт.
func (b *BotService) flagDetails(ctx context.Context, u max.Update, emp *domain.Employee) (bool, error) {
	text := strings.TrimSpace(u.Text)
	if text == "" {
		return false, nil
	}
	var done *domain.BotQuestion
	var res handoffResult
	var attached *domain.Ticket
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		w, err := tx.Questions.AwaitingDetails(ctx, emp.ID, b.now().Add(-detailsWindow))
		if err != nil || w == nil {
			return err
		}
		q, err := tx.Questions.ByIDForUpdate(ctx, w.ID)
		if err != nil || q == nil || !q.AwaitDetails {
			return err
		}
		now := b.now()
		q.AwaitDetails, q.Outcome = false, "details"
		if q.HumanAt == nil {
			q.HumanAt = &now
		}
		if emp.Consent != nil && *emp.Consent == domain.ConsentGiven {
			q.Details = text
		}
		for _, c := range flags.Codes(text) {
			if !q.Flags.Has(c) {
				q.Flags = append(q.Flags, c)
			}
		}
		done = q
		e, err := tx.Employees.ByIDForUpdate(ctx, emp.ID)
		if err != nil || e == nil {
			return err
		}
		e.LastActivityAt = &now
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		// Детали — в обращение: уже открытое (F5/F9) дополняется, для F7 детали и есть согласие
		// на передачу — обращение создаётся.
		cat := b.flagCategory(q.Flags)
		if q.TicketID != nil {
			t, err := tx.Tickets.ByIDForUpdate(ctx, *q.TicketID)
			if err != nil {
				return err
			}
			if t != nil && t.Status != domain.TicketResolved {
				if keep := keepText(e, cat, text); keep != "" {
					if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgNewcomer, Text: keep}); err != nil {
						return err
					}
				}
				attached = t
				return tx.Questions.Save(ctx, q)
			}
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: cat, source: domain.TicketFromFlag, quote: q.Question, questionID: &q.ID,
			what: fmt.Sprintf("Сработал сигнал %s в свободном тексте, новичок дописал подробности.", strings.Join(q.Flags, ", ")),
		})
		if err != nil {
			return err
		}
		if keep := keepText(e, cat, text); keep != "" {
			if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgNewcomer, Text: keep}); err != nil {
				return err
			}
		}
		q.TicketID, q.TicketNumber = &t.ID, t.Number
		res = handoffResult{ticket: t, created: created, lead: textFlagDetails, replyTo: u.MessageID}
		return tx.Questions.Save(ctx, q)
	})
	if err != nil || done == nil {
		return false, err
	}
	logger.Info("бот: детали к сигналу %s от новичка %s записаны", done.ID, emp.ID)
	if attached != nil {
		b.replyOn(ctx, recipientOf(emp).msg(fmt.Sprintf(textAttached, attached.Number), "", nil), u.MessageID)
		return true, nil
	}
	b.afterHandoff(ctx, emp, &res)
	return true, nil
}

// --- кнопки под ответом ---

// isQuestionPayload — payload кнопок свободного вопроса.
func isQuestionPayload(p string) bool {
	for _, pre := range []string{preQMore, preQHelpful, preQNotThat, preQAlt, preQHR, preQRecruiter, preQChat, preQRephrase, preQNoNeed, preFlagSend, preFlagWrite} {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// questionReply — что сделать после нажатия: всплывающее, правка нажатого сообщения и/или новое
// сообщение.
type questionReply struct {
	notification string
	edit         *max.OutMessage
	send         *max.OutMessage
	handoff      *handoffResult // передано человеку: после ответа — карточка и OPEN
}

func (b *BotService) questionCallback(ctx context.Context, u max.Update, emp *domain.Employee) error {
	p := u.Payload
	prefix, rest := p[:3], p[3:]
	idRaw, idxRaw, _ := strings.Cut(rest, ":")
	id, err := uuid.Parse(idRaw)
	if err != nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	var out questionReply
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		q, err := tx.Questions.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if q == nil || q.EmployeeID != emp.ID {
			out.notification = textButtonStale // вопрос удалён или чужой
			return nil
		}
		now := b.now()
		to := updateRecipient(u, emp) // кто нажал, а не чья запись
		// onMain — нажато в самом сообщении с ответом (а не в уточнении или «Передать HR?»).
		onMain := u.MessageID == "" || u.MessageID == q.MessageID
		final := func(line string) *max.OutMessage {
			text, format := line, ""
			if onMain {
				base, f := baseText(emp, q)
				text, format = base+"\n\n"+line, f
				if format == formatMarkdown {
					text = base + "\n\n" + escapeMarkdown(line)
				}
			}
			m := to.msg(text, format, nil)
			return &m
		}

		switch prefix {
		case preQMore:
			if q.Kind != domain.QuestionAnswer {
				out.notification = "Эта кнопка больше не работает"
				return nil
			}
			q.Expanded = true
			m := answerMessage(to, q, true)
			out.edit, out.notification = &m, "Цитата пункта"
		case preQHelpful:
			if q.HelpfulAt != nil {
				out.notification = "Уже записал"
				return nil
			}
			q.HelpfulAt, out.notification = &now, "Спасибо! Записал"
		case preQNotThat:
			if q.HumanAt != nil {
				out.notification = textHandedAlready
				return nil
			}
			q.NotThat++
			out.notification = "Понял"
			if q.NotThat == 1 {
				m := notThatMessage(to, emp, q)
				out.send = &m
			} else {
				m := offerHRMessage(to, q, textNotThatOffer)
				out.send = &m
			}
		case preQAlt:
			i, err := strconv.Atoi(idxRaw)
			srcs := distinctSources(q.Sources)
			if err != nil || i < 1 || i >= len(srcs) {
				out.notification = "Эта кнопка больше не работает"
				return nil
			}
			m := altMessage(to, q, srcs[i])
			out.send, out.notification = &m, "Другой пункт"
		case preQHR, preQRecruiter, preQChat, preFlagSend:
			if q.HumanAt != nil {
				out.notification = textHandedAlready
				return nil
			}
			target, line := domain.TargetHR, textHandedHR
			cat := taxonomy.Question(taxonomy.RouteHR)
			switch {
			case prefix == preQRecruiter || (prefix == preQChat && idxRaw != taxonomy.RouteHR):
				target, line = domain.TargetRecruiter, textHandedRecruiter
				cat = taxonomy.Question(taxonomy.RouteRecruiter)
			case prefix == preFlagSend || q.Kind == domain.QuestionFlag:
				cat = b.flagCategory(q.Flags)
				target, line = flagTarget(q.Flags), "Передаю "+ticketTo(cat)+"."
			}
			q.HumanAt, q.Outcome, q.AwaitDetails = &now, "handoff", false
			d := handoffDraft(q, cat, prefix)
			if q.Kind != domain.QuestionFlag { // сигнал флага записан HumanRequest сразу при срабатывании
				h := domain.HumanRequest{EmployeeID: emp.ID, Source: domain.HumanFromAnswer, Flags: domain.StringList{}, QuestionID: &q.ID, Target: target}
				if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
					return err
				}
				d.humanRequestID = &h.ID
			}
			t, created, err := b.tickets.open(ctx, tx, emp, d)
			if err != nil {
				return err
			}
			if prefix == preQChat && created { // «связаться с рекрутером» (r69) — сразу переписка
				t.Status = domain.TicketHumanActive
				if err := tx.Tickets.Save(ctx, t); err != nil {
					return err
				}
			}
			q.TicketID, q.TicketNumber = &t.ID, t.Number
			out.handoff = &handoffResult{ticket: t, created: created, replyTo: derefMID(q.SourceMID)}
			out.notification = "Передаю"
			if onMain && q.Kind == domain.QuestionAnswer {
				m := to.msg(line, "", nil) // ответ по документам не затираем
				out.send = &m
			} else {
				out.edit = final(line)
			}
		case preQRephrase:
			q.Outcome = "rephrase"
			out.notification, out.edit = "Жду", final(textRephrase)
		case preQNoNeed:
			if q.Outcome == "" {
				q.Outcome = "declined"
			}
			q.AwaitDetails = false
			line := byForm(emp, textNoNeedTy, textNoNeedVy)
			if q.Kind == domain.QuestionFlag {
				line = byForm(emp, textFlagLaterTy, textFlagLaterVy)
			}
			out.notification, out.edit = "Хорошо", final(line)
		case preFlagWrite:
			// F5/F9 уже переданы (TicketID есть) — дописать подробности можно; F7 — до передачи.
			if q.Kind != domain.QuestionFlag || (q.HumanAt != nil && q.TicketID == nil) {
				out.notification = textHandedAlready
				return nil
			}
			if err := tx.Questions.ClearAwaiting(ctx, emp.ID); err != nil {
				return err
			}
			q.AwaitDetails, q.AwaitSince = true, &now
			out.notification, out.edit = "Жду", final(byForm(emp, textFlagWriteTy, textFlagWriteVy))
		}
		// Сообщение старше суток MAX править не даёт: итог — новым сообщением.
		if out.edit != nil && now.Sub(q.CreatedAt) > editWindow {
			out.send, out.edit = out.edit, nil
		}
		return tx.Questions.Save(ctx, q)
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, max.Answer{Notification: orDefault(out.notification, "Записал"), Message: out.edit})
	if out.send != nil {
		b.send(ctx, *out.send)
	}
	b.afterHandoff(ctx, emp, out.handoff)
	return nil
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
