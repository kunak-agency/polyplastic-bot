package services

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"main/externalApi/rag"
	"main/repositories"

	"github.com/google/uuid"
)

// «Вопросы без ответа» (UX 3.6): отказы rag на вопросы новичков, сгруппированные по похожести.
// Без LLM: нормализация текста, стоп-слова, грубый стемминг окончаний и одиночная связь по
// общим ключевым словам. Только вопросы новичков с согласием на обработку сообщений (без него
// текст не сохраняется вовсе), тексты обезличиваются до ответа.

// unansweredReasons — отказы, которые значат «в документах нет»: routed_topic — намеренная
// передача человеку (зарплата, увольнение…), это не пробел базы знаний.
var unansweredReasons = []string{rag.AbstainNoSource, rag.AbstainBelowThreshold}

// Пределы ответа.
const (
	maxClusters        = 50
	maxClusterExamples = 3
	maxExampleRunes    = 160
)

// UnansweredFilter — фильтр кабинета.
type UnansweredFilter struct {
	Site        string
	Days        int
	IncludeTest bool
}

// UnansweredCluster — группа похожих вопросов.
type UnansweredCluster struct {
	Key       string   // ключевые слова через пробел — стабильный ключ группы
	Title     string   // самый типичный вопрос группы (обезличен)
	Keywords  []string // до трёх слов, как их писали новички
	Count     int
	Newcomers int
	Reasons   map[string]int
	Examples  []string // до трёх других формулировок, свежие первыми
	FirstAt   time.Time
	LastAt    time.Time
}

// UnansweredResult — группы и сколько всего вопросов разобрано.
type UnansweredResult struct {
	Clusters  []UnansweredCluster
	Questions int
	Since     time.Time
}

// Unanswered — группы отказов за f.Days дней.
func (s *DocumentService) Unanswered(ctx context.Context, f UnansweredFilter) (*UnansweredResult, error) {
	since := s.now().Add(-time.Duration(f.Days) * 24 * time.Hour)
	out := &UnansweredResult{Clusters: []UnansweredCluster{}, Since: since}
	if !s.repos.Enabled() {
		return out, nil
	}
	rows, err := s.repos.Documents.Unanswered(ctx, repositories.UnansweredFilter{
		Since: since, Site: f.Site, IncludeTest: f.IncludeTest, Reasons: unansweredReasons,
	})
	if err != nil {
		return nil, err
	}
	out.Questions = len(rows)
	out.Clusters = clusterQuestions(rows)
	return out, nil
}

// question — вопрос, подготовленный к группировке.
type question struct {
	row   repositories.UnansweredRow
	text  string          // обезличенный
	keys  map[string]bool // стеммы ключевых слов
	forms map[string]string
}

// clusterQuestions — одиночная связь: вопрос попадает в группу, если похож хотя бы на один её
// вопрос (similar). Группы, связанные новым вопросом, сливаются.
func clusterQuestions(rows []repositories.UnansweredRow) []UnansweredCluster {
	qs := make([]question, 0, len(rows))
	for _, r := range rows {
		text := anonymize(r.Question, r.FullName)
		keys, forms := keywords(text)
		qs = append(qs, question{row: r, text: text, keys: keys, forms: forms})
	}
	parent := make([]int, len(qs))
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
	for i := range qs {
		for j := 0; j < i; j++ {
			if similar(qs[i].keys, qs[j].keys) || (len(qs[i].keys) == 0 && len(qs[j].keys) == 0 && normText(qs[i].text) == normText(qs[j].text)) {
				parent[find(i)] = find(j)
			}
		}
	}
	groups := map[int][]int{}
	var order []int
	for i := range qs {
		root := find(i)
		if _, ok := groups[root]; !ok {
			order = append(order, root)
		}
		groups[root] = append(groups[root], i)
	}
	out := make([]UnansweredCluster, 0, len(groups))
	for _, root := range order {
		out = append(out, buildCluster(qs, groups[root]))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastAt.After(out[j].LastAt)
	})
	if len(out) > maxClusters {
		out = out[:maxClusters]
	}
	return out
}

// similar — общих ключевых слов два и больше, или половина объединения (Жаккар ≥ 0.5), или
// короткий вопрос (до двух ключевых слов) целиком входит в другой: «Есть ли ДМС?» и «а ДМС с
// какого месяца» — одна тема.
func similar(a, b map[string]bool) bool {
	shared := 0
	for k := range a {
		if b[k] {
			shared++
		}
	}
	if shared == 0 {
		return false
	}
	small := min(len(a), len(b))
	union := len(a) + len(b) - shared
	return shared >= 2 || float64(shared)/float64(union) >= 0.5 || (small <= 2 && shared == small)
}

