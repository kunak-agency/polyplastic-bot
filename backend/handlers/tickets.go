package handlers

import (
	"strings"

	"main/in"
	"main/out"
	"main/services"

	"github.com/gofiber/fiber/v2"
)

// ListTickets — GET /api/tickets?status=&site=&route=&mine=&newcomer_id=.
func (h *Handler) ListTickets(c *fiber.Ctx) error {
	var q in.TicketQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	f, err := q.Validate()
	if err != nil {
		return err
	}
	f.RecruiterID = recruiterScope(c) // FEATURE_AUTH: рекрутер — обращения своих новичков
	// FEATURE_AUTH: mine — «мои» по assignee_id вошедшего, имя из запроса не используется.
	if v := Viewer(c); v != nil && mineOn(q.Mine) {
		f.Assignee, f.AssigneeID = "", &v.ID
	}
	list, err := h.services.Tickets.List(c.UserContext(), f)
	if err != nil {
		return err
	}
	return c.JSON(out.NewList(out.TicketsFrom(list)))
}

// GetTicket — GET /api/tickets/{id}: обращение с перепиской.
func (h *Handler) GetTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.Get(c.UserContext(), id))
}

// PatchTicket — PATCH /api/tickets/{id} {"notify_manager"} («Уточнения v2»).
func (h *Handler) PatchTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TicketPatch
	if err := bindBody(c, &body); err != nil {
		return err
	}
	notify, err := body.Validate()
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.SetNotifyManager(c.UserContext(), id, notify))
}

// AssignTicket — POST /api/tickets/{id}/assign {"assignee_id"?,"assignee_name"?,"comment"?}. При
// FEATURE_AUTH пустое тело — на себя («Взять в работу»).
func (h *Handler) AssignTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TicketAssign
	if err := bindBody(c, &body); err != nil {
		return err
	}
	v, err := body.Validate()
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.Assign(c.UserContext(), id,
		services.AssignInput{AssigneeID: v.AssigneeID, Name: v.Name, Comment: v.Comment}))
}

// TicketAssignees — GET /api/tickets/assignees: кому можно назначить обращение (активные recruiter
// и hr_lead). Узкий список для выбора исполнителя: /api/users рекрутеру закрыт.
func (h *Handler) TicketAssignees(c *fiber.Ctx) error {
	list, err := h.services.Tickets.Assignees(c.UserContext())
	if err != nil {
		return err
	}
	items := make([]out.Assignee, 0, len(list))
	for _, u := range list {
		items = append(items, out.Assignee{ID: u.ID.String(), FullName: u.FullName, Role: u.Role})
	}
	return c.JSON(out.NewList(items))
}

// ReplyTicket — POST /api/tickets/{id}/reply {"text","resolve"}: ответ новичку через бота.
func (h *Handler) ReplyTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TicketReply
	if err := bindBody(c, &body); err != nil {
		return err
	}
	text, resolve, err := body.Validate()
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.Reply(c.UserContext(), id, text, resolve))
}

// NoteTicket — POST /api/tickets/{id}/note {"text"}: внутренняя заметка.
func (h *Handler) NoteTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TicketNote
	if err := bindBody(c, &body); err != nil {
		return err
	}
	text, err := body.Validate()
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.Note(c.UserContext(), id, text))
}

// ResolveTicket — POST /api/tickets/{id}/resolve {"done_note"?}: тело необязательно.
func (h *Handler) ResolveTicket(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	var body in.TicketResolve
	if err := bindBody(c, &body); err != nil {
		return err
	}
	note, err := body.Validate()
	if err != nil {
		return err
	}
	return card(c)(h.services.Tickets.Resolve(c.UserContext(), id, note))
}

// card — ответ действием: карточка обращения с перепиской.
func card(c *fiber.Ctx) func(*services.TicketView, error) error {
	return func(v *services.TicketView, err error) error {
		if err != nil {
			return err
		}
		return c.JSON(out.TicketCardFrom(*v))
	}
}

// mineOn — mine задан: «1», «true» или имя (старый кабинет).
func mineOn(m string) bool {
	switch m = strings.ToLower(strings.TrimSpace(m)); m {
	case "", "0", "false":
		return false
	}
	return true
}
