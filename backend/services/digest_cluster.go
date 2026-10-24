package services

import (
	"sort"
	"strings"
	"unicode/utf8"

	"main/pkg/taxonomy"
	"main/repositories"

	"github.com/google/uuid"
)

// Кластеризация для дайджеста (кейс, роль 04; UX 3.7; ресерч, блок C, раздел 4 «системные
// темы»). Без тяжёлых моделей: тема — код флага (ответ опроса или обращение), кнопка причины
// низкой оценки или, для текстов без кода, группа по общим ключевым словам — тем же способом,
// что «вопросы без ответа» базы знаний (keywords/similar из documents_unanswered.go).
//
// Правила приватности (taxonomy.yaml rules, UX 3.7):
//   - тема видна, только если за ней ≥ AnonymityMinGroup разных новичков; мельче — только
//     счётчик «скрыто тем», без чисел людей;
//   - F5 и F9 — одна строка «конфиденциальное», без деталей; число — тоже только от 5;
//   - F0 — экстренный канал, в темы не входит (считается в «критичных»);
//   - текст, рядом с которым есть F0/F5/F9/F10, в ключевые слова и примеры не идёт вовсе;
//   - примеры — обезличенные (anonymize), только в теме ≥ 5 человек и не для F10.

// Ключи тем, которые не являются кодом флага.
const (
	themeReasonPrefix = "reason:"
	themeTextPrefix   = "text:"
)

// maxThemeExamples — сколько обезличенных примеров показывать у темы.
const maxThemeExamples = 3

// digestConfidential — категории, которые в дайджесте видны только строкой «конфиденциальное».
var digestConfidential = map[string]bool{"F5": true, "F9": true}

// digestNoText — тексты с этими кодами не идут ни в ключевые слова, ни в примеры.
var digestNoText = map[string]bool{"F0": true, "F5": true, "F9": true, "F10": true}

// themeLabels — короткие названия тем на языке HR (код, не LLM).
var themeLabels = map[string]string{
	"F1":  "непонятные задачи и ожидания",
	"F2":  "проблемы с доступами, техникой, СИЗ",
	"F3":  "не хватает поддержки руководителя",
	"F4":  "конфликт с наставником или коллегами",
	"F6":  "перегрузка",
	"F7":  "мысли об уходе",
	"F8":  "оплата или график не как обещали",
	"F10": "личные обстоятельства",
}

// reasonLabels — кнопки причин низкой оценки (bot_render.go) → название темы.
var reasonLabels = map[string]string{
	"Задачи и нагрузка": "причина: задачи и нагрузка",
	"Люди и общение":    "причина: люди и общение",
	"Условия и место":   "причина: условия и место",
	"Другое":            "причина: другое",
}

// ownerLabels — первый получатель маршрута категории → ответственный за тему.
var ownerLabels = map[string]string{
	"hr": "HR", "hr_bp": "HR-партнёр", "recruiter": "рекрутер", "manager": "руководитель подразделения",
	"security": "СБ", "duty": "дежурный площадки",
}

// DigestTheme — тема недели.
type DigestTheme struct {
	Key       string
	Label     string
	Category  string // F1…F10 или пусто (причина, текст)
	Newcomers int    // разных новичков
	Mentions  int    // сигналов всего
	Sites     []string
	Owner     *string
	Keywords  []string // общие слова текстов (группа ≥ 5 человек)
	Examples  []string // обезличенные примеры (тема ≥ 5 человек, не F10)
}

// ThemeSet — темы окна: видимые (≥ 5 человек), сколько скрыто и конфиденциальное.
type ThemeSet struct {
	Visible       []DigestTheme
	HiddenThemes  int            // тем, за которыми 1–4 человека
	Confidential  int            // разных новичков с F5/F9 (показывается, только если ≥ 5)
	AllCounts     map[string]int // ключ темы → новичков, включая скрытые (для динамики)
	Signals       int            // новичков с любым кодом F0–F10
	CriticalUsers int            // новичков с критическим кодом
}

type themeAcc struct {
	key      string
	category string
	people   map[uuid.UUID]bool
	sites    map[string]bool
	mentions int
	texts    []themeText
}

type themeText struct {
	emp  uuid.UUID
	text string // обезличенный
	keys map[string]bool
	form map[string]string
}

