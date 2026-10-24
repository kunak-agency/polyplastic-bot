package domain

import (
	"time"

	"github.com/google/uuid"
)

// Виды напоминаний новичку (Nudge.Kind).
const (
	NudgeAuto   = "auto"   // планировщик: 4 ч / 24 ч без ответа (FEATURE_NUDGES_DEFAULT)
	NudgeManual = "manual" // кнопка «Напомнить» в карточке
)

// Nudge — журнал напоминаний новичку о неотвеченном вопросе (траектория: «🔔 Напоминание 2/2,
// вручную/авто»). Не путать с ManagerNudge — это push руководителю.
type Nudge struct {
	UUIDModel
	EmployeeID uuid.UUID `gorm:"type:uuid"`
	// EventID — событие-вопрос, о котором напомнили; nil — общее напоминание («у вас 2 вопроса»).
	EventID *uuid.UUID `gorm:"type:uuid"`
	Kind    string     // auto | manual
	N       int        // номер напоминания по этому вопросу, с единицы
	SentAt  time.Time
	// CreatedBy — пользователь кабинета (User.ID) для manual; nil — авто.
	CreatedBy *uuid.UUID `gorm:"type:uuid"`
	// MessageID — mid ушедшего напоминания; пусто — не ушло.
	MessageID string
}

// Holiday — нерабочий праздничный день (производственный календарь РФ, правится в Настройках).
// Шаг, попавший на праздник, переносится на следующий рабочий день (FEATURE_TZ, волна 2).
type Holiday struct {
	Date      time.Time `gorm:"type:date;primaryKey"`
	Title     string
	CreatedAt time.Time
}

// Тональность текста новичка (SurveyAnswer.Sentiment, BotQuestion.Sentiment). nil — не размечен.
const (
	SentimentPos = "pos"
	SentimentNeu = "neu"
	SentimentNeg = "neg"
)
