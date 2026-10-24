package services

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Число при «вы» (address-vy.md, E): «вы» требует множественного числа — «чтобы вы пришли
// подготовленными», «чтобы вы смогли», «вы были рождены». Ни модель, ни словарь правила этого не
// гарантируют: «чтобы вы пришли подготовленный», «чтобы вы смог». vySingular находит такие слова
// в пределах фразы после «вы», CheckRewrite отклоняет ответ модели с подсказкой формы, а правило-
// фолбэк (RuleRewriteVy) правит их сам.
//
// Обращение «Дорогой/Дорогая {Имя}» — тоже род: обе формы получают «Здравствуйте, {Имя}!»
// (neutralGreeting).

// vyShortSg — краткие формы и прошедшее время единственного числа → множественное.
var vyShortSg = map[string]string{
	"готов": "готовы", "готова": "готовы", "рад": "рады", "рада": "рады",
	"уверен": "уверены", "уверена": "уверены", "согласен": "согласны", "согласна": "согласны",
	"должен": "должны", "должна": "должны", "обязан": "обязаны", "обязана": "обязаны",
	"доволен": "довольны", "довольна": "довольны", "способен": "способны", "способна": "способны",
	"знаком": "знакомы", "знакома": "знакомы", "сам": "сами", "сама": "сами",
	"смог": "смогли", "мог": "могли", "помог": "помогли", "был": "были", "была": "были",
	"привык": "привыкли", "достиг": "достигли", "вырос": "выросли",
}

// vySkip — слова между «вы» и сказуемым, которые не меняют число: «вы уже пришёл».
var vySkip = map[string]bool{
	"уже": true, "не": true, "ещё": true, "еще": true, "только": true, "тоже": true, "также": true,
	"бы": true, "же": true, "точно": true, "сразу": true, "всегда": true, "обязательно": true,
	"наверняка": true, "сегодня": true, "завтра": true, "вчера": true, "здесь": true, "тут": true,
}

// vyLinkVerbs — после них краткое причастие на -ен/-ён/-ан: «вы были рождены».
var vyLinkVerbs = map[string]bool{"были": true, "будете": true, "стали": true, "станете": true, "оказались": true}

// vyFunction — служебные слова после полного прилагательного: «подготовленный в свой первый
// день» — прилагательное про «вы», а не определение к существительному.
var vyFunction = map[string]bool{
	"в": true, "во": true, "на": true, "к": true, "ко": true, "и": true, "а": true, "но": true, "с": true,
	"со": true, "по": true, "за": true, "у": true, "для": true, "до": true, "от": true, "из": true,
	"без": true, "при": true, "о": true, "об": true, "как": true, "чтобы": true, "или": true,
}

// vyAdjEndings — полные прилагательные и причастия единственного числа → творительный
// множественного («пришли подготовленными»).
var vyAdjEndings = [][2]string{
	{"нный", "нными"}, {"нная", "нными"}, {"вший", "вшими"}, {"вшая", "вшими"},
	{"тый", "тыми"}, {"тая", "тыми"}, {"ный", "ными"}, {"ная", "ными"},
}

// vyBoundary — конец фразы между словами.
const vyBoundary = ".,!?;:—–\n()«»\""

type vyFix struct {
	start, end int
	word, repl string
}

