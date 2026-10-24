package repositories

import (
	"context"
	"strings"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmployeeRepo — новички, приглашения, запросы «позвать человека».
type EmployeeRepo struct {
	db *gorm.DB
}

// EmployeeFilter — фильтры реестра (GET /api/newcomers). Пустое поле — без фильтра.
type EmployeeFilter struct {
	Site string
	Q    string // подстрока ФИО, подразделения или должности
	Risk string
}

func (r *EmployeeRepo) Create(ctx context.Context, e *domain.Employee) error {
	return r.db.WithContext(ctx).Create(e).Error
}

// Save — записать карточку целиком.
func (r *EmployeeRepo) Save(ctx context.Context, e *domain.Employee) error {
	return r.db.WithContext(ctx).Save(e).Error
}

func (r *EmployeeRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Employee, error) {
	var e domain.Employee
	return ignoreNotFound(&e, r.db.WithContext(ctx).First(&e, "id = ?", id).Error)
}

// ByIDForUpdate — карточка под блокировкой строки до конца транзакции: ответы на кнопки и
// поллер не перетирают друг другу поля.
func (r *EmployeeRepo) ByIDForUpdate(ctx context.Context, id uuid.UUID) (*domain.Employee, error) {
	var e domain.Employee
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, "id = ?", id).Error
	return ignoreNotFound(&e, err)
}

func (r *EmployeeRepo) ByMaxUser(ctx context.Context, userID int64) (*domain.Employee, error) {
	var e domain.Employee
	return ignoreNotFound(&e, r.db.WithContext(ctx).First(&e, "max_user_id = ?", userID).Error)
}

// List — реестр по фильтру, свежие даты выхода первыми. Подстрока ищется ILIKE с параметром:
// спецсимволы LIKE экранируются, чтобы «%» в поиске не значил «всё».
func (r *EmployeeRepo) List(ctx context.Context, f EmployeeFilter) ([]domain.Employee, error) {
	q := r.db.WithContext(ctx).Model(&domain.Employee{})
	if f.Site != "" {
		q = q.Where("site = ?", f.Site)
	}
	if f.Risk != "" {
		q = q.Where("risk = ?", f.Risk)
	}
	if s := strings.TrimSpace(f.Q); s != "" {
		like := "%" + likeEscape(s) + "%"
		q = q.Where("full_name ILIKE ? OR department ILIKE ? OR position ILIKE ?", like, like, like)
	}
	var out []domain.Employee
	return out, q.Order("start_date DESC, created_at DESC").Find(&out).Error
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ByScenario — новички сценария, которые ещё идут по таймлайну (не отключились).
func (r *EmployeeRepo) ByScenario(ctx context.Context, scenarioID uuid.UUID) ([]domain.Employee, error) {
	var out []domain.Employee
	err := r.db.WithContext(ctx).Where("scenario_id = ? AND connection <> ?", scenarioID, domain.ConnStopped).Find(&out).Error
	return out, err
}

// SetChat — запомнить чат диалога с новичком (из нажатия, если bot_started его не принёс) —
// точечно, без перезаписи карточки.
func (r *EmployeeRepo) SetChat(ctx context.Context, id uuid.UUID, chatID int64) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ? AND chat_id IS NULL", id).Update("chat_id", chatID).Error
}

// UnbindMaxUser — снять связку аккаунта MAX со всех карточек, кроме keep. Нужна, когда один
// тестовый аккаунт открывает приглашения разных синтетических новичков.
func (r *EmployeeRepo) UnbindMaxUser(ctx context.Context, userID int64, keep uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).
		Where("max_user_id = ? AND id <> ?", userID, keep).
		Updates(map[string]any{"max_user_id": nil, "chat_id": nil, "connection": domain.ConnStopped}).Error
}

// CreateInvite — новое приглашение.
func (r *EmployeeRepo) CreateInvite(ctx context.Context, inv *domain.InviteToken) error {
	return r.db.WithContext(ctx).Create(inv).Error
}

func (r *EmployeeRepo) InviteByToken(ctx context.Context, token string) (*domain.InviteToken, error) {
	var inv domain.InviteToken
	return ignoreNotFound(&inv, r.db.WithContext(ctx).First(&inv, "token = ?", token).Error)
}

// LatestInvites — последнее приглашение каждого из сотрудников: для invite_link в списке.
func (r *EmployeeRepo) LatestInvites(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.InviteToken, error) {
	out := map[uuid.UUID]domain.InviteToken{}
	if len(ids) == 0 {
		return out, nil
	}
	var list []domain.InviteToken
	err := r.db.WithContext(ctx).Where("employee_id IN ?", ids).Order("created_at").Find(&list).Error
	for _, inv := range list {
		out[inv.EmployeeID] = inv // по возрастанию: последнее перезаписывает
	}
	return out, err
}

func (r *EmployeeRepo) MarkInviteUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.InviteToken{}).Where("id = ?", id).Update("used_at", at).Error
}

// ExtendInvites — продлить приглашения сотрудника (перенос даты выхода).
func (r *EmployeeRepo) ExtendInvites(ctx context.Context, employeeID uuid.UUID, until time.Time) error {
	return r.db.WithContext(ctx).Model(&domain.InviteToken{}).
		Where("employee_id = ? AND expires_at < ?", employeeID, until).Update("expires_at", until).Error
}

func (r *EmployeeRepo) AddHumanRequest(ctx context.Context, h *domain.HumanRequest) error {
	return r.db.WithContext(ctx).Create(h).Error
}

// CountActiveByScenario — число новичков по сценариям (active_newcomers).
func (r *EmployeeRepo) CountActiveByScenario(ctx context.Context) (map[uuid.UUID]int, error) {
	var rows []struct {
		ScenarioID uuid.UUID
		N          int
	}
	err := r.db.WithContext(ctx).Model(&domain.Employee{}).
		Select("scenario_id, count(*) AS n").
		Where("connection <> ?", domain.ConnStopped).
		Group("scenario_id").Scan(&rows).Error
	out := make(map[uuid.UUID]int, len(rows))
	for _, x := range rows {
		out[x.ScenarioID] = x.N
	}
	return out, err
}

// SetScenarioVersion — новичок переведён на другую версию сценария (публикация «к активным»).
func (r *EmployeeRepo) SetScenarioVersion(ctx context.Context, id uuid.UUID, version int) error {
	return r.db.WithContext(ctx).Model(&domain.Employee{}).Where("id = ?", id).Update("scenario_version", version).Error
}
