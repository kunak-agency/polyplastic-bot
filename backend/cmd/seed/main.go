// Команда seed — демо-данные для кабинета: два синтетических сценария («Завод GTP», «Офис
// GrPP») и три синтетических новичка. Повторный запуск ничего не дублирует: сценарий или
// новичок с тем же именем пропускается.
//
//	cd backend && go run ./cmd/seed     (или make seed)
//	cd backend && go run ./cmd/seed -reset   (или make seed-reset) — чистая пересборка
//	cd backend && go run ./cmd/seed -users   — демо-аккаунты кабинета (users.go)
//	cd backend && go run ./cmd/seed -people  — руководители и наставники в справочник людей (users.go)
//
// -reset — только APP_ENV=development: удаляет всех новичков (вместе со смоук-данными агентов),
// сценарии со «Смоук-шагом» и правками, события, вопросы и обращения, затем заливает сид заново.
// marker long polling не трогает. Перед удалением показывает, что сотрёт, и ждёт «yes» (или -yes).
//
// Схему мигрирует сама (AutoMigrate идемпотентен): сид обычно запускают на пустой базе, до
// первого старта API. Реальных ПДн здесь нет: ФИО помечены «(синтетика)», телефоны +7 000 …,
// аккаунты MAX — заведомо несуществующие id 9000000xx.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"main/config"
	"main/domain"
	"main/externalApi"
	"main/pkg/taxonomy"
	"main/repositories"
	"main/services"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

func main() {
	reset := flag.Bool("reset", false, "очистить демо-данные и залить сид заново (только APP_ENV=development)")
	yes := flag.Bool("yes", false, "не спрашивать подтверждение для -reset")
	users := flag.Bool("users", false, "только демо-аккаунты кабинета (hr, recruiter1, recruiter2, manager) и закрепление новичков за рекрутерами")
	people := flag.Bool("people", false, "только перенос руководителей и наставников из строк карточек в справочник людей (manager_id, mentor_id); идемпотентно")
	flag.Parse()
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("конфигурация: %v", err)
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL пуст — сиду некуда писать (make env, make up)")
	}
	repos, err := repositories.InitRepos(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("база: %v", err)
	}
	if err := repos.MigrateDB(); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if *reset {
		resetDemo(repos, cfg, *yes)
	}
	// Без MAX: сид ничего не отправляет, только пишет в базу.
	tax, err := taxonomy.LoadDefault(cfg.TaxonomyPath)
	if err != nil {
		log.Fatalf("%v", err)
	}
	svc := services.NewServices(repos, &externalApi.ExternalAPI{}, services.Options{Bot: cfg.Bot, Taxonomy: tax})
	ctx := context.Background()
	if *users {
		seedUsers(ctx, repos, svc)
		return
	}
	if *people {
		seedPeople(ctx, svc)
		return
	}

	gtp := scenario(ctx, repos, svc, "Завод GTP", domain.SiteGTP)
	grpp := scenario(ctx, repos, svc, "Офис GrPP", domain.SiteGrPP)

	now := time.Now()
	site := domain.SiteOf(domain.SiteGTP)
	today := services.StartOfDay(now, site)
	workday := func(offset int) time.Time {
		d := today.AddDate(0, 0, offset)
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, 1)
		}
		return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	}

	// 1. Подключился и дал согласие, выход через три дня: машина времени шлёт ему шаги.
	if e := newcomer(ctx, repos, svc, services.NewcomerInput{
		FullName: "Синтетов Пётр (синтетика)", Phone: "+7 000 000-00-01", Site: domain.SiteGTP,
		Department: "Цех экструзии", Position: "Оператор линии", ManagerName: "Руководитель Цеха (синтетика)",
		MentorName: "Наставник Опытный (синтетика)", RecruiterName: "Рекрутер Первый (синтетика)",
		StartDate: workday(3), Shift: strp("1"), ScenarioID: gtp,
	}); e != nil {
		connect(ctx, repos, e, 900000001, domain.AddressTy, domain.ConsentGiven)
	}

	// 2. Вышла пять дней назад, низкая оценка первого дня → жёлтая зона.
	if e := newcomer(ctx, repos, svc, services.NewcomerInput{
		FullName: "Демова Анна (синтетика)", Phone: "+7 000 000-00-02", Site: domain.SiteGrPP,
		Department: "Отдел закупок", Position: "Специалист по закупкам", ManagerName: "Руководитель Отдела (синтетика)",
		MentorName: "Наставник Внимательный (синтетика)", RecruiterName: "Рекрутер Второй (синтетика)",
		StartDate: workday(-5), ScenarioID: grpp,
	}); e != nil {
		connect(ctx, repos, e, 900000002, domain.AddressVy, domain.ConsentGiven)
		history(ctx, repos, e, now)
	}

	// 3. Приглашён, ещё не открыл ссылку.
	newcomer(ctx, repos, svc, services.NewcomerInput{
		FullName: "Примеров Иван (синтетика)", Phone: "+7 000 000-00-03", Site: domain.SiteGTP,
		Department: "Склад сырья", Position: "Кладовщик", ManagerName: "Руководитель Склада (синтетика)",
		MentorName: "Наставник Терпеливый (синтетика)", RecruiterName: "Рекрутер Первый (синтетика)",
		StartDate: workday(10), Shift: strp("2"), ScenarioID: gtp,
	})
	log.Print("сид: готово")
}

