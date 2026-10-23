// Package taxonomy — маршруты, приоритеты и видимость флагов F0–F10 из data/taxonomy.yaml
// (источник правды для обращений, api_contract.md, v2). Файл читается один раз при старте;
// ошибка чтения или разбора — фатал в main: без таксономии обращение некуда направить.
//
// Берутся стартовые варианты (conflict.chosen, status pending_human): они уже записаны в поля
// route и manager_visibility, отдельный разбор conflict не нужен.
package taxonomy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Маршруты обращения в контракте v2 (route).
const (
	RouteHR        = "hr"
	RouteRecruiter = "recruiter"
	RouteManager   = "manager"
	RouteSecurity  = "security"
	RouteDuty      = "duty"
)

// Routes — допустимые значения route.
var Routes = []string{RouteHR, RouteRecruiter, RouteManager, RouteSecurity, RouteDuty}

// Приоритеты.
const (
	P0 = "P0"
	P1 = "P1"
	P2 = "P2"
)

// CategoryQuestion — обращение без флага: «Спросить человека», «Передать HR/рекрутеру».
const CategoryQuestion = "question"

// Category — то, что коду нужно от категории F0–F10.
type Category struct {
	Code              string
	Name              string
	Recipients        []string // route из файла как есть: hr, hr_bp, recruiter, manager, …
	Route             string   // первый получатель в терминах контракта
	Priority          string
	ManagerVisibility string // category | signal_only | none
	StoreQuote        bool
	Critical          bool
	// NotifyManager — руководителю уходит уведомление без текста: он есть среди получателей и
	// категория ему видна на уровне category.
	NotifyManager bool
	// ManagerHidden — руководителю не видна никогда: never_to_manager или manager_visibility=none
	// (F0, F4, F5, F9, F10). notify_manager у неё всегда false, PATCH → 422.
	ManagerHidden bool
	// Restricted — текст новичка скрыт и в кабинете (quote=null, hidden=true): только F5 и F9,
	// их видят лишь HR-партнёр и СБ (поправка оркестратора 24.10, «Уточнения v2»). F0, F4, F10
	// скрыты лишь от руководителя: HR видит переписку и цитату.
	Restricted bool
	// SecurityChat — карточка уходит в чат СБ, а не в HR-чат (F5, F9).
	SecurityChat bool
	// PartnerOnly — новичку «эту переписку видит только он(а)»: получатель — HR-партнёр или СБ.
	PartnerOnly bool
}

// Taxonomy — категории по коду.
type Taxonomy struct {
	Version    int
	Path       string
	categories map[string]Category
}

// securityChatCodes — контракт v2: «карточка обращения уходит в HR-чат, а F5/F9 — в чат СБ».
var securityChatCodes = []string{"F5", "F9"}

// recipientRoute — получатель таксономии в маршрут контракта. hr_bp и skip_manager отдельных
// маршрутов в v2 не имеют: их ведёт HR (в таксономии hr_bp — «тот же HR-менеджер с правами на
// конфиденциальные обращения»).
var recipientRoute = map[string]string{
	"hr": RouteHR, "hr_bp": RouteHR, "skip_manager": RouteHR,
	"recruiter": RouteRecruiter, "manager": RouteManager, "security": RouteSecurity, "duty": RouteDuty,
}

// urgencyPriority — срочность из файла в приоритет: P0 — немедленно/критическая, P1 — высокая,
// P2 — остальное. SLA приоритетов — в контракте (P0 — 1 рабочий час, P1 — 1 рабочий день,
// P2 — 3 рабочих дня).
func urgencyPriority(u string) (string, error) {
	u = strings.ToLower(strings.TrimSpace(u))
	switch {
	case strings.Contains(u, "немедленно"), strings.Contains(u, "критическ"):
		return P0, nil
	case strings.Contains(u, "высок"):
		return P1, nil
	case strings.Contains(u, "средн"), strings.Contains(u, "обычн"), strings.Contains(u, "низк"):
		return P2, nil
	}
	return "", fmt.Errorf("urgency %q не разобрана", u)
}

type fileRoot struct {
	Version int `yaml:"version"`
	Rules   struct {
		NeverToManager []string `yaml:"never_to_manager"`
	} `yaml:"rules"`
	Categories map[string]struct {
		Name              string   `yaml:"name"`
		Route             []string `yaml:"route"`
		Urgency           string   `yaml:"urgency"`
		ManagerVisibility string   `yaml:"manager_visibility"`
		StoreQuote        *bool    `yaml:"store_quote"`
		Critical          bool     `yaml:"critical"`
	} `yaml:"categories"`
}

