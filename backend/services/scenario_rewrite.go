package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// POST /api/scenarios/{id}/rewrite-vy (api_contract.md, v4): тексты «вы» черновика из текстов
// «ты» (address_form.go) и, по желанию, нейтральный род в текстах «ты». Только черновик:
// шаги (текст, реплики веток кнопок и вариантов опроса, outcomes), команды и правила
// сценария. Опубликованные версии — свои снимки, их не трогаем.
//
// Сервер отвечает за ~30 с (WriteTimeout), а модель тратит секунды на текст, поэтому кабинет
// зовёт эндпоинт по шагу (step_ids) и показывает прогресс; команды и правила — последним
// вызовом (scenario_texts). Сам вызов ограничен rewriteBudget: что модель не успела, правится
// правилом-фолбэком.

const (
	rewriteWorkers = 3
	rewriteBudget  = 22 * time.Second
)

// RewriteOptions — тело запроса.
type RewriteOptions struct {
	OnlyCopied    bool        // «вы» только там, где он пуст или скопирован из «ты»
	Neutralize    bool        // убрать род из текстов «ты»
	StepIDs       []uuid.UUID // nil — все шаги
	ScenarioTexts bool        // команды и правила сценария
}

// RewriteItem — одно переписанное поле.
type RewriteItem struct {
	StepID string // пусто — команда или правило сценария
	Field  string // text_vy, buttons[0].branch.messages[1].text_vy, commands[0].text_vy …
	Before string
	After  string
	Source string // llm | rule
}

// ClearedWarning — предупреждение импорта, которое снято переписыванием.
type ClearedWarning struct {
	StepID string
	Code   string
}

// RewriteReport — итог вызова.
type RewriteReport struct {
	Updated int
	Failed  int
	Items   []RewriteItem
	Cleared []ClearedWarning
	Detail  *ScenarioDetail
}

// textSlot — пара текстов «ты»/«вы» где-то в шаге или сценарии.
type textSlot struct {
	stepIdx int    // −1 — сценарий
	path    string // префикс поля: "", "buttons[0].branch.messages[1]." …
	ty, vy  *string
}

type rewriteKey struct{ mode, text string }

type rewriteOut struct {
	res RewriteResult
	err error
}