func buildCluster(qs []question, idx []int) UnansweredCluster {
	c := UnansweredCluster{Reasons: map[string]int{}, Count: len(idx)}
	people := map[uuid.UUID]bool{}
	freq := map[string]int{}
	forms := map[string]map[string]int{}
	for _, i := range idx {
		q := qs[i]
		people[q.row.EmployeeID] = true
		c.Reasons[q.row.AbstainReason]++
		if c.FirstAt.IsZero() || q.row.CreatedAt.Before(c.FirstAt) {
			c.FirstAt = q.row.CreatedAt
		}
		if q.row.CreatedAt.After(c.LastAt) {
			c.LastAt = q.row.CreatedAt
		}
		for k := range q.keys {
			freq[k]++
			if forms[k] == nil {
				forms[k] = map[string]int{}
			}
			forms[k][q.forms[k]]++
		}
	}
	c.Newcomers = len(people)
	stems := make([]string, 0, len(freq))
	for k := range freq {
		stems = append(stems, k)
	}
	sort.Slice(stems, func(i, j int) bool {
		if freq[stems[i]] != freq[stems[j]] {
			return freq[stems[i]] > freq[stems[j]]
		}
		return stems[i] < stems[j]
	})
	if len(stems) > 3 {
		stems = stems[:3]
	}
	// В группе из нескольких вопросов — только общие слова: случайное слово одного вопроса
	// тему не описывает.
	if len(idx) > 1 {
		common := stems[:0:0]
		for _, st := range stems {
			if freq[st] > 1 {
				common = append(common, st)
			}
		}
		if len(common) > 0 {
			stems = common
		}
	}
	c.Keywords = make([]string, 0, len(stems))
	for _, st := range stems {
		c.Keywords = append(c.Keywords, topForm(forms[st]))
	}
	c.Key = strings.Join(stems, " ")
	// Заголовок — самый типичный вопрос: наибольшая средняя частота его ключевых слов в группе.
	// Вопрос с заглушками ([имя], [телефон]) — только если других нет; при равенстве — короче.
	best, bestScore := -1, -1.0
	for _, i := range idx {
		score := 0.0
		for k := range qs[i].keys {
			score += float64(freq[k])
		}
		if n := len(qs[i].keys); n > 0 {
			score /= float64(n)
		}
		if strings.Contains(qs[i].text, "[") {
			score -= 1000
		}
		if best < 0 || score > bestScore || (score == bestScore && utf8.RuneCountInString(qs[i].text) < utf8.RuneCountInString(qs[best].text)) {
			best, bestScore = i, score
		}
	}
	c.Title = clip(qs[best].text)
	seen := map[string]bool{normText(c.Title): true}
	c.Examples = []string{}
	for k := len(idx) - 1; k >= 0 && len(c.Examples) < maxClusterExamples; k-- {
		t := clip(qs[idx[k]].text)
		if n := normText(t); !seen[n] {
			seen[n] = true
			c.Examples = append(c.Examples, t)
		}
	}
	return c
}

func topForm(m map[string]int) string {
	best, n := "", -1
	for f, c := range m {
		if c > n || (c == n && f < best) {
			best, n = f, c
		}
	}
	return best
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxExampleRunes {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:maxExampleRunes-1])) + "…"
}