// Load — разобрать файл. Любая неполнота (нет маршрута, неизвестный получатель, непонятная
// срочность) — ошибка с кодом категории.
func Load(path string) (*Taxonomy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("таксономия флагов: %w", err)
	}
	var f fileRoot
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("таксономия флагов %s: не YAML: %w", path, err)
	}
	if len(f.Categories) == 0 {
		return nil, fmt.Errorf("таксономия флагов %s: нет categories", path)
	}
	t := &Taxonomy{Version: f.Version, Path: path, categories: map[string]Category{}}
	var problems []string
	for code, c := range f.Categories {
		cat := Category{Code: code, Name: c.Name, Recipients: c.Route, ManagerVisibility: c.ManagerVisibility, Critical: c.Critical, StoreQuote: true}
		if c.StoreQuote != nil {
			cat.StoreQuote = *c.StoreQuote
		}
		if len(c.Route) == 0 {
			problems = append(problems, code+": пустой route")
			continue
		}
		r, ok := recipientRoute[c.Route[0]]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: неизвестный получатель %q", code, c.Route[0]))
			continue
		}
		cat.Route = r
		p, err := urgencyPriority(c.Urgency)
		if err != nil {
			problems = append(problems, code+": "+err.Error())
			continue
		}
		cat.Priority = p
		switch c.ManagerVisibility {
		case "category", "signal_only", "none":
		default:
			problems = append(problems, fmt.Sprintf("%s: manager_visibility %q", code, c.ManagerVisibility))
			continue
		}
		never := slices.Contains(f.Rules.NeverToManager, code)
		// Закрытая категория: руководителю не видна по правилу never_to_manager или по
		// manager_visibility=none (F0 — в том числе мысли о самоубийстве, F10 — здоровье).
		cat.ManagerHidden = never || c.ManagerVisibility == "none"
		cat.NotifyManager = !cat.ManagerHidden && c.ManagerVisibility == "category" && slices.Contains(c.Route, "manager")
		cat.SecurityChat = slices.Contains(securityChatCodes, code)
		cat.Restricted = cat.SecurityChat && cat.ManagerHidden
		cat.PartnerOnly = c.Route[0] == "hr_bp" || c.Route[0] == "security"
		t.categories[code] = cat
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, fmt.Errorf("таксономия флагов %s: %s", path, strings.Join(problems, "; "))
	}
	for _, code := range securityChatCodes {
		if _, ok := t.categories[code]; !ok {
			return nil, fmt.Errorf("таксономия флагов %s: нет категории %s", path, code)
		}
	}
	return t, nil
}

// candidates — где искать файл без TAXONOMY_PATH: из корня репозитория, из backend/ (make run)
// и из пакетов backend/*, backend/*/* (go test).
var candidates = []string{"data/taxonomy.yaml", "../data/taxonomy.yaml", "../../data/taxonomy.yaml", "../../../data/taxonomy.yaml"}

// LoadDefault — path пуст — первый существующий из candidates.
func LoadDefault(path string) (*Taxonomy, error) {
	if path != "" {
		return Load(path)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return Load(abs)
		}
	}
	return nil, errors.New("таксономия флагов: файл data/taxonomy.yaml не найден (задайте TAXONOMY_PATH)")
}

// Get — категория по коду. ok=false — такого кода нет.
func (t *Taxonomy) Get(code string) (Category, bool) {
	c, ok := t.categories[code]
	return c, ok
}

// Question — обращение без флага: маршрут по выбору новичка (hr или recruiter), P1 («обычно
// отвечают в течение рабочего дня», UX 2.5).
func Question(route string) Category {
	if route != RouteRecruiter {
		route = RouteHR
	}
	return Category{Code: CategoryQuestion, Name: "Вопрос человеку", Route: route, Priority: P1, ManagerVisibility: "none", StoreQuote: true}
}

// Codes — все коды по порядку (F0, F1, …, F10).
func (t *Taxonomy) Codes() []string {
	out := make([]string, 0, len(t.categories))
	for c := range t.categories {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b string) int {
		if len(a) != len(b) {
			return len(a) - len(b)
		}
		return strings.Compare(a, b)
	})
	return out
}
