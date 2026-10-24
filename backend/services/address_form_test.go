package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"main/domain"
)

// Переписывание обращения (address_form.go): проверки ответа, ретрай, фолбэк, кэш — на
// подделке модели; правило-фолбэк — на типичных фразах; промпт сверяется с md.

// fakeRewriteLLM — отдаёт ответы по очереди; закончились — последний. err — всегда ошибка.
type fakeRewriteLLM struct {
	mu      sync.Mutex
	answers []string
	err     error
	calls   int
	last    []llmMessage
}

func (f *fakeRewriteLLM) Complete(_ context.Context, m []llmMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = m
	if f.err != nil {
		return "", f.err
	}
	i := min(f.calls-1, len(f.answers)-1)
	return f.answers[i], nil
}

type memCache map[string]string

func (c memCache) Get(_ context.Context, h string) (string, bool, error) {
	v, ok := c[h]
	return v, ok, nil
}

func (c memCache) Put(_ context.Context, h, _, t string) error {
	c[h] = t
	return nil
}

func ans(text string) string {
	b, _ := json.Marshal(map[string]string{"text": text})
	return string(b)
}

const (
	tyText = "Привет, {Имя}! 👋 Завтра твой первый день: приходи к 8:30.\nСправочник: https://example.ru/guide"
	vyText = "Здравствуйте, {Имя}! 👋 Завтра ваш первый день: приходите к 8:30.\nСправочник: https://example.ru/guide"
)

func TestRewriteVyAcceptsValidAnswer(t *testing.T) {
	llm := &fakeRewriteLLM{answers: []string{ans(vyText)}}
	cache := memCache{}
	s := &AddressFormService{llm: llm, cache: cache, model: "tlite"}
	r, err := s.Rewrite(context.Background(), domain.RewriteVy, tyText)
	if err != nil || r.Text != vyText || r.Source != domain.RewriteSourceLLM || llm.calls != 1 {
		t.Fatalf("ответ: %+v, %v, вызовов %d", r, err, llm.calls)
	}
	// Промпт: system режима «вы», два примера, текст в теге.
	if llm.last[0].Content != addressSystemVy || len(llm.last) != 6 || !strings.Contains(llm.last[5].Content, "<text>\n"+tyText+"\n</text>") {
		t.Fatalf("сообщения модели: %d", len(llm.last))
	}
	// Кэш: второй раз модель не зовётся.
	r, err = s.Rewrite(context.Background(), domain.RewriteVy, tyText)
	if err != nil || r.Text != vyText || !r.Cached || llm.calls != 1 {
		t.Fatalf("кэш: %+v, вызовов %d", r, llm.calls)
	}
	if got, _ := s.RewriteVy(context.Background(), tyText); got != vyText {
		t.Fatalf("RewriteVy: %q", got)
	}
}

func TestRewriteVyRetryThenAccept(t *testing.T) {
	bad := strings.Replace(vyText, "8:30", "9:30", 1) // число не из входа
	llm := &fakeRewriteLLM{answers: []string{ans(bad), "```json\n" + ans(vyText) + "\n```"}}
	s := &AddressFormService{llm: llm, cache: memCache{}}
	r, err := s.Rewrite(context.Background(), domain.RewriteVy, tyText)
	if err != nil || r.Text != vyText || llm.calls != 2 || r.Source != domain.RewriteSourceLLM {
		t.Fatalf("ретрай: %+v, %v, вызовов %d", r, err, llm.calls)
	}
	// Ретрай несёт отклонённый ответ и причину отказа.
	n := len(llm.last)
	if n != 8 || llm.last[n-2].Content != ans(bad) || !strings.Contains(llm.last[n-1].Content, "числа: было") {
		t.Fatalf("сообщения ретрая: %d, %q", n, llm.last[n-1].Content)
	}
}

func TestRewriteVyFallbackToRule(t *testing.T) {
	stillTy := strings.Replace(vyText, "ваш", "твой", 1)
	for name, llm := range map[string]*fakeRewriteLLM{
		"проверки не прошли": {answers: []string{ans(stillTy), "не JSON"}},
		"модель недоступна":  {err: errors.New("LLM: статус 502")},
	} {
		cache := memCache{}
		s := &AddressFormService{llm: llm, cache: cache}
		r, err := s.Rewrite(context.Background(), domain.RewriteVy, tyText)
		if err != nil || r.Source != domain.RewriteSourceRule || llm.calls != 2 || len(r.Rejected) != 2 {
			t.Fatalf("%s: %+v, %v, вызовов %d", name, r, err, llm.calls)
		}
		if r.Text != vyText {
			t.Errorf("%s: правило дало %q", name, r.Text)
		}
		if len(cache) != 0 {
			t.Errorf("%s: фолбэк попал в кэш", name)
		}
	}
	// Без модели — сразу правило.
	r, err := (&AddressFormService{}).Rewrite(context.Background(), domain.RewriteVy, "Расскажи о себе")
	if err != nil || r.Text != "Расскажите о себе" || r.Source != domain.RewriteSourceRule {
		t.Fatalf("без модели: %+v, %v", r, err)
	}
	// Нечего менять — текст как был, без источника.
	r, err = (&AddressFormService{}).Rewrite(context.Background(), domain.RewriteVy, "Добро пожаловать на завод!")
	if err != nil || r.Source != "" || r.Text != "Добро пожаловать на завод!" {
		t.Fatalf("без обращения: %+v, %v", r, err)
	}
}

