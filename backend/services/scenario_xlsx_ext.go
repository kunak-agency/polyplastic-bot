package services

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"main/domain"

	"github.com/google/uuid"
)

// Лист «Корсик — расширения» (api_contract.md, v4 — экспорт): поля конструктора, которых в
// формате ТЗ нет. Экспорт пишет строку на шаг, правило и команду листа ТЗ; импорт, если лист есть,
// разбирает лист ТЗ как обычно и дополняет результат. Строка расширений действует, только пока
// строки ТЗ, к которым она относится, не правили (колонка «Контроль строки» — хеш ячеек B–J):
// HR поменял текст в листе ТЗ — для этого шага побеждает лист ТЗ, импорт предупреждает ext_stale.
//
// Колонки читаемые (текст на «вы», флаги вариантов, переход, «если не ответил», действия веток) —
// для людей. «Прочее (JSON)» — остальные поля шага, которые после разбора листа ТЗ и читаемых
// колонок ещё отличаются от конструктора: экспорт сам разбирает свой лист ТЗ и записывает разницу.
// Поэтому «импорт → экспорт → импорт» с листом расширений не теряет полей.

// Имя листа: двоеточие в именах листов Excel запрещено, поэтому тире.
const extSheetName = "Корсик — расширения"

// Колонки листа расширений.
const (
	extRows = iota
	extWhat
	extKey
	extTextVy
	extFlags
	extAdvance
	extNoAnswer
	extActions
	extPatch
	extHash
	extCols
)

var extHeader = []string{"Строки ТЗ", "Что", "Ключ шага", "Текст на «вы»", "Флаги вариантов (вопрос → вариант → флаг)",
	"Переход (button — кнопкой «Дальше →», auto — сам)", "Если не ответил", "Действия веток (escalate, handoff, файлы)",
	"Прочее (JSON полей, которых нет в формате ТЗ)", "Контроль строки"}

// Что описывает строка расширений.
const (
	extStep     = "шаг"
	extRule     = "правило"
	extCommand  = "команда"
	extScenario = "сценарий"
)

// Предупреждения импорта про лист расширений.
const (
	warnExtApplied = "ext_applied"
	warnExtStale   = "ext_stale"
)

type extEntry struct {
	what     string
	from, to int // строки листа ТЗ (Excel, с 1)
	key      string
	textVy   string
	flags    string
	advance  string
	noAnswer string
	actions  string
	patch    string
	hash     string
}

func isExtSheet(name string) bool {
	lo := strings.ToLower(name)
	return strings.HasPrefix(lo, "корсик") && strings.Contains(lo, "расширен")
}

