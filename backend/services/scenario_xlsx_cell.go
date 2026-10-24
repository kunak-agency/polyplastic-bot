package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"main/domain"
)

// Разбор ячейки «Текст сообщения» Excel-ТЗ (вердикт §1): внутри прозой записаны варианты
// ответов, ветки «Если ответ X: …», опросы, вложения, гиперссылки и альтернативы текста.

// labelLongRunes — подпись кнопки длиннее — предупреждение label_long (жёлтое, публикацию не
// блокирует). Лимита на длину подписи у MAX нет (до 7 кнопок в ряд, до 30 рядов): длинную
// подпись клиент сам обрезает на экране, поэтому импорт сохраняет подпись ТЗ целиком.
const labelLongRunes = 24

// Подписи терминальных кнопок пары «передать через Корсика / связаться с рекрутером».
const (
	labelEscalate      = "Описать проблему"
	labelChatRecruiter = "Чат с рекрутером"
	labelChatHR        = "Чат с HR"
)

// escalateButton — «передать через Корсика»: бот ждёт текст новичка → обращение по route.
// Каноническая форма, как пишет редактор: действие в branch.actions + opens_free_text. Старую
// строковую форму action «escalate:<route>» бот тоже понимает (branch.go, actionOf).
func escalateButton(label, route string) domain.StepButton {
	return domain.StepButton{Label: label, OpensFreeText: true, Branch: &domain.Branch{
		Messages: []domain.BranchMessage{}, Then: domain.ThenContinue,
		Actions: []domain.Action{{Type: domain.ActionEscalate, Route: route}},
	}}
}

// handoffButton — «связаться с рекрутером / HR»: обращение-переписка через бота.
func handoffButton(label, to string) domain.StepButton {
	return domain.StepButton{Label: label, Branch: &domain.Branch{
		Messages: []domain.BranchMessage{}, Then: domain.ThenContinue,
		Actions: []domain.Action{{Type: domain.ActionHandoff, To: to}},
	}}
}

// longLabelWarn — label_long для подписей длиннее labelLongRunes.
func longLabelWarn(labels ...string) *cellWarn {
	var long []string
	for _, l := range labels {
		if utf8.RuneCountInString(l) > labelLongRunes {
			long = append(long, "«"+l+"»")
		}
	}
	if len(long) == 0 {
		return nil
	}
	return &cellWarn{"label_long", fmt.Sprintf("Подписи кнопок длиннее %d символов: %s — в чате MAX они видны, на узком экране клиент их обрежет; сократите в редакторе, если нужно", labelLongRunes, strings.Join(long, ", "))}
}

type cellWarn struct{ code, msg string }

type cellOption struct {
	label string
	other bool // «Другое: свой вариант» → opens_free_text
}

// parsedCell — результат разбора ячейки.
type parsedCell struct {
	text     string
	alt      *string
	options  []cellOption
	branches map[int]*domain.Branch // по индексу варианта
	pending  map[int]bool           // «Если да: Следующая реплика» — ветка строками ниже
	tail     string                 // общий хвост после веток → следующий шаг блока
	survey   *domain.Survey
	outcomes []rawOutcome
	attach   []string // строки «# Приложить …» (удалены из текста)
	warns    []cellWarn
}

