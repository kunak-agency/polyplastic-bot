package services

import (
	"strconv"
	"strings"
	"time"

	"main/domain"
)

// Календарь таймлайна: когда шаг «D±N HH:MM» уходит конкретному новичку. Правила держит код
// (01-architecture.md): время площадки, рабочие часы, перенос с выходных. Праздники РФ и смены
// GTP — следующие итерации.

// defaultStepTime — время шага без своего времени (UX-спецификация п. 2.3: «иначе 10:00»).
const defaultStepTime = 10 * 60

// parseHHMM — «HH:MM» в минуты от полуночи; ok=false — пусто или не время.
func parseHHMM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	h, m, found := strings.Cut(s, ":")
	if !found {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 || len(m) != 2 {
		return 0, false
	}
	return hh*60 + mm, true
}

// civilDate — дата (год, месяц, день) как полночь в зоне loc. start из БД приходит полуночью
// UTC (колонка date): берём её календарные числа, а не момент.
func civilDate(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// FireAt — момент отправки шага: дата выхода + day_offset календарных дней, время шага по
// часам площадки.
//
//   - Выходной → следующий рабочий день. Исключение — шаги до выхода (day_offset < 0): если
//     перенос вперёд упирается в D0 или позже, шаг уходит в предыдущий рабочий день, иначе
//     «Завтра первый день» пришло бы в сам первый день.
//   - workHoursOnly: время раньше начала дня → начало дня, позже конца → конец дня (пятница —
//     до 17:00). Сдвигаем в пределах дня, а не на завтра: итог дня в пятницу 18:00 полезнее
//     в 17:00, чем в понедельник.
func FireAt(start time.Time, dayOffset int, hhmm string, site domain.Site, workHoursOnly bool) time.Time {
	d0 := civilDate(start, site.Loc)
	date := d0.AddDate(0, 0, dayOffset)
	if !site.IsWorkday(date.Weekday()) {
		fwd := nextWorkday(date, site)
		if dayOffset < 0 && !fwd.Before(d0) {
			date = prevWorkday(date, site)
		} else {
			date = fwd
		}
	}
	return atMinutes(date, stepMinutes(hhmm, date, site, workHoursOnly), site.Loc)
}

// NextWorkdayAt — «Напомнить завтра»: следующий рабочий день после now в то же время шага.
func NextWorkdayAt(now time.Time, hhmm string, site domain.Site, workHoursOnly bool) time.Time {
	date := nextWorkday(civilDate(now.In(site.Loc), site.Loc).AddDate(0, 0, 1), site)
	return atMinutes(date, stepMinutes(hhmm, date, site, workHoursOnly), site.Loc)
}

// DayNumber — какой сегодня день адаптации: 0 — день выхода, −3 — за три дня до него.
func DayNumber(start, now time.Time, site domain.Site) int {
	d0 := civilDate(start, site.Loc)
	today := civilDate(now.In(site.Loc), site.Loc)
	// Через полдень: переход на летнее время не даст 23-часовых суток и ошибки на единицу.
	return int(today.Add(12*time.Hour).Sub(d0.Add(12*time.Hour)).Hours() / 24)
}

// StartOfDay — полночь дня now на площадке.
func StartOfDay(now time.Time, site domain.Site) time.Time {
	return civilDate(now.In(site.Loc), site.Loc)
}

func stepMinutes(hhmm string, date time.Time, site domain.Site, workHoursOnly bool) int {
	mins, ok := parseHHMM(hhmm)
	if !ok {
		mins = defaultStepTime
	}
	if workHoursOnly {
		wd := date.Weekday()
		if lo := site.DayStart[wd]; mins < lo {
			mins = lo
		}
		if hi := site.DayEnd[wd]; mins > hi {
			mins = hi
		}
	}
	return mins
}

func atMinutes(date time.Time, mins int, loc *time.Location) time.Time {
	y, m, d := date.Date()
	return time.Date(y, m, d, mins/60, mins%60, 0, 0, loc)
}

func nextWorkday(date time.Time, site domain.Site) time.Time {
	for i := 0; i < 7 && !site.IsWorkday(date.Weekday()); i++ {
		date = date.AddDate(0, 0, 1)
	}
	return date
}

func prevWorkday(date time.Time, site domain.Site) time.Time {
	for i := 0; i < 7 && !site.IsWorkday(date.Weekday()); i++ {
		date = date.AddDate(0, 0, -1)
	}
	return date
}
