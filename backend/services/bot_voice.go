package services

import (
	"context"
	"strings"
	"unicode/utf8"

	"main/domain"
	"main/externalApi/max"
	"main/externalApi/rag"
	"main/pkg/logger"
)

// Голосовые новичка (api_contract.md, «Голосовые»): скачать по ссылке MAX → POST /transcribe
// rag-сервиса (T-one, локально) → показать «🎙 Распознал: …» → дальше текст идёт тем же путём,
// что набранный: ввод шага, «свой вариант», ответ опроса, детали, вопрос в rag.
//
// Ни звук, ни распознанный текст в лог не пишем: только id новичка, длительность и итог.

// Transcriber — что боту нужно от распознавания. Интерфейс здесь, чтобы тесты шли на подделке.
type Transcriber interface {
	Transcribe(ctx context.Context, filename string, data []byte) (*rag.Transcript, error)
}

const (
	// maxVoiceSeconds — длиннее не распознаём (лимит /transcribe — тоже 120 с).
	maxVoiceSeconds = 120
	// maxVoiceBytes — потолок скачивания: 2 минуты opus — сотни КБ, m4a/mp3 — единицы МБ.
	maxVoiceBytes = 20 << 20

	// voicePrefix — эхо распознанного (T-one пишет строчными — первая буква заглавной через
	// capitalize); отказ — textVoiceTy/textVoiceVy (bot_ask_render.go).
	voicePrefix = "🎙 Распознал: "
)

// voiceText — текст голосового сообщения. ok=false — разобрать не вышло, новичку уже ушло «Не
// разобрал голосовое…», дальше сообщение не обрабатывается.
func (b *BotService) voiceText(ctx context.Context, u max.Update, emp *domain.Employee) (string, bool) {
	to := recipientOf(emp)
	if to.ChatID == 0 && to.UserID == 0 {
		to = recipient{ChatID: u.ChatID, UserID: u.UserID}
	}
	text, why := b.recognize(ctx, u, to)
	if text == "" {
		logger.Info("бот: голосовое новичка %s не разобрано: %s", emp.ID, why)
		b.replyOn(ctx, to.msg(byForm(emp, textVoiceTy, textVoiceVy), "", nil), u.MessageID)
		return "", false
	}
	logger.Info("бот: голосовое новичка %s распознано (%s), %d символов", emp.ID, why, utf8.RuneCountInString(text))
	b.replyOn(ctx, to.msg(voicePrefix+capitalize(text), "", nil), u.MessageID) // реплаем на голосовое
	return text, true
}

// recognize — текст и откуда он (или почему его нет — для лога). Расшифровка от самого MAX, если
// пришла, берётся без своего распознавания.
func (b *BotService) recognize(ctx context.Context, u max.Update, to recipient) (string, string) {
	a := u.Audio[0]
	if t := strings.TrimSpace(a.Transcription); t != "" {
		return t, "расшифровка MAX"
	}
	if a.Duration > maxVoiceSeconds {
		return "", "длиннее 120 с"
	}
	dl, ok := b.max.(max.Downloader)
	if !ok || b.asr == nil {
		return "", "распознавание не подключено"
	}
	if a.URL == "" {
		return "", "нет ссылки на файл"
	}
	stop := b.typing(ctx, to.ChatID)
	defer stop()
	data, err := dl.Download(ctx, a.URL, maxVoiceBytes)
	if err != nil {
		return "", "не скачалось: " + err.Error()
	}
	res, err := b.asr.Transcribe(ctx, "voice.ogg", data)
	if err != nil {
		return "", "rag: " + err.Error()
	}
	if t := strings.TrimSpace(res.Text); t != "" {
		return t, "T-one"
	}
	return "", "пустое распознавание"
}
