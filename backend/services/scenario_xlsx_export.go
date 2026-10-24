package services

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/pkg/errs"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// Экспорт сценария обратно в Excel формата ТЗ заказчика (api_contract.md, v4 — экспорт). Лист 1
// пишется прозой ТЗ так, чтобы импорт (scenario_xlsx_tz.go, scenario_xlsx_cell.go) разобрал его
// в те же шаги: колонки A–J как у заказчика, варианты «Варианты ответов: 1) …», ветки «Если ответ
// N: …», опросы, «ОТВЕТЫ: … + … ЛИБО …», правила и команды отдельными строками. То, чего проза ТЗ
// не выражает, уходит на лист «Корсик — расширения» (scenario_xlsx_ext.go): импорт читает его
// обратно, и круговой путь «импорт → экспорт → импорт» не теряет полей.

const (
	exportMainSheet = "Сценарий"
	msgVersionNF    = "Версия сценария не найдена"
)

// Колонки листа ТЗ (A–J).
const (
	colNum = iota
	colWhen
	colTitle
	colSites
	colKind
	colText
	colRequired
	colFlag
	colMedia
	colExtra
	tzCols
)

var tzHeader = [tzCols]string{"№ п/п", "Месяц/день/время информирования", "Информационный повод", "Тег сотрудника",
	"Форма/Макет", "Текст сообщения", "Обязательный этап", "", "Сопроводительное изображение", ""}

// Подписи и служебные фразы, которые импорт узнаёт (scenario_xlsx_cell.go, scenario_xlsx_tz.go).
const (
	tzRecruiterFlag = "по отмашке рекрутера"
	tzHRTaskHead    = "Отправить письмо кандидату на почту"
	tzOptionsHead   = "Варианты ответов:"
	tzOther         = "Свой вариант"
	tzOpenQuestion  = "Открытый вопрос"
	tzAnswersHead   = "ОТВЕТЫ:"
	tzPersonalPlan  = "#Адаптационный план"
)

var kindLabels = map[string]string{
	domain.KindText: "Текст", domain.KindCard: "Карточка", domain.KindFile: "Файл",
	domain.KindSurvey: "Опрос бота", domain.KindTask: "Задача", domain.KindInput: "Ввод ответа",
}

// ScenarioExport — файл экспорта.
type ScenarioExport struct {
	FileName string
	Data     []byte
}

// exportInput — что выгружаем: черновик или опубликованная версия.
type exportInput struct {
	Name     string
	Label    string // «черновик» | «версия 3 от 24.10.2026»
	Sites    []string
	Steps    []domain.ScenarioStep
	Rules    []domain.ScenarioRule
	Commands []domain.Command
}

// ExportExcel — сценарий в .xlsx формата ТЗ. version 0 — черновик, иначе опубликованная версия.
func (s *ScenarioService) ExportExcel(ctx context.Context, id uuid.UUID, version int) (*ScenarioExport, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	in := exportInput{Name: d.Scenario.Name, Label: "черновик", Sites: d.Scenario.Sites, Steps: d.Steps,
		Rules: d.Scenario.Rules, Commands: d.Scenario.Commands}
	if version > 0 {
		v, err := s.repos.Scenarios.Version(ctx, id, version)
		if err != nil {
			return nil, err
		}
		if v == nil {
			return nil, errs.NewNotFoundError(msgVersionNF)
		}
		in.Label = fmt.Sprintf("версия %d от %s", v.Number, v.PublishedAt.Format("02.01.2006"))
		in.Steps, in.Rules, in.Commands = v.Steps, v.Rules, v.Commands
	}
	data, err := buildScenarioXLSX(in)
	if err != nil {
		return nil, err
	}
	name := in.Name + " — черновик.xlsx"
	if version > 0 {
		name = fmt.Sprintf("%s — версия %d.xlsx", in.Name, version)
	}
	return &ScenarioExport{FileName: name, Data: data}, nil
}

// tzLayout — строки листа ТЗ и где в них шаги, правила, команды (номера строк Excel с 1).
type tzLayout struct {
	rows     [][tzCols]string
	stepSpan [][2]int
	ruleRow  []int
	cmdRow   []int
}

func (l *tzLayout) add(r [tzCols]string) int {
	l.rows = append(l.rows, r)
	return len(l.rows)
}

