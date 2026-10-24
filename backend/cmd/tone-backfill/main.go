// Команда tone-backfill — тональность для уже существующих записей, без LLM (план итерации
// §3.7, агент E). Правило:
//   - текст ответа опроса или свободный вопрос: есть код F0–F9 → neg; иначе позитивные ключевые
//     слова → pos; иначе neu. Сигнал regex без текста (нет согласия) — тоже neg;
//   - синтетика seed-demo без текста («(синтетика)» в ФИО): первый вопрос опроса по коду и оценке,
//     как cmd/seed-demo/tone.go (F0–F9 → neg; 10-балльная ≥ 9, 5-балльная = 5, вариант 1 → pos;
//     иначе neu).
//
// Трогает только строки с sentiment IS NULL — повторный запуск ничего не меняет, разметку /flags
// не перезаписывает. Без -apply только считает, что изменится.
//
//	cd backend && go run ./cmd/tone-backfill            # сухой прогон: счётчики
//	cd backend && go run ./cmd/tone-backfill -apply     # записать (одна транзакция)
//
// DATABASE_URL — из окружения или backend/.env.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// negCodes — массив кодов для jsonb ?| (F10 тон не задаёт).
const negCodes = `array['F0','F1','F2','F3','F4','F5','F6','F7','F8','F9']`

// posWords — позитивные ключевые слова (Postgres ~*), без «не хорошо», «не нравится».
const posWords = `'(спасиб|отличн|нравит|понравил|хорошо|супер|класс|здорово|помогл|помогает|рад[аы]?\M|всё ок|все ок|всё норм)'`
const posNegated = `'\mне (очень |слишком )?(хорошо|нравит|понравил|помогл|помогает|рад)'`

// Statements — UPDATE по порядку. Каждый — с WHERE sentiment IS NULL.
var Statements = []struct{ Name, SQL string }{
	{"ответы опросов с текстом", `
UPDATE survey_answers SET sentiment = CASE
  WHEN COALESCE(flags, '[]'::jsonb) ?| ` + negCodes + ` THEN 'neg'
  WHEN text ~* ` + posWords + ` AND NOT text ~* ` + posNegated + ` THEN 'pos'
  ELSE 'neu' END
WHERE sentiment IS NULL AND btrim(text) <> ''`},
	{"свободные вопросы", `
UPDATE bot_questions SET sentiment = CASE
  WHEN COALESCE(flags, '[]'::jsonb) ?| ` + negCodes + ` THEN 'neg'
  WHEN question ~* ` + posWords + ` AND NOT question ~* ` + posNegated + ` THEN 'pos'
  ELSE 'neu' END
WHERE sentiment IS NULL AND (btrim(question) <> '' OR COALESCE(flags, '[]'::jsonb) ?| ` + negCodes + `)`},
	{"синтетика seed-demo без текста", `
UPDATE survey_answers a SET sentiment = CASE
  WHEN COALESCE(a.flags, '[]'::jsonb) ?| ` + negCodes + ` THEN 'neg'
  WHEN (a.scale = '1-10' AND a.value >= 9) OR (a.scale = '1-5' AND a.value = 5) OR (a.scale = 'choice' AND a.value = 1) THEN 'pos'
  ELSE 'neu' END
FROM employees e
WHERE e.id = a.employee_id AND e.full_name LIKE '%(синтетика)%' AND a.question_no = 1
  AND a.sentiment IS NULL AND btrim(a.text) = '' AND NOT a.skipped AND a.value IS NOT NULL`},
}

// Run — все UPDATE в одной транзакции; apply=false — откат, вернутся только счётчики.
func Run(db *gorm.DB, apply bool) (map[string]int64, error) {
	out := map[string]int64{}
	err := db.Transaction(func(tx *gorm.DB) error {
		for _, st := range Statements {
			res := tx.Exec(st.SQL)
			if res.Error != nil {
				return fmt.Errorf("%s: %w", st.Name, res.Error)
			}
			out[st.Name] = res.RowsAffected
		}
		if !apply {
			return errDryRun
		}
		return nil
	})
	if err == errDryRun {
		err = nil
	}
	return out, err
}

var errDryRun = fmt.Errorf("dry run")

func main() {
	apply := flag.Bool("apply", false, "записать (без флага — сухой прогон с откатом)")
	flag.Parse()
	_ = godotenv.Load()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL пуст")
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	counts, err := Run(db, *apply)
	if err != nil {
		log.Fatalf("tone-backfill: %v", err)
	}
	mode := "сухой прогон, ничего не записано"
	if *apply {
		mode = "записано"
	}
	for _, st := range Statements {
		log.Printf("tone-backfill: %-32s %d строк", st.Name, counts[st.Name])
	}
	log.Printf("tone-backfill: %s", mode)
}
