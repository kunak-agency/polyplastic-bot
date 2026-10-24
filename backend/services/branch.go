package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/flags"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Ветки и действия (api_contract.md v4). Ветка — ответ на кнопку шага или вариант опроса: до
// двух сообщений (кнопки — только терминальные: их нажатие исполняет действия, новых веток нет),
// действия из закрытого списка и then. Payload ссылается на событие, а событие — на (шаг,
// версия): нажатие резолвится по снимку версии, в которой сообщение ушло.

const (
	preTerminal = "bt:" // bt:<event>:<src>:<m>:<j> — терминальная кнопка j сообщения m ветки src
	preContinue = "nx:" // nx:<event> — «Дальше →»
)

// Тексты действий.
const (
	textFreeTextAsk   = "Напишите одним сообщением — я передам."
	textFreeTextAskTy = "Напиши одним сообщением — я передам."
	textFileMissing   = "Файл пока не загружен — HR пришлёт его отдельно."
	textPlanMissing   = "Плана ещё нет — я напомнил HR, он пришлёт его сюда."
)

// branchSrc — откуда ветка: кнопка шага i («3») или вариант v вопроса q опроса («q1v2»),
// правило после опроса k («o0»).
func branchSrcButton(i int) string { return strconv.Itoa(i) }
func branchSrcOption(q, v int) string {
	return "q" + strconv.Itoa(q) + "v" + strconv.Itoa(v)
}
func branchSrcOutcome(k int) string { return "o" + strconv.Itoa(k) }

// buttonBranch — ветка кнопки i: своя или общая (Branch.On другой кнопки).
func buttonBranch(s domain.ScenarioStep, i int) *domain.Branch {
	if i < 0 || i >= len(s.Buttons) {
		return nil
	}
	if br := s.Buttons[i].Branch; br != nil {
		return br
	}
	for _, b := range s.Buttons {
		if b.Branch != nil && containsInt(b.Branch.On, i) {
			return b.Branch
		}
	}
	return nil
}

// optionBranch — ветка варианта v вопроса: своя или общая (On).
func optionBranch(q domain.SurveyQuestion, v int) *domain.Branch {
	if br := q.Branches[strconv.Itoa(v)]; br != nil {
		return br
	}
	for _, br := range q.Branches {
		if br != nil && containsInt(br.On, v) {
			return br
		}
	}
	return nil
}

// resolveBranch — ветка по src из payload.
func resolveBranch(s domain.ScenarioStep, src string) *domain.Branch {
	switch {
	case strings.HasPrefix(src, "q"):
		qs, vs, ok := strings.Cut(src[1:], "v")
		q, err1 := strconv.Atoi(qs)
		v, err2 := strconv.Atoi(vs)
		if !ok || err1 != nil || err2 != nil || s.Survey == nil || q < 0 || q >= len(s.Survey.Questions) {
			return nil
		}
		return optionBranch(s.Survey.Questions[q], v)
	case strings.HasPrefix(src, "o"):
		k, err := strconv.Atoi(src[1:])
		if err != nil || s.Survey == nil || k < 0 || k >= len(s.Survey.Outcomes) {
			return nil
		}
		o := s.Survey.Outcomes[k]
		return &domain.Branch{Messages: o.Messages, Actions: o.Actions}
	}
	i, err := strconv.Atoi(src)
	if err != nil {
		return nil
	}
	return buttonBranch(s, i)
}

// actionOf — действие, закодированное в action кнопки (импорт ТЗ): «escalate:<route>»,
// «handoff:<to>». ok=false — обычный код ответа/ссылка.
func actionOf(action string) (domain.Action, bool) {
	kind, arg, _ := strings.Cut(strings.TrimSpace(action), ":")
	switch kind {
	case domain.ActionEscalate:
		return domain.Action{Type: domain.ActionEscalate, Route: arg}, true
	case domain.ActionHandoff:
		return domain.Action{Type: domain.ActionHandoff, To: arg}, true
	}
	return domain.Action{}, false
}

