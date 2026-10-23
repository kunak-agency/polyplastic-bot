// Команда seed-demo — синтетический поток новичков для раздела «Обзор»: 50 персон из
// data/personas.yaml и добор до -count (по умолчанию 150) из шаблонов extra.go, с ответами на
// опросы, сигналами и обращениями. Нужна, потому что трёх новичков сида мало для heat map и
// тренда с порогом анонимности ≥ 5 человек, а 50 открывают в heat map лишь пару ячеек.
//
//	cd backend && go run ./cmd/seed          # сначала обычный сид: из него берутся шаги сценария
//	cd backend && APP_ENV=development go run ./cmd/seed-demo [-count 150]
//
// Только APP_ENV=development. Повторный запуск никого не дублирует: персона с тем же ФИО
// пропускается (добор детерминирован: тот же -count — те же люди). Чистая пересборка — `go run ./cmd/seed -reset -yes`, затем снова seed-demo.
//
// Всё синтетическое: ФИО из personas.yaml и добора помечены «(синтетика)», телефоны +7 000 100-…,
// аккаунты MAX — несуществующие id 910000xxx. Текстов ответов нет: только оценки, кнопки
// причин и коды сигналов (как после разметки), так что и в обзор текст попасть не может.
// Значения шкал — по кривым траекторий из data/generator-spec.yaml (seed 20261023).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/pkg/taxonomy"
	"main/repositories"
	"main/services"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// demoScenario — сценарий потока: шаги «Офис GrPP» из сида, для всех площадок.
const (
	demoScenario = "Демо-поток · все площадки (синтетика)"
	baseScenario = "Офис GrPP"
	genSeed      = 20261023
)

type persona struct {
	ID          string       `yaml:"id"`
	FullName    string       `yaml:"full_name"`
	Site        string       `yaml:"site"`
	Department  string       `yaml:"department"`
	Position    string       `yaml:"position"`
	Shift       *int         `yaml:"shift"`
	AddressForm string       `yaml:"address_form"`
	Laconic     bool         `yaml:"laconic"`
	Trajectory  string       `yaml:"trajectory"`
	HiddenFlags []hiddenFlag `yaml:"hidden_flags"`
	// Day — день адаптации, заданный генератором добора (extra.go); у персон из yaml пусто —
	// день выбирает dayPlan.
	Day *int `yaml:"-"`
}

type hiddenFlag struct {
	Code     string  `yaml:"code"`
	Severity int     `yaml:"severity_peak"`
	Onset    string  `yaml:"onset"`
	Resolved *string `yaml:"resolved"`
	open     bool    // добор: сигнал P0 намеренно не решён — красная зона
}

type flagSpan struct {
	code     string
	severity int
	onset    int
	resolved int // math.MaxInt — не решён
}

func main() {
	count := flag.Int("count", 150, "сколько новичков в демо-потоке: сначала персоны из data/personas.yaml, сверх них — добор из шаблонов (extra.go)")
	flag.Parse()
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("конфигурация: %v", err)
	}
	if cfg.AppEnv != config.EnvDevelopment {
		log.Fatalf("seed-demo: только APP_ENV=development (сейчас %s)", cfg.AppEnv)
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL пуст — некуда писать")
	}
	repos, err := repositories.InitRepos(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	if err := repos.MigrateDB(); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	tax, err := taxonomy.LoadDefault(cfg.TaxonomyPath)
	if err != nil {
		log.Fatalf("%v", err)
	}
	people, err := loadPersonas()
	if err != nil {
		log.Fatalf("персоны: %v", err)
	}
	if *count < len(people) {
		people = people[:*count]
	}
	svc := services.NewServices(repos, &externalApi.ExternalAPI{}, services.Options{Bot: cfg.Bot, Taxonomy: tax})
	ctx := context.Background()
	scID, surveys := demoScenarioID(ctx, repos, svc)

	g := &gen{repos: repos, svc: svc, tax: tax, rng: rand.New(rand.NewSource(genSeed)), now: time.Now(), scenario: scID, surveys: surveys}
	days := g.dayPlan(people)
	for _, p := range extraPersonas(people, *count-len(people)) {
		people = append(people, p)
		days = append(days, *p.Day)
	}
	made := 0
	for i, p := range people {
		if g.person(ctx, i, p, days[i]) {
			made++
		}
	}
	log.Printf("seed-demo: готово, новых новичков %d из %d", made, len(people))
}

