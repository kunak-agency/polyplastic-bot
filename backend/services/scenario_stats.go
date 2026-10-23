package services

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Статистика шагов и кнопок сценария и последствия публикации (api_contract.md, v4:
// «Статистика веток», «Последствия публикации»). Вердикт ресерчера §Q6–Q7.

// StatsMinAnswers — порог анонимности: долю кнопки показываем только при стольких ответах на шаг.
const StatsMinAnswers = 5

// ButtonStats — кнопка шага: сколько раз нажали, доля от ответивших (nil — меньше порога),
// сколько обращений дала её ветка.
type ButtonStats struct {
	Label   string
	Picked  int
	Share   *float64
	Tickets int
}

// StepStats — шаг: отправлено, ответили, не ответили, медиана времени ответа (nil — нет ответов).
type StepStats struct {
	StepID        uuid.UUID
	Sent          int
	Answered      int
	NoAnswer      int
	MedianAnswerS *int
	Buttons       []ButtonStats
}

// ScenarioStats — по шагам черновика (id шага стабилен между версиями).
type ScenarioStats struct {
	Site  string
	Steps []StepStats
}

// Stats — статистика сценария; site пусто — все площадки. Считаются и тестовые отправки машины
// времени: на демо вся история — они.
func (s *ScenarioService) Stats(ctx context.Context, id uuid.UUID, site string) (*ScenarioStats, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if site != "" && !slices.Contains(domain.SiteCodes, site) {
		return nil, errs.NewValidationError("Площадка: GrPP, Luzhniki или GTP").
			WithDetails(errs.FieldError{Field: "site", Code: errs.CodeOutOfRange, Message: "GrPP, Luzhniki или GTP"})
	}
	sc, err := s.repos.Scenarios.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, errs.NewNotFoundError(msgScenarioNotFound)
	}
	steps, err := s.repos.Scenarios.Steps(ctx, id)
	if err != nil {
		return nil, err
	}
	employees, err := s.repos.Employees.ByScenario(ctx, id)
	if err != nil {
		return nil, err
	}
	var events []domain.ScheduledEvent
	empIDs := map[uuid.UUID]bool{}
	for _, e := range employees {
		if site != "" && e.Site != site {
			continue
		}
		empIDs[e.ID] = true
		evs, err := s.repos.Events.ByEmployee(ctx, e.ID)
		if err != nil {
			return nil, err
		}
		events = append(events, evs...)
	}
	tickets, err := s.repos.Tickets.List(ctx, repositories.TicketFilter{})
	if err != nil {
		return nil, err
	}
	ticketsByEvent := map[uuid.UUID]int{}
	for _, t := range tickets {
		if t.EventID != nil && empIDs[t.EmployeeID] {
			ticketsByEvent[*t.EventID]++
		}
	}
	return &ScenarioStats{Site: site, Steps: BuildStepStats(steps, events, ticketsByEvent)}, nil
}

// BuildStepStats — чистый подсчёт (тестируется без базы). Событие «отправлено», если у него есть
// sent_at или статус sent/answered; ответ — статус answered, подпись кнопки — answer. Время
// ответа — от sent_at до последнего изменения события (поля «ответили в» у события нет).
func BuildStepStats(steps []domain.ScenarioStep, events []domain.ScheduledEvent, ticketsByEvent map[uuid.UUID]int) []StepStats {
	byStep := map[uuid.UUID][]domain.ScheduledEvent{}
	for _, ev := range events {
		byStep[ev.StepID] = append(byStep[ev.StepID], ev)
	}
	out := make([]StepStats, 0, len(steps))
	for _, st := range steps {
		ss := StepStats{StepID: st.ID}
		picked := map[string]int{}
		tix := map[string]int{}
		var waits []int
		for _, ev := range byStep[st.ID] {
			sent := ev.SentAt != nil || ev.Status == domain.EventSent || ev.Status == domain.EventAnswered
			if !sent {
				continue
			}
			ss.Sent++
			if ev.Status != domain.EventAnswered {
				continue
			}
			ss.Answered++
			if ev.Answer != nil {
				picked[*ev.Answer]++
				tix[*ev.Answer] += ticketsByEvent[ev.ID]
			}
			if ev.SentAt != nil && ev.UpdatedAt.After(*ev.SentAt) {
				waits = append(waits, int(ev.UpdatedAt.Sub(*ev.SentAt)/time.Second))
			}
		}
		ss.NoAnswer = ss.Sent - ss.Answered
		if len(waits) > 0 {
			sort.Ints(waits)
			m := waits[len(waits)/2]
			if len(waits)%2 == 0 {
				m = (waits[len(waits)/2-1] + waits[len(waits)/2]) / 2
			}
			ss.MedianAnswerS = &m
		}
		ss.Buttons = make([]ButtonStats, 0, len(st.Buttons))
		for _, b := range st.Buttons {
			bs := ButtonStats{Label: b.Label, Picked: picked[b.Label], Tickets: tix[b.Label]}
			if ss.Answered >= StatsMinAnswers {
				share := float64(bs.Picked) / float64(ss.Answered)
				bs.Share = &share
			}
			ss.Buttons = append(ss.Buttons, bs)
		}
		out = append(out, ss)
	}
	return out
}

