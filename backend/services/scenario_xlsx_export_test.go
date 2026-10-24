package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"main/domain"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// Круговой путь «импорт → экспорт → импорт» (api_contract.md, v4 — экспорт). Сравниваем
// нормализованный JSON шагов без id, scenario_id и position, правила, команды и площадки.
//
// Два режима:
//   - с листом «Корсик — расширения» — структура совпадает полностью;
//   - только лист ТЗ (лист расширений удалён) — так файл увидит человек или чужой импорт.
//     Расхождения допустимы только по путям из tzOnlyAllowed, у каждого — причина.

// tzOnlyAllowed — что лист ТЗ без расширений передаёт не полностью, и почему.
var tzOnlyAllowed = []struct {
	path   *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`^\.text_vy$`), "в формате ТЗ одна форма обращения, как в файле заказчика; текст на «вы» — колонка листа расширений (шаг /help из импорта: ty и vy разные)"},
	{regexp.MustCompile(`^\.buttons\[\d+\]\.branch\.messages\[\d+\]\.text_vy$`), "то же для реплики ветки: ТЗ пишет одну форму"},
	{regexp.MustCompile(`^\.buttons\[\d+\]\.branch\.messages\[\d+\]\.media$`), "заглушку «Материал: …» в ветке «Да, расскажи» (implicit_yes_no) ставит импорт, в ТЗ её нет; картинку к реплике ветки ТЗ пишет только рядом с PDF"},
}

func allowedTZOnly(path string) (string, bool) {
	for _, a := range tzOnlyAllowed {
		if a.path.MatchString(path) {
			return a.reason, true
		}
	}
	return "", false
}

// jsonPaths — пути, по которым JSON a и b различаются (без значений: тексты заказчика не
// печатаем).
func jsonPaths(p string, a, b any, out *[]string) {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok {
			*out = append(*out, p)
			return
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		for k := range y {
			if _, ok := x[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			jsonPaths(p+"."+k, x[k], y[k], out)
		}
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			*out = append(*out, p)
			return
		}
		for i := range x {
			jsonPaths(fmt.Sprintf("%s[%d]", p, i), x[i], y[i], out)
		}
	default:
		if !reflect.DeepEqual(a, b) {
			*out = append(*out, p)
		}
	}
}

func anyOf(v any) any {
	b, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(b, &x)
	return x
}

// stepDiffs — «шаг N (ключ): путь» по каждому расхождению.
func stepDiffs(want, got []domain.ScenarioStep) []string {
	if len(want) != len(got) {
		return []string{fmt.Sprintf("шагов %d, после круга %d", len(want), len(got))}
	}
	var out []string
	for i := range want {
		var paths []string
		jsonPaths("", anyOf(stepMap(want[i])), anyOf(stepMap(got[i])), &paths)
		for _, p := range paths {
			out = append(out, fmt.Sprintf("шаг %d (%s): %s", i+1, want[i].Key, p))
		}
	}
	return out
}

func dropExtSheet(t *testing.T, data []byte) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.DeleteSheet(extSheetName); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func parseBytes(t *testing.T, data []byte) *tzImport {
	t.Helper()
	imp, err := parseScenarioFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return imp
}

func withoutRow(rules []domain.ScenarioRule) []domain.ScenarioRule {
	out := make([]domain.ScenarioRule, len(rules))
	for i, r := range rules {
		r.Row = 0
		out[i] = r
	}
	return out
}

// checkSame — с листом расширений: всё равно.
func checkSame(t *testing.T, in exportInput, b *tzImport) {
	t.Helper()
	if d := stepDiffs(in.Steps, b.steps); len(d) > 0 {
		t.Errorf("с листом расширений расходятся шаги:\n%s", strings.Join(d, "\n"))
	}
	if jsonOf(domain.RuleList(in.Rules)) != jsonOf(domain.RuleList(b.rules)) {
		t.Errorf("с листом расширений расходятся правила")
	}
	if jsonOf(domain.CommandList(in.Commands)) != jsonOf(domain.CommandList(b.commands)) {
		t.Errorf("с листом расширений расходятся команды")
	}
	if jsonOf(domain.StringList(nonNilStrings(in.Sites))) != jsonOf(domain.StringList(nonNilStrings(b.sites))) {
		t.Errorf("с листом расширений расходятся площадки: %v / %v", in.Sites, b.sites)
	}
}

