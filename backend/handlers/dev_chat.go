package handlers

import (
	"main/domain"
	"main/externalApi/max"
	"main/in"
	"main/out"
	"main/pkg/errs"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Демо-чат кабинета (api_contract.md, раздел «dev»): лента диалога с новичком из журнала
// подделки MAX и нажатия кнопок за новичка. Только APP_ENV=development (devOnly) и только когда
// бот на подделке — с настоящим MAX журнала нет, маршруты 404.

// devChatTarget — подделка и адрес диалога новичка: chat/user из привязки, а до подключения —
// синтетический user_id dev-хука (simulate-start).
func (h *Handler) devChatTarget(c *fiber.Ctx) (*max.Fake, *domain.Employee, int64, int64, error) {
	fake := h.services.FakeMax()
	if fake == nil || h.services.Bot == nil {
		return nil, nil, 0, 0, fiber.ErrNotFound
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return nil, nil, 0, 0, errs.NewValidationError("Некорректный id новичка").
			WithDetails(errs.FieldError{Field: "id", Code: errs.CodeInvalidFormat, Message: "Нужен UUID"})
	}
	card, err := h.services.Newcomers.Get(c.UserContext(), id)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	e := card.Employee
	user := devUserID(id)
	if e.MaxUserID != nil {
		user = *e.MaxUserID
	}
	chat := user
	if e.ChatID != nil {
		chat = *e.ChatID
	}
	return fake, &e, chat, user, nil
}

// DevChat — GET /api/dev/chat/{newcomer_id} → {"messages":[...],"typing":bool}.
func (h *Handler) DevChat(c *fiber.Ctx) error {
	fake, _, chat, user, err := h.devChatTarget(c)
	if err != nil {
		return err
	}
	entries, typing := fake.Chat(chat, user)
	return c.JSON(out.DevChatFrom(entries, typing))
}

// DevChatPress — POST /api/dev/chat/{newcomer_id}/press {"message_id","payload"}: нажатие
// кнопки за новичка тем же путём, что simulate-message с payload, но с callback_id, по которому
// ответ бота (AnswerCallback) правит именно это сообщение.
func (h *Handler) DevChatPress(c *fiber.Ctx) error {
	fake, e, chat, user, err := h.devChatTarget(c)
	if err != nil {
		return err
	}
	var body in.DevChatPress
	if err := bindBody(c, &body); err != nil {
		return err
	}
	input, err := body.Validate()
	if err != nil {
		return err
	}
	if e.Connection != domain.ConnConnected || e.MaxUserID == nil {
		return errs.NewConflictError("Новичок не подключён к боту — сначала simulate-start")
	}
	if !fake.HasMessage(chat, user, input.MessageID) {
		return errs.NewNotFoundError("Сообщения нет в диалоге с новичком")
	}
	cb := fake.JournalPress(chat, user, input.MessageID, input.Payload)
	bot := h.services.Bot
	bot.Handle(c.UserContext(), max.Update{Type: max.UpdateMessageCallback, UserID: user, ChatID: chat, Payload: input.Payload, CallbackID: cb})
	bot.Wait()
	return c.JSON(fiber.Map{"status": "ok"})
}

// devCallbackID — callback_id для нажатия из dev-хука: на подделке — через журнал (правка
// попадёт в нужное сообщение ленты), иначе — прежний синтетический.
func (h *Handler) devCallbackID(chat, user int64, payload string) string {
	if fake := h.services.FakeMax(); fake != nil {
		return fake.JournalPress(chat, user, "", payload)
	}
	return "dev-" + payload
}

// devIncoming — входящее новичка в ленту демо-чата (только на подделке).
func (h *Handler) devIncoming(chat, user int64, kind, text string) {
	if fake := h.services.FakeMax(); fake != nil {
		fake.JournalIncoming(chat, user, kind, text)
	}
}
