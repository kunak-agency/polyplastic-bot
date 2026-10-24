package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/flags"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Бот и обращения (итерация 3, UX-спецификация п. 2.7): создание из «Спросить человека»,
// «Передать HR / рекрутеру», «Чего-то нет», regex-флагов; кнопки статуса; переписка с HR, пока
// обращение в human_active (текст новичка идёт в обращение, а не в RAG); команды /menu и /help.

// handoffResult — обращение после передачи: created=false — такое уже было открыто.
type handoffResult struct {
	ticket  *domain.Ticket
	created bool
	lead    string // строка перед OPEN
	// replyTo — сообщение новичка, из которого выросло обращение: «Передал…» и «уже передано» —
	// реплаем на него (FEATURE_REPLIES). Пусто — нажатие кнопки, отвечать не на что.
	replyTo string
}

// afterHandoff — после транзакции: карточка людям и OPEN новичку; повтор — «уже передано».
func (b *BotService) afterHandoff(ctx context.Context, emp *domain.Employee, r *handoffResult) {
	if r == nil || r.ticket == nil {
		return
	}
	if !r.created {
		b.replyOn(ctx, recipientOf(emp).msg(fmt.Sprintf(textTicketExists, r.ticket.Number), "", nil), r.replyTo)
		return
	}
	b.tickets.notify(ctx, r.ticket, emp)
	b.tickets.sendOpen(ctx, emp, r.ticket, r.lead, r.replyTo)
}

// human — «Спросить человека» (кнопка под шагом, опросом, приветствием) и /human: запрос
// HumanRequest и обращение «вопрос человеку» к HR.
func (b *BotService) human(ctx context.Context, u max.Update, emp *domain.Employee, eventRaw string) error {
	h := domain.HumanRequest{EmployeeID: emp.ID, Source: domain.HumanFromButton, Flags: domain.StringList{}, Target: domain.TargetHR}
	what := "Новичок нажал «Спросить человека»."
	source := domain.TicketFromButton
	replyTo := ""
	if u.CallbackID == "" {
		what, source = "Новичок попросил человека командой /human.", domain.TicketFromCmd
		replyTo = u.MessageID // /human — реплаем на команду
	}
	var res handoffResult
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		if id, err := uuid.Parse(eventRaw); err == nil {
			if ev, err := tx.Events.ByIDForUpdate(ctx, id); err != nil {
				return err
			} else if ev != nil && ev.EmployeeID == emp.ID {
				h.EventID = &id
				what = fmt.Sprintf("Новичок нажал «Спросить человека» под шагом «%s».", ev.Title)
			}
		}
		if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
			return err
		}
		t, created, err := b.tickets.open(ctx, tx, emp, ticketDraft{
			cat: taxonomy.Question(taxonomy.RouteHR), source: source, what: what,
			humanRequestID: &h.ID, eventID: h.EventID, withContext: true,
		})
		res = handoffResult{ticket: t, created: created, replyTo: replyTo}
		return err
	})
	if err != nil {
		return err
	}
	logger.Info("бот: «Спросить человека» от новичка %s → обращение #%d", emp.ID, res.ticket.Number)
	b.answer(ctx, u.CallbackID, max.Answer{Notification: "Передаю"})
	b.afterHandoff(ctx, emp, &res)
	return nil
}

// handoffDraft — обращение из кнопки под ответом бота: что случилось — по виду ответа.
func handoffDraft(q *domain.BotQuestion, cat taxonomy.Category, prefix string) ticketDraft {
	what := "Новичок попросил передать вопрос человеку."
	switch q.Kind {
	case domain.QuestionAnswer:
		what = "Ответ бота по документам не помог, сотрудник попросил человека."
	case domain.QuestionAbstain:
		what = "Бот не нашёл ответа в документах, сотрудник попросил передать вопрос."
	case domain.QuestionRouted:
		what = "Вопрос на тему для человека (бот по документам не отвечает), сотрудник выбрал, кому передать."
	case domain.QuestionError:
		what = "Справочная база не ответила, сотрудник попросил передать вопрос."
	case domain.QuestionFlag:
		what = fmt.Sprintf("Сработал сигнал %s в свободном тексте, сотрудник согласился на передачу.", strings.Join(q.Flags, ", "))
	}
	if prefix == preQRecruiter {
		what = strings.TrimSuffix(what, ".") + " (рекрутеру)."
	}
	source := domain.TicketFromAnswer
	if q.Kind == domain.QuestionFlag {
		source = domain.TicketFromFlag
	}
	return ticketDraft{cat: cat, source: source, what: what, quote: q.Question, questionID: &q.ID, withContext: true}
}