// vySingular — слова единственного числа про «вы» в пределах фразы (до четырёх слов после «вы»).
func vySingular(s string) []vyFix {
	locs := reRuleWord.FindAllStringIndex(s, -1)
	var out []vyFix
	for i, loc := range locs {
		if strings.ToLower(s[loc[0]:loc[1]]) != "вы" {
			continue
		}
		content := 0 // слов сказуемого после «вы» (без vySkip)
		for j := i + 1; j < len(locs) && j <= i+4; j++ {
			if strings.ContainsAny(s[locs[j-1][1]:locs[j][0]], vyBoundary) {
				break
			}
			w := s[locs[j][0]:locs[j][1]]
			lw := strings.ToLower(w)
			if lw == "вы" {
				break
			}
			fix := func(repl string) {
				out = append(out, vyFix{start: locs[j][0], end: locs[j][1], word: w, repl: matchCase(w, repl)})
			}
			if vySkip[lw] {
				continue
			}
			content++
			if r, ok := vyShortSg[lw]; ok {
				fix(r)
				continue
			}
			n := utf8.RuneCountInString(lw)
			if content == 1 {
				if r := pastPlural(lw); r != "" {
					fix(r)
					continue
				}
			}
			prev := strings.ToLower(s[locs[j-1][0]:locs[j-1][1]])
			if vyLinkVerbs[prev] && n >= 5 && (strings.HasSuffix(lw, "ен") || strings.HasSuffix(lw, "ён") || strings.HasSuffix(lw, "ан")) {
				fix(lw + "ы")
				continue
			}
			if n >= 7 {
				for _, e := range vyAdjEndings {
					if !strings.HasSuffix(lw, e[0]) {
						continue
					}
					next := "" // следующее слово той же фразы
					if j+1 < len(locs) && !strings.ContainsAny(s[locs[j][1]:locs[j+1][0]], vyBoundary) {
						next = strings.ToLower(s[locs[j+1][0]:locs[j+1][1]])
					}
					if next == "" || vyFunction[next] {
						fix(strings.TrimSuffix(lw, e[0]) + e[1])
					}
					break
				}
			}
		}
	}
	return out
}

// pastPlural — прошедшее время единственного числа → множественное: пришёл → пришли,
// справился → справились, поняла → поняли. "" — не глагол прошедшего времени.
func pastPlural(w string) string {
	n := utf8.RuneCountInString(w)
	switch {
	case strings.HasSuffix(w, "шёл"), strings.HasSuffix(w, "шел"):
		return w[:len(w)-len("ёл")] + "ли"
	case n >= 5 && strings.HasSuffix(w, "лся"):
		return strings.TrimSuffix(w, "лся") + "лись"
	case n >= 6 && strings.HasSuffix(w, "лась"):
		return strings.TrimSuffix(w, "лась") + "лись"
	case n >= 4 && strings.HasSuffix(w, "ла") && w != "дела" && w != "сила":
		return strings.TrimSuffix(w, "ла") + "ли"
	case n >= 4 && strings.HasSuffix(w, "л"):
		return w + "и"
	}
	return ""
}

// fixVySingular — правило: слова единственного числа про «вы» во множественное.
func fixVySingular(s string) string {
	fixes := vySingular(s)
	for k := len(fixes) - 1; k >= 0; k-- {
		f := fixes[k]
		s = s[:f.start] + f.repl + s[f.end:]
	}
	return s
}

// vySingularProblem — проблема для ретрая: ««вы» … «подготовленный» — во множественном числе:
// «подготовленными»».
func vySingularProblem(s string) string {
	fixes := vySingular(s)
	if len(fixes) == 0 {
		return ""
	}
	var parts []string
	for _, f := range fixes {
		parts = append(parts, "«"+f.word+"» → «"+f.repl+"»")
	}
	return "при «вы» слова про сотрудника во множественном числе: " + strings.Join(parts, ", ")
}

// reDearName — «Дорогой {Имя},» / «Дорогая {имя}!» — обращение с родом.
var reDearName = regexp.MustCompile(`(?i)(^|[^\p{L}])(дорогой|дорогая)\s+(\{(?:Имя|имя|name)\})\s*[,!.]?`)

// neutralGreeting — «Дорогой {Имя},» → «Здравствуйте, {Имя}!» (в обеих формах обращения).
func neutralGreeting(s string) string {
	return reDearName.ReplaceAllStringFunc(s, func(m string) string {
		p := reDearName.FindStringSubmatch(m)
		return p[1] + "Здравствуйте, " + p[3] + "!"
	})
}