// Что изменилось в шаге между опубликованной версией и черновиком.
const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
	ChangeChanged = "changed"
)

// StepChange — строка diff публикации. Fields — что поменялось (text, buttons, branches, when,
// sites, media, survey, settings); ButtonsRemoved — подписи кнопок, которых в черновике нет.
type StepChange struct {
	StepID         uuid.UUID
	Title          string
	DayOffset      int
	Time           string
	Change         string
	Fields         []string
	ButtonsRemoved []string
}

// PublishPreview — последствия публикации черновика: сколько активных новичков затронет, сколько
// из них сейчас внутри опроса, сколько веток удалено у уже отправленных (живых) сообщений — эти
// ветки доживут по снимку старой версии. Recommend — «new», если кнопки удаляются или
// переименовываются, иначе «active» (вердикт §Q6).
type PublishPreview struct {
	NextVersion         int
	ActiveNewcomers     int
	InsideSurvey        int
	RemovedBranchesLive int
	Recommend           string
	Changes             []StepChange
}

// PublishPreview — без записи в базу; считается по сохранённому черновику.
func (s *ScenarioService) PublishPreview(ctx context.Context, id uuid.UUID) (*PublishPreview, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	sc, err := s.repos.Scenarios.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, errs.NewNotFoundError(msgScenarioNotFound)
	}
	draft, err := s.repos.Scenarios.Steps(ctx, id)
	if err != nil {
		return nil, err
	}
	var published []domain.ScenarioStep
	if sc.Version > 0 {
		v, err := s.repos.Scenarios.Version(ctx, id, sc.Version)
		if err != nil {
			return nil, err
		}
		if v != nil {
			published = v.Steps
		}
	}
	p := &PublishPreview{NextVersion: sc.Version + 1, Changes: DiffSteps(published, draft)}
	p.Recommend = ApplyActive
	for _, c := range p.Changes {
		if len(c.ButtonsRemoved) > 0 {
			p.Recommend = ApplyNew
		}
	}

	employees, err := s.repos.Employees.ByScenario(ctx, id)
	if err != nil {
		return nil, err
	}
	versions := map[int]*domain.ScenarioVersion{}
	draftByID := map[uuid.UUID]domain.ScenarioStep{}
	for _, st := range draft {
		draftByID[st.ID] = st
	}
	for _, e := range employees {
		if e.Connection == domain.ConnStopped {
			continue
		}
		p.ActiveNewcomers++
		evs, err := s.repos.Events.ByEmployee(ctx, e.ID)
		if err != nil {
			return nil, err
		}
		inSurvey := false
		for _, ev := range evs {
			if ev.Status != domain.EventSent {
				continue
			}
			if ev.Kind == domain.KindSurvey {
				inSurvey = true
			}
			v, ok := versions[ev.ScenarioVersion]
			if !ok {
				v, err = s.repos.Scenarios.Version(ctx, id, ev.ScenarioVersion)
				if err != nil {
					return nil, err
				}
				versions[ev.ScenarioVersion] = v
			}
			if v == nil {
				continue
			}
			old, ok := v.Step(ev.StepID)
			if !ok {
				continue
			}
			cur, kept := draftByID[ev.StepID]
			p.RemovedBranchesLive += removedBranches(old, cur, kept)
		}
		if inSurvey {
			p.InsideSurvey++
		}
	}
	return p, nil
}

