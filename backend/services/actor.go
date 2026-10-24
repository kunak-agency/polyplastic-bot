package services

import (
	"context"
	"slices"
	"strings"

	"main/domain"
)

// «Кто это сделал» при FEATURE_AUTH: исполнитель, автор заметки, подпись ответа, updated_by —
// вошедший пользователь из сессии, а не имя, которое прислал кабинет. Без входа (флаг выключен,
// бот, сид) — как раньше: имя из запроса или запасное.

// ActorName — имя вошедшего пользователя (full_name, пустое — логин); "" — входа нет.
func ActorName(ctx context.Context) string {
	u := ViewerFrom(ctx)
	if u == nil {
		return ""
	}
	if n := strings.TrimSpace(u.FullName); n != "" {
		return n
	}
	return u.LoginOr("")
}

// actorOr — имя вошедшего пользователя; без входа — fallback (имя из запроса).
func actorOr(ctx context.Context, fallback string) string {
	if n := ActorName(ctx); n != "" || ViewerFrom(ctx) != nil {
		return n
	}
	return fallback
}

// AssigneeRoles — кому можно назначить обращение.
var AssigneeRoles = []string{domain.RoleRecruiter, domain.RoleHRLead}

// canAssign — активный пользователь с ролью recruiter или hr_lead.
func canAssign(u *domain.User) bool {
	return u != nil && u.Active && slices.Contains(AssigneeRoles, u.Role)
}
