package in

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"
	"main/pkg/taxonomy"
	"main/repositories"
)

// maxTicketText — потолок ответа HR и заметки: сообщение MAX ≤ 4000 символов, часть занимает
// подпись.
const maxTicketText = 3500

// TicketQuery — GET /api/tickets?status=&site=&route=&mine=&newcomer_id=. status — один или
// несколько через запятую. mine — имя исполнителя (авторизации в v2 нет); «true», «1» и пусто —
// без фильтра.
type TicketQuery struct {
	Status     string `query:"status"`
	Site       string `query:"site"`
	Route      string `query:"route"`
	Mine       string `query:"mine"`
	NewcomerID string `query:"newcomer_id"`
}

func (q TicketQuery) Validate() (repositories.TicketFilter, error) {
	var f fields
	var out repositories.TicketFilter
	for _, st := range strings.Split(q.Status, ",") {
		if st = strings.TrimSpace(st); st == "" {
			continue
		}
		if !slices.Contains(domain.TicketStatuses, st) {
			f.add("status", errs.CodeOutOfRange, "Статус: "+strings.Join(domain.TicketStatuses, ", "))
			break
		}
		out.Statuses = append(out.Statuses, st)
	}
	if out.Site = strings.TrimSpace(q.Site); out.Site != "" {
		f.site("site", out.Site)
	}
	if out.Route = strings.TrimSpace(q.Route); out.Route != "" && !slices.Contains(taxonomy.Routes, out.Route) {
		f.add("route", errs.CodeOutOfRange, "Маршрут: "+strings.Join(taxonomy.Routes, ", "))
	}
	switch m := strings.TrimSpace(q.Mine); strings.ToLower(m) {
	case "", "true", "1", "false", "0":
	default:
		out.Assignee = f.text("mine", m, false)
	}
	if id := strings.TrimSpace(q.NewcomerID); id != "" {
		u := f.uuid("newcomer_id", id)
		out.Newcomer = &u
	}
	return out, f.err()
}

// TicketAssign — POST /api/tickets/{id}/assign.
type TicketAssign struct {
	AssigneeName string `json:"assignee_name"`
	Comment      string `json:"comment"`
}

func (b TicketAssign) Validate() (string, string, error) {
	var f fields
	name := f.text("assignee_name", b.AssigneeName, true)
	comment := strings.TrimSpace(b.Comment)
	if utf8.RuneCountInString(comment) > maxTicketText {
		f.add("comment", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxTicketText))
	}
	return name, comment, f.err()
}

// TicketReply — POST /api/tickets/{id}/reply.
type TicketReply struct {
	Text    string `json:"text"`
	Resolve bool   `json:"resolve"`
}

func (b TicketReply) Validate() (string, bool, error) {
	var f fields
	text := ticketText(&f, b.Text)
	return text, b.Resolve, f.err()
}

// TicketNote — POST /api/tickets/{id}/note.
type TicketNote struct {
	Text string `json:"text"`
}

func (b TicketNote) Validate() (string, error) {
	var f fields
	text := ticketText(&f, b.Text)
	return text, f.err()
}

// TicketResolve — POST /api/tickets/{id}/resolve {"done_note"?}: «что сделано», одна строка
// (переводы строк — пробелы), необязательно.
type TicketResolve struct {
	DoneNote string `json:"done_note"`
}

// MaxDoneNote — потолок строки «что сделано».
const MaxDoneNote = 300

func (b TicketResolve) Validate() (string, error) {
	var f fields
	note := strings.Join(strings.Fields(b.DoneNote), " ")
	if utf8.RuneCountInString(note) > MaxDoneNote {
		f.add("done_note", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", MaxDoneNote))
	}
	return note, f.err()
}

// TicketPatch — PATCH /api/tickets/{id} {"notify_manager"}.
type TicketPatch struct {
	NotifyManager *bool `json:"notify_manager"`
}

func (b TicketPatch) Validate() (bool, error) {
	if b.NotifyManager == nil {
		return false, errs.NewValidationError(errs.MsgInvalidFields).
			WithDetails(errs.FieldError{Field: "notify_manager", Code: errs.CodeRequired, Message: "Обязательное поле"})
	}
	return *b.NotifyManager, nil
}

func ticketText(f *fields, v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		f.add("text", errs.CodeRequired, "Обязательное поле")
	case utf8.RuneCountInString(v) > maxTicketText:
		f.add("text", errs.CodeOutOfRange, fmt.Sprintf("Не длиннее %d символов", maxTicketText))
	}
	return v
}
