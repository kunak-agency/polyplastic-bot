package services

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"main/domain"
	"main/pkg/errs"
)

// «Кто есть кто» при FEATURE_AUTH: исполнитель — пользователь (assignee_id), «Взять» — на
// вошедшего, подпись ответа, авторы заметок и updated_by — из сессии. Без входа — как раньше.
// Интеграционные: Postgres + подделка MAX.

// addUser — пользователь кабинета (синтетика).
func addUser(t *testing.T, e *env, login, name, role string, active bool) *domain.User {
	t.Helper()
	u := &domain.User{Login: &login, PasswordHash: "x", FullName: name, Role: role, Active: active}
	if err := e.repos.Users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTicketAssignByUserAndTakeSelf(t *testing.T) {
	e, _, c := askEnv(t, time.Second)
	hr := addUser(t, e, "hr", "Анна Кадрова", domain.RoleHRLead, true)
	r1 := addUser(t, e, "recruiter1", "Мария Р.", domain.RoleRecruiter, true)
	r2 := addUser(t, e, "recruiter2", "Ольга Р.", domain.RoleRecruiter, true)
	mgr := addUser(t, e, "manager", "Смирнов А.", domain.RoleManager, true)
	off := addUser(t, e, "old", "Ушедший Р.", domain.RoleRecruiter, false)

	e.press(payHuman)
	tk := e.onlyTicket(t, c.Employee.ID)
	asHR := WithViewer(context.Background(), hr)
	asR2 := WithViewer(context.Background(), r2)

	// Не recruiter/hr_lead и неактивный — 422.
	for _, u := range []*domain.User{mgr, off} {
		id := u.ID
		if _, err := e.svc.Tickets.Assign(asHR, tk.ID, AssignInput{AssigneeID: &id}); err == nil {
			t.Fatalf("назначение на %s", u.Role)
		} else if typ, _ := appCode(err); typ != errs.ErrTypeValidation {
			t.Fatalf("%v", err)
		}
	}

	// hr_lead назначает recruiter1 по id: assignee_id и копия имени; автор записи — hr_lead.
	id1 := r1.ID
	v, err := e.svc.Tickets.Assign(asHR, tk.ID, AssignInput{AssigneeID: &id1})
	if err != nil || v.Ticket.AssigneeID == nil || *v.Ticket.AssigneeID != r1.ID || *v.Ticket.AssigneeName != "Мария Р." {
		t.Fatalf("assign по id: %+v %v", v, err)
	}
	if last := v.Messages[len(v.Messages)-1]; last.Author == nil || *last.Author != "Анна Кадрова" {
		t.Fatalf("автор назначения %+v", last)
	}

	// Список исполнителей: активные recruiter и hr_lead.
	list, err := e.svc.Tickets.Assignees(asHR)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, u := range list {
		names = append(names, u.FullName)
	}
	if !slices.Equal(names, []string{"Анна Кадрова", "Мария Р.", "Ольга Р."}) {
		t.Fatalf("исполнители %v", names)
	}

	// «Взять» без тела у чужого — переназначение, нужен комментарий.
	if _, err := e.svc.Tickets.Assign(asR2, tk.ID, AssignInput{}); err == nil {
		t.Fatal("взять чужое без комментария")
	}
	v, err = e.svc.Tickets.Assign(asR2, tk.ID, AssignInput{Comment: "Мария на выезде"})
	if err != nil || v.Ticket.AssigneeID == nil || *v.Ticket.AssigneeID != r2.ID || *v.Ticket.AssigneeName != "Ольга Р." {
		t.Fatalf("взять на себя: %+v %v", v, err)
	}
	// Повторное «Взять» своим — не переназначение.
	if _, err := e.svc.Tickets.Assign(asR2, tk.ID, AssignInput{}); err != nil {
		t.Fatalf("повторно своё: %v", err)
	}
	// Имя, которое прислал старый кабинет, при входе игнорируется в пользу сессии.
	if v, err := e.svc.Tickets.Assign(asR2, tk.ID, AssignInput{Name: "Ольга Р."}); err != nil || *v.Ticket.AssigneeID != r2.ID {
		t.Fatalf("по имени вошедшего: %+v %v", v, err)
	}
}

// Подпись ответа и авторы — вошедший пользователь, даже если исполнитель другой и кабинет
// помнит «Анна К.».
func TestTicketReplySignedBySession(t *testing.T) {
	e, _, c := askEnv(t, time.Second)
	e.withChats()
	r1 := addUser(t, e, "recruiter1", "Мария Р.", domain.RoleRecruiter, true)
	r2 := addUser(t, e, "recruiter2", "Ольга Р.", domain.RoleRecruiter, true)
	e.press(payHuman)
	tk := e.onlyTicket(t, c.Employee.ID)

	asR2 := WithViewer(context.Background(), r2)
	id1 := r1.ID
	if _, err := e.svc.Tickets.Assign(asR2, tk.ID, AssignInput{AssigneeID: &id1, Name: "Анна К."}); err != nil {
		t.Fatal(err)
	}
	e.fake.Reset()
	v, err := e.svc.Tickets.Reply(asR2, tk.ID, "Пропуск будет завтра.", false)
	if err != nil {
		t.Fatal(err)
	}
	r := lastSent(t, e.fake)
	if !strings.HasPrefix(r.Text, "**Ольга Р., HR** (по обращению #1001):\n") || strings.Contains(r.Text, "Анна К.") {
		t.Fatalf("подпись %q", r.Text)
	}
	if last := v.Messages[len(v.Messages)-1]; last.Author == nil || *last.Author != "Ольга Р." {
		t.Fatalf("автор ответа %+v", last)
	}
	if v, err := e.svc.Tickets.Note(asR2, tk.ID, "Позвонить завтра"); err != nil || *v.Messages[len(v.Messages)-1].Author != "Ольга Р." {
		t.Fatalf("автор заметки %+v %v", v, err)
	}
	v, err = e.svc.Tickets.Resolve(asR2, tk.ID, "Пропуск выдан")
	if err != nil {
		t.Fatal(err)
	}
	var done *domain.TicketMessage
	for i := range v.Messages {
		if strings.HasPrefix(v.Messages[i].Text, DoneNotePrefix) {
			done = &v.Messages[i]
		}
	}
	if done == nil || done.Author == nil || *done.Author != "Ольга Р." {
		t.Fatalf("автор «что сделано» %+v", done)
	}
}

// Без входа — прежнее поведение: исполнитель по имени, «Взять» без имени — 422, подпись —
// исполнитель.
func TestTicketAssignWithoutAuth(t *testing.T) {
	e, _, c := askEnv(t, time.Second)
	e.withChats()
	ctx := context.Background()
	e.press(payHuman)
	tk := e.onlyTicket(t, c.Employee.ID)
	if _, err := e.svc.Tickets.Assign(ctx, tk.ID, AssignInput{}); err == nil {
		t.Fatal("без входа и без имени")
	} else if typ, _ := appCode(err); typ != errs.ErrTypeValidation {
		t.Fatalf("%v", err)
	}
	v, err := e.svc.Tickets.Assign(ctx, tk.ID, AssignInput{Name: "Анна К."})
	if err != nil || v.Ticket.AssigneeID != nil || *v.Ticket.AssigneeName != "Анна К." {
		t.Fatalf("%+v %v", v, err)
	}
	e.fake.Reset()
	if _, err := e.svc.Tickets.Reply(ctx, tk.ID, "Готово", false); err != nil {
		t.Fatal(err)
	}
	if r := lastSent(t, e.fake); !strings.HasPrefix(r.Text, "**Анна К., HR**") {
		t.Fatalf("подпись %q", r.Text)
	}
}

// updated_by медиатеки — из сессии; без входа — имя из запроса.
func TestMediaUpdatedBySession(t *testing.T) {
	e := newMediaEnv(t)
	it := e.create(t, "Карта", "", "map.png")
	r2 := &domain.User{FullName: "Ольга Р.", Role: domain.RoleRecruiter, Active: true}
	asR2 := WithViewer(context.Background(), r2)
	r, err := e.media.Rename(asR2, it.Media.ID, "Карта ГТП", "Анна К.")
	if err != nil || r.Item.Media.UpdatedBy != "Ольга Р." {
		t.Fatalf("при входе %+v %v", r, err)
	}
	r, err = e.media.Rename(context.Background(), it.Media.ID, "Карта проезда", "Анна К.")
	if err != nil || r.Item.Media.UpdatedBy != "Анна К." {
		t.Fatalf("без входа %+v %v", r, err)
	}
	put, err := e.media.PutFile(asR2, it.Media.ID, MediaUpload{Site: domain.SiteGTP, Filename: "m.png", Mime: "image/png", Kind: domain.MediaKindImage, By: "Анна К.", Data: append([]byte("m"), pngBytes...)})
	if err != nil || put.Media.UpdatedBy != "Ольга Р." {
		t.Fatalf("put %+v %v", put, err)
	}
}

// Получатели дайджеста: env + активные hr_lead с max_user_id, без дублей.
func TestDigestRecipientsAddHRLeads(t *testing.T) {
	e := newEnv(t, time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC))
	mk := func(login, role string, active bool, mid *int64) {
		u := addUser(t, e, login, login, role, active)
		u.MaxUserID = mid
		if err := e.repos.Users.Save(context.Background(), u); err != nil {
			t.Fatal(err)
		}
	}
	p := func(v int64) *int64 { return &v }
	mk("hr1", domain.RoleHRLead, true, p(20))  // дубль env
	mk("hr2", domain.RoleHRLead, true, p(30))  // новый
	mk("hr3", domain.RoleHRLead, false, p(40)) // выключен
	mk("hr4", domain.RoleHRLead, true, nil)    // без MAX
	mk("rec", domain.RoleRecruiter, true, p(50))
	d := e.svc.Digest()
	d.opts.UserIDs = []int64{10, 20, 0, 10}
	if got := d.recipients(context.Background()); !slices.Equal(got, []int64{10, 20, 30}) {
		t.Fatalf("получатели %v", got)
	}
}
