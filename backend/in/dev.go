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
	// AudioPath — голосовое из файла на машине API (только dev): бот скачает его у подделки MAX
	// и распознает, как настоящее. Вместо пути можно прислать multipart с файлом в поле audio.
	AudioPath string `json:"audio_path"`
}

// SimulateMessageInput — проверенное тело: ровно одно из Text, Payload и голосового.
type SimulateMessageInput struct {
	NewcomerID uuid.UUID
	Text       string
	Payload    string
	Flag       string
	AudioPath  string
}

// MaxSimulatedAudioBytes — потолок голосового в dev-хуке (как у бота: maxVoiceBytes).
const MaxSimulatedAudioBytes = 20 << 20

// Validate — hasAudio: голосовое пришло файлом multipart (audio_path проверяется здесь).
func (s SimulateMessage) Validate(hasAudio bool) (SimulateMessageInput, error) {
	var f fields
	out := SimulateMessageInput{NewcomerID: f.uuid("newcomer_id", s.NewcomerID), Text: strings.TrimSpace(s.Text), Payload: strings.TrimSpace(s.Payload), Flag: strings.ToUpper(strings.TrimSpace(s.Flag)), AudioPath: strings.TrimSpace(s.AudioPath)}
	voice := hasAudio || out.AudioPath != ""
	n := 0
	for _, set := range []bool{out.Text != "", out.Payload != "", voice} {
		if set {
			n++
		}
	}
	if n != 1 || (hasAudio && out.AudioPath != "") {
		f.add("text", errs.CodeRequired, "Нужно ровно одно: text, payload или голосовое (audio_path / файл audio)")
	}
	if out.Flag != "" && out.Text == "" {
		f.add("flag", errs.CodeRequired, "flag — только вместе с text")
	}
	if len([]rune(out.Text)) > 4000 || len(out.Payload) > 1024 {
		f.add("text", errs.CodeOutOfRange, "Слишком длинно")
	}
	return out, f.err()
}