// loadPersonas — data/personas.yaml от рабочего каталога вверх.
func loadPersonas() ([]persona, error) {
	for _, p := range []string{"data/personas.yaml", "../data/personas.yaml", "../../data/personas.yaml", "../../../data/personas.yaml"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var root struct {
			Synthetic bool      `yaml:"synthetic"`
			Personas  []persona `yaml:"personas"`
		}
		if err := yaml.Unmarshal(b, &root); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if !root.Synthetic {
			return nil, fmt.Errorf("%s: нет пометки synthetic: true — не берём", p)
		}
		abs, _ := filepath.Abs(p)
		log.Printf("seed-demo: персоны из %s (%d)", abs, len(root.Personas))
		return root.Personas, nil
	}
	return nil, errors.New("data/personas.yaml не найден")
}

// demoScenarioID — опубликованный сценарий потока; нет — копия шагов «Офис GrPP» без
// площадных меток. Возвращает и опросы по заголовку шага.
func demoScenarioID(ctx context.Context, repos *repositories.Repos, svc *services.Services) (uuid.UUID, map[string]*domain.Survey) {
	if sc, err := repos.Scenarios.ByName(ctx, demoScenario); err != nil {
		log.Fatalf("сценарий: %v", err)
	} else if sc != nil {
		d, err := svc.Scenarios.Get(ctx, sc.ID)
		if err != nil {
			log.Fatalf("сценарий: %v", err)
		}
		return sc.ID, surveysOf(d.Steps)
	}
	base, err := repos.Scenarios.ByName(ctx, baseScenario)
	if err != nil || base == nil {
		log.Fatalf("сценарий «%s» не найден — сначала go run ./cmd/seed (%v)", baseScenario, err)
	}
	d, err := svc.Scenarios.Get(ctx, base.ID)
	if err != nil {
		log.Fatalf("сценарий: %v", err)
	}
	var steps []domain.ScenarioStep
	for _, st := range d.Steps {
		if len(st.Sites) > 0 {
			continue // метки площадок: в общем потоке их нет
		}
		st.ID, st.ScenarioID = uuid.Nil, uuid.Nil
		steps = append(steps, st)
	}
	nd, err := svc.Scenarios.Create(ctx, demoScenario, []string{}, steps)
	if err != nil {
		log.Fatalf("сценарий: %v", err)
	}
	if _, err := svc.Scenarios.Publish(ctx, nd.Scenario.ID, "Синтетический поток для обзора", services.ApplyNew); err != nil {
		log.Fatalf("публикация: %v", err)
	}
	log.Printf("seed-demo: сценарий «%s», шагов %d", demoScenario, len(steps))
	return nd.Scenario.ID, surveysOf(nd.Steps)
}

func surveysOf(steps []domain.ScenarioStep) map[string]*domain.Survey {
	out := map[string]*domain.Survey{}
	for _, st := range steps {
		if st.Survey != nil {
			out[st.Title] = st.Survey
		}
	}
	return out
}

type gen struct {
	repos    *repositories.Repos
	svc      *services.Services
	tax      *taxonomy.Taxonomy
	rng      *rand.Rand
	now      time.Time
	scenario uuid.UUID
	surveys  map[string]*domain.Survey
}