// flagCategory — категория обращения по сработавшим regex-флагам: самый серьёзный код.
func (b *BotService) flagCategory(codes domain.StringList) taxonomy.Category {
	return b.tickets.category(flagCopyFor(codes).code, "")
}

// --- кнопки статуса обращения ---

func (b *BotService) ticketCallback(ctx context.Context, u max.Update, emp *domain.Employee) error {
	id, ok := ticketPayloadID(u.Payload)
	if !ok {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	prefix := u.Payload[:3]
	var note string
	var send *max.OutMessage
	var resolved *domain.Ticket
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		t, err := tx.Tickets.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if t == nil || t.EmployeeID != emp.ID {
			note = textButtonStale // обращение удалено или чужое
			return nil
		}
		closed := t.Status == domain.TicketResolved
		to := updateRecipient(u, emp) // кто нажал, а не чья запись
		switch prefix {
		case preTicketUp, preTicketDown:
			t.Feedback, note = "up", "Спасибо! Записал"
			if prefix == preTicketDown {
				t.Feedback, note = "down", "Записал. Передам HR, что не помогло"
			}
			return tx.Tickets.Save(ctx, t)
		case preTicketReply:
			if closed {
				note = textTicketGone
				return nil
			}
			note = byForm(emp, textReplyHintTy, textReplyHintVy)
			return nil
		case preTicketDetails:
			if closed {
				note = textTicketGone
				return nil
			}
			if err := tx.Tickets.ClearAwaiting(ctx, emp.ID); err != nil {
				return err
			}
			t.AwaitDetails, note = true, "Жду"
			m := to.msg(fmt.Sprintf(byForm(emp, textDetailsTy, textDetailsVy), t.Number), "", nil)
			send = &m
			return tx.Tickets.Save(ctx, t)
		case preTicketCancel, preTicketClose:
			if closed {
				note = textTicketGone
				return nil
			}
			b.tickets.markResolved(t)
			line := "Новичок отменил обращение."
			if prefix == preTicketClose {
				line, note = "Новичок нажал «Вопрос закрыт».", "Закрыл"
				resolved = t
			} else {
				note = "Отменил"
				m := to.msg(fmt.Sprintf(byForm(emp, textTicketCancT, textTicketCancel), t.Number), "", nil)
				send = &m
			}
			if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgBot, Text: line}); err != nil {
				return err
			}
			logger.Info("бот: обращение #%d закрыто новичком (%s)", t.Number, prefix)
			return tx.Tickets.Save(ctx, t)
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, max.Answer{Notification: orDefault(note, "Записал")})
	if send != nil {
		b.send(ctx, *send)
	}
	if resolved != nil {
		b.tickets.sendResolved(ctx, emp, resolved)
	}
	return nil
}

// accessReason — причина после «Чего-то нет» (ar:<event>:<i>): обращение F2 к HR, уведомление
// руководителю (без текста), шаг правится в итог.
func (b *BotService) accessReason(ctx context.Context, u max.Update, empID uuid.UUID) error {
	rest := strings.TrimPrefix(u.Payload, preAccessReason)
	evRaw, idxRaw, _ := strings.Cut(rest, ":")
	evID, err1 := uuid.Parse(evRaw)
	idx, err2 := strconv.Atoi(idxRaw)
	if err1 != nil || err2 != nil || idx < 0 || idx >= len(accessReasons) {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	cat, ok := b.tickets.tax.Get("F2")
	if !ok {
		return fmt.Errorf("таксономия: нет F2")
	}
	var reply max.Answer
	var res handoffResult
	var emp *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		if ev == nil {
			reply = max.Answer{Notification: textButtonStale}
			return nil
		}
		if ev.Status != domain.EventSent {
			reply = max.Answer{Notification: "Уже записал"}
			return nil
		}
		emp = e
		reason := accessReasons[idx]
		label := "Чего-то нет: " + reason
		now := b.now()
		ev.Status, ev.Answer = domain.EventAnswered, &label
		ev.Waiting = ""
		e.LastActivityAt = &now
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		if err := tx.Events.Save(ctx, ev); err != nil {
			return err
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: cat, source: domain.TicketFromAccess, eventID: &ev.ID,
			what: fmt.Sprintf("Новичок ответил «Чего-то нет» в шаге «%s» → «%s».", step.Title, reason),
		})
		if err != nil {
			return err
		}
		res = handoffResult{ticket: t, created: created}
		done := b.recipient(e).msg(stepAnsweredTextIn(step, e, label, ev.IsTest, sentAt(ev, now), b.siteFor(e)), "", nil)
		reply = max.Answer{Notification: "Передаю", Message: &done}
		return nil
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, reply)
	if emp != nil {
		b.afterHandoff(ctx, emp, &res)
		b.applyThen(ctx, emp.ID, evID, nil) // ответ получен — поток блока дальше
	}
	return nil
}

