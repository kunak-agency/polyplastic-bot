package services

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Руководитель подразделения (кейс, роль 03; UX 1, 3.1 и 3.7): вход в бота по своей ссылке
// mgr_<token>, push «встретьтесь 1:1» по правилам кода (manager_rules.go) с кнопками
// «Встретился» / «Запланирую» / «Подробнее», вид кабинета «Мои новички». Руководитель не видит
// обращений, текстов ответов и категорий F0/F4/F5/F9/F10 — ни в MAX, ни в кабинете.

// Payload кнопок push руководителю.
const (
	payMgrMet  = "mg:met:"
	payMgrPlan = "mg:plan:"
)

// ManagerInviteTTL — сколько живёт ссылка руководителя.
const ManagerInviteTTL = 30 * 24 * time.Hour

// managerTickEvery — такт правил push.
const managerTickEvery = 5 * time.Minute

// managerCabinetURL — адрес кабинета для «Подробнее» и «Мои новички»: бот (bot.go) не держит
// настроек дайджеста, поэтому NewServices кладёт адрес сюда (manager_bot.go читает).
var managerCabinetURL = config.DefaultCabinetURL

// ManagerService — руководитель подразделения.
type ManagerService struct {
	repos *repositories.Repos
	tax   *taxonomy.Taxonomy
	max   max.Client
	bot   config.Bot
	cab   string
	now   func() time.Time
}

// Manager — сервис руководителя (по образцу Overview: без поля в Services).
func (s *Services) Manager() *ManagerService {
	o := s.Overview()
	m := &ManagerService{repos: s.repos, tax: o.tax, now: o.now, bot: s.botCfg, cab: cabinetBase(s.digestOpts.CabinetURL)}
	if s.ext != nil {
		m.max = s.ext.Max
	}
	return m
}

func cabinetBase(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		return config.DefaultCabinetURL
	}
	return u
}

// ManagerPageURL — «Мои новички» руководителя в кабинете.
func ManagerPageURL(base, name string) string {
	return cabinetBase(base) + "/manager?name=" + url.QueryEscape(name)
}

// ManagerInviteResult — ссылка руководителя для карточки новичка.
type ManagerInviteResult struct {
	ManagerName string
	Link        string
	ExpiresAt   time.Time
	Connected   bool // у новичка уже привязан руководитель
}

// Invite — ссылка «Пригласить руководителя»: действующая для этого имени или новая.
func (s *ManagerService) Invite(ctx context.Context, employeeID uuid.UUID) (*ManagerInviteResult, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	e, err := s.repos.Employees.ByID(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errs.NewNotFoundError(msgNewcomerNotFound)
	}
	name := strings.TrimSpace(e.ManagerName)
	if name == "" {
		return nil, errs.NewValidationError("В карточке не указан руководитель").
			WithDetails(errs.FieldError{Field: "manager_name", Code: errs.CodeRequired, Message: "Укажите руководителя в карточке новичка"})
	}
	now := s.now()
	inv, err := s.repos.Manager().ActiveInvite(ctx, name, now)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		token, err := newInviteToken()
		if err != nil {
			return nil, err
		}
		inv = &domain.ManagerInvite{EmployeeID: e.ID, ManagerName: name, Token: token, ExpiresAt: now.Add(ManagerInviteTTL)}
		if err := s.repos.Manager().CreateInvite(ctx, inv); err != nil {
			return nil, err
		}
	}
	return &ManagerInviteResult{
		ManagerName: name, ExpiresAt: inv.ExpiresAt, Connected: e.ManagerUserID != nil,
		Link: fmt.Sprintf(s.bot.InviteLinkFormat, s.bot.BotUsername(), domain.ManagerPayloadPrefix+inv.Token),
	}, nil
}

// Managers — руководители из карточек (для выбора в кабинете).
func (s *ManagerService) Managers(ctx context.Context) ([]repositories.ManagerSummary, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	return s.repos.Manager().Managers(ctx)
}

// ManagerView — «Мои новички».
type ManagerView struct {
	ManagerName string
	Connected   bool
	Items       []ManagerCard
}