// dayPlan — день адаптации каждой персоны: все этапы, большинство — в последние полтора
// месяца (тогда тренд за 4 недели и heat map за 30 дней набирают ≥ 5 человек).
//
// Персоны с инцидентом (critical_incident) встают через 2–9 дней после него: иначе их сигнал
// ещё впереди и красной зоны в демо нет. Остальные с сигналами — через 1–25 дней после первого.
func (g *gen) dayPlan(people []persona) []int {
	n := len(people)
	type band struct{ share, from, to int }
	bands := []band{{6, -6, -1}, {3, 0, 0}, {10, 1, 7}, {17, 8, 30}, {10, 31, 60}, {4, 61, 89}}
	var days []int
	for _, b := range bands {
		for i := 0; i < b.share; i++ {
			days = append(days, b.from+g.rng.Intn(b.to-b.from+1))
		}
	}
	for len(days) < n {
		days = append(days, 8+g.rng.Intn(23))
	}
	g.rng.Shuffle(len(days), func(i, j int) { days[i], days[j] = days[j], days[i] })
	days = days[:n]
	for i, p := range people {
		flags, incident := spans(p)
		switch {
		case incident == math.MaxInt:
		case p.Trajectory == "critical_incident":
			days[i] = min(89, incident+2+g.rng.Intn(8))
		default:
			// Сигнал начался не раньше чем 25 дней назад: тема попадает в окно heat map (30 дней).
			first := flags[0].onset
			for _, f := range flags {
				first = min(first, f.onset)
			}
			days[i] = min(89, first+1+g.rng.Intn(25))
		}
	}
	return days
}

// spans — сигналы персоны и день инцидента (первый severity 3, иначе первый сигнал).
func spans(p persona) ([]flagSpan, int) {
	var flags []flagSpan
	incident := math.MaxInt
	for _, f := range p.HiddenFlags {
		s := flagSpan{code: f.Code, severity: f.Severity, onset: dayOf(f.Onset), resolved: math.MaxInt}
		if f.Resolved != nil {
			s.resolved = dayOf(*f.Resolved)
		}
		flags = append(flags, s)
		if f.Severity >= 3 && s.onset < incident {
			incident = s.onset
		}
	}
	if incident == math.MaxInt && len(flags) > 0 {
		incident = flags[0].onset
	}
	return flags, incident
}

func strp(s string) *string { return &s }

// dayOf — «D0», «D+14» → 0, 14.
func dayOf(s string) int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "D")
	n, _ := strconv.Atoi(strings.TrimPrefix(s, "+"))
	return n
}

// curve — базовая кривая шкал 1–10 по траектории (generator-spec.yaml → trajectories).
func curve(trajectory string, day, incident int) float64 {
	pts := map[string][][2]float64{
		"stable":      {{14, 8.6}, {90, 8.8}},
		"dip_recover": {{14, 7.0}, {30, 6.2}, {52, 7.4}, {90, 8.3}},
		"rising_risk": {{14, 7.2}, {30, 6.4}, {52, 5.6}, {90, 5.0}},
	}
	if trajectory == "critical_incident" {
		if day >= incident {
			return 4.0
		}
		return 8.0
	}
	p, ok := pts[trajectory]
	if !ok {
		p = pts["stable"]
	}
	d := float64(day)
	if d <= p[0][0] {
		// До D+14 точек нет: первые дни чуть светлее первой точки.
		return math.Min(9.2, p[0][1]+0.4)
	}
	for i := 1; i < len(p); i++ {
		if d <= p[i][0] {
			k := (d - p[i-1][0]) / (p[i][0] - p[i-1][0])
			return p[i-1][1] + k*(p[i][1]-p[i-1][1])
		}
	}
	return p[len(p)-1][1]
}

// workday — дата выхода не в выходной: суббота → пятница, воскресенье → понедельник.
func workday(d time.Time) time.Time {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	}
	return d
}

var (
	reasonByFlag = map[string]string{"F1": "Задачи и нагрузка", "F6": "Задачи и нагрузка", "F3": "Люди и общение", "F4": "Люди и общение", "F5": "Люди и общение", "F2": "Условия и место"}
	staff        = []string{"Анна Кадрова (синтетика)", "Олег Наймов (синтетика)", "Мария Опекова (синтетика)"}
)