// strings — ячейки как их вернёт excelize.GetRows (без хвостовых пустых).
func (l *tzLayout) strings() [][]string {
	out := make([][]string, len(l.rows))
	for i, r := range l.rows {
		n := tzCols
		for n > 0 && r[n-1] == "" {
			n--
		}
		out[i] = append([]string(nil), r[:n]...)
	}
	return out
}

// layoutTZ — лист ТЗ: заголовок, шапка, блоки шагов, правила, команды.
func layoutTZ(in exportInput) *tzLayout {
	l := &tzLayout{}
	title := "Сценарий адаптации «" + in.Name + "», " + in.Label
	if lo := strings.ToLower(title); strings.Contains(lo, "когда") || strings.Contains(lo, "информирован") {
		title = "Сценарий адаптации, " + in.Label // строка заголовка не должна походить на шапку
	}
	l.add([tzCols]string{title})
	l.add(tzHeader)

	steps := in.Steps
	block := 0
	for start := 0; start < len(steps); {
		end := start + 1
		for end < len(steps) && steps[end].DayOffset == steps[start].DayOffset && steps[end].Time == steps[start].Time {
			end++
		}
		block++
		lastSurvey := -1
		for i := start; i < end; i++ {
			if steps[i].Survey != nil && steps[i].Kind == domain.KindSurvey {
				lastSurvey = i
			}
		}
		for i := start; i < end; i++ {
			rows := stepRows(steps[i], in.Sites)
			if i == start {
				rows[0][colNum] = strconv.Itoa(block)
				rows[0][colWhen] = whenText(steps[i].DayOffset, steps[i].Time)
			}
			if i == lastSurvey {
				if cell := outcomesCell(steps[i].Survey); cell != "" {
					rows = append(rows, [tzCols]string{colText: cell})
				}
			}
			first := 0
			for j, r := range rows {
				n := l.add(r)
				if j == 0 {
					first = n
				}
			}
			l.stepSpan = append(l.stepSpan, [2]int{first, len(l.rows)})
		}
		start = end
	}
	for _, r := range in.Rules {
		l.ruleRow = append(l.ruleRow, l.add(ruleRow(r)))
	}
	for _, c := range in.Commands {
		l.cmdRow = append(l.cmdRow, l.add(commandRow(c)))
	}
	return l
}

// whenText — день и время прозой ТЗ; parseTZWhen читает её обратно без приближений.
func whenText(day int, hhmm string) string {
	var s string
	switch {
	case day < 0:
		s = fmt.Sprintf("За %d %s до выхода на работу", -day, ruDays(-day))
	case day == 0:
		s = "В первый рабочий день"
	default:
		s = fmt.Sprintf("Через %d %s после выхода на работу", day, ruDays(day))
	}
	if hhmm != "" {
		s += " в " + hhmm
	}
	return s
}

func ruDays(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "день"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return "дня"
	}
	return "дней"
}

// stepRows — строки шага: одна для сообщения, у опроса — по строке на группу вопросов.
func stepRows(st domain.ScenarioStep, scenarioSites []string) [][tzCols]string {
	var r [tzCols]string
	r[colTitle] = st.Title
	r[colSites] = strings.Join(st.Sites, ", ")
	r[colKind] = kindLabels[st.Kind]
	if r[colKind] == "" || st.HRTask {
		r[colKind] = "Текст"
	}
	if st.HRTask {
		r[colKind] = "Письмо HR"
	}
	if st.Required {
		r[colRequired] = "Да"
	}
	if st.FlagRequired != nil && *st.FlagRequired == flagRecruiter {
		r[colFlag] = tzRecruiterFlag
	}
	var media []string
	personal := false
	for _, m := range st.Media {
		if m.Personal == "adaptation_plan" {
			personal = true
			continue
		}
		media = append(media, m.Name)
	}
	r[colMedia] = strings.Join(media, "\n")

	if st.Kind == domain.KindSurvey && st.Survey != nil && len(st.Survey.Questions) > 0 {
		cells := surveyCells(st)
		out := make([][tzCols]string, len(cells))
		for i, c := range cells {
			if i == 0 {
				out[0] = r
				out[0][colText] = withPersonal(c, personal)
				continue
			}
			out[i] = [tzCols]string{colSites: r[colSites], colText: c}
		}
		return out
	}
	r[colText] = withPersonal(messageCell(st), personal)
	out := [][tzCols]string{r}
	for i := range st.Buttons {
		if fr, ok := branchFilesRow(st, i, scenarioSites); ok {
			out = append(out, fr)
		}
	}
	return out
}

