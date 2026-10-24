package handlers

import (
	"encoding/binary"
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/in"
	"main/out"
	"main/pkg/errs"
	"main/services"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Payload кнопок системных сообщений бота (services/bot_render.go: payAddress*, payConsent*).
// Дублируются здесь строками: dev-хук играет за новичка снаружи, как это делал бы MAX.
const (
	devPayAddressPrefix = "a:"
	devPayConsentYes    = "c:yes"
	devPayConsentNo     = "c:no"
)

// devOnly — 404 вне development: маршрут для чужого глаза не существует.
func devOnly(c *fiber.Ctx) error {
	if !isDev() {
		return fiber.ErrNotFound
	}
	return c.Next()
}

// devUserID — синтетический user_id MAX для новичка: стабильный (повтор вызова — тот же
// аккаунт, приглашение не «занято другим») и вне диапазона настоящих id. Берём последние
// байты: в UUIDv7 первые — время, и новички, заведённые за минуту, склеивались в один аккаунт.
func devUserID(id uuid.UUID) int64 {
	return 9_000_000_000_000 + int64(binary.BigEndian.Uint32(id[12:]))
}

// SimulateStart — POST /api/dev/simulate-start (только APP_ENV=development, api_contract.md,
// раздел «dev»): апдейт bot_started с payload приглашения идёт в обработчик бота, как из
// long polling. Нужен, чтобы машина времени работала без телефона: без подключения она 409.
func (h *Handler) SimulateStart(c *fiber.Ctx) error {
	var body in.SimulateStart
	if err := bindBody(c, &body); err != nil {
		return err
	}
	input, err := body.Validate()
	if err != nil {
		return err
	}
	bot := h.services.Bot
	if bot == nil {
		return errs.NewConflictError("Бот выключен: нет ни MAX_BOT_TOKEN, ни подделки MAX")
	}
	ctx := c.UserContext()
	card, err := h.services.Newcomers.Get(ctx, input.NewcomerID)
	if err != nil {
		return err
	}
	payload := invitePayload(card.InviteLink)
	if payload == "" {
		return errs.NewConflictError("У новичка нет действующего приглашения")
	}
	user := devUserID(input.NewcomerID)
	h.devIncoming(user, user, max.JournalKindStart, "")
	bot.Handle(ctx, max.Update{Type: max.UpdateBotStarted, UserID: user, ChatID: user, Payload: payload})
	if input.AddressForm != "" {
		bot.Handle(ctx, h.devCallback(user, devPayAddressPrefix+input.AddressForm))
	}
	switch input.Consent {
	case "given":
		bot.Handle(ctx, h.devCallback(user, devPayConsentYes))
	case "declined":
		bot.Handle(ctx, h.devCallback(user, devPayConsentNo))
	}
	if card, err = h.services.Newcomers.Get(ctx, input.NewcomerID); err != nil {
		return err
	}
	if card.Employee.Connection != domain.ConnConnected {
		return errs.NewConflictError("Приглашение не сработало: просрочено или занято другим аккаунтом")
	}
	return c.JSON(out.NewcomerFrom(*card))
}

func (h *Handler) devCallback(user int64, payload string) max.Update {
	return max.Update{Type: max.UpdateMessageCallback, UserID: user, ChatID: user, Payload: payload, CallbackID: h.devCallbackID(user, user, payload)}
}

// invitePayload — inv_<token> из ссылки-приглашения любого формата (INVITE_LINK_FORMAT).
func invitePayload(link string) string {
	i := strings.Index(link, services.InvitePayloadPrefix)
	if i < 0 {
		return ""
	}
	p := link[i:]
	if j := strings.IndexAny(p, "&/?#"); j >= 0 {
		p = p[:j]
	}
	return p
}

// SimulateMessage — POST /api/dev/simulate-message (только APP_ENV=development): текст или
// нажатие кнопки от подключённого новичка уходит в обработчик бота, как из long polling. Ответ —
// {"status":"ok"}; что ушло новичку, видно в логе подделки MAX. Нужен, чтобы показать обращения
// без телефона.
func (h *Handler) SimulateMessage(c *fiber.Ctx) error {
	var body in.SimulateMessage
	if err := bindBody(c, &body); err != nil {
		return err
	}
	input, err := body.Validate()
	if err != nil {
		return err
	}
	bot := h.services.Bot
	if bot == nil {
		return errs.NewConflictError("Бот выключен: нет ни MAX_BOT_TOKEN, ни подделки MAX")
	}
	ctx := c.UserContext()
	card, err := h.services.Newcomers.Get(ctx, input.NewcomerID)
	if err != nil {
		return err
	}
	e := card.Employee
	if e.Connection != domain.ConnConnected || e.MaxUserID == nil {
		return errs.NewConflictError("Новичок не подключён к боту — сначала simulate-start")
	}
	chat := *e.MaxUserID
	if e.ChatID != nil {
		chat = *e.ChatID
	}
	if input.Flag != "" {
		h.devIncoming(chat, *e.MaxUserID, max.JournalKindText, input.Text)
		if err := bot.SimulateFlag(ctx, e.ID, input.Flag, input.Text); err != nil {
			return err
		}
		bot.Wait()
		return c.JSON(fiber.Map{"status": "ok"})
	}
	u := max.Update{Type: max.UpdateMessageCreated, UserID: *e.MaxUserID, ChatID: chat, Text: input.Text}
	if input.Payload != "" {
		u = max.Update{Type: max.UpdateMessageCallback, UserID: *e.MaxUserID, ChatID: chat, Payload: input.Payload, CallbackID: h.devCallbackID(chat, *e.MaxUserID, input.Payload)}
	} else {
		h.devIncoming(chat, *e.MaxUserID, max.JournalKindText, input.Text)
	}
	bot.Handle(ctx, u)
	bot.Wait()
	return c.JSON(fiber.Map{"status": "ok"})
}
