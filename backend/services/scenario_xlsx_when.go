package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"main/domain"
)

// Колонка «когда» Excel-ТЗ (05-excel-tz-mapping.md §1.2, вердикт §1). Прозу переводим в день от
// D0 и время. Приближения: рабочие дни считаем календарными, месяц = 30 дней, «последний день
// ИС» = D+89 — импорт предупреждает об этом один раз на класс (when_approx).

// Виды строки по «когда».
const (
	whenTime     = "time"     // день (и, может быть, время) — блок по расписанию
	whenContinue = "continue" // пусто — продолжение предыдущего блока
	whenAfter    = "after"    // только время («в 09.01») — сразу после предыдущего, тот же блок
	whenBranch   = "branch"   // «В случае варианта ответа „Да“» — ветка последнего вопроса
	whenRule     = "rule"     // «Если сотрудник …» — глобальное правило сценария
	whenUnknown  = "unknown"
)

// Классы приближений для when_approx.
const (
	approxWorkdays = "workdays"
	approxMonths   = "months"
	approxProbEnd  = "probation_end"
)

// probationEndDay — «последний день ИС»: старт + 3 месяца − 1 день ≈ D+89.
const probationEndDay = 89

// officeSites — «для офисных сотрудников»: офисы GrPP и Лужники (производство — GTP).
var officeSites = domain.StringList{domain.SiteGrPP, domain.SiteLuzhniki}

type whenSpec struct {
	kind   string
	day    int
	time   string // HH:MM или пусто
	sites  domain.StringList
	approx string // класс приближения или пусто
	answer string // whenBranch: подпись ответа («Да»)
}

var (
	reWhenTime    = regexp.MustCompile(`(\d{1,2})[:.](\d{2})`)
	reWhenBefore  = regexp.MustCompile(`за\s+(\d+)(?:\s*-\s*(\d+))?\s+(?:дн|день|дня)`)
	reWhenDayBef  = regexp.MustCompile(`за\s+(?:один\s+)?день`)
	reWhenWorkday = regexp.MustCompile(`(\p{L}+)\s+рабоч\p{L}*\s+д`)
	reWhenAfterN  = regexp.MustCompile(`через\s+(\d+)\s*(дн|день|дня|недел|нед|мес)`)
	reWhenMonthW  = regexp.MustCompile(`через\s+(\d+)\s*мес\p{L}*\.?\s*(?:и\s+(\d+)\s*нед)?`)
	reWhenNthMon  = regexp.MustCompile(`первый\s+день\s+(\p{L}+)\s+месяц`)
	reWhenQuoted  = regexp.MustCompile(`[«"“]([^»"”]+)[»"”]`)
	reSpaces      = regexp.MustCompile(`\s+`)
)

var ordinals = map[string]int{
	"первый": 1, "первого": 1, "второй": 2, "второго": 2, "третий": 3, "третьего": 3,
	"четвертый": 4, "четвертого": 4, "пятый": 5, "пятого": 5, "шестой": 6, "шестого": 6,
	"седьмой": 7, "седьмого": 7, "восьмой": 8, "восьмого": 8, "девятый": 9, "девятого": 9,
	"десятый": 10, "десятого": 10,
}

// parseTZWhen — «когда» строки ТЗ. Старый формат (D−3, D0 10:00) тоже понимает.
func parseTZWhen(raw string) whenSpec {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.NewReplacer("−", "-", "–", "-", "—", "-", "ё", "е", " ", " ").Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	if s == "" {
		return whenSpec{kind: whenContinue}
	}
	if strings.Contains(s, "в случае варианта ответа") {
		ans := ""
		if m := reWhenQuoted.FindStringSubmatch(strings.TrimSpace(raw)); m != nil {
			ans = strings.TrimSpace(m[1])
		}
		return whenSpec{kind: whenBranch, answer: ans}
	}
	if strings.HasPrefix(s, "если ") {
		return whenSpec{kind: whenRule}
	}
	// Старый формат кабинета: D−3, D0 10:00, -1.
	if day, hhmm, ok := parseWhen(raw); ok {
		return whenSpec{kind: whenTime, day: day, time: hhmm}
	}

	w := whenSpec{kind: whenTime}
	if strings.Contains(s, "офис") {
		w.sites = append(domain.StringList{}, officeSites...)
	}
	if m := reWhenTime.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if h <= 23 && mm <= 59 {
			w.time = fmt.Sprintf("%02d:%02d", h, mm)
		}
		s = strings.Replace(s, m[0], " ", 1)
	}
	switch {
	case reWhenBefore.MatchString(s):
		m := reWhenBefore.FindStringSubmatch(s)
		a, _ := strconv.Atoi(m[1])
		b := a
		if m[2] != "" {
			b, _ = strconv.Atoi(m[2]) // «за 2–3 дня» — ранняя граница, как в вердикте (D−3)
		}
		w.day = -max(a, b)
	case reWhenDayBef.MatchString(s):
		w.day = -1
	case reWhenWorkday.MatchString(s):
		n, ok := ordinals[reWhenWorkday.FindStringSubmatch(s)[1]]
		if !ok {
			return whenSpec{kind: whenUnknown}
		}
		w.day = n - 1
		if n > 1 {
			w.approx = approxWorkdays
		}
	case reWhenNthMon.MatchString(s):
		n, ok := ordinals[reWhenNthMon.FindStringSubmatch(s)[1]]
		if !ok {
			return whenSpec{kind: whenUnknown}
		}
		w.day, w.approx = 30*(n-1), approxMonths
	case reWhenMonthW.MatchString(s):
		m := reWhenMonthW.FindStringSubmatch(s)
		mon, _ := strconv.Atoi(m[1])
		wk := 0
		if m[2] != "" {
			wk, _ = strconv.Atoi(m[2])
		}
		w.day, w.approx = 30*mon+7*wk, approxMonths
	case reWhenAfterN.MatchString(s):
		m := reWhenAfterN.FindStringSubmatch(s)
		n, _ := strconv.Atoi(m[1])
		switch {
		case strings.HasPrefix(m[2], "нед"):
			w.day = 7 * n
		case strings.HasPrefix(m[2], "мес"):
			w.day, w.approx = 30*n, approxMonths
		default:
			w.day = n
		}
	case strings.Contains(s, "последний день") && (strings.Contains(s, "испыт") || strings.Contains(s, " ис")):
		w.day, w.approx = probationEndDay, approxProbEnd
	case w.time != "" && strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "в.")) == "":
		// «В 09.01» — только время: сразу после предыдущего сообщения, тот же блок.
		return whenSpec{kind: whenAfter, time: w.time}
	default:
		return whenSpec{kind: whenUnknown}
	}
	if w.day > 365 || w.day < -60 {
		return whenSpec{kind: whenUnknown}
	}
	return w
}

// approxMessage — текст предупреждения when_approx для класса.
func approxMessage(class string) string {
	switch class {
	case approxWorkdays:
		return "Рабочие дни посчитаны календарными: «второй рабочий день» = D+1, «третий» = D+2. Проверьте шаги после выходных"
	case approxMonths:
		return "Месяц посчитан как 30 дней: «через 1 месяц и 2 недели» = D+44, «первый день второго месяца» = D+30"
	case approxProbEnd:
		return "«Последний день испытательного срока» поставлен на D+89 (3 месяца по 30 дней − 1)"
	}
	return ""
}
