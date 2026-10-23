package handlers

import (
	"strings"

	"main/domain"
	"main/externalApi/max"
	"main/in"
	"main/out"
	"main/pkg/errs"

	"github.com/gofiber/fiber/v2"
)

// ManagerInvite — POST /api/newcomers/{id}/manager-invite: ссылка руководителя mgr_<token>.
func (h *Handler) ManagerInvite(c *fiber.Ctx) error {
	id, err := pathID(c)
	if err != nil {
		return err
	}
	r, err := h.services.Manager().Invite(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(out.ManagerInvite{ManagerName: r.ManagerName, Link: r.Link,
		ExpiresAt: r.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"), Connected: r.Connected})
}

// Managers — GET /api/manager/managers: руководители из карточек.
func (h *Handler) Managers(c *fiber.Ctx) error {
	list, err := h.services.Manager().Managers(c.UserContext())
	if err != nil {
		return err
	}
	items := []out.ManagerSummary{}
	for _, m := range list {
		items = append(items, out.ManagerSummary{ManagerName: m.ManagerName, Newcomers: m.Newcomers, Connected: m.Connected > 0})
	}
	return c.JSON(fiber.Map{"items": items, "total": len(items)})
}

// ManagerNewcomers — GET /api/manager/newcomers?name=: «Мои новички».
func (h *Handler) ManagerNewcomers(c *fiber.Ctx) error {
	var q in.ManagerQuery
	if err := bindQuery(c, &q); err != nil {
		return err
	}
	name, err := q.Validate()
	if err != nil {
		return err
	}
	v, err := h.services.Manager().View(c.UserContext(), name)
	if err != nil {
		return err
	}
	return c.JSON(out.ManagerViewFrom(v))
}

// DevManagerPush — POST /api/dev/manager/push: такт правил сейчас, без рабочих часов.
func (h *Handler) DevManagerPush(c *fiber.Ctx) error {
	list, err := h.services.Manager().Tick(c.UserContext(), true)
	if err != nil {
		return err
	}
	items := []out.ManagerPush{}
	for _, n := range list {
		items = append(items, out.ManagerPush{NudgeID: n.NudgeID, NewcomerID: n.NewcomerID, Manager: n.Manager, Kind: n.Kind, Text: n.Text, Delivered: n.Delivered})
	}
	return c.JSON(fiber.Map{"items": items, "total": len(items)})
}

// devManagerBody — POST /api/dev/manager/start и /press: играет за руководителя в MAX.
type devManagerBody struct {
	UserID  int64  `json:"user_id"`
	Link    string `json:"link"`    // start: ссылка руководителя (или payload mgr_<token>)
	Payload string `json:"payload"` // press: payload кнопки push
}

// DevManagerStart — POST /api/dev/manager/start: bot_started по ссылке руководителя от
// синтетического user_id; ответ — лента этого «руководителя» на подделке MAX.
func (h *Handler) DevManagerStart(c *fiber.Ctx) error {
	var b devManagerBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	i := strings.Index(b.Link, domain.ManagerPayloadPrefix)
	if b.UserID == 0 || i < 0 {
		return errs.NewValidationError("Нужны user_id и ссылка руководителя (mgr_<token>)")
	}
	payload := b.Link[i:]
	if j := strings.IndexAny(payload, "&/?#"); j >= 0 {
		payload = payload[:j]
	}
	if h.services.Bot == nil {
		return errs.NewConflictError("Бот выключен")
	}
	h.devIncoming(b.UserID, b.UserID, max.JournalKindStart, "")
	h.services.Bot.Handle(c.UserContext(), max.Update{Type: max.UpdateBotStarted, UserID: b.UserID, ChatID: b.UserID, Payload: payload})
	return h.devManagerChat(c, b.UserID)
}

// DevManagerPress — POST /api/dev/manager/press: нажатие «Встретился» / «Запланирую».
func (h *Handler) DevManagerPress(c *fiber.Ctx) error {
	var b devManagerBody
	if err := bindBody(c, &b); err != nil {
		return err
	}
	if b.UserID == 0 || b.Payload == "" || h.services.Bot == nil {
		return errs.NewValidationError("Нужны user_id и payload")
	}
	h.services.Bot.Handle(c.UserContext(), h.devCallback(b.UserID, b.Payload))
	return h.devManagerChat(c, b.UserID)
}

// DevManagerChat — GET /api/dev/manager/chat?user_id=: лента руководителя на подделке.
func (h *Handler) DevManagerChat(c *fiber.Ctx) error {
	return h.devManagerChat(c, int64(c.QueryInt("user_id")))
}

func (h *Handler) devManagerChat(c *fiber.Ctx, user int64) error {
	fake := h.services.FakeMax()
	if fake == nil {
		return fiber.ErrNotFound
	}
	msgs, _ := fake.Chat(user, user)
	return c.JSON(fiber.Map{"messages": msgs})
}
