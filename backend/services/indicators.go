package services

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"main/domain"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// «Показатели» (GET /api/indicators, api_contract.md «Показатели»; ресерч
// 2026-10-24-overview-metrics.md): исходы адаптации за период для руководителя HR. Всё
// считает код из строк обзора; LLM нет, тексты новичков не читаются. Пороги, проценты и
// сравнения — здесь, кабинет только подписывает.
//
// Определения:
//   - когорта периода — сотрудники, чья дата выхода (D0) попала в период и уже наступила;
//   - «подключился до выхода» — согласие в боте (consent_at) раньше D0;
//   - «реакция» — последняя активность в боте (last_activity_at: ответ, нажатие, сообщение) или
//     ответ на опрос, что позже;
//   - «прошёл D0» — подключился до выхода и реагировал в день выхода или позже;
//   - «активен на D+N» (N = 30, 90) — D+N наступил, бот не остановлен и последняя реакция не
//     раньше D+(N−7): человек отвечал в последнюю неделю перед D+N или позже;
//   - «решение по ИС» — в карточке есть решение (keep | extend | part).

// Периоды «Показателей».
const (
	Period30d     = "30d"
	Period90d     = "90d"
	PeriodQuarter = "q"
)

// IndicatorPeriods — допустимые period.
var IndicatorPeriods = []string{Period30d, Period90d, PeriodQuarter}

// Пороги «Показателей».
const (
	// FunnelPercentMin — от скольких человек в когорте воронка показывает проценты (иначе только
	// абсолютные числа).
	FunnelPercentMin = 20
	// SiteNoiseN, SiteNoisePP — n < 30 и разница со средним меньше 5 п.п. — «в пределах колебаний».
	SiteNoiseN  = 30
	SiteNoisePP = 5.0
	// SiteFlagPP — отклонение от среднего от 10 п.п. — ссылка «что мешает на площадке».
	SiteFlagPP = 10.0
	// SameDeltaPP — сравнение с прошлым периодом: разница меньше 2 п.п. — «как было».
	SameDeltaPP = 2
	// ActiveLookbackDays — «активен на D+N»: реакция не раньше D+(N−7).
	ActiveLookbackDays = 7
	// SyntheticMark — так помечены синтетические сотрудники демо-потока (cmd/seed-demo).
	SyntheticMark = "(синтетика)"
)

// Ключи плиток исхода в порядке показа.
const (
	TileConnectedBeforeD0 = "connected_before_d0"
	TilePulse             = "pulse_response"
	TileBotClosed         = "bot_closed"
	TileFirstResponse     = "first_response"
	TileIPPassed          = "ip_passed"
	TileEarlyLeave        = "early_leave"
)

// IndicatorTileKeys — порядок плиток.
var IndicatorTileKeys = []string{TileConnectedBeforeD0, TilePulse, TileBotClosed, TileFirstResponse, TileIPPassed, TileEarlyLeave}

// Состояние плитки.
const (
	StatusOK       = "ok"
	StatusFew      = "few"       // n < 5: «мало данных»
	StatusLater90  = "later_90"  // «появится через 90 дней после запуска»
	StatusNoMarker = "no_marker" // «появится, когда HR начнёт отмечать увольнения»
)

// Сравнение с прошлым периодом и со средним.
const (
	DirUp   = "up"
	DirDown = "down"
	DirSame = "same"
	DirNew  = "new"
)

// Вердикт ячейки «Площадок».
const (
	VerdictHidden = "hidden" // группа меньше 5
	VerdictNoise  = "noise"  // в пределах колебаний
	VerdictSame   = "same"   // как в среднем
	VerdictAbove  = "above"
	VerdictBelow  = "below"
)

// Шаги воронки и причины потерь.
const (
	StepInvited    = "invited"
	StepConnected  = "connected_before_d0"
	StepPassedD0   = "passed_d0"
	StepActiveD30  = "active_d30"
	StepActiveD90  = "active_d90"
	StepIPDecision = "ip_decision"

	LossNotConnected  = "not_connected"  // так и не открыл приглашение
	LossConnectedLate = "connected_late" // подключился после выхода
	LossSilent        = "silent"         // не отвечает боту
	LossStopped       = "stopped"        // остановил бота
	LossNoDecision    = "no_decision"    // D+90 прошёл, решения нет
)

// Строки «Площадок» в фиксированном порядке.
const (
	SiteRowCohort    = "cohort"
	SiteRowConnected = "connected_before_d0"
	SiteRowPulse     = "pulse_response"
	SiteRowLowPulse  = "low_pulse"
	SiteRowTickets   = "tickets"
	SiteRowIP        = "ip_passed"
)