// branchFilesRow — файлы ответа-ветки строкой «В случае варианта ответа „Да“», как r26–r28 ТЗ:
// «#Приложить пдф-ку …» — документ (send_file), колонка изображения — картинка к реплике ветки
// или, без PDF, сами файлы; тег площадки — файл под площадку новичка (per_tag).
func branchFilesRow(st domain.ScenarioStep, bi int, scenarioSites []string) ([tzCols]string, bool) {
	var none [tzCols]string
	b := st.Buttons[bi]
	for _, x := range st.Buttons[:bi] { // импорт берёт первую кнопку с такой подписью
		if strings.HasPrefix(strings.ToLower(x.Label), strings.ToLower(b.Label)) {
			return none, false
		}
	}
	br := b.Branch
	if br == nil || strings.ContainsAny(b.Label, "«»\"“”") || strings.TrimSpace(b.Label) == "" {
		return none, false
	}
	var pdf, files []string
	perTag := false
	for i, a := range br.Actions {
		if a.Type != domain.ActionSendFile {
			continue
		}
		if i > 0 && a.PerTag != perTag {
			return none, false
		}
		perTag = a.PerTag
		if a.Media == "PDF" || strings.HasPrefix(a.Media, "PDF — ") {
			pdf = append(pdf, a.Media)
		} else {
			files = append(files, a.Media)
		}
	}
	var msgMedia []string
	if len(br.Messages) > 0 {
		for _, m := range br.Messages[0].Media {
			if m.Personal != "" || m.MediaID != nil || m.Caption != "" {
				return none, false
			}
			msgMedia = append(msgMedia, m.Name)
		}
	}
	var r [tzCols]string
	switch {
	case len(pdf) == 1 && len(files) == 0:
		r[colText] = "#Приложить пдф-ку " + strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(pdf[0], "PDF"), " — "))
		r[colMedia] = strings.Join(msgMedia, "\n")
	case len(pdf) == 0 && len(files) > 0 && len(msgMedia) == 0:
		r[colMedia] = strings.Join(files, "\n")
	default:
		return none, false
	}
	if perTag {
		sites := st.Sites
		if len(sites) == 0 {
			sites = scenarioSites
		}
		if len(sites) == 0 {
			return none, false
		}
		r[colSites] = strings.Join(sites, ", ")
	}
	r[colWhen] = "В случае варианта ответа «" + oneLine(b.Label) + "»"
	r[colTitle] = "Ответ"
	return r, true
}

func withPersonal(cell string, personal bool) string {
	if !personal {
		return cell
	}
	return joinTrim([]string{cell, "", tzPersonalPlan})
}

// messageCell — текст сообщения: письмо HR, авторские альтернативы, варианты и ветки.
func messageCell(st domain.ScenarioStep) string {
	if st.HRTask {
		return tzHRTaskHead + "\n" + st.TextTy
	}
	text := st.TextTy
	if st.AltText != nil && len(st.Buttons) == 0 {
		if cell, ok := altCell(st.TextTy, *st.AltText); ok {
			return cell
		}
	}
	if len(st.Buttons) == 0 {
		return text
	}
	labels := make([]string, len(st.Buttons))
	items := make([]brItem, 0, len(st.Buttons))
	for i, b := range st.Buttons {
		labels[i] = b.Label
		if b.Branch != nil {
			items = append(items, brItem{sel: i + 1, br: b.Branch, on: plusOne(b.Branch.On)})
		}
	}
	return joinTrim([]string{optionsBlock(text, labels), "", strings.Join(branchSections(items), "\n")})
}

func plusOne(on []int) []int {
	out := make([]int, len(on))
	for i, v := range on {
		out[i] = v + 1
	}
	return out
}

