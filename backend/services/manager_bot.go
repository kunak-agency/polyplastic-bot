package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"

	"github.com/google/uuid"
)

// Бот для руководителя подразделения: вход по ссылке mgr_<token> и кнопки push. bot.go зовёт
// эти методы точечно — первым делом в started() и callback(); руководитель не новичок, и
// обычная привязка по приглашению его бы не узнала.

const (
	textManagerWelcome = "Здравствуйте! Я Корсик, бот адаптации. Подключил вас как руководителя: %s.\n\n" +
		"Буду писать, когда новичку стоит уделить время: второй низкий пульс подряд, молчит больше 3 дней, " +
		"месяц в компании, скоро решение по испытательному сроку. Ответы новичков и обращения я не пересылаю — только повод встретиться 1:1."
	textManagerBroken = "Ссылка не сработала: она устарела или уже использована. Попросите HR прислать новую."
	textManagerMet    = "Отмечено: встреча проведена. Спасибо!"
	textManagerPlan   = "Отмечено: встреча запланирована. Напомню в кабинете, пока не отметите «Встретился»."
	// Решение по ИС под push D+87 (FEATURE_RISK_V2).
	textManagerIPKeep    = "Записал решение: оставить. HR увидит его в карточке."
	textManagerIPExtend  = "Записал решение: продлить испытательный срок. HR свяжется с вами по оформлению."
	textManagerIPDiscuss = "Передал HR: с вами свяжутся, чтобы обсудить решение."
	textManagerIPAlready = "Решение уже записано: %s."
	// textHRIPDiscuss — карточка в HR-чат: только имя, день и дата, без ответов новичка.
	textHRIPDiscuss = "Руководитель %s просит обсудить решение по испытательному сроку: %s (%s), срок %s."
)

// managerStart — bot_started с payload mgr_<token>. false — это не ссылка руководителя.
func (b *BotService) managerStart(ctx context.Context, u max.Update) (bool, error) {
	token, ok := strings.CutPrefix(strings.TrimSpace(u.Payload), domain.ManagerPayloadPrefix)
	if !ok {
		return false, nil
	}
	to := recipient{ChatID: u.ChatID, UserID: u.UserID}
	repo := b.repos.Manager()
	inv, err := repo.InviteByToken(ctx, token)
	if err != nil {
		return true, err
	}
	now := b.now()
	if inv == nil || inv.ExpiresAt.Before(now) || (inv.UsedBy != nil && *inv.UsedBy != u.UserID) {
		logger.Info("бот: ссылка руководителя не сработала, user=%d", u.UserID)
		b.send(ctx, to.msg(textManagerBroken, "", nil))
		return true, nil
	}
	n, err := repo.Bind(ctx, inv, u.UserID, now)
	if err != nil {
		return true, err
	}
	logger.Info("бот: руководитель подключён user=%d, новичков %d", u.UserID, n)
	b.send(ctx, to.msg(fmt.Sprintf(textManagerWelcome, plural(n, "новичок", "новичка", "новичков")), "",
		[][]max.Button{{{Text: "Мои новички", URL: ManagerPageURL(managerCabinetURL, inv.ManagerName)}}}))
	return true, nil
}

// managerCallback — «Встретился» / «Запланирую» под push. false — кнопка не руководителя.
func (b *BotService) managerCallback(ctx context.Context, u max.Update) (bool, error) {
	if strings.HasPrefix(u.Payload, payMgrIP) {
		return true, b.managerIPDecision(ctx, u)
	}
	var status, rest string
	switch {
	case strings.HasPrefix(u.Payload, payMgrMet):
		status, rest = domain.NudgeMet, strings.TrimPrefix(u.Payload, payMgrMet)
	case strings.HasPrefix(u.Payload, payMgrPlan):
		status, rest = domain.NudgePlanned, strings.TrimPrefix(u.Payload, payMgrPlan)
	default:
		return false, nil
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return true, nil
	}
	repo := b.repos.Manager()
	n, err := repo.Nudge(ctx, id)
	if err != nil {
		return true, err
	}
	if n == nil || n.ManagerUserID != u.UserID {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: textButtonStale})
		return true, nil
	}
	now := b.now()
	n.Status, n.AnsweredAt = status, &now
	if err := repo.SaveNudge(ctx, n); err != nil {
		return true, err
	}
	note := textManagerPlan
	if status == domain.NudgeMet {
		note = textManagerMet
	}
	name := ""
	if e, err := b.repos.Employees.ByID(ctx, n.EmployeeID); err == nil && e != nil {
		name = e.ManagerName
	}
	edited := max.OutMessage{UserID: u.UserID, ChatID: u.ChatID, Text: n.Text + "\n\n" + note,
		Buttons: [][]max.Button{{{Text: "Подробнее", URL: ManagerPageURL(managerCabinetURL, name)}}}}
	b.answer(ctx, u.CallbackID, max.Answer{Notification: note, Message: &edited})
	return true, nil
}