// Единицы значений.
const (
	UnitCount = "count"
	UnitPct   = "pct"
	UnitPer10 = "per10"
	UnitMin   = "min"
)

// IndicatorPeriod — окно [From, To) и прошлое окно [PrevFrom, PrevTo).
type IndicatorPeriod struct {
	Key              string
	From, To         time.Time
	PrevFrom, PrevTo time.Time
	Days             int
}

// IndicatorPeriodOf — окно периода на момент now в зоне loc. 30d и 90d — последние N
// календарных дней включая сегодня, прошлое окно — N дней до них; q — с начала квартала по
// сейчас, прошлое — весь предыдущий квартал.
func IndicatorPeriodOf(key string, now time.Time, loc *time.Location) IndicatorPeriod {
	today := civilDate(now.In(loc), loc)
	p := IndicatorPeriod{Key: key, To: now}
	switch key {
	case PeriodQuarter:
		qm := time.Month((int(today.Month())-1)/3*3 + 1)
		p.From = time.Date(today.Year(), qm, 1, 0, 0, 0, 0, loc)
		p.PrevFrom, p.PrevTo = p.From.AddDate(0, -3, 0), p.From
	default:
		n := 30
		if key == Period90d {
			n = 90
		} else {
			p.Key = Period30d
		}
		p.From = today.AddDate(0, 0, -(n - 1))
		p.PrevFrom, p.PrevTo = p.From.AddDate(0, 0, -n), p.From
	}
	p.Days = int(today.Add(12*time.Hour).Sub(p.From.Add(12*time.Hour)).Hours()/24) + 1
	return p
}

// Indicators — результат GET /api/indicators.
type Indicators struct {
	Site        string
	Period      IndicatorPeriod
	GeneratedAt time.Time
	Cohort      int // вышло за период
	CohortPrev  int
	Synthetic   int // из когорты — синтетические (демо-поток)
	Today       TodayLink
	Tiles       []IndicatorTile
	Funnel      Funnel
	Sites       SitesTable
	Topics      TopicsBlock
	HRLoad      HRLoad
}

// TodayLink — строка «Сегодня у команды: N дел, M срочных». Source: hrlead — «Сегодня»
// руководителя HR; actions — «Требуют действия» обзора (без входа).
type TodayLink struct {
	Total, Urgent int
	Source        string
}

// IndicatorTile — плитка исхода. Value nil — мало данных или ещё нет данных (Status).
// Delta — Value минус прошлый период (п.п. или минуты); Direction — словами.
type IndicatorTile struct {
	Key       string
	Status    string
	Unit      string
	Value     *int
	Num, N    int
	Prev      *int
	PrevN     int
	Delta     *int
	Direction string
	// Demo — число получено на синтетике демо-потока: исходы (ИС, уход) не настоящие.
	Demo bool
	// SLA — first_response: % первых ответов в SLA и по скольким обращениям.
	SLAPct *int
	SLAN   int
}

// Funnel — воронка когорты. Percent шагов — только при Cohort ≥ FunnelPercentMin; при
// Cohort < AnonymityMinGroup — только шаг «приглашены».
type Funnel struct {
	Cohort   int
	Percents bool
	Hidden   bool
	Steps    []FunnelStep
}

// FunnelStep — шаг: дошли (Count), ещё не дошли по времени (Pending), потеряны по причинам.
type FunnelStep struct {
	Key     string
	Count   int
	Percent *int
	Pending int
	Lost    []FunnelLoss
	Hidden  bool
}

type FunnelLoss struct {
	Reason string
	Count  int
}

// SitesTable — площадки × строки в фиксированном порядке, среднее — по всем площадкам.
type SitesTable struct {
	Sites   []string
	Cohorts map[string]int
	Rows    []SiteRow
	// Flagged — площадки, где хоть одна ячейка отклонилась от среднего на SiteFlagPP и больше.
	Flagged []string
}

type SiteRow struct {
	Key   string
	Unit  string
	Avg   *float64
	AvgN  int
	Cells []SiteCell
}

// SiteCell — Value nil при Hidden. Diff — в единицах строки (п.п. или «на 10»), DiffPP — в п.п.
type SiteCell struct {
	Site    string
	Value   *float64
	N       int
	Hidden  bool
	Diff    *float64
	DiffPP  *float64
	Verdict string
	Flag    bool
}

// TopicsBlock — тепловая карта за период и динамика тем к прошлому периоду.
type TopicsBlock struct {
	Heatmap Heatmap
	Trends  []TopicTrend
}

// TopicTrend — тема по всем видимым площадкам: разных сотрудников за период и за прошлый. nil —
// меньше порога (скрыто). Trend: up | down | new | same; "" — обе стороны скрыты.
type TopicTrend struct {
	Category string
	Name     string
	Count    *int
	Prev     *int
	Trend    string
}

