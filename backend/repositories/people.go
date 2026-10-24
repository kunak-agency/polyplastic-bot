package repositories

import (
	"context"
	"strings"
	"time"

	"main/domain"

	"github.com/google/uuid"
)

// Справочник людей (руководители, наставники, рекрутеры): users с ролью, в том числе без входа.
// Карточка сотрудника ссылается на них manager_id / mentor_id (api_contract_auth.md, «Люди»).

// PeopleFilter — GET /api/people?role=&q=. Roles пусто — все роли справочника.
type PeopleFilter struct {
	Roles  []string
	Q      string
	Active *bool
}

// People — люди справочника по имени.
func (r *UserRepo) People(ctx context.Context, f PeopleFilter) ([]domain.User, error) {
	q := r.db.WithContext(ctx).Model(&domain.User{})
	if len(f.Roles) > 0 {
		q = q.Where("role IN ?", f.Roles)
	}
	if s := strings.TrimSpace(f.Q); s != "" {
		q = q.Where("full_name ILIKE ?", "%"+likeEscape(s)+"%")
	}
	if f.Active != nil {
		q = q.Where("active = ?", *f.Active)
	}
	var out []domain.User
	return out, q.Order("full_name, created_at").Limit(500).Find(&out).Error
}

// PersonByName — человек роли role с этим ФИО (без учёта регистра и пробелов по краям) или,
// для руководителя, со старым users.manager_name. Сначала активные со входом, затем старшие.
// nil — нет такого.
func (r *UserRepo) PersonByName(ctx context.Context, role, name string) (*domain.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("role = ?", role)
	if role == domain.RoleManager {
		q = q.Where("lower(trim(full_name)) = lower(?) OR lower(trim(coalesce(manager_name, ''))) = lower(?)", name, name)
	} else {
		q = q.Where("lower(trim(full_name)) = lower(?)", name)
	}
	var u domain.User
	err := q.Order("active DESC, (login IS NOT NULL) DESC, created_at").First(&u).Error
	return ignoreNotFound(&u, err)
}

// SyncPeopleNames — переименовали человека справочника: копия ФИО в карточках, где он выбран
// руководителем или наставником.
func (r *EmployeeRepo) SyncPeopleNames(ctx context.Context, userID uuid.UUID, name string) error {
	db := r.db.WithContext(ctx).Model(&domain.Employee{})
	if err := db.Where("manager_id = ?", userID).Updates(map[string]any{"manager_name": name, "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("mentor_id = ?", userID).
		Updates(map[string]any{"mentor_name": name, "updated_at": time.Now()}).Error
}

// UnlinkedPeopleNames — различные непустые имена руководителей (column = manager_name) или
// наставников (mentor_name) в карточках без связи (manager_id / mentor_id IS NULL).
func (r *EmployeeRepo) UnlinkedPeopleNames(ctx context.Context, mentor bool) ([]string, error) {
	nameCol, idCol := "manager_name", "manager_id"
	if mentor {
		nameCol, idCol = "mentor_name", "mentor_id"
	}
	var out []string
	err := r.db.WithContext(ctx).Raw("SELECT DISTINCT trim(" + nameCol + ") AS name FROM employees WHERE trim(" + nameCol +
		") <> '' AND " + idCol + " IS NULL ORDER BY 1").Scan(&out).Error
	return out, err
}

// LinkPeopleByName — карточкам без связи с этим именем проставить manager_id / mentor_id.
// Имя карточки не трогаем: тексты бота уже его показывают. Сколько карточек обновлено.
func (r *EmployeeRepo) LinkPeopleByName(ctx context.Context, mentor bool, name string, userID uuid.UUID) (int64, error) {
	nameCol, idCol := "manager_name", "manager_id"
	if mentor {
		nameCol, idCol = "mentor_name", "mentor_id"
	}
	res := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Where(idCol+" IS NULL AND trim("+nameCol+") = ?", name).
		Updates(map[string]any{idCol: userID, "updated_at": time.Now()})
	return res.RowsAffected, res.Error
}

// SetPeople — руководитель и/или наставник карточки (поля: manager_id, manager_name,
// manager_user_id, mentor_id, mentor_name). Сколько карточек обновлено.
func (r *EmployeeRepo) SetPeople(ctx context.Context, id uuid.UUID, fields map[string]any) (int64, error) {
	fields["updated_at"] = time.Now()
	res := r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).Updates(fields)
	return res.RowsAffected, res.Error
}