var (
	reLinkHint    = regexp.MustCompile(`(?is)\(\s*наложить\s+на\s+слово\s*["«“]([^"»”]+)["»”]\s*гиперссылку\s+на\s+(\S+?)\s*\)`)
	reAttachLine  = regexp.MustCompile(`(?i)^\s*(#|\+\s*открытк)`)
	reAltLine     = regexp.MustCompile(`(?i)^\s*([12])\.\s*вариант\s*:\s*(.*)$`)
	reNumItem     = regexp.MustCompile(`^\s*(\d+)[.)]\s*(.*)$`)
	reDashLine    = regexp.MustCompile(`^\s*[-–—]\s*(.+)$`)
	reOptLine     = regexp.MustCompile(`^\s*(?:(\d+)\)|([А-ГA-D])\)|[-–—])\s*(.+)$`)
	reOptNoHeader = regexp.MustCompile(`^\s*(?:(\d+)\)|([А-ГA-D])\))\s*(.+)$`)
	reOptHeader   = regexp.MustCompile(`(?i)^\s*варианты\s+ответов\s*:?\s*$`)
	reIfHeader    = regexp.MustCompile(`(?i)^\s*если\s+(?:ответили\s+|ответ\s+)?(\d+\s*-\s*\d+|\d+|[А-ГA-D]|да|нет)\s*(?::|\s-\s|-)\s*(.*)$`)
	reGoNext      = regexp.MustCompile(`(?i)\s*(?:далее\s+)?переход\s+(?:ко?\s+)?(?:\d+\s+)?(?:вопрос\p{L}*|следующ\p{L}*)[^.]*\.?\s*$`)
	reGoNextLine  = regexp.MustCompile(`(?i)^\s*(?:сразу\s+после\s+ответа\s+|далее\s+)?переход(?:\s|$)`)
	reQuoted      = regexp.MustCompile(`["«“]([^"»”]+)["»”]`)
	reScaleAnchor = regexp.MustCompile(`1\s*[-–]\s*([^,;()+]+?)[,;]?\s+10\s*[-–]\s*([^()+.]+?)\s*(?:\)|\+|\.|$)`)
	reBelow       = regexp.MustCompile(`(?i)ниже\s+(\d+)`)
	reOutcomeHead = regexp.MustCompile(`(?i)^\s*если\s+на\s+(\p{L}+)\s+вопрос.*?(\d+)\s*-?\p{L}*\s+(?:вариант|ответ)`)
	reNameX       = regexp.MustCompile(`(^|[^\p{L}])Х([^\p{L}]|$)`)
	reTyWords     = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(ты|тебя|тебе|тобой|твой|твоя|твое|твоё|твои|твоего|твоей|твоих|твоим)(?:[^\p{L}]|$)`)
	reVyWords     = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(вы|вас|вам|вами|ваш|ваша|ваше|ваши|вашего|вашей|ваших|вашим)(?:[^\p{L}]|$)`)
	reHRTaskHead  = regexp.MustCompile(`(?i)^\s*(?:отправить|направить|выслать)\s.*(?:письм|e-?mail|почт)`)
	reBlankLines  = regexp.MustCompile(`\n{3,}`)
	// reServiceInline — служебная фраза ветки посреди строки.
	reServiceInline = regexp.MustCompile(`(?i)(?:возможность|опции)\s+для\s+сотрудника`)
)

