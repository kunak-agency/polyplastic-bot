package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"main/config"
	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/pkg/logger"
	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Еженедельный дайджест «Настроения в коллективе новичков» (кейс, роль 04; UX 3.7; ресерч,
// блок C; agency/prompts/digest.md). Код считает все цифры, таблицы и темы; LLM пишет только
// связный текст по агрегатам. В MAX уходит одно сообщение (≤ 4000 символов) с кнопкой-ссылкой
// «Открыть аналитику»; в кабинете — тот же текст и таблицы.

// Пределы и расписание.
const (
	// DigestMaxText — потолок сообщения MAX (лимит платформы 4000 символов).
	DigestMaxText = 4000
	// DigestSendWeekday, DigestSendHour — понедельник 9:00 по Москве.
	DigestSendWeekday = time.Monday
	DigestSendHour    = 9
	// digestMarkerKey — какая неделя уже отправлена (marker_states): после рестарта в понедельник
	// второй раз не шлём.
	digestMarkerKey = "digest_sent_week"
	// digestCheckEvery — как часто планировщик дайджеста смотрит на часы.
	digestCheckEvery = time.Minute
)

// DigestOptions — получатели и ссылка на кабинет (config.Digest).
type DigestOptions struct {
	UserIDs    []int64 // MAX_DIGEST_USER_IDS
	CabinetURL string  // CABINET_URL: база ссылки «Открыть аналитику»
	LLM        config.LLM
}

// DigestService — дайджест.
type DigestService struct {
	repos *repositories.Repos
	tax   *taxonomy.Taxonomy
	max   max.Client
	llm   DigestLLM
	opts  DigestOptions
	// risk — режим зон риска, как у обзора (FEATURE_RISK_V2): цифры дайджеста и обзора совпадают.
	risk OverviewOptions
	now  func() time.Time
}

// digestTextCache — текст LLM по хэшу агрегатов: кабинет не ждёт модель на каждом открытии.
var digestTextCache sync.Map

// Digest — сервис дайджеста (по образцу Overview: без поля в Services).
func (s *Services) Digest() *DigestService {
	o := s.Overview()
	d := &DigestService{repos: s.repos, tax: o.tax, now: o.now, opts: s.digestOpts, risk: o.opts}
	if s.ext != nil {
		d.max = s.ext.Max
	}
	d.llm = newDigestLLM(d.opts.LLM)
	return d
}

// Digest — результат GET /api/digest.
type Digest struct {
	Week        string // 2026-W43
	From, To    time.Time
	Site        string
	GeneratedAt time.Time
	Partial     bool // неделя ещё идёт
	Totals      DigestTotals
	Prev        DigestTotals
	Themes      []DigestThemeDelta
	Hidden      int // тем меньше 5 человек
	// Confidential — новичков с F5/F9; nil — меньше 5 (тогда только ConfidentialAny).
	Confidential    *int
	ConfidentialAny bool
	Risks           DigestRisks
	Sites           []DigestSiteRow
	Probation       int  // решений по ИС в неделю после отчётной
	ProbationNow    bool // эта неделя после отчётной — текущая («на этой неделе»)
	// Tone — тон текстовых ответов за отчётную неделю; ToneTrend — 4 недели по площадкам,
	// последняя — отчётная (overview_tone.go). Числа считает код, строку в MAX тоже пишет код.
	Tone      ToneCell
	ToneTrend ToneTrend
	// Done — «Сделано по темам» из «что сделано» при решении обращений за неделю (#31).
	Done       []DigestDoneTheme
	Aggregates DigestAggregates
	Text       DigestText
	MaxText    string
	Link       string
}

// DigestTotals — сводка окна.
type DigestTotals struct {
	OnPulse         int // новичков, которым в окне уходил опрос
	Answered        int // из них ответили хоть на один
	ResponseRatePct *int
	WAvg            *int     // индекс W по шкалам; nil — меньше 5 ответивших
	PulseAvg        *float64 // средний пульс на шкале 10; nil — меньше 5 ответивших
	Respondents     int      // ответивших на шкалы
	Problems        int      // новичков с сигналом F0–F10
	Critical        int      // из них с критическим кодом
}

// DigestThemeDelta — тема и её динамика к прошлой неделе.
type DigestThemeDelta struct {
	DigestTheme
	PrevCount  *int // nil — темы не было или PrevHidden
	PrevHidden bool // на прошлой неделе было 1–4 человека
	Delta      *int
}

// DigestRisks — зоны риска на конец окна (как в обзоре).
type DigestRisks struct {
	Red, Yellow int
	Items       []RiskZone
}

// DigestSiteRow — по площадкам.
type DigestSiteRow struct {
	Site       string
	OnPulse    int
	Answered   int
	WAvg       *int
	Problems   int
	TopTheme   *string
	Respondent int
}

// ParseISOWeek — «2026-W43» → понедельник 00:00 по Москве.
func ParseISOWeek(s string) (time.Time, error) {
	var y, w int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d-W%d", &y, &w); err != nil || w < 1 || w > 53 || y < 2000 || y > 2100 {
		return time.Time{}, fmt.Errorf("неделя в формате YYYY-Www")
	}
	loc := domain.SiteOf("").Loc
	jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, loc)
	mon := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+7*(w-1))
	if yy, ww := mon.ISOWeek(); yy != y || ww != w {
		return time.Time{}, fmt.Errorf("в %d году нет недели %d", y, w)
	}
	return mon, nil
}