// roundTrip — разбор файла, экспорт, разбор; с листом расширений и без него.
func roundTrip(t *testing.T, src []byte) {
	t.Helper()
	a := parseBytes(t, src)
	in := exportInput{Name: "Круг", Label: "черновик", Sites: a.sites, Steps: a.steps, Rules: a.rules, Commands: a.commands}
	data, err := buildScenarioXLSX(in)
	if err != nil {
		t.Fatal(err)
	}
	checkSame(t, in, parseBytes(t, data))

	c := parseBytes(t, dropExtSheet(t, data))
	d := stepDiffs(a.steps, c.steps)
	if len(a.steps) != len(c.steps) {
		t.Fatalf("только лист ТЗ: %s", d[0])
	}
	reasons := map[string]int{}
	for _, x := range d {
		path := x[strings.LastIndex(x, ": ")+2:]
		r, ok := allowedTZOnly(path)
		if !ok {
			t.Errorf("только лист ТЗ: расхождение вне допустимых — %s", x)
			continue
		}
		reasons[r]++
	}
	t.Logf("только лист ТЗ: шагов %d, совпали без расхождений %d", len(a.steps), len(a.steps)-countSteps(d))
	for r, n := range reasons {
		t.Logf("  допустимо (%d): %s", n, r)
	}
	// Правила: Row — строка нового файла, остальное совпадает.
	if jsonOf(withoutRow(a.rules)) != jsonOf(withoutRow(c.rules)) {
		t.Errorf("только лист ТЗ: правила расходятся не только номером строки")
	}
	if jsonOf(a.commands) != jsonOf(c.commands) {
		t.Errorf("только лист ТЗ: команды расходятся")
	}
	if jsonOf(a.sites) != jsonOf(c.sites) {
		t.Errorf("только лист ТЗ: площадки расходятся: %v / %v", a.sites, c.sites)
	}
}

func countSteps(diffs []string) int {
	seen := map[string]bool{}
	for _, d := range diffs {
		seen[d[:strings.Index(d, ":")]] = true
	}
	return len(seen)
}

func TestExportRoundTripSynthetic(t *testing.T) {
	src, err := os.ReadFile(tzFixture)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, src)
}

// TestExportRoundTripCustomerTZ — тот же круг на файле заказчика (конфиденциален, читается по
// пути): TZ_XLSX=…/agency/playbook/artifacts/case/<файл>.xlsx. Без переменной пропускается;
// печатает только ключи шагов и пути полей.
func TestExportRoundTripCustomerTZ(t *testing.T) {
	path := os.Getenv("TZ_XLSX")
	if path == "" {
		t.Skip("TZ_XLSX не задан")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, src)
}

func ptr[T any](v T) *T { return &v }

