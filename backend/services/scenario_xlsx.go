package services

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"main/domain"
	"main/pkg/errs"

	"github.com/xuri/excelize/v2"
)

// Импорт Excel-таймлайна ТЗ. Файла кейса ещё нет, поэтому колонки предварительные
// (api_contract.md): «когда / повод / метка / макет / текст / обязательный / изображение».
// Заголовок ищется в первых строках первого листа по вхождению слова, регистр не важен.
// «обязательный» и «изображение» пока не ложатся ни в одно поле шага — читаются и
// игнорируются (TODO: сверить с файлом кейса).

// importColumns — колонка → слово в заголовке.
var importColumns = map[string]string{
	"when":  "когда",
	"title": "повод",
	"sites": "метка",
	"kind":  "макет",
	"text":  "текст",
}

const (
	msgImportFile    = "Файл не читается как Excel (.xlsx)"
	msgImportHeader  = "Не найдена строка заголовка с колонкой «когда»"
	msgImportNoText  = "Нужна колонка «текст» или «повод»"
	msgImportRows    = "Некоторые строки не разобраны"
	msgImportEmpty   = "В файле нет ни одного шага"
	headerSearchRows = 10
	maxImportErrors  = 20
)

// ImportExcel — сценарий-черновик из Excel. name пусто — имя по файлу.
func (s *ScenarioService) ImportExcel(ctx context.Context, name string, r io.Reader) (*ScenarioDetail, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	steps, sites, err := ParseScenarioExcel(r)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = "Импорт из Excel"
	}
	return s.Create(ctx, name, sites, steps)
}

// ParseScenarioExcel — шаги и объединение меток площадок из файла.
func ParseScenarioExcel(r io.Reader) ([]domain.ScenarioStep, []string, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, nil, errs.NewValidationError(msgImportFile).WithCause(err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, errs.NewValidationError(msgImportEmpty)
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, nil, errs.NewValidationError(msgImportFile).WithCause(err)
	}

	header, cols := -1, map[string]int{}
	for i := 0; i < len(rows) && i < headerSearchRows && header < 0; i++ {
		for j, cell := range rows[i] {
			c := strings.ToLower(strings.TrimSpace(cell))
			for key, word := range importColumns {
				if _, seen := cols[key]; !seen && strings.Contains(c, word) {
					cols[key] = j
				}
			}
		}
		if _, ok := cols["when"]; ok {
			header = i
		} else {
			cols = map[string]int{}
		}
	}
	if header < 0 {
		return nil, nil, errs.NewValidationError(msgImportHeader)
	}
	_, hasText := cols["text"]
	_, hasTitle := cols["title"]
	if !hasText && !hasTitle {
		return nil, nil, errs.NewValidationError(msgImportNoText)
	}

	cell := func(row []string, key string) string {
		j, ok := cols[key]
		if !ok || j >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[j])
	}
	var steps []domain.ScenarioStep
	var problems []errs.FieldError
	siteSet := map[string]bool{}
	for i := header + 1; i < len(rows); i++ {
		row := rows[i]
		when, title, text := cell(row, "when"), cell(row, "title"), cell(row, "text")
		if when == "" && title == "" && text == "" {
			continue
		}
		day, hhmm, ok := parseWhen(when)
		if !ok {
			if len(problems) < maxImportErrors {
				problems = append(problems, errs.FieldError{
					Field: fmt.Sprintf("row %d", i+1), Code: errs.CodeInvalidFormat,
					Message: fmt.Sprintf("строка %d: не разобрано «когда» — ожидается вида D−3, D0 10:00, D+14", i+1),
				})
			}
			continue
		}
		if title == "" {
			title = truncateRunes(text, 40)
		}
		sites := parseSites(cell(row, "sites"))
		for _, s := range sites {
			siteSet[s] = true
		}
		st := domain.ScenarioStep{
			DayOffset: day, Time: hhmm, Stage: domain.StageForDay(day), Sites: sites,
			Kind: parseKind(cell(row, "kind")), Title: title, TextTy: text, TextVy: text,
			WorkHoursOnly: true,
		}
		if st.Kind == domain.KindSurvey {
			st.Survey = DefaultImportSurvey(text, title)
		}
		steps = append(steps, st)
	}
	if len(problems) > 0 {
		return nil, nil, errs.NewValidationError(msgImportRows).WithDetails(problems...)
	}
	if len(steps) == 0 {
		return nil, nil, errs.NewValidationError(msgImportEmpty)
	}
	var sites []string
	for _, code := range domain.SiteCodes {
		if siteSet[code] {
			sites = append(sites, code)
		}
	}
	return steps, sites, nil
}