// withButtonAction — действия ветки + действие из action кнопки; «escalate» с opens_free_text —
// обращение после текста (open_free_text).
// Каноническая форма (escalate в branch.actions + opens_free_text) — так же.
func withButtonAction(acts []domain.Action, b domain.StepButton) []domain.Action {
	out := append([]domain.Action{}, acts...)
	if a, ok := actionOf(b.Action); ok {
		out = append(out, a)
	}
	if len(out) == 0 {
		return acts
	}
	if _, esc := hasAction(out, domain.ActionEscalate); esc && b.OpensFreeText {
		if _, has := hasAction(out, domain.ActionOpenFreeText); !has {
			out = append(out, domain.Action{Type: domain.ActionOpenFreeText})
		}
	}
	return out
}

func containsInt(l []int, v int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func hasAction(acts []domain.Action, t string) (domain.Action, bool) {
	for _, a := range acts {
		if a.Type == t {
			return a, true
		}
	}
	return domain.Action{}, false
}

func hasTerminal(br *domain.Branch) bool {
	if br == nil {
		return false
	}
	for _, m := range br.Messages {
		if len(m.Buttons) > 0 {
			return true
		}
	}
	return false
}

// applySilent — действия без сообщений (в транзакции, над заблокированной карточкой):
// set_field, set_flag; open_free_text → ждём текст. Возвращает «флаг включён» и «ждём текст».
func applySilent(e *domain.Employee, acts []domain.Action, evID uuid.UUID, label string, br *domain.Branch) (flagged, freeText bool) {
	for _, a := range acts {
		switch a.Type {
		case domain.ActionSetField:
			if a.Field == "address_form" && (a.Value == domain.AddressTy || a.Value == domain.AddressVy) {
				v := a.Value
				e.AddressForm = &v
			}
		case domain.ActionSetFlag:
			if a.Flag != "" {
				if e.Flags == nil {
					e.Flags = domain.BoolMap{}
				}
				v := true
				if a.Value == "false" || a.Value == "0" {
					v = false
				}
				e.Flags[a.Flag] = v
				flagged = flagged || v
			}
		case domain.ActionOpenFreeText:
			freeText = true
		}
	}
	if freeText {
		w := &domain.FreeTextWait{EventID: evID, Label: label}
		if esc, ok := hasAction(acts, domain.ActionEscalate); ok {
			w.Category, w.Route, w.NotifyManager = esc.Category, esc.Route, esc.NotifyManager
		}
		if br != nil {
			w.Then, w.Goto = br.Then, br.Goto
		}
		e.AwaitFreeText = w
	}
	return flagged, freeText
}

// pressPlan — что сделать после транзакции нажатия.
type pressPlan struct {
	emp      *domain.Employee
	ev       domain.ScheduledEvent
	step     domain.ScenarioStep
	src      string
	branch   *domain.Branch // ветка: сообщения и действия (nil — только записать)
	messages bool           // слать сообщения ветки (терминальная кнопка — нет)
	acts     []domain.Action
	label    string
	flagged  bool
	freeText bool // ждём текст новичка
	edit     *max.OutMessage
	thenOf   *domain.Branch // чей then применять, когда ветка закончится
	thenGo   bool           // применить then (поток блока ждал это нажатие)
	resume   bool           // ветка варианта опроса ждала текст или эту кнопку — дальше вопрос опроса
}

// stepButton — кнопка обычного шага: ответ записывается, сообщение правится в «✅ …», затем
// ветка кнопки и следующая реплика блока.
func (b *BotService) stepButton(ctx context.Context, u max.Update, empID uuid.UUID, rest string) error {
	evRaw, idxRaw, _ := strings.Cut(rest, ":")
	evID, err1 := uuid.Parse(evRaw)
	idx, err2 := strconv.Atoi(idxRaw)
	if err1 != nil || err2 != nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	var reply max.Answer
	var plan *pressPlan
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		if ev == nil || ev.Status != domain.EventSent || idx < 0 || idx >= len(step.Buttons) {
			reply = max.Answer{Notification: "Уже записал"}
			return nil
		}
		btn := step.Buttons[idx]
		if accessMissing(btn) {
			// «Чего-то нет» (UX 2.3, D0 12:00): сначала причины, обращение F2 — после выбора.
			m := accessReasonsMessage(recipientOf(e), step, e, ev, sentAt(ev, b.now()))
			reply = max.Answer{Notification: "Что именно?", Message: &m}
			return nil
		}
		ev.Status, ev.Answer = domain.EventAnswered, &btn.Label
		if btn.Action == "skip" {
			ev.Status = domain.EventSkipped
		}
		ev.Waiting = ""
		now := b.now()
		e.LastActivityAt = &now
		br := buttonBranch(step, idx)
		p := &pressPlan{step: step, src: branchSrcButton(idx), branch: br, messages: true, label: btn.Label, thenOf: br, thenGo: true}
		var acts []domain.Action
		if br != nil {
			acts = br.Actions
		}
		if acts = withButtonAction(acts, btn); len(acts) > 0 {
			p.acts = acts
			p.flagged, p.freeText = applySilent(e, acts, ev.ID, btn.Label, br)
		}
		if btn.OpensFreeText && !p.freeText {
			p.freeText = true
			e.AwaitFreeText = &domain.FreeTextWait{EventID: ev.ID, Label: btn.Label}
			if br != nil {
				e.AwaitFreeText.Then, e.AwaitFreeText.Goto = br.Then, br.Goto
			}
		}
		switch {
		case hasTerminal(br):
			ev.Waiting = domain.WaitBranch
		case p.freeText:
			ev.Waiting = domain.WaitFreeText
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		done := recipientOf(e).msg(stepAnsweredText(step, e, btn.Label, ev.IsTest, sentAt(ev, now)), "", nil)
		reply = max.Answer{Notification: "Записал", Message: &done}
		p.emp, p.ev, p.edit = e, *ev, &done
		plan = p
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		return err
	}
	b.reply(ctx, u.CallbackID, reply, plan)
	if plan != nil {
		b.runPlan(ctx, plan)
	}
	return nil
}

// reply — ответ на нажатие; эмуляция без callback (send-test «Пройти этот путь») правит
// сообщение напрямую.
func (b *BotService) reply(ctx context.Context, callbackID string, a max.Answer, p *pressPlan) {
	if callbackID != "" {
		b.answer(ctx, callbackID, a)
		return
	}
	if p != nil && a.Message != nil && p.ev.MessageID != "" {
		if err := b.max.Edit(ctx, p.ev.MessageID, *a.Message); err != nil {
			logger.Warn("бот: правка сообщения %s не ушла: %v", p.ev.ID, err)
		}
	}
}

// terminalButton — кнопка в сообщении ветки: только действия и запись, без новых веток.
func (b *BotService) terminalButton(ctx context.Context, u max.Update, empID uuid.UUID, rest string) error {
	parts := strings.Split(rest, ":")
	var evID uuid.UUID
	var m, j int
	var err error
	ok := len(parts) == 4
	if ok {
		evID, err = uuid.Parse(parts[0])
		ok = err == nil
	}
	if ok {
		m, err = strconv.Atoi(parts[2])
		ok = err == nil
	}
	if ok {
		j, err = strconv.Atoi(parts[3])
		ok = err == nil
	}
	if !ok {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	src := parts[1]
	var reply max.Answer
	var plan *pressPlan
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, step, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil {
			return err
		}
		br := resolveBranch(step, src)
		if ev == nil || br == nil || m < 0 || m >= len(br.Messages) || j < 0 || j >= len(br.Messages[m].Buttons) {
			reply = max.Answer{Notification: "Эта кнопка больше не работает"}
			return nil
		}
		tb := br.Messages[m].Buttons[j]
		pressed := "→ " + tb.Label
		if ev.Answer != nil && strings.Contains(*ev.Answer, pressed) {
			reply = max.Answer{Notification: "Уже записал"}
			return nil
		}
		ans := pressed
		if ev.Answer != nil {
			ans = *ev.Answer + " " + pressed
		}
		ev.Answer = &ans
		wasWaiting := ev.Waiting == domain.WaitBranch && !strings.HasPrefix(src, "q") && !strings.HasPrefix(src, "o")
		if wasWaiting {
			ev.Waiting = ""
		}
		now := b.now()
		e.LastActivityAt = &now
		p := &pressPlan{step: step, src: src, label: tb.Label, thenOf: br, thenGo: wasWaiting}
		var acts []domain.Action
		if tb.Branch != nil {
			acts = tb.Branch.Actions
		}
		if acts = withButtonAction(acts, tb); len(acts) > 0 {
			p.acts = acts
			p.flagged, p.freeText = applySilent(e, acts, ev.ID, tb.Label, br)
		}
		if tb.OpensFreeText && !p.freeText {
			p.freeText = true
			e.AwaitFreeText = &domain.FreeTextWait{EventID: ev.ID, Label: tb.Label, Then: br.Then, Goto: br.Goto}
		}
		// Ветка варианта опроса ждала «ответ в чате или кнопку» (ТЗ r63): выбрана кнопка — текст
		// больше не ждём, следующий вопрос опроса.
		if strings.HasPrefix(src, "q") && !p.freeText && e.AwaitFreeText != nil && e.AwaitFreeText.EventID == ev.ID {
			e.AwaitFreeText, p.resume = nil, true
		}
		if p.freeText && wasWaiting {
			ev.Waiting = domain.WaitFreeText // then — после текста
			p.thenGo = false
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		bm := br.Messages[m]
		done := recipientOf(e).msg(branchText(bm, e, ev)+"\n\n✅ "+tb.Label, "", nil)
		reply = max.Answer{Notification: "Записал", Message: &done}
		p.emp, p.ev = e, *ev
		plan = p
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, reply)
	if plan != nil {
		b.runPlan(ctx, plan)
	}
	return nil
}

// continuePress — «Дальше →»: клавиатура убирается (без «✅»), уходит следующая реплика.
func (b *BotService) continuePress(ctx context.Context, u max.Update, empID uuid.UUID, rest string) error {
	evID, err := uuid.Parse(rest)
	if err != nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	var reply max.Answer
	release := false
	err = b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		ev, e, _, err := b.loadEvent(ctx, tx, evID, empID)
		if err != nil || ev == nil {
			return err
		}
		if ev.Waiting != domain.WaitContinue {
			reply = max.Answer{Notification: "Уже дальше"}
			return nil
		}
		ev.Waiting, release = "", true
		now := b.now()
		e.LastActivityAt = &now
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		text := ""
		if ev.Text != nil {
			text = *ev.Text
		}
		m := recipientOf(e).msg(text, "", nil)
		reply = max.Answer{Notification: " ", Message: &m}
		return tx.Events.Save(ctx, ev)
	})
	if err != nil {
		return err
	}
	b.answer(ctx, u.CallbackID, reply)
	if release {
		return b.scheduler.Release(ctx, empID, evID)
	}
	return nil
}