// optionsBlock — текст и «Варианты ответов: 1) … 2) …».
func optionsBlock(text string, labels []string) string {
	lines := []string{}
	if strings.TrimSpace(text) != "" {
		lines = append(lines, text)
	}
	lines = append(lines, tzOptionsHead)
	for i, l := range labels {
		lines = append(lines, fmt.Sprintf("%d) %s", i+1, oneLine(l)))
	}
	return strings.Join(lines, "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// altCell — «1. Вариант: … 2. Вариант: …» + общий хвост (splitAlternatives).
func altCell(text, alt string) (string, bool) {
	a, ta, _ := strings.Cut(text, "\n\n")
	b, tb, _ := strings.Cut(alt, "\n\n")
	if ta != tb || strings.Contains(a, "\n") || strings.Contains(b, "\n") || strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return "", false
	}
	return joinTrim([]string{"1. Вариант: " + a, "2. Вариант: " + b, "", ta}), true
}

// brItem — ветка варианта: sel — номер варианта с 1, on — общая ветка (номера с 1).
type brItem struct {
	sel int
	br  *domain.Branch
	on  []int
}

// branchSections — «Если ответ N: …», общая ветка — «Если ответ 1-2: …» (одна на диапазон).
func branchSections(items []brItem) []string {
	bySel := map[int]brItem{}
	for _, it := range items {
		bySel[it.sel] = it
	}
	done := map[int]bool{}
	var secs [][]string
	for _, it := range items {
		if done[it.sel] {
			continue
		}
		sel := strconv.Itoa(it.sel)
		if lo, hi, ok := sharedRange(it, bySel); ok {
			sel = fmt.Sprintf("%d-%d", lo, hi)
			for n := lo; n <= hi; n++ {
				done[n] = true
			}
		}
		done[it.sel] = true
		body := branchBody(it.br)
		if len(body) == 0 {
			continue
		}
		secs = append(secs, append([]string{"Если ответ " + sel + ": " + body[0]}, body[1:]...))
	}
	var out []string
	for i, s := range secs {
		if i == len(secs)-1 {
			s = collapseBlank(s) // абзац после последней ветки импорт считает общим хвостом
		}
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, s...)
	}
	return out
}

// sharedRange — общая ветка на непрерывный диапазон вариантов с одинаковым содержанием.
func sharedRange(it brItem, bySel map[int]brItem) (int, int, bool) {
	if len(it.on) < 2 {
		return 0, 0, false
	}
	lo, hi := it.on[0], it.on[len(it.on)-1]
	if hi-lo+1 != len(it.on) || it.sel < lo || it.sel > hi {
		return 0, 0, false
	}
	want := jsonOf(it.br)
	for n := lo; n <= hi; n++ {
		if it.on[n-lo] != n {
			return 0, 0, false
		}
		o, ok := bySel[n]
		if !ok || jsonOf(o.br) != want {
			return 0, 0, false
		}
	}
	return lo, hi, true
}