// --- текст новичка в обращение ---

// ticketConversation — текст новичка, пока обращение в human_active или после «Добавить
// детали»: в переписку обращения, в RAG не уходит. false — такого обращения нет.
func (b *BotService) ticketConversation(ctx context.Context, u max.Update, emp *domain.Employee) (bool, error) {
	text := strings.TrimSpace(u.Text)
	if text == "" {
		return false, nil
	}
	var reply string
	var t *domain.Ticket
	toRAG := false
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		cur, err := tx.Tickets.Conversation(ctx, emp.ID)
		if err != nil || cur == nil {
			return err
		}
		if t, err = tx.Tickets.ByIDForUpdate(ctx, cur.ID); err != nil || t == nil {
			return err
		}
		// Новый вопрос, пока HR ещё не включился в переписку, — в RAG, а не в обращение: иначе
		// после «Спросить HR» про транспорт бот молчит на «как взять отпуск?» до закрытия.
		if !t.AwaitDetails && looksLikeQuestion(text) {
			msgs, err := tx.Tickets.Messages(ctx, t.ID)
			if err != nil {
				return err
			}
			if !staffJoined(msgs) {
				t, toRAG = nil, true
				return nil
			}
		}
		e, err := tx.Employees.ByIDForUpdate(ctx, emp.ID)
		if err != nil || e == nil {
			return err
		}
		now := b.now()
		e.LastActivityAt = &now
		if codes := flags.Codes(text); codes != nil {
			markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в переписке с HR")
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		cat := b.tickets.category(t.Category, t.Route)
		keep := text
		if t.AwaitDetails {
			// Детали к обращению — как цитата: только при согласии и если категория разрешает.
			keep = keepText(e, cat, text)
			t.AwaitDetails = false
			reply = fmt.Sprintf(textAttached, t.Number)
			if keep == "" {
				reply = fmt.Sprintf("Без согласия на обработку сообщений я не сохраняю текст. HR увидит, что вы хотели "+
					"дополнить обращение #%d, и свяжется с вами.", t.Number)
			}
		} else {
			// human_active: новичок пишет человеку напрямую — это и есть цель переписки. Подтверждаем
			// только первое сообщение после ответа HR, чтобы не дублировать каждое.
			last, err := tx.Tickets.LastMessage(ctx, t.ID)
			if err != nil {
				return err
			}
			if last == nil || last.From != domain.TicketMsgNewcomer {
				reply = fmt.Sprintf(textForwarded, t.Number)
			}
		}
		if keep != "" {
			if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgNewcomer, Text: keep}); err != nil {
				return err
			}
		} else if err := tx.Tickets.AddMessage(ctx, &domain.TicketMessage{TicketID: t.ID, From: domain.TicketMsgBot, Text: "Новичок хотел дополнить обращение; текст не сохранён (нет согласия или категория без цитат)."}); err != nil {
			return err
		}
		return tx.Tickets.Save(ctx, t)
	})
	if toRAG {
		logger.Info("бот: вопрос новичка %s при открытом обращении без ответа HR → в RAG", emp.ID)
		return false, nil
	}
	if err != nil || t == nil {
		return false, err
	}
	logger.Info("бот: текст новичка %s → обращение #%d (%s), в rag не отправлено", emp.ID, t.Number, t.Status)
	if reply != "" {
		b.replyOn(ctx, recipientOf(emp).msg(reply, "", nil), u.MessageID) // «Добавил к обращению…»
	}
	return true, nil
}

// staffJoined — HR уже ответил в переписке обращения: дальше новичок говорит с человеком, и
// любой его текст — в обращение. Пока HR не ответил (сразу после «Спросить HR»), новый вопрос
// идёт в RAG.
func staffJoined(msgs []domain.TicketMessage) bool {
	for _, m := range msgs {
		if m.From == domain.TicketMsgStaff && !m.Internal {
			return true
		}
	}
	return false
}

// questionWords — начало вопроса без знака «?»: голосовой ввод и быстрый набор его теряют.
var questionWords = []string{"как ", "когда ", "где ", "что ", "сколько ", "можно ", "почему ", "кто ",
	"какой ", "какая ", "какие ", "куда ", "зачем ", "есть ли ", "нужно ли ", "надо ли ", "подскажи"}

// looksLikeQuestion — текст похож на новый вопрос, а не на «Хорошо» или уточнение к обращению.
func looksLikeQuestion(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	for _, lead := range []string{"а ", "и ", "ещё ", "еще "} {
		t = strings.TrimPrefix(t, lead)
	}
	if strings.Contains(t, "?") {
		return true
	}
	for _, w := range questionWords {
		if strings.HasPrefix(t, w) {
			return true
		}
	}
	return false
}

// --- команды ---

