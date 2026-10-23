package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"main/domain"
	"main/repositories"

	"github.com/google/uuid"
)

// Дайджест (кейс, роль 04): кластеризация, порог анонимности, конфиденциальное, проверка
// чисел LLM, сообщение MAX ≤ 4000 и «каждое число текста есть во входе». Без базы.

func dgText(emp uuid.UUID, name, site, reason, text string, at time.Time, codes ...string) repositories.DigestText {
	return repositories.DigestText{EmployeeID: emp, FullName: name, Site: site, Source: "answer", Reason: reason, Text: text, Codes: codes, CreatedAt: at}
}

func newID() uuid.UUID { id, _ := uuid.NewV7(); return id }

func themeByKey(set ThemeSet, key string) *DigestTheme {
	for i := range set.Visible {
		if set.Visible[i].Key == key {
			return &set.Visible[i]
		}
	}
	return nil
}

func TestClusterThemesAnonymityAndConfidential(t *testing.T) {
	tax := ovTax(t)
	at := ovNow.Add(-24 * time.Hour)
	var rows []repositories.DigestText
	for i := 0; i < 6; i++ { // F2 у шести на GTP — тема видна
		rows = append(rows, dgText(newID(), "Иванов Пётр", domain.SiteGTP, "", "", at, "F2"))
	}
	for i := 0; i < 3; i++ { // F4 у троих — скрыта
		rows = append(rows, dgText(newID(), "Петров Иван", domain.SiteGrPP, "", "", at, "F4"))
	}
	for i := 0; i < 2; i++ { // F5 — только «конфиденциальное»
		rows = append(rows, dgText(newID(), "Сидоров", domain.SiteGTP, "", "его зовут Пётр Ильич, он орёт", at, "F5"))
	}
	rows = append(rows, dgText(newID(), "Х", domain.SiteGTP, "", "", at, "F0"))
	rows = append(rows, dgText(newID(), "Х", domain.SiteGTP, "", "вопрос", at, "question")) // не сигнал

	set := ClusterThemes(rows, tax)
	if len(set.Visible) != 1 || set.Visible[0].Key != "F2" || set.Visible[0].Newcomers != 6 {
		t.Fatalf("видимые темы: %+v", set.Visible)
	}
	if set.Visible[0].Owner == nil || *set.Visible[0].Owner != "HR" {
		t.Fatalf("ответственный F2: %v", set.Visible[0].Owner)
	}
	if set.HiddenThemes != 1 {
		t.Fatalf("скрыто тем %d, want 1 (F4)", set.HiddenThemes)
	}
	if set.Confidential != 2 {
		t.Fatalf("конфиденциальных %d, want 2", set.Confidential)
	}
	for _, th := range set.Visible {
		if th.Category == "F5" || th.Category == "F9" || th.Category == "F0" {
			t.Fatalf("тема %s не должна быть видна", th.Category)
		}
	}
	if set.Signals != 12 || set.CriticalUsers != 3 { // 6+3+2+1; критичные: F5×2 + F0
		t.Fatalf("сигналов %d, критичных %d", set.Signals, set.CriticalUsers)
	}

	// Дайджест: конфиденциальное меньше 5 — без числа.
	d := BuildDigest(nil, rows, "", weekMon(), ovNow, tax)
	if d.Confidential != nil || !d.ConfidentialAny {
		t.Fatalf("конфиденциальное: %v %v", d.Confidential, d.ConfidentialAny)
	}
	if strings.Contains(d.MaxText, "орёт") || strings.Contains(d.MaxText, "Пётр") {
		t.Fatalf("текст новичка в сообщении: %s", d.MaxText)
	}
}

func weekMon() time.Time { return weekStart(ovNow, ovMoscow) }

