package services

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"main/domain"
	"main/pkg/errs"

	"github.com/google/uuid"
)

// Детерминированный парсер Excel-ТЗ заказчика: вердикт ресерчера §1
// (artifacts/research/2026-10-24-branching-verdict.md) с поправками построчного разбора
// (steps/06-excel-tz-line-by-line.md — главнее вердикта). Контракт — api_contract.md, v4.
// Блок = шаги с одним днём и временем; шаг = сообщение блока в порядке строк; ветка живёт
// в кнопке или в варианте вопроса опроса. Мягкий режим: неоднозначность — предупреждение в
// отчёте, а не отказ файла.

// ImportWarning — пункт отчёта импорта. StepKey/StepID — шаг, к которому относится (пусто —
// к сценарию или правилу).
type ImportWarning struct {
	Row     int    `json:"row"`
	Code    string `json:"code"`
	Message string `json:"message"`
	StepKey string `json:"step_key"`
	StepID  string `json:"step_id"`
}

// ImportReport — import_report: что получилось из файла.
type ImportReport struct {
	Blocks          int             `json:"blocks"`
	Messages        int             `json:"messages"`  // все шаги (сообщения блоков)
	Questions       int             `json:"questions"` // шаги с кнопками + вопросы-выбор в опросах
	Branches        int             `json:"branches"`  // ветки кнопок и вариантов опроса
	Rules           int             `json:"rules"`
	MediaNames      int             `json:"media_names"`
	MediaNameList   []string        `json:"media_name_list"`
	Surveys         int             `json:"surveys"`
	SurveyQuestions int             `json:"survey_questions"`
	Outcomes        int             `json:"outcomes"`
	Commands        int             `json:"commands"`
	ImplicitYesNo   int             `json:"implicit_yes_no"` // вопросы без вариантов → «Да, расскажи / Нет, спасибо»
	NextButton      int             `json:"next_button"`     // шаги без вопроса, не последние в блоке — бот даст «Дальше →»
	QuestionFirst   int             `json:"question_first"`  // блоки, которые начинаются с вопроса
	HRTasks         int             `json:"hr_tasks"`
	Warnings        []ImportWarning `json:"warnings"`
}

// Коды предупреждений (api_contract.md, v4).
const (
	warnNoText        = "no_text"
	warnAltText       = "alt_text"
	warnColsShifted   = "columns_shifted"
	warnHRTask        = "hr_task"
	warnMediaMissing  = "media_missing"
	warnPhotoUnnamed  = "photo_unnamed"
	warnOutcomes      = "survey_outcomes"
	warnRecruiter     = "recruiter_flag"
	warnUnknownWhen   = "unknown_when"
	warnWhenApprox    = "when_approx"
	warnAddressForm   = "address_form_copied"
	warnBranchRows    = "branch_unmatched"
	warnOptionsMerged = "options_row_merged"
	warnImplicitYesNo = "implicit_yes_no"
	warnNoContent     = "answer_content_missing"
	warnMixedForm     = "mixed_address_form"
	warnGendered      = "gendered_text"
	warnCredentials   = "out_of_scope_credentials"
	warnNonHTTPS      = "non_https_link"
	warnTimeOrder     = "time_order_fixed"
	warnCommand       = "command_unmapped"
	warnOnboarding    = "covered_by_onboarding"
	warnFromHelp      = "text_from_help"
	warnStartDay      = "start_day_placeholder"
	warnDupGreeting   = "duplicate_greeting"
	warnSurveyMissing = "survey_missing"
)

// flagRecruiter — флаг новичка «рекрутер дал отмашку» (ТЗ, колонка H).
const flagRecruiter = "recruiter_enabled"

// Подписи кнопок «вопроса без вариантов» (06 §1.5).
const (
	labelImplicitYes = "Да, расскажи"
	labelImplicitNo  = "Нет, спасибо"
)

// tzColumns — колонка → слова в шапке (первое совпадение).
var tzColumns = []struct {
	key   string
	words []string
}{
	{"when", []string{"когда", "информирован"}},
	{"title", []string{"повод"}},
	{"sites", []string{"тег", "метка"}},
	{"kind", []string{"макет", "форма"}},
	{"text", []string{"текст"}},
	{"required", []string{"обязательн"}},
	{"media", []string{"изображ", "вложен"}},
	{"num", []string{"№"}},
}

