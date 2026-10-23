package out

import (
	"time"

	"main/externalApi/max"
)

// DevChat — ответ GET /api/dev/chat/{newcomer_id}: лента диалога с новичком на подделке MAX.
type DevChat struct {
	Messages []DevChatMessage `json:"messages"`
	Typing   bool             `json:"typing"`
}

type DevChatMessage struct {
	ID          string              `json:"id"`
	From        string              `json:"from"` // bot | newcomer | toast
	Kind        string              `json:"kind,omitempty"`
	Text        string              `json:"text"`
	Format      string              `json:"format"`
	Buttons     [][]DevChatButton   `json:"buttons"`
	Attachments []DevChatAttachment `json:"attachments"`
	Notify      bool                `json:"notify"`
	Payload     string              `json:"payload,omitempty"`
	ReplyTo     string              `json:"reply_to,omitempty"`
	Edited      bool                `json:"edited"`
	At          time.Time           `json:"at"`
	EditedAt    *time.Time          `json:"edited_at"`
}

type DevChatButton struct {
	Text    string `json:"text"`
	Type    string `json:"type"` // callback | link | request_contact
	Payload string `json:"payload,omitempty"`
	URL     string `json:"url,omitempty"`
}

type DevChatAttachment struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

func DevChatFrom(entries []max.JournalEntry, typing bool) DevChat {
	msgs := make([]DevChatMessage, 0, len(entries))
	for _, e := range entries {
		rows := make([][]DevChatButton, 0, len(e.Buttons))
		for _, r := range e.Buttons {
			row := make([]DevChatButton, 0, len(r))
			for _, b := range r {
				row = append(row, DevChatButton{Text: b.Text, Type: b.Type, Payload: b.Payload, URL: b.URL})
			}
			rows = append(rows, row)
		}
		atts := make([]DevChatAttachment, 0, len(e.Attachments))
		for _, a := range e.Attachments {
			atts = append(atts, DevChatAttachment{Type: a.Type, URL: a.URL})
		}
		msgs = append(msgs, DevChatMessage{
			ID: e.ID, From: e.From, Kind: e.Kind, Text: e.Text, Format: e.Format, Buttons: rows, Attachments: atts,
			Notify: e.Notify, Payload: e.Payload, ReplyTo: e.ReplyTo, Edited: e.Edited, At: e.At, EditedAt: e.EditedAt,
		})
	}
	return DevChat{Messages: msgs, Typing: typing}
}
