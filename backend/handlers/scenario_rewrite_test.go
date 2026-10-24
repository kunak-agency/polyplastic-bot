package handlers

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"main/repositories"
	"main/services"

	"github.com/google/uuid"
)

// POST /api/scenarios/{id}/rewrite-vy (api_contract.md, v4) на синтетическом ТЗ: модель —
// подделка (вы — словарём правила, нейтральный род — заменой пар), база — TEST_DATABASE_URL.

type rewriteFake struct {
	mu    sync.Mutex
	fail  bool
	calls int
}

func (f *rewriteFake) Complete(_ context.Context, m []services.LLMMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return "", errors.New("LLM: статус 503")
	}
	user := m[len(m)-1].Content
	text := strings.TrimSuffix(strings.TrimPrefix(user, "<text>\n"), "\n</text>")
	var out string
	if strings.Contains(m[0].Content, "Пол сотрудника неизвестен") {
		out = strings.NewReplacer("Готов/Готова", "Всё готово", "готов/готова", "всё готово",
			"справился", "смог", "подготовленный", "без спешки", "Дорогой {Имя},", "Здравствуйте, {Имя}!").Replace(text)
	} else {
		out = services.RuleRewriteVy(text)
	}
	return `{"text": ` + quoteJSON(out) + `}`, nil
}

