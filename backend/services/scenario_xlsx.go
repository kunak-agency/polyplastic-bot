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
	"main/repositories"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// Импорт Excel-таймлайна: файл ТЗ заказчика (шапка «Месяц/день/время информирования … Текст
// сообщения …») и прежний формат кабинета («Когда / Повод / Метка / Макет / Текст»). Колонки
// ищутся по словам в шапке (tzColumns), разбор строк — scenario_xlsx_tz.go, ячейки —
// scenario_xlsx_cell.go, «когда» — scenario_xlsx_when.go.

const (
	msgImportFile    = "Файл не читается как Excel (.xlsx)"
	msgImportHeader  = "Не найдена строка заголовка с колонкой «когда» («Месяц/день/время информирования»)"
	msgImportNoText  = "Нужна колонка «текст» или «повод»"
	msgImportEmpty   = "В файле нет ни одного шага"
	headerSearchRows = 10
)

// ImportResult — сценарий после импорта и отчёт по файлу.
type ImportResult struct {
	Detail *ScenarioDetail
	Report ImportReport
}

// ImportExcel — сценарий-черновик из Excel. name пусто — имя по умолчанию.
func (s *ScenarioService) ImportExcel(ctx context.Context, name string, r io.Reader) (*ImportResult, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	imp, err := parseScenarioFile(r)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name = "Импорт из Excel"
	}
	var id uuid.UUID
	err = s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		sc := domain.Scenario{Name: name, Sites: imp.sites, Status: domain.ScenarioDraft,
			Rules: imp.rules, Commands: imp.commands}
		if sc.Sites == nil {
			sc.Sites = domain.StringList{}
		}
		if err := tx.Scenarios.Create(ctx, &sc); err != nil {
			return err
		}
		id = sc.ID
		return tx.Scenarios.ReplaceSteps(ctx, sc.ID, prepareSteps(sc.ID, imp.steps, nil))
	})
	if err != nil {
		return nil, err
	}
	if s.onImport != nil {
		s.onImport(ctx, imp)
	}
	d, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Detail: d, Report: imp.report}, nil
}

// ParseScenarioExcel — шаги и площадки из файла (без отчёта; сид и тесты).
func ParseScenarioExcel(r io.Reader) ([]domain.ScenarioStep, []string, error) {
	imp, err := parseScenarioFile(r)
	if err != nil {
		return nil, nil, err
	}
	return imp.steps, imp.sites, nil
}

// ParseScenarioImport — шаги, правила, команды и отчёт (ручная проверка на файле заказчика).
func ParseScenarioImport(r io.Reader) ([]domain.ScenarioStep, []domain.ScenarioRule, []domain.Command, ImportReport, error) {
	imp, err := parseScenarioFile(r)
	if err != nil {
		return nil, nil, nil, ImportReport{}, err
	}
	return imp.steps, imp.rules, imp.commands, imp.report, nil
}

func parseScenarioFile(r io.Reader) (*tzImport, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, errs.NewValidationError(msgImportFile).WithCause(err)
	}
	defer func() { _ = f.Close() }()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, errs.NewValidationError(msgImportEmpty)
	}
	// Первый лист — таймлайн; остальные (у заказчика «Лист1» — черновой дубль) пропускаем, кроме
	// листа «Корсик — расширения» из экспорта кабинета (scenario_xlsx_ext.go).
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, errs.NewValidationError(msgImportFile).WithCause(err)
	}
	imp, err := parseSheetRows(rows)
	if err != nil {
		return nil, err
	}
	for _, name := range sheets[1:] {
		if !isExtSheet(name) {
			continue
		}
		ext, err := f.GetRows(name)
		if err != nil {
			break
		}
		if entries := readExtRows(ext); len(entries) > 0 {
			applyExtReport(imp, applyExt(imp, rows, entries))
		}
		break
	}
	return imp, nil
}

// parseSheetRows — лист ТЗ: строка шапки (по словам), затем разбор строк.
func parseSheetRows(rows [][]string) (*tzImport, error) {
	header, cols := -1, map[string]int{}
	for i := 0; i < len(rows) && i < headerSearchRows && header < 0; i++ {
		for j, cell := range rows[i] {
			c := strings.ToLower(strings.TrimSpace(cell))
			if c == "" {
				continue
			}
			for _, col := range tzColumns {
				if _, seen := cols[col.key]; seen {
					continue
				}
				hit := false
				for _, w := range col.words {
					if strings.Contains(c, w) {
						hit = true
					}
				}
				if hit {
					cols[col.key] = j
					break
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
		return nil, errs.NewValidationError(msgImportHeader)
	}
	_, hasText := cols["text"]
	_, hasTitle := cols["title"]
	if !hasText && !hasTitle {
		return nil, errs.NewValidationError(msgImportNoText)
	}
	if !hasText {
		cols["text"] = cols["title"]
	}
	return parseTZRows(rows, header, cols)
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
	case reSurveyWord.MatchString(s):
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
