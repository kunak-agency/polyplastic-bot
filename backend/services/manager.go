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
	// payMgrIP — решение по ИС под push D+87 (FEATURE_RISK_V2): mg:ip:<keep|extend|hr>:<nudge id>.
	payMgrIP    = "mg:ip:"
	ipDiscussHR = "hr"
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
	v2    bool // FEATURE_RISK_V2: поводы v2 и лимиты (manager_rules.go)
}

// Manager — сервис руководителя (по образцу Overview: без поля в Services).
func (s *Services) Manager() *ManagerService {
	o := s.Overview()
	m := &ManagerService{repos: s.repos, tax: o.tax, now: o.now, bot: s.botCfg, cab: cabinetBase(s.digestOpts.CabinetURL), v2: s.features.RiskV2}
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

// NewcomerPageURL — карточка новичка в кабинете.
func NewcomerPageURL(base string, id uuid.UUID) string {
	return cabinetBase(base) + "/newcomers/" + id.String()
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
			WithDetails(errs.FieldError{Field: "manager_name", Code: errs.CodeRequired, Message: "Укажите руководителя в карточке сотрудника"})
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
	// IPDecision — решение по ИС: keep | extend | part | nil; IPDecisionAt — когда записано.
	IPDecision   *string
	IPDecisionAt *time.Time
	Attention    []string // поводы правил (manager_rules.go), без категорий
	Meetings     []ManagerTask
}

// ManagerTask — незакрытая задача «Встреча 1:1» (push без «Встретился»).
type ManagerTask struct {
	ID        uuid.UUID
	Kind      string
	Status    string
	Text      string
	CreatedAt time.Time
}

// View — новички руководителя name в адаптации (hr_lead выбирает руководителя по имени; имя в
// карточке — копия ФИО из справочника).
func (s *ManagerService) View(ctx context.Context, name string) (*ManagerView, error) {
	return s.view(ctx, name, func(e *domain.Employee) bool {
		return name != "" && strings.TrimSpace(e.ManagerName) == name
	})
}

// ViewFor — «Мои новички» вошедшего руководителя: подчинённые по manager_id, старые карточки
// без manager_id — по имени (ManagerOwns).
func (s *ManagerService) ViewFor(ctx context.Context, v *domain.User) (*ManagerView, error) {
	return s.view(ctx, strings.TrimSpace(v.FullName), func(e *domain.Employee) bool { return ManagerOwns(v, e) })
}

func (s *ManagerService) view(ctx context.Context, name string, keep func(*domain.Employee) bool) (*ManagerView, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	rows, err := s.repos.Overview().Load(ctx, "", now.Add(-60*24*time.Hour))
	if err != nil {
		return nil, err
	}
	mine := filterManagerRows(rows, keep)
	var ids []uuid.UUID
	for _, e := range mine.Employees {
		ids = append(ids, e.ID)
	}
	nudges, err := s.repos.Manager().NudgesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	opt, err := s.nudgeOptions(ctx, mine)
	if err != nil {
		return nil, err
	}
	v := BuildManagerView(mine, nudges, name, now, s.tax, opt)
	return &v, nil
}

// nudgeOptions — режим правил: v2 и у кого приложен адаптационный план.
func (s *ManagerService) nudgeOptions(ctx context.Context, rows *repositories.OverviewRows) (NudgeOptions, error) {
	opt := NudgeOptions{V2: s.v2}
	if !s.v2 {
		return opt, nil
	}
	ids := make([]uuid.UUID, 0, len(rows.Employees))
	for _, e := range rows.Employees {
		ids = append(ids, e.ID)
	}
	plan, err := s.repos.Employees.WithFile(ctx, ids, domain.PersonalAdaptationPlan)
	if err != nil {
		return opt, err
	}
	opt.WithPlan = plan
	return opt, nil
}

// filterManagerRows — только подчинённые руководителя (mine).
func filterManagerRows(rows *repositories.OverviewRows, mine func(*domain.Employee) bool) *repositories.OverviewRows {
	out := &repositories.OverviewRows{}
	keep := map[uuid.UUID]bool{}
	for i := range rows.Employees {
		if e := rows.Employees[i]; mine(&e) {
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
func BuildManagerView(rows *repositories.OverviewRows, nudges []domain.ManagerNudge, name string, now time.Time, tax *taxonomy.Taxonomy, opts ...NudgeOptions) ManagerView {
	v := ManagerView{ManagerName: name, Items: []ManagerCard{}}
	attention := map[uuid.UUID][]string{}
	for _, c := range ManagerNudgeCandidates(rows, now, tax, opts...) {
		attention[c.Employee.ID] = append(attention[c.Employee.ID], c.Reason)
	}
	tasks := map[uuid.UUID][]ManagerTask{}
	for _, n := range nudges {
		if n.Status == domain.NudgeMet || n.Kind == domain.NudgeIPDiscuss {
			continue // «Обсудить с HR» — задача HR, не руководителя
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
			IPDecision:    e.IPDecision, IPDecisionAt: e.IPDecisionAt,
			Attention: attention[e.ID], Meetings: tasks[e.ID],
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
	opt, err := s.nudgeOptions(ctx, rows)
	if err != nil {
		return nil, err
	}
	cands := ManagerNudgeCandidates(rows, now, s.tax, opt)
	maxOf, err := s.managerMax(ctx, rows)
	if err != nil {
		return nil, err
	}
	var lim *nudgeLimiter
	if s.v2 {
		sort.SliceStable(cands, func(i, j int) bool { return nudgeRank[cands[i].Kind] < nudgeRank[cands[j].Kind] })
		if lim, err = s.limiter(ctx, rows, now); err != nil {
			return nil, err
		}
	}
	var out []SentNudge
	for _, c := range cands {
		if sent[c.Key] || (c.Employee.ManagerID == nil && strings.TrimSpace(c.Employee.ManagerName) == "") {
			continue
		}
		if !force && !InWorkHours(now, domain.SiteOf(c.Employee.Site)) {
			continue
		}
		if lim != nil && !lim.allow(c) {
			continue // лимит: повод не записан и уйдёт в другой день, если ещё в окне
		}
		n := &domain.ManagerNudge{EmployeeID: c.Employee.ID, Kind: c.Kind, Key: c.Key, Text: c.Text, Status: domain.NudgeOpen}
		n.CreatedAt = now // часы сервиса: по ним считаются лимиты v2
		if c.Employee.ManagerUserID != nil {
			n.ManagerUserID = *c.Employee.ManagerUserID
		} else if c.Employee.ManagerID != nil && maxOf[*c.Employee.ManagerID] != 0 {
			// MAX руководителя из справочника (users.max_user_id): привязка по ссылке ещё не
			// дошла до этой карточки.
			n.ManagerUserID = maxOf[*c.Employee.ManagerID]
		}
		created, err := s.repos.Manager().CreateNudge(ctx, n)
		if err != nil {
			return out, err
		}
		if !created {
			continue
		}
		if lim != nil {
			lim.take(c)
		}
		res := SentNudge{NudgeID: n.ID, NewcomerID: c.Employee.ID, Manager: c.Employee.ManagerName, Kind: c.Kind, Text: c.Text}
		if n.ManagerUserID != 0 && s.max != nil {
			msg := managerNudgeMessage(n, c.Employee.ManagerName, s.cab)
			if s.v2 {
				msg = managerNudgeMessageV2(n, c.Employee.ManagerName, s.cab)
			}
			mid, err := s.max.Send(ctx, msg)
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

// managerMax — MAX user_id руководителей из справочника (users.max_user_id) по manager_id.
func (s *ManagerService) managerMax(ctx context.Context, rows *repositories.OverviewRows) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	var ids []uuid.UUID
	for _, e := range rows.Employees {
		if e.ManagerID != nil && e.ManagerUserID == nil {
			ids = append(ids, *e.ManagerID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	users, err := s.repos.Users.ByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for id, u := range users {
		if u.MaxUserID != nil && u.Active {
			out[id] = *u.MaxUserID
		}
	}
	return out, nil
}

// managerKey — кто руководитель для лимитов push: "id:<manager_id>", у старых карточек — имя.
func managerKey(e *domain.Employee) string {
	if e.ManagerID != nil {
		return "id:" + e.ManagerID.String()
	}
	return strings.TrimSpace(e.ManagerName)
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

// managerNudgeMessageV2 — push v2: D+87 — кнопки решения по ИС, D+1 — ссылка на карточку для
// плана, остальное — как раньше («Встретился» / «Запланирую» / «Подробнее»).
func managerNudgeMessageV2(n *domain.ManagerNudge, managerName, cab string) max.OutMessage {
	id := n.ID.String()
	switch n.Kind {
	case domain.NudgeProbation:
		return max.OutMessage{UserID: n.ManagerUserID, Text: n.Text, Buttons: [][]max.Button{
			{{Text: "Оставить", Payload: payMgrIP + domain.IPKeep + ":" + id}, {Text: "Продлить", Payload: payMgrIP + domain.IPExtend + ":" + id}},
			{{Text: "Обсудить с HR", Payload: payMgrIP + ipDiscussHR + ":" + id}},
		}}
	case domain.NudgePlanNeeded:
		return max.OutMessage{UserID: n.ManagerUserID, Text: n.Text, Buttons: [][]max.Button{
			{{Text: "Приложить план", URL: NewcomerPageURL(cab, n.EmployeeID)}},
		}}
	}
	return managerNudgeMessage(n, managerName, cab)
}

// nudgeLimiter — лимиты push на руководителя (v2): не больше nudgeLimitDay в день и
// nudgeLimitWeek поводов для 1:1 за 7 дней. Руководитель — managerKey (manager_id или имя).
type nudgeLimiter struct {
	day, week map[string]int
}

// limiter — счётчики по уже записанным push за 7 дней.
func (s *ManagerService) limiter(ctx context.Context, rows *repositories.OverviewRows, now time.Time) (*nudgeLimiter, error) {
	lim := &nudgeLimiter{day: map[string]int{}, week: map[string]int{}}
	mgr := map[uuid.UUID]string{}
	ids := make([]uuid.UUID, 0, len(rows.Employees))
	for i := range rows.Employees {
		e := &rows.Employees[i]
		mgr[e.ID] = managerKey(e)
		ids = append(ids, e.ID)
	}
	list, err := s.repos.Manager().NudgesOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	loc := domain.SiteOf("").Loc
	today := civilDate(now.In(loc), loc)
	weekFrom := now.AddDate(0, 0, -nudgeLimitWindowDays)
	for _, n := range list {
		name := mgr[n.EmployeeID]
		if name == "" || n.Kind == domain.NudgeIPDiscuss || n.CreatedAt.Before(weekFrom) {
			continue
		}
		if !civilDate(n.CreatedAt.In(loc), loc).Before(today) {
			lim.day[name]++
		}
		if nudgeMeeting(n.Kind) {
			lim.week[name]++
		}
	}
	return lim, nil
}

func (l *nudgeLimiter) allow(c NudgeCandidate) bool {
	name := managerKey(&c.Employee)
	if l.day[name] >= nudgeLimitDay {
		return false
	}
	return !nudgeMeeting(c.Kind) || l.week[name] < nudgeLimitWeek
}

func (l *nudgeLimiter) take(c NudgeCandidate) {
	name := managerKey(&c.Employee)
	l.day[name]++
	if nudgeMeeting(c.Kind) {
		l.week[name]++
	}
}
