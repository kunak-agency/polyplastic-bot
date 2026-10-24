package domain

import (
	"time"
	_ "time/tzdata" // зоны в бинарнике: в контейнере и на машине без tzdata расписание то же
)

// Площадки кейса (api_contract.md: site — GrPP|Luzhniki|GTP).
const (
	SiteGrPP     = "GrPP"
	SiteLuzhniki = "Luzhniki"
	SiteGTP      = "GTP"
)

// Site — правила календаря площадки: часовой пояс и рабочие часы (ПВТР: пн–чт 9–18, пт 9–17).
// На хакатоне у всех трёх площадок Москва; смены GTP — следующая итерация.
type Site struct {
	Code string
	// TZ — IANA-имя пояса площадки (Employee.TZ переопределяет); Loc — он же, загруженный.
	TZ  string
	Loc *time.Location
	// DayStart, DayEnd — рабочие часы в минутах от полуночи по дням недели (time.Weekday).
	// DayEnd 0 — выходной.
	DayStart [7]int
	DayEnd   [7]int
	// Holidays — нерабочие праздничные даты («2006-01-02», календарь площадки); nil — праздников
	// нет (правила без FEATURE_TZ). Заполняет планировщик (services/scheduler_tz.go).
	Holidays map[string]bool
}

// TZMoscow — пояс всех трёх площадок на хакатоне.
const TZMoscow = "Europe/Moscow"

// moscow — Europe/Moscow. Пакет time/tzdata подключён выше, поэтому загрузка не падает.
var moscow = mustLoc(TZMoscow)

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// standardHours — пн–чт 9:00–18:00, пт 9:00–17:00, сб и вс — выходные.
func standardHours(code string) Site {
	s := Site{Code: code, TZ: TZMoscow, Loc: moscow}
	for d := time.Monday; d <= time.Friday; d++ {
		s.DayStart[d] = 9 * 60
		s.DayEnd[d] = 18 * 60
	}
	s.DayEnd[time.Friday] = 17 * 60
	return s
}

// WorkHours8to18 — правила площадки с окном 8:00–18:00 пн–пт (сб и вс — выходные): набор из
// плана итерации 24.10. Пока НЕ используется — Sites держат standardHours; переключает
// планировщик под FEATURE_TZ.
func WorkHours8to18(code string) Site {
	s := SiteOf(code)
	s.DayStart, s.DayEnd = [7]int{}, [7]int{}
	for d := time.Monday; d <= time.Friday; d++ {
		s.DayStart[d] = 8 * 60
		s.DayEnd[d] = 18 * 60
	}
	return s
}

// Sites — ЕДИНСТВЕННЫЙ справочник площадок.
var Sites = map[string]Site{
	SiteGrPP:     standardHours(SiteGrPP),
	SiteLuzhniki: standardHours(SiteLuzhniki),
	SiteGTP:      standardHours(SiteGTP),
}

// SiteCodes — допустимые коды в порядке показа.
var SiteCodes = []string{SiteGrPP, SiteLuzhniki, SiteGTP}

// SiteOf — правила площадки; неизвестный код — правила GrPP (Москва), чтобы расчёт не падал на
// старой записи. Валидация кода — на входе API.
func SiteOf(code string) Site {
	if s, ok := Sites[code]; ok {
		return s
	}
	return Sites[SiteGrPP]
}

// IsWorkday — рабочий ли день недели на площадке (без праздников; с ними — IsWorkDate).
func (s Site) IsWorkday(d time.Weekday) bool { return s.DayEnd[d] > 0 }

// IsWorkDate — рабочий ли календарный день date (его число в своей зоне): день недели рабочий
// и не праздник. Без Holidays — то же, что IsWorkday.
func (s Site) IsWorkDate(date time.Time) bool {
	return s.IsWorkday(date.Weekday()) && !s.Holidays[date.Format(time.DateOnly)]
}
