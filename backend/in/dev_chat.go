package in

import (
	"strings"

	"main/pkg/errs"
)

// DevChatPress — тело POST /api/dev/chat/{newcomer_id}/press (только development, бот на
// подделке): новичок нажал кнопку payload на сообщении бота message_id.
type DevChatPress struct {
	MessageID string `json:"message_id"`
	Payload   string `json:"payload"`
}

func (p DevChatPress) Validate() (DevChatPress, error) {
	var f fields
	out := DevChatPress{MessageID: strings.TrimSpace(p.MessageID), Payload: strings.TrimSpace(p.Payload)}
	if out.MessageID == "" {
		f.add("message_id", errs.CodeRequired, "Нужен message_id")
	}
	if out.Payload == "" {
		f.add("payload", errs.CodeRequired, "Нужен payload")
	}
	if len(out.MessageID) > 128 || len(out.Payload) > 1024 {
		f.add("payload", errs.CodeOutOfRange, "Слишком длинно")
	}
	return out, f.err()
}
