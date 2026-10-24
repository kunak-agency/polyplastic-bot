package out

import "main/services"

// Tone — доли тона текстовых ответов за неделю (api_contract.md, «Тональность»). pos/neu/neg —
// проценты (сумма 100); null, когда enough=false (n < min_group, «мало данных»).
type Tone struct {
	Pos    *int `json:"pos"`
	Neu    *int `json:"neu"`
	Neg    *int `json:"neg"`
	N      int  `json:"n"`
	Enough bool `json:"enough"`
}

// ToneTrend — тон по неделям: weeks — понедельники, series — по площадкам и «all».
type ToneTrend struct {
	Weeks    []string     `json:"weeks"`
	MinGroup int          `json:"min_group"`
	Series   []ToneSeries `json:"series"`
}

type ToneSeries struct {
	Site   string          `json:"site"`
	Points []TonePointWeek `json:"points"`
}

type TonePointWeek struct {
	Week string `json:"week"`
	Tone
}

func toneFrom(c services.ToneCell) Tone {
	return Tone{Pos: c.Pos, Neu: c.Neu, Neg: c.Neg, N: c.N, Enough: c.Enough}
}

func toneTrendFrom(t services.ToneTrend) ToneTrend {
	r := ToneTrend{Weeks: []string{}, MinGroup: services.AnonymityMinGroup, Series: []ToneSeries{}}
	for _, w := range t.Weeks {
		r.Weeks = append(r.Weeks, w.Format(overviewDate))
	}
	for _, s := range t.Series {
		ts := ToneSeries{Site: s.Site, Points: []TonePointWeek{}}
		for i, p := range s.Points {
			ts.Points = append(ts.Points, TonePointWeek{Week: r.Weeks[i], Tone: toneFrom(p)})
		}
		r.Series = append(r.Series, ts)
	}
	return r
}