func TestNeutralize(t *testing.T) {
	in := "Чтобы ты пришёл подготовленный, возьми паспорт.\nГотов/Готова?"
	good := "Чтобы всё прошло спокойно, возьми паспорт.\nВсё готово?"
	llm := &fakeRewriteLLM{answers: []string{ans(in), ans(good)}} // первый ответ — без правки
	s := &AddressFormService{llm: llm}
	out, err := s.Neutralize(context.Background(), in)
	if err != nil || out != good || llm.calls != 2 {
		t.Fatalf("нейтрализация: %q, %v, вызовов %d", out, err, llm.calls)
	}
	if llm.last[0].Content != addressSystemNeutral {
		t.Fatal("system нейтрального режима")
	}
	// Для нейтрального режима фолбэка нет: текст как был и ошибка.
	vy := "Чтобы вы пришли, возьмите паспорт.\nГотовы?"
	llm = &fakeRewriteLLM{answers: []string{ans(vy)}} // перевела на «вы» — отказ
	r, err := (&AddressFormService{llm: llm}).Rewrite(context.Background(), domain.RewriteNeutral, in)
	if !errors.Is(err, ErrRewriteFailed) || r.Text != in || llm.calls != 2 {
		t.Fatalf("отказ: %+v, %v", r, err)
	}
}

func TestCheckRewrite(t *testing.T) {
	cases := map[string]string{
		"ссылки":       strings.Replace(vyText, "https://example.ru/guide", "https://example.ru", 1),
		"плейсхолдеры": strings.Replace(vyText, "{Имя}", "Иван", 1),
		"эмодзи":       strings.Replace(vyText, " 👋", "", 1),
		"переносов":    strings.Replace(vyText, "\n", " ", 1),
		"длина":        vyText + " Мы очень рады, что вы с нами, и будем помогать на каждом шагу адаптации.",
		"осталось":     strings.Replace(vyText, "ваш", "твой", 1),
		"родовые":      strings.Replace(vyText, "Здравствуйте", "Готов/Готова", 1),
		"пустой":       "  ",
		"о себе":       strings.Replace(vyText, "Справочник", "Мой справочник", 1),
	}
	for want, out := range cases {
		p := CheckRewrite(domain.RewriteVy, tyText, out)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "; "), want) {
			t.Errorf("%s: проблемы %v", want, p)
		}
	}
	if p := CheckRewrite(domain.RewriteVy, tyText, vyText); len(p) != 0 {
		t.Fatalf("годный ответ отклонён: %v", p)
	}
	if p := CheckRewrite(domain.RewriteVy, "Расскажи о себе", "Расскажи о себе"); len(p) == 0 {
		t.Fatal("непереписанный текст принят")
	}
	// «ты» убрали, а глагол в мужском роде остался (живой прогон).
	if p := CheckRewrite(domain.RewriteNeutral, "Скорее всего, ты вышел в период нагрузки.", "Скорее всего, вышел в период нагрузки."); len(p) == 0 {
		t.Fatal("«вышел» без «ты» принят")
	}
	// Название в кавычках модель вправе не трогать.
	if p := CheckRewrite(domain.RewriteVy, "Прочитай «Ты способна на большее»", "Прочитайте «Ты способна на большее»"); len(p) != 0 {
		t.Fatalf("цитата: %v", p)
	}
	// Местоимение переписано, а глагол остался на «ты» (так ответила T-lite на живом поде).
	in, out := "Оцени, пожалуйста, насколько тебе комфортно.", "Оцени, пожалуйста, насколько вам комфортно."
	if p := CheckRewrite(domain.RewriteVy, in, out); len(p) == 0 || !strings.Contains(p[0], "Оцени") {
		t.Fatalf("глагол на «ты» принят: %v", p)
	}
}

