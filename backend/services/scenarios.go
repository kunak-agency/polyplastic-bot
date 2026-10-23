package services

import (
	"context"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// ScenarioService — конструктор сценариев: черновик шагов, публикация версий, импорт Excel.
type ScenarioService struct {
	repos     *repositories.Repos
	scheduler *SchedulerService
	now       func() time.Time
	// onPublish — после публикации: список команд бота в MAX (branch_commands.go).
	onPublish func(ctx context.Context)
}

// ScenarioSummary — строка списка сценариев.
type ScenarioSummary struct {
	Scenario        domain.Scenario
	ActiveNewcomers int
}

// ScenarioDetail — сценарий со шагами черновика.
type ScenarioDetail struct {
	ScenarioSummary
	Steps []domain.ScenarioStep
}

// Режимы публикации (api_contract.md: apply).
const (
	ApplyNew    = "new"
	ApplyActive = "active"
)

const (
	msgNoSteps = "В сценарии нет шагов — публиковать нечего"
)

func (s *ScenarioService) ready() error {
	if !s.repos.Enabled() {
		return repositories.ErrNoDatabase
	}
	return nil
}

// List — все сценарии с числом активных новичков.
func (s *ScenarioService) List(ctx context.Context) ([]ScenarioSummary, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	list, err := s.repos.Scenarios.List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repos.Employees.CountActiveByScenario(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ScenarioSummary, len(list))
	for i, sc := range list {
		out[i] = ScenarioSummary{Scenario: sc, ActiveNewcomers: counts[sc.ID]}
	}
	return out, nil
}

// Get — сценарий со шагами черновика.
func (s *ScenarioService) Get(ctx context.Context, id uuid.UUID) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.detail(ctx, s.repos, id)
}

func (s *ScenarioService) detail(ctx context.Context, r *repositories.Repos, id uuid.UUID) (*ScenarioDetail, error) {
	sc, err := r.Scenarios.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sc == nil {
		return nil, errs.NewNotFoundError(msgScenarioNotFound)
	}
	steps, err := r.Scenarios.Steps(ctx, id)
	if err != nil {
		return nil, err
	}
	counts, err := r.Employees.CountActiveByScenario(ctx)
	if err != nil {
		return nil, err
	}
	return &ScenarioDetail{ScenarioSummary: ScenarioSummary{Scenario: *sc, ActiveNewcomers: counts[id]}, Steps: steps}, nil
}

// Create — новый сценарий-черновик со шагами (импорт, сид).
func (s *ScenarioService) Create(ctx context.Context, name string, sites []string, steps []domain.ScenarioStep) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc := domain.Scenario{Name: name, Sites: sites, Status: domain.ScenarioDraft}
		if err := tx.Scenarios.Create(ctx, &sc); err != nil {
			return err
		}
		id = sc.ID
		return tx.Scenarios.ReplaceSteps(ctx, sc.ID, prepareSteps(sc.ID, steps, nil))
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// prepareSteps — порядок и id шагов. id клиента сохраняется (контракт: новый шаг приходит с
// UUID клиента); taken — id, занятые шагами других сценариев, и повторы внутри списка: такие
// получают новый id, иначе столкнутся с первичным ключом.
func prepareSteps(scenarioID uuid.UUID, steps []domain.ScenarioStep, taken map[uuid.UUID]bool) []domain.ScenarioStep {
	out := make([]domain.ScenarioStep, len(steps))
	seen := map[uuid.UUID]bool{}
	for i, st := range steps {
		if st.ID == uuid.Nil || taken[st.ID] || seen[st.ID] {
			st.ID, _ = uuid.NewV7()
		}
		seen[st.ID] = true
		st.ScenarioID = scenarioID
		st.Position = i
		if st.Stage == "" {
			st.Stage = domain.StageForDay(st.DayOffset)
		}
		if st.Sites == nil {
			st.Sites = domain.StringList{}
		}
		if st.Buttons == nil {
			st.Buttons = domain.ButtonList{}
		}
		out[i] = st
	}
	return out
}

// PutSteps — заменить весь список шагов черновика. Сценарий становится draft до публикации.
func (s *ScenarioService) PutSteps(ctx context.Context, id uuid.UUID, steps []domain.ScenarioStep) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, id)
		if err != nil {
			return err
		}
		if sc == nil {
			return errs.NewNotFoundError(msgScenarioNotFound)
		}
		ids := make([]uuid.UUID, 0, len(steps))
		for _, st := range steps {
			if st.ID != uuid.Nil {
				ids = append(ids, st.ID)
			}
		}
		taken, err := tx.Scenarios.StepIDsElsewhere(ctx, id, ids)
		if err != nil {
			return err
		}
		if err := tx.Scenarios.ReplaceSteps(ctx, id, prepareSteps(id, steps, taken)); err != nil {
			return err
		}
		sc.Status = domain.ScenarioDraft
		return tx.Scenarios.Save(ctx, sc)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Publish — снимок черновика становится новой версией. apply=new — только новые запуски;
// apply=active — активным новичкам недоставленные события пересобираются под новую версию,
// отправленные остаются как были (01-architecture.md, «Публикация сценария»).
func (s *ScenarioService) Publish(ctx context.Context, id uuid.UUID, comment, apply string) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, id)
		if err != nil {
			return err
		}
		if sc == nil {
			return errs.NewNotFoundError(msgScenarioNotFound)
		}
		steps, err := tx.Scenarios.Steps(ctx, id)
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			return errs.NewValidationError(msgNoSteps)
		}
		v := domain.ScenarioVersion{
			ScenarioID: id, Number: sc.Version + 1, Comment: comment, Apply: apply,
			Steps: steps, PublishedAt: s.now(), Commands: sc.Commands, Rules: sc.Rules,
		}
		if err := tx.Scenarios.CreateVersion(ctx, &v); err != nil {
			return err
		}
		sc.Version, sc.Status = v.Number, domain.ScenarioPublished
		if err := tx.Scenarios.Save(ctx, sc); err != nil {
			return err
		}
		if apply != ApplyActive {
			return nil
		}
		return s.applyToActive(ctx, tx, sc.ID, &v)
	})
	if err != nil {
		return nil, err
	}
	if s.onPublish != nil {
		s.onPublish(ctx)
	}
	return s.Get(ctx, id)
}

// applyToActive — пересборка таймлайна активных новичков под версию v. Шаг, чья дата уже
// прошла до сегодняшнего дня, не догоняет новичка — он помечается пропущенным.
func (s *ScenarioService) applyToActive(ctx context.Context, tx *repositories.Repos, scenarioID uuid.UUID, v *domain.ScenarioVersion) error {
	employees, err := tx.Employees.ByScenario(ctx, scenarioID)
	if err != nil {
		return err
	}
	now := s.now()
	for i := range employees {
		e := &employees[i]
		if err := tx.Events.DeleteScheduled(ctx, e.ID); err != nil {
			return err
		}
		delivered, err := tx.Events.DeliveredStepIDs(ctx, e.ID)
		if err != nil {
			return err
		}
		events := s.scheduler.BuildEvents(e, v, delivered)
		today := StartOfDay(now, domain.SiteOf(e.Site))
		for j := range events {
			if events[j].FireAt.Before(today) {
				events[j].Status, events[j].Answer = domain.EventSkipped, strPtr(skipAddedLater)
			}
		}
		if err := tx.Events.CreateMissing(ctx, events); err != nil {
			return err
		}
		if err := tx.Employees.SetScenarioVersion(ctx, e.ID, v.Number); err != nil {
			return err
		}
	}
	return nil
}
