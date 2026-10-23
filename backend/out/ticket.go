package out

import (
	"time"

	"main/domain"
	"main/services"
)

// Ticket — обращение по контракту v2 (+ «Уточнения v2»).
type Ticket struct {
	ID            string  `json:"id"`
	Number        int64   `json:"number"`
	NewcomerID    string  `json:"newcomer_id"`
	NewcomerName  string  `json:"newcomer_name"`
	Site          string  `json:"site"`
	Day           int     `json:"day"`
	Category      string  `json:"category"`
	Priority      string  `json:"priority"`
	Status        string  `json:"status"`
	Route         string  `json:"route"`
	AssigneeName  *string `json:"assignee_name"`
	Anonymous     bool    `json:"anonymous"`
	Summary       string  `json:"summary"`
	Quote         *string `json:"quote"`
	SLADueAt      *string `json:"sla_due_at"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	NotifyManager bool    `json:"notify_manager"`
	Restricted    bool    `json:"restricted"`
}

// TicketCard — карточка и ответы действий: обращение и переписка (messages — всегда массив).
type TicketCard struct {
	Ticket
	Messages []TicketMessage `json:"messages"`
}

// TicketMessage — сообщение переписки. У закрытых обращений текст новичка скрыт: text=null,
// hidden=true (роли HR-партнёра и СБ — позже).
type TicketMessage struct {
	From     string  `json:"from"`
	Author   *string `json:"author"`
	Text     *string `json:"text"`
	At       string  `json:"at"`
	Internal bool    `json:"internal"`
	Hidden   bool    `json:"hidden"`
}

func ticketTime(t time.Time, site string) string { return zoned(t, site) }

// TicketFrom — обращение без переписки (список).
func TicketFrom(v services.TicketView) Ticket {
	t := v.Ticket
	o := Ticket{
		ID: t.ID.String(), Number: t.Number, NewcomerID: t.EmployeeID.String(), NewcomerName: v.NewcomerName,
		Site: v.Site, Day: t.Day, Category: t.Category, Priority: t.Priority, Status: t.Status, Route: t.Route,
		AssigneeName: t.AssigneeName, Anonymous: t.Anonymous, Summary: t.Summary, Quote: t.Quote,
		CreatedAt: ticketTime(t.CreatedAt, v.Site), UpdatedAt: ticketTime(t.UpdatedAt, v.Site),
		NotifyManager: t.NotifyManager && !t.Restricted, Restricted: t.Restricted,
	}
	if t.Restricted {
		o.Quote = nil
	}
	if t.SLADueAt != nil && t.Status != domain.TicketHumanActive {
		s := ticketTime(*t.SLADueAt, v.Site)
		o.SLADueAt = &s
	}
	return o
}

// TicketCardFrom — карточка с перепиской.
func TicketCardFrom(v services.TicketView) TicketCard {
	c := TicketCard{Ticket: TicketFrom(v), Messages: make([]TicketMessage, 0, len(v.Messages))}
	for _, m := range v.Messages {
		text := m.Text
		msg := TicketMessage{From: m.From, Author: m.Author, Text: &text, At: ticketTime(m.CreatedAt, v.Site), Internal: m.Internal}
		if v.Ticket.Restricted && m.From == domain.TicketMsgNewcomer {
			msg.Text, msg.Hidden = nil, true
		}
		c.Messages = append(c.Messages, msg)
	}
	return c
}

// TicketsFrom — список без переписки.
func TicketsFrom(list []services.TicketView) []Ticket {
	out := make([]Ticket, len(list))
	for i, v := range list {
		out[i] = TicketFrom(v)
	}
	return out
}
