package in

import (
	"strings"

	"main/pkg/errs"
)

// OverviewQuery — GET /api/overview?site= (api_contract.md, v3). Пусто — все площадки.
type OverviewQuery struct {
	Site string `query:"site"`
}

func (q OverviewQuery) Validate() (string, error) {
	var f fields
	site := strings.TrimSpace(q.Site)
	if site != "" {
		f.site("site", site)
	}
	return site, f.err()
}

// IndicatorsQuery — GET /api/indicators?site=&period= («Показатели»). period пусто — 30d.
type IndicatorsQuery struct {
	Site   string `query:"site"`
	Period string `query:"period"`
}

func (q IndicatorsQuery) Validate() (site, period string, err error) {
	var f fields
	site = strings.TrimSpace(q.Site)
	if site != "" {
		f.site("site", site)
	}
	period = strings.TrimSpace(q.Period)
	switch period {
	case "":
		period = "30d"
	case "30d", "90d", "q":
	default:
		f.add("period", errs.CodeOutOfRange, "Период: 30d, 90d или q")
	}
	return site, period, f.err()
}