func TestClusterThemesTextsExamplesAndKeywords(t *testing.T) {
	tax := ovTax(t)
	at := ovNow.Add(-24 * time.Hour)
	texts := []string{
		"пропуск не работает на проходной",
		"Пропуск на проходной не пускает",
		"третий день пропуск не работает",
		"проходной пропуск снова не работает",
		"пропуск не открывает проходной турникет",
		"пропуск сломан, Сергеевич сказал ждать проходной",
	}
	var rows []repositories.DigestText
	for i, s := range texts {
		rows = append(rows, dgText(newID(), fmt.Sprintf("Новиков%d Пётр", i), domain.SiteGTP, "Другое", s, at.Add(time.Duration(i)*time.Minute)))
	}
	// Тексты при F10 и F5 не идут ни в примеры, ни в ключевые слова.
	for i := 0; i < 5; i++ {
		rows = append(rows, dgText(newID(), "Болеев", domain.SiteGrPP, "", "болею, ребёнок в больнице", at, "F10"))
	}
	set := ClusterThemes(rows, tax)
	var textTheme *DigestTheme
	for i := range set.Visible {
		if strings.HasPrefix(set.Visible[i].Key, themeTextPrefix) {
			textTheme = &set.Visible[i]
		}
	}
	if textTheme == nil || textTheme.Newcomers != 6 {
		t.Fatalf("тема из текстов не собралась: %+v", set.Visible)
	}
	if !strings.Contains(textTheme.Label, "пропуск") {
		t.Fatalf("название темы: %q", textTheme.Label)
	}
	if len(textTheme.Examples) == 0 || len(textTheme.Examples) > maxThemeExamples {
		t.Fatalf("примеры: %v", textTheme.Examples)
	}
	for _, ex := range textTheme.Examples {
		if strings.Contains(ex, "Сергеевич") || strings.Contains(ex, maskName) {
			t.Fatalf("пример с именем: %q", ex)
		}
	}
	f10 := themeByKey(set, "F10")
	if f10 == nil || len(f10.Examples) != 0 || len(f10.Keywords) != 0 {
		t.Fatalf("F10: %+v", f10)
	}

	// Меньше 5 человек — темы нет, примеров нет.
	small := ClusterThemes(rows[:4], tax)
	if len(small.Visible) != 0 || small.HiddenThemes != 1 {
		t.Fatalf("малая группа видна: %+v", small)
	}
}

func TestParseISOWeek(t *testing.T) {
	mon, err := ParseISOWeek("2026-W43")
	if err != nil || mon.Format("2006-01-02") != "2026-10-19" || mon.Weekday() != time.Monday {
		t.Fatalf("2026-W43 → %v %v", mon, err)
	}
	if ISOWeekOf(mon.AddDate(0, 0, 6)) != "2026-W43" {
		t.Fatal("ISOWeekOf воскресенья")
	}
	for _, bad := range []string{"", "2026-43", "2026-W54", "abc", "2025-W53"} {
		if _, err := ParseISOWeek(bad); err == nil {
			t.Fatalf("%q принят", bad)
		}
	}
}

func sampleAgg() DigestAggregates {
	r, w, d := 77, 74, -3
	o := "ИТ-служба GTP"
	return DigestAggregates{
		PeriodLabel: "20–24 окт",
		Totals:      DigestAggTotals{OnPulse: 31, Answered: 24, ResponseRatePct: &r, WAvg: &w, WDelta: &d, Problems: 6, Critical: 2},
		Themes: []DigestAggTheme{
			{Label: "доступы на GTP", Count: 3, Sites: []string{"GTP"}, Owner: &o},
			{Label: "задачи не объяснили", Count: 2, Sites: []string{"GrPP", "Luzhniki"}},
		},
		PreviousActions: []DigestAggAction{},
	}
}

