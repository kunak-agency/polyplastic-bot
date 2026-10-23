package in

import "strings"

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
