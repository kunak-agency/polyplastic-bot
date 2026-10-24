package services

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi/rag"
)

// CallRAG — что звонку нужно от rag-пода (rag.Client): ответ по регламентам, распознавание
// и синтез речи.
type CallRAG interface {
	AskWithin(ctx context.Context, req rag.AskRequest, d time.Duration) (*rag.AskResponse, error)
	AskTimeout() time.Duration
	DialStream(ctx context.Context) (*rag.Stream, error)
	Synthesize(ctx context.Context, text string) (io.ReadCloser, error)
}

// CallService — звонок Корсику в браузере (FEATURE_CALL): лимит одновременных звонков и одна
// реплика — один вызов rag /ask. В базу ничего не пишет: звонок — демо, только лог метрик.
type CallService struct {
	rag     CallRAG
	cfg     config.Call
	enabled bool

	mu     sync.Mutex
	active int
}

// NewCallService — rag nil: звонок открывается, но каждый ответ — «не получилось найти».
func NewCallService(r CallRAG, cfg config.Call, enabled bool) *CallService {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.MaxDuration <= 0 {
		cfg.MaxDuration = 5 * time.Minute
	}
	return &CallService{rag: r, cfg: cfg, enabled: enabled}
}

// Enabled — FEATURE_CALL.
func (s *CallService) Enabled() bool { return s != nil && s.enabled }

// Config — лимиты звонка.
func (s *CallService) Config() config.Call { return s.cfg }

// Acquire — занять линию. false — линии заняты (CALL_MAX_CONCURRENT); release освобождает
// ровно один раз.
func (s *CallService) Acquire() (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active >= s.cfg.MaxConcurrent {
		return nil, false
	}
	s.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.active--
			s.mu.Unlock()
		})
	}, true
}

// VoiceAnswer — ответ на реплику звонка: что сказать, что показать и сколько думал rag.
type VoiceAnswer struct {
	Kind    string // VoiceKindAnswer | VoiceKindAbstain | VoiceKindError
	Text    string
	Sources []VoiceSource
	RagMs   int64
	Err     error // для лога; собеседнику — Text
}

// Answer — вопрос в rag /ask, как у бота в MAX (на «вы», с площадкой), и текст для синтеза.
func (s *CallService) Answer(ctx context.Context, question, site string) VoiceAnswer {
	started := time.Now()
	if s.rag == nil {
		return VoiceAnswer{Kind: VoiceKindError, Text: VoiceError, Err: errNoRAG}
	}
	vy := domain.AddressVy
	req := rag.AskRequest{Question: question, AddressForm: &vy}
	if site = CallSite(site); site != "" {
		req.Site = &site
	}
	resp, err := s.rag.AskWithin(ctx, req, s.rag.AskTimeout())
	kind, text, sources := VoiceAnswerText(resp, err)
	return VoiceAnswer{Kind: kind, Text: text, Sources: sources, RagMs: time.Since(started).Milliseconds(), Err: err}
}

// CallSite — код площадки из start{site} (GrPP, GTP, Luzhniki; регистр не важен) или "".
func CallSite(site string) string {
	site = strings.TrimSpace(site)
	if i := slices.IndexFunc(domain.SiteCodes, func(c string) bool { return strings.EqualFold(c, site) }); i >= 0 {
		return domain.SiteCodes[i]
	}
	return ""
}

// DialSpeech — сокет распознавания на весь звонок.
func (s *CallService) DialSpeech(ctx context.Context) (*rag.Stream, error) {
	if s.rag == nil {
		return nil, errNoRAG
	}
	return s.rag.DialStream(ctx)
}

// Synthesize — звук фразы: PCM16LE 8 кГц потоком.
func (s *CallService) Synthesize(ctx context.Context, text string) (io.ReadCloser, error) {
	if s.rag == nil {
		return nil, errNoRAG
	}
	return s.rag.Synthesize(ctx, text)
}