func collapseBlank(lines []string) []string {
	out := lines[:0:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// branchBody — строки ветки: текст первого сообщения и служебные строки кнопок и действий.
func branchBody(br *domain.Branch) []string {
	var lines []string
	var buttons []domain.StepButton
	if len(br.Messages) > 0 {
		m := br.Messages[0]
		lines = append(lines, splitLines(strings.TrimSpace(m.TextTy))...)
		buttons = m.Buttons
	}
	return append(lines, serviceLines(buttons, br.Actions)...)
}

func hasActionType(acts []domain.Action, typ string) bool {
	for _, a := range acts {
		if a.Type == typ {
			return true
		}
	}
	return false
}

func firstAction(b domain.StepButton, typ string) (domain.Action, bool) {
	if b.Branch == nil {
		return domain.Action{}, false
	}
	for _, a := range b.Branch.Actions {
		if a.Type == typ {
			return a, true
		}
	}
	return domain.Action{}, false
}

// serviceLines — терминальные кнопки и «ответить в чате» фразами ТЗ: «Опции для сотрудника:
// передать проблему … через Корсика, связаться с рекрутером», «Возможность для сотрудника …».
func serviceLines(buttons []domain.StepButton, actions []domain.Action) []string {
	var out []string
	freeText := hasActionType(actions, domain.ActionOpenFreeText)
	chatLabel := func(b domain.StepButton) (string, bool) {
		a, ok := firstAction(b, domain.ActionHandoff)
		switch {
		case ok && b.Label == labelChatRecruiter && a.To == "recruiter":
			return "связаться с рекрутером", true
		case ok && b.Label == labelChatHR && a.To == "hr":
			return "связаться с HR", true
		}
		return "", false
	}
	for i := 0; i < len(buttons); i++ {
		b := buttons[i]
		if a, ok := firstAction(b, domain.ActionEscalate); ok && b.Label == labelEscalate {
			line := "Опции для сотрудника: передать проблему через Корсика"
			if a.Route == "hr" {
				line = "Опции для сотрудника: передать проблему в HR через Корсика"
			}
			if i+1 < len(buttons) {
				if chat, ok := chatLabel(buttons[i+1]); ok {
					line += ", " + chat
					i++
				}
			}
			out = append(out, line)
			continue
		}
		if chat, ok := chatLabel(b); ok {
			out = append(out, "Опции для сотрудника: "+chat)
			continue
		}
		if _, ok := firstAction(b, domain.ActionHandoff); ok && b.Label != "" {
			if freeText {
				out = append(out, "Возможность для сотрудника ответить в чате с ботом либо нажать кнопку «"+oneLine(b.Label)+"»")
				freeText = false
				continue
			}
			out = append(out, "Возможность для сотрудника нажать кнопку «"+oneLine(b.Label)+"»")
		}
		// Прочие кнопки прозой ТЗ не выражаются — их вернёт лист расширений.
	}
	if freeText {
		out = append(out, "Возможность для сотрудника ответить в чате с ботом")
	}
	return out
}

// surveyCells — вопросы опроса группами (одна группа — одна строка листа):
// подряд идущие выборы без веток — нумерованный список с вариантами через дефис; подряд идущие
// шкалы, да/нет и открытые — нумерованный список с пометками; выбор с ветками — «Варианты
// ответов» и «Если ответ N»; остальное — «Открытый вопрос». Вступление опроса — в первой строке.
func surveyCells(st domain.ScenarioStep) []string {
	qs := st.Survey.Questions
	var cells []string
	intro := strings.TrimSpace(st.TextTy)
	for i := 0; i < len(qs); {
		q := qs[i]
		switch {
		case isChoiceQ(q) && len(q.Branches) == 0 && dashOK(q):
			j := i + 1
			for j < len(qs) && isChoiceQ(qs[j]) && len(qs[j].Branches) == 0 && dashOK(qs[j]) {
				j++
			}
			if j-i >= 2 {
				cells = append(cells, numberedCell(introFor(cells, intro), dashItems(qs[i:j])))
				i = j
				continue
			}
			cells = append(cells, choiceCell(q))
		case isChoiceQ(q) && len(q.Options)+boolInt(q.Other) >= 2 && strings.TrimSpace(q.Text) != "":
			cells = append(cells, choiceCell(q))
		case !isChoiceQ(q) && len(q.Branches) == 0:
			j := i + 1
			for j < len(qs) && !isChoiceQ(qs[j]) && len(qs[j].Branches) == 0 {
				j++
			}
			if n := markerRun(qs[i:j]); n >= 2 {
				items := make([]string, n)
				for k := 0; k < n; k++ {
					items[k] = scaleLine(qs[i+k])
				}
				cells = append(cells, numberedCell(introFor(cells, intro), items))
				i += n
				continue
			}
			cells = append(cells, openCell(q))
		default:
			cells = append(cells, openCell(q))
		}
		i++
	}
	return cells
}

func introFor(cells []string, intro string) string {
	if len(cells) > 0 {
		return ""
	}
	return intro
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isChoiceQ(q domain.SurveyQuestion) bool { return q.Type == "choice" || q.Type == "multi" }

func dashOK(q domain.SurveyQuestion) bool {
	if strings.Contains(strings.TrimSpace(q.Text), "\n") || strings.TrimSpace(q.Text) == "" {
		return false
	}
	for _, o := range q.Options {
		if strings.TrimSpace(o) == "" {
			return false
		}
	}
	return len(q.Options)+boolInt(q.Other) >= 2
}

// markerRun — сколько вопросов с начала можно записать одним блоком шкал: импорт берёт блок, если
// хотя бы половина пунктов с пометкой типа (да/нет, шкала).
func markerRun(qs []domain.SurveyQuestion) int {
	for n := len(qs); n >= 2; n-- {
		marked := 0
		for _, q := range qs[:n] {
			if q.Type != "text" {
				marked++
			}
		}
		if marked*2 >= n {
			return n
		}
	}
	return 0
}

func numberedCell(intro string, items []string) string {
	var lines []string
	if intro != "" {
		lines = append(lines, intro, "")
	}
	for i, it := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, it))
	}
	return strings.Join(lines, "\n")
}