func TestLowerVyInside(t *testing.T) {
	in := "Вы молодец! Мы поможем Вам, а Ваш наставник — тоже.\nВаш пропуск готов. 👋 Вы здесь"
	want := "Вы молодец! Мы поможем вам, а ваш наставник — тоже.\nВаш пропуск готов. 👋 Вы здесь"
	if got := LowerVyInside(in); got != want {
		t.Fatalf("%q", got)
	}
	out, p := ParseRewrite(domain.RewriteVy, "Помогу тебе", ans("Помогу Вам"))
	if out != "Помогу вам" || len(p) != 0 {
		t.Fatalf("ParseRewrite: %q %v", out, p)
	}
}

// Правило-фолбэк на типичных фразах ТЗ на «ты» (синтетика, не тексты заказчика).
func TestRuleRewriteVy(t *testing.T) {
	cases := [][2]string{
		{"Привет, {Имя}! Я помогу тебе освоиться.", "Здравствуйте, {Имя}! Я помогу вам освоиться."},
		{"Расскажи немного о себе 😊", "Расскажите немного о себе 😊"},
		{"Если у тебя есть вопросы — обращайся!", "Если у вас есть вопросы — обращайтесь!"},
		{"Ты можешь написать мне в любое время.", "Вы можете написать мне в любое время."},
		{"Твой наставник встретит тебя на проходной в 8:30.", "Ваш наставник встретит вас на проходной в 8:30."},
		{"Ты уже получил пропуск?", "Вы уже получили пропуск?"},
		{"Готов/Готова начать?", "Готовы начать?"},
		{"Не стесняйся, свяжись с рекрутером: https://example.ru/hr", "Не стесняйтесь, свяжитесь с рекрутером: https://example.ru/hr"},
		{"Как ты себя чувствуешь после первой недели?", "Как вы себя чувствуете после первой недели?"},
		{"Познакомься с командой и возьми с собой паспорт.\n1. Приходи к 8:00.", "Познакомьтесь с командой и возьмите с собой паспорт.\n1. Приходите к 8:00."},
		{"Ты справился! Я готов помочь, запись в журнале.", "Вы справились! Я готов помочь, запись в журнале."},
		{"Ты готов? Пропуск готов, оцени нашу столовую.", "Вы готовы? Пропуск готов, оцените нашу столовую."},
		// Найдено живым прогоном: повтор «Привет-привет», возвратный инфинитив, -ишься, цитата.
		{"Привет-привет! Здесь можно развлечься, и ты насладишься отдыхом.", "Привет-привет! Здесь можно развлечься, и вы насладитесь отдыхом."},
		{"Книга «Ты способна на большее» — для тебя, если ты был рад работе.", "Книга «Ты способна на большее» — для вас, если вы были рады работе."},
		{"Книга «Ты способна на большее\", автор — Голдсмит. Прочитай!", "Книга «Ты способна на большее\", автор — Голдсмит. Прочитайте!"},
		// e2e-прогон 24.10: число при «вы», «бойся», «Дорогой {Имя}».
		{"Чтобы ты пришёл подготовленный в свой первый день, возьми паспорт.", "Чтобы вы пришли подготовленными в свой первый день, возьмите паспорт."},
		{"Опрос, чтобы ты смог поделиться впечатлениями.", "Опрос, чтобы вы смогли поделиться впечатлениями."},
		{"Работа, для которой ты был рожден. Не бойся спрашивать.", "Работа, для которой вы были рождены. Не бойтесь спрашивать."},
		{"Дорогой {Имя},\nТы только начинаешь.", "Здравствуйте, {Имя}!\nВы только начинаете."},
	}
	for _, c := range cases {
		got := RuleRewriteVy(c[0])
		if got != c[1] {
			t.Errorf("%q\n got %q\nwant %q", c[0], got, c[1])
		}
		if p := CheckRewrite(domain.RewriteVy, c[0], got); len(p) != 0 {
			t.Errorf("%q: правило не проходит проверки: %v", c[0], p)
		}
	}
	if NeedsVy("Добро пожаловать!") || NeedsVy("Привет-привет-привет! Я ваш гид.") || NeedsVy("Книга «Ты способна на большее»") || !NeedsVy("Расскажи о себе") {
		t.Fatal("NeedsVy")
	}
}

