package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"main/config"
	"main/domain"
	"main/pkg/logger"
)

// Переписывание текстов сценария (agency/prompts/address-vy.md): «ты» → «вы» (RewriteVy) и
// нейтральный род для варианта «ты» (Neutralize). LLM только правит текст; что ответ годен,
// решает код (CheckRewrite): те же числа, ссылки, плейсхолдеры, эмодзи и переносы строк, длина
// ±35 %, нет «ты» (режим vy) и родовых форм. Не прошло — один ретрай, затем правило-фолбэк
// (address_form_rules.go, только для vy). Удачные ответы LLM — в кэше text_rewrites.

const (
	addressLLMTimeout = 60 * time.Second
	// addressLenSlack — допуск длины: ±35 %, но не меньше 8 символов («Пиши» → «Пишите»).
	addressLenRatio = 0.35
	addressLenMin   = 8
	addressMaxOut   = 3000
)

// ErrRewriteFailed — текст не удалось переписать: LLM не прошёл проверки, а фолбэка для
// режима нет (neutral) или он ничего не изменил.
var ErrRewriteFailed = errors.New("текст не переписан")

// LLMMessage — сообщение запроса к модели (для подделок вне пакета).
type LLMMessage = llmMessage

// RewriteLLM — модель: system + few-shot + user → содержимое ответа (как DigestLLM).
type RewriteLLM interface {
	Complete(ctx context.Context, messages []llmMessage) (string, error)
}

// rewriteCache — кэш по хэшу (repositories.TextRewriteRepo; в тестах — память).
type rewriteCache interface {
	Get(ctx context.Context, hash string) (string, bool, error)
	Put(ctx context.Context, hash, mode, text string) error
}

// AddressFormService — переписывание обращения и рода.
type AddressFormService struct {
	llm   RewriteLLM   // nil — сразу правило-фолбэк
	cache rewriteCache // nil — без кэша
	model string
}

// RewriteResult — текст и откуда он. Source пуст, если текст не менялся.
type RewriteResult struct {
	Text     string
	Source   string // llm | rule
	Cached   bool
	Rejected []string // почему отброшены ответы LLM (для лога)
}

// newAddressLLM — nil, если LLM_BASE_URL не задан: работает правило-фолбэк.
func newAddressLLM(cfg config.LLM) RewriteLLM {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	return &openAILLM{cfg: cfg, http: &http.Client{Timeout: addressLLMTimeout},
		schemaName: "address_text", schemaJSON: addressSchemaJSON,
		// address-vy.md G: правка, а не пересказ; штраф за повтор мешает копировать текст.
		temperature: 0.1, presencePenalty: 0,
		maxTokens: func(m []llmMessage) int {
			n := 256
			if len(m) > 0 {
				n += utf8.RuneCountInString(m[len(m)-1].Content)
			}
			return min(n, addressMaxOut)
		}}
}

// AddressForm — сервис переписывания (по образцу Digest: без поля в Services).
func (s *Services) AddressForm() *AddressFormService {
	a := &AddressFormService{model: s.digestOpts.LLM.Model}
	if s.rewriteLLM != nil {
		a.llm = s.rewriteLLM
	} else {
		a.llm = newAddressLLM(s.digestOpts.LLM)
	}
	if s.repos.Enabled() {
		a.cache = s.repos.Rewrites
	}
	return a
}

// SetRewriteLLM — подменить модель переписывания (тесты, подделка).
func (s *Services) SetRewriteLLM(l RewriteLLM) { s.rewriteLLM = l }

// RewriteVy — текст на «вы» по тексту на «ты».
func (s *AddressFormService) RewriteVy(ctx context.Context, textTy string) (string, error) {
	r, err := s.Rewrite(ctx, domain.RewriteVy, textTy)
	return r.Text, err
}

// Neutralize — текст на «ты» без мужского и женского рода.
func (s *AddressFormService) Neutralize(ctx context.Context, textTy string) (string, error) {
	r, err := s.Rewrite(ctx, domain.RewriteNeutral, textTy)
	return r.Text, err
}