// cabinetSteps — синтетика после правок в конструкторе: поля, которых в ТЗ нет совсем.
func cabinetSteps() []domain.ScenarioStep {
	mid := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	th := 7
	return []domain.ScenarioStep{
		{Key: "intro", DayOffset: -2, Time: "11:15", Stage: domain.StageBefore, Kind: domain.KindCard, Title: "Привет", TextTy: "Привет, {Имя}!", TextVy: "Здравствуйте, {Имя}!",
			Sites: domain.StringList{domain.SiteGTP}, WorkHoursOnly: false, Media: domain.MediaRefList{{MediaID: &mid, Name: "Лого", Caption: "подпись"}},
			Advance: ptr("auto"), NoAnswer: &domain.NoAnswerRule{RemindAfterH: []int{24, 48}, Max: 2}, Buttons: domain.ButtonList{},
			AIRewrite: &domain.AIRewrite{Source: "llm", Fields: []string{"text_vy"}, At: "2026-10-24T10:00:00Z"}},
		{Key: "ask", DayOffset: -2, Time: "11:15", Kind: domain.KindText, Title: "Вопрос", TextTy: "Всё ли понятно?", TextVy: "Всё ли понятно вам?", WorkHoursOnly: true,
			Condition: ptr(domain.ConditionAddressUnset), Buttons: domain.ButtonList{
				{Label: "Да", Branch: &domain.Branch{Then: domain.ThenContinue, Actions: []domain.Action{{Type: domain.ActionSetFlag, Flag: "ok"}},
					Messages: []domain.BranchMessage{{TextTy: "Отлично", TextVy: "Отлично!", Media: domain.MediaRefList{}, Buttons: []domain.StepButton{}},
						{TextTy: "Второе сообщение", TextVy: "Второе", Media: domain.MediaRefList{{Name: "Схема"}}, Buttons: []domain.StepButton{}}}}},
				{Label: "Нет", Branch: &domain.Branch{Then: "goto_block", Goto: "d+1", Actions: []domain.Action{{Type: domain.ActionEscalate, Category: "F1", Route: "hr", NotifyManager: true}},
					Messages: []domain.BranchMessage{{TextTy: "Расскажи", TextVy: "Расскажите", Media: domain.MediaRefList{}, Buttons: []domain.StepButton{
						escalateButton(labelEscalate, "hr"), handoffButton(labelChatHR, "hr")}}}}},
				{Label: "Ссылка", Action: "link:https://example.com"},
			}},
		{Key: "input", DayOffset: 1, Time: "", Kind: domain.KindInput, Title: "Расскажи о себе", TextTy: "Напиши пару слов", TextVy: "Напишите пару слов",
			FlagRequired: ptr("manager_ok"), HRTask: false, Required: true, Buttons: domain.ButtonList{}, Media: domain.MediaRefList{}},
		{Key: "pulse", DayOffset: 1, Time: "", Kind: domain.KindSurvey, Title: "Пульс", TextTy: "Как дела?", TextVy: "Как дела у вас?", Buttons: domain.ButtonList{}, Media: domain.MediaRefList{},
			Survey: &domain.Survey{Scale: domain.Scale5, Threshold: 3, Questions: []domain.SurveyQuestion{
				{Text: "Как настроение?", Options: []string{}},
				{Text: "Что мешает?", TextVy: "Что вам мешает?", Type: "multi", Options: []string{"Шум", "Задачи"}, OptionsVy: []string{"Шум", "Задачи"}, Other: true, Required: true,
					Branches:    map[string]*domain.Branch{"2": {Then: domain.ThenContinue, Actions: []domain.Action{}, Messages: []domain.BranchMessage{{TextTy: "Понял", TextVy: "Понял", Media: domain.MediaRefList{}, Buttons: []domain.StepButton{}}}}},
					OptionFlags: map[string]domain.OptionFlag{"2": {Code: "F1", Severity: "low"}}},
				{Text: "Оценка", Type: "scale", Scale: domain.Scale5, Threshold: &th, Anchors: []string{"плохо, очень", "хорошо"}, Comment: "below",
					Branches: map[string]*domain.Branch{"1": {Then: domain.ThenContinue, Actions: []domain.Action{{Type: domain.ActionOpenFreeText, Prompt: "Почему?"}}, Messages: []domain.BranchMessage{}}}},
			},
				Outcomes: []domain.Outcome{{If: []domain.OutcomeCond{{Q: 2, Contains: []int{1, 2}}}, Match: "any", Messages: []domain.BranchMessage{{TextTy: "Спасибо", TextVy: "Спасибо!", Media: domain.MediaRefList{}, Buttons: []domain.StepButton{}}}, Actions: []domain.Action{}}}}},
	}
}

// TestExportRoundTripCabinetFields — поля конструктора, которых в ТЗ нет: с листом расширений
// круг их не теряет.
func TestExportRoundTripCabinetFields(t *testing.T) {
	in := exportInput{Name: "Конструктор", Label: "черновик", Sites: []string{domain.SiteLuzhniki, domain.SiteGTP}, Steps: cabinetSteps(),
		Rules: []domain.ScenarioRule{{Key: "rule-x", Kind: domain.RuleFreeQuestion, Trigger: "Свободный вопрос", TextTy: "Спрошу", TextVy: "Спрошу коллег",
			Media: domain.MediaRefList{}, Buttons: []domain.StepButton{{Label: "Позвать", Action: "human"}}}},
		Commands: []domain.Command{{Name: "/docs", TextTy: "Документы", TextVy: "Документы", Actions: []domain.Action{{Type: domain.ActionSendFile, Media: "Список", PerTag: false}}}},
	}
	data, err := buildScenarioXLSX(in)
	if err != nil {
		t.Fatal(err)
	}
	checkSame(t, in, parseBytes(t, data))
}

