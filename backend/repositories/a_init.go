package repositories

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/pkg/logger"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Repos — доступ к БД. Репозитории добавляются полями и заполняются в InitRepos.
//
// Выключенное состояние — nil *Repos: метод с указательным получателем вызывается и на nil,
// поэтому вызывающему достаточно Enabled(). Правило пакета: любой метод Repos начинается с
// проверки Enabled(), иначе он разыменует nil и уронит процесс.
type Repos struct {
	db *gorm.DB

	Employees *EmployeeRepo
	Scenarios *ScenarioRepo
	Events    *EventRepo
	BotState  *BotStateRepo
	Questions *QuestionRepo
	Tickets   *TicketRepo
	Documents *DocumentRepo
	Media     *MediaRepo
	Rewrites  *TextRewriteRepo
}

// newRepos — репозитории поверх db (база или транзакция).
func newRepos(db *gorm.DB) *Repos {
	return &Repos{
		db:        db,
		Employees: &EmployeeRepo{db: db},
		Scenarios: &ScenarioRepo{db: db},
		Events:    &EventRepo{db: db},
		BotState:  &BotStateRepo{db: db},
		Questions: &QuestionRepo{db: db},
		Tickets:   &TicketRepo{db: db},
		Documents: &DocumentRepo{db: db},
		Media:     &MediaRepo{db: db},
		Rewrites:  &TextRewriteRepo{db: db},
	}
}

// Transaction — fn в одной транзакции: tx — те же репозитории поверх неё. Ошибка или паника fn
// откатывает всё.
func (r *Repos) Transaction(ctx context.Context, fn func(tx *Repos) error) error {
	if !r.Enabled() {
		return ErrNoDatabase
	}
	return r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error { return fn(newRepos(db)) })
}

// Truncate — очистить доменные таблицы. Только для тестов и сида на тестовой базе.
func (r *Repos) Truncate(ctx context.Context) error {
	if !r.Enabled() {
		return ErrNoDatabase
	}
	if err := r.db.WithContext(ctx).Exec(`TRUNCATE employees, invite_tokens, human_requests, scenarios,
		scenario_versions, scenario_steps, scheduled_events, survey_answers, marker_states, bot_questions,
		tickets, ticket_messages, bot_chats, text_rewrites`).Error; err != nil {
		return err
	}
	return r.restartTicketSeq(ctx)
}

// ResetDemo — чистая пересборка демо-данных (cmd/seed -reset, только development): всё, кроме
// marker long polling — иначе бот перечитал бы очередь MAX с начала.
func (r *Repos) ResetDemo(ctx context.Context) error {
	if !r.Enabled() {
		return ErrNoDatabase
	}
	if err := r.db.WithContext(ctx).Exec(`TRUNCATE employees, invite_tokens, human_requests, scenarios,
		scenario_versions, scenario_steps, scheduled_events, survey_answers, bot_questions,
		tickets, ticket_messages`).Error; err != nil {
		return err
	}
	return r.restartTicketSeq(ctx)
}

// DemoCounts — сколько записей в демо-таблицах: сид показывает это перед сбросом.
func (r *Repos) DemoCounts(ctx context.Context) (map[string]int64, error) {
	if !r.Enabled() {
		return nil, ErrNoDatabase
	}
	out := map[string]int64{}
	for _, t := range []string{"employees", "scenarios", "scheduled_events", "bot_questions", "tickets"} {
		var n int64
		if err := r.db.WithContext(ctx).Table(t).Count(&n).Error; err != nil {
			return nil, err
		}
		out[t] = n
	}
	return out, nil
}

func (r *Repos) restartTicketSeq(ctx context.Context) error {
	return r.db.WithContext(ctx).Exec(fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH %d", ticketSeq, domain.TicketNumberStart)).Error
}

// ignoreNotFound — промах First как (nil, nil): «такого нет» — штатный ответ, не ошибка.
func ignoreNotFound[T any](v *T, err error) (*T, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// ErrNoDatabase — обращение к БД, когда база выключена. До клиента доезжать не должна: сервис,
// которому нужна запись, обязан спросить Enabled() раньше.
var ErrNoDatabase = errs.NewInternalError("Сервис запущен без базы данных")

// Enabled — включены ли репозитории.
func (r *Repos) Enabled() bool { return r != nil && r.db != nil }

// gormLogger — ParameterizedQueries: значения в лог не подставляются, иначе ошибка БД уносила
// бы в лог сообщения новичков и их данные. IgnoreRecordNotFoundError: промах First — штатный
// ответ «такого нет», а не сбой.
var gormLogger = gormlogger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), gormlogger.Config{
	SlowThreshold:             200 * time.Millisecond,
	LogLevel:                  gormlogger.Warn,
	Colorful:                  true,
	ParameterizedQueries:      true,
	IgnoreRecordNotFoundError: true,
})

// InitRepos — подключение к БД. Пустой dsn → выключенные репозитории (nil) и WARN: каркас и
// бот поднимаются без базы. Заданный, но нерабочий dsn — ошибка: это опечатка в конфиге или
// не поднятый `make up`, и молча работать без базы тогда хуже, чем упасть.
func InitRepos(dsn string) (*Repos, error) {
	if dsn == "" {
		logger.Warn("DATABASE_URL пуст — работаем без базы: миграция не выполняется, health отвечает db: down")
		return nil, nil
	}
	// DisableForeignKeyConstraintWhenMigrating: никаких FK в БД — все связи резолвим в коде.
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
		Logger:                                   gormLogger,
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetConnMaxLifetime(10 * time.Minute)
	logger.Info("database connected")
	return newRepos(db), nil
}

// Ping — база отвечает. Для health: выключенная база — ErrNoDatabase.
func (r *Repos) Ping(ctx context.Context) error {
	if !r.Enabled() {
		return ErrNoDatabase
	}
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// MigrateDB — ЕДИНСТВЕННАЯ точка миграции схемы. Без базы мигрировать нечего, и это не отказ:
// про пустой DATABASE_URL уже сказано WARN в InitRepos.
func (r *Repos) MigrateDB() error {
	if !r.Enabled() {
		return nil
	}
	if err := migrateTicketSeq(r.db); err != nil {
		return err
	}
	return r.db.AutoMigrate(domain.AllModels()...)
}

// IsUniqueViolation — нарушение уникального индекса Postgres (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