// ISOWeekOf — «2026-W43» для момента t (по Москве).
func ISOWeekOf(t time.Time) string {
	y, w := t.In(domain.SiteOf("").Loc).ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// CurrentWeek — неделя по умолчанию в кабинете: текущая (идёт, Partial).
func (s *DigestService) CurrentWeek() string { return ISOWeekOf(s.now()) }

// LastFullWeek — прошедшая неделя: её дайджест уходит в понедельник.
func (s *DigestService) LastFullWeek() string { return ISOWeekOf(s.now().AddDate(0, 0, -7)) }

// Get — дайджест недели (week пусто — текущая) по площадке (пусто — все).
func (s *DigestService) Get(ctx context.Context, week, site string) (*Digest, error) {
	if !s.repos.Enabled() {
		return nil, repositories.ErrNoDatabase
	}
	if week == "" {
		week = s.CurrentWeek()
	}
	from, err := ParseISOWeek(week)
	if err != nil {
		return nil, errs.NewValidationError(err.Error()).WithDetails(errs.FieldError{Field: "week", Code: errs.CodeInvalidFormat, Message: err.Error()})
	}
	now := s.now()
	if from.After(now) {
		return nil, errs.NewValidationError("Неделя ещё не началась").WithDetails(errs.FieldError{Field: "week", Code: errs.CodeOutOfRange, Message: "Неделя ещё не началась"})
	}
	to := from.AddDate(0, 0, 7)
	prevFrom := from.AddDate(0, 0, -7)
	rows, err := s.repos.Overview().Load(ctx, site, prevFrom.AddDate(0, 0, -28))
	if err != nil {
		return nil, err
	}
	texts, err := s.repos.Overview().DigestTexts(ctx, site, prevFrom, to)
	if err != nil {
		return nil, err
	}
	done, err := s.repos.Overview().DigestDone(ctx, site, from, to)
	if err != nil {
		return nil, err
	}
	d := BuildDigest(rows, texts, site, from, now, s.tax, s.risk)
	d.Done = BuildDigestDone(done, s.tax)
	d.Link = s.link(week, site)
	d.Text = s.text(ctx, d.Aggregates)
	d.MaxText = DigestMaxMessage(d)
	return d, nil
}

func (s *DigestService) link(week, site string) string {
	base := strings.TrimRight(s.opts.CabinetURL, "/")
	if base == "" {
		base = config.DefaultCabinetURL
	}
	u := base + "/analytics?week=" + week
	if site != "" {
		u += "&site=" + site
	}
	return u
}

// text — LLM по агрегатам с кэшем по их хэшу (те же числа — тот же текст).
func (s *DigestService) text(ctx context.Context, agg DigestAggregates) DigestText {
	b, _ := json.Marshal(agg)
	sum := sha256.Sum256(append(b, []byte(digestPromptVersion)...))
	key := hex.EncodeToString(sum[:])
	if v, ok := digestTextCache.Load(key); ok {
		return v.(DigestText)
	}
	// Кабинет ждёт не дольше 30 с: два ответа модели или шаблон.
	lctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	txt := GenerateDigestText(lctx, s.llm, agg)
	if txt.Source == DigestTextLLM {
		digestTextCache.Store(key, txt)
	}
	return txt
}

// BuildDigest — все цифры дайджеста из строк. Чистая функция (тесты без базы); текст LLM
// подставляет Get, здесь — шаблон. opts — режим зон риска, как у обзора (без него — прежние правила).
func BuildDigest(rows *repositories.OverviewRows, texts []repositories.DigestText, site string, from, now time.Time, tax *taxonomy.Taxonomy, opts ...OverviewOptions) *Digest {
	if rows == nil {
		rows = &repositories.OverviewRows{}
	}
	to := from.AddDate(0, 0, 7)
	prevFrom := from.AddDate(0, 0, -7)
	d := &Digest{Week: ISOWeekOf(from), From: from, To: to.AddDate(0, 0, -1), Site: site, GeneratedAt: now, Partial: now.Before(to)}

	inWin := func(t time.Time, a, b time.Time) bool { return !t.Before(a) && t.Before(b) }
	var cur, prev []repositories.DigestText
	for _, t := range texts {
		switch {
		case inWin(t.CreatedAt, from, to):
			cur = append(cur, t)
		case inWin(t.CreatedAt, prevFrom, from):
			prev = append(prev, t)
		}
	}
	curSet, prevSet := ClusterThemes(cur, tax), ClusterThemes(prev, tax)
	d.Totals = windowTotals(rows, "", from, to, curSet)
	d.Prev = windowTotals(rows, "", prevFrom, from, prevSet)

	for _, th := range curSet.Visible {
		td := DigestThemeDelta{DigestTheme: th}
		if p, ok := prevSet.AllCounts[th.Key]; ok && p > 0 {
			if p < AnonymityMinGroup {
				td.PrevHidden = true
			} else {
				pc := p
				delta := th.Newcomers - p
				td.PrevCount, td.Delta = &pc, &delta
			}
		} else {
			zero, delta := 0, th.Newcomers
			td.PrevCount, td.Delta = &zero, &delta
		}
		d.Themes = append(d.Themes, td)
	}
	d.Hidden = curSet.HiddenThemes
	if curSet.Confidential > 0 {
		d.ConfidentialAny = true
		if curSet.Confidential >= AnonymityMinGroup {
			n := curSet.Confidential
			d.Confidential = &n
		}
	}

	// Зоны риска — как в обзоре, на конец окна (или сейчас, если неделя идёт).
	at := now
	if !d.Partial {
		at = to.Add(-time.Second)
	}
	o := BuildOverview(rows, site, at, tax, opts...)
	for _, r := range o.Risks {
		if r.Zone == domain.RiskRed {
			d.Risks.Red++
		} else {
			d.Risks.Yellow++
		}
	}
	d.Risks.Items = o.Risks

	// По площадкам.
	for _, st := range overviewSites(site) {
		var stTexts []repositories.DigestText
		for _, t := range cur {
			if t.Site == st {
				stTexts = append(stTexts, t)
			}
		}
		set := ClusterThemes(stTexts, tax)
		tot := windowTotals(rows, st, from, to, set)
		row := DigestSiteRow{Site: st, OnPulse: tot.OnPulse, Answered: tot.Answered, WAvg: tot.WAvg, Problems: tot.Problems, Respondent: tot.Respondents}
		if len(set.Visible) > 0 {
			l := set.Visible[0].Label
			row.TopTheme = &l
		}
		d.Sites = append(d.Sites, row)
	}

	// Решения по ИС в неделю после отчётной.
	loc := domain.SiteOf(site).Loc
	nextTo := to.AddDate(0, 0, 7)
	d.ProbationNow = !now.Before(to)
	for _, e := range rows.Employees {
		d90 := civilDate(e.StartDate, loc).AddDate(0, 0, 90)
		if !d90.Before(to) && d90.Before(nextTo) {
			d.Probation++
		}
	}

	d.ToneTrend = BuildTone(rows, site, from.AddDate(0, 0, -7*(TrendWeeks-1)), TrendWeeks, loc)
	d.Tone = toneWeek(rows, site, from, loc)

	d.Aggregates = digestAggregates(d)
	d.Text = TemplateDigestText(d.Aggregates)
	d.MaxText = DigestMaxMessage(d)
	return d
}

// windowTotals — сводка окна [from, to) по площадке (пусто — все строки).
func windowTotals(rows *repositories.OverviewRows, site string, from, to time.Time, set ThemeSet) DigestTotals {
	siteOf := people2site(rows)
	keep := func(emp uuid.UUID) bool {
		s, ok := siteOf[emp]
		return ok && (site == "" || s == site)
	}
	t := DigestTotals{Problems: set.Signals, Critical: set.CriticalUsers}
	onPulse, answered := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, ev := range rows.Events {
		if ev.IsTest || ev.Kind != domain.KindSurvey || ev.SentAt == nil || ev.SentAt.Before(from) || !ev.SentAt.Before(to) || !keep(ev.EmployeeID) {
			continue
		}
		onPulse[ev.EmployeeID] = true
		if ev.Status == domain.EventAnswered {
			answered[ev.EmployeeID] = true
		}
	}
	t.OnPulse, t.Answered = len(onPulse), len(answered)
	if t.OnPulse > 0 {
		p := int(math.Round(float64(t.Answered) * 100 / float64(t.OnPulse)))
		t.ResponseRatePct = &p
	}
	// Индекс W по шкалам (ресерч, блок C): d = max(0, 8 − x)/7 на шкале 10, W = 100·(1 − mean d).
	var sumD, sumX float64
	n := 0
	resp := map[uuid.UUID]bool{}
	for _, a := range rows.Answers {
		if a.IsTest || a.CreatedAt.Before(from) || !a.CreatedAt.Before(to) || !keep(a.EmployeeID) {
			continue
		}
		x, ok := pulseValue(a)
		if !ok {
			continue
		}
		sumD += math.Max(0, 8-x) / 7
		sumX += x
		n++
		resp[a.EmployeeID] = true
	}
	t.Respondents = len(resp)
	if n > 0 && len(resp) >= AnonymityMinGroup {
		w := int(math.Round(100 * (1 - sumD/float64(n))))
		avg := math.Round(sumX/float64(n)*10) / 10
		t.WAvg, t.PulseAvg = &w, &avg
	}
	return t
}

// periodLabel — «19–25 окт» или «29 сен – 5 окт».
func periodLabel(from, to time.Time) string {
	months := []string{"янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}
	if from.Month() == to.Month() {
		return fmt.Sprintf("%d–%d %s", from.Day(), to.Day(), months[to.Month()-1])
	}
	return fmt.Sprintf("%d %s – %d %s", from.Day(), months[from.Month()-1], to.Day(), months[to.Month()-1])
}

// digestAggregates — вход LLM: сводка и до четырёх тем. Конфиденциальное в LLM не идёт.
func digestAggregates(d *Digest) DigestAggregates {
	t := d.Totals
	agg := DigestAggregates{
		PeriodLabel: periodLabel(d.From, d.To),
		Totals: DigestAggTotals{
			OnPulse: t.OnPulse, Answered: t.Answered, ResponseRatePct: t.ResponseRatePct,
			WAvg: t.WAvg, Problems: t.Problems, Critical: t.Critical,
		},
		Themes:          []DigestAggTheme{},
		PreviousActions: []DigestAggAction{},
	}
	if t.WAvg != nil && d.Prev.WAvg != nil {
		delta := *t.WAvg - *d.Prev.WAvg
		agg.Totals.WDelta = &delta
	}
	for i, th := range d.Themes {
		if i == digestLLMThemes {
			break
		}
		sites := th.Sites
		if sites == nil {
			sites = []string{}
		}
		agg.Themes = append(agg.Themes, DigestAggTheme{Label: th.Label, Count: th.Newcomers, Sites: sites, Owner: th.Owner})
	}
	return agg
}

// DigestMaxMessage — одно сообщение в MAX (UX 3.7): однострочник кодом, связный текст,
// темы, зоны риска, решения по ИС. Без имён и цитат. Не длиннее DigestMaxText.
func DigestMaxMessage(d *Digest) string {
	agg := d.Aggregates
	t := d.Totals
	var b strings.Builder
	scope := "все площадки"
	if d.Site != "" {
		scope = d.Site
	}
	fmt.Fprintf(&b, "Пульс адаптации · %s · %s\n", agg.PeriodLabel, scope)
	line := []string{fmt.Sprintf("на пульсе %d", t.OnPulse)}
	ans := fmt.Sprintf("ответили %d", t.Answered)
	if t.ResponseRatePct != nil {
		ans += fmt.Sprintf(" (%d %%)", *t.ResponseRatePct)
	}
	line = append(line, ans)
	if t.WAvg != nil {
		w := fmt.Sprintf("W %d", *t.WAvg)
		if agg.Totals.WDelta != nil && *agg.Totals.WDelta != 0 {
			w += fmt.Sprintf(" (%s)", signed(*agg.Totals.WDelta))
		}
		line = append(line, w)
	}
	line = append(line, fmt.Sprintf("проблем %d", t.Problems), fmt.Sprintf("критичных %d", t.Critical))
	b.WriteString(capitalize(strings.Join(line, " · ")))
	b.WriteString("\n\n")
	b.WriteString(d.Text.Summary)
	b.WriteString("\n\n")

	if len(d.Themes) > 0 {
		b.WriteString("Темы недели:\n")
		for _, th := range d.Themes {
			fmt.Fprintf(&b, "• %s — %s", th.Label, plural(th.Newcomers, "сотрудник", "сотрудника", "сотрудников"))
			if len(th.Sites) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(th.Sites, ", "))
			}
			switch {
			case th.PrevHidden:
				b.WriteString(", на прошлой неделе меньше 5")
			case th.Delta != nil && *th.Delta != 0:
				fmt.Fprintf(&b, ", к прошлой неделе %s", signed(*th.Delta))
			case th.Delta != nil:
				b.WriteString(", как на прошлой неделе")
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("Тем, за которыми 5 и больше сотрудников, нет.\n")
	}
	if d.Hidden > 0 {
		fmt.Fprintf(&b, "Ещё %s — меньше 5 человек в каждой, скрыты (порог анонимности).\n", plural(d.Hidden, "тема", "темы", "тем"))
	}
	switch {
	case d.Confidential != nil:
		fmt.Fprintf(&b, "Конфиденциальные сигналы: %d — у HR-партнёра и СБ.\n", *d.Confidential)
	case d.ConfidentialAny:
		b.WriteString("Есть конфиденциальные сигналы — у HR-партнёра и СБ.\n")
	}
	b.WriteString(ToneLine(d.Tone) + "\n")
	if block := digestDoneBlock(d.Done); block != "" {
		b.WriteString("\n" + block)
	}
	fmt.Fprintf(&b, "\nЗоны риска: красных %d, жёлтых %d.\n", d.Risks.Red, d.Risks.Yellow)
	when := "на следующей неделе"
	if d.ProbationNow {
		when = "на этой неделе"
	}
	fmt.Fprintf(&b, "Решения по ИС %s: %d.", when, d.Probation)
	return clipRunes(b.String(), DigestMaxText)
}

func signed(n int) string {
	switch {
	case n > 0:
		return "+" + strconv.Itoa(n)
	case n < 0:
		return "−" + strconv.Itoa(-n)
	}
	return "0"
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// DigestFacts — все числа, которые код вправе вставить в сообщение MAX: агрегаты LLM и
// таблицы. Тест «каждое число текста есть во входе» сверяет MaxText с этим JSON.
func DigestFacts(d *Digest) string {
	themes := make([]map[string]any, 0, len(d.Themes))
	for _, th := range d.Themes {
		themes = append(themes, map[string]any{"label": th.Label, "count": th.Newcomers, "prev": th.PrevCount, "delta": th.Delta})
	}
	b, _ := json.Marshal(map[string]any{
		"aggregates": d.Aggregates, "themes": themes, "hidden": d.Hidden, "confidential": d.Confidential,
		"risks_red": d.Risks.Red, "risks_yellow": d.Risks.Yellow, "probation": d.Probation, "min_group": AnonymityMinGroup,
		"tone": map[string]any{"pos": d.Tone.Pos, "neu": d.Tone.Neu, "neg": d.Tone.Neg, "n": d.Tone.N},
		"done": d.Done,
	})
	return string(b)
}

// DigestSendResult — что ушло.
type DigestSendResult struct {
	Week   string
	Text   string
	SentTo int
	Logged bool // получателей нет или бот выключен — текст только в лог
}

// Send — отправить дайджест недели (пусто — прошедшая) всем из MAX_DIGEST_USER_IDS и активным
// hr_lead с users.max_user_id (без дублей).
func (s *DigestService) Send(ctx context.Context, week string) (*DigestSendResult, error) {
	if week == "" {
		week = s.LastFullWeek()
	}
	d, err := s.Get(ctx, week, "")
	if err != nil {
		return nil, err
	}
	res := &DigestSendResult{Week: d.Week, Text: d.MaxText}
	to := s.recipients(ctx)
	if len(to) == 0 || s.max == nil {
		res.Logged = true
		logger.Info("дайджест %s: получателей нет (MAX_DIGEST_USER_IDS, hr_lead с max_user_id) или бот выключен — текст в лог:\n%s", d.Week, d.MaxText)
		return res, nil
	}
	for _, uid := range to {
		_, err := s.max.Send(ctx, max.OutMessage{UserID: uid, Text: d.MaxText,
			Buttons: [][]max.Button{{{Text: "Открыть аналитику", URL: d.Link}}}})
		if err != nil {
			logger.Warn("дайджест %s: user=%d не ушло: %v", d.Week, uid, err)
			continue
		}
		res.SentTo++
	}
	logger.Info("дайджест %s: отправлен %d из %d получателей", d.Week, res.SentTo, len(to))
	return res, nil
}

// recipients — MAX user_id получателей: сначала MAX_DIGEST_USER_IDS, затем активные hr_lead с
// заданным users.max_user_id. Нули и повторы отброшены. База недоступна — только env.
func (s *DigestService) recipients(ctx context.Context) []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(id int64) {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range s.opts.UserIDs {
		add(id)
	}
	if !s.repos.Enabled() {
		return out
	}
	users, err := s.repos.Users.List(ctx)
	if err != nil {
		logger.Warn("дайджест: пользователи hr_lead не прочитаны: %v", err)
		return out
	}
	for _, u := range users {
		if u.Active && u.Role == domain.RoleHRLead && u.MaxUserID != nil {
			add(*u.MaxUserID)
		}
	}
	return out
}

// RunWeekly — понедельник 9:00 по Москве: дайджест прошедшей недели, один раз за неделю
// (неделя записывается в marker_states).
func (s *DigestService) RunWeekly(ctx context.Context) {
	logger.Info("дайджест: расписание пн %02d:00 МСК, получателей %d", DigestSendHour, len(s.opts.UserIDs))
	t := time.NewTicker(digestCheckEvery)
	defer t.Stop()
	for {
		s.tickWeekly(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *DigestService) tickWeekly(ctx context.Context) {
	now := s.now().In(domain.SiteOf("").Loc)
	if !DigestDue(now) {
		return
	}
	week := ISOWeekOf(now.AddDate(0, 0, -7))
	y, w := now.AddDate(0, 0, -7).ISOWeek()
	mark := int64(y*100 + w)
	if last, err := s.repos.BotState.Marker(ctx, digestMarkerKey); err != nil || last >= mark {
		return
	}
	if _, err := s.Send(ctx, week); err != nil {
		logger.Warn("дайджест %s не собран: %v", week, err)
		return
	}
	if err := s.repos.BotState.SaveMarker(ctx, digestMarkerKey, mark); err != nil {
		logger.Warn("дайджест: отметка недели не сохранена: %v", err)
	}
}

// DigestDue — пора ли слать: понедельник с 9:00 до конца дня (после рестарта днём — догоняем).
func DigestDue(now time.Time) bool {
	return now.Weekday() == DigestSendWeekday && now.Hour() >= DigestSendHour
}
