package in

import (
	"strings"

	"main/pkg/errs"

	"github.com/google/uuid"
)

// SimulateStart — тело POST /api/dev/simulate-start (только APP_ENV=development): сыграть за
// новичка открытие ссылки-приглашения, а по желанию — и ответы на знакомство и согласие.
type SimulateStart struct {
	NewcomerID  string  `json:"newcomer_id"`
	AddressForm *string `json:"address_form"` // ty | vy | null — не отвечать
	Consent     *string `json:"consent"`      // given | declined | null — не отвечать
}

// SimulateStartInput — проверенное тело.
type SimulateStartInput struct {
	NewcomerID  uuid.UUID
	AddressForm string // "" — не отвечать
	Consent     string // "" — не отвечать
}

func (s SimulateStart) Validate() (SimulateStartInput, error) {
	var f fields
	out := SimulateStartInput{NewcomerID: f.uuid("newcomer_id", s.NewcomerID)}
	if s.AddressForm != nil {
		switch v := strings.TrimSpace(*s.AddressForm); v {
		case "", "ty", "vy":
			out.AddressForm = v
		default:
			f.add("address_form", errs.CodeOutOfRange, "Обращение: ty, vy или null")
		}
	}
	if s.Consent != nil {
		switch v := strings.TrimSpace(*s.Consent); v {
		case "", "given", "declined":
			out.Consent = v
		default:
			f.add("consent", errs.CodeOutOfRange, "Согласие: given, declined или null")
		}
	}
	return out, f.err()
}

// SimulateMessage — тело POST /api/dev/simulate-message (только development): сыграть за
// подключённого новичка текст или нажатие кнопки (payload), как из long polling. Нужен, чтобы
// показать обращения без телефона.
type SimulateMessage struct {
	NewcomerID string `json:"newcomer_id"`
	Text       string `json:"text"`
	Payload    string `json:"payload"`
	// Flag — необязательно, только с text: текст считается размеченным этим кодом F0–F10, как
	// ответ /flags (бот его пока не вызывает). Так в dev показываются F4, F10 и др. без regex.
	Flag string `json:"flag"`
}

// SimulateMessageInput — проверенное тело: ровно одно из Text и Payload.
type SimulateMessageInput struct {
	NewcomerID uuid.UUID
	Text       string
	Payload    string
	Flag       string
}

func (s SimulateMessage) Validate() (SimulateMessageInput, error) {
	var f fields
	out := SimulateMessageInput{NewcomerID: f.uuid("newcomer_id", s.NewcomerID), Text: strings.TrimSpace(s.Text), Payload: strings.TrimSpace(s.Payload), Flag: strings.ToUpper(strings.TrimSpace(s.Flag))}
	if (out.Text == "") == (out.Payload == "") {
		f.add("text", errs.CodeRequired, "Нужно ровно одно: text или payload")
	}
	if out.Flag != "" && out.Text == "" {
		f.add("flag", errs.CodeRequired, "flag — только вместе с text")
	}
	if len([]rune(out.Text)) > 4000 || len(out.Payload) > 1024 {
		f.add("text", errs.CodeOutOfRange, "Слишком длинно")
	}
	return out, f.err()
}