func normText(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(s, "ё", "е")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// --- обезличивание ---

// Заглушки на месте персональных данных.
const (
	maskName  = "[имя]"
	maskPhone = "[телефон]"
	maskEmail = "[почта]"
	maskNum   = "[номер]"
)

var (
	reEmail  = regexp.MustCompile(`[\p{L}\p{N}._%+\-]+@[\p{L}\p{N}.\-]+\.\p{L}{2,}`)
	rePhoneQ = regexp.MustCompile(`\+?\d[\d\s()\-]{8,}\d`)
	reDigits = regexp.MustCompile(`\d{5,}`)
)

// keepCapitalized — слова с заглавной, которые не имена: площадки, компания, бот, мессенджер.
var keepCapitalized = map[string]bool{
	"лужники": true, "полипластик": true, "корсик": true, "макс": true, "max": true, "грпп": true, "гтп": true,
	"москва": true, "москве": true, "москвы": true, "пвтр": true, "дмс": true, "hr": true,
}

// anonymize — почта, телефоны, длинные номера (табельный, паспорт, карта), части имени
// самого новичка и слова с заглавной буквы не в начале предложения (похожи на имена и фамилии
// коллег) заменяются заглушками.
func anonymize(text, fullName string) string {
	text = reEmail.ReplaceAllString(text, maskEmail)
	text = rePhoneQ.ReplaceAllString(text, maskPhone)
	text = reDigits.ReplaceAllString(text, maskNum)
	own := nameStems(fullName)
	var b strings.Builder
	sentenceStart := true
	runes := []rune(text)
	for i := 0; i < len(runes); {
		r := runes[i]
		if !unicode.IsLetter(r) {
			if r == '.' || r == '!' || r == '?' || r == '\n' {
				sentenceStart = true
			}
			if r == '[' { // заглушка целиком, её буквы — не слово
				if j := indexRune(runes[i:], ']'); j > 0 {
					b.WriteString(string(runes[i : i+j+1]))
					i += j + 1
					sentenceStart = false
					continue
				}
			}
			b.WriteRune(r)
			i++
			continue
		}
		j := i
		for j < len(runes) && (unicode.IsLetter(runes[j]) || runes[j] == '-') {
			j++
		}
		word := string(runes[i:j])
		lower := strings.ToLower(word)
		switch {
		case ownName(lower, own):
			b.WriteString(maskName)
		case !sentenceStart && looksLikeName(word) && !keepCapitalized[lower]:
			b.WriteString(maskName)
		default:
			b.WriteString(word)
		}
		sentenceStart = false
		i = j
	}
	return strings.TrimSpace(b.String())
}

func indexRune(rs []rune, r rune) int {
	for i, x := range rs {
		if x == r {
			return i
		}
	}
	return -1
}

// looksLikeName — «Иван», «Петровой»: заглавная кириллица и дальше строчные, не меньше трёх
// букв. Аббревиатуры («ДМС», «СИЗ») и латиницу (Excel, Wi-Fi) не трогаем.
func looksLikeName(w string) bool {
	rs := []rune(w)
	if len(rs) < 3 || !unicode.IsUpper(rs[0]) || !unicode.Is(unicode.Cyrillic, rs[0]) {
		return false
	}
	for _, r := range rs[1:] {
		if unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

// nameStems — основы частей ФИО новичка (без пометок в скобках): «Иванов» ловит и «Иванова».
func nameStems(fullName string) []string {
	if k := strings.IndexRune(fullName, '('); k >= 0 {
		fullName = fullName[:k]
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(fullName)) {
		w = strings.Trim(w, ".,")
		rs := []rune(w)
		switch {
		case len(rs) < 3:
			continue
		case len(rs) >= 5:
			rs = rs[:len(rs)-2]
		case len(rs) == 4:
			rs = rs[:3]
		}
		out = append(out, string(rs))
	}
	return out
}

func ownName(lower string, stems []string) bool {
	for _, s := range stems {
		if strings.HasPrefix(lower, s) && utf8.RuneCountInString(lower) <= utf8.RuneCountInString(s)+4 {
			return true
		}
	}
	return false
}

// --- ключевые слова ---

// stopWords — служебные слова и вежливые обороты вопросов: по ним группы не строятся.
var stopWords = toSet(`а без бы был была были было быть в вам вас весь во вот все всё всего всех вы где да даже для до его ее её если есть еще ещё же за здесь и из или им их к как какая какие каким какой когда кого ком кто куда ли либо мне может можно мой моя мы на над надо нам нас не него нее неё нет ни них ничего но ну о об обо он она они оно от по под подскажите пожалуйста после при про раз с сам свой себе себя сейчас скажите сколько со так такой там тебе тебя то тоже только тот ту тут у уже хочу хотел хотела чем через что чтобы чтоб это этот эта эти этом я вопрос узнать нужно нужен нужна здравствуйте привет добрый день бот корсик спасибо будет буду ли
	какого каком какую каких которые который которая дается даётся бывает получится получить`)

func toSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// endings — окончания для грубого стемминга, длинные первыми.
var endings = []string{
	"иями", "ями", "ами", "ией", "иях", "ого", "ему", "ому", "ыми", "ими", "ее", "ие", "ые", "ое", "ей", "ий", "ый", "ой",
	"ем", "им", "ым", "ом", "ах", "ях", "ую", "юю", "ая", "яя", "ою", "ею", "ов", "ев", "ам", "ям", "ия", "ья",
	"а", "я", "о", "е", "ы", "и", "у", "ю", "ь", "й",
}

// stem — основа слова: снимается самое длинное окончание, если остаётся не меньше 4 букв.
func stem(w string) string {
	n := utf8.RuneCountInString(w)
	for _, e := range endings {
		if strings.HasSuffix(w, e) && n-utf8.RuneCountInString(e) >= 4 {
			return strings.TrimSuffix(w, e)
		}
	}
	return w
}

// keywords — стеммы значимых слов текста и их словоформы (для показа).
func keywords(text string) (map[string]bool, map[string]string) {
	keys, forms := map[string]bool{}, map[string]string{}
	words := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(text, "ё", "е")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '[' && r != ']'
	})
	for _, w := range words {
		if strings.HasPrefix(w, "[") || utf8.RuneCountInString(w) < 3 || stopWords[w] {
			continue
		}
		s := stem(w)
		keys[s] = true
		if _, ok := forms[s]; !ok {
			forms[s] = w
		}
	}
	return keys, forms
}