// branchText — текст сообщения ветки в форме обращения новичка.
func branchText(m domain.BranchMessage, e *domain.Employee, ev *domain.ScheduledEvent) string {
	var at time.Time
	if ev != nil && ev.SentAt != nil {
		at = *ev.SentAt
	}
	text := stepText(domain.ScenarioStep{TextTy: m.TextTy, TextVy: m.TextVy}, e, at)
	if ev != nil && ev.IsTest {
		text = testPrefix + text
	}
	return text
}

// branchMessage — сообщение ветки: текст, вложения, терминальные кнопки и «Спросить человека».
func (b *BotService) branchMessage(ctx context.Context, e *domain.Employee, ev *domain.ScheduledEvent, src string, mi int, m domain.BranchMessage) max.OutMessage {
	var btns []max.Button
	for j, tb := range m.Buttons {
		if tb.Action == "human" {
			continue
		}
		btn := max.Button{Text: tb.Label, Payload: fmt.Sprintf("%s%s:%s:%d:%d", preTerminal, ev.ID, src, mi, j)}
		if u := buttonURL(tb.Action); u != "" {
			btn = max.Button{Text: tb.Label, URL: u}
		}
		btns = append(btns, btn)
	}
	rows := packRows(btns)
	if len(m.Buttons) > 0 {
		id := ev.ID
		rows = append(rows, humanRow(&id))
	}
	msg := recipientOf(e).msg(branchText(m, e, ev), "", rows)
	msg.Attachments = b.attach(ctx, e, m.Media)
	if emptyMessage(msg) {
		logger.Warn("бот: реплика ветки «%s» без текста и без вложений (файла нет в медиатеке) — новичку %s запасной текст", src, e.ID)
		msg.Text = strings.TrimSuffix(msg.Text, " ") + byForm(e, textMaterialSoonTy, textMaterialSoonVy)
	}
	return msg
}