func quoteJSON(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func TestRewriteVyAPI(t *testing.T) {
	a := newAPI(t)
	fake := &rewriteFake{}
	a.svc.SetRewriteLLM(fake)
	b, err := os.ReadFile("../services/testdata/tz_synthetic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	imp := a.upload("/api/scenarios/import", "tz.xlsx", b, "ТЗ (синтетика)", 201)
	id := imp["id"].(string)
	before := map[string]map[string]any{}
	for _, s := range imp["steps"].([]any) {
		m := s.(map[string]any)
		before[m["id"].(string)] = m
	}
	// Версия 1 — до переписывания: её снимок трогать нельзя.
	a.json("POST", "/api/scenarios/"+id+"/publish", map[string]any{"comment": "v1", "apply": "new"}, 200)

	a.json("POST", "/api/scenarios/"+id+"/rewrite-vy", map[string]any{"step_ids": []string{"x"}}, 422)
	a.json("POST", "/api/scenarios/"+uuid.NewString()+"/rewrite-vy", nil, 404)

	res := a.json("POST", "/api/scenarios/"+id+"/rewrite-vy", map[string]any{"neutralize": true}, 200)
	its := res["items"].([]any)
	if res["updated"] != float64(len(its)) || len(its) == 0 || res["failed"] != float64(0) {
		t.Fatalf("итог: updated %v, failed %v, items %d", res["updated"], res["failed"], len(its))
	}
	fields := map[string]bool{}
	var vyStep, ruleScoped string
	for _, it := range its {
		m := it.(map[string]any)
		f := m["field"].(string)
		fields[f[strings.LastIndex(f, ".")+1:]] = true
		if m["source"] != "llm" || m["before"] == m["after"] {
			t.Fatalf("пункт: %v", m)
		}
		if strings.HasSuffix(f, "text_vy") && services.NeedsVy(m["after"].(string)) {
			t.Errorf("в «вы» осталась форма на «ты»: %v", m)
		}
		sid, _ := m["step_id"].(string)
		switch {
		case sid != "" && f == "text_vy" && vyStep == "":
			vyStep = sid
			if before[sid]["text_vy"] != m["before"] {
				t.Errorf("before не из черновика: %v", m)
			}
		case sid == "":
			ruleScoped = f
		}
	}
	// Вопрос опроса на «ты» получает свой text_vy (e2e 24.10: «Как тебя встретили…» уходил на «вы»).
	question := false
	for _, it := range its {
		m := it.(map[string]any)
		if f := m["field"].(string); strings.HasPrefix(f, "survey.questions[") && strings.HasSuffix(f, "].text_vy") && m["after"] == "Останетесь с нами?" {
			question = true
		}
	}
	if !question {
		t.Errorf("вопрос опроса не переписан на «вы»: %v", its)
	}
	if !fields["text_vy"] || !fields["text_ty"] || vyStep == "" {
		t.Fatalf("поля: %v, шаг %q", fields, vyStep)
	}
	if ruleScoped != "" && !strings.HasPrefix(ruleScoped, "commands[") && !strings.HasPrefix(ruleScoped, "rules[") {
		t.Errorf("поле сценария: %s", ruleScoped)
	}
	codes := map[string]bool{}
	for _, c := range res["cleared_warnings"].([]any) {
		codes[c.(map[string]any)["code"].(string)] = true
	}
	if !codes["address_form_copied"] {
		t.Errorf("не сняты предупреждения: %v", res["cleared_warnings"])
	}
	// Шаг черновика: новый «вы» и пометка для бейджа.
	sc := a.json("GET", "/api/scenarios/"+id, nil, 200)
	if sc["status"] != "draft" {
		t.Fatalf("статус: %v", sc["status"])
	}
	for _, s := range sc["steps"].([]any) {
		m := s.(map[string]any)
		if m["id"] != vyStep {
			continue
		}
		mark, _ := m["ai_rewrite"].(map[string]any)
		if mark == nil || mark["source"] != "llm" || mark["text_vy"] != m["text_vy"] || m["text_vy"] == before[vyStep]["text_vy"] {
			t.Fatalf("шаг после переписывания: %v / %v", m["text_vy"], mark)
		}
	}
	// Опубликованная версия — как была.
	repos, err := repositories.InitRepos(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := repos.Scenarios.Version(context.Background(), uuid.MustParse(id), 1)
	if err != nil || v == nil {
		t.Fatalf("версия 1: %v", err)
	}
	st, _ := v.Step(uuid.MustParse(vyStep))
	if st.TextVy != before[vyStep]["text_vy"] || st.AIRewrite != nil {
		t.Fatalf("снимок версии изменился: %q", st.TextVy)
	}
	// Повтор: всё уже переписано — нечего делать.
	calls := fake.calls
	again := a.json("POST", "/api/scenarios/"+id+"/rewrite-vy", map[string]any{}, 200)
	if again["updated"] != float64(0) || fake.calls != calls {
		t.Fatalf("повтор: updated %v, вызовов модели %d", again["updated"], fake.calls-calls)
	}
	// PUT шагов из кабинета сохраняет пометку (один шаг: весь импорт PUT сейчас не проходит
	// проверку опросов-выборов с одним вариантом — это не про переписывание).
	var one []any
	for _, s := range sc["steps"].([]any) {
		if s.(map[string]any)["id"] == vyStep {
			one = append(one, s)
		}
	}
	put := a.json("PUT", "/api/scenarios/"+id+"/steps", map[string]any{"steps": one}, 200)
	for _, s := range put["steps"].([]any) {
		if m := s.(map[string]any); m["id"] == vyStep && m["ai_rewrite"] == nil {
			t.Fatal("PUT потерял ai_rewrite")
		}
	}
}

// Модель недоступна — правило-фолбэк (source=rule); step_ids — только эти шаги;
// step_ids: [] + scenario_texts — только команды и правила.
func TestRewriteVyAPIFallbackAndChunks(t *testing.T) {
	a := newAPI(t)
	a.svc.SetRewriteLLM(&rewriteFake{fail: true})
	b, err := os.ReadFile("../services/testdata/tz_synthetic.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	imp := a.upload("/api/scenarios/import", "tz.xlsx", b, "ТЗ (синтетика)", 201)
	id := imp["id"].(string)
	var ids []string
	for _, s := range imp["steps"].([]any) {
		m := s.(map[string]any)
		if services.NeedsVy(m["text_ty"].(string)) && m["text_vy"] == m["text_ty"] && len(ids) < 2 {
			ids = append(ids, m["id"].(string))
		}
	}
	if len(ids) == 0 {
		t.Fatal("в синтетике нет шагов на «ты»")
	}
	res := a.json("POST", "/api/scenarios/"+id+"/rewrite-vy", map[string]any{"step_ids": ids}, 200)
	its := res["items"].([]any)
	if len(its) == 0 {
		t.Fatal("ничего не переписано")
	}
	for _, it := range its {
		m := it.(map[string]any)
		sid, _ := m["step_id"].(string)
		if m["source"] != "rule" || (sid != ids[0] && (len(ids) < 2 || sid != ids[1])) {
			t.Fatalf("пункт вне step_ids или не правило: %v", m)
		}
	}
	res = a.json("POST", "/api/scenarios/"+id+"/rewrite-vy", map[string]any{"step_ids": []string{}, "scenario_texts": true}, 200)
	for _, it := range res["items"].([]any) {
		if m := it.(map[string]any); m["step_id"] != nil {
			t.Fatalf("при step_ids: [] переписан шаг: %v", m)
		}
	}
}
