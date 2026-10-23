// Package flags — regex-триггеры флагов по основам слов. Срабатывают в Go независимо от
// разметки rag-сервиса (POST /flags): «правила держит код, а не промпт» (a_core_stack.md).
// Триггер — сигнал «посмотреть», а не вердикт: отрицание («меня не уволят») тоже срабатывает,
// смысл уточняет LLM-разметка и человек.
//
// Коды F1–F10 — таксономия кейса. Здесь только те, у которых есть надёжные основы:
//   - F7 — увольнение, риск ухода;
//   - F5 — физическое насилие на площадке;
//   - F9 — коррупция (взятка, откат).
//
// F5 и F9 не уходят руководителю новичка — маршрут решает код обращений, не этот пакет.
package flags

import (
	"regexp"
	"strings"
)

// Коды флагов.
const (
	CodeViolence   = "F5"
	CodeQuit       = "F7"
	CodeCorruption = "F9"
)

// Hit — сработавший триггер: код флага и фрагмент текста, на котором он сработал (для цитаты
// в карточке обращения).
type Hit struct {
	Code    string
	Trigger string
}

// wordStart / wordEnd — граница слова для кириллицы. \b в RE2 понимает только ASCII-буквы,
// и для «уволюсь» он не видит границы вовсе.
const (
	wordStart = `(?:^|[^\p{L}\p{N}])`
	wordEnd   = `(?:$|[^\p{L}\p{N}])`
)

// rule — основа слова и код. Основы подобраны так, чтобы не ловить соседние слова:
//   - «увол» покрывает уволю(сь), уволили, уволен, увольнение, увольняют (основы «уволь» и
//     «увольня» из постановки — его частные случаи);
//   - «ударил» — ударил/а/и, но не «ударение», «ударный», «удар током»;
//   - «взятк», «взяточ», «взяток» — все формы «взятки», но не «взят», «взятый»;
//   - «откат» — только формы существительного с концом слова: «откатить тележку» на
//     производстве звучит каждый день и флагом не является.
type rule struct {
	code string
	re   *regexp.Regexp
}

var rules = []rule{
	{CodeQuit, stem(`увол\p{L}*`)},
	{CodeViolence, stem(`ударил\p{L}*`)},
	{CodeCorruption, stem(`(?:взятк\p{L}*|взяточ\p{L}*|взяток)`)},
	{CodeCorruption, stem(`откат(?:ы|а|у|ом|е|ов|ам|ами|ах)?`)},
}

// stem — регулярка «слово целиком, начинается с основы». (?i) в RE2 делает простое
// Unicode-сворачивание регистра, кириллица в нём есть.
func stem(word string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + wordStart + `(` + word + `)` + wordEnd)
}

// Detect — флаги в тексте, по одному на код, в порядке первого срабатывания. ё и е не
// различаются: в основах её нет, а в живых сообщениях пишут как попало.
func Detect(text string) []Hit {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "ё", "е"), "Ё", "Е")
	var hits []Hit
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.code] {
			continue
		}
		if m := r.re.FindStringSubmatch(text); m != nil {
			seen[r.code] = true
			hits = append(hits, Hit{Code: r.code, Trigger: m[1]})
		}
	}
	return hits
}

// Codes — только коды сработавших флагов.
func Codes(text string) []string {
	var out []string
	for _, h := range Detect(text) {
		out = append(out, h.Code)
	}
	return out
}
