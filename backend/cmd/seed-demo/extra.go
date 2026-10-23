package main

// Добор персон сверх data/personas.yaml: детерминированно (seed), без LLM, из шаблонов ниже.
// Нужен, чтобы на показе в «Обзоре» открылась тепловая карта (порог анонимности 5 человек
// на ячейку) и тренд по неделям на каждой площадке.
//
// Пропорции флагов на весь поток (50 из yaml + добор) — ресерч 2026-10-23, блок C:
// F1 ≈ 25 %, F6 ≈ 15 %, F2 ≈ 15 % (выше на GTP), F3+F4 ≈ 10 %, F7 ≈ 8 %, F8 ≈ 8 %, F5 ≈ 3 %,
// F9 ≈ 2 %, F10 ≈ 4 %, без флагов 45–50 %, красных 10–15 %. Квоты добора ниже рассчитаны на
// 100 персон (итого 150) и сдвинуты по площадкам так, чтобы темы набирали ≥ 5 человек в ячейке:
// задачи и нагрузка — везде, доступы/СИЗ, уход и деньги — на производстве, руководитель — в офисе.
// При другом -count квоты масштабируются пропорционально.

import (
	"fmt"
	"math/rand"
	"sort"

	"main/domain"
)

const extraSeed = genSeed + 1

// extraPlan — площадки и флаги добора на 100 персон.
var (
	extraSiteOrder = []string{domain.SiteGTP, domain.SiteGrPP, domain.SiteLuzhniki}
	extraSites     = map[string]int{domain.SiteGTP: 44, domain.SiteGrPP: 32, domain.SiteLuzhniki: 24}
	extraFlags     = map[string]map[string]int{
		domain.SiteGTP:      {"F1": 10, "F2": 9, "F6": 6, "F7": 6, "F8": 6, "F4": 3, "F10": 2, "F5": 1, "F9": 1},
		domain.SiteGrPP:     {"F1": 9, "F3": 6, "F6": 5, "F2": 3, "F7": 1, "F8": 1, "F10": 1, "F5": 1, "F9": 1},
		domain.SiteLuzhniki: {"F1": 7, "F6": 5, "F2": 2, "F8": 1, "F4": 1, "F7": 1, "F10": 1},
	}
	// extraOpenP0 — сколько сигналов P0 (F5, F7, F9) на площадке остаются нерешёнными: это
	// красные зоны добора. Остальные P0 решены между опросами — тема в карте, без красного.
	extraOpenP0 = map[string]int{domain.SiteGTP: 4, domain.SiteGrPP: 3, domain.SiteLuzhniki: 1}
	// extraSevere — код, первый сигнал которого на площадке severity 3 и не решён (красная
	// зона «сигнал … в сообщении боту»). Вместе с открытыми P0 и 50 персонами yaml красных ≈ 11 %.
	extraSevere = map[string]string{domain.SiteGTP: "F1", domain.SiteLuzhniki: "F6"}
	// Только кандидат в «flaggedShare» персон площадки получает флаги (≈ 52 % добора).
	flaggedShare = 0.52

	jobsPlant = [][2]string{
		{"Цех экструзии", "Оператор линии экструзии"}, {"Цех экструзии", "Укладчик-упаковщик"},
		{"Цех экструзии", "Мастер смены"}, {"Цех литья фитингов", "Оператор термопластавтомата"},
		{"Цех литья фитингов", "Наладчик оборудования"}, {"Склад готовой продукции", "Водитель погрузчика"},
		{"Склад сырья", "Кладовщик"}, {"ОТК", "Контролёр ОТК"}, {"Энергослужба", "Электромонтёр"},
		{"Ремонтно-механический участок", "Слесарь-ремонтник"},
	}
	jobsOffice = [][2]string{
		{"Отдел продаж", "Менеджер по продажам"}, {"Бухгалтерия", "Бухгалтер"}, {"Финансы", "Финансовый аналитик"},
		{"Закупки", "Специалист по закупкам"}, {"Логистика", "Логист"}, {"ИТ-департамент", "Специалист техподдержки"},
		{"HR", "Специалист по кадрам"}, {"Маркетинг", "Маркетолог"}, {"Юридический отдел", "Юрист"},
	}
	// Фамилии мужского рода на -ов/-ев/-ин: женская форма — плюс «а». Пересечений с
	// personas.yaml и сидом нет.
	surnames = []string{
		"Андреев", "Беляев", "Виноградов", "Голованов", "Давыдов", "Ефимов", "Журавлёв", "Захаров", "Исаев",
		"Калинин", "Лазарев", "Макаров", "Назаров", "Осипов", "Родионов", "Симонов", "Титов", "Ульянов",
		"Фролов", "Харитонов", "Цветков", "Чистяков", "Шаров", "Юдин", "Яшин", "Агафонов", "Бобров",
		"Воронин", "Гордеев", "Денисов", "Ефремов", "Жданов", "Зуев", "Игнатов", "Кудрявцев", "Логинов",
		"Миронов", "Никитин", "Овчинников", "Пахомов", "Рожков", "Селезнёв", "Тимофеев", "Устинов",
		"Фадеев", "Хохлов", "Шестаков", "Щукин", "Белоусов", "Гришин",
	}
	namesM = []string{"Алексей", "Борис", "Виктор", "Григорий", "Дмитрий", "Захар", "Леонид", "Марк", "Николай",
		"Пётр", "Руслан", "Семён", "Тимофей", "Филипп", "Эдуард", "Юрий", "Ярослав", "Аркадий", "Вячеслав", "Ринат"}
	namesF = []string{"Анастасия", "Вероника", "Галина", "Диана", "Евгения", "Жанна", "Зоя", "Инна", "Кира",
		"Людмила", "Маргарита", "Надежда", "Оксана", "Раиса", "Светлана", "Таисия", "Ульяна", "Фаина", "Эмилия", "Юлиана"}
)