// Запасной текст реплики, у которой нет ни текста, ни найденного вложения (заготовка ветки
// «да» вопроса без вариантов, а материал HR ещё не загрузил): пустое сообщение не уходит.
const (
	textMaterialSoonVy = "Материал скоро добавим. А пока можно спросить меня или HR."
	textMaterialSoonTy = "Материал скоро добавим. А пока можешь спросить меня или HR."
)

// emptyMessage — в сообщении нет ни текста (кроме пометки [ТЕСТ]), ни вложений.
func emptyMessage(m max.OutMessage) bool {
	return len(m.Attachments) == 0 && strings.TrimSpace(strings.TrimPrefix(m.Text, testPrefix)) == ""
}

// runPlan — после транзакции нажатия: сообщения ветки, видимые действия, then.
func (b *BotService) runPlan(ctx context.Context, p *pressPlan) {
	e, ev := p.emp, &p.ev
	if p.branch != nil && p.messages {
		for mi, m := range p.branch.Messages {
			if mi >= 2 { // ≤ 2 сообщений в ветке
				break
			}
			b.send(ctx, b.branchMessage(ctx, e, ev, p.src, mi, m))
		}
	}
	freeAsk := ""
	for _, a := range p.acts {
		switch a.Type {
		case domain.ActionSendFile:
			b.sendFile(ctx, e, a)
		case domain.ActionEscalate:
			if _, ok := hasAction(p.acts, domain.ActionOpenFreeText); ok {
				continue // обращение — когда придёт текст
			}
			b.escalate(ctx, e, ev, p.step, a, p.label, "")
		case domain.ActionHandoff:
			b.handoff(ctx, e, ev, p.step, a, p.label)
		case domain.ActionOpenFreeText:
			freeAsk = orDefault(a.Prompt, byForm(e, textFreeTextAskTy, textFreeTextAsk))
		}
	}
	if p.freeText {
		b.send(ctx, recipientOf(e).msg(orDefault(freeAsk, byForm(e, textFreeTextAskTy, textFreeTextAsk)), "", nil))
	}
	if p.flagged {
		if err := b.scheduler.FlagChanged(ctx, e.ID); err != nil {
			logger.Warn("бот: шаги по флагу новичка %s не отпущены: %v", e.ID, err)
		}
	}
	if p.thenGo && p.ev.Waiting == "" {
		b.applyThen(ctx, e.ID, ev.ID, p.thenOf)
	}
	if p.resume {
		b.resumeSurvey(ctx, e.ID, ev.ID)
	}
}