// managerIPDecision — «Оставить» / «Продлить» / «Обсудить с HR» под push D+87. Решение пишется в
// карточку (ip_decision, кем — «руководитель <имя> (MAX)»), «Обсудить» — задача HR (ManagerNudge
// ip_discuss) и карточка в HR-чат. Уже записанное решение кнопка не перезаписывает.
func (b *BotService) managerIPDecision(ctx context.Context, u max.Update) error {
	rest := strings.TrimPrefix(u.Payload, payMgrIP)
	choice, rawID, ok := strings.Cut(rest, ":")
	id, err := uuid.Parse(rawID)
	if !ok || err != nil || (choice != domain.IPKeep && choice != domain.IPExtend && choice != ipDiscussHR) {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
		return nil
	}
	repo := b.repos.Manager()
	n, err := repo.Nudge(ctx, id)
	if err != nil {
		return err
	}
	if n == nil || n.ManagerUserID != u.UserID || n.Kind != domain.NudgeProbation {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: textButtonStale})
		return nil
	}
	e, err := b.repos.Employees.ByID(ctx, n.EmployeeID)
	if err != nil {
		return err
	}
	if e == nil {
		b.answer(ctx, u.CallbackID, max.Answer{Notification: textButtonStale})
		return nil
	}
	now := b.now()
	var note string
	switch {
	case e.IPDecision != nil:
		note = fmt.Sprintf(textManagerIPAlready, ipDecisionRu(*e.IPDecision))
	case choice == ipDiscussHR:
		task := &domain.ManagerNudge{EmployeeID: e.ID, ManagerUserID: u.UserID, Kind: domain.NudgeIPDiscuss,
			Key: domain.NudgeIPDiscuss + ":" + e.ID.String(), Status: domain.NudgeOpen,
			Text: "Руководитель просит обсудить решение по испытательному сроку"}
		created, err := repo.CreateNudge(ctx, task)
		if err != nil {
			return err
		}
		if created {
			b.notifyHRIPDiscuss(ctx, e, now)
		}
		note = textManagerIPDiscuss
	default:
		by := "руководитель " + strings.TrimSpace(e.ManagerName) + " (MAX)"
		if err := b.repos.Employees.SetIPDecision(ctx, e.ID, &choice, now, by); err != nil {
			return err
		}
		logger.Info("бот: руководитель user=%d записал решение по ИС %s новичку %s", u.UserID, choice, e.ID)
		note = textManagerIPKeep
		if choice == domain.IPExtend {
			note = textManagerIPExtend
		}
	}
	n.Status, n.AnsweredAt = domain.NudgeMet, &now
	if err := repo.SaveNudge(ctx, n); err != nil {
		return err
	}
	edited := max.OutMessage{UserID: u.UserID, ChatID: u.ChatID, Text: n.Text + "\n\n" + note,
		Buttons: [][]max.Button{{{Text: "Подробнее", URL: ManagerPageURL(managerCabinetURL, e.ManagerName)}}}}
	b.answer(ctx, u.CallbackID, max.Answer{Notification: note, Message: &edited})
	return nil
}

// notifyHRIPDiscuss — карточка «обсудить решение по ИС» в HR-чат; чата нет — строка в лог.
func (b *BotService) notifyHRIPDiscuss(ctx context.Context, e *domain.Employee, now time.Time) {
	site := domain.SiteOf(e.Site)
	day := DayNumber(e.StartDate, now, site)
	due := civilDate(e.StartDate, site.Loc).AddDate(0, 0, 90).Format("02.01")
	text := fmt.Sprintf(textHRIPDiscuss, strings.TrimSpace(e.ManagerName), e.FullName, dayRu(day), due)
	var chat int64
	if b.tickets != nil {
		chat = b.tickets.hrChat
	}
	if chat == 0 {
		logger.Info("решение по ИС: HR-чат не задан, задача только в кабинете: новичок %s", e.ID)
		return
	}
	b.send(ctx, max.OutMessage{ChatID: chat, Text: text,
		Buttons: [][]max.Button{{{Text: "Открыть карточку", URL: NewcomerPageURL(managerCabinetURL, e.ID)}}}})
}
