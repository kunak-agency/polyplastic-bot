package in

import "strings"

// DigestQuery — GET /api/digest?week=YYYY-Www&site= (api_contract.md, «v4 — дайджест»).
// Формат недели проверяет сервис (ParseISOWeek), здесь — площадка.
type DigestQuery struct {
	Week string `query:"week"`
	Site string `query:"site"`
}

func (q DigestQuery) Validate() (week, site string, err error) {
	var f fields
	site = strings.TrimSpace(q.Site)
	if site != "" {
		f.site("site", site)
	}
	return strings.TrimSpace(q.Week), site, f.err()
}

// DigestSend — POST /api/dev/digest/send {"week":"YYYY-Www"} (пусто — прошедшая неделя).
type DigestSend struct {
	Week string `json:"week"`
}

// ManagerQuery — GET /api/manager/newcomers?name= (ролей нет: фильтр по manager_name).
type ManagerQuery struct {
	Name string `query:"name"`
}

func (q ManagerQuery) Validate() (string, error) {
	var f fields
	return f.text("name", q.Name, true), f.err()
}
