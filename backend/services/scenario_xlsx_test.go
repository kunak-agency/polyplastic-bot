package services

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"main/domain"

	"github.com/xuri/excelize/v2"
)

// Синтетический файл ТЗ (testdata/tz_synthetic.xlsx): повторяет все конструкции файла
// заказчика (шапка, форматы «когда», варианты в ячейке и отдельной строкой, ветки «Если …»,
// ветка строками по тегам, опросы с ветками, шкалы, мультивыбор, правила после опроса, вложения,
// флаги, правила сценария), но тексты — свои. Пересоздать: UPDATE_TESTDATA=1 go test ./services
// -run TestSyntheticTZFile.

const tzFixture = "testdata/tz_synthetic.xlsx"

// tzRow — A № · B когда · C повод · D тег · E макет · F текст · G обязательный · H · I картинка · J.
type tzRow [10]string

func syntheticTZRows() []tzRow {
	d3 := "За 2-3 дня до выхода на работу"
	return []tzRow{
		{"Синтетическое ТЗ бота адаптации"},
		{"№ п/п", "Месяц/день/время информирования", "Информационный повод", "Тег сотрудника", "Форма/Макет", "Текст сообщения", "Обязательный этап", "", "Сопроводительное изображение"},
		// 3: заглушка
		{"0", d3, "", "", "", "Описание бота...", "Да", "", "Картинка 1"},
		// 4: вступление на «вы»
		{"0", d3, "", "", "", "Здравствуйте! Я ваш помощник Вася.", "Да", "", "Картинка 2"},
		// 5–6: вопрос и варианты отдельной строкой
		{"1.1", d3, "Форма обращения", "", "Следующая реплика", "Как к тебе обращаться?", "Да"},
		{"1.2", d3, "Форма обращения", "", "Следующая реплика", "Варианты ответов:\n1) На ТЫ\n2) На ВЫ\n", "Да"},
		// 7: авторские альтернативы
		{"1.3", d3, "Знакомство", "", "Макет_Привет", "1.\tВариант: Я Вася, помогу освоиться!\n2.\tВариант: Я бот Вася, создан помогать новичкам!\n\nТри месяца буду рядом.", "Да", "", "Картинка 3"},
		// 8–9: варианты по тегам + колонка J
		{"1.4", d3, "Документы", "GrPP, Luzhniki", "Следующая реплика", "Возьми документы, Х, когда пришел на оформление.", "да", "", "Картинка 4", "Список:\n1. Паспорт\n2. СНИЛС"},
		{"1.4", d3, "Документы", "GTP", "Следующая реплика", "Возьми документы, Х, когда пришел на оформление.", "да", "", "Картинка 4", "Список:\n1. Паспорт\n2. СНИЛС\n3. Фото"},
		// 10–11: вопрос без вариантов по тегам
		{"", d3, "Транспорт", "GrPP", "", "У нас есть автобус. Рассказать о маршруте?", "да"},
		{"", d3, "Транспорт", "GTP", "", "До цеха ходит автобус. Интересно?", "да"},
		// 12: просто текст
		{"1.5", d3, "", "", "Следующая реплика", "До встречи, Х! 👋", "да"},
		// 13: варианты + общая ветка 1–2 + хвост
		{"2", "За день до выхода на работу", "Готовность", "", "Макет_Готов", "Привет, Х! Завтра первый день. Ты готов?\nВарианты ответов:\n1) Да, жду с нетерпением!\n2) Готов/Готова\n3) Немного волнуюсь\n\nЕсли ответили 1-2:\nОтлично!\n\nЕсли ответили 3:\nЭто нормально.\n\nЗавтра всё покажу.", "да"},
		// 14: сдвиг колонок
		{"", "За день до выхода на работу", "Готовность", "Напомнить про документы", "", "Напоминаю про документы.", "да", "", "Картинка 4"},
		// 15–16: доступы и письмо HR
		{"3", "В первый рабочий день в 10:00", "Поздравление. Отправка всех доступов к системам", "", "Открытка", "С первым днём, Х!", "да", "", "Картинка 5"},
		{"", "В первый рабочий день в 10:00", "Поздравление. Отправка всех доступов к системам", "", "", "отправить приветственное письмо\nЗдравствуйте, Имя кандидата!\n\nРады видеть Вас.\n- пункт первый\n- пункт второй", ""},
		// 17–18: варианты через дефис + Если да/нет, по тегам
		{"3.1", "В первый рабочий день в 12:00", "", "GrPP, Luzhniki", "Макет_Доступы", "Всё ли есть на месте?\n\nВарианты ответов:\n- Да, все есть\n- Нет\n\nЕсли да:\nСупер!\n\nЕсли нет:\nСпроси руководителя.", "Да"},
		{"3.1", "В первый рабочий день в 12:00", "", "GTP", "Макет_Доступы", "Тебе всё выдали? Всё ли есть у вас на месте?\n\nВарианты ответов:\n- Да, все есть\n- Нет\n\nЕсли да:\nСупер!\n\nЕсли нет:\nСпроси руководителя.", "Да"},
		// 19: 12:01 — тот же блок
		{"", "В первый рабочий день в 12:01", "", "", "Следующая реплика", "Держи открытку!\n# Приложить Открытку к сообщению", "", "", "Открытка А"},
		// 20: вопрос с веткой строками ниже и командой
		{"3.2", "В первый рабочий день В 12.30", "Вопрос: обед", "", "Макет_Обед_вопрос", "Подсказать, где обедать?\n\nВарианты ответов:\n- Да!\n- Нет, спасибо!\n\nЕсли нет:\nНаберешь /Food — подскажу.\n\nЕсли да:\nСледующая реплика", "Да"},
		{"", "В случае варианта ответа\n «Да»", "Ответ", "GrPP", "Макет_Обед_ответ", "#Приложить пдф-ку с кафе\nПриятного аппетита!", "Да", "", "Картинка 6"},
		{"", "В случае варианта ответа\n «Да»", "Ответ", "Luzhniki", "Макет_Обед_ответ", "#Приложить пдф-ку с кафе\nПриятного аппетита!", "Да", "", "Картинка 6"},
		{"", "В случае варианта ответа\n «Да»", "Ответ", "GTP", "Макет_Обед_ответ", "#Приложить пдф-ку с кафе\nПриятного аппетита!", "Да", "", "Картинка 6"},
		// 24: http-ссылка
		{"3.3", "В первый рабочий день 13.00", "Справочник", "GTP", "", "Справочник: http://intra/\n# Приложить ссылку на справочник", "Да", "", "Картинка 7"},
		// 25: «для офисных» в «когда»
		{"3.5", "В первый рабочий день В 18:00 для сотрудников офиса", "Конец дня", "", "", "День позади! Ты справился.", "Да"},
		// 26: по отмашке рекрутера + ввод от новичка
		{"", "Во второй рабочий день 10:00", "", "", "", "Составь, пожалуйста, рассказ о себе и приложи фотографию.", "", "переменная по отмашке рекрутера"},
		// 27–28: «для офисных» в поводе + продолжение с личным файлом
		{"5", "В третий рабочий день в 09.00", "План. Для офисных сотрудников", "", "Макет_План", "Привет, Х!", ""},
		{"", "", "", "", "", "Руководитель подготовил план.\n\n#Адаптационный план", "", "переменная по отмашке рекрутера", "Картинка 8"},
		// 29–33: опрос с ветками внутри
		{"", "В третий рабочий день В 17:30", "", "", "", "Пройди короткий опрос!", "Да"},
		{"5.1", "В третий рабочий день В 17:30", "Опрос третьего дня", "", "Опрос бота", "Вопрос 1: Как прошло оформление?\nА) Хорошо\nБ) Есть вопросы\nВ) Трудности с документами\n\nЕсли ответ А: Переход к Вопросу 2\n\nЕсли ответ Б: Понял! Возможность для сотрудника нажать кнопку \"связаться с рекрутером\" (=перейти в диалог к нему). Далее переход ко 2 вопросу.\n\nЕсли ответ В: Расскажи подробнее.\nОпции для сотрудника: передать проблему в отдел подбора персонала через Корсика, связаться с рекрутером (перейти в диалог к нему). Далее переход ко 2 вопросу.", "Да"},
		{"", "В третий рабочий день В 17:30", "", "", "", "Вопрос 2: Как встретила команда?\nА) Отлично\nБ) Присматриваюсь\nВ) Никак, все заняты\n\nЕсли ответ В: Жаль.\nОпции для сотрудника: передать проблему в HR через Корсика, связаться с HR (перейти в диалог к нему). Далее переход к 3 вопросу.", "Да"},
		{"", "В третий рабочий день В 17:30", "", "", "", "Вопрос 3: Задачи понятны?\nА) Да\nБ) Жду разъяснений\nВ) Ничего не понял\n\nЕсли ответ А: Здорово!", "Да"},
		{"", "В третий рабочий день В 17:30", "", "", "", "Спасибо за ответы!", "Да"},
		// 34–35: блок через 10 дней, ФОТО
		{"6", "Через 10 дней после выхода на работу", "Праздники", "GrPP, Luzhniki", "", "Привет, Х!", ""},
		{"", "Через 10 дней после выхода на работу", "", "GrPP, Luzhniki", "", "Фото с праздника", "", "", "ФОТО"},
		// 36: шкалы
		{"", "Через 2 недели после выхода на работу", "Чек", "", "", "Привет, Х! Как дела?\n\n1. Вам понятны задачи? (да/нет)\nПри отрицательном варианте спросить причину.\n2. Насколько доступен руководитель? (шкала 1 - недоступен, 10 - всегда доступен) + поле для комментария. В случае ответа ниже 8 спросить причину.\n3. Насколько быстро выдали доступы. Обязательное поле (1 - долго, 10 - сразу) + поле для комментария. В случае ответа ниже 8 спросить причину.", "Да", "", "Картинка 1"},
		// 37–38: месяц, варианты без заголовка + /HR
		{"7", "Первый день второго месяца работы в 09.00.", "Месяц", "", "", "Привет, Х! Месяц позади.", "Да", "", "Картинка 9"},
		{"", "Первый день второго месяца работы в 09.00.", "", "", "", "Как идёт план?\n\n1) Все хорошо!\n2) Немного отстаю\n3) Я отстаю от графика.\n\nЕсли 1-2: Так держать!\nЕсли 3 - Команда /HR поможет.\n\nОпции для сотрудника: передать проблему в отдел подбора персонала через Корсика, связаться с рекрутером (перейти в диалог к нему).", "Да"},
		// 39–43: порядок времени, опросы по площадкам, правила после опроса
		{"", "Через 1 месяц и  3 недели", "", "", "", "Привет!\n# Приложить Открытку к сообщению", "Да", "", "Картинка 10"},
		{"", "Через 1 месяц и  3 недели", "", "", "", "Сейчас будет опрос.", "Да"},
		{"8.1", "Через 1 месяц и  3 недели. В 09.01", "Опрос двух месяцев", "GrPP, Luzhniki", "Опрос бота", "1) Настроение?\n - Отличное\n - Безразличное\n - Устал\n\n2) Кто помогает? (несколько вариантов ответа)\n - Руководитель\n - Коллеги\n - Никто!\n\n3) Что ускорит адаптацию?\n - Удаленка\n - Четкие задачи\n - Свой вариант: написать свой", "Да"},
		{"8.1", "Через 1 месяц и  3 недели. В 09.01", "Опрос двух месяцев", "GTP", "Опрос бота", "1) Настроение?\n - Отличное\n - Безразличное\n - Устал\n\n2) Кто помогает? (несколько вариантов ответа)\n - Руководитель\n - Коллеги\n - Никто!\n\n3) Что ускорит адаптацию?\n - Четкие задачи\n - Свой вариант: написать свой", "Да"},
		{"", "", "", "", "", "ОТВЕТЫ:\n\nСпасибо!\n\n+\n\nЕсли на первый вопрос сотрудник дал 2 вариант ответа:\nЖаль.\nОпции для сотрудника: передать проблему в отдел подбора персонала через Корсика, связаться с рекрутером (перейти в диалог к нему).\n\nЛИБО\n\nЕсли на первый вопрос сотрудник дал 3 вариант ответа:\nОтдохни.\n\n+\n\nЕсли на второй вопрос был дан 3-ий ответ:\nМы рядом.", "Да"},
		// 44–45: офис в «когда» и тот же день для всех
		{"", "Через 2 месяца и 2 недели. Для офисных сотрудников", "", "", "", "Подборка книг для тебя.", "Да"},
		{"", "Через 2 месяца и 2 недели", "", "", "", "# Приложить Открытку к сообщению", "Да", "", "Открытка Б"},
		// 46–51: итоговый опрос
		{"9", "Последний день испытат. срока в 09.00", "Итоги", "", "", "Дорогой Х, поделишься впечатлениями?", "Да", "", "Картинка 11"},
		{"9.1", "В 09.01", "Итоговый опрос", "", "Опрос бота", "Вопрос 1: Что было сложнее всего?\nА) Ничего\nБ) Общение\nВ) Нечеткие задачи\nГ) Другое: свой ответ\n\nСразу после ответа переход к следующему вопросу.", "Да"},
		{"", "", "", "", "", "Вопрос 2: Останешься с нами?\nА) Да!\nБ) Не уверен\nВ) Нет, думаю об уходе\n\nЕсли ответ А: Ура!\nДалее переход к Вопросу 3\n\nЕсли ответ Б: Почему?\nВозможность для сотрудника ответить в чате с ботом либо нажать кнопку \"Готов рассказать причину лично рекрутеру\" для перехода в диалог к рекрутеру. Далее переход к Вопросу 3", "Да"},
		{"", "Последний день ИС", "", "", "", "1. Оцените нагрузку (1 - тяжело, 10 - комфортно) + поле для комментария (В случае ответа ниже 8).\n2. Хватает информации (да/нет) +поле для комментария (В случае ответа нет).", ""},
		{"", "", "", "", "", "Вопрос 3: Что улучшить?\nОткрытый вопрос", "Да"},
		{"", "", "", "", "", "И плейлист напоследок!\n\n(наложить на слово \"плейлист\" гиперссылку на https://example.com/list )", "Да"},
		{"", "", "", "", "", "Пока, Х!\n\nВарианты ответов:\n- Пока! 💙\n- 😭\n\n+ОТКРЫТКА с пожеланием", "Да", "", "Картинка 12"},
		// 53–54: правила сценария
		{"0", "Если сотрудник задаст вопрос Корсику (просто так)", "", "", "", "Спрошу коллег.\n\nОпции для сотрудника: передать проблему в отдел подбора персонала через Корсика, связаться с рекрутером (перейти в диалог к нему)", "", "", "Картинка 13"},
		{"0", "Если Корсик запустил опрос, а сотрудник НЕ прошел его в течение 1 дня (напомнить на завтра + на послезавтра)", "", "", "", "Привет, Х! Напоминаю про опрос.", "", "", "Картинка 14"},
	}
}

