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
	Loc  *time.Location
	// DayStart, DayEnd — рабочие часы в минутах от полуночи по дням недели (time.Weekday).
	// DayEnd 0 — выходной.
	DayStart [7]int
	DayEnd   [7]int
}

// moscow — Europe/Moscow. Пакет time/tzdata подключён выше, поэтому загрузка не падает.
var moscow = mustLoc("Europe/Moscow")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// standardHours — пн–чт 9:00–18:00, пт 9:00–17:00, сб и вс — выходные.
func standardHours(code string) Site {
	s := Site{Code: code, Loc: moscow}
	for d := time.Monday; d <= time.Friday; d++ {
		s.DayStart[d] = 9 * 60
		s.DayEnd[d] = 18 * 60
	}
	s.DayEnd[time.Friday] = 17 * 60
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

// IsWorkday — рабочий ли день на площадке. Праздники РФ не учтены (TODO: производственный
// календарь).
func (s Site) IsWorkday(d time.Weekday) bool { return s.DayEnd[d] > 0 }