// RewriteVyScenario — переписать тексты черновика.
func (s *Services) RewriteVyScenario(ctx context.Context, id uuid.UUID, o RewriteOptions) (*RewriteReport, error) {
	sc := s.Scenarios
	if err := sc.ready(); err != nil {
		return nil, err
	}
	scen, steps, err := loadRewriteDraft(ctx, s.repos, id)
	if err != nil {
		return nil, err
	}
	// 1. Что переписывать: уникальные тексты (один текст в двадцати ветках — один вызов).
	var keys []rewriteKey
	seen := map[rewriteKey]bool{}
	add := func(k rewriteKey) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	walkRewriteSlots(scen, steps, o, func(sl textSlot) {
		if wantVy(sl, o) {
			add(rewriteKey{domain.RewriteVy, *sl.ty})
		}
		if wantNeutral(sl, o) {
			add(rewriteKey{domain.RewriteNeutral, *sl.ty})
		}
	})
	// 2. Модель — вне транзакции, несколько текстов параллельно, в пределах бюджета.
	results := s.runRewrites(ctx, keys)
	// 3. Запись — по свежему черновику: поле меняется, только если его текст тот же, что ушёл в модель.
	rep := &RewriteReport{Items: []RewriteItem{}, Cleared: []ClearedWarning{}}
	now := s.Scenarios.now().UTC().Format(time.RFC3339)
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		scen, steps, err := loadRewriteDraft(ctx, tx, id)
		if err != nil {
			return err
		}
		changedSteps := map[int]bool{}
		changedScenario := false
		clearedVy, clearedGender := map[int]bool{}, map[int]bool{}
		walkRewriteSlots(scen, steps, o, func(sl textSlot) {
			srcTy := *sl.ty
			if wantVy(sl, o) {
				if r, ok := results[rewriteKey{domain.RewriteVy, srcTy}]; ok {
					switch {
					case r.err != nil:
						rep.Failed++
					case r.res.Source != "" && r.res.Text != *sl.vy:
						rep.Items = append(rep.Items, RewriteItem{StepID: stepIDOf(steps, sl.stepIdx), Field: sl.path + "text_vy",
							Before: *sl.vy, After: r.res.Text, Source: r.res.Source})
						*sl.vy = r.res.Text
						markRewrite(steps, sl, "text_vy", r.res.Source, now)
						changedSteps[sl.stepIdx], changedScenario = true, changedScenario || sl.stepIdx < 0
						clearedVy[sl.stepIdx] = true
					}
				}
			}
			if wantNeutral(sl, o) {
				if r, ok := results[rewriteKey{domain.RewriteNeutral, srcTy}]; ok {
					switch {
					case r.err != nil || r.res.Text == srcTy:
						rep.Failed++
					default:
						rep.Items = append(rep.Items, RewriteItem{StepID: stepIDOf(steps, sl.stepIdx), Field: sl.path + "text_ty",
							Before: srcTy, After: r.res.Text, Source: r.res.Source})
						*sl.ty = r.res.Text
						markRewrite(steps, sl, "text_ty", r.res.Source, now)
						changedSteps[sl.stepIdx], changedScenario = true, changedScenario || sl.stepIdx < 0
						if sl.stepIdx >= 0 && sl.path == "" {
							clearedGender[sl.stepIdx] = true
						}
					}
				}
			}
		})
		for i := range steps {
			if !changedSteps[i] {
				continue
			}
			if err := tx.Scenarios.SaveStep(ctx, &steps[i]); err != nil {
				return err
			}
		}
		for i := range steps {
			id := steps[i].ID.String()
			if clearedVy[i] {
				rep.Cleared = append(rep.Cleared, ClearedWarning{StepID: id, Code: warnAddressForm})
			}
			if clearedGender[i] && !stepGendered(steps[i]) {
				rep.Cleared = append(rep.Cleared, ClearedWarning{StepID: id, Code: warnGendered})
			}
		}
		switch {
		case changedScenario:
			scen.Status = domain.ScenarioDraft
			return tx.Scenarios.Save(ctx, scen)
		case len(changedSteps) > 0:
			return tx.Scenarios.MarkDraft(ctx, scen.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	rep.Updated = len(rep.Items)
	d, err := sc.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	rep.Detail = d
	return rep, nil
}

func loadRewriteDraft(ctx context.Context, r *repositories.Repos, id uuid.UUID) (*domain.Scenario, []domain.ScenarioStep, error) {
	scen, err := r.Scenarios.ByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if scen == nil {
		return nil, nil, errs.NewNotFoundError(msgScenarioNotFound)
	}
	steps, err := r.Scenarios.Steps(ctx, id)
	return scen, steps, err
}

// runRewrites — тексты через модель: rewriteWorkers параллельно, не дольше rewriteBudget (после
// него вызовы модели падают по контексту и текст правит фолбэк).
func (s *Services) runRewrites(ctx context.Context, keys []rewriteKey) map[rewriteKey]rewriteOut {
	af := s.AddressForm()
	llmCtx, cancel := context.WithTimeout(ctx, rewriteBudget)
	defer cancel()
	out := make(map[rewriteKey]rewriteOut, len(keys))
	var mu sync.Mutex
	jobs := make(chan rewriteKey)
	var wg sync.WaitGroup
	for range min(rewriteWorkers, len(keys)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range jobs {
				res, err := af.Rewrite(llmCtx, k.mode, k.text)
				mu.Lock()
				out[k] = rewriteOut{res: res, err: err}
				mu.Unlock()
			}
		}()
	}
	for _, k := range keys {
		jobs <- k
	}
	close(jobs)
	wg.Wait()
	return out
}

func wantVy(sl textSlot, o RewriteOptions) bool {
	ty := strings.TrimSpace(*sl.ty)
	if ty == "" {
		return false
	}
	vy := strings.TrimSpace(*sl.vy)
	return !o.OnlyCopied || vy == "" || vy == ty
}

func wantNeutral(sl textSlot, o RewriteOptions) bool {
	return o.Neutralize && strings.TrimSpace(*sl.ty) != "" && isGendered(unquoted(*sl.ty))
}

func stepIDOf(steps []domain.ScenarioStep, i int) string {
	if i < 0 {
		return ""
	}
	return steps[i].ID.String()
}

// markRewrite — пометка шага для бейджа «ИИ — проверьте».
func markRewrite(steps []domain.ScenarioStep, sl textSlot, field, source, at string) {
	if sl.stepIdx < 0 {
		return
	}
	st := &steps[sl.stepIdx]
	m := st.AIRewrite
	if m == nil {
		m = &domain.AIRewrite{Source: source, Fields: []string{}}
		st.AIRewrite = m
	}
	if source == domain.RewriteSourceRule {
		m.Source = domain.RewriteSourceRule
	}
	if f := sl.path + field; !slices.Contains(m.Fields, f) {
		m.Fields = append(m.Fields, f)
	}
	if sl.path == "" {
		if field == "text_vy" {
			m.TextVy = st.TextVy
		} else {
			m.TextTy = st.TextTy
		}
	}
	m.At = at
}

// stepGendered — после правки в шаге не осталось родовых форм (текст, кнопки, вопросы).
func stepGendered(st domain.ScenarioStep) bool {
	all := st.TextTy
	for _, b := range st.Buttons {
		all += "\n" + b.Label
	}
	if st.Survey != nil {
		for _, q := range st.Survey.Questions {
			all += "\n" + q.Text
		}
	}
	return isGendered(unquoted(all))
}

// unquoted — текст без цитат «…»: род в названии книги («Ты способна на большее») не правим.
func unquoted(s string) string { return reRwQuoted.ReplaceAllString(s, "«»") }

// walkRewriteSlots — все пары «ты»/«вы» выбранных шагов и (scenario_texts) сценария, в
// стабильном порядке. HR-задачи бот не отправляет — их не трогаем.
func walkRewriteSlots(scen *domain.Scenario, steps []domain.ScenarioStep, o RewriteOptions, fn func(textSlot)) {
	for i := range steps {
		st := &steps[i]
		if st.HRTask || (o.StepIDs != nil && !slices.Contains(o.StepIDs, st.ID)) {
			continue
		}
		fn(textSlot{stepIdx: i, ty: &st.TextTy, vy: &st.TextVy})
		for b := range st.Buttons {
			walkBranch(i, fmt.Sprintf("buttons[%d].branch.", b), st.Buttons[b].Branch, fn)
		}
		if st.Survey == nil {
			continue
		}
		for q := range st.Survey.Questions {
			br := st.Survey.Questions[q].Branches
			keys := make([]string, 0, len(br))
			for k := range br {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			for _, k := range keys {
				walkBranch(i, fmt.Sprintf("survey.questions[%d].branches.%s.", q, k), br[k], fn)
			}
		}
		for oi := range st.Survey.Outcomes {
			walkMessages(i, fmt.Sprintf("survey.outcomes[%d].", oi), st.Survey.Outcomes[oi].Messages, fn)
		}
	}
	if !o.ScenarioTexts {
		return
	}
	for c := range scen.Commands {
		fn(textSlot{stepIdx: -1, path: fmt.Sprintf("commands[%d].", c), ty: &scen.Commands[c].TextTy, vy: &scen.Commands[c].TextVy})
	}
	for r := range scen.Rules {
		rule := &scen.Rules[r]
		fn(textSlot{stepIdx: -1, path: fmt.Sprintf("rules[%d].", r), ty: &rule.TextTy, vy: &rule.TextVy})
		for b := range rule.Buttons {
			walkBranch(-1, fmt.Sprintf("rules[%d].buttons[%d].branch.", r, b), rule.Buttons[b].Branch, fn)
		}
	}
}

func walkBranch(stepIdx int, path string, br *domain.Branch, fn func(textSlot)) {
	if br != nil {
		walkMessages(stepIdx, path, br.Messages, fn)
	}
}

func walkMessages(stepIdx int, path string, msgs []domain.BranchMessage, fn func(textSlot)) {
	for m := range msgs {
		fn(textSlot{stepIdx: stepIdx, path: fmt.Sprintf("%smessages[%d].", path, m), ty: &msgs[m].TextTy, vy: &msgs[m].TextVy})
	}
}
