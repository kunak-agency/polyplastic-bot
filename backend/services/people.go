package services

import (
	"context"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Справочник людей (api_contract_auth.md, «Люди»): руководители и наставники — пользователи
// с ролью manager / mentor, по умолчанию без входа. Карточка сотрудника ссылается на них
// manager_id / mentor_id; строки manager_name / mentor_name — копия ФИО для текстов бота.

// CodePersonExists — POST /api/people со входом для человека, у которого вход уже есть.
const CodePersonExists = "PERSON_EXISTS"

const msgMentorNoLogin = "Наставник в кабинет не входит"

// PeopleRoles — роли справочника людей.
var PeopleRoles = []string{domain.RoleManager, domain.RoleMentor, domain.RoleRecruiter}

// PersonInput — POST /api/people. Login и Password — оба или ни одного.
type PersonInput struct {
	FullName string
	Role     string
	Login    string
	Password string
}

// ManagerOwns — сотрудник e — подчинённый руководителя v: по manager_id; у старой записи без
// manager_id — по совпадению имени (users.manager_name, иначе full_name).
func ManagerOwns(v *domain.User, e *domain.Employee) bool {
	if v == nil || e == nil {
		return false
	}
	if e.ManagerID != nil {
		return *e.ManagerID == v.ID
	}
	name := managerFallbackName(v)
	return name != "" && strings.TrimSpace(e.ManagerName) == name
}

// managerFallbackName — имя руководителя для старых карточек без manager_id.
func managerFallbackName(v *domain.User) string {
	if v.ManagerName != nil && strings.TrimSpace(*v.ManagerName) != "" {
		return strings.TrimSpace(*v.ManagerName)
	}
	return strings.TrimSpace(v.FullName)
}

// People — люди справочника: роль (пусто — manager, mentor, recruiter), подстрока ФИО.
func (s *UserService) People(ctx context.Context, role, q string) ([]domain.User, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	f := repositories.PeopleFilter{Roles: PeopleRoles, Q: q}
	if role != "" {
		if !isPeopleRole(role) {
			return nil, fieldErr("role", errs.CodeOutOfRange, "Роль: manager, mentor или recruiter")
		}
		f.Roles = []string{role}
	}
	if utf8.RuneCountInString(q) > 200 {
		return nil, fieldErr("q", errs.CodeOutOfRange, "Не длиннее 200 символов")
	}
	return s.repos.Users.People(ctx, f)
}

func isPeopleRole(r string) bool {
	for _, x := range PeopleRoles {
		if r == x {
			return true
		}
	}
	return false
}

// CreatePerson — новый руководитель или наставник. Без входа — может recruiter и hr_lead; со
// входом (login + password) — только hr_lead (или вход в кабинет выключен). Человек той же роли
// с тем же ФИО уже есть — возвращается он (created=false); если просили вход, а входа у него
// нет — вход выдаётся ему.
func (s *UserService) CreatePerson(ctx context.Context, in PersonInput) (*domain.User, bool, error) {
	if !s.repos.Enabled() {
		return nil, false, repositories.ErrNoDatabase
	}
	name := strings.TrimSpace(in.FullName)
	login := NormalizeLogin(in.Login)
	withLogin := login != "" || in.Password != ""
	switch {
	case name == "":
		return nil, false, fieldErr("full_name", errs.CodeRequired, "Обязательное поле")
	case utf8.RuneCountInString(name) > 200:
		return nil, false, fieldErr("full_name", errs.CodeOutOfRange, "Не длиннее 200 символов")
	case in.Role != domain.RoleManager && in.Role != domain.RoleMentor:
		return nil, false, fieldErr("role", errs.CodeOutOfRange, "Роль: manager или mentor")
	case withLogin && in.Role == domain.RoleMentor:
		return nil, false, fieldErr("login", errs.CodeOutOfRange, msgMentorNoLogin)
	case withLogin && login == "":
		return nil, false, fieldErr("login", errs.CodeRequired, "Логин: от 1 до 100 символов")
	case withLogin && utf8.RuneCountInString(login) > 100:
		return nil, false, fieldErr("login", errs.CodeOutOfRange, "Логин: от 1 до 100 символов")
	}
	if withLogin {
		if v := ViewerFrom(ctx); v != nil && v.Role != domain.RoleHRLead {
			return nil, false, errs.NewForbiddenError("Вход в кабинет выдаёт руководитель HR")
		}
		if err := checkPasswordLen(in.Password); err != nil {
			return nil, false, err
		}
	}
	existing, err := s.repos.Users.PersonByName(ctx, in.Role, name)
	if err != nil {
		return nil, false, err
	}
	if existing != nil && !strings.EqualFold(strings.TrimSpace(existing.FullName), name) {
		existing = nil // совпало только старое users.manager_name — это другой человек
	}
	if existing != nil {
		if !withLogin {
			return existing, false, nil
		}
		if existing.HasLogin() {
			return nil, false, errs.NewConflictError("У этого человека уже есть вход").WithCode(CodePersonExists)
		}
		u, err := s.Patch(ctx, existing.ID, UserPatch{Login: &login, Password: &in.Password})
		return u, false, err
	}
	if withLogin {
		u, err := s.Create(ctx, UserInput{Login: login, FullName: name, Password: in.Password, Role: in.Role, Active: true})
		return u, err == nil, err
	}
	u := &domain.User{FullName: name, Role: in.Role, Active: true}
	if err := s.repos.Users.Create(ctx, u); err != nil {
		return nil, false, err
	}
	return u, true, nil
}

// PersonRef — руководитель или наставник в теле запроса: Set — поле было; ID nil при Set —
// снять. Name — старое текстовое поле (POST /api/newcomers): найти по точному ФИО или создать.
type PersonRef struct {
	Set  bool
	ID   *uuid.UUID
	Name string
}

// resolvePerson — человек роли role по id (нет такого или другая роль — 422 field) или, если id
// не задан, по точному ФИО: нашёлся — он, нет — создаётся без входа. Пустое имя — nil.
func resolvePerson(ctx context.Context, r *repositories.Repos, role, field string, ref PersonRef) (*domain.User, error) {
	if ref.ID != nil {
		u, err := r.Users.ByID(ctx, *ref.ID)
		if err != nil {
			return nil, err
		}
		if u == nil || u.Role != role {
			msg := "Нужен руководитель из справочника"
			if role == domain.RoleMentor {
				msg = "Нужен наставник из справочника"
			}
			return nil, fieldErr(field, errs.CodeOutOfRange, msg)
		}
		return u, nil
	}
	name := strings.TrimSpace(ref.Name)
	if name == "" {
		return nil, nil
	}
	u, err := r.Users.PersonByName(ctx, role, name)
	if err != nil || u != nil {
		return u, err
	}
	u = &domain.User{FullName: name, Role: role, Active: true}
	if err := r.Users.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// PeopleLinkResult — итог переноса строк в справочник (cmd/seed -people).
type PeopleLinkResult struct {
	ManagersCreated, MentorsCreated int
	ManagersLinked, MentorsLinked   int64
}

// LinkPeople — перенос данных: для каждого различного ManagerName / MentorName в карточках без
// связи найти человека с ролью manager / mentor и тем же ФИО (руководителя — и по старому
// users.manager_name, так связывается демо-аккаунт manager) или создать его без входа, затем
// проставить manager_id / mentor_id. Идемпотентно: связанные карточки не трогаются.
func (s *UserService) LinkPeople(ctx context.Context) (*PeopleLinkResult, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	res := &PeopleLinkResult{}
	for _, mentor := range []bool{false, true} {
		role := domain.RoleManager
		if mentor {
			role = domain.RoleMentor
		}
		names, err := s.repos.Employees.UnlinkedPeopleNames(ctx, mentor)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
				u, err := tx.Users.PersonByName(ctx, role, name)
				if err != nil {
					return err
				}
				if u == nil {
					u = &domain.User{FullName: name, Role: role, Active: true}
					if err := tx.Users.Create(ctx, u); err != nil {
						return err
					}
					if mentor {
						res.MentorsCreated++
					} else {
						res.ManagersCreated++
					}
				}
				n, err := tx.Employees.LinkPeopleByName(ctx, mentor, name, u.ID)
				if mentor {
					res.MentorsLinked += n
				} else {
					res.ManagersLinked += n
				}
				return err
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// SetPeople — PATCH /api/newcomers/{id}: руководитель и/или наставник из справочника (ID nil —
// снять). Копия ФИО в карточке синхронизируется; при смене руководителя MAX-связка для push
// берётся у нового (users.max_user_id) — прежняя принадлежала другому человеку.
func (s *NewcomerService) SetPeople(ctx context.Context, id uuid.UUID, manager, mentor PersonRef) error {
	if err := s.ready(); err != nil {
		return err
	}
	if !manager.Set && !mentor.Set {
		return nil
	}
	return s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		e, err := tx.Employees.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if e == nil {
			return errs.NewNotFoundError(msgNewcomerNotFound)
		}
		fields := map[string]any{}
		if manager.Set {
			if manager.ID == nil {
				fields["manager_id"], fields["manager_name"], fields["manager_user_id"] = nil, "", nil
			} else {
				u, err := resolvePerson(ctx, tx, domain.RoleManager, "manager_id", PersonRef{ID: manager.ID})
				if err != nil {
					return err
				}
				fields["manager_id"], fields["manager_name"] = u.ID, u.FullName
				if e.ManagerID == nil || *e.ManagerID != u.ID {
					if e.ManagerID != nil || strings.TrimSpace(e.ManagerName) != strings.TrimSpace(u.FullName) || u.MaxUserID != nil {
						fields["manager_user_id"] = u.MaxUserID
					}
				}
			}
		}
		if mentor.Set {
			if mentor.ID == nil {
				fields["mentor_id"], fields["mentor_name"] = nil, ""
			} else {
				u, err := resolvePerson(ctx, tx, domain.RoleMentor, "mentor_id", PersonRef{ID: mentor.ID})
				if err != nil {
					return err
				}
				fields["mentor_id"], fields["mentor_name"] = u.ID, u.FullName
			}
		}
		_, err = tx.Employees.SetPeople(ctx, id, fields)
		return err
	})
}
