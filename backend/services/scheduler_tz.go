package services

import (
	"context"
	"sync"
	"time"

	"main/domain"
	"main/pkg/logger"
)

// Пояса, окно 8–18 и праздники (FEATURE_TZ, план итерации 24.10 §3.3). Без флага правила
// прежние: domain.SiteOf — пояс площадки, пн–чт 9–18, пт 9–17, без праздников.
//
// С флагом:
//   - пояс — Employee.TZ, если задан и известен, иначе пояс площадки;
//   - окно — domain.WorkHours8to18 (8:00–18:00 пн–пт);
//   - праздники — федеральные даты РФ из кода (holidaysRU) плюс таблица holidays (Настройки).
//
// «10:00» шага — местное время новичка: FireAt строит момент в его зоне, в БД он лежит в UTC.

// holidaysTTL — как долго держим таблицу holidays в памяти.
const holidaysTTL = 10 * time.Minute

// holidaysRU — нерабочие праздничные дни РФ (ст. 112 ТК) в будни и переносы выходных: 2026 — по
// постановлению правительства; 2027 — по ТК (праздник в выходной → следующий рабочий день),
// переносы январских выходных 2 и 3 января 2027 ещё не учтены: сверить с постановлением и
// добавить в таблицу holidays. Таблица дополняет список (правка в Настройках), не заменяет.
var holidaysRU = map[string]string{
	"2026-01-01": "Новогодние каникулы", "2026-01-02": "Новогодние каникулы", "2026-01-05": "Новогодние каникулы",
	"2026-01-06": "Новогодние каникулы", "2026-01-07": "Рождество Христово", "2026-01-08": "Новогодние каникулы",
	"2026-01-09": "Перенос выходного", "2026-02-23": "День защитника Отечества", "2026-03-09": "Перенос: 8 Марта",
	"2026-05-01": "Праздник Весны и Труда", "2026-05-11": "Перенос: День Победы", "2026-06-12": "День России",
	"2026-11-04": "День народного единства", "2026-12-31": "Перенос выходного",
	"2027-01-01": "Новогодние каникулы", "2027-01-04": "Новогодние каникулы", "2027-01-05": "Новогодние каникулы",
	"2027-01-06": "Новогодние каникулы", "2027-01-07": "Рождество Христово", "2027-01-08": "Новогодние каникулы",
	"2027-02-23": "День защитника Отечества", "2027-03-08": "Международный женский день",
	"2027-05-03": "Перенос: Праздник Весны и Труда", "2027-05-10": "Перенос: День Победы",
	"2027-06-14": "Перенос: День России", "2027-11-04": "День народного единства",
}

// holidayCache — праздники из таблицы holidays с кодовым списком.
type holidayCache struct {
	mu     sync.Mutex
	set    map[string]bool
	loaded time.Time
}

// holidays — множество нерабочих дат; ошибка базы — кодовый список (расписание не падает).
func (s *SchedulerService) holidays(ctx context.Context) map[string]bool {
	c := &s.hol
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.set != nil && now.Sub(c.loaded) < holidaysTTL {
		return c.set
	}
	set := make(map[string]bool, len(holidaysRU))
	for d := range holidaysRU {
		set[d] = true
	}
	if s.repos != nil && s.repos.Enabled() {
		list, err := s.repos.Holidays.List(ctx, time.Time{}, time.Time{})
		if err != nil {
			logger.Warn("планировщик: праздники из базы не прочитаны, беру кодовый список: %v", err)
		}
		for _, h := range list {
			set[h.Date.Format(time.DateOnly)] = true
		}
	}
	c.set, c.loaded = set, now
	return set
}

// ResetHolidays — забыть кэш праздников (после правки в Настройках и в тестах).
func (s *SchedulerService) ResetHolidays() {
	s.hol.mu.Lock()
	s.hol.set = nil
	s.hol.mu.Unlock()
}

// SiteFor — календарь новичка: пояс, рабочее окно и праздники. Без FEATURE_TZ — прежние правила
// площадки. Все расчёты времени новичка (fire_at, окно напоминаний, «какой сегодня день») идут
// через него.
func (s *SchedulerService) SiteFor(e *domain.Employee) domain.Site {
	if e == nil {
		return domain.SiteOf("")
	}
	if !s.features.TZ {
		return domain.SiteOf(e.Site)
	}
	site := domain.WorkHours8to18(e.Site)
	if loc := employeeLoc(e); loc != nil {
		site.TZ, site.Loc = *e.TZ, loc
	}
	site.Holidays = s.holidays(context.Background())
	return site
}

// employeeLoc — зона Employee.TZ; nil — не задан или неизвестен (тогда пояс площадки).
func employeeLoc(e *domain.Employee) *time.Location {
	if e == nil || e.TZ == nil || *e.TZ == "" {
		return nil
	}
	loc, err := time.LoadLocation(*e.TZ)
	if err != nil {
		logger.Warn("планировщик: пояс %q новичка %s неизвестен — беру пояс площадки", *e.TZ, e.ID)
		return nil
	}
	return loc
}

// EffectiveTZ — пояс, по которому живёт расписание новичка (для карточки: «10:00 по времени
// новичка»), и пояс площадки.
func (s *SchedulerService) EffectiveTZ(e *domain.Employee) (tz, siteTZ string) {
	siteTZ = domain.SiteOf(e.Site).TZ
	return s.SiteFor(e).TZ, siteTZ
}

// NextWorkMoment — t, если оно в рабочем окне календаря site, иначе начало ближайшего рабочего
// дня (пятница 18:30 → понедельник 8:00 при окне 8–18).
func NextWorkMoment(t time.Time, site domain.Site) time.Time { return workStart(t, site) }
