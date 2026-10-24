package max

import (
	"context"
	"io"
	"sync"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
)

// callTimeout — срок одного вызова Send/Edit/AnswerCallback вместе с повторами SDK
// (attachment.not.ready: паузы 1 и 2 с) и нашими на 429.
const callTimeout = 30 * time.Second

// Config — настройки клиента MAX.
type Config struct {
	Token  string
	CAFile string // корневой сертификат Минцифры; пусто — системный пул
	// BaseURL — другой хост API (тесты, двойник). Пусто — platform-api2.max.ru из SDK.
	BaseURL      string
	PollTimeout  time.Duration // timeout long polling; 0 — 30 с, как в SDK
	SendInterval time.Duration // пауза между сообщениями в чат; 0 — DefaultSendInterval
}

// SDKClient — Client поверх официального SDK.
type SDKClient struct {
	api   *maxbot.Api
	doer  *doer
	pacer *pacer

	pollMu sync.Mutex // один GET /updates за раз: копия тела в doer общая
}

var _ Client = (*SDKClient)(nil)

// New — клиент MAX. Ошибка — только от сертификата: без токена клиент не собирают (main).
func New(cfg Config) (*SDKClient, error) {
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 30 * time.Second
	}
	hc, err := newHTTPClient(cfg.CAFile, cfg.PollTimeout)
	if err != nil {
		return nil, err
	}
	d := &doer{client: hc, pause: default429Pause, sleep: sleepCtx}
	opts := []maxbot.Opt{maxbot.WithHTTPClient(d), maxbot.WithPollingTimeout(cfg.PollTimeout)}
	if cfg.BaseURL != "" {
		opts = append(opts, maxbot.WithBaseURL(cfg.BaseURL))
	}
	api, err := maxbot.NewApi(cfg.Token, opts...)
	if err != nil {
		return nil, err
	}
	return &SDKClient{api: api, doer: d, pacer: newPacer(cfg.SendInterval)}, nil
}

func (c *SDKClient) Send(ctx context.Context, m OutMessage) (string, error) {
	if m.ChatID == 0 && m.UserID == 0 {
		return "", errNoRecipient
	}
	msg := buildMessage(m)
	var mid string
	err := c.pacer.Do(ctx, chatKey(m.ChatID, m.UserID), func() error {
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		res, err := c.api.Messages.Send(ctx, msg)
		mid = res.Message.Body.Mid
		return err
	})
	return mid, err
}

func (c *SDKClient) Edit(ctx context.Context, messageID string, m OutMessage) error {
	m.ReplyTo = "" // правка не меняет, на что сообщение отвечает
	body := buildMessage(m).MessageBody()
	return c.pacer.Do(ctx, chatKey(m.ChatID, m.UserID), func() error {
		ctx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		_, err := c.api.Messages.EditMessage(ctx, messageID, body)
		return err
	})
}

// AnswerCallback не стоит в очереди чата: ответ на нажатие MAX ждёт сразу, и это не новое
// сообщение в ленте.
func (c *SDKClient) AnswerCallback(ctx context.Context, callbackID string, a Answer) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var ans model.CallbackAnswer
	if a.Notification != "" {
		n := a.Notification
		ans.Notification = &n
	}
	if a.Message != nil {
		em := *a.Message
		em.ReplyTo = ""
		body := buildMessage(em).MessageBody()
		ans.Message = &body
	}
	_, err := c.api.Messages.AnswerOnCallback(ctx, callbackID, ans)
	return err
}

// Upload — срок задаёт ctx вызывающего: видео грузится дольше любого разумного общего срока.
func (c *SDKClient) Upload(ctx context.Context, kind UploadKind, r io.Reader, name string, size int64) (string, error) {
	return c.api.Upload.Upload(ctx, model.UploadType(kind), r, name, size)
}

func (c *SDKClient) GetUpdates(ctx context.Context, marker int64) ([]Update, int64, error) {
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	c.doer.takeUpdates()
	ups, next, err := c.api.Subscriptions.GetUpdates(ctx, marker)
	if err != nil {
		return nil, marker, err
	}
	body := c.doer.takeUpdates()
	audio := audioByMid(body)
	images := imagesByMid(body)
	out := make([]Update, 0, len(ups))
	for _, u := range ups {
		x := fromSDK(u, audio)
		if x.MessageID != "" {
			x.Images = images[x.MessageID]
		}
		out = append(out, x)
	}
	return out, next, nil
}

// fromSDK — событие SDK в нашу форму. Для message_callback SDK кладёт в UserID получателя
// сообщения с кнопками; нажал же его Callback.User — берём его, когда он есть.
func fromSDK(u model.Update, audio map[string][]Audio) Update {
	msg := u.GetMessage()
	out := Update{
		Type:      string(u.UpdateType),
		Timestamp: u.Timestamp,
		ChatID:    u.ChatID,
		UserID:    u.UserID,
		FromBot:   msg.Sender.IsBot,
		MessageID: u.MessageID,
		Text:      msg.Body.Text,
		Payload:   u.Payload,
	}
	if cb := u.GetCallback(); cb.CallbackID != "" {
		out.CallbackID = cb.CallbackID
		if cb.User.UserID != 0 {
			out.UserID = cb.User.UserID
		}
	}
	if out.MessageID != "" {
		out.Audio = audio[out.MessageID]
	}
	return out
}

// buildMessage — наше сообщение в построитель SDK. Кнопки — через AddCallBack/AddLink:
// устаревший AddCallback молча теряет intent.
func buildMessage(m OutMessage) *maxbot.Message {
	msg := maxbot.NewMessage().SetText(m.Text)
	if m.ChatID != 0 {
		msg.SetChat(m.ChatID)
	} else if m.UserID != 0 {
		msg.SetUser(m.UserID)
	}
	if m.Format != "" {
		msg.SetFormat(model.TextFormat(m.Format))
	}
	if m.Silent {
		msg.WithoutNotify()
	}
	if m.ReplyTo != "" {
		msg.SetReply(m.Text, m.ReplyTo) // link {type: reply, mid}
	}
	for _, a := range m.Attachments {
		msg.AddAttachByToken(a.Token, model.AttachmentType(a.Kind))
	}
	if len(m.Buttons) > 0 {
		kb := model.NewKeyboard()
		for _, row := range m.Buttons {
			r := kb.AddRow()
			for _, b := range row {
				switch {
				case b.Contact:
					r.AddContact(b.Text)
				case b.URL != "":
					r.AddLink(b.Text, b.URL)
				default:
					r.AddCallBack(b.Text, b.Payload)
				}
			}
		}
		msg.AddKeyboard(kb)
	}
	return msg
}