// p0 — сигналы с приоритетом P0 в таксономии (F0 в персоны не закладываем).
func p0(code string) bool { return code == "F5" || code == "F7" || code == "F9" }

// extraPersonas — n персон добора: P51, P52, … Один и тот же n всегда даёт тех же людей.
func extraPersonas(base []persona, n int) []persona {
	if n <= 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(extraSeed))
	used := map[string]bool{}
	for _, p := range base {
		used[p.FullName] = true
	}
	// Площадки: квоты на 100, масштабируются под n; остаток — GTP.
	var sites []string
	for _, s := range extraSiteOrder {
		k := extraSites[s] * n / 100
		for i := 0; i < k; i++ {
			sites = append(sites, s)
		}
	}
	for len(sites) < n {
		sites = append(sites, domain.SiteGTP)
	}
	bySite := map[string][]int{}
	for i, s := range sites {
		bySite[s] = append(bySite[s], i)
	}

	out := make([]persona, n)
	for i := range out {
		out[i] = newExtra(rng, used, len(base)+i+1, sites[i])
	}
	for _, s := range extraSiteOrder {
		assignFlags(rng, out, bySite[s], s, n)
	}
	for i := range out {
		planExtra(rng, &out[i])
	}
	return out
}

// newExtra — человек без флагов: имя, должность, смена, стиль.
func newExtra(rng *rand.Rand, used map[string]bool, no int, site string) persona {
	var name string
	for {
		sur := surnames[rng.Intn(len(surnames))]
		if rng.Intn(2) == 0 {
			name = sur + "а " + namesF[rng.Intn(len(namesF))]
		} else {
			name = sur + " " + namesM[rng.Intn(len(namesM))]
		}
		name += " (синтетика)"
		if !used[name] {
			used[name] = true
			break
		}
	}
	jobs := jobsOffice
	if site == domain.SiteGTP {
		jobs = jobsPlant
	}
	j := jobs[rng.Intn(len(jobs))]
	p := persona{
		ID: fmt.Sprintf("P%02d", no), FullName: name, Site: site, Department: j[0], Position: j[1],
		AddressForm: domain.AddressVy, Laconic: rng.Float64() < 0.2, Trajectory: "stable",
	}
	if rng.Float64() < 0.3 {
		p.AddressForm = domain.AddressTy
	}
	if site == domain.SiteGTP {
		sh := 1 + rng.Intn(3)
		p.Shift = &sh
	}
	return p
}