// person — одна персона: запуск адаптации, подключение, история опросов и обращения.
func (g *gen) person(ctx context.Context, i int, p persona, day int) bool {
	list, err := g.repos.Employees.List(ctx, repositories.EmployeeFilter{Q: p.FullName})
	if err != nil {
		log.Fatalf("новичок: %v", err)
	}
	if slices.ContainsFunc(list, func(e domain.Employee) bool { return e.FullName == p.FullName }) {
		log.Printf("seed-demo: «%s» уже есть — пропускаю", p.FullName)
		return false
	}
	site := domain.SiteOf(p.Site)
	today := services.StartOfDay(g.now, site)
	start := workday(today.AddDate(0, 0, -day))
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	day = services.DayNumber(start, g.now, site)
	var shift *string
	if p.Shift != nil && p.Site == domain.SiteGTP {
		shift = strp(strconv.Itoa(*p.Shift))
	}
	c, err := g.svc.Newcomers.Create(ctx, services.NewcomerInput{
		FullName: p.FullName, Phone: fmt.Sprintf("+7 000 100-%02d-%02d", (i+1)/100, (i+1)%100), Site: p.Site,
		Department: p.Department, Position: p.Position,
		ManagerName: "Руководитель " + p.Department + " (синтетика)", MentorName: "Наставник " + p.ID + " (синтетика)",
		RecruiterName: "Рекрутер Демо (синтетика)", StartDate: start, Shift: shift, ScenarioID: g.scenario,
	})
	if err != nil {
		log.Fatalf("новичок %s: %v", p.FullName, err)
	}
	e := c.Employee

	// Каждый третий из тех, кто ещё не вышел, не открыл приглашение.
	if day < 0 && i%3 == 0 {
		log.Printf("seed-demo: %s %-28s %s D%+d (не подключён)", p.ID, p.FullName, p.Site, day)
		return true
	}
	uid := int64(910000000 + i)
	consent := domain.ConsentGiven
	form := domain.AddressVy
	if p.AddressForm == domain.AddressTy {
		form = domain.AddressTy
	}
	connectedAt := civilDateAt(start.AddDate(0, 0, -3), site, 10, 5+g.rng.Intn(50))
	if connectedAt.After(g.now) {
		connectedAt = g.now.Add(-time.Hour)
	}
	e.MaxUserID, e.ChatID = &uid, &uid
	e.Connection, e.AddressForm, e.Consent, e.ConsentAt, e.LastActivityAt = domain.ConnConnected, &form, &consent, &connectedAt, &connectedAt

	flags, incident := spans(p)
	// Молчуны: лаконичные персоны после второй недели перестают отвечать.
	silentFrom := math.MaxInt
	if p.Laconic && day >= 12 {
		silentFrom = day - 5 - g.rng.Intn(4)
	}

	events, err := g.repos.Events.ByEmployee(ctx, e.ID)
	if err != nil {
		log.Fatalf("события: %v", err)
	}
	var lastActivity = connectedAt
	for k := range events {
		ev := &events[k]
		if ev.FireAt.After(g.now) {
			continue
		}
		at := ev.FireAt
		ev.Status, ev.SentAt, ev.MessageID = domain.EventSent, &at, "mid.demo."+ev.ID.String()[:8]
		sv := g.surveys[ev.Title]
		if ev.Kind != domain.KindSurvey || sv == nil {
			if at.After(lastActivity) && ev.DayOffset < silentFrom {
				lastActivity = at
			}
			g.save(ctx, ev)
			continue
		}
		skipRate := 0.10
		if p.Laconic {
			skipRate = 0.3
		}
		age := g.now.Sub(at)
		switch {
		case ev.DayOffset >= silentFrom || g.rng.Float64() < skipRate:
			if age > 4*24*time.Hour && g.rng.Intn(2) == 0 {
				ev.Status, ev.Answer = domain.EventSkipped, strp("не отвечен: напоминания исчерпаны")
			} // иначе остаётся sent — просрочка в обзоре
		case age < 3*time.Hour:
			// только что ушёл — ещё не ответили
		default:
			answeredAt := at.Add(time.Duration(10+g.rng.Intn(180)) * time.Minute)
			if answeredAt.After(g.now) {
				answeredAt = g.now.Add(-time.Minute)
			}
			g.answer(ctx, &e, ev, sv, p, flags, incident, answeredAt)
			if answeredAt.After(lastActivity) {
				lastActivity = answeredAt
			}
		}
		g.save(ctx, ev)
	}
	e.LastActivityAt = &lastActivity
	// Сигнал severity 3 уже начался — красная зона, как после regex или /flags в сообщении.
	for _, f := range flags {
		if f.severity >= 3 && f.onset <= day && day < f.resolved {
			e.Risk, e.RiskReason = domain.RiskRed, strp(signalLabel(g.tax, f.code)+" в сообщении боту")
		}
	}
	g.tickets(ctx, &e, flags, start, day, site)
	if err := g.repos.Employees.Save(ctx, &e); err != nil {
		log.Fatalf("новичок: %v", err)
	}
	log.Printf("seed-demo: %s %-28s %-8s D%+d %s", p.ID, p.FullName, p.Site, day, e.Risk)
	return true
}

