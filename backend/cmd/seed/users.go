package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"main/domain"
	"main/repositories"
	"main/services"
)

// Демо-аккаунты кабинета (FEATURE_AUTH): go run ./cmd/seed -users.
//
//   - hr      — hr_lead «Анна Кадрова»;
//   - recruiter1, recruiter2 — рекрутеры с именами из карточек (два самых частых RecruiterName,
//     иначе «Рекрутер Первый/Второй (синтетика)»);
//   - manager — руководитель подразделения с ManagerName из карточек (самый частый, иначе
//     «Руководитель Бухгалтерия (синтетика)»).
//
// Пароль — из DEMO_PASSWORD_HR / _RECRUITER1 / _RECRUITER2 / _MANAGER; нет — новому аккаунту
// генерируется и печатается ОДИН раз в stdout, у существующего не меняется. В репозиторий
// пароли не пишутся. После — новичкам без рекрутера проставляется RecruiterID по совпадению
// RecruiterName с full_name рекрутера. Повторный запуск идемпотентен.

func genPassword() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("пароль: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// pick — i-е имя из частых, иначе запасное.
func pick(list []repositories.RecruiterNameCount, i int, fallback string) string {
	if i < len(list) {
		return list[i].Name
	}
	return fallback
}

func seedUsers(ctx context.Context, repos *repositories.Repos, svc *services.Services) {
	recNames, err := repos.Employees.RecruiterNames(ctx)
	if err != nil {
		log.Fatalf("сид -users: %v", err)
	}
	mgrNames, err := repos.Employees.ManagerNames(ctx)
	if err != nil {
		log.Fatalf("сид -users: %v", err)
	}
	mgr := pick(mgrNames, 0, "Руководитель Бухгалтерия (синтетика)")
	accounts := []struct {
		env string
		u   services.DemoUser
	}{
		{"DEMO_PASSWORD_HR", services.DemoUser{Login: "hr", FullName: "Анна Кадрова", Role: domain.RoleHRLead}},
		{"DEMO_PASSWORD_RECRUITER1", services.DemoUser{Login: "recruiter1", FullName: pick(recNames, 0, "Рекрутер Первый (синтетика)"), Role: domain.RoleRecruiter}},
		{"DEMO_PASSWORD_RECRUITER2", services.DemoUser{Login: "recruiter2", FullName: pick(recNames, 1, "Рекрутер Второй (синтетика)"), Role: domain.RoleRecruiter}},
		{"DEMO_PASSWORD_MANAGER", services.DemoUser{Login: "manager", FullName: mgr, Role: domain.RoleManager, ManagerName: &mgr}},
	}
	fmt.Println("Демо-аккаунты кабинета (FEATURE_AUTH=true):")
	for _, a := range accounts {
		d := a.u
		d.Password = os.Getenv(a.env)
		existing, err := repos.Users.ByLogin(ctx, d.Login)
		if err != nil {
			log.Fatalf("сид -users: %v", err)
		}
		generated := false
		if d.Password == "" && existing == nil {
			d.Password, generated = genPassword(), true
		}
		u, created, err := svc.Users.EnsureUser(ctx, d)
		if err != nil {
			log.Fatalf("сид -users %s: %v", d.Login, err)
		}
		pw := "(не менялся)"
		switch {
		case generated:
			pw = d.Password + "   ← сгенерирован, запишите: больше не покажется"
		case d.Password != "":
			pw = "(из " + a.env + ")"
		}
		state := "обновлён"
		if created {
			state = "создан"
		}
		fmt.Printf("  %-10s %-9s %-40s %s, пароль %s\n", u.Login, u.Role, u.FullName, state, pw)
	}
	n, err := svc.Users.LinkRecruitersByName(ctx)
	if err != nil {
		log.Fatalf("сид -users: закрепление: %v", err)
	}
	fmt.Printf("Закреплено новичков за рекрутерами по имени: %d\n", n)
}