// resumeSurvey — текущий вопрос опроса новым сообщением: ветка варианта с «ответь в чате или
// нажми кнопку» (open_free_text, ТЗ r63 Б/В) держала следующий вопрос, пока новичок не ответит, —
// иначе причина ушла бы ответом на следующий вопрос.
func (b *BotService) resumeSurvey(ctx context.Context, empID, evID uuid.UUID) {
	ev, err := b.repos.Events.ByIDForUpdate(ctx, evID) // вне транзакции — просто чтение
	if err != nil || ev == nil || ev.Status != domain.EventSent || ev.Survey == nil {
		return
	}
	e, err := b.repos.Employees.ByID(ctx, empID)
	if err != nil || e == nil {
		return
	}
	var step domain.ScenarioStep
	var ok bool
	if ev.Snapshot != nil {
		step, ok = domain.ScenarioStep(*ev.Snapshot), true
	} else if v, err := b.repos.Scenarios.Version(ctx, e.ScenarioID, ev.ScenarioVersion); err == nil && v != nil {
		step, ok = v.Step(ev.StepID)
	}
	if !ok || !isSurvey(step) || ev.Survey.Q >= len(step.Survey.Questions) {
		return
	}
	msg := surveyMessage(recipientOf(e), step, e, ev, b.now())
	b.resend(ctx, surveyOutcome{msg: &msg, eventID: evID})
}