// normalizeCell — переводы строк, неразрывные пробелы, хвостовые пробелы строк.
func normalizeCell(s string) string {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", " ", " ").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(reBlankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// withName — плейсхолдер «Х» и «Имя кандидата» → {Имя}.
func withName(s string) string {
	s = strings.ReplaceAll(s, "Имя кандидата", "{Имя}")
	for {
		n := reNameX.ReplaceAllString(s, "${1}{Имя}${2}")
		if n == s {
			return s
		}
		s = n
	}
}

// addressForm — «ty», «vy» или пусто по обращению в тексте.
func addressForm(s string) string {
	ty, vy := len(reTyWords.FindAllString(s, -1)), len(reVyWords.FindAllString(s, -1))
	switch {
	case ty > vy:
		return "ty"
	case vy > ty:
		return "vy"
	}
	return ""
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinTrim(lines []string) string { return strings.TrimSpace(strings.Join(lines, "\n")) }

// parseCell — разбор текста ячейки.
func parseCell(raw string) parsedCell {
	var pc parsedCell
	s := withName(normalizeCell(raw))

	// Гиперссылка на слове: «(наложить на слово "X" гиперссылку на URL)» → [X](URL).
	if m := reLinkHint.FindStringSubmatchIndex(s); m != nil {
		word, url := s[m[2]:m[3]], s[m[4]:m[5]]
		s = normalizeCell(s[:m[0]] + s[m[1]:])
		if i := strings.Index(s, word); i >= 0 {
			s = s[:i] + "[" + word + "](" + url + ")" + s[i+len(word):]
		} else {
			s += "\n" + url
		}
		pc.warns = append(pc.warns, cellWarn{"link_in_text", fmt.Sprintf("Гиперссылка на слове «%s» оформлена разметкой [текст](ссылка) — проверьте, что MAX её показывает", word)})
	}

	// «# Приложить …», «#Адаптационный план», «+ОТКРЫТКА …» — вложения, из текста убираем.
	var kept []string
	for _, l := range splitLines(s) {
		if reAttachLine.MatchString(l) {
			pc.attach = append(pc.attach, strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#+ ")))
			continue
		}
		kept = append(kept, l)
	}
	lines := splitLines(normalizeCell(strings.Join(kept, "\n")))

	// Правила после опроса: «ОТВЕТЫ: … + … ЛИБО …».
	if len(lines) > 0 && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(lines[0])), "ОТВЕТЫ") {
		pc.outcomes = parseOutcomes(lines[1:], &pc)
		return pc
	}

	// Авторские альтернативы: «1. Вариант: … 2. Вариант: …» + общий текст.
	if alt, text, ok := splitAlternatives(lines); ok {
		pc.text, pc.alt = text, &alt
		return pc
	}

	// Открытый вопрос.
	for i, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l), "открытый вопрос") {
			q := joinTrim(append(append([]string{}, lines[:i]...), lines[i+1:]...))
			pc.survey = &domain.Survey{Scale: domain.Scale10, Questions: []domain.SurveyQuestion{
				{Text: q, Options: []string{}, Type: "text"},
			}}
			return pc
		}
	}

	if sv, intro, ok := parseSurveyItems(lines); ok {
		pc.survey, pc.text = sv, intro
		return pc
	}

	text, opts, rest := splitOptions(lines)
	pc.text = text
	if len(opts) == 0 {
		pc.text = joinTrim(lines)
		return pc
	}
	for _, o := range opts {
		label := o
		other := false
		lo := strings.ToLower(o)
		if strings.HasPrefix(lo, "другое") || strings.HasPrefix(lo, "свой вариант") {
			if i := strings.Index(o, ":"); i > 0 {
				label = strings.TrimSpace(o[:i])
			}
			other = true
		}
		// Подпись ТЗ целиком: длинная — предупреждение label_long при сборке шага.
		pc.options = append(pc.options, cellOption{label: label, other: other})
	}
	pc.parseBranches(rest)
	return pc
}

// splitAlternatives — r8: первый вариант — текст, второй — alt_text; общий хвост у обоих.
func splitAlternatives(lines []string) (alt, text string, ok bool) {
	var v1, v2 string
	end := -1
	for i, l := range lines {
		m := reAltLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if m[1] == "1" {
			v1 = m[2]
		} else {
			v2, end = m[2], i
		}
	}
	if v1 == "" || v2 == "" {
		return "", "", false
	}
	tail := joinTrim(lines[end+1:])
	join := func(v string) string {
		if tail == "" {
			return strings.TrimSpace(v)
		}
		return strings.TrimSpace(v) + "\n\n" + tail
	}
	return join(v2), join(v1), true
}

// numItem — пункт нумерованного списка с продолжением.
type numItem struct {
	head  string
	lines []string // продолжение (без пустых)
}

func numberedItems(lines []string) (intro []string, items []numItem) {
	next := 1
	for _, l := range lines {
		if m := reNumItem.FindStringSubmatch(l); m != nil {
			if n, _ := strconv.Atoi(m[1]); n == next {
				items = append(items, numItem{head: strings.TrimSpace(m[2])})
				next++
				continue
			}
		}
		if len(items) == 0 {
			intro = append(intro, l)
		} else if strings.TrimSpace(l) != "" {
			items[len(items)-1].lines = append(items[len(items)-1].lines, l)
		}
	}
	return intro, items
}

func hasTypeMarker(s string) bool {
	lo := strings.ToLower(s)
	return strings.Contains(lo, "да/нет") || strings.Contains(lo, "шкал") || reBelow.MatchString(s) ||
		reScaleAnchor.MatchString(s)
}