func buildSyntheticTZ(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	if err := f.SetSheetName("Sheet1", "27.02.2026"); err != nil {
		t.Fatal(err)
	}
	for i, r := range syntheticTZRows() {
		row := make([]any, len(r))
		for j, c := range r {
			row[j] = c
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("27.02.2026", cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	// «Лист1» — черновой дубль, импорт его пропускает.
	if _, err := f.NewSheet("Лист1"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Лист1", "A1", &[]any{"8", "Первый день третьего месяца в 09.00", "Дубль", "Макет", "Привет"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSyntheticTZFile(t *testing.T) {
	if os.Getenv("UPDATE_TESTDATA") != "" {
		if err := os.WriteFile(tzFixture, buildSyntheticTZ(t), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(tzFixture); err != nil {
		t.Fatalf("нет %s: UPDATE_TESTDATA=1 go test ./services -run TestSyntheticTZFile", tzFixture)
	}
}

func parseFixture(t *testing.T) ([]domain.ScenarioStep, []domain.ScenarioRule, []domain.Command, ImportReport) {
	t.Helper()
	b, err := os.ReadFile(tzFixture)
	if err != nil {
		t.Fatalf("нет %s: %v", tzFixture, err)
	}
	steps, rules, cmds, rep, err := ParseScenarioImport(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return steps, rules, cmds, rep
}

func stepByText(t *testing.T, steps []domain.ScenarioStep, sub string) *domain.ScenarioStep {
	t.Helper()
	for i := range steps {
		if strings.Contains(steps[i].TextTy, sub) {
			return &steps[i]
		}
	}
	t.Fatalf("нет шага с текстом %q", sub)
	return nil
}

func surveyByQuestion(t *testing.T, steps []domain.ScenarioStep, sub string) *domain.ScenarioStep {
	t.Helper()
	for i := range steps {
		if sv := steps[i].Survey; sv != nil {
			for _, q := range sv.Questions {
				if strings.Contains(q.Text, sub) {
					return &steps[i]
				}
			}
		}
	}
	t.Fatalf("нет опроса с вопросом %q", sub)
	return nil
}

func warnCodes(rep ImportReport) map[string]int {
	m := map[string]int{}
	for _, w := range rep.Warnings {
		m[w.Code]++
	}
	return m
}

func TestImportTZReport(t *testing.T) {
	steps, rules, cmds, rep := parseFixture(t)
	if rep.Blocks != 16 {
		t.Errorf("блоков %d, ждём 16", rep.Blocks)
	}
	if rep.Messages != len(steps) || rep.Rules != 2 || len(rules) != 2 || rep.Commands != 2 || len(cmds) != 2 {
		t.Errorf("сообщений %d/%d, правил %d, команд %d", rep.Messages, len(steps), rep.Rules, rep.Commands)
	}
	if rep.Surveys != 5 || rep.ImplicitYesNo != 2 || rep.HRTasks != 1 || rep.Outcomes != 4 {
		t.Errorf("опросов %d, да/нет %d, HR %d, outcomes %d", rep.Surveys, rep.ImplicitYesNo, rep.HRTasks, rep.Outcomes)
	}
	// Медиа: 14 картинок + 2 открытки + ФОТО + 2 заглушки «Материал: …»; личный план не в медиатеке.
	if rep.MediaNames != 19 || len(rep.MediaNameList) != 19 {
		t.Errorf("медиа-имён %d: %v", rep.MediaNames, rep.MediaNameList)
	}
	codes := warnCodes(rep)
	for code, n := range map[string]int{
		warnNoText: 1, warnOptionsMerged: 1, warnAltText: 1, warnColsShifted: 1, warnCredentials: 1,
		warnHRTask: 1, warnNonHTTPS: 1, warnRecruiter: 2, warnPhotoUnnamed: 1, warnTimeOrder: 1,
		warnOutcomes: 1, warnImplicitYesNo: 2, warnNoContent: 2, "link_in_text": 1, warnAddressForm: 1,
		warnWhenApprox: 3,
	} {
		if codes[code] != n {
			t.Errorf("предупреждений %s: %d, ждём %d", code, codes[code], n)
		}
	}
	if codes[warnGendered] == 0 || codes[warnMixedForm] == 0 || codes["label_truncated"] == 0 {
		t.Errorf("нет gendered/mixed/label_truncated: %v", codes)
	}
	for _, w := range rep.Warnings {
		if w.Row < 3 || w.Message == "" {
			t.Errorf("предупреждение без строки или текста: %+v", w)
		}
		if w.Code != warnAddressForm && (w.StepKey == "" || w.StepID == "") {
			t.Errorf("предупреждение без шага: %+v", w)
		}
	}
	keys := map[string]bool{}
	for _, s := range steps {
		if s.Key == "" || keys[s.Key] {
			t.Errorf("ключ шага пуст или повторён: %q", s.Key)
		}
		keys[s.Key] = true
		if s.Title == "" {
			t.Errorf("шаг %s без заголовка", s.Key)
		}
		if s.Advance != nil {
			t.Errorf("импорт не заполняет advance: %s", s.Key)
		}
	}
}

func TestImportTZBlocksAndOrder(t *testing.T) {
	steps, _, _, rep := parseFixture(t)
	// Первый блок: D−3, все строки до «за день» — один блок в порядке строк.
	first := steps[0]
	if first.DayOffset != -3 || first.Time != "" || first.Key != "d-3-auto-01" {
		t.Fatalf("первый шаг: %+v", first)
	}
	// Позиции блока идут подряд по строкам; 12:01 — тот же блок, что 12:00.
	card := stepByText(t, steps, "Держи открытку")
	if card.DayOffset != 0 || card.Time != "12:00" || !strings.HasSuffix(card.Key, "-03") {
		t.Errorf("12:01 после блока 12:00: %+v", card)
	}
	if strings.Contains(card.TextTy, "Приложить") || len(card.Media) != 1 || card.Media[0].Name != "Открытка А" {
		t.Errorf("вложение открытки: %q %+v", card.TextTy, card.Media)
	}
	// «В 09.01» после строк без времени — строки блока на 09:00.
	pre := stepByText(t, steps, "Сейчас будет опрос")
	if pre.DayOffset != 51 || pre.Time != "09:00" {
		t.Errorf("порядок времени: %+v", pre)
	}
	// Рабочий день как календарный, месяц = 30 дней, конец ИС = D+89.
	for sub, day := range map[string]int{"Составь": 1, "Месяц позади": 30, "поделишься": 89, "Подборка книг": 74} {
		if s := stepByText(t, steps, sub); s.DayOffset != day {
			t.Errorf("%s: день %d, ждём %d", sub, s.DayOffset, day)
		}
	}
	// Итоговый опрос: «В 09.01», шкалы и открытый вопрос — один шаг-опрос блока D+89.
	final := surveyByQuestion(t, steps, "Останешься")
	if final.DayOffset != 89 || final.Time != "09:00" || len(final.Survey.Questions) != 5 {
		t.Fatalf("итоговый опрос: %+v", final.Survey)
	}
	if rep.QuestionFirst == 0 || rep.NextButton == 0 {
		t.Errorf("question_first %d, next_button %d", rep.QuestionFirst, rep.NextButton)
	}
}

func TestImportTZButtonsAndBranches(t *testing.T) {
	steps, _, cmds, _ := parseFixture(t)
	// Варианты отдельной строкой → кнопки предыдущего сообщения, set_field address_form.
	ask := stepByText(t, steps, "Как к тебе обращаться")
	if len(ask.Buttons) != 2 || ask.Buttons[0].Branch == nil || ask.Buttons[0].Branch.Actions[0].Type != domain.ActionSetField ||
		ask.Buttons[1].Branch.Actions[0].Value != "vy" {
		t.Fatalf("ты/вы: %+v", ask.Buttons)
	}
	// Общая ветка «1–2» и хвост после веток — следующий шаг блока.
	ready := stepByText(t, steps, "Ты готов?")
	if len(ready.Buttons) != 3 || ready.Buttons[0].Branch == nil || ready.Buttons[1].Branch == nil ||
		len(ready.Buttons[0].Branch.On) != 2 || ready.Buttons[2].Branch.Messages[0].TextTy != "Это нормально." {
		t.Fatalf("ветки 1–2/3: %+v", ready.Buttons)
	}
	if strings.Contains(ready.TextTy, "Если") || strings.Contains(ready.TextTy, "Варианты") {
		t.Errorf("ветки остались в тексте: %q", ready.TextTy)
	}
	tail := stepByText(t, steps, "Завтра всё покажу")
	if tail.DayOffset != ready.DayOffset || tail.Key <= ready.Key || len(tail.Buttons) != 0 {
		t.Errorf("хвост: %+v", tail)
	}
	if !strings.Contains(ready.Buttons[0].Label, "…") || len([]rune(ready.Buttons[0].Label)) > maxImportLabel {
		t.Errorf("подпись не сокращена: %q", ready.Buttons[0].Label)
	}
	// Ветка строками по тегам → send_file per_tag и текст ветки; /food — тот же файл.
	lunch := stepByText(t, steps, "Подсказать, где обедать")
	yes := lunch.Buttons[0].Branch
	if yes == nil || len(yes.Actions) != 1 || yes.Actions[0].Type != domain.ActionSendFile || !yes.Actions[0].PerTag ||
		yes.Actions[0].Media != "Картинка 6" || yes.Messages[0].TextTy != "Приятного аппетита!" || lunch.Buttons[0].Action != "" {
		t.Fatalf("ветка «Да» строками: %+v", yes)
	}
	cmd := map[string]domain.Command{}
	for _, c := range cmds {
		cmd[c.Name] = c
	}
	if c := cmd["/food"]; len(c.Actions) != 1 || c.Actions[0].Media != "Картинка 6" {
		t.Errorf("/food: %+v", c)
	}
	if c := cmd["/hr"]; len(c.Actions) != 1 || c.Actions[0].Type != domain.ActionHandoff || c.Actions[0].To != "hr" {
		t.Errorf("/hr: %+v", c)
	}
	// Варианты без заголовка + пара эскалации после веток.
	plan := stepByText(t, steps, "Как идёт план")
	if len(plan.Buttons) != 3 || plan.Buttons[2].Branch == nil || len(plan.Buttons[2].Branch.Messages[0].Buttons) != 2 {
		t.Fatalf("план: %+v", plan.Buttons)
	}
	pair := plan.Buttons[2].Branch.Messages[0].Buttons
	if !pair[0].OpensFreeText || pair[0].Action != "escalate:recruiter" || pair[1].Action != "handoff:recruiter" {
		t.Errorf("пара эскалации: %+v", pair)
	}
	// Вопрос без вариантов → «Да, расскажи / Нет, спасибо» с заглушкой материала.
	bus := stepByText(t, steps, "Рассказать о маршруте")
	if len(bus.Buttons) != 2 || bus.Buttons[0].Label != labelImplicitYes || bus.Buttons[0].Branch == nil ||
		!strings.HasPrefix(bus.Buttons[0].Branch.Messages[0].Media[0].Name, "Материал:") {
		t.Errorf("неявный да/нет: %+v", bus.Buttons)
	}
	// Перед опросом вопрос не превращается в да/нет.
	if s := stepByText(t, steps, "поделишься"); len(s.Buttons) != 0 {
		t.Errorf("вход в опрос получил кнопки: %+v", s.Buttons)
	}
	// Прощание: дефисы и «+ОТКРЫТКА» убрана из текста.
	bye := stepByText(t, steps, "Пока, {Имя}!")
	if len(bye.Buttons) != 2 || strings.Contains(bye.TextTy, "ОТКРЫТКА") {
		t.Errorf("прощание: %q %+v", bye.TextTy, bye.Buttons)
	}
}

func TestImportTZSurveys(t *testing.T) {
	steps, _, _, _ := parseFixture(t)
	// Опрос третьего дня: три строки — один шаг, ветки по вариантам, маршрут HR у вопроса 2.
	d3 := surveyByQuestion(t, steps, "Как прошло оформление")
	sv := d3.Survey
	if d3.Kind != domain.KindSurvey || sv.Scale != domain.ScaleChoice || len(sv.Questions) != 3 || d3.TextTy != "" {
		t.Fatalf("опрос 3-го дня: %+v", sv)
	}
	q1 := sv.Questions[0]
	if q1.Text != "Как прошло оформление?" || q1.Branches["1"] != nil || q1.Branches["2"] == nil || q1.Branches["3"] == nil {
		t.Fatalf("ветки вопроса 1: %+v", q1.Branches)
	}
	if b := q1.Branches["2"].Messages[0]; b.TextTy != "Понял!" || len(b.Buttons) != 1 || b.Buttons[0].Action != "handoff:recruiter" {
		t.Errorf("ветка Б: %+v", b)
	}
	if f := q1.OptionFlags["3"]; f.Code != "F2" {
		t.Errorf("флаг «трудности с документами»: %+v", q1.OptionFlags)
	}
	q2 := sv.Questions[1]
	if b := q2.Branches["3"].Messages[0].Buttons; len(b) != 2 || b[0].Action != "escalate:hr" || b[1].Action != "handoff:hr" {
		t.Errorf("маршрут HR: %+v", b)
	}
	if q2.OptionFlags["3"].Code != "F4" || sv.Questions[2].OptionFlags["3"].Code != "F1" {
		t.Errorf("флаги вопросов 2–3: %+v %+v", q2.OptionFlags, sv.Questions[2].OptionFlags)
	}
	// Шкалы: да/нет с причиной, шкала с подписями и порогом, обязательный вопрос.
	chk := surveyByQuestion(t, steps, "Вам понятны задачи?")
	cq := chk.Survey.Questions
	if chk.Survey.Scale != domain.Scale10 || chk.Survey.Threshold != 8 || len(cq) != 3 || !strings.Contains(chk.TextTy, "Как дела?") {
		t.Fatalf("шкалы: %+v", chk.Survey)
	}
	if cq[0].Type != "yesno" || cq[0].Comment != "on_no" {
		t.Errorf("да/нет: %+v", cq[0])
	}
	if cq[1].Type != "scale" || cq[1].Threshold == nil || *cq[1].Threshold != 8 || cq[1].Comment != "below" ||
		len(cq[1].Anchors) != 2 || cq[1].Anchors[0] != "недоступен" || cq[1].OptionFlags["7"].Code != "F3" {
		t.Errorf("шкала: %+v", cq[1])
	}
	if !cq[2].Required || cq[2].OptionFlags["1"].Code != "F2" {
		t.Errorf("обязательный вопрос: %+v", cq[2])
	}
	// Опрос по площадкам: multi, «свой вариант», разные наборы, правила после опроса.
	var two []*domain.ScenarioStep
	for i := range steps {
		if steps[i].Survey != nil && steps[i].Survey.Questions[0].Text == "Настроение?" {
			two = append(two, &steps[i])
		}
	}
	if len(two) != 2 || !sitesEqual(two[0].Sites, domain.StringList{domain.SiteGrPP, domain.SiteLuzhniki}) ||
		!sitesEqual(two[1].Sites, domain.StringList{domain.SiteGTP}) {
		t.Fatalf("опрос по площадкам: %d", len(two))
	}
	q := two[0].Survey.Questions
	if q[1].Type != "multi" || q[1].Text != "Кто помогает?" || !q[2].Other || len(q[2].Options) != 2 || len(two[1].Survey.Questions[2].Options) != 1 {
		t.Errorf("multi/other: %+v", q)
	}
	if q[0].OptionFlags["2"].Code != "F6" || q[1].OptionFlags["3"].Code != "F3" {
		t.Errorf("флаги опроса: %+v %+v", q[0].OptionFlags, q[1].OptionFlags)
	}
	for _, s := range two {
		oc := s.Survey.Outcomes
		if len(oc) != 4 || len(oc[0].If) != 0 || oc[1].Group == "" || oc[1].Group != oc[2].Group || oc[3].Group != "" {
			t.Fatalf("outcomes: %+v", oc)
		}
		if oc[1].If[0].Q != 1 || oc[1].If[0].In[0] != 2 || oc[3].If[0].Q != 2 || oc[3].If[0].Contains[0] != 3 {
			t.Errorf("условия outcomes: %+v", oc)
		}
		if len(oc[1].Messages[0].Buttons) != 2 {
			t.Errorf("пара кнопок в outcome: %+v", oc[1].Messages)
		}
	}
	// Итоговый опрос: «Другое» → other, ветки с ответом в чат и кнопкой к рекрутеру, F7.
	fin := surveyByQuestion(t, steps, "Останешься")
	fq := fin.Survey.Questions
	if !fq[0].Other || len(fq[0].Options) != 3 || fq[0].OptionFlags["3"].Code != "F1" {
		t.Errorf("вопрос 1 итогового: %+v", fq[0])
	}
	b2 := fq[1].Branches["2"]
	if b2 == nil || len(b2.Actions) != 1 || b2.Actions[0].Type != domain.ActionOpenFreeText ||
		b2.Messages[0].Buttons[0].Label != labelChatRecruiter || fq[1].Branches["1"].Messages[0].TextTy != "Ура!" {
		t.Errorf("ветки вопроса 2: %+v", fq[1].Branches)
	}
	if fq[1].OptionFlags["2"].Code != "F7" || fq[1].OptionFlags["3"].Severity != "high" {
		t.Errorf("F7: %+v", fq[1].OptionFlags)
	}
	if fq[2].Type != "scale" || fq[3].Type != "yesno" || fq[4].Type != "text" || fin.Survey.Scale != domain.Scale10 {
		t.Errorf("типы итогового: %+v", fq)
	}
}

func TestImportTZRowsAndText(t *testing.T) {
	steps, rules, _, _ := parseFixture(t)
	// Плейсхолдер имени, колонка J — в текст.
	docs := stepByText(t, steps, "Возьми документы")
	if !strings.Contains(docs.TextTy, "{Имя}") || !strings.Contains(docs.TextTy, "СНИЛС") || docs.TextVy != docs.TextTy {
		t.Errorf("документы: %q", docs.TextTy)
	}
	// Сдвиг колонок: повод из D, шаг для всех.
	shifted := stepByText(t, steps, "Напоминаю про документы")
	if shifted.Title != "Напомнить про документы" || len(shifted.Sites) != 0 {
		t.Errorf("сдвиг колонок: %+v", shifted)
	}
	// Альтернатива текста.
	alt := stepByText(t, steps, "Я Вася, помогу")
	if alt.AltText == nil || !strings.Contains(*alt.AltText, "создан помогать") || !strings.Contains(alt.TextTy, "Три месяца") {
		t.Errorf("альтернатива: %+v", alt)
	}
	// Письмо HR.
	hr := stepByText(t, steps, "Рады видеть")
	if !hr.HRTask || hr.Kind != domain.KindTask || strings.Contains(hr.TextTy, "отправить") || len(hr.Buttons) != 0 {
		t.Errorf("письмо HR: %+v", hr)
	}
	// Офис в «когда» и в поводе, продолжение блока наследует условие; флаг и личный файл.
	if s := stepByText(t, steps, "День позади"); !sitesEqual(s.Sites, officeSites) {
		t.Errorf("офис в «когда»: %v", s.Sites)
	}
	plan := stepByText(t, steps, "Руководитель подготовил")
	if !sitesEqual(plan.Sites, officeSites) || plan.FlagRequired == nil || *plan.FlagRequired != flagRecruiter {
		t.Errorf("план: %+v", plan)
	}
	personal := false
	for _, m := range plan.Media {
		if m.Personal == "adaptation_plan" {
			personal = true
		}
	}
	if !personal || strings.Contains(plan.TextTy, "#") {
		t.Errorf("личный файл: %+v %q", plan.Media, plan.TextTy)
	}
	if s := stepByText(t, steps, "Составь"); s.Kind != domain.KindInput || s.FlagRequired == nil {
		t.Errorf("ввод от новичка: %+v", s)
	}
	if s := stepByText(t, steps, "Фото с праздника"); !s.Gallery || s.Media[0].Name != "ФОТО" {
		t.Errorf("ФОТО: %+v", s)
	}
	// Гиперссылка на слове.
	if s := stepByText(t, steps, "плейлист"); !strings.Contains(s.TextTy, "[плейлист](https://example.com/list)") || strings.Contains(s.TextTy, "наложить") {
		t.Errorf("гиперссылка: %q", s.TextTy)
	}
	// Правила сценария.
	if rules[0].Kind != domain.RuleFreeQuestion || len(rules[0].Buttons) != 2 || rules[0].Media[0].Name != "Картинка 13" {
		t.Errorf("правило r69: %+v", rules[0])
	}
	if rules[1].Kind != domain.RuleSurveyReminder || len(rules[1].RemindAfterH) != 2 || !strings.Contains(rules[1].TextTy, "{Имя}") {
		t.Errorf("правило r70: %+v", rules[1])
	}
}

func TestParseTZWhen(t *testing.T) {
	cases := map[string]whenSpec{
		"За 2-3 дня до выхода на работу":             {kind: whenTime, day: -3},
		"За день до выхода на работу":                {kind: whenTime, day: -1},
		"В первый рабочий день в 10:00":              {kind: whenTime, day: 0, time: "10:00"},
		"В первый рабочий день В 12.30":              {kind: whenTime, day: 0, time: "12:30"},
		"Во второй рабочий день 10:00":               {kind: whenTime, day: 1, time: "10:00", approx: approxWorkdays},
		"Через 10 дней после выхода на работу":       {kind: whenTime, day: 10},
		"Через 2 недели после выхода на работу":      {kind: whenTime, day: 14},
		"Первый день второго месяца работы в 09.00.": {kind: whenTime, day: 30, time: "09:00", approx: approxMonths},
		"Через 1 месяц и  3 недели. В 09.01":         {kind: whenTime, day: 51, time: "09:01", approx: approxMonths},
		"Последний день ИС":                          {kind: whenTime, day: 89, approx: approxProbEnd},
		"В 09.01":                                    {kind: whenAfter, time: "09:01"},
		"":                                           {kind: whenContinue},
		"D+14":                                       {kind: whenTime, day: 14},
		"когда-нибудь":                               {kind: whenUnknown},
		"Если сотрудник задаст вопрос":               {kind: whenRule},
		"В первый рабочий день В 18:00 для сотрудников офиса": {kind: whenTime, time: "18:00"},
	}
	for in, want := range cases {
		got := parseTZWhen(in)
		if got.kind != want.kind || got.day != want.day || got.time != want.time || got.approx != want.approx {
			t.Errorf("%q: %+v, ждём %+v", in, got, want)
		}
	}
	if w := parseTZWhen("В случае варианта ответа\n «Да»"); w.kind != whenBranch || w.answer != "Да" {
		t.Errorf("ветка: %+v", w)
	}
}

func TestImportLegacyFormat(t *testing.T) {
	f := excelize.NewFile()
	rows := [][]any{
		{"Когда", "Повод", "Метка", "Макет", "Текст"},
		{"D−3", "Документы", "GTP, GrPP", "текст", "Возьмите паспорт"},
		{"D+14", "Чек", "Лужники", "опрос", "Как дела?"},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		_ = f.SetSheetRow("Sheet1", cell, &r)
	}
	var buf bytes.Buffer
	_ = f.Write(&buf)
	steps, sites, err := ParseScenarioExcel(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].DayOffset != -3 || len(sites) != 3 || steps[1].Survey == nil || steps[1].Survey.Questions[0].Text != "Как дела?" {
		t.Fatalf("старый формат: %+v %v", steps, sites)
	}
}
