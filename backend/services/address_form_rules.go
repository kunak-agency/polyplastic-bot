package services

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Правило-фолбэк «ты» → «вы» (address-vy.md, E): словарная замена кодом, когда LLM недоступен
// или не прошёл проверки. Грубее модели, поэтому текст помечается source=rule («машинная
// правка») и HR его проверяет. Меняет: местоимения, «привет», частые повелительные формы,
// глаголы на -ешь/-ишь(ся), прошедшее время сразу после «ты», пары «готов/готова».

// ruleVyWords — слово целиком (строчными) → замена.
var ruleVyWords = map[string]string{
	"ты": "вы", "тебя": "вас", "тебе": "вам", "тобой": "вами", "тобою": "вами",
	"твой": "ваш", "твоя": "ваша", "твоё": "ваше", "твое": "ваше", "твои": "ваши",
	"твоего": "вашего", "твоей": "вашей", "твоих": "ваших", "твоим": "вашим", "твоими": "вашими",
	"твоему": "вашему", "твою": "вашу", "твоём": "вашем", "твоем": "вашем",
	"привет": "здравствуйте", "хочешь": "хотите", "давай": "давайте", "держи": "держите",
}

// ruleImperatives — повелительное наклонение «ты» → + «те» (расскажи → расскажите). Только
// список: по окончанию «-и» не отличить глагол от существительного. «пришли» нет — это ещё и
// прошедшее время («пришли подготовленными»).
var ruleImperatives = map[string]bool{
	"расскажи": true, "напиши": true, "пиши": true, "приходи": true, "возьми": true, "посмотри": true,
	"смотри": true, "заходи": true, "зайди": true, "звони": true, "позвони": true, "проверь": true,
	"приготовь": true, "подготовь": true, "будь": true, "сделай": true, "делай": true, "читай": true,
	"прочитай": true, "прочти": true, "нажми": true, "выбери": true, "ответь": true, "отправь": true,
	"заполни": true, "приложи": true, "загляни": true, "сохрани": true, "жми": true, "иди": true,
	"помни": true, "запомни": true, "найди": true, "скачай": true, "открой": true, "передай": true,
	"покажи": true, "представь": true, "оцени": true, "поставь": true, "укажи": true, "дай": true,
	"спроси": true, "уточни": true, "подожди": true, "начни": true, "продолжи": true, "попробуй": true,
	"используй": true, "отметь": true, "сообщи": true, "загрузи": true, "введи": true, "узнай": true,
	"получи": true, "пройди": true, "захвати": true, "забудь": true, "обрати": true, "пообедай": true,
	"отдохни": true, "приезжай": true, "подходи": true, "подойди": true,
	"прими": true, "помоги": true, "веди": true, "следи": true, "выйди": true, "приди": true,
	"зарегистрируй": true, "подпиши": true, "распечатай": true, "изучи": true, "посети": true,
	"напомни": true, "переходи": true, "перейди": true,
}

// ruleReflexiveIs — повелительные на «-ись» (свяжись → свяжитесь): только список, иначе
// «запись» станет «запитесь».
var ruleReflexiveIs = map[string]bool{
	"свяжись": true, "обратись": true, "поделись": true, "отметись": true, "дождись": true,
	"соберись": true, "садись": true, "учись": true, "возьмись": true, "вернись": true,
	"зарегистрируйся": true, "договорись": true, "переоденься": true, "убедись": true,
	"подпишись": true, "появись": true, "остановись": true, "явись": true,
}

// ruleShortAdj — краткие прилагательные про «ты» → множественное.
var ruleShortAdj = map[string]string{
	"готов": "готовы", "готова": "готовы", "рад": "рады", "рада": "рады",
	"уверен": "уверены", "уверена": "уверены", "согласен": "согласны", "согласна": "согласны",
}

// ruleNoVerb — слова на -ешь/-ишь, которые не глаголы.
var ruleNoVerb = map[string]bool{"мышь": true, "тишь": true, "глушь": true, "вошь": true, "брешь": true}

var (
	reRuleWord  = regexp.MustCompile(`\p{L}+(?:-\p{L}+)*`)
	reRuleSlash = regexp.MustCompile(`(\p{L}+)/(\p{L}+)`)
	// reRwQuoted — цитаты «…»: названия книг, курсов, кнопок. Их правило не трогает, а проверки не
	// требуют править («Ты способна на большее» — название книги).
	// Кавычки в ТЗ бывают разнобоем: «…", поэтому закрывающая — любая.
	reRwQuoted = regexp.MustCompile(`«[^«»\n]{1,200}?[»"”]|"[^"\n]{1,200}"|“[^“”\n]{1,200}”`)
)

// RuleRewriteVy — «ты» → «вы» словарём. Регистр первой буквы сохраняется.
func RuleRewriteVy(s string) string { return ruleRewrite(s, true) }