func TestParseDigestTextGuardrails(t *testing.T) {
	agg := sampleAgg()
	b, _ := json.Marshal(agg)
	good := `{"summary": "На связи 31 новичок, ответили 24 (77 %). Средний индекс W — 74, снизился на 3 пункта. Проблем — 6, из них критичных — 2.", "themes": "Доступы на GTP — 3, GTP. Задачи не объяснили — 2, GrPP и Luzhniki.", "changes": null}`
	if _, p := ParseDigestText(good, agg, string(b)); len(p) != 0 {
		t.Fatalf("годный ответ отклонён: %v", p)
	}
	cases := map[string]string{
		"лишнее число":    `{"summary": "На связи 31 новичок, примерно 40 % проблем.", "themes": "доступы на GTP — 3. задачи не объяснили — 2.", "changes": null}`,
		"пересчёт":        `{"summary": "Ответили 24 из 31, это 7 из 10.", "themes": "доступы на GTP — 3. задачи не объяснили — 2.", "changes": null}`,
		"нет темы":        `{"summary": "На связи 31 новичок.", "themes": "доступы на GTP — 3.", "changes": null}`,
		"не JSON":         `Вот ваш отчёт: всё хорошо`,
		"длинный summary": fmt.Sprintf(`{"summary": "%s", "themes": "доступы на GTP. задачи не объяснили.", "changes": null}`, strings.Repeat("а", 301)),
	}
	for name, c := range cases {
		if _, p := ParseDigestText(c, agg, string(b)); len(p) == 0 {
			t.Errorf("%s: ответ принят", name)
		}
	}
	// Шаблон кодом всегда проходит свою же проверку.
	tmpl := TemplateDigestText(agg)
	for _, s := range []string{tmpl.Summary, *tmpl.Themes} {
		if extra := ExtraNumbers(s, string(b)); len(extra) > 0 {
			t.Fatalf("шаблон: числа %v не из входа: %s", extra, s)
		}
	}
}

type fakeLLM struct {
	answers []string
	calls   int
}

func (f *fakeLLM) Complete(context.Context, []llmMessage) (string, error) {
	f.calls++
	if f.calls > len(f.answers) {
		return "", errors.New("нет ответа")
	}
	return f.answers[f.calls-1], nil
}

func TestGenerateDigestTextRetryAndFallback(t *testing.T) {
	agg := sampleAgg()
	bad := `{"summary": "Проблем 99.", "themes": "доступы на GTP, задачи не объяснили", "changes": null}`
	good := `{"summary": "На связи 31 новичок, ответили 24.", "themes": "доступы на GTP — 3; задачи не объяснили — 2.", "changes": null}`

	llm := &fakeLLM{answers: []string{bad, good}}
	if txt := GenerateDigestText(context.Background(), llm, agg); txt.Source != DigestTextLLM || llm.calls != 2 {
		t.Fatalf("ретрай: %+v, вызовов %d", txt, llm.calls)
	}
	llm = &fakeLLM{answers: []string{bad, bad}}
	txt := GenerateDigestText(context.Background(), llm, agg)
	if txt.Source != DigestTextTemplate || len(txt.Rejected) == 0 || strings.Contains(txt.Summary, "99") {
		t.Fatalf("шаблон после двух отказов: %+v", txt)
	}
	if txt := GenerateDigestText(context.Background(), nil, agg); txt.Source != DigestTextTemplate {
		t.Fatal("без LLM — шаблон")
	}
}

// digestRows — 20 новичков на трёх площадках: опросы за неделю, ответы, сигналы.
func digestRows(t *testing.T) (*repositories.OverviewRows, []repositories.DigestText) {
	t.Helper()
	rows := &repositories.OverviewRows{}
	var texts []repositories.DigestText
	mon := weekMon()
	for i := 0; i < 20; i++ {
		site := domain.SiteCodes[i%3]
		e := ovEmp(fmt.Sprintf("Синтетиков%d Иван", i), site, 10+i)
		rows.Employees = append(rows.Employees, e)
		for wk, base := range []time.Time{mon.AddDate(0, 0, -7), mon} {
			ev := repositories.OverviewEvent{ID: newID(), EmployeeID: e.ID, Kind: domain.KindSurvey, Title: "Пульс", Status: domain.EventAnswered}
			sent := base.Add(10 * time.Hour)
			ev.SentAt = &sent
			rows.Events = append(rows.Events, ev)
			v := 4 + (i+wk)%7
			a := ovAnswer(e.ID, domain.Scale10, v, sent.Add(time.Hour))
			a.EventID = ev.ID
			rows.Answers = append(rows.Answers, a)
			if i < 7+wk*2 {
				texts = append(texts, dgText(e.ID, e.FullName, site, "", "", sent.Add(time.Hour), "F2"))
			}
		}
	}
	return rows, texts
}