// parseSurveyItems — опрос из нумерованных пунктов: с вариантами через дефис (choice/multi)
// или с маркерами типов (да/нет, шкала 1–10, «ниже 8»).
func parseSurveyItems(lines []string) (*domain.Survey, string, bool) {
	intro, items := numberedItems(lines)
	if len(items) < 2 {
		return nil, "", false
	}
	withDash, withMarker := 0, 0
	for _, it := range items {
		dash := 0
		for _, l := range it.lines {
			if reDashLine.MatchString(l) {
				dash++
			}
		}
		if dash >= 2 {
			withDash++
		}
		if hasTypeMarker(it.head + " " + strings.Join(it.lines, " ")) {
			withMarker++
		}
	}
	switch {
	case withDash == len(items):
		sv := &domain.Survey{Scale: domain.ScaleChoice}
		for _, it := range items {
			q := domain.SurveyQuestion{Text: it.head, Type: "choice", Options: []string{}}
			if lo := strings.ToLower(q.Text); strings.Contains(lo, "несколько вариант") {
				q.Type = "multi"
				q.Text = strings.TrimSpace(regexp.MustCompile(`(?i)\s*\(\s*несколько вариант[^)]*\)`).ReplaceAllString(q.Text, ""))
			}
			for _, l := range it.lines {
				m := reDashLine.FindStringSubmatch(l)
				if m == nil {
					continue
				}
				o := strings.TrimSpace(m[1])
				lo := strings.ToLower(o)
				if strings.HasPrefix(lo, "свой вариант") || strings.HasPrefix(lo, "другое") {
					q.Other = true
					continue
				}
				q.Options = append(q.Options, o)
			}
			sv.Questions = append(sv.Questions, q)
		}
		return sv, joinTrim(intro), true
	case withMarker*2 >= len(items):
		sv := &domain.Survey{Scale: domain.Scale10}
		for _, it := range items {
			q := scaleQuestion(it.head + " " + strings.Join(it.lines, " "))
			if q.Threshold != nil && sv.Threshold == 0 {
				sv.Threshold = *q.Threshold
			}
			sv.Questions = append(sv.Questions, q)
		}
		return sv, joinTrim(intro), true
	}
	return nil, "", false
}

// scaleQuestion — пункт блока шкал: тип, порог, подписи краёв, когда просить комментарий.
func scaleQuestion(s string) domain.SurveyQuestion {
	s = strings.Join(strings.Fields(s), " ")
	lo := strings.ToLower(s)
	q := domain.SurveyQuestion{Options: []string{}}
	switch {
	case strings.Contains(lo, "да/нет"):
		q.Type = "yesno"
		q.Scale = domain.ScaleYesNo
		if strings.Contains(lo, "отрицательн") || strings.Contains(lo, "ответа нет") || strings.Contains(lo, "комментар") {
			q.Comment = "on_no"
		}
	case reScaleAnchor.MatchString(s) || strings.Contains(lo, "шкал"):
		q.Type = "scale"
		q.Scale = domain.Scale10
		if m := reScaleAnchor.FindStringSubmatch(s); m != nil {
			q.Anchors = []string{strings.TrimSpace(m[1]), strings.TrimSpace(m[2])}
		}
		if m := reBelow.FindStringSubmatch(s); m != nil {
			n, _ := strconv.Atoi(m[1])
			q.Threshold = &n
			q.Comment = "below"
		}
	default:
		q.Type = "text"
	}
	if strings.Contains(lo, "обязательное поле") {
		q.Required = true
	}
	if q.Type == "scale" && q.Comment == "" && strings.Contains(lo, "комментар") {
		q.Comment = "optional"
	}
	q.Text = questionText(s)
	return q
}