// isFlagCode — F0…F10.
func isFlagCode(c string) bool {
	if !strings.HasPrefix(c, "F") || len(c) < 2 || len(c) > 3 {
		return false
	}
	for _, r := range c[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ClusterThemes — темы по строкам окна. Чистая функция: её проверяют тесты без базы.
func ClusterThemes(rows []repositories.DigestText, tax *taxonomy.Taxonomy) ThemeSet {
	accs := map[string]*themeAcc{}
	var order []string
	get := func(key, cat string) *themeAcc {
		a := accs[key]
		if a == nil {
			a = &themeAcc{key: key, category: cat, people: map[uuid.UUID]bool{}, sites: map[string]bool{}}
			accs[key] = a
			order = append(order, key)
		}
		return a
	}
	signals, critical, conf := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	var freeTexts []themeText
	freeSite := map[uuid.UUID]string{}

	for _, r := range rows {
		if r.Codes.Has(taxonomy.CategoryQuestion) {
			continue // «вопрос человеку» — не сигнал настроения, его текст в темы не идёт
		}
		var codes []string
		noText := false
		for _, c := range r.Codes {
			if !isFlagCode(c) {
				continue // «вопрос человеку» и прочее — не сигнал настроения
			}
			codes = append(codes, c)
			signals[r.EmployeeID] = true
			if digestNoText[c] {
				noText = true
			}
			if cat, ok := tax.Get(c); ok && cat.Critical {
				critical[r.EmployeeID] = true
			} else if !ok && (c == "F0" || c == "F5" || c == "F7" || c == "F9") {
				critical[r.EmployeeID] = true
			}
		}
		// Уточнение к причине («Люди и общение → Наставник», clarify.go): своя тема-причина,
		// основа — как обычная кнопка причины.
		reason, clar := splitClarify(r.Reason)
		if clar != "" && !noText {
			a := get(themeReasonPrefix+r.Reason, "")
			a.people[r.EmployeeID] = true
			a.sites[r.Site] = true
			a.mentions++
		}
		var tt *themeText
		if !noText && strings.TrimSpace(r.Text) != "" {
			text := anonymize(r.Text, r.FullName)
			keys, forms := keywords(text)
			tt = &themeText{emp: r.EmployeeID, text: text, keys: keys, form: forms}
		}
		switch {
		case len(codes) > 0:
			for _, c := range codes {
				switch {
				case c == "F0":
					continue
				case digestConfidential[c]:
					conf[r.EmployeeID] = true
					continue
				}
				a := get(c, c)
				a.people[r.EmployeeID] = true
				a.sites[r.Site] = true
				a.mentions++
				if tt != nil {
					a.texts = append(a.texts, *tt)
				}
			}
		case reason != "" && reasonLabels[reason] != "" && (reason != "Другое" || tt == nil):
			a := get(themeReasonPrefix+reason, "")
			a.people[r.EmployeeID] = true
			a.sites[r.Site] = true
			a.mentions++
			if tt != nil {
				a.texts = append(a.texts, *tt)
			}
		case tt != nil:
			// Текст без кода (в том числе «Другое» + свои слова): группа по ключевым словам.
			freeTexts = append(freeTexts, *tt)
			freeSite[r.EmployeeID] = r.Site
		}
	}

	// Тексты без кода — по ключевым словам, одиночной связью (как вопросы без ответа).
	for _, group := range keywordGroups(freeTexts) {
		kw := groupKeywords(freeTexts, group, 3)
		key := themeTextPrefix + strings.Join(kw, " ")
		if len(kw) == 0 {
			key = themeTextPrefix + "прочее"
		}
		a := get(key, "")
		for _, i := range group {
			t := freeTexts[i]
			a.people[t.emp] = true
			a.sites[freeSite[t.emp]] = true
			a.mentions++
			a.texts = append(a.texts, t)
		}
	}

	set := ThemeSet{AllCounts: map[string]int{}, Signals: len(signals), CriticalUsers: len(critical), Confidential: len(conf)}
	for _, key := range order {
		a := accs[key]
		n := len(a.people)
		set.AllCounts[key] = n
		if n < AnonymityMinGroup {
			set.HiddenThemes++
			continue
		}
		th := DigestTheme{Key: key, Category: a.category, Newcomers: n, Mentions: a.mentions, Label: themeLabel(key, a)}
		for _, s := range orderedSites(a.sites) {
			th.Sites = append(th.Sites, s)
		}
		if a.category != "" {
			if cat, ok := tax.Get(a.category); ok && len(cat.Recipients) > 0 {
				if o := ownerLabels[cat.Recipients[0]]; o != "" {
					th.Owner = &o
				}
			}
		}
		th.Keywords, th.Examples = themeTexts(a)
		set.Visible = append(set.Visible, th)
	}
	sort.SliceStable(set.Visible, func(i, j int) bool {
		if set.Visible[i].Newcomers != set.Visible[j].Newcomers {
			return set.Visible[i].Newcomers > set.Visible[j].Newcomers
		}
		return set.Visible[i].Label < set.Visible[j].Label
	})
	return set
}

// themeLabel — название темы кодом. Тема из текстов — «тема: <ключевые слова>».
func themeLabel(key string, a *themeAcc) string {
	switch {
	case a.category != "":
		if l := themeLabels[a.category]; l != "" {
			return l
		}
		return a.category
	case strings.HasPrefix(key, themeReasonPrefix):
		base, clar := splitClarify(strings.TrimPrefix(key, themeReasonPrefix))
		l := reasonLabels[base]
		if l == "" {
			l = "причина: " + strings.ToLower(base)
		}
		if clar != "" {
			l += " — " + strings.ToLower(clar)
		}
		return l
	}
	kw := groupKeywordsAll(a.texts, 3)
	if len(kw) == 0 {
		return "другое"
	}
	return "тема: " + strings.Join(kw, ", ")
}

// themeTexts — ключевые слова самой большой текстовой подгруппы (если за ней ≥ 5 человек) и
// до трёх обезличенных примеров от разных новичков (тема ≥ 5 человек, не F10).
func themeTexts(a *themeAcc) ([]string, []string) {
	if len(a.texts) == 0 || len(a.people) < AnonymityMinGroup || digestNoText[a.category] {
		return nil, nil
	}
	var kws []string
	best, bestN := []int(nil), 0
	for _, g := range keywordGroups(a.texts) {
		people := map[uuid.UUID]bool{}
		for _, i := range g {
			people[a.texts[i].emp] = true
		}
		if len(people) > bestN {
			best, bestN = g, len(people)
		}
	}
	if bestN >= AnonymityMinGroup {
		kws = groupKeywords(a.texts, best, 3)
	}
	// Примеры: от разных новичков, свежие первыми, без дублей.
	var ex []string
	seenEmp, seenText := map[uuid.UUID]bool{}, map[string]bool{}
	for i := len(a.texts) - 1; i >= 0 && len(ex) < maxThemeExamples; i-- {
		t := a.texts[i]
		n := normText(t.text)
		if seenEmp[t.emp] || seenText[n] || n == "" || strings.Contains(t.text, maskName) {
			continue // текст с заглушкой имени не показываем: по нему можно догадаться, о ком речь
		}
		seenEmp[t.emp], seenText[n] = true, true
		ex = append(ex, clip(t.text))
	}
	return kws, ex
}

// keywordGroups — индексы текстов, сгруппированные одиночной связью по similar.
func keywordGroups(ts []themeText) [][]int {
	parent := make([]int, len(ts))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i := range ts {
		for j := 0; j < i; j++ {
			if similar(ts[i].keys, ts[j].keys) {
				parent[find(i)] = find(j)
			}
		}
	}
	groups := map[int][]int{}
	var roots []int
	for i := range ts {
		r := find(i)
		if _, ok := groups[r]; !ok {
			roots = append(roots, r)
		}
		groups[r] = append(groups[r], i)
	}
	out := make([][]int, 0, len(roots))
	for _, r := range roots {
		out = append(out, groups[r])
	}
	return out
}

// groupKeywords — до n самых частых слов группы, как их писали новички. В группе из
// нескольких текстов — только слова, встретившиеся хотя бы дважды.
func groupKeywords(ts []themeText, idx []int, n int) []string {
	freq := map[string]int{}
	forms := map[string]map[string]int{}
	for _, i := range idx {
		for k := range ts[i].keys {
			freq[k]++
			if forms[k] == nil {
				forms[k] = map[string]int{}
			}
			forms[k][ts[i].form[k]]++
		}
	}
	stems := make([]string, 0, len(freq))
	for k, c := range freq {
		if len(idx) > 1 && c < 2 {
			continue
		}
		stems = append(stems, k)
	}
	sort.Slice(stems, func(i, j int) bool {
		if freq[stems[i]] != freq[stems[j]] {
			return freq[stems[i]] > freq[stems[j]]
		}
		return stems[i] < stems[j]
	})
	if len(stems) > n {
		stems = stems[:n]
	}
	out := make([]string, 0, len(stems))
	for _, s := range stems {
		if f := topForm(forms[s]); utf8.RuneCountInString(f) > 0 {
			out = append(out, f)
		}
	}
	return out
}

func groupKeywordsAll(ts []themeText, n int) []string {
	idx := make([]int, len(ts))
	for i := range ts {
		idx[i] = i
	}
	return groupKeywords(ts, idx, n)
}

// orderedSites — площадки в порядке справочника.
func orderedSites(m map[string]bool) []string {
	var out []string
	for _, s := range overviewSites("") {
		if m[s] {
			out = append(out, s)
		}
	}
	return out
}