// HRLoad — нагрузка HR по команде за период.
type HRLoad struct {
	Tickets   int
	Employees int      // в адаптации за период
	Per100    *float64 // обращений на 100 сотрудников; nil — Employees < 5
	BotClosed IndicatorTile
	FirstResp IndicatorTile
	// KBGaps — тем вопросов без ответа в базе знаний за период; nil — не считали.
	KBGaps      *int
	KBQuestions int
}

// IndicatorRowsAll — всё, из чего считаются «Показатели».
type IndicatorRowsAll struct {
	Overview *repositories.OverviewRows
	Extra    *repositories.IndicatorRows
}

// IndicatorsService — «Показатели».
type IndicatorsService struct {
	repos *repositories.Repos
	tax   *taxonomy.Taxonomy
	now   func() time.Time
}

// Indicators — сервис «Показателей» (поля в Services нет, как у обзора).
func (s *Services) Indicators() *IndicatorsService {
	o := s.Overview()
	return &IndicatorsService{repos: o.repos, tax: o.tax, now: o.now}
}

// Get — показатели по площадке (пусто — все) за период.
func (s *IndicatorsService) Get(ctx context.Context, site, period string) (*Indicators, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	now := s.now()
	p := IndicatorPeriodOf(period, now, domain.SiteOf(site).Loc)
	// Все площадки: таблица «Площадок» сравнивает со средним по компании при любом фильтре.
	rows, err := s.repos.Overview().Load(ctx, "", p.PrevFrom.AddDate(0, 0, -ActiveLookbackDays))
	if err != nil {
		return nil, err
	}
	extra, err := s.repos.Overview().Indicators(ctx, p.PrevFrom)
	if err != nil {
		return nil, err
	}
	ind := BuildIndicators(IndicatorRowsAll{Overview: rows, Extra: extra}, site, p, now, s.tax)
	return &ind, nil
}

// ── Расчёт ──────────────────────────────────────────────────────────────────

// IsSynthetic — сотрудник демо-потока.
func IsSynthetic(e domain.Employee) bool { return strings.Contains(e.FullName, SyntheticMark) }

// indPerson — сотрудник и всё, что о нём нужно «Показателям».
type indPerson struct {
	e        domain.Employee
	site     domain.Site
	d0       time.Time
	day      int
	lastReac *time.Time
}

func (p *indPerson) dayAt(n int) time.Time { return p.d0.AddDate(0, 0, n) }

func (p *indPerson) connectedBeforeD0() bool {
	return p.e.Connection != domain.ConnInvited && p.e.ConsentAt != nil && p.e.ConsentAt.Before(p.d0)
}

// reactedSince — была реакция не раньше t.
func (p *indPerson) reactedSince(t time.Time) bool { return p.lastReac != nil && !p.lastReac.Before(t) }

// activeAt — «активен на D+n» (см. шапку файла).
func (p *indPerson) activeAt(n int) bool {
	return p.e.Connection != domain.ConnStopped && p.reactedSince(p.dayAt(n-ActiveLookbackDays))
}

type indCtx struct {
	people []*indPerson
	byID   map[uuid.UUID]*indPerson
	rows   *repositories.OverviewRows
	extra  *repositories.IndicatorRows
	now    time.Time
	today  func(p *indPerson) time.Time
}

func newIndCtx(all IndicatorRowsAll, now time.Time) *indCtx {
	rows, extra := all.Overview, all.Extra
	if rows == nil {
		rows = &repositories.OverviewRows{}
	}
	if extra == nil {
		extra = &repositories.IndicatorRows{FirstStaff: map[uuid.UUID]time.Time{}}
	}
	c := &indCtx{byID: map[uuid.UUID]*indPerson{}, rows: rows, extra: extra, now: now}
	for _, e := range rows.Employees {
		if e.IsTest {
			continue
		}
		site := domain.SiteOf(e.Site)
		p := &indPerson{e: e, site: site, d0: civilDate(e.StartDate, site.Loc), day: DayNumber(e.StartDate, now, site)}
		if e.LastActivityAt != nil {
			t := *e.LastActivityAt
			p.lastReac = &t
		}
		c.people = append(c.people, p)
		c.byID[e.ID] = p
	}
	for _, a := range rows.Answers {
		p := c.byID[a.EmployeeID]
		if p == nil || a.IsTest {
			continue
		}
		if p.lastReac == nil || a.CreatedAt.After(*p.lastReac) {
			t := a.CreatedAt
			p.lastReac = &t
		}
	}
	return c
}

