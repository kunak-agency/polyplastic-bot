package services

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Пользователи кабинета и команда рекрутеров (FEATURE_AUTH, п. 9–10 плана итерации 24.10):
// создать и деактивировать рекрутера или руководителя, закрепить новичков, «Команда» hr_lead.

// Коды отказов (agency/system/api_contract_auth.md).
const (
	CodeLoginTaken     = "LOGIN_TAKEN"
	CodeSelfDeactivate = "SELF_DEACTIVATE"
)

// TeamResponseDays — окно среднего времени первого ответа в «Команде».
const TeamResponseDays = 30

const msgUserNotFound = "Пользователь не найден"

// UserService — пользователи кабинета.
type UserService struct {
	repos *repositories.Repos
	now   func() time.Time
	tax   *taxonomy.Taxonomy
}

// UserInput — POST /api/users.
type UserInput struct {
	Login       string
	FullName    string
	Password    string
	Role        string
	TZ          *string
	ManagerName *string
	Active      bool
}

// UserPatch — PATCH /api/users/{id}: nil — поле не меняется. ManagerName "" — снять. Login —
// «выдать вход» человеку из справочника (вместе с Password, если пароля ещё нет).
type UserPatch struct {
	Active      *bool
	Role        *string
	FullName    *string
	Password    *string
	ManagerName *string
	Login       *string
}

func validRole(r string) bool {
	for _, x := range domain.Roles {
		if r == x {
			return true
		}
	}
	return false
}

func fieldErr(field, code, msg string) error {
	return errs.NewValidationError(errs.MsgInvalidFields).WithDetails(errs.FieldError{Field: field, Code: code, Message: msg})
}

func checkPasswordLen(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return fieldErr("password", errs.CodeOutOfRange, "Пароль не короче 8 символов")
	}
	return nil
}