// rowsHash — контроль строк листа ТЗ: ячейки B–J без крайних пробелов.
func rowsHash(rows [][]string, from, to int) string {
	if from < 1 || to < from || to > len(rows) {
		return ""
	}
	h := sha1.New()
	for i := from - 1; i < to; i++ {
		for j := colWhen; j < tzCols; j++ {
			c := ""
			if j < len(rows[i]) {
				c = strings.TrimSpace(rows[i][j])
			}
			h.Write([]byte(c))
			h.Write([]byte{0x1f})
		}
		h.Write([]byte{0x1e})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// --- Запись ---------------------------------------------------------------------------

// buildExt — строки листа расширений. Экспорт разбирает собственный лист ТЗ тем же импортом,
// применяет читаемые колонки и пишет в «Прочее» то, что ещё отличается.
func buildExt(in exportInput, l *tzLayout) ([][]string, error) {
	rows := l.strings()
	var entries []extEntry
	for i, st := range in.Steps {
		sp := l.stepSpan[i]
		entries = append(entries, explicitEntry(st, sp[0], sp[1], rowsHash(rows, sp[0], sp[1])))
	}
	for i := range in.Rules {
		r := l.ruleRow[i]
		entries = append(entries, extEntry{what: extRule, from: r, to: r, hash: rowsHash(rows, r, r)})
	}
	for i := range in.Commands {
		r := l.cmdRow[i]
		entries = append(entries, extEntry{what: extCommand, from: r, to: r, hash: rowsHash(rows, r, r)})
	}

	// Шаги: разница после читаемых колонок.
	imp := parseOwnSheet(rows)
	res := applyExt(imp, rows, entries)
	for e, si := range res.stepOf {
		patch, err := diffJSON(stepMap(in.Steps[stepIndexOf(entries, e)]), stepMap(imp.steps[si]))
		if err != nil {
			return nil, err
		}
		entries[e].patch = patch
	}
	// Правила и команды: поля, которые разбор строки передал не так (номер строки, «вы»).
	for e := range entries {
		var orig, got any
		switch entries[e].what {
		case extRule:
			orig = in.Rules[kindIndexOf(entries, e, extRule)]
			if i := res.ruleOf[e]; i >= 0 {
				got = imp.rules[i]
			} else {
				got = domain.ScenarioRule{}
			}
		case extCommand:
			orig = in.Commands[kindIndexOf(entries, e, extCommand)]
			if i := res.cmdOf[e]; i >= 0 {
				got = imp.commands[i]
			} else {
				got = domain.Command{}
			}
		default:
			continue
		}
		patch, err := diffJSON(objMap(orig), objMap(got))
		if err != nil {
			return nil, err
		}
		entries[e].patch = patch
	}
	// Сценарий: площадки, а также правила и команды, если список целиком всё ещё другой
	// (например, импорт нашёл команду в тексте, а в сценарии её нет).
	imp2 := parseOwnSheet(rows)
	applyExt(imp2, rows, entries)
	sc := map[string]any{}
	sites := in.Sites
	if sites == nil {
		sites = []string{}
	}
	if jsonOf(domain.StringList(sites)) != jsonOf(domain.StringList(nonNilStrings(imp2.sites))) {
		sc["sites"] = sites
	}
	if jsonOf(domain.RuleList(in.Rules)) != jsonOf(domain.RuleList(imp2.rules)) {
		sc["rules"] = nonNilRules(in.Rules)
	}
	if jsonOf(domain.CommandList(in.Commands)) != jsonOf(domain.CommandList(imp2.commands)) {
		sc["commands"] = nonNilCommands(in.Commands)
	}
	if len(sc) > 0 {
		entries = append(entries, extEntry{what: extScenario, patch: jsonOf(sc)})
	}

	out := [][]string{{"Корсик — расширения: поля конструктора, которых нет в формате ТЗ. Импорт читает лист обратно; строку, чьи строки ТЗ поменяли, он пропускает."}, extHeader}
	for _, e := range entries {
		r := make([]string, extCols)
		switch {
		case e.from == 0:
		case e.from == e.to:
			r[extRows] = strconv.Itoa(e.from)
		default:
			r[extRows] = fmt.Sprintf("%d–%d", e.from, e.to)
		}
		r[extWhat], r[extKey], r[extTextVy], r[extFlags] = e.what, e.key, e.textVy, e.flags
		r[extAdvance], r[extNoAnswer], r[extActions], r[extPatch], r[extHash] = e.advance, e.noAnswer, e.actions, e.patch, e.hash
		out = append(out, r)
	}
	return out, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilRules(r []domain.ScenarioRule) []domain.ScenarioRule {
	if r == nil {
		return []domain.ScenarioRule{}
	}
	return r
}

func nonNilCommands(c []domain.Command) []domain.Command {
	if c == nil {
		return []domain.Command{}
	}
	return c
}

// parseOwnSheet — разбор листа ТЗ, который экспорт только что собрал (пустой — пустой импорт).
func parseOwnSheet(rows [][]string) *tzImport {
	imp, err := parseSheetRows(rows)
	if err != nil {
		return &tzImport{}
	}
	return imp
}

func stepIndexOf(entries []extEntry, e int) int { return kindIndexOf(entries, e, extStep) }

// kindIndexOf — порядковый номер строки e среди строк того же вида.
func kindIndexOf(entries []extEntry, e int, what string) int {
	n := 0
	for i := 0; i < e; i++ {
		if entries[i].what == what {
			n++
		}
	}
	return n
}

// explicitEntry — читаемые колонки шага.
func explicitEntry(st domain.ScenarioStep, from, to int, hash string) extEntry {
	e := extEntry{what: extStep, from: from, to: to, key: st.Key, hash: hash}
	if st.TextVy != st.TextTy && st.TextVy != "" {
		e.textVy = st.TextVy
	}
	if st.Survey != nil {
		hasFlags := false
		m := map[string]map[string]domain.OptionFlag{}
		for i, q := range st.Survey.Questions {
			fl := q.OptionFlags
			if fl == nil {
				fl = map[string]domain.OptionFlag{}
			}
			hasFlags = hasFlags || len(fl) > 0
			m[strconv.Itoa(i+1)] = fl
		}
		if hasFlags {
			e.flags = jsonOf(m)
		}
	}
	if st.Advance != nil {
		e.advance = *st.Advance
	}
	if st.NoAnswer != nil {
		e.noAnswer = noAnswerText(*st.NoAnswer)
	}
	var lines []string
	walkActions(&st, func(path string, acts *[]domain.Action) {
		if len(*acts) > 0 {
			lines = append(lines, path+" = "+jsonOf(*acts))
		}
	})
	e.actions = strings.Join(lines, "\n")
	return e
}

func noAnswerText(r domain.NoAnswerRule) string {
	hs := make([]string, len(r.RemindAfterH))
	for i, h := range r.RemindAfterH {
		hs[i] = strconv.Itoa(h)
	}
	return fmt.Sprintf("напомнить через %s ч; не больше %d раз", strings.Join(hs, ", "), r.Max)
}

var (
	reNoAnswerH   = regexp.MustCompile(`через\s+([\d,\s]*)ч`)
	reNoAnswerMax = regexp.MustCompile(`не больше\s+(\d+)`)
	reInt         = regexp.MustCompile(`\d+`)
)

func parseNoAnswer(s string) *domain.NoAnswerRule {
	r := &domain.NoAnswerRule{RemindAfterH: []int{}}
	if m := reNoAnswerH.FindStringSubmatch(s); m != nil {
		for _, x := range reInt.FindAllString(m[1], -1) {
			n, _ := strconv.Atoi(x)
			r.RemindAfterH = append(r.RemindAfterH, n)
		}
	}
	if m := reNoAnswerMax.FindStringSubmatch(s); m != nil {
		r.Max, _ = strconv.Atoi(m[1])
	}
	return r
}

// walkActions — все действия шага с путём: «кнопка 2», «вопрос 1 / вариант 3», «реакция 1»,
// терминальные кнопки — «… / сообщение 1 / кнопка 2».
func walkActions(st *domain.ScenarioStep, fn func(path string, acts *[]domain.Action)) {
	for i := range st.Buttons {
		if br := st.Buttons[i].Branch; br != nil {
			walkActBranch(fmt.Sprintf("кнопка %d", i+1), &br.Actions, br.Messages, fn)
		}
	}
	if st.Survey == nil {
		return
	}
	for qi := range st.Survey.Questions {
		q := &st.Survey.Questions[qi]
		for _, k := range sortedKeys(q.Branches) {
			if br := q.Branches[k]; br != nil {
				walkActBranch(fmt.Sprintf("вопрос %d / вариант %s", qi+1, k), &br.Actions, br.Messages, fn)
			}
		}
	}
	for oi := range st.Survey.Outcomes {
		o := &st.Survey.Outcomes[oi]
		walkActBranch(fmt.Sprintf("реакция %d", oi+1), &o.Actions, o.Messages, fn)
	}
}

func walkActBranch(path string, acts *[]domain.Action, msgs []domain.BranchMessage, fn func(string, *[]domain.Action)) {
	fn(path, acts)
	for mi := range msgs {
		for bi := range msgs[mi].Buttons {
			if br := msgs[mi].Buttons[bi].Branch; br != nil {
				walkActBranch(fmt.Sprintf("%s / сообщение %d / кнопка %d", path, mi+1, bi+1), &br.Actions, br.Messages, fn)
			}
		}
	}
}

// --- JSON-разница ----------------------------------------------------------------------

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func objMap(v any) map[string]json.RawMessage {
	b, _ := json.Marshal(v)
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(b, &m)
	return m
}

// overlayJSON — поля patch поверх v (v — указатель).
func overlayJSON(v any, patch string) error {
	var p map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &p); err != nil {
		return err
	}
	m := objMap(v)
	for k, x := range p {
		m[k] = x
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// stepMap — шаг как JSON-объект без id, scenario_id и position (их задаёт сохранение).
func stepMap(st domain.ScenarioStep) map[string]json.RawMessage {
	b, _ := json.Marshal(st)
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(b, &m)
	delete(m, "id")
	delete(m, "scenario_id")
	delete(m, "position")
	return m
}

// diffJSON — поля want, которые отличаются от got, одним JSON-объектом (пусто — совпадают).
func diffJSON(want, got map[string]json.RawMessage) (string, error) {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	n := 0
	for _, k := range keys {
		if bytes.Equal(want[k], got[k]) {
			continue
		}
		if n == 0 {
			buf.WriteString("{")
		} else {
			buf.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteString(":")
		buf.Write(want[k])
		n++
	}
	if n == 0 {
		return "", nil
	}
	buf.WriteString("}")
	return buf.String(), nil
}

// applyStepPatch — поля из «Прочее (JSON)» поверх шага; id и позиция остаются.
func applyStepPatch(st *domain.ScenarioStep, patch string) error {
	if strings.TrimSpace(patch) == "" {
		return nil
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal([]byte(patch), &p); err != nil {
		return err
	}
	m := stepMap(*st)
	for k, v := range p {
		if k == "id" || k == "scenario_id" || k == "position" {
			continue
		}
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	var out domain.ScenarioStep
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	out.ID, out.ScenarioID, out.Position = st.ID, st.ScenarioID, st.Position
	*st = out
	return nil
}

// applyExplicit — читаемые колонки, затем «Прочее».
func applyExplicit(st *domain.ScenarioStep, e extEntry) error {
	if e.key != "" {
		st.Key = e.key
	}
	if e.textVy != "" {
		st.TextVy = e.textVy
	}
	if e.flags != "" && st.Survey != nil {
		var m map[string]map[string]domain.OptionFlag
		if err := json.Unmarshal([]byte(e.flags), &m); err != nil {
			return err
		}
		for k, fl := range m {
			i, err := strconv.Atoi(k)
			if err != nil || i < 1 || i > len(st.Survey.Questions) {
				continue
			}
			if len(fl) == 0 {
				fl = nil
			}
			st.Survey.Questions[i-1].OptionFlags = fl
		}
	}
	if e.advance != "" {
		v := e.advance
		st.Advance = &v
	}
	if e.noAnswer != "" {
		st.NoAnswer = parseNoAnswer(e.noAnswer)
	}
	if e.actions != "" {
		m := map[string][]domain.Action{}
		for _, l := range splitLines(e.actions) {
			path, js, ok := strings.Cut(l, " = ")
			if !ok {
				continue
			}
			var acts []domain.Action
			if err := json.Unmarshal([]byte(js), &acts); err != nil {
				return err
			}
			m[strings.TrimSpace(path)] = acts
		}
		walkActions(st, func(path string, acts *[]domain.Action) {
			if v, ok := m[path]; ok {
				*acts = append([]domain.Action{}, v...)
			}
		})
	}
	return applyStepPatch(st, e.patch)
}

// --- Чтение ----------------------------------------------------------------------------

var reExtRows = regexp.MustCompile(`^\s*(\d+)\s*(?:[–—-]\s*(\d+))?\s*$`)

// readExtRows — строки листа расширений (шапка — строка с «Строки ТЗ» в колонке A).
func readExtRows(rows [][]string) []extEntry {
	start := -1
	for i := 0; i < len(rows) && i < headerSearchRows; i++ {
		if len(rows[i]) > 0 && strings.Contains(strings.ToLower(rows[i][0]), "строки тз") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var out []extEntry
	for _, r := range rows[start:] {
		cell := func(j int) string {
			if j < len(r) {
				return r[j]
			}
			return ""
		}
		e := extEntry{what: strings.TrimSpace(cell(extWhat)), key: strings.TrimSpace(cell(extKey)), textVy: cell(extTextVy),
			flags: strings.TrimSpace(cell(extFlags)), advance: strings.TrimSpace(cell(extAdvance)), noAnswer: strings.TrimSpace(cell(extNoAnswer)),
			actions: strings.TrimSpace(cell(extActions)), patch: strings.TrimSpace(cell(extPatch)), hash: strings.TrimSpace(cell(extHash))}
		if m := reExtRows.FindStringSubmatch(cell(extRows)); m != nil {
			e.from, _ = strconv.Atoi(m[1])
			e.to = e.from
			if m[2] != "" {
				e.to, _ = strconv.Atoi(m[2])
			}
		}
		switch {
		case e.what == extScenario:
		case e.from == 0 || (e.what != extStep && e.what != extRule && e.what != extCommand):
			continue
		}
		out = append(out, e)
	}
	return out
}

// extResult — куда легли строки расширений: индекс строки → индекс шага/правила/команды.
type extResult struct {
	stepOf   map[int]int
	ruleOf   map[int]int
	cmdOf    map[int]int
	restored map[uuid.UUID]bool
	stale    []int // строки ТЗ, которые правили после экспорта
	errs     []int // строки, где JSON не разобрался
}

// applyExt — дополнить разбор листа ТЗ листом расширений. Шаг строки расширений — первый шаг,
// который импорт собрал из её первой строки ТЗ; другие шаги из её строк (хвост после веток,
// случайно разобранный вопрос) отбрасываются, а если шага нет (строка слилась с предыдущей) —
// шаг собирается заново из «Прочее». Шаги из строк без расширений (HR дописал строку) остаются.
func applyExt(imp *tzImport, rows [][]string, entries []extEntry) extResult {
	res := extResult{stepOf: map[int]int{}, ruleOf: map[int]int{}, cmdOf: map[int]int{}, restored: map[uuid.UUID]bool{}}
	valid := map[int]bool{}
	span := map[int]int{} // строка ТЗ → строка расширений
	for e, x := range entries {
		if x.what == extScenario {
			continue
		}
		if x.hash == "" || x.hash != rowsHash(rows, x.from, x.to) {
			res.stale = append(res.stale, x.from)
			continue
		}
		valid[e] = true
		if x.what == extStep {
			for r := x.from; r <= x.to; r++ {
				span[r] = e
			}
		}
	}

	type item struct {
		row, seq int
		st       domain.ScenarioStep
		entry    int
	}
	var items []item
	base := map[int]int{}
	for i, st := range imp.steps {
		r := 0
		if i < len(imp.rows) {
			r = imp.rows[i]
		}
		e, ok := span[r]
		if !ok {
			items = append(items, item{row: r, seq: i, st: st, entry: -1})
			continue
		}
		if _, has := base[e]; !has && r == entries[e].from {
			base[e] = i
			items = append(items, item{row: r, seq: i, st: st, entry: e})
		}
	}
	for e, x := range entries {
		if !valid[e] || x.what != extStep {
			continue
		}
		if _, has := base[e]; has {
			continue
		}
		id, _ := uuid.NewV7()
		items = append(items, item{row: x.from, seq: -1, st: domain.ScenarioStep{ID: id}, entry: e})
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].row != items[b].row {
			return items[a].row < items[b].row
		}
		return items[a].seq < items[b].seq
	})
	steps := make([]domain.ScenarioStep, 0, len(items))
	stepRows := make([]int, 0, len(items))
	for _, it := range items {
		st := it.st
		if it.entry >= 0 {
			if err := applyExplicit(&st, entries[it.entry]); err != nil {
				res.errs = append(res.errs, entries[it.entry].from)
			}
			res.stepOf[it.entry] = len(steps)
			res.restored[st.ID] = true
		}
		steps = append(steps, st)
		stepRows = append(stepRows, it.row)
	}
	imp.steps, imp.rows = steps, stepRows

	for e, x := range entries {
		switch {
		case !valid[e]:
		case x.what == extRule:
			i := -1
			for k, r := range imp.rules {
				if r.Row == x.from && i < 0 {
					i = k
				}
			}
			if x.patch != "" {
				r := domain.ScenarioRule{}
				if i >= 0 {
					r = imp.rules[i]
				}
				if err := overlayJSON(&r, x.patch); err != nil {
					res.errs = append(res.errs, x.from)
				} else if i >= 0 {
					imp.rules[i] = r
				} else {
					imp.rules = append(imp.rules, r)
					i = len(imp.rules) - 1
				}
			}
			res.ruleOf[e] = i
		case x.what == extCommand:
			i := -1
			for k, r := range imp.cmdRows {
				if r == x.from && i < 0 {
					i = k
				}
			}
			if x.patch != "" {
				c := domain.Command{}
				if i >= 0 {
					c = imp.commands[i]
				}
				if err := overlayJSON(&c, x.patch); err != nil {
					res.errs = append(res.errs, x.from)
				} else if i >= 0 {
					imp.commands[i] = c
				} else {
					imp.commands = append(imp.commands, c)
					imp.cmdRows = append(imp.cmdRows, x.from)
					i = len(imp.commands) - 1
				}
			}
			res.cmdOf[e] = i
		}
	}
	for _, x := range entries {
		if x.what != extScenario || x.patch == "" {
			continue
		}
		var sc struct {
			Sites    *[]string              `json:"sites"`
			Rules    *[]domain.ScenarioRule `json:"rules"`
			Commands *[]domain.Command      `json:"commands"`
		}
		if err := json.Unmarshal([]byte(x.patch), &sc); err != nil {
			res.errs = append(res.errs, 0)
			continue
		}
		if sc.Sites != nil {
			imp.sites = *sc.Sites
		}
		if sc.Rules != nil {
			imp.rules = *sc.Rules
		}
		if sc.Commands != nil {
			imp.commands = *sc.Commands
			imp.cmdRows = make([]int, len(imp.commands))
		}
	}
	return res
}

// applyExtReport — отчёт импорта после листа расширений: предупреждения восстановленных шагов
// снимаются (их уже видели в конструкторе), ключи шагов в предупреждениях — новые.
func applyExtReport(imp *tzImport, res extResult) {
	rep := &imp.report
	keyOf := map[string]string{}
	for _, st := range imp.steps {
		keyOf[st.ID.String()] = st.Key
	}
	kept := rep.Warnings[:0:0]
	for _, w := range rep.Warnings {
		if w.Code == warnAddressForm && len(res.stepOf) > 0 {
			continue // вторая форма не копия: её вернул лист расширений
		}
		if w.StepID != "" {
			if id, err := uuid.Parse(w.StepID); err == nil && res.restored[id] {
				continue
			}
			if k, ok := keyOf[w.StepID]; ok {
				w.StepKey = k
			} else {
				w.StepKey, w.StepID = "", ""
			}
		}
		kept = append(kept, w)
	}
	kept = append(kept, ImportWarning{Code: warnExtApplied, Message: fmt.Sprintf("Применён лист «%s»: %d шагов дополнены полями конструктора (текст на «вы», флаги, действия веток и другое)", extSheetName, len(res.stepOf))})
	for _, r := range res.stale {
		kept = append(kept, ImportWarning{Row: r, Code: warnExtStale, Message: "Строку листа ТЗ поменяли после выгрузки — расширения для неё не применены, шаг собран только из листа ТЗ"})
	}
	for _, r := range res.errs {
		kept = append(kept, ImportWarning{Row: r, Code: warnExtStale, Message: "Строка листа расширений не разобралась — применён только лист ТЗ"})
	}
	rep.Warnings = kept
	rep.Messages = len(imp.steps)
	rep.Rules = len(imp.rules)
	rep.Commands = len(imp.commands)
}