// ruleRewrite — greet=false: «Привет» не трогать (NeedsVy: приветствие — не обращение на «ты»).
func ruleRewrite(s string, greet bool) string {
	s = reRuleSlash.ReplaceAllStringFunc(s, func(m string) string {
		p := reRuleSlash.FindStringSubmatch(m)
		if pl, ok := genderPairPlural(p[1], p[2]); ok {
			return pl
		}
		return m
	})
	locs := reRuleWord.FindAllStringIndex(s, -1)
	quoted := reRwQuoted.FindAllStringIndex(s, -1)
	var b strings.Builder
	last, afterTy := 0, 0
	for _, loc := range locs {
		if inSpans(loc[0], quoted) {
			continue
		}
		w := s[loc[0]:loc[1]]
		lw := strings.ToLower(w)
		// Конец предложения обрывает «ты …»: в «Ты молодец. Пришёл…» род не про «ты».
		if strings.ContainsAny(s[last:loc[0]], ".!?\n") {
			afterTy = 0
		}
		repl := ruleVyWord(lw, afterTy > 0)
		if !greet && lw == "привет" {
			repl = ""
		}
		if afterTy > 0 {
			afterTy--
		}
		if lw == "ты" {
			afterTy = 3
		}
		b.WriteString(s[last:loc[0]])
		if repl != "" && repl != lw {
			b.WriteString(matchCase(w, repl))
		} else {
			b.WriteString(w)
		}
		last = loc[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

// ruleVyWord — замена одного слова (строчными) или "" — не менять. pastOK — слово стоит
// сразу после «ты»: прошедшее время в мужском/женском роде → множественное.
func ruleVyWord(w string, pastOK bool) string {
	if r, ok := ruleVyWords[w]; ok {
		return r
	}
	if ruleImperatives[w] {
		return w + "те"
	}
	if ruleReflexiveIs[w] {
		if strings.HasSuffix(w, "йся") {
			return strings.TrimSuffix(w, "йся") + "йтесь"
		}
		if strings.HasSuffix(w, "ься") {
			return strings.TrimSuffix(w, "ься") + "ьтесь"
		}
		return strings.TrimSuffix(w, "ись") + "итесь"
	}
	n := utf8.RuneCountInString(w)
	switch {
	case n >= 6 && strings.HasSuffix(w, "йся") && !strings.HasSuffix(w, "ийся") && !strings.HasSuffix(w, "ыйся"): // обращайся (не «являющийся»)
		return strings.TrimSuffix(w, "йся") + "йтесь"
	case n >= 6 && strings.HasSuffix(w, "ься") && !strings.HasSuffix(w, "ться") && !strings.HasSuffix(w, "чься") &&
		!strings.HasSuffix(w, "шься"): // познакомься (не «развлечься», не «насладишься»)
		return strings.TrimSuffix(w, "ься") + "ьтесь"
	}
	if !ruleNoVerb[w] && n >= 5 {
		for _, suf := range [][2]string{{"ешься", "етесь"}, {"ёшься", "ётесь"}, {"ишься", "итесь"}, {"ешь", "ете"}, {"ёшь", "ёте"}, {"ишь", "ите"}} {
			if strings.HasSuffix(w, suf[0]) {
				return strings.TrimSuffix(w, suf[0]) + suf[1]
			}
		}
	}
	if pastOK && ruleShortAdj[w] != "" { // «ты готов» → «вы готовы»; «пропуск готов» не трогаем
		return ruleShortAdj[w]
	}
	if pastOK && (w == "был" || w == "мог") {
		return w + map[string]string{"был": "и", "мог": "ли"}[w]
	}
	if pastOK && n >= 4 {
		switch {
		case strings.HasSuffix(w, "лся"):
			return strings.TrimSuffix(w, "лся") + "лись"
		case strings.HasSuffix(w, "лась"):
			return strings.TrimSuffix(w, "лась") + "лись"
		case strings.HasSuffix(w, "шёл"), strings.HasSuffix(w, "шел"):
			return w[:len(w)-len("ёл")] + "ли"
		case strings.HasSuffix(w, "ла") && w != "дела":
			return strings.TrimSuffix(w, "ла") + "ли"
		case strings.HasSuffix(w, "л"):
			return w + "и"
		}
	}
	return ""
}

// genderPairPlural — «Готов/Готова» → «Готовы», «пришёл/пришла» → «пришли»; ok=false — не пара.
func genderPairPlural(a, b string) (string, bool) {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	switch {
	case strings.HasSuffix(lb, "лась") && strings.HasSuffix(la, "лся"):
		return matchCase(a, strings.TrimSuffix(lb, "лась")+"лись"), true
	case strings.HasSuffix(lb, "ла") && (strings.HasSuffix(la, "л") || strings.HasSuffix(la, "ёл")):
		return matchCase(a, strings.TrimSuffix(lb, "ла")+"ли"), true
	case strings.HasSuffix(lb, "ая") && strings.HasSuffix(la, "ый"):
		return matchCase(a, strings.TrimSuffix(lb, "ая")+"ые"), true
	case lb == la+"а":
		return matchCase(a, la+"ы"), true
	}
	return "", false
}

// matchCase — регистр замены по образцу: «Ты» → «Вы», «ТЫ» → «ВЫ».
func matchCase(src, repl string) string {
	r, _ := utf8.DecodeRuneInString(src)
	if !unicode.IsUpper(r) {
		return repl
	}
	if utf8.RuneCountInString(src) > 1 && strings.ToUpper(src) == src {
		return strings.ToUpper(repl)
	}
	fr, size := utf8.DecodeRuneInString(repl)
	return string(unicode.ToUpper(fr)) + repl[size:]
}

// NeedsVy — в тексте есть обращение на «ты» или род, который правило знает: местоимение,
// глагол на -ешь/-ишь, повелительная форма из списка, «готов/готова».
// Цитаты «…» не в счёт.
func NeedsVy(s string) bool {
	s = unquoted(s)
	return reTyWords.MatchString(s) || isGendered(s) || ruleRewrite(s, false) != s
}

func inSpans(i int, spans [][]int) bool {
	for _, sp := range spans {
		if i >= sp[0] && i < sp[1] {
			return true
		}
	}
	return false
}

// firstRuleChange — первое слово, которое правило поменяло бы (для сообщения проверки).
func firstRuleChange(s string) (word, repl string) {
	for _, w := range reRuleWord.FindAllString(reRwQuoted.ReplaceAllString(s, "«»"), -1) {
		if r := ruleVyWord(strings.ToLower(w), false); r != "" && strings.ToLower(w) != "привет" {
			return w, matchCase(w, r)
		}
	}
	return "", ""
}
