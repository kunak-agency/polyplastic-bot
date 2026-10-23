package services

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Медиатека (api_contract.md, «v4»): файлы по площадкам, «используется в N», переименование с
// автозаменой ссылок, заглушки импорта, вложения для бота с кэшем токена MAX. Postgres —
// TEST_DATABASE_URL, MAX — подделка загрузки.

// fakeUploader — подделка POST /uploads MAX: считает загрузки, может отказать.
type fakeUploader struct {
	mu    sync.Mutex
	calls []string
	fail  error
}

func (f *fakeUploader) Upload(_ context.Context, kind max.UploadKind, r io.Reader, name string, _ int64) (string, error) {
	_, _ = io.Copy(io.Discard, r)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.calls = append(f.calls, string(kind)+":"+name)
	return "tok-" + string(kind) + "-" + name + "-" + strings.Repeat("i", len(f.calls)), nil
}

func (f *fakeUploader) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type mediaEnv struct {
	repos *repositories.Repos
	svc   *Services
	media *MediaService
	up    *fakeUploader
	clock *time.Time
}

func newMediaEnv(t *testing.T) *mediaEnv {
	t.Helper()
	e := newEnv(t, time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC))
	if err := e.repos.Media.TruncateMedia(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	up := &fakeUploader{}
	m := newMediaService(e.repos, up, MediaOptions{}, func() time.Time { return clock })
	return &mediaEnv{repos: e.repos, svc: e.svc, media: m, up: up, clock: &clock}
}

var pngBytes = []byte("\x89PNG\r\n\x1a\nсинтетика")

func (e *mediaEnv) create(t *testing.T, name, site, filename string) MediaItem {
	t.Helper()
	kind, mime := domain.MediaKindImage, "image/png"
	if strings.HasSuffix(filename, ".pdf") {
		kind, mime = domain.MediaKindFile, "application/pdf"
	}
	it, err := e.media.Create(context.Background(), MediaUpload{Name: name, Site: site, Filename: filename, Mime: mime, Kind: kind, Data: append([]byte(filename), pngBytes...)})
	if err != nil {
		t.Fatal(err)
	}
	return *it
}