// questionText — формулировка без служебного хвоста: до «?» включительно, иначе до «(»,
// «+ поле», «1 -», «Обязательное поле».
func questionText(s string) string {
	if i := strings.Index(s, "?"); i > 0 {
		return strings.TrimSpace(s[:i+1])
	}
	cut := len(s)
	for _, stop := range []string{" (", "(", "+ поле", "+поле", " 1 -", " 1 –", "Обязательное поле", ". В случае"} {
		if i := strings.Index(s, stop); i > 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimRight(strings.TrimSpace(s[:cut]), ".")
}

// splitOptions — текст до вариантов, варианты, остаток (ветки и хвост).
func splitOptions(lines []string) (text string, opts []string, rest []string) {
	header := -1
	for i, l := range lines {
		if reOptHeader.MatchString(l) {
			header = i
			break
		}
	}
	if header >= 0 {
		i := header + 1
		for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
			i++
		}
		for ; i < len(lines); i++ {
			m := reOptLine.FindStringSubmatch(lines[i])
			if m == nil {
				break
			}
			opts = append(opts, strings.TrimSpace(m[3]))
		}
		if len(opts) >= 2 {
			return joinTrim(lines[:header]), opts, lines[i:]
		}
		opts = nil
	}
	// Без заголовка: подряд «1) 2) 3)» или «А) Б) В)».
	for i := 0; i < len(lines); i++ {
		if !reOptNoHeader.MatchString(lines[i]) {
			continue
		}
		j := i
		for j < len(lines) && reOptNoHeader.MatchString(lines[j]) {
			opts = append(opts, strings.TrimSpace(reOptNoHeader.FindStringSubmatch(lines[j])[3]))
			j++
		}
		if len(opts) >= 2 {
			return joinTrim(lines[:i]), opts, lines[j:]
		}
		opts = nil
	}
	return joinTrim(lines), nil, nil
}

// optionIndexes — «1-2», «3», «Б», «да» → индексы вариантов.
func (pc *parsedCell) optionIndexes(sel string) []int {
	sel = strings.ToLower(strings.TrimSpace(sel))
	if a, b, ok := strings.Cut(sel, "-"); ok {
		x, e1 := strconv.Atoi(strings.TrimSpace(a))
		y, e2 := strconv.Atoi(strings.TrimSpace(b))
		if e1 != nil || e2 != nil {
			return nil
		}
		var out []int
		for n := x; n <= y; n++ {
			if n >= 1 && n <= len(pc.options) {
				out = append(out, n-1)
			}
		}
		return out
	}
	if n, err := strconv.Atoi(sel); err == nil {
		if n >= 1 && n <= len(pc.options) {
			return []int{n - 1}
		}
		return nil
	}
	if sel == "да" || sel == "нет" {
		var out []int
		for i, o := range pc.options {
			if strings.HasPrefix(strings.ToLower(o.label), sel) {
				out = append(out, i)
			}
		}
		return out
	}
	letters := map[string]int{"а": 0, "б": 1, "в": 2, "г": 3, "a": 0, "b": 1, "c": 2, "d": 3}
	if n, ok := letters[sel]; ok && n < len(pc.options) {
		return []int{n}
	}
	return nil
}

// parseBranches — «Если ответ X: …» после вариантов. Абзацы после последней ветки, не похожие
// на её служебные строки, — общий хвост (следующий шаг блока).
func (pc *parsedCell) parseBranches(rest []string) {
	type section struct {
		sel  string
		body []string
	}
	var secs []section
	var pre []string
	for _, l := range rest {
		if m := reIfHeader.FindStringSubmatch(l); m != nil {
			secs = append(secs, section{sel: m[1], body: []string{m[2]}})
			continue
		}
		if len(secs) == 0 {
			if !reGoNextLine.MatchString(l) {
				pre = append(pre, l)
			}
			continue
		}
		secs[len(secs)-1].body = append(secs[len(secs)-1].body, l)
	}
	var tail []string
	if strings.TrimSpace(strings.Join(pre, "")) != "" {
		tail = append(tail, pre...)
	}
	if n := len(secs); n > 0 {
		body, after := splitBranchTail(secs[n-1].body)
		secs[n-1].body = body
		tail = append(tail, after...)
	}
	pc.tail = joinTrim(tail)
	for _, sec := range secs {
		idx := pc.optionIndexes(sec.sel)
		if len(idx) == 0 {
			pc.warns = append(pc.warns, cellWarn{"unknown_branch", fmt.Sprintf("Ветка «Если %s» не сопоставлена с вариантом ответа — текст перенесён в сообщение", sec.sel)})
			pc.tail = joinTrim([]string{pc.tail, joinTrim(sec.body)})
			continue
		}
		br, pending, warns := parseBranchBody(sec.body)
		pc.warns = append(pc.warns, warns...)
		for _, i := range idx {
			if pending {
				if pc.pending == nil {
					pc.pending = map[int]bool{}
				}
				pc.pending[i] = true
			}
			if br == nil {
				continue
			}
			if pc.branches == nil {
				pc.branches = map[int]*domain.Branch{}
			}
			cp := cloneBranch(*br)
			if len(idx) > 1 {
				cp.On = append([]int{}, idx...) // общая ветка «1–2»: индексы кнопок с нуля
			}
			pc.branches[i] = &cp
		}
	}
}