var (
	reQuestionNo = regexp.MustCompile(`(?i)^\s*вопрос\s*\d+\s*[:.]\s*`)
	// reSurveyWord — «Опрос бота», но не «…_вопрос» (r25).
	reSurveyWord = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(?:опрос|survey)`)
)

// tzImport — результат разбора файла.
type tzImport struct {
	steps    []domain.ScenarioStep
	rules    []domain.ScenarioRule
	commands []domain.Command
	sites    []string
	report   ImportReport
}

type tzBlock struct {
	day   int
	time  string
	first int // индекс первого шага
}

type tzParser struct {
	cols     map[string]int
	out      tzImport
	blocks   []*tzBlock
	stepRow  []int // строка Excel каждого шага
	warns    []ImportWarning
	warnStep []int // индекс шага предупреждения (−1 — без шага)
	approx   map[string]bool
	siteSet  map[string]bool

	lastCondSites domain.StringList // условие площадки строки («для офисных») для строк-продолжений
	lastTitle     string
	openSurvey    int // индекс шага-опроса, в который дописываются вопросы следующих строк (−1 — нет)
	pdfWarned     map[string]bool
}

// parseTZRows — разбор листа: rows — ячейки строк, header — индекс строки шапки.
func parseTZRows(rows [][]string, header int, cols map[string]int) (*tzImport, error) {
	p := &tzParser{cols: cols, approx: map[string]bool{}, siteSet: map[string]bool{}, openSurvey: -1, pdfWarned: map[string]bool{}}
	for i := header + 1; i < len(rows); i++ {
		p.row(i+1, rows[i])
	}
	if len(p.out.steps) == 0 {
		return nil, errs.NewValidationError(msgImportEmpty)
	}
	p.finish()
	return &p.out, nil
}

func (p *tzParser) cell(row []string, key string) string {
	j, ok := p.cols[key]
	if !ok || j >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[j])
}

// extras — ячейки колонок без шапки: H «по отмашке рекрутера», J — дополнение к тексту
// (список документов, 06 §1.7).
func (p *tzParser) extras(row []string) (flag bool, extra string) {
	mapped := map[int]bool{}
	for _, j := range p.cols {
		mapped[j] = true
	}
	for j, c := range row {
		c = strings.TrimSpace(c)
		if mapped[j] || c == "" {
			continue
		}
		if strings.Contains(strings.ToLower(c), "отмашк") {
			flag = true
			continue
		}
		if mj, ok := p.cols["media"]; ok && j > mj {
			extra = normalizeCell(c)
		}
	}
	return flag, extra
}

func (p *tzParser) warn(row int, step int, code, msg string) {
	p.warns = append(p.warns, ImportWarning{Row: row, Code: code, Message: msg})
	p.warnStep = append(p.warnStep, step)
}

func (p *tzParser) curBlock() *tzBlock {
	if len(p.blocks) == 0 {
		return nil
	}
	return p.blocks[len(p.blocks)-1]
}

// sameBlock — тот же день и время совпадает, не задано или идёт сразу после (до 5 минут).
func sameBlock(b *tzBlock, w whenSpec) bool {
	if b == nil || b.day != w.day {
		return false
	}
	if w.time == "" || b.time == "" || w.time == b.time {
		return true
	}
	d := minutesOf(w.time) - minutesOf(b.time)
	return d > 0 && d <= 5
}

func minutesOf(hhmm string) int {
	var h, m int
	_, _ = fmt.Sscanf(hhmm, "%d:%d", &h, &m)
	return h*60 + m
}

func hhmmOf(min int) string {
	if min < 0 {
		min = 0
	}
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

func isOfficeCond(s string) bool {
	lo := strings.ToLower(s)
	return strings.Contains(lo, "для офисн") || strings.Contains(lo, "сотрудников офиса")
}

func (p *tzParser) row(n int, row []string) {
	whenRaw := p.cell(row, "when")
	rawText := p.cell(row, "text")
	title := p.cell(row, "title")
	tag := p.cell(row, "sites")
	media := p.cell(row, "media")
	kindRaw := p.cell(row, "kind")
	flag, extra := p.extras(row)
	if whenRaw == "" && rawText == "" && title == "" && media == "" {
		return
	}
	w := parseTZWhen(whenRaw)

	switch w.kind {
	case whenRule:
		p.openSurvey = -1
		p.rule(n, whenRaw, rawText, media)
		return
	case whenBranch:
		p.openSurvey = -1
		p.branchRow(n, w, rawText, tag, media)
		return
	case whenUnknown:
		p.warn(n, -1, warnUnknownWhen, fmt.Sprintf("«Когда» не разобрано («%s») — строка добавлена в предыдущий блок", truncateRunes(whenRaw, 60)))
		w = whenSpec{kind: whenContinue}
	}

	// Блок.
	b := p.curBlock()
	newBlock := false
	switch {
	case b == nil:
		b, newBlock = p.newBlock(w.day, w.time), true
	case w.kind == whenContinue:
	case w.kind == whenAfter || sameBlock(b, w):
		if b.time == "" && w.time != "" {
			if len(p.out.steps) > b.first {
				// «В 09.01» после строк без времени: они уходят минутой раньше (06 §1.8).
				b.time = hhmmOf(minutesOf(w.time) - 1)
				p.warn(n, len(p.out.steps), warnTimeOrder, fmt.Sprintf("Строки блока без времени поставлены на %s, чтобы уйти до сообщения «в %s»", b.time, w.time))
			} else {
				b.time = w.time
			}
		}
	default:
		b, newBlock = p.newBlock(w.day, w.time), true
	}
	if w.kind == whenTime && w.approx != "" && !p.approx[w.approx] {
		p.approx[w.approx] = true
		p.warn(n, len(p.out.steps), warnWhenApprox, approxMessage(w.approx))
	}

	// Условие площадки: «для офисных» в «когда» или в «поводе» (06 §1.7); продолжение — как у
	// предыдущей строки.
	condSites := w.sites
	if isOfficeCond(title) {
		condSites = append(domain.StringList{}, officeSites...)
	}
	if w.kind == whenContinue && len(condSites) == 0 {
		condSites = p.lastCondSites
	}
	p.lastCondSites = condSites

	pc := parseCell(rawText)
	isSurveyKind := reSurveyWord.MatchString(kindRaw)
	if newBlock || (kindRaw != "" && !isSurveyKind) {
		p.openSurvey = -1
	}

	// Правила после опроса (r57) — к опросам текущего блока.
	if pc.outcomes != nil {
		if p.attachOutcomes(n, b, pc) {
			return
		}
		pc = parsedCell{text: withName(normalizeCell(rawText))}
	}

	// Площадка; не из словаря — это сдвинутая колонка «повод» (r17).
	sites := parseSites(tag)
	shifted := tag != "" && len(sites) == 0
	if shifted {
		title = tag
	}
	if len(sites) == 0 && len(condSites) > 0 {
		sites = append(domain.StringList{}, condSites...)
	}
	for _, s := range sites {
		p.siteSet[s] = true
	}

	// Вопрос, продолжающий открытый опрос (r40, r41, r63, r64, r65).
	questions := surveyQuestionsOf(pc)
	if p.openSurvey >= 0 && !isSurveyKind && kindRaw == "" && len(questions) > 0 && !shifted &&
		sitesEqual(p.out.steps[p.openSurvey].Sites, sites) {
		sv := p.out.steps[p.openSurvey].Survey
		sv.Questions = append(sv.Questions, questions...)
		normalizeSurvey(sv)
		for _, cw := range pc.warns {
			p.warn(n, p.openSurvey, cw.code, cw.msg)
		}
		if pc.tail != "" {
			p.warn(n, p.openSurvey, warnNoText, "Текст после веток вопроса опроса не перенесён — добавьте его отдельным шагом")
		}
		return
	}
	if !isSurveyKind {
		p.openSurvey = -1
	}

	st := domain.ScenarioStep{
		DayOffset: b.day, Time: b.time, Kind: parseKind(kindRaw), Sites: sites,
		Required:      strings.HasPrefix(strings.ToLower(p.cell(row, "required")), "да"),
		WorkHoursOnly: true, Buttons: domain.ButtonList{}, Media: domain.MediaRefList{},
	}
	st.ID, _ = uuid.NewV7()
	stepIdx := len(p.out.steps)
	if shifted {
		p.warn(n, stepIdx, warnColsShifted, "В колонке «Тег сотрудника» текст повода — он взят заголовком, шаг для всех площадок")
	}

	// Письмо HR (r19): бот не отправляет.
	text := pc.text
	lines := splitLines(normalizeCell(rawText))
	if len(lines) > 0 && reHRTaskHead.MatchString(lines[0]) {
		st.HRTask, st.Kind = true, domain.KindText // текстовый шаг с hr_task: бот не шлёт, HR видит текст
		text = withName(joinTrim(lines[1:]))
		pc = parsedCell{text: text}
		questions = nil
		p.warn(n, stepIdx, warnHRTask, "Письмо HR кандидату: бот его не отправляет — шаг стал задачей HR")
	}

	for _, cw := range pc.warns {
		p.warn(n, stepIdx, cw.code, cw.msg)
	}
	if pc.alt != nil {
		alt := *pc.alt
		st.AltText = &alt
		p.warn(n, stepIdx, warnAltText, "У текста два авторских варианта: первый взят в сообщение, второй — в альтернативу")
	}

	// Вложения по имени (колонка I); «#Адаптационный план» — личный файл новичка (r36).
	for _, name := range splitMediaNames(media) {
		if strings.EqualFold(name, "фото") {
			st.Gallery = true
			p.warn(n, stepIdx, warnPhotoUnnamed, "Вложение «ФОТО» без имени файла: несколько фото одним сообщением — загрузите их в медиатеку")
		}
		st.Media = append(st.Media, domain.MediaRef{Name: name})
	}
	for _, a := range pc.attach {
		lo := strings.ToLower(a)
		switch {
		case strings.Contains(lo, "адаптационн"):
			st.Media = append(st.Media, domain.MediaRef{Name: "Адаптационный план", Personal: "adaptation_plan"})
		case len(splitMediaNames(media)) == 0 && !strings.Contains(lo, "ссылк"):
			p.warn(n, stepIdx, warnMediaMissing, fmt.Sprintf("В тексте «# %s», но имени вложения в колонке «Сопроводительное изображение» нет", truncateRunes(a, 60)))
		}
	}
	// «(презентация)» в макете (r32), а вложения нет — заглушка файла в медиатеке.
	if len(splitMediaNames(media)) == 0 && strings.Contains(strings.ToLower(kindRaw), "презентац") {
		st.Media = append(st.Media, domain.MediaRef{Name: "Корпоративная презентация (файл)"})
		p.warn(n, stepIdx, warnNoContent, "В макете указана презентация, но файла в ТЗ нет — загрузите «Корпоративная презентация (файл)» в медиатеку")
	}
	// Колонка J — главное содержание (список документов): к тексту целиком.
	if extra != "" {
		text = joinTrim([]string{text, "", withName(extra)})
	}

	if flag {
		f := flagRecruiter
		st.FlagRequired = &f
		p.warn(n, stepIdx, warnRecruiter, "Шаг «по отмашке рекрутера»: уйдёт, только если у новичка включён флаг recruiter_enabled")
	}

	// Заголовок: повод, иначе первая строка текста.
	if title == "" {
		title = firstLineTitle(text)
	}
	if title == "" && len(questions) > 0 {
		title = firstLineTitle(questions[0].Text)
	}
	if title == "" {
		title = p.lastTitle
	}
	if title == "" {
		title = "Сообщение"
	}
	p.lastTitle = title
	st.Title = truncateRunes(strings.Join(strings.Fields(title), " "), 120)

	// Опрос («Опрос бота» или блок шкал) — вопросы со своими ветками; иначе кнопки шага.
	switch {
	case len(questions) > 0 && (isSurveyKind || pc.survey != nil):
		st.Kind = domain.KindSurvey
		st.Survey = &domain.Survey{Questions: questions}
		if pc.survey != nil {
			st.Survey.Threshold = pc.survey.Threshold
		} else {
			text = "" // текст вопроса ушёл в вопрос опроса
		}
		normalizeSurvey(st.Survey)
		p.openSurvey = stepIdx
	default:
		var labels []string
		for i, o := range pc.options {
			label := strings.TrimSpace(o.label)
			labels = append(labels, label)
			btn := domain.StepButton{Label: label, OpensFreeText: o.other}
			if br, ok := pc.branches[i]; ok {
				btn.Branch = br
			}
			btn.Branch = addressFormBranch(o.label, btn.Branch)
			if pc.pending[i] {
				if btn.Branch == nil {
					btn.Branch = emptyBranch()
				}
				btn.Action = "pending_rows"
			}
			st.Buttons = append(st.Buttons, btn)
		}
		if w := longLabelWarn(labels...); w != nil {
			p.warn(n, stepIdx, w.code, w.msg)
		}
		if len(st.Buttons) > 0 && st.Kind == domain.KindSurvey {
			st.Kind = domain.KindText
		} else if st.Kind == domain.KindSurvey {
			st.Survey = DefaultImportSurvey(text, st.Title)
		}
	}
	if st.Kind != domain.KindSurvey && len(st.Buttons) == 0 && !st.HRTask && reInputAsk.MatchString(text) {
		st.Kind = domain.KindInput // ждёт текст и фото новичка (r34)
	}
	st.TextTy, st.TextVy = text, text

	if strings.TrimSpace(text) == "" && len(st.Media) == 0 && st.Survey == nil && len(st.Buttons) == 0 {
		p.warn(n, stepIdx, warnNoText, "Пустое сообщение — допишите текст")
	} else if isPlaceholder(text) {
		p.warn(n, stepIdx, warnNoText, "Текст-заглушка («…») — допишите сообщение")
	}
	p.textChecks(n, stepIdx, &st, title+" "+kindRaw)

	// «Варианты ответов» отдельной строкой без текста (r6) — кнопки предыдущего сообщения блока.
	if strings.TrimSpace(text) == "" && len(st.Buttons) > 0 && len(st.Media) == 0 && stepIdx > b.first {
		prev := &p.out.steps[stepIdx-1]
		if len(prev.Buttons) == 0 && prev.Survey == nil && sitesEqual(prev.Sites, st.Sites) {
			prev.Buttons = st.Buttons
			if prev.Kind == domain.KindInput {
				prev.Kind = domain.KindText
			}
			p.moveWarns(stepIdx, stepIdx-1)
			p.warn(n, stepIdx-1, warnOptionsMerged, "Варианты ответов стояли отдельной строкой — стали кнопками предыдущего сообщения")
			return
		}
	}

	p.out.steps = append(p.out.steps, st)
	p.stepRow = append(p.stepRow, n)

	// Безусловный хвост после веток (r16) — следующий шаг того же блока.
	if pc.tail != "" {
		t := st
		t.ID, _ = uuid.NewV7()
		t.TextTy, t.TextVy = pc.tail, pc.tail
		t.Buttons, t.Survey, t.Media, t.AltText, t.Gallery = domain.ButtonList{}, nil, domain.MediaRefList{}, nil, false
		t.Kind = domain.KindText
		p.out.steps = append(p.out.steps, t)
		p.stepRow = append(p.stepRow, n)
	}
}

// textChecks — смешанная форма, род, http-ссылки, доступы в мессенджере.
func (p *tzParser) textChecks(n, stepIdx int, st *domain.ScenarioStep, meta string) {
	if st.HRTask {
		return
	}
	all := st.TextTy
	for _, b := range st.Buttons {
		all += "\n" + b.Label
	}
	if st.Survey != nil {
		for _, q := range st.Survey.Questions {
			all += "\n" + q.Text
		}
	}
	if isMixedForm(st.TextTy) || (st.Survey != nil && isMixedForm(all)) {
		p.warn(n, stepIdx, warnMixedForm, "В сообщении смешаны «ты» и «вы» — выровняйте форму обращения")
	}
	if isGendered(all) {
		p.warn(n, stepIdx, warnGendered, "Текст в мужском или женском роде («готов/готова», «справился») — нужна нейтральная формулировка")
	}
	if m := reHTTPLink.FindString(all); m != "" {
		p.warn(n, stepIdx, warnNonHTTPS, fmt.Sprintf("Ссылка без https (%s): MAX может не открыть её кнопкой — оставлена текстом", m))
	}
	if isCredentials(meta, st.TextTy) {
		p.warn(n, stepIdx, warnCredentials, "Отправка доступов к системам: логины и пароли в мессенджер не шлём — это задача HR/ИТ")
	}
}

// surveyQuestionsOf — вопросы опроса из ячейки: блок шкал, опрос-выбор, открытый вопрос или
// вопрос с вариантами и ветками (r39–r41, r62, r63).
func surveyQuestionsOf(pc parsedCell) []domain.SurveyQuestion {
	if pc.survey != nil {
		qs := append([]domain.SurveyQuestion{}, pc.survey.Questions...)
		for i := range qs {
			qs[i].Text = reQuestionNo.ReplaceAllString(qs[i].Text, "")
		}
		return qs
	}
	if len(pc.options) == 0 || strings.TrimSpace(pc.text) == "" {
		return nil
	}
	q := domain.SurveyQuestion{Text: reQuestionNo.ReplaceAllString(pc.text, ""), Type: "choice", Options: []string{}}
	value := 0
	for i, o := range pc.options {
		if o.other {
			q.Other = true
			continue
		}
		value++
		q.Options = append(q.Options, o.label)
		if br, ok := pc.branches[i]; ok {
			if q.Branches == nil {
				q.Branches = map[string]*domain.Branch{}
			}
			cp := cloneBranch(*br)
			if len(cp.On) > 0 { // индексы кнопок → значения вариантов (с 1)
				for k := range cp.On {
					cp.On[k]++
				}
			}
			q.Branches[strconv.Itoa(value)] = &cp
		}
	}
	return []domain.SurveyQuestion{q}
}

// copyQuestionVy — вторая форма вопроса (address_form_copied): text_vy — копия текста ТЗ,
// options_vy — копия подписей, если в них есть обращение на «ты». rewrite-vy перепишет копии.
func copyQuestionVy(q *domain.SurveyQuestion) {
	if q.TextVy == "" {
		q.TextVy = q.Text
	}
	if len(q.OptionsVy) > 0 {
		return
	}
	for _, o := range q.Options {
		if reTyWords.MatchString(o) {
			q.OptionsVy = slices.Clone(q.Options)
			return
		}
	}
}

// normalizeSurvey — шкала опроса по вопросам и флаги вариантов.
func normalizeSurvey(sv *domain.Survey) {
	allChoice := true
	for i := range sv.Questions {
		q := &sv.Questions[i]
		if q.Options == nil {
			q.Options = []string{}
		}
		if q.Type != "choice" && q.Type != "multi" {
			allChoice = false
		}
		if q.Threshold != nil && sv.Threshold == 0 {
			sv.Threshold = *q.Threshold
		}
		applyQuestionFlags(q)
		copyQuestionVy(q)
	}
	if allChoice {
		sv.Scale = domain.ScaleChoice
	} else {
		sv.Scale = domain.Scale10
		if sv.Threshold == 0 {
			sv.Threshold = importSurveyThreshold
		}
	}
}

// attachOutcomes — r57: правила после опроса ко всем опросам текущего блока (площадочные
// варианты r55/r56 получают одинаковые правила).
func (p *tzParser) attachOutcomes(n int, b *tzBlock, pc parsedCell) bool {
	attached := 0
	for i := b.first; i < len(p.out.steps); i++ {
		sv := p.out.steps[i].Survey
		if sv == nil || p.out.steps[i].Kind != domain.KindSurvey {
			continue
		}
		for _, ro := range pc.outcomes {
			o := domain.Outcome{If: []domain.OutcomeCond{}, Group: ro.group,
				Messages: ro.branch.Messages, Actions: ro.branch.Actions}
			if ro.q > 0 {
				c := domain.OutcomeCond{Q: ro.q}
				if ro.q <= len(sv.Questions) && sv.Questions[ro.q-1].Type == "multi" {
					c.Contains = []int{ro.option}
				} else {
					c.In = []int{ro.option}
				}
				o.If = append(o.If, c)
			}
			sv.Outcomes = append(sv.Outcomes, o)
		}
		attached++
	}
	if attached == 0 {
		return false
	}
	p.out.report.Outcomes += len(pc.outcomes)
	p.warn(n, b.first+firstSurveyOffset(p.out.steps[b.first:]), warnOutcomes,
		fmt.Sprintf("Реакции на ответы опроса (%d правил, «ЛИБО» — взаимоисключающие) добавлены к %d опросам блока — проверьте условия", len(pc.outcomes), attached))
	return true
}

func firstSurveyOffset(steps []domain.ScenarioStep) int {
	for i, s := range steps {
		if s.Kind == domain.KindSurvey {
			return i
		}
	}
	return 0
}

func emptyBranch() *domain.Branch {
	return &domain.Branch{Messages: []domain.BranchMessage{}, Actions: []domain.Action{}, Then: domain.ThenContinue}
}

func (p *tzParser) moveWarns(from, to int) {
	for i := range p.warnStep {
		if p.warnStep[i] == from {
			p.warnStep[i] = to
		}
	}
}

func (p *tzParser) newBlock(day int, hhmm string) *tzBlock {
	b := &tzBlock{day: day, time: hhmm, first: len(p.out.steps)}
	p.blocks = append(p.blocks, b)
	p.lastTitle = ""
	return b
}

// branchRow — «В случае варианта ответа „Да“» (r26–28): ветка кнопки последнего вопроса;
// строки по тегам — один файл на площадку (send_file per_tag).
func (p *tzParser) branchRow(n int, w whenSpec, rawText, tag, media string) {
	var btn *domain.StepButton
	stepIdx := -1
	for i := len(p.out.steps) - 1; i >= 0 && btn == nil; i-- {
		for j := range p.out.steps[i].Buttons {
			b := &p.out.steps[i].Buttons[j]
			if b.Action == "pending_rows" || (w.answer != "" && strings.HasPrefix(strings.ToLower(b.Label), strings.ToLower(w.answer))) {
				btn, stepIdx = b, i
				break
			}
		}
	}
	if btn == nil {
		p.warn(n, -1, warnBranchRows, "Строка-ответ «В случае варианта ответа» не нашла вопроса с такой кнопкой — пропущена")
		return
	}
	if btn.Branch == nil {
		btn.Branch = emptyBranch()
	}
	br := btn.Branch
	pc := parseCell(rawText)
	for _, cw := range pc.warns {
		p.warn(n, stepIdx, cw.code, cw.msg)
	}
	names := splitMediaNames(media)
	perTag := len(parseSites(tag)) > 0
	addFile := func(name string) {
		for _, a := range br.Actions {
			if a.Type == domain.ActionSendFile && a.Media == name {
				return
			}
		}
		br.Actions = append(br.Actions, domain.Action{Type: domain.ActionSendFile, Media: name, PerTag: perTag})
	}
	// «#Приложить пдф-ку …» — документ площадки (send_file per_tag); картинка из колонки I —
	// иллюстрация к реплике ветки (r26–r28: «Изображение 5» + PDF «где пообедать»).
	pdf := ""
	for _, a := range pc.attach {
		if lo := strings.ToLower(a); strings.Contains(lo, "пдф") || strings.Contains(lo, "pdf") {
			pdf = pdfMediaName(a)
		}
	}
	if pdf != "" {
		addFile(pdf)
		if !p.pdfWarned[pdf] {
			p.pdfWarned[pdf] = true
			p.warn(n, stepIdx, warnMediaMissing, fmt.Sprintf("Документ «%s» в ТЗ не приложен — загрузите PDF для каждой площадки в медиатеку", pdf))
		}
	}
	if pc.text != "" && len(br.Messages) == 0 {
		br.Messages = append(br.Messages, domain.BranchMessage{TextTy: pc.text, TextVy: pc.text, Media: domain.MediaRefList{}, Buttons: []domain.StepButton{}})
	}
	for _, name := range names {
		if pdf == "" || len(br.Messages) == 0 {
			addFile(name)
			continue
		}
		m := &br.Messages[0]
		dup := false
		for _, r := range m.Media {
			dup = dup || r.Name == name
		}
		if !dup {
			m.Media = append(m.Media, domain.MediaRef{Name: name})
		}
	}
	if len(names) == 0 && pdf == "" && len(pc.attach) > 0 {
		p.warn(n, stepIdx, warnMediaMissing, "Ветка просит приложить файл, но имени вложения в строке нет")
	}
	for _, s := range parseSites(tag) {
		p.siteSet[s] = true
	}
}

// rule — r69/r70: глобальные правила сценария.
func (p *tzParser) rule(n int, trigger, rawText, media string) {
	pc := parseCell(rawText)
	r := domain.ScenarioRule{Kind: domain.RuleFreeQuestion, Trigger: strings.Join(strings.Fields(trigger), " "), Row: n,
		Media: domain.MediaRefList{}, Buttons: []domain.StepButton{}}
	lo := strings.ToLower(trigger)
	if reSurveyWord.MatchString(lo) {
		r.Kind = domain.RuleSurveyReminder
		r.RemindAfterH = []int{24}
		if strings.Contains(lo, "послезавтра") {
			r.RemindAfterH = []int{24, 48}
		}
	}
	var text []string
	for _, l := range splitLines(pc.text) {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "опции") {
			r.Buttons = append(r.Buttons, escalationButtons(l)...)
			continue
		}
		text = append(text, l)
	}
	r.TextTy = joinTrim(text)
	r.TextVy = r.TextTy
	for _, name := range splitMediaNames(media) {
		r.Media = append(r.Media, domain.MediaRef{Name: name})
	}
	r.Key = fmt.Sprintf("rule-%s", r.Kind)
	for _, x := range p.out.rules {
		if x.Key == r.Key {
			r.Key = fmt.Sprintf("%s-%d", r.Key, n)
		}
	}
	p.out.rules = append(p.out.rules, r)
}

// finish — время блоков, ключи шагов, вопросы без вариантов, команды, «ты/вы», счётчики.
func (p *tzParser) finish() {
	steps := p.out.steps
	rep := &p.out.report
	ty, vy := 0, 0
	firstAddr := 0
	for bi, b := range p.blocks {
		end := len(steps)
		if bi+1 < len(p.blocks) {
			end = p.blocks[bi+1].first
		}
		if end <= b.first {
			continue
		}
		rep.Blocks++
		hhmm := strings.ReplaceAll(b.time, ":", "")
		if hhmm == "" {
			hhmm = "auto"
		}
		for i := b.first; i < end; i++ {
			st := &steps[i]
			st.DayOffset, st.Time, st.Stage = b.day, b.time, domain.StageForDay(b.day)
			st.Key = fmt.Sprintf("d%+d-%s-%02d", b.day, hhmm, i-b.first+1)
			nextSurvey := i+1 < end && steps[i+1].Kind == domain.KindSurvey
			if p.implicitYesNo(st, nextSurvey) {
				rep.ImplicitYesNo++
				p.warn(p.stepRow[i], i, warnImplicitYesNo, "Вопрос без вариантов ответа — добавлены кнопки «Да, расскажи» / «Нет, спасибо»")
				p.warn(p.stepRow[i], i, warnNoContent, "Ответа на «да» в ТЗ нет — в ветке заглушка вложения и запасной текст «Материал скоро добавим…»: загрузите материал и замените текст")
			}
			isQ := len(st.Buttons) > 0 || st.Survey != nil || st.Kind == domain.KindInput
			if i == b.first && isQ {
				rep.QuestionFirst++
			}
			if !isQ && !st.HRTask && i < end-1 {
				rep.NextButton++
			}
		}
	}
	media := map[string]bool{}
	addMedia := func(refs ...domain.MediaRef) {
		for _, r := range refs {
			if r.Name != "" && r.Personal == "" && !media[r.Name] {
				media[r.Name] = true
				rep.MediaNameList = append(rep.MediaNameList, r.Name)
			}
		}
	}
	addBranch := func(br *domain.Branch) {
		rep.Branches++
		for _, a := range br.Actions {
			addMedia(domain.MediaRef{Name: a.Media})
		}
		for _, m := range br.Messages {
			addMedia(m.Media...)
		}
	}
	for i := range steps {
		st := &steps[i]
		addMedia(st.Media...)
		for j := range st.Buttons {
			b := &st.Buttons[j]
			if b.Action == "pending_rows" {
				b.Action = ""
			}
			if b.Branch != nil {
				addBranch(b.Branch)
			}
		}
		if st.HRTask {
			rep.HRTasks++
		}
		if len(st.Buttons) > 0 {
			rep.Questions++
		}
		if st.Survey != nil {
			rep.Surveys++
			rep.SurveyQuestions += len(st.Survey.Questions)
			for _, q := range st.Survey.Questions {
				if q.Type == "choice" || q.Type == "multi" {
					rep.Questions++
				}
				for _, k := range sortedKeys(q.Branches) {
					addBranch(q.Branches[k])
				}
			}
			for _, o := range st.Survey.Outcomes {
				for _, m := range o.Messages {
					addMedia(m.Media...)
				}
			}
		}
		if !st.HRTask {
			switch addressForm(st.TextTy) {
			case "ty":
				ty++
			case "vy":
				vy++
			}
			if firstAddr == 0 && st.TextTy != "" {
				firstAddr = p.stepRow[i]
			}
		}
	}
	for _, r := range p.out.rules {
		addMedia(r.Media...)
	}
	p.collectCommands()
	p.postProcess()
	if ty+vy > 0 {
		p.warn(firstAddr, -1, warnAddressForm, fmt.Sprintf("Тексты ТЗ в одной форме обращения (на «ты» — %d, на «вы» — %d): вторая форма скопирована из найденной, перепишите её в редакторе", ty, vy))
	}
	rep.Messages = len(steps)
	rep.Rules = len(p.out.rules)
	rep.Commands = len(p.out.commands)
	rep.MediaNames = len(rep.MediaNameList)
	if rep.MediaNameList == nil {
		rep.MediaNameList = []string{}
	}
	rep.Warnings = make([]ImportWarning, len(p.warns))
	for i, w := range p.warns {
		if s := p.warnStep[i]; s >= 0 && s < len(steps) {
			w.StepKey, w.StepID = steps[s].Key, steps[s].ID.String()
		}
		rep.Warnings[i] = w
	}
	for _, code := range domain.SiteCodes {
		if p.siteSet[code] {
			p.out.sites = append(p.out.sites, code)
		}
	}
	if p.out.rules == nil {
		p.out.rules = []domain.ScenarioRule{}
	}
	if p.out.commands == nil {
		p.out.commands = []domain.Command{}
	}
}

// implicitYesNo — вопрос без вариантов (r11–r13, r20–r21, r37): кнопки «да/нет» и заготовка
// ветки «да» со ссылкой-заглушкой на материал. Перед опросом (r61) не трогаем — опрос и есть ответ.
func (p *tzParser) implicitYesNo(st *domain.ScenarioStep, nextSurvey bool) bool {
	if len(st.Buttons) > 0 || st.Survey != nil || st.HRTask || st.Kind == domain.KindInput || nextSurvey {
		return false
	}
	lines := splitLines(strings.TrimSpace(st.TextTy))
	if len(lines) == 0 || !strings.HasSuffix(strings.TrimSpace(lines[len(lines)-1]), "?") {
		return false
	}
	name := "Материал: " + truncateRunes(st.Title, 40)
	if len(st.Sites) > 0 {
		name += " (" + strings.Join(st.Sites, ", ") + ")"
	}
	yes := emptyBranch()
	// Текст-заглушка — HR видит его в редакторе и заменит; пока файла нет, бот пошлёт только его.
	yes.Messages = []domain.BranchMessage{{TextTy: textMaterialSoonTy, TextVy: textMaterialSoonVy,
		Media: domain.MediaRefList{{Name: name}}, Buttons: []domain.StepButton{}}}
	st.Buttons = domain.ButtonList{{Label: labelImplicitYes, Branch: yes}, {Label: labelImplicitNo}}
	return true
}

// collectCommands — «/Food», «/HR» в текстах → команды сценария. /hr — связаться с HR; другие —
// файлы ветки того же шага (r25: «Да» шлёт PDF «где пообедать» по площадке).
func (p *tzParser) collectCommands() {
	seen := map[string]bool{}
	for i, st := range p.out.steps {
		texts := []string{st.TextTy}
		var files []domain.Action
		for _, b := range st.Buttons {
			if b.Branch == nil {
				continue
			}
			for _, m := range b.Branch.Messages {
				texts = append(texts, m.TextTy)
			}
			for _, a := range b.Branch.Actions {
				if a.Type == domain.ActionSendFile {
					files = append(files, a)
				}
			}
		}
		if st.Survey != nil {
			for _, q := range st.Survey.Questions {
				for _, k := range sortedKeys(q.Branches) {
					for _, m := range q.Branches[k].Messages {
						texts = append(texts, m.TextTy)
					}
				}
			}
		}
		for _, t := range texts {
			for _, m := range reCommand.FindAllStringSubmatch(t, -1) {
				name := "/" + strings.ToLower(m[1])
				if seen[name] {
					continue
				}
				seen[name] = true
				c := domain.Command{Name: name, Actions: []domain.Action{}}
				switch {
				case name == "/hr":
					c.Actions = append(c.Actions, domain.Action{Type: domain.ActionHandoff, To: "hr"})
				case len(files) > 0:
					c.Actions = append(c.Actions, files...)
				default:
					p.warn(p.stepRow[i], i, warnCommand, fmt.Sprintf("Команда %s упомянута в тексте, но что она делает, из ТЗ не ясно — настройте действие", name))
				}
				p.out.commands = append(p.out.commands, c)
			}
		}
	}
}

func sortedKeys(m map[string]*domain.Branch) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keyLess(keys[j], keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func keyLess(a, b string) bool {
	x, e1 := strconv.Atoi(a)
	y, e2 := strconv.Atoi(b)
	if e1 == nil && e2 == nil {
		return x < y
	}
	return a < b
}

// addressFormBranch — кнопки «На ТЫ» / «На ВЫ» (r6) ставят форму обращения новичка.
func addressFormBranch(label string, br *domain.Branch) *domain.Branch {
	lo := strings.ToLower(strings.TrimSpace(label))
	var v string
	switch {
	case strings.HasSuffix(lo, " ты") || lo == "ты":
		v = "ty"
	case strings.HasSuffix(lo, " вы") || lo == "вы":
		v = "vy"
	default:
		return br
	}
	if br == nil {
		br = emptyBranch()
	}
	br.Actions = append(br.Actions, domain.Action{Type: domain.ActionSetField, Field: "address_form", Value: v})
	return br
}

func splitMediaNames(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ';' }) {
		if p := strings.Join(strings.Fields(part), " "); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstLineTitle(text string) string {
	for _, l := range splitLines(text) {
		if t := strings.TrimSpace(l); t != "" {
			return truncateRunes(t, 40)
		}
	}
	return ""
}

func isPlaceholder(text string) bool {
	t := strings.TrimSpace(text)
	return t != "" && len([]rune(t)) < 60 && (strings.HasSuffix(t, "...") || strings.HasSuffix(t, "…"))
}

func sitesEqual(a, b domain.StringList) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !b.Has(s) {
			return false
		}
	}
	return true
}

// Тексты команд сценария по умолчанию (кабинет их правит).
var defaultCommandTexts = map[string][2]string{ // ty, vy
	"/food": {"Лови список мест, где можно пообедать рядом с работой 🍽", "Вот список мест, где можно пообедать рядом с работой 🍽"},
	"/hr":   {"Соединяю с HR — напиши вопрос, ответ придёт сюда же.", "Соединяю с HR — напишите вопрос, ответ придёт сюда же."},
}

// defaultCommandHelp — строка команды в тексте /help, подставленном в заглушку r3.
var defaultCommandHelp = map[string]string{"/food": "подскажу, где пообедать", "/hr": "свяжу с HR"}

var (
	reTomorrow = regexp.MustCompile(`(^|[^\p{L}])([Зз])автра([^\p{L}]|$)`)
	reGreeting = regexp.MustCompile(`(?i)^\s*(привет|здравствуй|добрый\s+день)`)
	reNiceMeet = regexp.MustCompile(`(?i)приятно\s+(?:с\s+\p{L}+\s+)?познакомит`)
)

// isAddressQuestion — все кнопки шага ставят форму обращения (r5/r6).
func isAddressQuestion(st domain.ScenarioStep) bool {
	if len(st.Buttons) == 0 {
		return false
	}
	for _, b := range st.Buttons {
		ok := false
		if b.Branch != nil {
			for _, a := range b.Branch.Actions {
				ok = ok || (a.Type == domain.ActionSetField && a.Field == "address_form")
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// postProcess — правки после сборки блоков (06 §1.4, §1.8, §1.11; замечания оркестратора):
// вопрос ТЗ о форме обращения и реакция на него покрыты знакомством; заглушка «Что умеет этот
// бот…» — текст /help; «завтра» в D−1 — {start_day}; тексты команд; двойное приветствие;
// «опрос» в поводе без опроса в блоке.
func (p *tzParser) postProcess() {
	steps := p.out.steps
	for bi, b := range p.blocks {
		end := len(steps)
		if bi+1 < len(p.blocks) {
			end = p.blocks[bi+1].first
		}
		hasSurvey, surveyWord := false, -1
		for i := b.first; i < end; i++ {
			st := &steps[i]
			if st.Survey != nil {
				hasSurvey = true
			}
			if surveyWord < 0 && reSurveyWord.MatchString(st.Title) {
				surveyWord = i
			}
			if isAddressQuestion(*st) {
				c := domain.ConditionAddressUnset
				st.Condition = &c
				p.warn(p.stepRow[i], i, warnOnboarding, "Форму обращения спрашивает встроенное знакомство до первого шага — этот вопрос уйдёт, только если форма не выбрана")
				if i+1 < end && len(steps[i+1].Buttons) == 0 && reNiceMeet.MatchString(steps[i+1].TextTy) {
					c2 := domain.ConditionAddressUnset
					steps[i+1].Condition = &c2
					p.warn(p.stepRow[i+1], i+1, warnOnboarding, "Реакцию на выбор формы («очень приятно») уже говорит встроенное знакомство — шаг уйдёт, только если форма не выбрана")
				}
			}
			if st.DayOffset == -1 && reTomorrow.MatchString(st.TextTy) {
				fix := func(t string) string {
					return reTomorrow.ReplaceAllStringFunc(t, func(m string) string {
						sm := reTomorrow.FindStringSubmatch(m)
						ph := "{start_day}"
						if sm[2] == "З" {
							ph = "{Start_day}"
						}
						return sm[1] + ph + sm[3]
					})
				}
				st.TextTy, st.TextVy = fix(st.TextTy), fix(st.TextVy)
				p.warn(p.stepRow[i], i, warnStartDay, "«Завтра» заменено на {start_day}: при выходе в понедельник шаг D−1 уходит в пятницу, и бот скажет «в понедельник, …», а не «завтра»")
			}
			if i > b.first && reGreeting.MatchString(st.TextTy) && reGreeting.MatchString(steps[i-1].TextTy) &&
				steps[i-1].Survey == nil && sitesEqual(steps[i-1].Sites, st.Sites) {
				p.warn(p.stepRow[i], i, warnDupGreeting, "Два приветствия подряд в одном блоке — новичок получит «Привет» дважды; объедините или уберите одно")
			}
		}
		if surveyWord >= 0 && !hasSurvey {
			p.warn(p.stepRow[surveyWord], surveyWord, warnSurveyMissing, "В поводе указан опрос, но вопросов опроса в этом блоке нет — добавьте опрос или поправьте повод")
		}
	}
	// Заглушка «Что умеет этот бот…» (r3) — текст /help и команды сценария.
	for i := range steps {
		st := &steps[i]
		if !isPlaceholder(st.TextTy) || !strings.Contains(strings.ToLower(st.TextTy), "умеет") {
			continue
		}
		ty, vy := textHelpTy, textHelpVy
		for _, c := range p.out.commands {
			what, ok := defaultCommandHelp[c.Name]
			if !ok {
				what = strings.ToLower(commandDescription(c))
			}
			line := "\n• " + c.Name + " — " + what
			ty += line
			vy += line
		}
		st.TextTy, st.TextVy = ty, vy
		p.warn(p.stepRow[i], i, warnFromHelp, "В ТЗ здесь заглушка — подставлен текст /help бота; проверьте и поправьте")
	}
	for i := range p.out.commands {
		c := &p.out.commands[i]
		if t, ok := defaultCommandTexts[c.Name]; ok {
			c.TextTy, c.TextVy = t[0], t[1]
		}
	}
}

var rePDFHead = regexp.MustCompile(`(?i)^\s*(?:приложить\s+)?(?:пдф-?\p{L}*|pdf)\s*`)

// pdfMediaName — имя записи медиатеки для «#Приложить пдф-ку с …»: «PDF — с …».
func pdfMediaName(attach string) string {
	rest := strings.TrimSpace(rePDFHead.ReplaceAllString(attach, ""))
	if rest == "" {
		return "PDF"
	}
	return "PDF — " + truncateRunes(rest, 80)
}