func dashItems(qs []domain.SurveyQuestion) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		lines := []string{oneLine(q.Text)}
		if q.Type == "multi" {
			lines[0] += " (несколько вариантов ответа)"
		}
		for _, o := range q.Options {
			lines = append(lines, "- "+oneLine(o))
		}
		if q.Other {
			lines = append(lines, "- "+tzOther)
		}
		out[i] = strings.Join(lines, "\n")
	}
	return out
}

// choiceCell — вопрос с вариантами и ветками; «свой вариант» — последним.
func choiceCell(q domain.SurveyQuestion) string {
	labels := append([]string{}, q.Options...)
	if q.Other {
		labels = append(labels, tzOther)
	}
	var items []brItem
	for _, k := range sortedKeys(q.Branches) {
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 || n > len(q.Options) || q.Branches[k] == nil {
			continue
		}
		items = append(items, brItem{sel: n, br: q.Branches[k], on: q.Branches[k].On})
	}
	return joinTrim([]string{optionsBlock(q.Text, labels), "", strings.Join(branchSections(items), "\n")})
}

func openCell(q domain.SurveyQuestion) string {
	return joinTrim([]string{q.Text, tzOpenQuestion})
}

// scaleLine — пункт блока шкал: «Вопрос? (1 - …, 10 - …) В случае ответа ниже 8 спросить
// причину.», «Вопрос? (да/нет) При отрицательном ответе спросить причину.», «Обязательное поле.»
func scaleLine(q domain.SurveyQuestion) string {
	s := oneLine(q.Text)
	switch q.Type {
	case "yesno":
		s += " (да/нет)"
		if q.Comment == "on_no" {
			s += " При отрицательном ответе спросить причину."
		}
	case "text":
	default:
		if len(q.Anchors) == 2 && cleanAnchor(q.Anchors[0]) && cleanAnchor(q.Anchors[1]) {
			s += fmt.Sprintf(" (1 - %s, 10 - %s)", oneLine(q.Anchors[0]), oneLine(q.Anchors[1]))
		} else {
			s += " (шкала 1–10)"
		}
		if q.Threshold != nil {
			s += fmt.Sprintf(" В случае ответа ниже %d спросить причину.", *q.Threshold)
		} else if q.Comment == "optional" {
			s += " + поле для комментария"
		}
	}
	if q.Required {
		s += " Обязательное поле."
	}
	return s
}

func cleanAnchor(a string) bool {
	return strings.TrimSpace(a) != "" && !strings.ContainsAny(a, ",;()+.\n")
}

// ordinalsNom — номер вопроса словом для «Если на первый вопрос …» (parseOutcomes).
var ordinalsNom = []string{"", "первый", "второй", "третий", "четвертый", "пятый", "шестой", "седьмой", "восьмой", "девятый", "десятый"}

// outcomesCell — «ОТВЕТЫ:» после опроса: «+» — независимые правила, «ЛИБО» — одна группа.
func outcomesCell(sv *domain.Survey) string {
	if sv == nil || len(sv.Outcomes) == 0 {
		return ""
	}
	var secs [][]string
	var secGroup []string
	for _, o := range sv.Outcomes {
		seg, ok := outcomeSegment(o)
		if !ok {
			continue
		}
		n := len(secs)
		if o.Group != "" && n > 0 && secGroup[n-1] == o.Group {
			secs[n-1] = append(append(secs[n-1], "", "ЛИБО", ""), seg...)
			continue
		}
		secs = append(secs, seg)
		secGroup = append(secGroup, o.Group)
	}
	if len(secs) == 0 {
		return ""
	}
	lines := []string{tzAnswersHead, ""}
	for i, s := range secs {
		if i > 0 {
			lines = append(lines, "", "+", "")
		}
		lines = append(lines, s...)
	}
	return strings.Join(lines, "\n")
}

func outcomeSegment(o domain.Outcome) ([]string, bool) {
	var head string
	switch {
	case len(o.If) == 0:
	case len(o.If) == 1 && o.If[0].Q >= 1 && o.If[0].Q < len(ordinalsNom):
		c := o.If[0]
		var v int
		switch {
		case len(c.In) == 1 && len(c.Contains) == 0:
			v = c.In[0]
		case len(c.Contains) == 1 && len(c.In) == 0:
			v = c.Contains[0]
		default:
			return nil, false
		}
		head = fmt.Sprintf("Если на %s вопрос выбран %d вариант ответа:", ordinalsNom[c.Q], v)
	default:
		return nil, false
	}
	body := branchBody(&domain.Branch{Messages: o.Messages, Actions: o.Actions})
	if len(body) == 0 {
		return nil, false
	}
	if head != "" {
		body = append([]string{head}, body...)
	}
	return body, true
}