// removedBranches — ветки кнопок старого шага, которых в черновике больше нет: шаг удалён, кнопки
// с той же подписью нет или у неё убрали ветку.
func removedBranches(old, cur domain.ScenarioStep, kept bool) int {
	n := 0
	for _, b := range old.Buttons {
		if b.Branch == nil {
			continue
		}
		if !kept {
			n++
			continue
		}
		found := false
		for _, nb := range cur.Buttons {
			if nb.Label == b.Label && nb.Branch != nil {
				found = true
				break
			}
		}
		if !found {
			n++
		}
	}
	return n
}

// DiffSteps — изменения черновика относительно опубликованных шагов, в порядке черновика;
// удалённые — в конце.
func DiffSteps(published, draft []domain.ScenarioStep) []StepChange {
	old := map[uuid.UUID]domain.ScenarioStep{}
	for _, st := range published {
		old[st.ID] = st
	}
	out := []StepChange{}
	seen := map[uuid.UUID]bool{}
	for _, st := range draft {
		seen[st.ID] = true
		c := StepChange{StepID: st.ID, Title: st.Title, DayOffset: st.DayOffset, Time: st.Time}
		prev, ok := old[st.ID]
		if !ok {
			c.Change = ChangeAdded
			out = append(out, c)
			continue
		}
		c.Fields = changedFields(prev, st)
		if len(c.Fields) == 0 {
			continue
		}
		c.Change = ChangeChanged
		c.ButtonsRemoved = removedLabels(prev.Buttons, st.Buttons)
		out = append(out, c)
	}
	for _, st := range published {
		if seen[st.ID] {
			continue
		}
		out = append(out, StepChange{
			StepID: st.ID, Title: st.Title, DayOffset: st.DayOffset, Time: st.Time,
			Change: ChangeRemoved, ButtonsRemoved: removedLabels(st.Buttons, nil),
		})
	}
	return out
}

func removedLabels(prev, cur []domain.StepButton) []string {
	var out []string
	for _, b := range prev {
		if !slices.ContainsFunc(cur, func(x domain.StepButton) bool { return x.Label == b.Label }) {
			out = append(out, b.Label)
		}
	}
	return out
}

func changedFields(a, b domain.ScenarioStep) []string {
	var f []string
	add := func(name string, differ bool) {
		if differ {
			f = append(f, name)
		}
	}
	add("when", a.DayOffset != b.DayOffset || a.Time != b.Time)
	add("text", a.TextTy != b.TextTy || a.TextVy != b.TextVy || a.Title != b.Title || !eqJSON(a.AltText, b.AltText))
	add("sites", !slices.Equal(a.Sites, b.Sites))
	add("buttons", !eqJSON(buttonsNoBranch(a.Buttons), buttonsNoBranch(b.Buttons)))
	add("branches", !eqJSON(branchesOf(a.Buttons), branchesOf(b.Buttons)))
	add("media", !eqJSON(a.Media, b.Media) || a.Gallery != b.Gallery)
	add("survey", !eqJSON(a.Survey, b.Survey))
	add("settings", a.Kind != b.Kind || a.Required != b.Required || a.HRTask != b.HRTask ||
		a.WorkHoursOnly != b.WorkHoursOnly || !eqJSON(a.FlagRequired, b.FlagRequired) ||
		!eqJSON(a.NoAnswer, b.NoAnswer) || !eqJSON(a.Condition, b.Condition))
	return f
}

func buttonsNoBranch(l []domain.StepButton) []domain.StepButton {
	out := make([]domain.StepButton, len(l))
	for i, b := range l {
		b.Branch = nil
		out[i] = b
	}
	return out
}

func branchesOf(l []domain.StepButton) []*domain.Branch {
	out := make([]*domain.Branch, len(l))
	for i, b := range l {
		out[i] = b.Branch
	}
	return out
}

// eqJSON — равенство по JSON-представлению: поля шага — указатели, срезы и вложенные структуры.
func eqJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}
