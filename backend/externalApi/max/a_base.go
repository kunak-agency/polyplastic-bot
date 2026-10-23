// Package max — бот в мессенджере MAX. Наш интерфейс Client поверх официального SDK
// (github.com/max-messenger/max-bot-api-client-go/v2): бизнес-код видит только типы этого
// пакета, тесты идут на подделке Fake.
//
// Что обёртка добавляет к SDK (agency/system/a_core_stack.md, «Проверка клиента»):
//   - свой http.Client с корневым сертификатом Минцифры (transport.go);
//   - очередь отправки ≤ 2 сообщений/с на чат (pacer.go) и паузу на 429 (transport.go) —
//     SDK повторяет только attachment.not.ready;
//   - поле transcription у аудио-вложений: в SDK-модели его нет, берём из сырого JSON (audio.go).
package max

import (
	"context"
	"io"
)

// Client — то, что бизнес-коду нужно от MAX. Реализации: SDKClient (настоящий API) и Fake.
type Client interface {
	// Send — новое сообщение в чат (ChatID) или пользователю (UserID). Возвращает mid.
	// Отправки в один чат идут не чаще раза в SendInterval: вызов ждёт своей очереди.
	Send(ctx context.Context, m OutMessage) (string, error)
	// Edit — заменить текст и кнопки отправленного сообщения (живые опросы). m.ChatID нужен
	// только очереди отправки: правка идёт в тот же лимит сообщений чата.
	Edit(ctx context.Context, messageID string, m OutMessage) error
	// AnswerCallback — ответ на нажатие кнопки: всплывающее уведомление и/или правка сообщения.
	AnswerCallback(ctx context.Context, callbackID string, a Answer) error
	// Upload — загрузить файл, вернуть токен вложения для OutMessage.Attachments.
	Upload(ctx context.Context, kind UploadKind, r io.Reader, name string, size int64) (string, error)
	// GetUpdates — один long polling запрос. marker 0 — с начала очереди сервера. Возвращает
	// события и marker для следующего запроса.
	GetUpdates(ctx context.Context, marker int64) ([]Update, int64, error)
}

// Типы событий, которые бот обрабатывает. Остальные (dialog_muted, …) приходят с
// тем же Type и пока пропускаются.
const (
	UpdateMessageCreated  = "message_created"
	UpdateMessageCallback = "message_callback"
	UpdateBotStarted      = "bot_started"
	UpdateBotStopped      = "bot_stopped"
	UpdateBotAdded        = "bot_added"   // бота добавили в групповой чат (HR-чат, чат СБ)
	UpdateBotRemoved      = "bot_removed" // бота удалили из чата
)

// Update — событие от MAX в нашей форме. Имён и прочих данных профиля здесь нет намеренно:
// новичок опознаётся по UserID, а ПДн из мессенджера в наши логи и базу не тянем.
type Update struct {
	Type      string
	Timestamp int64 // мс Unix
	ChatID    int64
	UserID    int64 // кто написал, нажал кнопку или запустил бота
	FromBot   bool  // отправитель — бот: на такие не отвечаем, иначе два бота зациклятся

	MessageID string // mid: message_created, message_callback
	Text      string
	Audio     []Audio
	Images    []Image // фото от новичка (шаг kind=input, ТЗ r34)

	// Payload — bot_started: параметр deep link (invite-токен новичка); message_callback —
	// payload нажатой кнопки.
	Payload    string
	CallbackID string // message_callback: для AnswerCallback
}

// Audio — голосовое или аудиовложение.
type Audio struct {
	URL      string
	Token    string
	Duration int // секунды, если MAX прислал
	// Transcription — расшифровка от MAX. Пусто — поля не было или оно null; тогда нужен
	// свой ASR. [проверить]: присылает ли его MAX вообще (в схеме v0.0.30 поле есть).
	Transcription string
}

// Image — фото во входящем сообщении: ссылка и токен MAX (токен годится для пересылки).
type Image struct {
	URL   string
	Token string
}

// OutMessage — исходящее сообщение. Указывается ChatID или UserID.
type OutMessage struct {
	ChatID int64
	UserID int64
	Text   string
	Format string // "" | "markdown" | "html"
	// Buttons — inline-клавиатура по рядам.
	Buttons [][]Button
	// Attachments — токены загруженных файлов (Upload) с их видом.
	Attachments []AttachmentRef
	// Silent — без уведомления (notify=false), для напоминаний в тихие часы.
	Silent bool
}

// Button — кнопка inline-клавиатуры: callback (Payload), ссылка (URL) или запрос контакта
// (Contact — request_contact: MAX пришлёт контакт пользователя отдельным сообщением).
type Button struct {
	Text    string
	Payload string
	URL     string
	Contact bool
}

// AttachmentRef — ранее загруженный файл.
type AttachmentRef struct {
	Kind  UploadKind
	Token string
}

// Answer — ответ на нажатие кнопки. Notification — короткое всплывающее; Message — заменить
// сообщение с кнопками (nil — не менять).
type Answer struct {
	Notification string
	Message      *OutMessage
}

// UploadKind — вид загружаемого файла; значения совпадают с type в POST /uploads.
type UploadKind string

const (
	UploadImage UploadKind = "image"
	UploadVideo UploadKind = "video"
	UploadAudio UploadKind = "audio"
	UploadFile  UploadKind = "file"
)