// Число при «вы» (e2e 24.10): ответ модели «чтобы вы пришли подготовленный» отклоняется, ретрай
// получает подсказку формы множественного числа.
func TestCheckRewriteVyPlural(t *testing.T) {
	in := "Чтобы ты пришёл подготовленный в первый день, возьми паспорт."
	for out, hint := range map[string]string{
		"Чтобы вы пришли подготовленный в первый день, возьмите паспорт.": "«подготовленными»",
		"Чтобы вы смог прийти в первый день, возьмите паспорт.":           "«смогли»",
		"Чтобы вы уже пришёл в первый день, возьмите паспорт.":            "«пришли»",
	} {
		p := strings.Join(CheckRewrite(domain.RewriteVy, in, out), "; ")
		if !strings.Contains(p, "множественном числе") || !strings.Contains(p, hint) {
			t.Errorf("%q: %s", out, p)
		}
	}
	good := []string{
		"Чтобы вы пришли подготовленными в первый день, возьмите паспорт.",
		"Вы получите собственный пропуск в первый день.",
		"Если вы находитесь на распутье, прочитайте книгу.",
		"Я уверен, что вы успешно справились с задачами.",
		"Ваш пропуск готов, а вы — молодец.",
	}
	for _, out := range good {
		if fx := vySingular(out); len(fx) != 0 {
			t.Errorf("ложное срабатывание %q: %+v", out, fx)
		}
	}
	llm := &fakeRewriteLLM{answers: []string{ans("Чтобы вы пришли подготовленный в первый день, возьмите паспорт."),
		ans("Чтобы вы пришли подготовленными в первый день, возьмите паспорт.")}}
	r, err := (&AddressFormService{llm: llm}).Rewrite(context.Background(), domain.RewriteVy, in)
	if err != nil || llm.calls != 2 || r.Source != domain.RewriteSourceLLM || !strings.Contains(r.Text, "подготовленными") {
		t.Fatalf("ретрай: %+v %v вызовов %d", r, err, llm.calls)
	}
	if retry := llm.last[len(llm.last)-1].Content; !strings.Contains(retry, "во множественном числе") || !strings.Contains(retry, "«подготовленными»") {
		t.Fatalf("подсказка ретрая: %q", retry)
	}
}

// «Дорогой {Имя}» — род: нейтрализация и «вы» дают «Здравствуйте, {Имя}!», даже без модели.
func TestDearNameGreeting(t *testing.T) {
	in := "Дорогой {Имя},\nЭти 3 месяца пролетели быстро!"
	if !isGendered(in) {
		t.Fatal("«Дорогой {Имя}» не родовая форма")
	}
	r, err := (&AddressFormService{}).Rewrite(context.Background(), domain.RewriteNeutral, in)
	if err != nil || r.Text != "Здравствуйте, {Имя}!\nЭти 3 месяца пролетели быстро!" || r.Source != domain.RewriteSourceRule {
		t.Fatalf("нейтрально: %+v %v", r, err)
	}
	p := strings.Join(CheckRewrite(domain.RewriteNeutral, in, in), "; ")
	if !strings.Contains(p, "Здравствуйте, {Имя}!") {
		t.Fatalf("подсказка: %s", p)
	}
	if got := RuleRewriteVy("Дорогая {имя}! Ты молодец."); got != "Здравствуйте, {имя}! Вы молодец." {
		t.Fatalf("вы: %q", got)
	}
}

// TestAddressPromptMatchesDoc — константы промпта совпадают с agency/prompts/address-vy.md.
func TestAddressPromptMatchesDoc(t *testing.T) {
	raw, err := os.ReadFile("../../agency/prompts/address-vy.md")
	if err != nil {
		t.Skip("address-vy.md не найден: ", err)
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
	if block("## A) System prompt", "text") != addressSystemVy {
		t.Error("system prompt «вы» разошёлся с address-vy.md")
	}
	if block("## A2) System prompt", "text") != addressSystemNeutral {
		t.Error("system prompt нейтрального рода разошёлся с address-vy.md")
	}
	if block("## B) User message", "text") != addressUserTemplate {
		t.Error("user template разошёлся с address-vy.md")
	}
	if block("## B2) Ретрай", "text") != addressRetryTemplate {
		t.Error("шаблон ретрая разошёлся с address-vy.md")
	}
	if block("## C) JSON-схема", "json") != addressSchemaJSON {
		t.Error("схема разошлась с address-vy.md")
	}
	shots := map[string][2]string{
		"### Пример В1": addressFewShotVy[0], "### Пример В2": addressFewShotVy[1],
		"### Пример Н1": addressFewShotNeutral[0], "### Пример Н2": addressFewShotNeutral[1],
	}
	for h, shot := range shots {
		if block(h, "text") != shot[0] || block(h, "json") != shot[1] {
			t.Errorf("few-shot %s разошёлся с address-vy.md", h)
		}
	}
	// Примеры сами проходят проверки кодом.
	for i, shot := range addressFewShotVy {
		if _, p := ParseRewrite(domain.RewriteVy, shot[0], shot[1]); len(p) != 0 {
			t.Errorf("пример В%d не проходит проверки: %v", i+1, p)
		}
	}
	for i, shot := range addressFewShotNeutral {
		if _, p := ParseRewrite(domain.RewriteNeutral, shot[0], shot[1]); len(p) != 0 {
			t.Errorf("пример Н%d не проходит проверки: %v", i+1, p)
		}
	}
}
