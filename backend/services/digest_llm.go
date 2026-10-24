package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"main/config"
	"main/pkg/logger"
)

// Связный текст дайджеста (agency/prompts/digest.md): LLM пишет summary/themes/changes только
// по агрегатам, цифры считает код. Guardrails (п. E): каждое число ответа есть во входе,
// длины полей, названия тем дословно; не прошло — один ретрай, затем шаблон кодом.

// Пределы полей (digest.md, E.2).
const (
	digestSummaryMax = 300
	digestThemesMax  = 400
	digestChangesMax = 300
	// digestLLMThemes — тем во входе LLM: шесть не укладываются в 400 символов (F.3).
	digestLLMThemes  = 4
	digestLLMTimeout = 25 * time.Second
)

// Источник текста.
const (
	DigestTextLLM      = "llm"
	DigestTextTemplate = "template"
)

// DigestAggregates — вход LLM (схема digest.md): только числа, названия тем и площадки.
type DigestAggregates struct {
	PeriodLabel     string            `json:"period_label"`
	Totals          DigestAggTotals   `json:"totals"`
	Themes          []DigestAggTheme  `json:"themes"`
	PreviousActions []DigestAggAction `json:"previous_actions"`
}

type DigestAggTotals struct {
	OnPulse         int  `json:"on_pulse"`
	Answered        int  `json:"answered"`
	ResponseRatePct *int `json:"response_rate_pct"`
	WAvg            *int `json:"w_avg"`
	WDelta          *int `json:"w_delta"`
	Problems        int  `json:"problems"`
	Critical        int  `json:"critical"`
}

type DigestAggTheme struct {
	Label string   `json:"label"`
	Count int      `json:"count"`
	Sites []string `json:"sites"`
	Owner *string  `json:"owner"`
}

// DigestAggAction — прошлые действия по темам. Хранилища действий пока нет: список пуст.
type DigestAggAction struct {
	Theme  string `json:"theme"`
	Action string `json:"action"`
	Status string `json:"status"`
	Before *int   `json:"before"`
	After  *int   `json:"after"`
}

// DigestText — три поля текста и откуда они.
type DigestText struct {
	Summary string
	Themes  *string
	Changes *string
	Source  string // llm | template
	// Rejected — почему отброшен ответ LLM (для лога и кабинета): «число 12 не из входа» и т. п.
	Rejected []string
}

// DigestLLM — модель: system + few-shot + user → содержимое ответа.
type DigestLLM interface {
	Complete(ctx context.Context, messages []llmMessage) (string, error)
}

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAILLM — vLLM на поде (OpenAI-совместимый /chat/completions), structured outputs.
// Схема и параметры запроса — свои у каждого промпта: дайджест (newDigestLLM) и переписывание
// обращения (newAddressLLM, address_form.go).
type openAILLM struct {
	cfg             config.LLM
	http            *http.Client
	schemaName      string
	schemaJSON      string
	temperature     float64
	presencePenalty float64
	// maxTokens — потолок ответа по сообщениям запроса.
	maxTokens func(messages []llmMessage) int
}

// newDigestLLM — nil, если LLM_BASE_URL не задан: дайджест собирается шаблоном.
func newDigestLLM(cfg config.LLM) DigestLLM {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	return &openAILLM{cfg: cfg, http: &http.Client{Timeout: digestLLMTimeout},
		schemaName: "digest_text", schemaJSON: digestSchemaJSON,
		temperature: 0.2, presencePenalty: cfg.PresencePenalty, // digest.md G: передавать явно
		maxTokens: func([]llmMessage) int { return 450 }}
}

func (c *openAILLM) Complete(ctx context.Context, messages []llmMessage) (string, error) {
	var schema any
	_ = json.Unmarshal([]byte(c.schemaJSON), &schema)
	body := map[string]any{
		"model":            c.cfg.Model,
		"messages":         messages,
		"temperature":      c.temperature,
		"presence_penalty": c.presencePenalty,
		"top_p":            0.8,
		"max_tokens":       c.maxTokens(messages),
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": c.schemaName, "schema": schema},
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM: статус %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("LLM: ответ не по форме")
	}
	return out.Choices[0].Message.Content, nil
}

// digestMessages — system, два примера и агрегаты.
func digestMessages(aggJSON string) []llmMessage {
	msgs := []llmMessage{{Role: "system", Content: digestSystemPrompt}}
	for _, shot := range digestFewShot {
		msgs = append(msgs, llmMessage{Role: "user", Content: shot[0]}, llmMessage{Role: "assistant", Content: shot[1]})
	}
	return append(msgs, llmMessage{Role: "user", Content: strings.Replace(digestUserTemplate, "{aggregates_json}", aggJSON, 1)})
}

// GenerateDigestText — текст по агрегатам: LLM с проверками, иначе шаблон.
func GenerateDigestText(ctx context.Context, llm DigestLLM, agg DigestAggregates) DigestText {
	aggJSON, _ := json.Marshal(agg)
	tmpl := TemplateDigestText(agg)
	if llm == nil {
		return tmpl
	}
	var rejected []string
	for attempt := 0; attempt < 2; attempt++ {
		content, err := llm.Complete(ctx, digestMessages(string(aggJSON)))
		if err != nil {
			rejected = append(rejected, err.Error())
			logger.Warn("дайджест: LLM недоступен, попытка %d: %v", attempt+1, err)
			if ctx.Err() != nil {
				break
			}
			continue
		}
		txt, problems := ParseDigestText(content, agg, string(aggJSON))
		if len(problems) == 0 {
			txt.Source = DigestTextLLM
			return txt
		}
		rejected = append(rejected, problems...)
		logger.Warn("дайджест: ответ LLM отклонён (попытка %d): %s", attempt+1, strings.Join(problems, "; "))
	}
	tmpl.Rejected = rejected
	return tmpl
}

var fenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*|\\s*```$")

// ParseDigestText — разбор и проверки ответа LLM (digest.md, E). Пустой список проблем — годен.
func ParseDigestText(content string, agg DigestAggregates, aggJSON string) (DigestText, []string) {
	var raw struct {
		Summary *string `json:"summary"`
		Themes  *string `json:"themes"`
		Changes *string `json:"changes"`
	}
	if err := json.Unmarshal([]byte(fenceRe.ReplaceAllString(strings.TrimSpace(content), "")), &raw); err != nil {
		return DigestText{}, []string{"ответ не JSON"}
	}
	var problems []string
	if raw.Summary == nil || strings.TrimSpace(*raw.Summary) == "" {
		problems = append(problems, "нет summary")
		raw.Summary = new(string)
	}
	txt := DigestText{Summary: strings.TrimSpace(*raw.Summary), Themes: trimPtr(raw.Themes), Changes: trimPtr(raw.Changes)}
	check := func(name string, v *string, limit int) {
		if v == nil {
			return
		}
		if n := utf8.RuneCountInString(*v); n > limit {
			problems = append(problems, fmt.Sprintf("%s длиннее %d символов", name, limit))
		}
		for _, num := range ExtraNumbers(*v, aggJSON) {
			problems = append(problems, fmt.Sprintf("%s: число %s не из входа", name, num))
		}
	}
	check("summary", &txt.Summary, digestSummaryMax)
	check("themes", txt.Themes, digestThemesMax)
	check("changes", txt.Changes, digestChangesMax)
	switch {
	case len(agg.Themes) == 0 && txt.Themes != nil:
		txt.Themes = nil // тем нет — текст про темы не нужен (и выдумывать нечего)
	case len(agg.Themes) > 0 && txt.Themes == nil:
		problems = append(problems, "themes пуст при непустых темах")
	case txt.Themes != nil:
		low := strings.ToLower(*txt.Themes)
		for _, t := range agg.Themes {
			if !strings.Contains(low, strings.ToLower(t.Label)) {
				problems = append(problems, fmt.Sprintf("тема «%s» не названа дословно", t.Label))
			}
		}
	}
	if len(agg.PreviousActions) == 0 {
		txt.Changes = nil
	}
	return txt, problems
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

var numberRe = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

// numberSet — все числа строки (запятая как точка).
func numberSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, n := range numberRe.FindAllString(s, -1) {
		out[strings.ReplaceAll(n, ",", ".")] = true
	}
	return out
}

// ExtraNumbers — числа текста, которых нет во входе (digest.md, E.1). Пусто — проверка прошла.
func ExtraNumbers(text, input string) []string {
	in := numberSet(input)
	var out []string
	seen := map[string]bool{}
	for _, n := range numberRe.FindAllString(text, -1) {
		k := strings.ReplaceAll(n, ",", ".")
		if !in[k] && !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	return out
}

// TemplateDigestText — текст кодом из тех же агрегатов (без LLM и при отказе проверки).
func TemplateDigestText(agg DigestAggregates) DigestText {
	t := agg.Totals
	var b strings.Builder
	fmt.Fprintf(&b, "На связи %s, ответили %d", plural(t.OnPulse, "новичок", "новичка", "новичков"), t.Answered)
	if t.ResponseRatePct != nil {
		fmt.Fprintf(&b, " (%d %%)", *t.ResponseRatePct)
	}
	b.WriteString(".")
	if t.WAvg != nil {
		fmt.Fprintf(&b, " Средний индекс W — %d", *t.WAvg)
		if t.WDelta != nil {
			switch d := *t.WDelta; {
			case d < 0:
				fmt.Fprintf(&b, ", снизился на %d", -d)
			case d > 0:
				fmt.Fprintf(&b, ", вырос на %d", d)
			default:
				b.WriteString(", без изменений")
			}
		}
		b.WriteString(".")
	}
	if t.Problems == 0 {
		b.WriteString(" Проблем не отмечено.")
	} else {
		fmt.Fprintf(&b, " Проблем — %d, из них критичных — %d.", t.Problems, t.Critical)
	}
	out := DigestText{Summary: b.String(), Source: DigestTextTemplate}
	if len(agg.Themes) > 0 {
		parts := make([]string, 0, len(agg.Themes))
		for _, th := range agg.Themes {
			p := fmt.Sprintf("%s — %s", capitalize(th.Label), plural(th.Count, "новичок", "новичка", "новичков"))
			if len(th.Sites) > 0 {
				p += ", " + strings.Join(th.Sites, " и ")
			}
			if th.Owner != nil {
				p += ", ответственный: " + *th.Owner
			}
			parts = append(parts, p+".")
		}
		s := strings.Join(parts, " ")
		out.Themes = &s
	}
	return out
}

// plural — «1 новичок», «3 новичка», «5 новичков».
func plural(n int, one, few, many string) string {
	a := n
	if a < 0 {
		a = -a
	}
	w := many
	switch {
	case a%100 >= 11 && a%100 <= 14:
	case a%10 == 1:
		w = one
	case a%10 >= 2 && a%10 <= 4:
		w = few
	}
	return fmt.Sprintf("%d %s", n, w)
}