// assignFlags — флаги площадки раздаются по кругу первым ≈ 52 % её людей: у каждого 1–2 разных
// кода, частые коды (F1, F2, F6) не скапливаются у одного человека.
func assignFlags(rng *rand.Rand, out []persona, idx []int, site string, n int) {
	if len(idx) == 0 {
		return
	}
	k := int(float64(len(idx))*flaggedShare + 0.5)
	if k < 1 {
		k = 1
	}
	rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	codes := make([]string, 0, len(extraFlags[site]))
	for c := range extraFlags[site] {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool {
		a, b := extraFlags[site][codes[i]], extraFlags[site][codes[j]]
		if a != b {
			return a > b
		}
		return codes[i] < codes[j]
	})
	openP0 := extraOpenP0[site] * n / 100
	severe := extraSevere[site]
	slot := 0
	for _, c := range codes {
		cnt := extraFlags[site][c] * n / 100
		for j := 0; j < cnt; j++ {
			p := &out[idx[slot%k]]
			slot++
			f := hiddenFlag{Code: c, Severity: 2}
			if c == "F10" {
				f.Severity = 1
			}
			if c == severe && n >= 50 {
				severe = ""
				f.Severity, f.open = 3, true
			}
			if p0(c) && openP0 > 0 {
				openP0--
				f.Resolved = nil
				f.open = true
			}
			p.HiddenFlags = append(p.HiddenFlags, f)
		}
	}
}

// planExtra — день адаптации, начало и конец сигналов, траектория.
//
// Без флагов: день — по всему испытательному сроку, гуще в первые 6 недель (тренд по неделям).
// С флагами: каждый сигнал начался не раньше 24 дней назад — тема в окне heat map (30 дней);
// у severity 2 сразу есть обращение, так что тема видна и без ответа на опрос. Сигналы P0,
// кроме «открытых», решены между опросами D+14 и D+90 — тема в карте, красной зоны нет.
func planExtra(rng *rand.Rand, p *persona) {
	if len(p.HiddenFlags) == 0 {
		var d int
		switch r := rng.Float64(); {
		case r < 0.10:
			d = -6 + rng.Intn(6)
		case r < 0.14:
			d = 0
		case r < 0.32:
			d = 1 + rng.Intn(7)
		case r < 0.76:
			d = 8 + rng.Intn(28)
		case r < 0.90:
			d = 36 + rng.Intn(25)
		default:
			d = 61 + rng.Intn(29)
		}
		if rng.Float64() < 0.2 {
			p.Trajectory = "dip_recover"
		}
		p.Day = &d
		return
	}
	quietP0 := false
	for _, f := range p.HiddenFlags {
		if p0(f.Code) && !f.open {
			quietP0 = true
		}
	}
	d := 4 + rng.Intn(42) // D+4 … D+45
	if quietP0 && d < 22 {
		d = 22 + rng.Intn(20)
	}
	p.Day = &d
	open := false
	prev := -1
	for k := range p.HiddenFlags {
		f := &p.HiddenFlags[k]
		lo := max(0, d-24)
		var on, res int
		switch {
		case p0(f.Code) && !f.open:
			// Между опросами D+14 и D+90: [on, res) без дней опросов, res ≤ d — решено.
			lo = max(lo, 15)
			on = lo + rng.Intn(max(1, d-3-lo))
			res = on + 2 + rng.Intn(max(1, d-on-1))
			res = min(res, d)
			f.Resolved = strp(fmt.Sprintf("D+%d", res))
		default:
			on = lo + rng.Intn(max(1, d-lo))
			// Второй сигнал — не в тот же день, что первый: просадка шкал растянута.
			if on == prev && on+1 < d {
				on++
			}
			// Три четверти решены к сегодняшнему дню (обращение закрыто): иначе открытых
			// обращений в обзоре почти столько же, сколько новичков.
			if !f.open && rng.Float64() < 0.75 {
				res = max(on+1, min(on+3+rng.Intn(8), d-1))
				f.Resolved = strp(fmt.Sprintf("D+%d", res))
			}
		}
		prev = on
		f.Onset = fmt.Sprintf("D+%d", on)
		if f.Resolved == nil {
			open = true
		}
	}
	p.Trajectory = "dip_recover"
	if open && rng.Float64() < 0.4 {
		p.Trajectory = "rising_risk"
	}
}