// scenario — сценарий по таблице 2.3, опубликованный версией 1. Уже есть — берём его id.
func scenario(ctx context.Context, repos *repositories.Repos, svc *services.Services, name, site string) uuid.UUID {
	if sc, err := repos.Scenarios.ByName(ctx, name); err != nil {
		log.Fatalf("сценарий %s: %v", name, err)
	} else if sc != nil {
		log.Printf("сид: сценарий «%s» уже есть — пропускаю", name)
		return sc.ID
	}
	d, err := svc.Scenarios.Create(ctx, name, []string{site}, demoSteps(site))
	if err != nil {
		log.Fatalf("сценарий %s: %v", name, err)
	}
	if _, err := svc.Scenarios.Publish(ctx, d.Scenario.ID, "Демо-сценарий по таблице 2.3 UX-спецификации", services.ApplyNew); err != nil {
		log.Fatalf("публикация %s: %v", name, err)
	}
	log.Printf("сид: сценарий «%s», шагов %d, версия 1", name, len(d.Steps))
	return d.Scenario.ID
}

// newcomer — «Запустить адаптацию». Уже есть с таким ФИО — nil.
func newcomer(ctx context.Context, repos *repositories.Repos, svc *services.Services, in services.NewcomerInput) *domain.Employee {
	list, err := repos.Employees.List(ctx, repositories.EmployeeFilter{Q: in.FullName})
	if err != nil {
		log.Fatalf("новичок: %v", err)
	}
	if len(list) > 0 {
		log.Printf("сид: «%s» уже есть — пропускаю", in.FullName)
		return nil
	}
	c, err := svc.Newcomers.Create(ctx, in)
	if err != nil {
		log.Fatalf("новичок %s: %v", in.FullName, err)
	}
	log.Printf("сид: «%s», ссылка %s", in.FullName, c.InviteLink)
	return &c.Employee
}

// connect — как будто новичок открыл ссылку, выбрал форму обращения и ответил на согласие.
func connect(ctx context.Context, repos *repositories.Repos, e *domain.Employee, userID int64, form, consent string) {
	now := time.Now()
	e.MaxUserID, e.ChatID = &userID, &userID
	e.Connection, e.AddressForm, e.Consent, e.ConsentAt, e.LastActivityAt = domain.ConnConnected, &form, &consent, &now, &now
	if err := repos.Employees.Save(ctx, e); err != nil {
		log.Fatalf("подключение %s: %v", e.FullName, err)
	}
}

// history — прошедшие шаги отправлены, на кнопки ответили, итог первого дня — 2 из 5.
func history(ctx context.Context, repos *repositories.Repos, e *domain.Employee, now time.Time) {
	events, err := repos.Events.ByEmployee(ctx, e.ID)
	if err != nil {
		log.Fatalf("история: %v", err)
	}
	for i := range events {
		ev := &events[i]
		if ev.FireAt.After(now) {
			continue
		}
		at := ev.FireAt
		ev.Status, ev.SentAt, ev.MessageID = domain.EventSent, &at, "mid.seed."+ev.ID.String()[:8]
		if ev.Kind == domain.KindSurvey && ev.DayOffset == 0 {
			two := 2
			ev.Status, ev.Answer = domain.EventAnswered, strp("2/5 (Люди и общение)")
			ev.Survey = &domain.SurveyState{Q: 1, Screen: domain.ScreenQuestion, Results: []domain.SurveyResult{{Value: &two, Reason: "Люди и общение"}}}
			if err := repos.Events.UpsertAnswer(ctx, &domain.SurveyAnswer{
				EmployeeID: e.ID, EventID: ev.ID, QuestionNo: 1, Scale: domain.Scale5, Value: &two,
				Reason: "Люди и общение", Flags: domain.StringList{},
			}); err != nil {
				log.Fatalf("история: %v", err)
			}
			e.LastPulse = &two
			e.Risk, e.RiskReason = domain.RiskYellow, strp("оценка 2/5 в «Итог первого дня»")
		} else if ev.Kind == domain.KindSurvey {
			ev.Status, ev.Answer = domain.EventSkipped, strp("не отвечен")
		}
		if err := repos.Events.Save(ctx, ev); err != nil {
			log.Fatalf("история: %v", err)
		}
	}
	if err := repos.Employees.Save(ctx, e); err != nil {
		log.Fatalf("история: %v", err)
	}
}

func strp(s string) *string { return &s }

// resetDemo — чистая пересборка: только development, с подтверждением в выводе.
func resetDemo(repos *repositories.Repos, cfg config.Config, yes bool) {
	if cfg.AppEnv != config.EnvDevelopment {
		log.Fatalf("сид -reset: только при APP_ENV=development (сейчас %s)", cfg.AppEnv)
	}
	ctx := context.Background()
	counts, err := repos.DemoCounts(ctx)
	if err != nil {
		log.Fatalf("сид -reset: %v", err)
	}
	fmt.Printf("Сброс демо-данных в базе %s:\n", dbName(cfg.DatabaseURL))
	fmt.Printf("  новичков %d, сценариев %d, событий таймлайна %d, вопросов боту %d, обращений %d\n",
		counts["employees"], counts["scenarios"], counts["scheduled_events"], counts["bot_questions"], counts["tickets"])
	fmt.Println("  Всё это будет удалено и заменено сидом (2 сценария, 3 синтетических новичка). marker бота остаётся.")
	if !yes {
		fmt.Print("Продолжить? Введите yes: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Println("Отменено, база не тронута.")
			os.Exit(1)
		}
	}
	if err := repos.ResetDemo(ctx); err != nil {
		log.Fatalf("сид -reset: %v", err)
	}
	fmt.Println("Демо-данные удалены, заливаю сид…")
}

// dbName — хост и имя базы без пароля: что именно сотрём.
func dbName(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return "(DATABASE_URL)"
	}
	return u.Host + u.Path
}