func TestDigestMaxMessageNumbersFromInput(t *testing.T) {
	tax := ovTax(t)
	rows, texts := digestRows(t)
	d := BuildDigest(rows, texts, "", weekMon(), ovNow, tax)
	if d.Totals.OnPulse != 20 || d.Totals.Answered != 20 || d.Totals.WAvg == nil {
		t.Fatalf("сводка: %+v", d.Totals)
	}
	if len(d.Themes) != 1 || d.Themes[0].Newcomers != 9 || d.Themes[0].Delta == nil || *d.Themes[0].Delta != 2 {
		t.Fatalf("темы и динамика: %+v", d.Themes)
	}
	if n := utf8.RuneCountInString(d.MaxText); n == 0 || n > DigestMaxText {
		t.Fatalf("длина сообщения %d", n)
	}
	if extra := ExtraNumbers(d.MaxText, DigestFacts(d)); len(extra) > 0 {
		t.Fatalf("в сообщении числа не из входа %v:\n%s", extra, d.MaxText)
	}
	for _, name := range []string{"Синтетиков", "Иван"} {
		if strings.Contains(d.MaxText, name) {
			t.Fatalf("имя в сообщении MAX: %s", d.MaxText)
		}
	}
	// Агрегаты LLM: только числа, названия тем и площадки — без имён.
	b, _ := json.Marshal(d.Aggregates)
	if strings.Contains(string(b), "Синтетиков") {
		t.Fatalf("имя во входе LLM: %s", b)
	}

	// Потолок 4000: много тем — сообщение режется.
	big := *d
	for i := 0; i < 300; i++ {
		big.Themes = append(big.Themes, DigestThemeDelta{DigestTheme: DigestTheme{Label: strings.Repeat("тема ", 5), Newcomers: 5}})
	}
	if n := utf8.RuneCountInString(DigestMaxMessage(&big)); n > DigestMaxText {
		t.Fatalf("сообщение %d > %d", n, DigestMaxText)
	}
}

func TestDigestAnonymityTotals(t *testing.T) {
	tax := ovTax(t)
	rows, texts := digestRows(t)
	// Площадка GTP: 7 новичков — W есть; одна — 3 ответивших: W скрыт.
	rows.Employees = rows.Employees[:3]
	d := BuildDigest(rows, texts, "", weekMon(), ovNow, tax)
	if d.Totals.WAvg != nil || d.Totals.PulseAvg != nil {
		t.Fatalf("W при 3 ответивших: %v", d.Totals.WAvg)
	}
}

func TestDigestDue(t *testing.T) {
	mon := time.Date(2026, 10, 26, 9, 0, 0, 0, ovMoscow)
	if !DigestDue(mon) || DigestDue(mon.Add(-time.Minute)) || DigestDue(mon.AddDate(0, 0, 1)) {
		t.Fatal("расписание пн 9:00")
	}
}

// TestDigestPromptMatchesDoc — константы промпта совпадают с agency/prompts/digest.md.
func TestDigestPromptMatchesDoc(t *testing.T) {
	raw, err := os.ReadFile("../../agency/prompts/digest.md")
	if err != nil {
		t.Skip("digest.md не найден: ", err)
	}
	s := string(raw)
	block := func(after, lang string) string {
		i := strings.Index(s, after)
		if i < 0 {
			t.Fatalf("нет раздела %q", after)
		}
		j := strings.Index(s[i:], "```"+lang+"\n") + i + len("```"+lang+"\n")
		k := strings.Index(s[j:], "\n```") + j
		return s[j:k]
	}
	if block("## A) System prompt", "text") != digestSystemPrompt {
		t.Error("system prompt разошёлся с digest.md")
	}
	if block("## B) User message", "text") != digestUserTemplate {
		t.Error("user template разошёлся с digest.md")
	}
	if block("## C) JSON-схема", "json") != digestSchemaJSON {
		t.Error("схема разошлась с digest.md")
	}
	if block("### Пример 1", "text") != digestFewShot[0][0] || block("### Пример 2", "json") != digestFewShot[1][1] {
		t.Error("few-shot разошёлся с digest.md")
	}
}
