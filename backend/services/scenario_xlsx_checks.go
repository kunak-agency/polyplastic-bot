package services

import (
	"regexp"
	"strconv"
	"strings"

	"main/domain"
)

// Проверки текста и сигналы вариантов при импорте ТЗ (06-excel-tz-line-by-line.md §1.3, §1.9,
// §1.10). Всё детерминированно, без LLM.

var (
	reGenderSlash = regexp.MustCompile(`(?i)(\p{L}+)/(\p{L}+)`)
	reGenderPast  = regexp.MustCompile(`(?i)(?:^|[^\p{L}])ты\s+(?:\p{L}+\s+){0,3}?\p{L}+(?:лся|л)(?:[^\p{L}]|$)`)
	reGenderWords = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(способна|подготовленный|подготовленная|готова)(?:[^\p{L}]|$)`)
	reHTTPLink    = regexp.MustCompile(`http://\S+`)
	reCommand     = regexp.MustCompile(`(?:^|\s)/([A-Za-z]\w*)`)
	reInputAsk    = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(составь|приложи\s+фотограф|пришли\s+фото|расскажи\s+о\s+себе)`)
	reCredentials = regexp.MustCompile(`(?i)(логин|парол)`)
)

// isGendered — текст в мужском/женском роде для новичка: «Готов/Готова», «ты справился», «способна».
func isGendered(s string) bool {
	for _, m := range reGenderSlash.FindAllStringSubmatch(s, -1) {
		a, b := strings.ToLower(m[1]), strings.ToLower(m[2])
		if b == a+"а" || a == b+"а" {
			return true
		}
	}
	return reGenderPast.MatchString(s) || reGenderWords.MatchString(s)
}

// isMixedForm — в одном сообщении и «ты», и «вы».
func isMixedForm(s string) bool {
	return len(reTyWords.FindAllString(s, -1)) > 0 && len(reVyWords.FindAllString(s, -1)) > 0
}

// isCredentials — «отправка всех доступов», логины и пароли: в мессенджер слать нельзя.
func isCredentials(meta, text string) bool {
	lo := strings.ToLower(meta)
	return (strings.Contains(lo, "отправк") && strings.Contains(lo, "доступ")) || reCredentials.MatchString(text)
}

// optionFlag — сигнал варианта ответа по таблице 06 §1.3.
func optionFlag(label string) (domain.OptionFlag, bool) {
	lo := strings.ToLower(strings.ReplaceAll(label, "ё", "е"))
	has := func(w ...string) bool {
		for _, x := range w {
			if !strings.Contains(lo, x) {
				return false
			}
		}
		return true
	}
	switch {
	case has("уход"):
		return domain.OptionFlag{Code: "F7", Severity: "high"}, true
	case has("не уверен"):
		return domain.OptionFlag{Code: "F7", Severity: "medium"}, true
	case has("трудност", "документ"):
		return domain.OptionFlag{Code: "F2", Severity: "medium"}, true
	case has("никак"), has("все заняты"):
		return domain.OptionFlag{Code: "F4", Severity: "medium"}, true
	case has("ничего не понял"), has("жду разъяснен"), has("нечетк"):
		return domain.OptionFlag{Code: "F1", Severity: "medium"}, true
	case has("отстаю") && !has("немного"):
		return domain.OptionFlag{Code: "F1", Severity: "medium"}, true
	case has("безразлич"):
		return domain.OptionFlag{Code: "F6", Severity: "low"}, true
	case has("устал"):
		return domain.OptionFlag{Code: "F6", Severity: "medium"}, true
	case has("никто"):
		return domain.OptionFlag{Code: "F3", Severity: "medium"}, true
	}
	return domain.OptionFlag{}, false
}

// scaleFlagCode — тема шкалы → код флага для оценок ниже порога (06 §1.3).
func scaleFlagCode(text string) string {
	lo := strings.ToLower(text)
	switch {
	case strings.Contains(lo, "руководител"):
		return "F3"
	case strings.Contains(lo, "коллег"), strings.Contains(lo, "подразделени"):
		return "F4"
	case strings.Contains(lo, "доступ"), strings.Contains(lo, "техник"):
		return "F2"
	case strings.Contains(lo, " hr"):
		return "F8"
	}
	return ""
}

// applyQuestionFlags — option_flags вопроса: choice/multi — по подписи варианта (ключ — номер с
// 1), шкала — оценки ниже порога по теме вопроса.
func applyQuestionFlags(q *domain.SurveyQuestion) {
	set := func(k string, f domain.OptionFlag) {
		if q.OptionFlags == nil {
			q.OptionFlags = map[string]domain.OptionFlag{}
		}
		q.OptionFlags[k] = f
	}
	switch q.Type {
	case "choice", "multi":
		for i, o := range q.Options {
			if f, ok := optionFlag(o); ok {
				set(strconv.Itoa(i+1), f)
			}
		}
	case "scale":
		code := scaleFlagCode(q.Text)
		if code == "" || q.Threshold == nil {
			return
		}
		for v := 1; v < *q.Threshold; v++ {
			sev := "low"
			if v <= 4 {
				sev = "medium"
			}
			set(strconv.Itoa(v), domain.OptionFlag{Code: code, Severity: sev})
		}
	}
}