func (e *mediaEnv) put(t *testing.T, id uuid.UUID, site, filename string) *MediaItem {
	t.Helper()
	kind, mime := domain.MediaKindImage, "image/png"
	if strings.HasSuffix(filename, ".pdf") {
		kind, mime = domain.MediaKindFile, "application/pdf"
	}
	it, err := e.media.PutFile(context.Background(), id, MediaUpload{Site: site, Filename: filename, Mime: mime, Kind: kind, Data: append([]byte(filename+site), pngBytes...)})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func ref(name string) domain.MediaRef { return domain.MediaRef{Name: name} }

func TestMediaRepoFilesAndStatus(t *testing.T) {
	e := newMediaEnv(t)
	ctx := context.Background()
	it := e.create(t, "Карта ГТП", "", "map.png")
	if it.Status != domain.MediaReady || len(it.Files) != 1 || it.Files[0].Site != "" || it.Files[0].SHA256 == "" {
		t.Fatalf("создание: %+v", it)
	}
	// Тот же ключ имени в другом регистре — 409.
	_, err := e.media.Create(ctx, MediaUpload{Name: "карта  гтп", Filename: "x.png", Mime: "image/png", Kind: domain.MediaKindImage, Data: pngBytes})
	var ae *errs.Error
	if !errors.As(err, &ae) || ae.Type != errs.ErrTypeConflict {
		t.Fatalf("дубль имени: %v", err)
	}
	got := e.put(t, it.Media.ID, domain.SiteGTP, "map-gtp.png")
	got = e.put(t, it.Media.ID, domain.SiteGTP, "map-gtp-v2.png") // замена, не второй файл
	if len(got.Files) != 2 {
		t.Fatalf("файлы: %+v", got.Files)
	}
	sorted := got.SortedFiles()
	if sorted[0].Site != "" || sorted[1].Filename != "map-gtp-v2.png" {
		t.Fatalf("порядок/замена: %+v", sorted)
	}
	// Документ в запись с картинками — 422.
	_, err = e.media.PutFile(ctx, it.Media.ID, MediaUpload{Site: domain.SiteGrPP, Filename: "a.pdf", Mime: "application/pdf", Kind: domain.MediaKindFile, Data: []byte("%PDF-")})
	if !errors.As(err, &ae) || ae.Type != errs.ErrTypeValidation {
		t.Fatalf("вид файла: %v", err)
	}
	f, data, err := e.media.Content(ctx, it.Media.ID, domain.SiteGTP)
	if err != nil || f.Filename != "map-gtp-v2.png" || !strings.HasPrefix(string(data), "map-gtp-v2.png") {
		t.Fatalf("content GTP: %v %v", f, err)
	}
	f, _, _ = e.media.Content(ctx, it.Media.ID, domain.SiteLuzhniki)
	if f.Site != "" {
		t.Fatalf("content Лужники → общий: %+v", f)
	}
	if _, err := e.media.DeleteFile(ctx, it.Media.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.media.Content(ctx, it.Media.ID, domain.SiteLuzhniki); err == nil {
		t.Fatal("Лужники без общего файла — 404")
	}
	missing, _ := e.media.List(ctx, domain.MediaMissing)
	if len(missing) != 0 {
		t.Fatalf("missing: %+v", missing)
	}
}

// usedSteps — черновик: шаг с картинкой по имени, шаг с веткой (сообщение с картинкой по id и
// send_file по имени), шаг без медиа.
func usedSteps(mapID uuid.UUID) []domain.ScenarioStep {
	return []domain.ScenarioStep{
		{DayOffset: 0, Time: "10:00", Kind: domain.KindText, Title: "Карта", TextVy: "Вот карта", Media: domain.MediaRefList{ref("карта гтп")}},
		{DayOffset: 0, Time: "11:00", Kind: domain.KindText, Title: "Вопрос", TextVy: "Нужна карта?", Buttons: domain.ButtonList{
			{Label: "Да", Branch: &domain.Branch{Messages: []domain.BranchMessage{{TextVy: "Держите", Media: domain.MediaRefList{{MediaID: &mapID, Name: "Карта ГТП"}}}}}},
			{Label: "Файлом", Branch: &domain.Branch{Actions: []domain.Action{{Type: "send_file", Media: "Памятка PDF"}}}},
			{Label: "Нет"},
		}},
		{DayOffset: 1, Time: "10:00", Kind: domain.KindText, Title: "Пусто", TextVy: "Без медиа"},
	}
}

func TestMediaUsedInRenameDelete(t *testing.T) {
	e := newMediaEnv(t)
	ctx := context.Background()
	mp := e.create(t, "Карта ГТП", "", "map.png")
	memo := e.create(t, "Памятка PDF", "", "memo.pdf")
	free := e.create(t, "Лишняя", "", "free.png")

	d, err := e.svc.Scenarios.Create(ctx, "Медиа (синтетика)", []string{domain.SiteGTP}, usedSteps(mp.Media.ID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Scenarios.Publish(ctx, d.Scenario.ID, "v1", ApplyNew); err != nil {
		t.Fatal(err)
	}
	used := func() map[string]int {
		list, err := e.media.List(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int{}
		for _, it := range list {
			out[it.Media.Name] = it.UsedIn
		}
		return out
	}
	// Карта: шаг «Карта» + ветка «Да»; черновик и версия совпадают — не удваиваются.
	if u := used(); u["Карта ГТП"] != 2 || u["Памятка PDF"] != 1 || u["Лишняя"] != 0 {
		t.Fatalf("used_in: %v", u)
	}

	// Удалить используемую — 409, свободную — можно.
	var ae *errs.Error
	if err := e.media.Delete(ctx, mp.Media.ID); !errors.As(err, &ae) || ae.Type != errs.ErrTypeConflict {
		t.Fatalf("удаление используемой: %v", err)
	}
	if err := e.media.Delete(ctx, free.Media.ID); err != nil {
		t.Fatal(err)
	}

	// Переименование: обе ссылки черновика (по имени и по id) + send_file.
	r, err := e.media.Rename(ctx, mp.Media.ID, "Схема проезда", "Анна HR (синтетика)")
	if err != nil {
		t.Fatal(err)
	}
	if r.Updated != 2 || r.Item.Media.Name != "Схема проезда" || !r.Item.Media.Aliases.Has("карта гтп") {
		t.Fatalf("rename: %+v", r)
	}
	r2, err := e.media.Rename(ctx, memo.Media.ID, "Памятка новичка", "")
	if err != nil || r2.Updated != 1 {
		t.Fatalf("rename send_file: %+v %v", r2, err)
	}
	steps, _ := e.repos.Scenarios.Steps(ctx, d.Scenario.ID)
	if s := steps[0].Media[0]; s.Name != "Схема проезда" || s.MediaID == nil || *s.MediaID != mp.Media.ID {
		t.Fatalf("ссылка шага: %+v", s)
	}
	if a := steps[1].Buttons[1].Branch.Actions[0]; a.Media != "Памятка новичка" {
		t.Fatalf("send_file: %+v", a)
	}
	// Опубликованная версия ссылается на старое имя — запись всё равно считается и находится.
	if u := used(); u["Схема проезда"] != 2 || u["Памятка новичка"] != 1 {
		t.Fatalf("used_in после переименования: %v", u)
	}
	atts, warns := e.media.Attachments(ctx, []domain.MediaRef{ref("Карта ГТП")}, domain.SiteGTP)
	if len(atts) != 1 || len(warns) != 0 {
		t.Fatalf("по старому имени: %v %v", atts, warns)
	}
	// Имя занято другой записью — 409; вернуть своё прежнее имя — можно.
	if _, err := e.media.Rename(ctx, mp.Media.ID, "памятка новичка", ""); !errors.As(err, &ae) || ae.Type != errs.ErrTypeConflict {
		t.Fatalf("занятое имя: %v", err)
	}
	if r3, err := e.media.Rename(ctx, mp.Media.ID, "Карта ГТП", ""); err != nil || r3.Item.Media.Aliases.Has("карта гтп") {
		t.Fatalf("возврат имени: %+v %v", r3, err)
	}
}

func TestMediaEnsure(t *testing.T) {
	e := newMediaEnv(t)
	ctx := context.Background()
	e.create(t, "Изображение 1", "", "1.png")
	r, err := e.media.Ensure(ctx, []string{"Изображение 1", "изображение 14", "Изображение  14", "Памятка PDF", " "}, "", "импорт")
	if err != nil {
		t.Fatal(err)
	}
	if r.Created != 2 || len(r.Items) != 3 {
		t.Fatalf("ensure: %+v", r)
	}
	if r.Items[1].Status != domain.MediaMissing || r.Items[1].Media.Kind != domain.MediaKindImage || r.Items[2].Media.Kind != domain.MediaKindFile {
		t.Fatalf("заглушки: %+v", r.Items)
	}
	again, _ := e.media.Ensure(ctx, []string{"ИЗОБРАЖЕНИЕ 14"}, "", "")
	if again.Created != 0 || again.Items[0].Media.ID != r.Items[1].Media.ID {
		t.Fatalf("повтор: %+v", again)
	}
	// POST с именем заглушки — файл ложится в неё.
	it, err := e.media.Create(ctx, MediaUpload{Name: "Изображение 14", Filename: "14.png", Mime: "image/png", Kind: domain.MediaKindImage, Data: pngBytes})
	if err != nil || it.Media.ID != r.Items[1].Media.ID || it.Status != domain.MediaReady {
		t.Fatalf("заполнение заглушки: %+v %v", it, err)
	}
	missing, _ := e.media.List(ctx, domain.MediaMissing)
	if len(missing) != 1 || missing[0].Media.Name != "Памятка PDF" {
		t.Fatalf("фильтр missing: %+v", missing)
	}
}

func TestMediaAttachments(t *testing.T) {
	e := newMediaEnv(t)
	ctx := context.Background()
	mp := e.create(t, "Карта", "", "map.png")
	e.put(t, mp.Media.ID, domain.SiteGTP, "map-gtp.png")
	memo := e.create(t, "Памятка", domain.SiteLuzhniki, "memo.pdf")
	if _, err := e.media.Ensure(ctx, []string{"Изображение 14"}, "", ""); err != nil {
		t.Fatal(err)
	}

	// GTP: файл площадки; Лужники: общий.
	atts, warns := e.media.Attachments(ctx, []domain.MediaRef{ref("карта")}, domain.SiteGTP)
	if len(atts) != 1 || atts[0].Kind != max.UploadImage || !strings.Contains(atts[0].Token, "map-gtp.png") || len(warns) != 0 {
		t.Fatalf("GTP: %v %v", atts, warns)
	}
	atts, _ = e.media.Attachments(ctx, []domain.MediaRef{{MediaID: &mp.Media.ID, Name: "что угодно"}}, domain.SiteLuzhniki)
	if len(atts) != 1 || !strings.Contains(atts[0].Token, "-map.png") {
		t.Fatalf("Лужники → общий: %v", atts)
	}
	if e.up.n() != 2 {
		t.Fatalf("загрузок %d", e.up.n())
	}

	// Кэш: повтор не загружает и отдаёт тот же токен.
	atts2, _ := e.media.Attachments(ctx, []domain.MediaRef{ref("Карта")}, domain.SiteGTP)
	atts3, _ := e.media.Attachments(ctx, []domain.MediaRef{ref("Карта")}, domain.SiteGTP)
	if e.up.n() != 2 || atts2[0].Token != atts3[0].Token {
		t.Fatalf("кэш: загрузок %d", e.up.n())
	}

	// Пропуски: missing, неизвестное имя, нет файла площадки и общего; повтор одного файла — один раз.
	atts, warns = e.media.Attachments(ctx, []domain.MediaRef{ref("Изображение 14"), ref("Нет такого"), ref("Памятка"), ref("Карта"), ref("Карта")}, domain.SiteGTP)
	if len(atts) != 1 || len(warns) != 3 || !strings.Contains(warns[0], "missing") || !strings.Contains(warns[1], "нет в медиатеке") || !strings.Contains(warns[2], "GTP") {
		t.Fatalf("пропуски: %v %v", atts, warns)
	}
	atts, _ = e.media.Attachments(ctx, []domain.MediaRef{ref("памятка")}, domain.SiteLuzhniki)
	if len(atts) != 1 || atts[0].Kind != max.UploadFile {
		t.Fatalf("файл: %v", atts)
	}

	// Протухший токен: MAX не принял → ForgetTokens → загрузка заново.
	before := e.up.n()
	e.media.ForgetTokens(ctx, []string{atts[0].Token})
	again, _ := e.media.Attachments(ctx, []domain.MediaRef{ref("памятка")}, domain.SiteLuzhniki)
	if e.up.n() != before+1 || again[0].Token == atts[0].Token {
		t.Fatalf("после ForgetTokens: загрузок %d→%d", before, e.up.n())
	}
	// Срок токена вышел — тоже заново.
	*e.clock = e.clock.Add(DefaultMediaTokenTTL + time.Minute)
	before = e.up.n()
	e.media.Attachments(ctx, []domain.MediaRef{ref("памятка")}, domain.SiteLuzhniki)
	if e.up.n() != before+1 {
		t.Fatalf("TTL: загрузок %d→%d", before, e.up.n())
	}
	// Замена файла сбрасывает токен.
	e.put(t, memo.Media.ID, domain.SiteLuzhniki, "memo-v2.pdf")
	before = e.up.n()
	a3, _ := e.media.Attachments(ctx, []domain.MediaRef{ref("памятка")}, domain.SiteLuzhniki)
	if e.up.n() != before+1 || !strings.Contains(a3[0].Token, "memo-v2.pdf") {
		t.Fatalf("замена файла: %v", a3)
	}
	// MAX отказал — пропуск с предупреждением.
	e.up.fail = errors.New("attachment.not.ready")
	e.media.ForgetTokens(ctx, []string{a3[0].Token})
	a4, w4 := e.media.Attachments(ctx, []domain.MediaRef{ref("памятка")}, domain.SiteLuzhniki)
	if len(a4) != 0 || len(w4) != 1 || !strings.Contains(w4[0], "MAX") {
		t.Fatalf("отказ MAX: %v %v", a4, w4)
	}
}

func TestMediaAttachmentsNoMax(t *testing.T) {
	e := newMediaEnv(t)
	e.create(t, "Карта", "", "map.png")
	m := newMediaService(e.repos, nil, MediaOptions{}, time.Now)
	atts, warns := m.Attachments(context.Background(), []domain.MediaRef{ref("Карта")}, "")
	if len(atts) != 0 || len(warns) != 1 {
		t.Fatalf("без MAX: %v %v", atts, warns)
	}
}

func TestGroupAttachments(t *testing.T) {
	img := func(i int) max.AttachmentRef {
		return max.AttachmentRef{Kind: max.UploadImage, Token: string(rune('a' + i))}
	}
	file := max.AttachmentRef{Kind: max.UploadFile, Token: "f"}
	var atts []max.AttachmentRef
	for i := 0; i < 14; i++ {
		atts = append(atts, img(i))
	}
	atts = append(atts, file, img(20), file)
	g := GroupAttachments(atts)
	if len(g) != 5 || len(g[0]) != 12 || len(g[1]) != 2 || g[2][0].Token != "f" || len(g[3]) != 1 || g[4][0].Kind != max.UploadFile {
		t.Fatalf("группы: %v", g)
	}
	if len(GroupAttachments(nil)) != 0 {
		t.Fatal("пусто")
	}
}
