package services

import (
	"context"
	"fmt"
	"strings"

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
		b.answer(ctx, u.CallbackID, max.Answer{Notification: "Эта кнопка больше не работает"})
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