// inCohort — D0 в окне [from, to) и уже наступил.
func inCohort(p *indPerson, from, to time.Time) bool {
	return !p.d0.Before(civilDate(from.In(p.site.Loc), p.site.Loc)) && p.d0.Before(to) && p.day >= 0
}

// winStats — числа одного окна по одной группе сотрудников.
type winStats struct {
	cohort, synthetic        int
	connBefore               int
	pulseSent, pulseAnswered int
	respondents, lowResp     int
	inAdapt, tickets         int
	qAll, qClosed            int
	frMins                   []float64
	slaIn, slaN              int
	ipDecided, ipKeep, ipSyn int
	scopeSyn, scope          int
}

// stats — окно [from, to) по сотрудникам, для которых keep(p) == true.
func (c *indCtx) stats(from, to time.Time, keep func(*indPerson) bool) winStats {
	var w winStats
	in := map[uuid.UUID]bool{}
	for _, p := range c.people {
		if !keep(p) {
			continue
		}
		in[p.e.ID] = true
		w.scope++
		if IsSynthetic(p.e) {
			w.scopeSyn++
		}
		if inCohort(p, from, to) {
			w.cohort++
			if IsSynthetic(p.e) {
				w.synthetic++
			}
			if p.connectedBeforeD0() {
				w.connBefore++
			}
		}
		// В адаптации за окно: вышел до конца окна, D+90 — не раньше начала.
		if p.d0.Before(to) && !p.dayAt(90).Before(civilDate(from.In(p.site.Loc), p.site.Loc)) {
			w.inAdapt++
		}
		// ИС: D+90 пришёлся на окно и уже наступил.
		d90 := p.dayAt(90)
		if !d90.Before(civilDate(from.In(p.site.Loc), p.site.Loc)) && d90.Before(to) && p.day >= 90 && p.e.IPDecision != nil {
			w.ipDecided++
			if *p.e.IPDecision == domain.IPKeep {
				w.ipKeep++
			}
			if IsSynthetic(p.e) {
				w.ipSyn++
			}
		}
	}
	inWin := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	for _, ev := range c.rows.Events {
		if !in[ev.EmployeeID] || ev.IsTest || ev.Kind != domain.KindSurvey || ev.SentAt == nil || !inWin(*ev.SentAt) {
			continue
		}
		w.pulseSent++
		if ev.Status == domain.EventAnswered {
			w.pulseAnswered++
		}
	}
	// Низкий пульс: среднее по опросу на шкале 10 не выше LowPulse; считаются люди.
	type agg struct {
		emp    uuid.UUID
		sum    float64
		n      int
		inside bool
	}
	byEvent := map[uuid.UUID]*agg{}
	for _, a := range c.rows.Answers {
		if !in[a.EmployeeID] || a.IsTest || !inWin(a.CreatedAt) {
			continue
		}
		v, ok := pulseValue(a)
		if !ok {
			continue
		}
		g := byEvent[a.EventID]
		if g == nil {
			g = &agg{emp: a.EmployeeID}
			byEvent[a.EventID] = g
		}
		g.sum += v
		g.n++
	}
	resp, low := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, g := range byEvent {
		resp[g.emp] = true
		if g.sum/float64(g.n) <= LowPulse {
			low[g.emp] = true
		}
	}
	w.respondents, w.lowResp = len(resp), len(low)
	for _, t := range c.rows.Tickets {
		if !in[t.EmployeeID] || t.IsTest || !inWin(t.CreatedAt) {
			continue
		}
		w.tickets++
		p := c.byID[t.EmployeeID]
		first, answered := c.extra.FirstStaff[t.ID]
		if answered {
			w.frMins = append(w.frMins, workMinutesBetween(t.CreatedAt, first, p.site))
		}
		if t.SLADueAt != nil {
			switch {
			case answered:
				w.slaN++
				if !first.After(*t.SLADueAt) {
					w.slaIn++
				}
			case t.Status != domain.TicketResolved && c.now.After(*t.SLADueAt):
				w.slaN++ // без ответа, срок прошёл — вне SLA
			}
		}
	}
	for _, q := range c.extra.Questions {
		if !in[q.EmployeeID] || q.IsTest || !inWin(q.CreatedAt) {
			continue
		}
		w.qAll++
		if botClosed(q) {
			w.qClosed++
		}
	}
	return w
}

// botClosed — вопрос закрыт ботом без человека: ответ rag, без «Не то», без передачи человеку.
func botClosed(q repositories.IndicatorQuestion) bool {
	return q.Kind == domain.QuestionAnswer && q.NotThat == 0 && q.HumanAt == nil && q.TicketID == nil
}

