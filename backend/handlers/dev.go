package handlers

import (
	"encoding/binary"
	"os"
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
//
// Голосовое: `audio_path` (файл на машине API) или multipart с полями newcomer_id и audio. Только
// на подделке MAX: файл кладётся в неё, бот скачивает его по ссылке fake:// и распознаёт через
// rag /transcribe — тем же путём, что голосовое из MAX.
func (h *Handler) SimulateMessage(c *fiber.Ctx) error {
	var body in.SimulateMessage
	var audio []byte
	if strings.HasPrefix(string(c.Request().Header.ContentType()), fiber.MIMEMultipartForm) {
		body = in.SimulateMessage{NewcomerID: c.FormValue("newcomer_id"), Text: c.FormValue("text"), Payload: c.FormValue("payload"), Flag: c.FormValue("flag")}
		if fh, err := c.FormFile("audio"); err == nil {
			if audio, err = readPart(fh, in.MaxSimulatedAudioBytes); err != nil {
				return err
			}
			if len(audio) == 0 {
				return errs.NewValidationError("Пустой файл audio")
			}
		}
	} else if err := bindBody(c, &body); err != nil {
		return err
	}
	input, err := body.Validate(len(audio) > 0)
	if err != nil {
		return err
	}
	if input.AudioPath != "" {
		if audio, err = readDevAudio(input.AudioPath); err != nil {
			return err
		}
	}
	if int64(len(audio)) > in.MaxSimulatedAudioBytes {
		return errs.NewValidationError("Голосовое больше 20 МБ")
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
	if len(audio) > 0 {
		fake := h.services.FakeMax()
		if fake == nil {
			return errs.NewConflictError("Голосовое в dev — только на подделке MAX (без MAX_BOT_TOKEN)")
		}
		u.Audio = []max.Audio{{URL: fake.PutFile(audio)}}
		h.devIncoming(chat, *e.MaxUserID, max.JournalKindVoice, "🎙 Голосовое")
	} else if input.Payload != "" {
		u = max.Update{Type: max.UpdateMessageCallback, UserID: *e.MaxUserID, ChatID: chat, Payload: input.Payload, CallbackID: h.devCallbackID(chat, *e.MaxUserID, input.Payload)}
	} else {
		h.devIncoming(chat, *e.MaxUserID, max.JournalKindText, input.Text)
	}
	bot.Handle(ctx, u)
	bot.Wait()
	return c.JSON(fiber.Map{"status": "ok"})
}

// readDevAudio — голосовое по audio_path (dev): обычный файл не больше 20 МБ.
func readDevAudio(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return nil, errs.NewValidationError("audio_path: файла нет").
			WithDetails(errs.FieldError{Field: "audio_path", Code: errs.CodeInvalidFormat, Message: "Нет такого файла"})
	}
	if st.Size() > in.MaxSimulatedAudioBytes {
		return nil, errs.NewValidationError("Голосовое больше 20 МБ")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.NewValidationError("audio_path: файл не читается").WithCause(err)
	}
	return data, nil
}