// applyThen — continue (по умолчанию) | end_block | goto_block.
func (b *BotService) applyThen(ctx context.Context, empID, evID uuid.UUID, br *domain.Branch) {
	then, goTo := domain.ThenContinue, ""
	if br != nil && br.Then != "" {
		then, goTo = br.Then, br.Goto
	}
	var err error
	switch then {
	case domain.ThenEndBlock:
		if err = b.scheduler.EndBlock(ctx, empID, evID, skipEndBlock); err == nil {
			err = b.scheduler.Release(ctx, empID, evID)
		}
	case domain.ThenGoto:
		err = b.scheduler.GotoBlock(ctx, empID, evID, goTo)
	default:
		err = b.scheduler.Release(ctx, empID, evID)
	}
	if err != nil {
		logger.Warn("бот: поток после ветки %s не продолжен: %v", evID, err)
	}
}

// sendFile — send_file: файл медиатеки (per_tag — под площадку новичка).
func (b *BotService) sendFile(ctx context.Context, e *domain.Employee, a domain.Action) {
	site := ""
	if a.PerTag {
		site = e.Site
	}
	refs := []domain.MediaRef{{Name: a.Media}}
	if p, ok := strings.CutPrefix(a.Media, "personal:"); ok {
		refs = []domain.MediaRef{{Personal: p}}
	}
	att, missing := attachRefs(ctx, attachDeps{media: b.media, repos: b.repos, max: b.max, tickets: b.tickets}, e, refs, site)
	if len(missing) > 0 {
		personalMissingTask(ctx, b.tickets, e, missing[0], "send_file")
		b.send(ctx, recipientOf(e).msg(textPlanMissing, "", nil))
		return
	}
	if len(att) == 0 {
		logger.Warn("бот: send_file «%s» новичку %s — файла нет", a.Media, e.ID)
		b.send(ctx, recipientOf(e).msg(textFileMissing, "", nil))
		return
	}
	m := recipientOf(e).msg("📎 "+a.Media, "", nil)
	m.Attachments = att
	b.send(ctx, m)
}

// escalate — обращение v2: категория и маршрут из ветки, notify_manager. quote — текст новичка
// (open_free_text).
func (b *BotService) escalate(ctx context.Context, emp *domain.Employee, ev *domain.ScheduledEvent, step domain.ScenarioStep, a domain.Action, label, quote string) {
	if b.tickets == nil {
		return
	}
	code := a.Category
	if code == "" {
		code = taxonomy.CategoryQuestion
	}
	var res handoffResult
	var e *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if e, err = tx.Employees.ByIDForUpdate(ctx, emp.ID); err != nil || e == nil {
			return err
		}
		cat := b.tickets.category(code, routeOr(a.Route, taxonomy.RouteHR))
		if a.Route != "" {
			cat.Route = a.Route
		}
		if a.NotifyManager && !cat.ManagerHidden {
			cat.NotifyManager = true
		}
		evID := eventRef(ev)
		what := fmt.Sprintf("Новичок выбрал «%s» в шаге «%s».", label, step.Title)
		if quote != "" {
			what = fmt.Sprintf("Новичок ответил своими словами после «%s» в шаге «%s».", label, step.Title)
		}
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{cat: cat, source: domain.TicketFromAnswer, what: what, quote: quote, eventID: evID})
		res = handoffResult{ticket: t, created: created}
		return err
	})
	if err != nil || e == nil || res.ticket == nil {
		if err != nil {
			logger.Warn("бот: escalate новичка %s не создано: %v", emp.ID, err)
		}
		return
	}
	logger.Info("бот: ветка «%s» → обращение #%d %s %s", step.Title, res.ticket.Number, res.ticket.Category, res.ticket.Route)
	b.afterHandoff(ctx, e, &res)
}

// eventRef — id события для обращения; у команды события нет.
func eventRef(ev *domain.ScheduledEvent) *uuid.UUID {
	if ev == nil || ev.ID == uuid.Nil {
		return nil
	}
	id := ev.ID
	return &id
}

func routeOr(r, def string) string {
	if r == "" {
		return def
	}
	return r
}

