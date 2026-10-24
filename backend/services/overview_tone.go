package services

import (
	"sort"
	"time"

	"main/domain"
	"main/repositories"

	"github.com/google/uuid"
)

// Тональность текстовых ответов новичков (план итерации §3.7): только агрегаты, без
// «индекса настроения» человека. Тон размечает /flags (LLM zero-shot) или код по regex (F5/F7/F9
// → neg). Тексты здесь не читаются: строки несут только тон, площадку и время.
//
// n — число размеченных текстов (ответы опросов «своими словами» и свободные вопросы) за
// ISO-неделю. При n < AnonymityMinGroup доли не показываются: «мало данных».

// ToneCell — доли тона за неделю. Pos/Neu/Neg — проценты (сумма 100); nil, когда !Enough.
type ToneCell struct {
	Pos, Neu, Neg *int
	N             int
	Enough        bool
}

// ToneSeries — тон по неделям на площадке или «all».
type ToneSeries struct {
	Site   string
	Points []ToneCell
}

// ToneTrend — тон за несколько недель: Weeks — понедельники, Series — по площадкам и «all».
type ToneTrend struct {
	Weeks  []time.Time
	Series []ToneSeries
}

// toneCount — сколько текстов каждого тона.
type toneCount struct{ pos, neu, neg int }

func (c *toneCount) add(s *string) {
	if s == nil {
		return
	}
	switch *s {
	case domain.SentimentPos:
		c.pos++
	case domain.SentimentNeg:
		c.neg++
	case domain.SentimentNeu:
		c.neu++
	}
}

// cell — доли методом наибольшего остатка: сумма ровно 100.
func (c toneCount) cell() ToneCell {
	n := c.pos + c.neu + c.neg
	out := ToneCell{N: n, Enough: n >= AnonymityMinGroup}
	if !out.Enough {
		return out
	}
	raw := []int{c.pos, c.neu, c.neg}
	pct := make([]int, 3)
	type rem struct{ i, r int }
	rems := make([]rem, 3)
	sum := 0
	for i, v := range raw {
		pct[i] = v * 100 / n
		rems[i] = rem{i, v * 100 % n}
		sum += pct[i]
	}
	sort.SliceStable(rems, func(a, b int) bool { return rems[a].r > rems[b].r })
	for k := 0; sum < 100; k++ {
		pct[rems[k%3].i]++
		sum++
	}
	out.Pos, out.Neu, out.Neg = &pct[0], &pct[1], &pct[2]
	return out
}

// BuildTone — тон за weeks недель начиная с понедельника first (зона loc) по площадкам site
// (пусто — все площадки плюс «all»). Тестовые записи (машина времени) не считаются.
func BuildTone(rows *repositories.OverviewRows, site string, first time.Time, weeks int, loc *time.Location) ToneTrend {
	tr := ToneTrend{}
	if rows == nil {
		rows = &repositories.OverviewRows{}
	}
	for i := 0; i < weeks; i++ {
		tr.Weeks = append(tr.Weeks, first.AddDate(0, 0, 7*i))
	}
	series := append([]string{}, overviewSites(site)...)
	if site == "" {
		series = append(series, TrendAll)
	}
	data := map[string][]toneCount{}
	for _, s := range series {
		data[s] = make([]toneCount, weeks)
	}
	siteOf := people2site(rows)
	put := func(emp uuid.UUID, at time.Time, s *string, test bool) {
		if test || s == nil || at.Before(first) {
			return
		}
		st, ok := siteOf[emp]
		if !ok {
			return
		}
		w := int(weekStart(at, loc).Sub(first).Hours()/24) / 7
		if w < 0 || w >= weeks {
			return
		}
		for _, key := range []string{st, TrendAll} {
			if data[key] != nil {
				data[key][w].add(s)
			}
		}
	}
	for _, a := range rows.Answers {
		put(a.EmployeeID, a.CreatedAt, a.Sentiment, a.IsTest)
	}
	for _, q := range rows.Questions {
		put(q.EmployeeID, q.CreatedAt, q.Sentiment, q.IsTest)
	}
	for _, s := range series {
		ts := ToneSeries{Site: s}
		for _, c := range data[s] {
			ts.Points = append(ts.Points, c.cell())
		}
		tr.Series = append(tr.Series, ts)
	}
	return tr
}

// toneWeek — плитка «тон за неделю» (неделя с понедельника from): по площадке или по всем.
func toneWeek(rows *repositories.OverviewRows, site string, from time.Time, loc *time.Location) ToneCell {
	tr := BuildTone(rows, site, from, 1, loc)
	key := TrendAll
	if site != "" {
		key = site
	}
	for _, s := range tr.Series {
		if s.Site == key && len(s.Points) == 1 {
			return s.Points[0]
		}
	}
	return ToneCell{}
}

// ToneLine — строка дайджеста (числа — только из ToneCell, пишет код, не модель).
func ToneLine(c ToneCell) string {
	if !c.Enough {
		return "Тон текстовых ответов: мало данных (" + plural(c.N, "текст", "текста", "текстов") + ", нужно от 5)."
	}
	return "Тон текстовых ответов: " + itoa(*c.Neu) + " % нейтр · " + itoa(*c.Pos) + " % поз · " + itoa(*c.Neg) + " % нег (n = " + itoa(c.N) + ")."
}