// TestExportExtStale — HR поправил строку листа ТЗ: для этого шага побеждает лист ТЗ, импорт
// предупреждает ext_stale; остальные шаги дополняются.
func TestExportExtStale(t *testing.T) {
	in := exportInput{Name: "Конструктор", Label: "черновик", Steps: cabinetSteps()}
	data, err := buildScenarioXLSX(in)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Строка 3 — первый шаг: меняем текст.
	if err := f.SetCellStr(exportMainSheet, "F3", "Привет, {Имя}! Добро пожаловать."); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	b := parseBytes(t, buf.Bytes())
	if b.steps[0].TextTy != "Привет, {Имя}! Добро пожаловать." || b.steps[0].Advance != nil {
		t.Fatalf("правка листа ТЗ должна победить расширения: %q advance=%v", b.steps[0].TextTy, b.steps[0].Advance)
	}
	if b.steps[1].TextVy != "Всё ли понятно вам?" {
		t.Fatalf("остальные шаги дополняются расширениями: %q", b.steps[1].TextVy)
	}
	if warnCodes(b.report)[warnExtStale] != 1 || warnCodes(b.report)[warnExtApplied] != 1 {
		t.Fatalf("ext_stale и ext_applied: %v", warnCodes(b.report))
	}
}

// TestExportScenarioDB — то же через базу: ImportExcel → ExportExcel → ImportExcel, черновик и
// опубликованная версия (TEST_DATABASE_URL).
func TestExportScenarioDB(t *testing.T) {
	e := newEnv(t, time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC))
	ctx := context.Background()
	src, err := os.ReadFile(tzFixture)
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.svc.Scenarios.ImportExcel(ctx, "Синтетика", bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	id := a.Detail.Scenario.ID
	if _, err := e.svc.Scenarios.Publish(ctx, id, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	// Правка черновика после публикации: экспорт по умолчанию — черновик, version=1 — снимок.
	steps := append([]domain.ScenarioStep{}, a.Detail.Steps...)
	steps[0].TextVy = "Правка после публикации"
	if _, err := e.svc.Scenarios.PutSteps(ctx, id, steps); err != nil {
		t.Fatal(err)
	}
	draft, err := e.svc.Scenarios.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	exp, err := e.svc.Scenarios.ExportExcel(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if exp.FileName != "Синтетика — черновик.xlsx" {
		t.Fatalf("имя файла: %q", exp.FileName)
	}
	b, err := e.svc.Scenarios.ImportExcel(ctx, "Синтетика 2", bytes.NewReader(exp.Data))
	if err != nil {
		t.Fatal(err)
	}
	if d := stepDiffs(draft.Steps, b.Detail.Steps); len(d) > 0 {
		t.Errorf("черновик после круга через базу:\n%s", strings.Join(d, "\n"))
	}
	if jsonOf(draft.Scenario.Rules) != jsonOf(b.Detail.Scenario.Rules) || jsonOf(draft.Scenario.Commands) != jsonOf(b.Detail.Scenario.Commands) {
		t.Errorf("правила или команды после круга через базу расходятся")
	}
	if jsonOf(draft.Scenario.Sites) != jsonOf(b.Detail.Scenario.Sites) {
		t.Errorf("площадки: %v / %v", draft.Scenario.Sites, b.Detail.Scenario.Sites)
	}

	v1, err := e.svc.Scenarios.ExportExcel(ctx, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.FileName != "Синтетика — версия 1.xlsx" {
		t.Fatalf("имя файла версии: %q", v1.FileName)
	}
	c, err := e.svc.Scenarios.ImportExcel(ctx, "Синтетика v1", bytes.NewReader(v1.Data))
	if err != nil {
		t.Fatal(err)
	}
	if d := stepDiffs(a.Detail.Steps, c.Detail.Steps); len(d) > 0 {
		t.Errorf("версия 1 после круга через базу:\n%s", strings.Join(d, "\n"))
	}
	if _, err := e.svc.Scenarios.ExportExcel(ctx, id, 7); err == nil {
		t.Fatal("несуществующая версия — ошибка 404")
	}
}