// ruleRow — r69/r70: «Если сотрудник …», текст, кнопки «Опции для сотрудника», картинка.
func ruleRow(r domain.ScenarioRule) [tzCols]string {
	trigger := oneLine(r.Trigger)
	if !strings.HasPrefix(strings.ToLower(trigger), "если ") {
		switch {
		case r.Kind == domain.RuleSurveyReminder && len(r.RemindAfterH) > 1:
			trigger = "Если сотрудник не прошёл опрос в течение дня (напомнить на завтра + на послезавтра)"
		case r.Kind == domain.RuleSurveyReminder:
			trigger = "Если сотрудник не прошёл опрос в течение дня (напомнить на завтра)"
		default:
			trigger = "Если сотрудник задаст вопрос Корсику (просто так)"
		}
	}
	var row [tzCols]string
	row[colWhen] = trigger
	row[colTitle] = "Правило бота"
	row[colText] = joinTrim([]string{r.TextTy, "", strings.Join(serviceLines(r.Buttons, nil), "\n")})
	names := make([]string, 0, len(r.Media))
	for _, m := range r.Media {
		names = append(names, m.Name)
	}
	row[colMedia] = strings.Join(names, "\n")
	return row
}

// commandRow — «Команда /food»: ответ бота, файлы по площадке, «Связаться с HR».
func commandRow(c domain.Command) [tzCols]string {
	var row [tzCols]string
	row[colWhen] = "Команда " + c.Name
	row[colText] = c.TextTy
	var files []string
	for _, a := range c.Actions {
		switch {
		case a.Type == domain.ActionHandoff && a.To == "hr":
			row[colTitle] = "Связаться с HR"
		case a.Type == domain.ActionHandoff && a.To == "recruiter":
			row[colTitle] = "Связаться с рекрутером"
		case a.Type == domain.ActionSendFile:
			files = append(files, a.Media)
		}
	}
	if row[colTitle] == "" {
		row[colTitle] = "Команда бота"
	}
	row[colMedia] = strings.Join(files, "\n")
	return row
}

// buildScenarioXLSX — книга: лист ТЗ и лист расширений.
func buildScenarioXLSX(in exportInput) ([]byte, error) {
	l := layoutTZ(in)
	ext, err := buildExt(in, l)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	if err := f.SetSheetName("Sheet1", exportMainSheet); err != nil {
		return nil, err
	}
	if err := writeSheet(f, exportMainSheet, l.strings(), []float64{7, 30, 28, 16, 14, 70, 11, 14, 26, 8}); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(extSheetName); err != nil {
		return nil, err
	}
	if err := writeSheet(f, extSheetName, ext, []float64{10, 9, 18, 50, 30, 10, 22, 50, 60, 14}); err != nil {
		return nil, err
	}
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeSheet — строки как текст (без формул и дат), шапка во второй строке жирная, перенос слов.
func writeSheet(f *excelize.File, sheet string, rows [][]string, widths []float64) error {
	wrap, err := f.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"}})
	if err != nil {
		return err
	}
	head, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"E8EEF7"}, Pattern: 1},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"}})
	if err != nil {
		return err
	}
	bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}})
	if err != nil {
		return err
	}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, col, col, w); err != nil {
			return err
		}
	}
	for i, r := range rows {
		for j, v := range r {
			if v == "" {
				continue
			}
			if utf8.RuneCountInString(v) > excelize.TotalCellChars {
				return fmt.Errorf("ячейка %d:%d длиннее %d символов", i+1, j+1, excelize.TotalCellChars)
			}
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			if err := f.SetCellStr(sheet, cell, v); err != nil {
				return err
			}
		}
	}
	last, _ := excelize.CoordinatesToCellName(len(widths), len(rows))
	if len(rows) > 2 {
		if err := f.SetCellStyle(sheet, "A3", last, wrap); err != nil {
			return err
		}
	}
	end, _ := excelize.CoordinatesToCellName(len(widths), 2)
	if err := f.SetCellStyle(sheet, "A2", end, head); err != nil {
		return err
	}
	if err := f.SetCellStyle(sheet, "A1", "A1", bold); err != nil {
		return err
	}
	return f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 2, TopLeftCell: "A3", ActivePane: "bottomLeft"})
}