// ManagerCard — карточка новичка для руководителя: статус адаптации и задачи «Встреча 1:1».
// Нет: обращений, риска с причинами, текстов ответов, категорий.
type ManagerCard struct {
	NewcomerID    uuid.UUID
	FullName      string
	Site          string
	Department    string
	Position      string
	StartDate     time.Time
	Day           int
	Stage         string
	Connection    string
	LastPulse     *int
	ProbationDate time.Time
	Attention     []string // поводы правил (manager_rules.go), без категорий
	Meetings      []ManagerTask
}

// ManagerTask — незакрытая задача «Встреча 1:1» (push без «Встретился»).
type ManagerTask struct {
	ID        uuid.UUID
	Kind      string
	Status    string
	Text      string
	CreatedAt time.Time
}

// View — новички руководителя name в адаптации.
func (s *ManagerService) View(ctx context.Context, name string) (*ManagerView, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	rows, err := s.repos.Overview().Load(ctx, "", now.Add(-60*24*time.Hour))
	if err != nil {
		return nil, err
	}
	mine := filterManagerRows(rows, name)
	var ids []uuid.UUID
	for _, e := range mine.Employees {
		ids = append(ids, e.ID)
	}
	nudges, err := s.repos.Manager().NudgesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	v := BuildManagerView(mine, nudges, name, now, s.tax)
	return &v, nil
}

// filterManagerRows — только новички с этим manager_name.
func filterManagerRows(rows *repositories.OverviewRows, name string) *repositories.OverviewRows {
	out := &repositories.OverviewRows{}
	keep := map[uuid.UUID]bool{}
	for _, e := range rows.Employees {
		if strings.TrimSpace(e.ManagerName) == name && name != "" {
			out.Employees = append(out.Employees, e)
			keep[e.ID] = true
		}
	}
	for _, ev := range rows.Events {
		if keep[ev.EmployeeID] {
			out.Events = append(out.Events, ev)
		}
	}
	for _, a := range rows.Answers {
		if keep[a.EmployeeID] {
			out.Answers = append(out.Answers, a)
		}
	}
	return out // обращения руководителю не нужны и не передаются
}

// BuildManagerView — карточки «Мои новички». Чистая функция (тесты видимости без базы).
func BuildManagerView(rows *repositories.OverviewRows, nudges []domain.ManagerNudge, name string, now time.Time, tax *taxonomy.Taxonomy) ManagerView {
	v := ManagerView{ManagerName: name, Items: []ManagerCard{}}
	attention := map[uuid.UUID][]string{}
	for _, c := range ManagerNudgeCandidates(rows, now, tax) {
		attention[c.Employee.ID] = append(attention[c.Employee.ID], c.Reason)
	}
	tasks := map[uuid.UUID][]ManagerTask{}
	for _, n := range nudges {
		if n.Status == domain.NudgeMet {
			continue
		}
		tasks[n.EmployeeID] = append(tasks[n.EmployeeID], ManagerTask{ID: n.ID, Kind: n.Kind, Status: n.Status, Text: n.Text, CreatedAt: n.CreatedAt})
	}
	for _, e := range rows.Employees {
		if e.ManagerUserID != nil {
			v.Connected = true
		}
		site := domain.SiteOf(e.Site)
		day := DayNumber(e.StartDate, now, site)
		stage := domain.StageForDay(day)
		if stage == domain.StageDone {
			continue
		}
		card := ManagerCard{
			NewcomerID: e.ID, FullName: e.FullName, Site: e.Site, Department: e.Department, Position: e.Position,
			StartDate: e.StartDate, Day: day, Stage: stage, Connection: e.Connection, LastPulse: e.LastPulse,
			ProbationDate: civilDate(e.StartDate, site.Loc).AddDate(0, 0, 90),
			Attention:     attention[e.ID], Meetings: tasks[e.ID],
		}
		if card.Attention == nil {
			card.Attention = []string{}
		}
		if card.Meetings == nil {
			card.Meetings = []ManagerTask{}
		}
		v.Items = append(v.Items, card)
	}
	sort.SliceStable(v.Items, func(i, j int) bool {
		a, b := v.Items[i], v.Items[j]
		if (len(a.Attention)+len(a.Meetings) > 0) != (len(b.Attention)+len(b.Meetings) > 0) {
			return len(a.Attention)+len(a.Meetings) > 0
		}
		return a.Day > b.Day
	})
	return v
}