// workMinutesBetween — рабочие минуты площадки между a и b (пн–чт 9–18, пт 9–17, без
// выходных и праздников).
func workMinutesBetween(a, b time.Time, site domain.Site) float64 {
	if !b.After(a) {
		return 0
	}
	a, b = a.In(site.Loc), b.In(site.Loc)
	total := 0.0
	for day := civilDate(a, site.Loc); !day.After(b); day = day.AddDate(0, 0, 1) {
		if !site.IsWorkDate(day) {
			continue
		}
		lo := atMinutes(day, site.DayStart[day.Weekday()], site.Loc)
		hi := atMinutes(day, site.DayEnd[day.Weekday()], site.Loc)
		if a.After(lo) {
			lo = a
		}
		if b.Before(hi) {
			hi = b
		}
		if hi.After(lo) {
			total += hi.Sub(lo).Minutes()
		}
	}
	return total
}

func median(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := s[len(s)/2]
	if len(s)%2 == 0 {
		m = (s[len(s)/2-1] + s[len(s)/2]) / 2
	}
	return &m
}

func pctOf(num, n int) *int {
	if n == 0 {
		return nil
	}
	v := int(math.Round(float64(num) * 100 / float64(n)))
	return &v
}

// compare — сравнение с прошлым периодом: delta = cur − prev, «как было» при |delta| < same.
func compare(cur, prev *int, same int) (*int, string) {
	if cur == nil || prev == nil {
		return nil, ""
	}
	d := *cur - *prev
	switch {
	case abs(d) < same:
		return &d, DirSame
	case d > 0:
		return &d, DirUp
	}
	return &d, DirDown
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// rateTile — плитка-доля: n < 5 — «мало данных», прошлый период — только от 5.
func rateTile(key string, num, n, prevNum, prevN int) IndicatorTile {
	t := IndicatorTile{Key: key, Unit: UnitPct, Num: num, N: n, PrevN: prevN, Status: StatusOK}
	if n < AnonymityMinGroup {
		t.Status = StatusFew
		return t
	}
	t.Value = pctOf(num, n)
	if prevN >= AnonymityMinGroup {
		t.Prev = pctOf(prevNum, prevN)
	}
	t.Delta, t.Direction = compare(t.Value, t.Prev, SameDeltaPP)
	return t
}

// firstResponseTile — медиана первого ответа в рабочих минутах и % в SLA.
func firstResponseTile(cur, prev winStats) IndicatorTile {
	t := IndicatorTile{Key: TileFirstResponse, Unit: UnitMin, N: len(cur.frMins), PrevN: len(prev.frMins), SLAN: cur.slaN, Status: StatusOK}
	if t.N < AnonymityMinGroup {
		t.Status = StatusFew
	} else {
		v := int(math.Round(*median(cur.frMins)))
		t.Value = &v
		if t.PrevN >= AnonymityMinGroup {
			pv := int(math.Round(*median(prev.frMins)))
			t.Prev = &pv
			// «Как было» — разница меньше 10 % прошлого значения или 5 минут.
			same := int(math.Max(5, math.Round(float64(pv)*0.1)))
			t.Delta, t.Direction = compare(t.Value, t.Prev, same)
		}
	}
	if cur.slaN >= AnonymityMinGroup {
		t.SLAPct = pctOf(cur.slaIn, cur.slaN)
	}
	return t
}

// BuildIndicators — все «Показатели» из строк. Чистая функция: её проверяют тесты без базы.
func BuildIndicators(all IndicatorRowsAll, site string, p IndicatorPeriod, now time.Time, tax *taxonomy.Taxonomy) Indicators {
	c := newIndCtx(all, now)
	ind := Indicators{Site: site, Period: p, GeneratedAt: now}
	scope := func(x *indPerson) bool { return site == "" || x.e.Site == site }
	cur := c.stats(p.From, p.To, scope)
	prev := c.stats(p.PrevFrom, p.PrevTo, scope)
	ind.Cohort, ind.CohortPrev, ind.Synthetic = cur.cohort, prev.cohort, cur.synthetic

	// ── Плитки ──
	conn := rateTile(TileConnectedBeforeD0, cur.connBefore, cur.cohort, prev.connBefore, prev.cohort)
	pulse := rateTile(TilePulse, cur.pulseAnswered, cur.pulseSent, prev.pulseAnswered, prev.pulseSent)
	bot := rateTile(TileBotClosed, cur.qClosed, cur.qAll, prev.qClosed, prev.qAll)
	fr := firstResponseTile(cur, prev)
	ip := rateTile(TileIPPassed, cur.ipKeep, cur.ipDecided, prev.ipKeep, prev.ipDecided)
	if cur.ipDecided == 0 {
		ip.Status = StatusLater90
	}
	// Исходы на синтетике — не настоящие: «демо», если синтетика — большинство выборки (или
	// всего потока, когда решений ещё нет).
	ip.Demo = majority(cur.ipSyn, cur.ipDecided) || (cur.ipDecided == 0 && majority(cur.scopeSyn, cur.scope))
	// Ранний уход: признака увольнения с датой в карточке нет — плитка ждёт, пока HR начнёт
	// отмечать увольнения (схему ради неё не меняем).
	leave := IndicatorTile{Key: TileEarlyLeave, Unit: UnitPct, Status: StatusNoMarker, N: cur.cohort, Demo: majority(cur.scopeSyn, cur.scope)}
	ind.Tiles = []IndicatorTile{conn, pulse, bot, fr, ip, leave}

	ind.Funnel = buildFunnel(c, p, scope)
	ind.Sites = buildSitesTable(c, p)
	ind.Topics = buildTopics(c, site, p, tax)

	ind.HRLoad = HRLoad{Tickets: cur.tickets, Employees: cur.inAdapt, BotClosed: bot, FirstResp: fr}
	if cur.inAdapt >= AnonymityMinGroup {
		v := math.Round(float64(cur.tickets)*1000/float64(cur.inAdapt)) / 10
		ind.HRLoad.Per100 = &v
	}

	// Строка «Сегодня» без входа — из «Требуют действия» обзора (handler подменит на «Сегодня»
	// руководителя HR, если он вошёл).
	ov := BuildOverview(filterRows(c.rows, site), site, now, tax)
	ind.Today = TodayLink{Source: "actions"}
	for _, a := range ov.Actions {
		ind.Today.Total += a.Count
		if a.Kind == ActionTicketsSLAOverdue || a.Kind == ActionRedZones {
			ind.Today.Urgent += a.Count
		}
	}
	return ind
}

func majority(part, n int) bool { return n > 0 && part*2 >= n }

// filterRows — строки обзора одной площадки (пусто — все).
func filterRows(rows *repositories.OverviewRows, site string) *repositories.OverviewRows {
	if site == "" {
		return rows
	}
	out := &repositories.OverviewRows{}
	keep := map[uuid.UUID]bool{}
	for _, e := range rows.Employees {
		if e.Site == site {
			out.Employees = append(out.Employees, e)
			keep[e.ID] = true
		}
	}
	for _, x := range rows.Events {
		if keep[x.EmployeeID] {
			out.Events = append(out.Events, x)
		}
	}
	for _, x := range rows.Answers {
		if keep[x.EmployeeID] {
			out.Answers = append(out.Answers, x)
		}
	}
	for _, x := range rows.Tickets {
		if keep[x.EmployeeID] {
			out.Tickets = append(out.Tickets, x)
		}
	}
	for _, x := range rows.Questions {
		if keep[x.EmployeeID] {
			out.Questions = append(out.Questions, x)
		}
	}
	return out
}

// buildFunnel — воронка когорты периода. Каждый шаг — подмножество предыдущего:
// count + pending + потери = count предыдущего шага.
func buildFunnel(c *indCtx, p IndicatorPeriod, scope func(*indPerson) bool) Funnel {
	var cohort []*indPerson
	for _, x := range c.people {
		if scope(x) && inCohort(x, p.From, p.To) {
			cohort = append(cohort, x)
		}
	}
	f := Funnel{Cohort: len(cohort), Percents: len(cohort) >= FunnelPercentMin, Hidden: len(cohort) < AnonymityMinGroup}
	type stepFn func(x *indPerson) (passed, pending bool, loss string)
	steps := []struct {
		key string
		fn  stepFn
	}{
		{StepConnected, func(x *indPerson) (bool, bool, string) {
			switch {
			case x.connectedBeforeD0():
				return true, false, ""
			case x.e.Connection == domain.ConnInvited:
				return false, false, LossNotConnected
			}
			return false, false, LossConnectedLate
		}},
		{StepPassedD0, func(x *indPerson) (bool, bool, string) {
			if x.reactedSince(x.d0) {
				return true, false, ""
			}
			if x.day < 1 {
				return false, true, ""
			}
			return false, false, lossOf(x)
		}},
		{StepActiveD30, activeStep(30)},
		{StepActiveD90, activeStep(90)},
		{StepIPDecision, func(x *indPerson) (bool, bool, string) {
			switch {
			case x.e.IPDecision != nil:
				return true, false, ""
			case x.day < 90:
				return false, true, ""
			}
			return false, false, LossNoDecision
		}},
	}
	percent := func(n int) *int {
		if !f.Percents {
			return nil
		}
		return pctOf(n, f.Cohort)
	}
	f.Steps = append(f.Steps, FunnelStep{Key: StepInvited, Count: f.Cohort, Percent: percent(f.Cohort), Lost: []FunnelLoss{}})
	alive := cohort
	for _, st := range steps {
		s := FunnelStep{Key: st.key, Lost: []FunnelLoss{}, Hidden: f.Hidden}
		lost := map[string]int{}
		var next []*indPerson
		for _, x := range alive {
			ok, pending, loss := st.fn(x)
			switch {
			case ok:
				next = append(next, x)
			case pending:
				s.Pending++
			default:
				lost[loss]++
			}
		}
		s.Count = len(next)
		for _, r := range []string{LossNotConnected, LossConnectedLate, LossSilent, LossStopped, LossNoDecision} {
			if lost[r] > 0 {
				s.Lost = append(s.Lost, FunnelLoss{Reason: r, Count: lost[r]})
			}
		}
		if f.Hidden {
			s.Count, s.Pending, s.Lost = 0, 0, []FunnelLoss{}
		} else {
			s.Percent = percent(s.Count)
		}
		f.Steps = append(f.Steps, s)
		alive = next
	}
	return f
}

// activeStep — шаг «активен на D+n»: D+n не наступил — ещё не дошёл.
func activeStep(n int) func(x *indPerson) (bool, bool, string) {
	return func(x *indPerson) (bool, bool, string) {
		if x.day < n {
			return false, true, ""
		}
		if x.activeAt(n) {
			return true, false, ""
		}
		return false, false, lossOf(x)
	}
}

func lossOf(x *indPerson) string {
	if x.e.Connection == domain.ConnStopped {
		return LossStopped
	}
	return LossSilent
}

// siteMetric — числитель и знаменатель строки «Площадок» из окна.
func siteMetric(key string, w winStats) (num, n int) {
	switch key {
	case SiteRowConnected:
		return w.connBefore, w.cohort
	case SiteRowPulse:
		return w.pulseAnswered, w.pulseSent
	case SiteRowLowPulse:
		return w.lowResp, w.respondents
	case SiteRowTickets:
		return w.tickets, w.inAdapt
	case SiteRowIP:
		return w.ipKeep, w.ipDecided
	}
	return w.cohort, w.cohort
}

// buildSitesTable — площадки × строки; среднее — по всем площадкам вместе (не среднее средних).
// Без сортировки и рейтинга: порядок площадок — domain.SiteCodes.
func buildSitesTable(c *indCtx, p IndicatorPeriod) SitesTable {
	t := SitesTable{Sites: domain.SiteCodes, Cohorts: map[string]int{}, Flagged: []string{}}
	all := c.stats(p.From, p.To, func(*indPerson) bool { return true })
	per := map[string]winStats{}
	for _, s := range domain.SiteCodes {
		site := s
		per[s] = c.stats(p.From, p.To, func(x *indPerson) bool { return x.e.Site == site })
		t.Cohorts[s] = per[s].cohort
	}
	flagged := map[string]bool{}
	for _, key := range []string{SiteRowCohort, SiteRowConnected, SiteRowPulse, SiteRowLowPulse, SiteRowTickets, SiteRowIP} {
		row := SiteRow{Key: key, Unit: UnitPct}
		scale := 100.0
		switch key {
		case SiteRowCohort:
			row.Unit, scale = UnitCount, 1
		case SiteRowLowPulse, SiteRowTickets:
			row.Unit, scale = UnitPer10, 10
		}
		num, n := siteMetric(key, all)
		row.AvgN = n
		var avgShare *float64
		if key == SiteRowCohort {
			v := float64(all.cohort)
			row.Avg = &v
		} else if n >= AnonymityMinGroup {
			sh := float64(num) / float64(n)
			avgShare = &sh
			v := roundUnit(sh*scale, row.Unit)
			row.Avg = &v
		}
		for _, s := range domain.SiteCodes {
			w := per[s]
			num, n := siteMetric(key, w)
			cell := SiteCell{Site: s, N: n}
			if key == SiteRowCohort {
				v := float64(w.cohort)
				cell.Value, cell.Verdict = &v, ""
				row.Cells = append(row.Cells, cell)
				continue
			}
			// Группа площадки меньше 5 — скрыта вся колонка; ячейка с n < 5 — тоже.
			if w.inAdapt < AnonymityMinGroup || n < AnonymityMinGroup {
				cell.Hidden, cell.Verdict = true, VerdictHidden
				row.Cells = append(row.Cells, cell)
				continue
			}
			sh := float64(num) / float64(n)
			v := roundUnit(sh*scale, row.Unit)
			cell.Value = &v
			if avgShare != nil {
				pp := math.Round((sh-*avgShare)*1000) / 10
				diff := roundUnit((sh-*avgShare)*scale, row.Unit)
				cell.DiffPP, cell.Diff = &pp, &diff
				cell.Verdict = siteVerdict(pp, n)
				if math.Abs(pp) >= SiteFlagPP {
					cell.Flag = true
					flagged[s] = true
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		t.Rows = append(t.Rows, row)
	}
	for _, s := range domain.SiteCodes {
		if flagged[s] {
			t.Flagged = append(t.Flagged, s)
		}
	}
	return t
}

// siteVerdict — словами: n < 30 и |разница| < 5 п.п. — «в пределах колебаний»; |разница| < 1 п.п.
// — «как в среднем»; иначе выше или ниже.
func siteVerdict(pp float64, n int) string {
	switch {
	case n < SiteNoiseN && math.Abs(pp) < SiteNoisePP:
		return VerdictNoise
	case math.Abs(pp) < 1:
		return VerdictSame
	case pp > 0:
		return VerdictAbove
	}
	return VerdictBelow
}

func roundUnit(v float64, unit string) float64 {
	if unit == UnitPer10 {
		return math.Round(v*10) / 10
	}
	return math.Round(v)
}

// buildTopics — тепловая карта за период и динамика тем по всем видимым площадкам.
func buildTopics(c *indCtx, site string, p IndicatorPeriod, tax *taxonomy.Taxonomy) TopicsBlock {
	siteOf := people2site(c.rows)
	b := TopicsBlock{Heatmap: buildHeatmapWindow(c.rows, siteOf, site, p.From, p.To, tax)}
	b.Heatmap.PeriodDays = p.Days
	cur := topicCounts(c.rows, siteOf, site, p.From, p.To)
	prev := topicCounts(c.rows, siteOf, site, p.PrevFrom, p.PrevTo)
	for _, cat := range HeatmapCategories {
		tt := TopicTrend{Category: cat, Name: categoryName(cat, tax)}
		cn, pn := cur[cat], prev[cat]
		if cn >= AnonymityMinGroup {
			v := cn
			tt.Count = &v
		}
		if pn >= AnonymityMinGroup {
			v := pn
			tt.Prev = &v
		}
		tt.Trend = topicTrend(tt.Count, tt.Prev)
		b.Trends = append(b.Trends, tt)
	}
	return b
}

// topicTrend — динамика темы. Обе стороны скрыты — ""; сейчас ≥ 5, раньше скрыто или 0 —
// «новое»; сейчас скрыто, раньше было — «падает»; разница не больше 1 — «как было».
func topicTrend(cur, prev *int) string {
	switch {
	case cur == nil && prev == nil:
		return ""
	case cur != nil && prev == nil:
		return DirNew
	case cur == nil:
		return DirDown
	}
	d := *cur - *prev
	switch {
	case abs(d) <= 1:
		return DirSame
	case d > 0:
		return DirUp
	}
	return DirDown
}

// topicCounts — тема → разных сотрудников, поднявших её в окне [from, to) (ответ опроса с
// флагом или обращение), по площадке (пусто — все).
func topicCounts(rows *repositories.OverviewRows, siteOf map[uuid.UUID]string, site string, from, to time.Time) map[string]int {
	seen := map[string]map[uuid.UUID]bool{}
	mark := func(emp uuid.UUID, codes ...string) {
		s, ok := siteOf[emp]
		if !ok || (site != "" && s != site) {
			return
		}
		for _, c := range codes {
			if seen[c] == nil {
				seen[c] = map[uuid.UUID]bool{}
			}
			seen[c][emp] = true
		}
	}
	inWin := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	for _, a := range rows.Answers {
		if !a.IsTest && inWin(a.CreatedAt) {
			mark(a.EmployeeID, a.Flags...)
		}
	}
	for _, t := range rows.Tickets {
		if !t.IsTest && inWin(t.CreatedAt) {
			mark(t.EmployeeID, t.Category)
		}
	}
	out := map[string]int{}
	for c, m := range seen {
		out[c] = len(m)
	}
	return out
}

// buildHeatmapWindow — тепловая карта обзора по окну [from, to).
func buildHeatmapWindow(rows *repositories.OverviewRows, siteOf map[uuid.UUID]string, site string, from, to time.Time, tax *taxonomy.Taxonomy) Heatmap {
	clip := &repositories.OverviewRows{Employees: rows.Employees}
	for _, a := range rows.Answers {
		if a.CreatedAt.Before(to) {
			clip.Answers = append(clip.Answers, a)
		}
	}
	for _, t := range rows.Tickets {
		if t.CreatedAt.Before(to) {
			clip.Tickets = append(clip.Tickets, t)
		}
	}
	return buildHeatmap(clip, siteOf, site, from, tax)
}
