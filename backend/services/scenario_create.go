package services

import (
	"context"
	"io"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Коды отказов 409 по контракту (api_contract.md, «Создание и удаление сценария»).
const (
	CodeScenarioNameTaken    = "scenario_name_taken"
	CodeScenarioNotDeletable = "scenario_not_deletable"
)

const (
	msgScenarioNameTaken   = "Сценарий с таким названием уже есть — выберите другое"
	msgSourceNotFound      = "Сценарий-источник не найден"
	msgScenarioPublishedRm = "Сценарий уже публиковался — удалить можно только черновик без публикаций"
	msgScenarioHasNewcomer = "По сценарию запущены сотрудники — удалить его нельзя"
)

// CreateDraft — POST /api/scenarios: черновик версии 0. from — источник копии: шаги его
// последней опубликованной версии (не публиковался — шаги черновика) с новыми id. Имя
// уникально без учёта регистра → иначе 409 scenario_name_taken.
func (s *ScenarioService) CreateDraft(ctx context.Context, name string, sites []string, from *uuid.UUID) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		if err := tx.Scenarios.LockNames(ctx); err != nil {
			return err
		}
		dup, err := tx.Scenarios.ByNameFold(ctx, name)
		if err != nil {
			return err
		}
		if dup != nil {
			return errs.NewConflictError(msgScenarioNameTaken).WithCode(CodeScenarioNameTaken)
		}
		var steps []domain.ScenarioStep
		if from != nil {
			steps, err = sourceSteps(ctx, tx, *from)
			if err != nil {
				return err
			}
		}
		if sites == nil {
			sites = []string{}
		}
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

// sourceSteps — шаги источника копии без id: prepareSteps выдаст новые, чтобы шаги копии не
// путались с шагами источника в таймлайнах новичков.
func sourceSteps(ctx context.Context, tx *repositories.Repos, from uuid.UUID) ([]domain.ScenarioStep, error) {
	src, err := tx.Scenarios.ByID(ctx, from)
	if err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errs.NewValidationError(msgSourceNotFound).
			WithDetails(errs.FieldError{Field: "from_scenario_id", Code: errs.CodeInvalidFormat, Message: msgSourceNotFound})
	}
	var steps []domain.ScenarioStep
	if src.Version > 0 {
		v, err := tx.Scenarios.Version(ctx, from, src.Version)
		if err != nil {
			return nil, err
		}
		if v != nil {
			steps = append(steps, v.Steps...)
		}
	}
	if steps == nil {
		if steps, err = tx.Scenarios.Steps(ctx, from); err != nil {
			return nil, err
		}
	}
	for i := range steps {
		steps[i].ID = uuid.Nil
		steps[i].ScenarioID = uuid.Nil
	}
	return steps, nil
}

// Delete — DELETE /api/scenarios/{id}: только черновик без публикаций и без новичков,
// иначе 409 scenario_not_deletable.
func (s *ScenarioService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, id)
		if err != nil {
			return err
		}
		if sc == nil {
			return errs.NewNotFoundError(msgScenarioNotFound)
		}
		versions, err := tx.Scenarios.CountVersions(ctx, id)
		if err != nil {
			return err
		}
		if sc.Version > 0 || versions > 0 {
			return errs.NewConflictError(msgScenarioPublishedRm).WithCode(CodeScenarioNotDeletable)
		}
		n, err := tx.Scenarios.CountEmployees(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return errs.NewConflictError(msgScenarioHasNewcomer).WithCode(CodeScenarioNotDeletable)
		}
		return tx.Scenarios.Delete(ctx, id)
	})
}

// ImportExcelInto — шаги из Excel заменяют черновик существующего сценария (пустой редактор:
// «Загрузить Excel»), правила и команды файла — правила и команды сценария. Площадки сценария
// не меняются.
func (s *ScenarioService) ImportExcelInto(ctx context.Context, id uuid.UUID, r io.Reader) (*ImportResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	imp, err := parseScenarioFile(r)
	if err != nil {
		return nil, err
	}
	d, err := s.PutSteps(ctx, id, imp.steps)
	if err != nil {
		return nil, err
	}
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc, err := tx.Scenarios.ByID(ctx, id)
		if err != nil || sc == nil {
			return err
		}
		sc.Rules, sc.Commands = imp.rules, imp.commands
		return tx.Scenarios.Save(ctx, sc)
	})
	if err != nil {
		return nil, err
	}
	d.Scenario.Rules, d.Scenario.Commands = imp.rules, imp.commands
	if s.onImport != nil {
		s.onImport(ctx, imp)
	}
	return &ImportResult{Detail: d, Report: imp.report}, nil
}