// SentNudge — push, отправленный тактом.
type SentNudge struct {
	NudgeID    uuid.UUID
	NewcomerID uuid.UUID
	Manager    string
	Kind       string
	Text       string
	Delivered  bool // ушёл в MAX (руководитель подключён)
}

// Tick — правила по всем новичкам: новый повод → задача «Встреча 1:1» и push руководителю.
// Вне рабочих часов площадки не шлём (force — dev: сразу). Руководитель не подключён — задача
// создаётся, текст — в лог.
func (s *ManagerService) Tick(ctx context.Context, force bool) ([]SentNudge, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	rows, err := s.repos.Overview().Load(ctx, "", now.Add(-60*24*time.Hour))
	if err != nil {
		return nil, err
	}
	sent, err := s.repos.Manager().NudgeKeys(ctx)
	if err != nil {
		return nil, err
	}
	var out []SentNudge
	for _, c := range ManagerNudgeCandidates(rows, now, s.tax) {
		if sent[c.Key] || strings.TrimSpace(c.Employee.ManagerName) == "" {
			continue
		}
		if !force && !InWorkHours(now, domain.SiteOf(c.Employee.Site)) {
			continue
		}
		n := &domain.ManagerNudge{EmployeeID: c.Employee.ID, Kind: c.Kind, Key: c.Key, Text: c.Text, Status: domain.NudgeOpen}
		if c.Employee.ManagerUserID != nil {
			n.ManagerUserID = *c.Employee.ManagerUserID
		}
		created, err := s.repos.Manager().CreateNudge(ctx, n)
		if err != nil {
			return out, err
		}
		if !created {
			continue
		}
		res := SentNudge{NudgeID: n.ID, NewcomerID: c.Employee.ID, Manager: c.Employee.ManagerName, Kind: c.Kind, Text: c.Text}
		if n.ManagerUserID != 0 && s.max != nil {
			mid, err := s.max.Send(ctx, managerNudgeMessage(n, c.Employee.ManagerName, s.cab))
			if err != nil {
				logger.Warn("руководитель: push %s новичку %s не ушёл: %v", c.Kind, c.Employee.ID, err)
			} else {
				t := s.now()
				n.MessageID, n.SentAt = mid, &t
				res.Delivered = true
				if err := s.repos.Manager().SaveNudge(ctx, n); err != nil {
					logger.Warn("руководитель: push %s не сохранён: %v", n.ID, err)
				}
			}
		} else {
			logger.Info("руководитель «%s» не подключён к боту — push %s только в лог: %s", c.Employee.ManagerName, c.Kind, c.Text)
		}
		out = append(out, res)
	}
	return out, nil
}

// Run — такт правил push.
func (s *ManagerService) Run(ctx context.Context) {
	logger.Info("руководитель: правила push, такт %v", managerTickEvery)
	t := time.NewTicker(managerTickEvery)
	defer t.Stop()
	for {
		if list, err := s.Tick(ctx, false); err != nil && ctx.Err() == nil {
			logger.Warn("руководитель: такт не удался: %v", err)
		} else if len(list) > 0 {
			logger.Info("руководитель: новых поводов %d", len(list))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// managerNudgeMessage — push с кнопками «Встретился» / «Запланирую» / «Подробнее».
func managerNudgeMessage(n *domain.ManagerNudge, managerName, cab string) max.OutMessage {
	return max.OutMessage{
		UserID: n.ManagerUserID,
		Text:   n.Text,
		Buttons: [][]max.Button{
			{{Text: "Встретился", Payload: payMgrMet + n.ID.String()}, {Text: "Запланирую", Payload: payMgrPlan + n.ID.String()}},
			{{Text: "Подробнее", URL: ManagerPageURL(cab, managerName)}},
		},
	}
}