// handoff — «Связаться с человеком»: обращение к recruiter|hr|security, сразу human_active —
// переписка идёт через бота.
func (b *BotService) handoff(ctx context.Context, emp *domain.Employee, ev *domain.ScheduledEvent, step domain.ScenarioStep, a domain.Action, label string) {
	if b.tickets == nil {
		return
	}
	to := routeOr(a.To, routeOr(a.Route, taxonomy.RouteRecruiter))
	var res handoffResult
	var e *domain.Employee
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if e, err = tx.Employees.ByIDForUpdate(ctx, emp.ID); err != nil || e == nil {
			return err
		}
		target := domain.TargetHR
		switch to {
		case taxonomy.RouteRecruiter:
			target = domain.TargetRecruiter
		case taxonomy.RouteSecurity:
			target = domain.TargetSecurity
		}
		evID := eventRef(ev)
		h := domain.HumanRequest{EmployeeID: e.ID, EventID: evID, Source: domain.HumanFromAnswer, Flags: domain.StringList{}, Target: target}
		if err := tx.Employees.AddHumanRequest(ctx, &h); err != nil {
			return err
		}
		cat := taxonomy.Question(to)
		t, created, err := b.tickets.open(ctx, tx, e, ticketDraft{
			cat: cat, source: domain.TicketFromAnswer, humanRequestID: &h.ID, eventID: evID, withContext: true,
			what: fmt.Sprintf("Новичок нажал «%s» в шаге «%s» — просит связаться с человеком (%s).", label, step.Title, to),
		})
		if err != nil {
			return err
		}
		if created {
			t.Status = domain.TicketHumanActive
			if err := tx.Tickets.Save(ctx, t); err != nil {
				return err
			}
		}
		res = handoffResult{ticket: t, created: created}
		return nil
	})
	if err != nil || e == nil || res.ticket == nil {
		if err != nil {
			logger.Warn("бот: handoff новичка %s не создан: %v", emp.ID, err)
		}
		return
	}
	logger.Info("бот: handoff «%s» → обращение #%d (%s, human_active)", step.Title, res.ticket.Number, to)
	b.afterHandoff(ctx, e, &res)
}

// freeText — текст новичка после «Другое» / open_free_text: regex-флаги → обращение escalate
// (категория флага, иначе из ветки), /flags в фоне; затем then ветки. В RAG не уходит.
func (b *BotService) freeText(ctx context.Context, emp *domain.Employee, text string) error {
	var w *domain.FreeTextWait
	var e *domain.Employee
	var ev *domain.ScheduledEvent
	var step domain.ScenarioStep
	release := false
	codes := flags.Codes(text)
	err := b.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		var err error
		if e, err = tx.Employees.ByIDForUpdate(ctx, emp.ID); err != nil || e == nil || e.AwaitFreeText == nil {
			return err
		}
		w = e.AwaitFreeText
		e.AwaitFreeText = nil
		now := b.now()
		e.LastActivityAt = &now
		if codes != nil {
			markRisk(e, domain.RiskRed, "сигнал "+strings.Join(codes, ", ")+" в ответе «свой вариант»")
		}
		if err := tx.Employees.Save(ctx, e); err != nil {
			return err
		}
		x, _, st, err := b.loadEvent(ctx, tx, w.EventID, e.ID)
		if err != nil || x == nil {
			return err
		}
		ev, step = x, st
		ans := w.Label + ": свой ответ"
		if x.Answer != nil && *x.Answer != w.Label {
			ans = *x.Answer + " (свой ответ)"
		}
		x.Answer = &ans
		if x.Waiting == domain.WaitFreeText {
			x.Waiting, release = "", true
		}
		return tx.Events.Save(ctx, x)
	})
	if err != nil || w == nil || e == nil {
		return err
	}
	logger.Info("бот: свой ответ новичка %s к событию %s, флаги %v (в rag не отправлено)", e.ID, w.EventID, codes)
	if ev == nil {
		ev = &domain.ScheduledEvent{ID: w.EventID}
	}
	a := domain.Action{Type: domain.ActionEscalate, Category: w.Category, Route: w.Route, NotifyManager: w.NotifyManager}
	if codes != nil {
		a.Category, a.Route = b.flagCategory(codes).Code, ""
	}
	b.escalate(ctx, e, ev, step, a, w.Label, text)
	if codes == nil {
		b.classifyAsync(ctx, e, flagInput{text: text, where: "в ответе «свой вариант» на «" + step.Title + "»"})
	}
	if isSurvey(step) {
		b.resumeSurvey(ctx, e.ID, w.EventID) // ветка варианта опроса ждала этот текст
	}
	if release {
		b.applyThen(ctx, e.ID, w.EventID, &domain.Branch{Then: w.Then, Goto: w.Goto})
	}
	return nil
}