func (g *gen) save(ctx context.Context, ev *domain.ScheduledEvent) {
	if err := g.repos.Events.Save(ctx, ev); err != nil {
		log.Fatalf("событие: %v", err)
	}
}

// civilDateAt — дата d на площадке в hh:mm.
func civilDateAt(d time.Time, site domain.Site, hh, mm int) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), hh, mm, 0, 0, site.Loc)
}

// answer — ответы на опрос по кривой траектории минус активные сигналы, коды сигналов — на
// первом вопросе (как после разметки причины).
func (g *gen) answer(ctx context.Context, e *domain.Employee, ev *domain.ScheduledEvent, sv *domain.Survey, p persona,
	flags []flagSpan, incident int, at time.Time) {
	d := ev.DayOffset
	base := curve(p.Trajectory, d, incident)
	var active []string
	dip := 0.0
	for _, f := range flags {
		if f.onset <= d && d < f.resolved {
			active = append(active, f.code)
			dip += map[int]float64{1: 1, 2: 3, 3: 5}[f.severity] * 0.6
		}
	}
	sort.Strings(active)
	st := &domain.SurveyState{Q: len(sv.Questions), Screen: domain.ScreenQuestion}
	var summary []string
	for q := range sv.Questions {
		x := base - dip + g.rng.NormFloat64()*0.7
		x = math.Max(1, math.Min(10, x))
		a := domain.SurveyAnswer{
			UUIDModel:  domain.UUIDModel{CreatedAt: at},
			EmployeeID: e.ID, EventID: ev.ID, QuestionNo: q + 1, Scale: sv.Scale, Flags: domain.StringList{},
		}
		var v int
		low := false
		switch sv.Scale {
		case domain.Scale10:
			v = int(math.Round(x))
			low = v < sv.Threshold
			summary = append(summary, fmt.Sprintf("%d/10", v))
		case domain.Scale5:
			v = int(math.Max(1, math.Min(5, math.Round(x/2))))
			low = sv.Threshold > 0 && v < sv.Threshold
			summary = append(summary, fmt.Sprintf("%d/5", v))
		case domain.ScaleChoice:
			v = 1
			if x < 8 {
				v = 2
			}
			if x <= 5 {
				v = 3
			}
			if opts := sv.Questions[q].Options; v <= len(opts) {
				summary = append(summary, opts[v-1])
			}
		default:
			v = 1
		}
		a.Value = &v
		r := domain.SurveyResult{Value: &v}
		if q == 0 && len(active) > 0 {
			a.Flags = active
		}
		if low && sv.Scale != domain.ScaleChoice {
			a.Reason = "Другое"
			if len(active) > 0 {
				if x, ok := reasonByFlag[active[0]]; ok {
					a.Reason = x
				}
			}
			r.Reason = a.Reason
			if e.Risk == domain.RiskGreen {
				e.Risk = domain.RiskYellow
				e.RiskReason = strp(fmt.Sprintf("оценка %s в «%s»", summary[len(summary)-1], ev.Title))
			}
		}
		if sv.Scale == domain.Scale10 || sv.Scale == domain.Scale5 {
			e.LastPulse = &v
		}
		st.Results = append(st.Results, r)
		if err := g.repos.Events.UpsertAnswer(ctx, &a); err != nil {
			log.Fatalf("ответ: %v", err)
		}
	}
	for _, c := range active {
		if c == "F5" || c == "F7" || c == "F9" || c == "F0" {
			e.Risk, e.RiskReason = domain.RiskRed, strp(signalLabel(g.tax, c)+" в ответе на опрос")
			break
		}
	}
	ev.Status, ev.Answer, ev.Survey = domain.EventAnswered, strp(strings.Join(summary, ", ")), st
}