const (
	textHelpVy = "Я Корсик — помощник новых сотрудников.\n" +
		"• Спросите о правилах, графике, пропуске — отвечу по документам компании.\n" +
		"• Напомню, что нужно в первые дни, и иногда коротко спрошу, как дела.\n" +
		"• /human — передам вопрос человеку из HR.\n" +
		"• /menu — меню."
	textHelpTy = "Я Корсик — помощник новых сотрудников.\n" +
		"• Спроси о правилах, графике, пропуске — отвечу по документам компании.\n" +
		"• Напомню, что нужно в первые дни, и иногда коротко спрошу, как дела.\n" +
		"• /human — передам вопрос человеку из HR.\n" +
		"• /menu — меню."
	textMenuVy = "Чем помочь? Напишите вопрос своими словами — например, «Когда зарплата?» или «Как получить пропуск?». " +
		"Или передам вопрос человеку."
	textMenuTy = "Чем помочь? Напиши вопрос своими словами — например, «Когда зарплата?» или «Как получить пропуск?». " +
		"Или передам вопрос человеку."
	textActiveTicketVy = "\n\nСейчас открыта переписка с HR по обращению #%d: всё, что вы напишете, уйдёт им, а не мне. " +
		"Когда вопрос решён — нажмите «Вопрос закрыт»."
	textActiveTicketTy = "\n\nСейчас открыта переписка с HR по обращению #%d: всё, что ты напишешь, уйдёт им, а не мне. " +
		"Когда вопрос решён — нажми «Вопрос закрыт»."
)

// command — /help, /menu, /human (/hr). Работают всегда, и в переписке с HR тоже. false — это
// не команда.
func (b *BotService) command(ctx context.Context, u max.Update, emp *domain.Employee) (bool, error) {
	text := strings.TrimSpace(u.Text)
	if !strings.HasPrefix(text, "/") {
		return false, nil
	}
	cmd := strings.ToLower(strings.Fields(text)[0])
	if i := strings.IndexByte(cmd, '@'); i > 0 {
		cmd = cmd[:i]
	}
	to := recipientOf(emp)
	if to.ChatID == 0 && to.UserID == 0 {
		to = recipient{ChatID: u.ChatID, UserID: u.UserID}
	}
	if ok, err := b.scenarioCommand(ctx, emp, cmd); ok || err != nil { // команды сценария (branch_input.go)
		return true, err
	}
	switch cmd {
	case "/human", "/hr":
		return true, b.human(ctx, u, emp, "")
	case "/menu":
		text = byForm(emp, textMenuTy, textMenuVy)
	default: // /help и всё незнакомое
		text = byForm(emp, textHelpTy, textHelpVy)
	}
	if t, err := b.repos.Tickets.Conversation(ctx, emp.ID); err != nil {
		return true, err
	} else if t != nil && t.Status == domain.TicketHumanActive {
		text += fmt.Sprintf(byForm(emp, textActiveTicketTy, textActiveTicketVy), t.Number)
	}
	logger.Info("бот: команда %s от новичка %s", cmd, emp.ID)
	b.send(ctx, to.msg(text, "", [][]max.Button{humanRow(nil)}))
	return true, nil
}

// --- групповые чаты ---

// botAdded — бота добавили в групповой чат (или удалили). Запоминаем id для справки и пишем его
// в лог: карточки туда пойдут, только если HR пропишет его в MAX_HR_CHAT_ID / MAX_SB_CHAT_ID.
func (b *BotService) botAdded(ctx context.Context, u max.Update, removed bool) error {
	if u.ChatID == 0 {
		return nil
	}
	if err := b.repos.Tickets.SaveChat(ctx, &domain.BotChat{ChatID: u.ChatID, Removed: removed}); err != nil {
		return err
	}
	role := "не назначен"
	switch u.ChatID {
	case b.tickets.hrChat:
		role = "HR-чат"
	case b.tickets.sbChat:
		role = "чат СБ"
	}
	logger.Info("бот: %s, chat_id=%d (%s). Карточки обращений идут в чаты из MAX_HR_CHAT_ID / MAX_SB_CHAT_ID", u.Type, u.ChatID, role)
	return nil
}

// groupChat — событие пришло из группового чата (HR, СБ или любого, куда добавили бота): там бот
// не отвечает, как новичку, и не просит приглашение.
func (b *BotService) groupChat(ctx context.Context, chatID int64) bool {
	if chatID == 0 {
		return false
	}
	if b.tickets != nil && (chatID == b.tickets.hrChat || chatID == b.tickets.sbChat) {
		return true
	}
	ok, err := b.repos.Tickets.IsGroupChat(ctx, chatID)
	if err != nil {
		logger.Warn("бот: чат %d не проверен: %v", chatID, err)
	}
	return ok
}