func isBranchService(p string) bool {
	lo := strings.ToLower(strings.TrimSpace(p))
	return strings.HasPrefix(lo, "опции") || strings.HasPrefix(lo, "возможность") ||
		strings.HasPrefix(lo, "далее") || strings.HasPrefix(lo, "сразу") || strings.HasPrefix(lo, "если")
}

// splitBranchTail — первый абзац и служебные абзацы — ветке, остальное — хвост.
func splitBranchTail(body []string) (keep, tail []string) {
	para, first := 0, true
	inTail := false
	for _, l := range body {
		if strings.TrimSpace(l) == "" {
			if !first {
				para++
			}
			if inTail {
				tail = append(tail, l)
			} else {
				keep = append(keep, l)
			}
			continue
		}
		if !inTail && para > 0 && !isBranchService(l) && !first {
			inTail = true
		}
		first = false
		if inTail {
			tail = append(tail, l)
		} else {
			keep = append(keep, l)
		}
	}
	return keep, tail
}

// parseBranchBody — текст ветки, терминальные кнопки и действия.
func parseBranchBody(body []string) (*domain.Branch, bool, []cellWarn) {
	var text []string
	var buttons []domain.StepButton
	var actions []domain.Action
	var warns []cellWarn
	pending := false
	// «… текст. Возможность для сотрудника …» в одной строке — служебную часть отдельно.
	var split []string
	for _, l := range body {
		if loc := reServiceInline.FindStringIndex(l); loc != nil && loc[0] > 0 {
			split = append(split, l[:loc[0]], l[loc[0]:])
			continue
		}
		split = append(split, l)
	}
	for _, l := range split {
		t := strings.TrimSpace(l)
		lo := strings.ToLower(t)
		switch {
		case lo == "следующая реплика":
			pending = true
		case reGoNextLine.MatchString(t):
		case strings.HasPrefix(lo, "опции"):
			buttons = append(buttons, escalationButtons(t)...)
		case strings.HasPrefix(lo, "возможность"):
			if strings.Contains(lo, "ответить в чате") || strings.Contains(lo, "написать") {
				actions = append(actions, domain.Action{Type: "open_free_text"})
			}
			if m := reQuoted.FindStringSubmatch(t); m != nil {
				to, label := "recruiter", upperFirst(m[1])
				if strings.Contains(strings.ToLower(m[1]), "hr") {
					to = "hr"
				}
				if w := longLabelWarn(label); w != nil {
					warns = append(warns, *w)
				}
				buttons = append(buttons, handoffButton(label, to))
			}
		default:
			text = append(text, reGoNext.ReplaceAllString(l, ""))
		}
	}
	msg := joinTrim(text)
	if msg == "" && len(buttons) == 0 && len(actions) == 0 {
		return nil, pending, warns
	}
	// «Ответить в чате с ботом либо нажать кнопку … рекрутеру» (r63): текст уходит тому же адресату,
	// что и кнопка, — escalate с её маршрутом перед open_free_text.
	if len(actions) > 0 && actions[0].Type == domain.ActionOpenFreeText {
		for _, b := range buttons {
			if b.Branch != nil && len(b.Branch.Actions) > 0 && b.Branch.Actions[0].Type == domain.ActionHandoff {
				actions = append([]domain.Action{{Type: domain.ActionEscalate, Route: b.Branch.Actions[0].To}}, actions...)
				break
			}
		}
	}
	br := &domain.Branch{Actions: actions, Then: "continue"}
	if br.Actions == nil {
		br.Actions = []domain.Action{}
	}
	if msg != "" || len(buttons) > 0 {
		if buttons == nil {
			buttons = []domain.StepButton{}
		}
		br.Messages = []domain.BranchMessage{{TextTy: msg, TextVy: msg, Media: domain.MediaRefList{}, Buttons: buttons}}
	} else {
		br.Messages = []domain.BranchMessage{}
	}
	return br, pending, warns
}