var (
	reTime    = regexp.MustCompile(`(\d{1,2})[:.](\d{2})`)
	reDay     = regexp.MustCompile(`(?i)(?:^|\s)(?:D|Д)\s*([+-]?)\s*(\d*)(?:\s|$)`)
	reBareDay = regexp.MustCompile(`^([+-]?)\s*(\d+)$`)
)

// parseWhen — «D−3», «D0 10:00», «Д+2 17:30», «-3» → день от D0 и время «HH:MM» (может быть
// пустым — тогда 10:00 по календарю).
func parseWhen(s string) (int, string, bool) {
	s = strings.NewReplacer("−", "-", "–", "-", "—", "-").Replace(strings.TrimSpace(s))
	hhmm := ""
	if m := reTime.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if h > 23 || mm > 59 {
			return 0, "", false
		}
		hhmm = fmt.Sprintf("%02d:%02d", h, mm)
		s = strings.TrimSpace(strings.Replace(s, m[0], "", 1))
	}
	var sign, num string
	if m := reDay.FindStringSubmatch(s); m != nil {
		sign, num = m[1], m[2]
		if num == "" {
			if sign != "" {
				return 0, "", false
			}
			num = "0"
		}
	} else if m := reBareDay.FindStringSubmatch(s); m != nil {
		sign, num = m[1], m[2]
	} else {
		return 0, "", false
	}
	n, err := strconv.Atoi(num)
	if err != nil || n > 365 {
		return 0, "", false
	}
	if sign == "-" {
		n = -n
	}
	return n, hhmm, true
}

// siteAliases — как площадки пишут в таблице.
var siteAliases = map[string]string{
	"gtp": domain.SiteGTP, "гтп": domain.SiteGTP,
	"grpp": domain.SiteGrPP, "грпп": domain.SiteGrPP,
	"luzhniki": domain.SiteLuzhniki, "лужники": domain.SiteLuzhniki,
}

func parseSites(s string) domain.StringList {
	out := domain.StringList{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == ' ' || r == '\n'
	}) {
		if code, ok := siteAliases[tok]; ok && !out.Has(code) {
			out = append(out, code)
		}
	}
	return out
}

// parseKind — «макет» в тип шага; неизвестное — текст.
func parseKind(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "опрос"), strings.Contains(s, "survey"):
		return domain.KindSurvey
	case strings.Contains(s, "файл"), strings.Contains(s, "pdf"), strings.Contains(s, "file"):
		return domain.KindFile
	case strings.Contains(s, "карточ"), strings.Contains(s, "открыт"), strings.Contains(s, "изображ"), strings.Contains(s, "card"):
		return domain.KindCard
	case strings.Contains(s, "задач"), strings.Contains(s, "task"):
		return domain.KindTask
	}
	return domain.KindText
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Опрос по умолчанию для строки «опрос» из Excel (хвост итерации 1–2): в файле ТЗ шкалы и
// порога нет, а шаг-опрос без survey сервер не сохраняет. Берём то же, что предлагает кабинет
// в «Настроить опрос»: шкала 1–10, порог 8, один вопрос — текст строки (пусто — повод).
const (
	importSurveyScale     = domain.Scale10
	importSurveyThreshold = 8
)

// DefaultImportSurvey — опрос по умолчанию для импортированного шага.
func DefaultImportSurvey(text, title string) *domain.Survey {
	q := strings.TrimSpace(text)
	if q == "" {
		q = strings.TrimSpace(title)
	}
	return &domain.Survey{
		Scale: importSurveyScale, Threshold: importSurveyThreshold,
		Questions: []domain.SurveyQuestion{{Text: q}},
	}
}
