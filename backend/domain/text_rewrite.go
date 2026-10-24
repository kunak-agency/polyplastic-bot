package domain

import (
	"database/sql/driver"
	"time"
)

// Режимы переписывания текста сценария (services/address_form.go, agency/prompts/address-vy.md).
const (
	RewriteVy      = "vy"      // «ты» → «вы»
	RewriteNeutral = "neutral" // убрать род, «ты» оставить
)

// Источник переписанного текста.
const (
	RewriteSourceLLM  = "llm"
	RewriteSourceRule = "rule" // словарная замена кодом — «машинная правка»
)

// TextRewrite — кэш удачных ответов LLM: sha256(режим | версия промпта | модель | текст) →
// переписанный текст. Тот же текст во второй раз модель не ждёт.
type TextRewrite struct {
	Hash      string `gorm:"primaryKey"`
	Mode      string
	TextOut   string
	CreatedAt time.Time
}

// AIRewrite — пометка шага: тексты переписаны автоматически, HR должен проверить. TextVy и
// TextTy — что записал генератор: кабинет показывает бейдж «ИИ — проверьте», пока текст шага
// с ними совпадает (правка руками снимает бейдж). Fields — все переписанные поля шага (включая
// реплики веток и опросов), Source — rule, если хоть одно поле правил код, иначе llm.
type AIRewrite struct {
	Source string   `json:"source"`
	TextVy string   `json:"text_vy,omitempty"`
	TextTy string   `json:"text_ty,omitempty"`
	Fields []string `json:"fields"`
	At     string   `json:"at"`
}

func (a *AIRewrite) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	return jsonValue(*a)
}

func (a *AIRewrite) Scan(src any) error { return jsonScan(src, a) }