// escalationButtons — «Опции для сотрудника: передать проблему … через Корсика, связаться с
// рекрутером» → терминальные кнопки.
func escalationButtons(line string) []domain.StepButton {
	lo := strings.ToLower(line)
	var out []domain.StepButton
	if strings.Contains(lo, "через корсика") {
		route := "recruiter"
		if strings.Contains(lo, " в hr") {
			route = "hr"
		}
		out = append(out, escalateButton(labelEscalate, route))
	}
	switch {
	case strings.Contains(lo, "связаться с рекрутером"):
		out = append(out, handoffButton(labelChatRecruiter, "recruiter"))
	case strings.Contains(lo, "связаться с hr"):
		out = append(out, handoffButton(labelChatHR, "hr"))
	}
	return out
}

// rawOutcome — правило после опроса до привязки к типам вопросов: Q (с 1, 0 — всегда),
// Option (с 1), Group — взаимоисключение «ЛИБО».
type rawOutcome struct {
	q, option int
	group     string
	branch    domain.Branch
}

// parseOutcomes — «ОТВЕТЫ:» после опроса: «+» — независимые правила, «ЛИБО» — одна группа.
func parseOutcomes(lines []string, pc *parsedCell) []rawOutcome {
	type seg struct {
		lines []string
		group int
	}
	group := 0
	segs := []seg{{group: group}}
	var orGroups = map[int]bool{}
	for _, l := range lines {
		t := strings.ToUpper(strings.TrimSpace(l))
		switch t {
		case "+":
			group++
			segs = append(segs, seg{group: group})
			continue
		case "ЛИБО":
			orGroups[group] = true
			segs = append(segs, seg{group: group})
			continue
		}
		segs[len(segs)-1].lines = append(segs[len(segs)-1].lines, l)
	}
	var out []rawOutcome
	for _, sg := range segs {
		if joinTrim(sg.lines) == "" {
			continue
		}
		o := rawOutcome{}
		if orGroups[sg.group] {
			o.group = fmt.Sprintf("g%d", sg.group)
		}
		body := sg.lines
		for i, l := range sg.lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			if m := reOutcomeHead.FindStringSubmatch(l); m != nil {
				o.q = ordinals[strings.ToLower(m[1])]
				o.option, _ = strconv.Atoi(m[2])
				body = sg.lines[i+1:]
			}
			break
		}
		br, _, warns := parseBranchBody(body)
		pc.warns = append(pc.warns, warns...)
		if br == nil {
			continue
		}
		o.branch = *br
		out = append(out, o)
	}
	return out
}

func cloneBranch(b domain.Branch) domain.Branch {
	c := b
	c.Actions = append([]domain.Action{}, b.Actions...)
	c.On = append([]int(nil), b.On...)
	c.Messages = make([]domain.BranchMessage, len(b.Messages))
	for i, m := range b.Messages {
		m.Buttons = append([]domain.StepButton{}, m.Buttons...)
		m.Media = append(domain.MediaRefList{}, m.Media...)
		c.Messages[i] = m
	}
	return c
}

func upperFirst(s string) string {
	s = strings.TrimSpace(s)
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[n:]
}