// Rewrite — текст в режиме mode: кэш, LLM с проверками и ретраем, затем правило-фолбэк.
func (s *AddressFormService) Rewrite(ctx context.Context, mode, text string) (RewriteResult, error) {
	if !hasCyrillic(text) {
		return RewriteResult{Text: text}, nil
	}
	hash := rewriteHash(mode, s.model, text)
	if s.cache != nil {
		if out, ok, err := s.cache.Get(ctx, hash); err == nil && ok {
			return RewriteResult{Text: out, Source: domain.RewriteSourceLLM, Cached: true}, nil
		}
	}
	var rejected []string
	if s.llm != nil {
		msgs := addressMessages(mode, text)
		for attempt := 0; attempt < 2 && ctx.Err() == nil; attempt++ {
			content, err := s.llm.Complete(ctx, msgs)
			if err != nil {
				rejected = append(rejected, err.Error())
				logger.Warn("переписывание (%s): LLM недоступен, попытка %d: %v", mode, attempt+1, err)
				continue
			}
			out, problems := ParseRewrite(mode, text, content)
			if len(problems) == 0 {
				if s.cache != nil {
					if err := s.cache.Put(ctx, hash, mode, out); err != nil {
						logger.Warn("переписывание: кэш не записан: %v", err)
					}
				}
				return RewriteResult{Text: out, Source: domain.RewriteSourceLLM}, nil
			}
			rejected = append(rejected, problems...)
			// Ретрай (address-vy.md, B2): модель видит свой ответ и что в нём не так.
			msgs = append(addressMessages(mode, text), llmMessage{Role: "assistant", Content: content},
				llmMessage{Role: "user", Content: strings.Replace(addressRetryTemplate, "{problems}", strings.Join(problems, "; "), 1)})
			logger.Warn("переписывание (%s): ответ LLM отклонён (попытка %d): %s", mode, attempt+1, strings.Join(problems, "; "))
		}
	}
	if mode != domain.RewriteVy {
		return RewriteResult{Text: text, Rejected: rejected}, ErrRewriteFailed
	}
	out := RuleRewriteVy(text)
	if out == text {
		if NeedsVy(text) {
			return RewriteResult{Text: text, Rejected: rejected}, ErrRewriteFailed
		}
		return RewriteResult{Text: text, Rejected: rejected}, nil
	}
	return RewriteResult{Text: out, Source: domain.RewriteSourceRule, Rejected: rejected}, nil
}

func rewriteHash(mode, model, text string) string {
	h := sha256.Sum256([]byte(mode + "|" + addressPromptVersion + "|" + model + "|" + text))
	return hex.EncodeToString(h[:])
}

// addressMessages — system режима, два примера и текст.
func addressMessages(mode, text string) []llmMessage {
	system, shots := addressSystemVy, addressFewShotVy
	if mode == domain.RewriteNeutral {
		system, shots = addressSystemNeutral, addressFewShotNeutral
	}
	user := func(t string) string { return strings.Replace(addressUserTemplate, "{text}", t, 1) }
	msgs := []llmMessage{{Role: "system", Content: system}}
	for _, shot := range shots {
		msgs = append(msgs, llmMessage{Role: "user", Content: user(shot[0])}, llmMessage{Role: "assistant", Content: shot[1]})
	}
	return append(msgs, llmMessage{Role: "user", Content: user(text)})
}

// ParseRewrite — разбор ответа LLM и проверки (address-vy.md, E). Пустой список — годен.
func ParseRewrite(mode, in, content string) (string, []string) {
	var raw struct {
		Text *string `json:"text"`
	}
	if err := json.Unmarshal([]byte(fenceRe.ReplaceAllString(strings.TrimSpace(content), "")), &raw); err != nil || raw.Text == nil {
		return "", []string{"ответ не JSON с полем text"}
	}
	out := strings.TrimSpace(*raw.Text)
	// Модель могла вернуть \r\n или обрезать хвостовые переносы — сравниваем как во входе.
	out = strings.ReplaceAll(out, "\r\n", "\n")
	if mode == domain.RewriteVy {
		out = LowerVyInside(out)
	}
	if lead := leadingSpace(in); lead != "" {
		out = lead + out
	}
	if trail := trailingSpace(in); trail != "" {
		out += trail
	}
	return out, CheckRewrite(mode, in, out)
}

func leadingSpace(s string) string  { return s[:len(s)-len(strings.TrimLeftFunc(s, unicode.IsSpace))] }
func trailingSpace(s string) string { return s[len(strings.TrimRightFunc(s, unicode.IsSpace)):] }

var (
	reRwNumber      = regexp.MustCompile(`\d+`)
	reRwURL         = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s)\]»"<>]+`)
	reRwPlaceholder = regexp.MustCompile(`\{[^{}\n]{1,40}\}`)
	reCyrillic      = regexp.MustCompile(`\p{Cyrillic}`)
	reRwFirstSg     = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(я|меня|мне|мной|мой|моя|моё|мое|мои|моего|моей|моих|моим|моими|моему|мою)(?:[^\p{L}]|$)`)
	reRwFirstPl     = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(мы|нас|нам|нами|наш|наша|наше|наши|нашего|нашей|наших|нашим|нашими|нашему|нашу)(?:[^\p{L}]|$)`)
	// reVyCapital — «Вы/Ваш…» с заглавной после буквы, цифры, запятой, двоеточия или тире.
	reVyCapital = regexp.MustCompile(`([\p{L}\p{N},;:—–\-)])(\s+)(Вы|Вас|Вам|Вами|Ваш|Ваша|Ваше|Ваши|Вашего|Вашей|Ваших|Вашим|Вашими|Вашему|Вашу|Вашем)([^\p{L}]|$)`)
)

func hasCyrillic(s string) bool { return reCyrillic.MatchString(s) }

// LowerVyInside — «Вы» внутри предложения со строчной: «помогу Вам» → «помогу вам».
func LowerVyInside(s string) string {
	for {
		n := reVyCapital.ReplaceAllStringFunc(s, func(m string) string {
			p := reVyCapital.FindStringSubmatch(m)
			return p[1] + p[2] + strings.ToLower(p[3]) + p[4]
		})
		if n == s {
			return s
		}
		s = n
	}
}