// tickets — обращение на каждый сигнал severity ≥ 2, который уже начался. Решённые сигналы —
// решённые обращения; старые нерешённые — в работе у человека (SLA остановлен), свежие —
// открыты или назначены.
func (g *gen) tickets(ctx context.Context, e *domain.Employee, flags []flagSpan, start time.Time, day int, site domain.Site) {
	for _, f := range flags {
		if f.severity < 2 || f.onset > day || day < 0 {
			continue
		}
		cat, ok := g.tax.Get(f.code)
		if !ok {
			continue
		}
		created := civilDateAt(start.AddDate(0, 0, f.onset), site, 11+g.rng.Intn(5), g.rng.Intn(60))
		if created.After(g.now) {
			created = g.now.Add(-30 * time.Minute)
		}
		who := staff[g.rng.Intn(len(staff))]
		t := domain.Ticket{
			UUIDModel:  domain.UUIDModel{CreatedAt: created},
			EmployeeID: e.ID, Day: f.onset, Category: f.code, Priority: cat.Priority, Route: cat.Route,
			Restricted: cat.Restricted, Source: domain.TicketFromFlag,
			Summary: fmt.Sprintf("Синтетика: сигнал «%s» на D+%d. Текста новичка в сводке нет.", cat.Name, f.onset),
		}
		age := g.now.Sub(created)
		switch {
		case f.resolved <= day:
			resolved := created.Add(time.Duration(2+g.rng.Intn(40)) * time.Hour)
			t.Status, t.AssigneeName, t.ResolvedAt = domain.TicketResolved, &who, &resolved
		case age > 3*24*time.Hour:
			t.Status, t.AssigneeName = domain.TicketHumanActive, &who
		case g.rng.Intn(2) == 0:
			t.Status = domain.TicketOpen
			due := services.SLADue(created, cat.Priority, site)
			t.SLADueAt = &due
		default:
			t.Status, t.AssigneeName = domain.TicketAssigned, &who
			due := services.SLADue(created, cat.Priority, site)
			t.SLADueAt = &due
		}
		if err := g.repos.Tickets.Create(ctx, &t); err != nil {
			log.Fatalf("обращение: %v", err)
		}
		if t.Status != domain.TicketResolved && cat.Priority == taxonomy.P0 {
			e.Risk, e.RiskReason = domain.RiskRed, strp(signalLabel(g.tax, f.code)+": обращение #"+strconv.FormatInt(t.Number, 10))
		}
	}
}

// signalLabel — «сигнал F7»; F5 и F9 видят только HR-партнёр и СБ — без кода.
func signalLabel(tax *taxonomy.Taxonomy, code string) string {
	if c, ok := tax.Get(code); ok && c.Restricted {
		return "конфиденциальный сигнал"
	}
	return "сигнал " + code
}