func emptyToNil(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// Create — новый пользователь с паролем (bcrypt). Логин занят — 409 LOGIN_TAKEN.
func (s *UserService) Create(ctx context.Context, in UserInput) (*domain.User, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	login := NormalizeLogin(in.Login)
	switch {
	case login == "" || utf8.RuneCountInString(login) > 100:
		return nil, fieldErr("login", errs.CodeRequired, "Логин: от 1 до 100 символов")
	case strings.TrimSpace(in.FullName) == "":
		return nil, fieldErr("full_name", errs.CodeRequired, "Обязательное поле")
	case !validRole(in.Role):
		return nil, fieldErr("role", errs.CodeOutOfRange, "Роль: hr_lead, recruiter или manager")
	case in.Role == domain.RoleMentor:
		return nil, fieldErr("role", errs.CodeOutOfRange, msgMentorNoLogin)
	}
	if err := checkPasswordLen(in.Password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{Login: &login, PasswordHash: hash, FullName: strings.TrimSpace(in.FullName), Role: in.Role,
		Active: in.Active, TZ: emptyToNil(in.TZ), ManagerName: emptyToNil(in.ManagerName)}
	if err := s.repos.Users.Create(ctx, u); err != nil {
		if repositories.IsUniqueViolation(err) {
			return nil, errs.NewConflictError("Логин уже занят").WithCode(CodeLoginTaken)
		}
		return nil, err
	}
	return u, nil
}

// Patch — изменить пользователя. Деактивация и смена пароля удаляют его сессии; себя
// деактивировать нельзя (409 SELF_DEACTIVATE).
func (s *UserService) Patch(ctx context.Context, id uuid.UUID, p UserPatch) (*domain.User, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	u, err := s.repos.Users.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, errs.NewNotFoundError(msgUserNotFound)
	}
	fields := map[string]any{}
	dropSessions := false
	if p.Active != nil {
		if !*p.Active {
			if v := ViewerFrom(ctx); v != nil && v.ID == id {
				return nil, errs.NewConflictError("Себя деактивировать нельзя").WithCode(CodeSelfDeactivate)
			}
			dropSessions = true
		}
		fields["active"] = *p.Active
	}
	if p.Role != nil {
		if !validRole(*p.Role) {
			return nil, fieldErr("role", errs.CodeOutOfRange, "Роль: hr_lead, recruiter или manager")
		}
		fields["role"] = *p.Role
	}
	renamed := ""
	if p.FullName != nil {
		name := strings.TrimSpace(*p.FullName)
		if name == "" {
			return nil, fieldErr("full_name", errs.CodeRequired, "Обязательное поле")
		}
		fields["full_name"] = name
		if name != u.FullName {
			renamed = name
		}
	}
	if p.Login != nil {
		login := NormalizeLogin(*p.Login)
		role := u.Role
		if p.Role != nil {
			role = *p.Role
		}
		switch {
		case role == domain.RoleMentor:
			return nil, fieldErr("login", errs.CodeOutOfRange, msgMentorNoLogin)
		case login == "" || utf8.RuneCountInString(login) > 100:
			return nil, fieldErr("login", errs.CodeRequired, "Логин: от 1 до 100 символов")
		case u.PasswordHash == "" && p.Password == nil:
			return nil, fieldErr("password", errs.CodeRequired, "Чтобы выдать вход, задайте пароль")
		}
		fields["login"] = login
	}
	if p.ManagerName != nil {
		fields["manager_name"] = emptyToNil(p.ManagerName)
	}
	if p.Password != nil {
		if err := checkPasswordLen(*p.Password); err != nil {
			return nil, err
		}
		hash, err := HashPassword(*p.Password)
		if err != nil {
			return nil, err
		}
		fields["password_hash"] = hash
		dropSessions = true
	}
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		if len(fields) > 0 {
			if err := tx.Users.Patch(ctx, id, fields); err != nil {
				if repositories.IsUniqueViolation(err) {
					return errs.NewConflictError("Логин уже занят").WithCode(CodeLoginTaken)
				}
				return err
			}
		}
		if renamed != "" {
			// Копия ФИО в карточках (тексты бота, шаблоны) следует за справочником.
			if err := tx.Employees.SyncPeopleNames(ctx, id, renamed); err != nil {
				return err
			}
		}
		if dropSessions {
			return tx.Sessions.DeleteByUser(ctx, id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repos.Users.ByID(ctx, id)
}

// List — пользователи по фильтру.
func (s *UserService) List(ctx context.Context, f repositories.UserFilter) ([]domain.User, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	return s.repos.Users.ListBy(ctx, f)
}

// Reassign — закрепить новичков за рекрутером (nil — снять). Рекрутер — активный пользователь
// с ролью recruiter или hr_lead, иначе 422.
func (s *UserService) Reassign(ctx context.Context, ids []uuid.UUID, recruiterID *uuid.UUID) (int64, error) {
	if !s.repos.Enabled() {
		return 0, repositories.ErrNoDatabase
	}
	if recruiterID != nil {
		u, err := s.repos.Users.ByID(ctx, *recruiterID)
		if err != nil {
			return 0, err
		}
		if u == nil || !u.Active || (u.Role != domain.RoleRecruiter && u.Role != domain.RoleHRLead) {
			return 0, fieldErr("recruiter_id", errs.CodeOutOfRange, "Нужен активный рекрутер")
		}
	}
	return s.repos.Employees.SetRecruiter(ctx, ids, recruiterID)
}

// TeamMember — строка «Команды»: нагрузка, без времени ответа по человеку (без рейтингов).
type TeamMember struct {
	User              domain.User
	Newcomers         int
	TicketsOpen       int
	TicketsSLAOverdue int
}

// Team — команда рекрутеров и её медиана первого ответа.
type Team struct {
	Members    []TeamMember
	Unassigned int // новичков без рекрутера
	// MedianFirstResponseMin — медиана первого ответа по команде за TeamResponseDays; nil — мало
	// данных (меньше AnonymityMinGroup ответов). MedianN — по скольким обращениям.
	MedianFirstResponseMin *int
	MedianN                int
}

// Team — «Команда» hr_lead: рекрутеры (и hr_lead, у кого есть новички), сначала с просрочками.
func (s *UserService) Team(ctx context.Context) (*Team, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	rows, err := s.repos.Team(ctx, now)
	if err != nil {
		return nil, err
	}
	users, err := s.repos.Users.List(ctx)
	if err != nil {
		return nil, err
	}
	t := &Team{Members: []TeamMember{}}
	byUser := map[uuid.UUID]repositories.TeamRow{}
	for _, r := range rows {
		if r.RecruiterID == nil {
			t.Unassigned = r.Newcomers
			continue
		}
		byUser[*r.RecruiterID] = r
	}
	for _, u := range users {
		r, has := byUser[u.ID]
		if u.Role != domain.RoleRecruiter && !(u.Role == domain.RoleHRLead && has) {
			continue
		}
		t.Members = append(t.Members, TeamMember{User: u, Newcomers: r.Newcomers, TicketsOpen: r.TicketsOpen, TicketsSLAOverdue: r.TicketsSLAOverdue})
	}
	sort.SliceStable(t.Members, func(i, j int) bool { return t.Members[i].TicketsSLAOverdue > t.Members[j].TicketsSLAOverdue })
	med, n, err := s.repos.TeamMedianFirstResponse(ctx, now.AddDate(0, 0, -TeamResponseDays))
	if err != nil {
		return nil, err
	}
	t.MedianN = n
	if med != nil && n >= AnonymityMinGroup {
		v := int(math.Round(*med))
		t.MedianFirstResponseMin = &v
	}
	return t, nil
}

// DemoUser — демо-аккаунт сида (cmd/seed -users).
type DemoUser struct {
	Login       string
	FullName    string
	Role        string
	ManagerName *string
	Password    string
}

// EnsureUser — создать пользователя, если логина нет; есть — обновить имя, роль, руководителя и
// пароль (если задан) и включить. created — создан заново.
func (s *UserService) EnsureUser(ctx context.Context, d DemoUser) (*domain.User, bool, error) {
	if !s.repos.Enabled() {
		return nil, false, repositories.ErrNoDatabase
	}
	u, err := s.repos.Users.ByLogin(ctx, NormalizeLogin(d.Login))
	if err != nil {
		return nil, false, err
	}
	if u == nil {
		u, err = s.Create(ctx, UserInput{Login: d.Login, FullName: d.FullName, Password: d.Password, Role: d.Role, ManagerName: d.ManagerName, Active: true})
		return u, err == nil, err
	}
	active := true
	p := UserPatch{Active: &active, Role: &d.Role, FullName: &d.FullName}
	mn := ""
	if d.ManagerName != nil {
		mn = *d.ManagerName
	}
	p.ManagerName = &mn
	if d.Password != "" {
		p.Password = &d.Password
	}
	u, err = s.Patch(ctx, u.ID, p)
	return u, false, err
}

// LinkRecruitersByName — новичкам без RecruiterID проставить рекрутера по совпадению
// RecruiterName с full_name пользователя-рекрутера.
func (s *UserService) LinkRecruitersByName(ctx context.Context) (int64, error) {
	if !s.repos.Enabled() {
		return 0, repositories.ErrNoDatabase
	}
	return s.repos.Employees.LinkRecruitersByName(ctx)
}

// NewcomerRecruiter — рекрутер новичка (nil — без рекрутера) и найден ли новичок.
func (s *UserService) NewcomerRecruiter(ctx context.Context, id uuid.UUID) (*uuid.UUID, bool, error) {
	if !s.repos.Enabled() {
		return nil, false, repositories.ErrNoDatabase
	}
	return s.repos.Employees.RecruiterOf(ctx, id)
}

// TicketNewcomer — новичок обращения; uuid.Nil — обращения нет.
func (s *UserService) TicketNewcomer(ctx context.Context, ticketID uuid.UUID) (uuid.UUID, error) {
	if !s.repos.Enabled() {
		return uuid.Nil, repositories.ErrNoDatabase
	}
	return s.repos.Tickets.TicketEmployee(ctx, ticketID)
}

// RecruiterNames — full_name пользователей по id (recruiter_name в карточках новичков).
func (s *UserService) RecruiterNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if !s.repos.Enabled() || len(ids) == 0 {
		return out, nil
	}
	m, err := s.repos.Users.ByIDs(ctx, ids)
	for id, u := range m {
		out[id] = u.FullName
	}
	return out, err
}

// NewcomerFiles — личные файлы новичка (без содержимого).
func (s *UserService) NewcomerFiles(ctx context.Context, id uuid.UUID) ([]repositories.NewcomerFileInfo, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	e, err := s.repos.Employees.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	return s.repos.Employees.Files(ctx, id)
}

// ManagerTask — «Встретился» (done) / «Запланировал» (planned) из кабинета: то же, что кнопки
// mg:met / mg:plan под push в MAX (manager_bot.go). Роль manager — только задачи своих
// подчинённых (ManagerOwns: manager_id, для старых записей — имя), чужая — 404.
func (s *UserService) ManagerTask(ctx context.Context, nudgeID uuid.UUID, status string) error {
	if !s.repos.Enabled() {
		return repositories.ErrNoDatabase
	}
	var st string
	switch status {
	case "done", domain.NudgeMet:
		st = domain.NudgeMet
	case domain.NudgePlanned:
		st = domain.NudgePlanned
	default:
		return fieldErr("status", errs.CodeOutOfRange, "Статус: done или planned")
	}
	repo := s.repos.Manager()
	n, err := repo.Nudge(ctx, nudgeID)
	if err != nil {
		return err
	}
	notFound := errs.NewNotFoundError("Задача не найдена")
	if n == nil {
		return notFound
	}
	if v := ViewerFrom(ctx); v != nil && v.Role == domain.RoleManager {
		e, err := s.repos.Employees.ByID(ctx, n.EmployeeID)
		if err != nil {
			return err
		}
		if e == nil || !ManagerOwns(v, e) {
			return notFound
		}
	}
	now := s.now()
	n.Status, n.AnsweredAt = st, &now
	return repo.SaveNudge(ctx, n)
}