// CheckRewrite — проверки переписанного текста (address-vy.md, E). Пусто — годен.
func CheckRewrite(mode, in, out string) []string {
	if strings.TrimSpace(out) == "" {
		return []string{"пустой текст"}
	}
	var problems []string
	same := func(what string, re *regexp.Regexp) {
		if a, b := sortedAll(re, in), sortedAll(re, out); !slices.Equal(a, b) {
			problems = append(problems, fmt.Sprintf("%s: было %v, стало %v", what, a, b))
		}
	}
	same("числа", reRwNumber)
	same("ссылки", reRwURL)
	same("плейсхолдеры", reRwPlaceholder)
	if a, b := emojiOf(in), emojiOf(out); !slices.Equal(a, b) {
		problems = append(problems, fmt.Sprintf("эмодзи: было %d, стало %d", len(a), len(b)))
	}
	// Бот говорит о себе — не обращение: «я мог» не должно стать «вы могли».
	for _, c := range []struct {
		re   *regexp.Regexp
		what string
	}{{reRwFirstSg, "«я, мне, мой»"}, {reRwFirstPl, "«мы, нас, наш»"}} {
		if a, b := len(c.re.FindAllString(in, -1)), len(c.re.FindAllString(out, -1)); a != b {
			problems = append(problems, fmt.Sprintf("слова бота о себе %s: было %d, стало %d — их не меняй", c.what, a, b))
		}
	}
	if a, b := strings.Count(in, "\n"), strings.Count(out, "\n"); a != b {
		problems = append(problems, fmt.Sprintf("переносов строк было %d, стало %d", a, b))
	}
	ni, no := utf8.RuneCountInString(in), utf8.RuneCountInString(out)
	slack := max(int(float64(ni)*addressLenRatio), addressLenMin)
	if no < ni-slack || no > ni+slack {
		problems = append(problems, fmt.Sprintf("длина %d вместо %d (±35 %%)", no, ni))
	}
	// Цитаты «…», которые были во входе, не проверяем: это названия («Ты способна на большее»).
	plain := keptQuotesOut(in, out)
	if mode == domain.RewriteVy {
		if m := reTyWords.FindStringSubmatch(plain); m != nil {
			problems = append(problems, fmt.Sprintf("осталось «%s»", m[1]))
		} else if NeedsVy(plain) && !isGendered(plain) {
			// глагол на «ты» без местоимения: «Оцени, пожалуйста» (правило знает форму)
			w, r := firstRuleChange(plain)
			problems = append(problems, fmt.Sprintf("осталась форма на «ты»: «%s» → «%s»", w, r))
		}
	} else if len(reVyWords.FindAllString(out, -1)) > len(reVyWords.FindAllString(in, -1)) {
		problems = append(problems, "обращение сменилось на «вы»")
	}
	if isGendered(plain) {
		problems = append(problems, "остались родовые формы"+genderedHint(plain))
	} else if v := keptGenderVerb(in, plain); v != "" {
		// «ты вышел» → «вышел»: «ты» убрали, а глагол в мужском роде остался
		problems = append(problems, "остались родовые формы: «"+v+"»")
	}
	return problems
}

// genderedHint — «: «ты вышел»» — что именно в роде (для ретрая, address-vy.md B2).
func genderedHint(s string) string {
	for _, re := range []*regexp.Regexp{reGenderPast, reGenderWords, reRuleSlash} {
		if m := re.FindString(s); m != "" {
			return ": «" + strings.TrimFunc(m, func(r rune) bool { return !unicode.IsLetter(r) }) + "»"
		}
	}
	return ""
}

// keptGenderVerb — глагол из «ты пришёл» входа, который остался в out без «ты».
func keptGenderVerb(in, out string) string {
	outWords := map[string]bool{}
	for _, w := range reRuleWord.FindAllString(strings.ToLower(out), -1) {
		outWords[w] = true
	}
	for _, m := range reGenderPast.FindAllString(unquoted(in), -1) {
		ws := reRuleWord.FindAllString(strings.ToLower(m), -1)
		if v := ws[len(ws)-1]; outWords[v] {
			return v
		}
	}
	return ""
}

// keptQuotesOut — out без цитат «…», которые дословно есть во входе.
func keptQuotesOut(in, out string) string {
	return reRwQuoted.ReplaceAllStringFunc(out, func(q string) string {
		if strings.Contains(in, q) {
			return "«»"
		}
		return q
	})
}

func sortedAll(re *regexp.Regexp, s string) []string {
	out := re.FindAllString(s, -1)
	slices.Sort(out)
	return out
}

// emojiOf — эмодзи и символы-пиктограммы текста по порядку кода (без VS16 и ZWJ).
func emojiOf(s string) []rune {
	var out []rune
	for _, r := range s {
		if isEmojiRune(r) {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF, r >= 0x2300 && r <= 0x23FF,
		r >= 0x2B00 && r <= 0x2BFF, r >= 0x2190 && r <= 0x21FF:
		return true
	}
	return false
}
