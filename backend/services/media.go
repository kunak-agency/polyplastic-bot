package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"main/domain"
	"main/pkg/errs"
	"main/repositories"

	"github.com/google/uuid"
)

// Медиатека (api_contract.md, «v4»; вердикт ресерчера §Q5): записи с файлами по площадкам,
// «используется в N», переименование с автозаменой ссылок в черновиках, заглушки для импорта.
// Отправка в MAX — media_max.go (Attachments).

// MediaOptions — настройки медиатеки.
type MediaOptions struct {
	// MaxUploadMB — потолок файла (MEDIA_MAX_MB, 20). Тело запроса API — до 26 МБ.
	MaxUploadMB int
	// TokenTTL — сколько верим токену вложения MAX; старше — загружаем файл заново. 0 — 24 ч.
	TokenTTL time.Duration
}

// MediaService — медиатека.
type MediaService struct {
	repos    *repositories.Repos
	max      MediaUploader // nil — MAX не подключён: Attachments отвечает предупреждением
	maxBytes int64
	tokenTTL time.Duration
	now      func() time.Time

	// uploadMu — одна загрузка в MAX за раз: два сообщения с одной картинкой не загрузят её дважды.
	uploadMu sync.Mutex
}

// DefaultMediaTokenTTL — срок токена вложения MAX по умолчанию.
const DefaultMediaTokenTTL = 24 * time.Hour

func newMediaService(r *repositories.Repos, up MediaUploader, o MediaOptions, now func() time.Time) *MediaService {
	if o.MaxUploadMB <= 0 {
		o.MaxUploadMB = 20
	}
	if o.TokenTTL <= 0 {
		o.TokenTTL = DefaultMediaTokenTTL
	}
	return &MediaService{repos: r, max: up, maxBytes: int64(o.MaxUploadMB) << 20, tokenTTL: o.TokenTTL, now: now}
}

// MaxUploadBytes — потолок файла.
func (s *MediaService) MaxUploadBytes() int64 { return s.maxBytes }

// MediaItem — строка медиатеки.
type MediaItem struct {
	Media  domain.Media
	Files  []domain.MediaFile
	Status string
	UsedIn int
}

// MediaUpload — проверенный файл (in.MediaUploadValid) и его байты.
type MediaUpload struct {
	Name     string // для POST; у PUT не используется
	Site     string // "" — общий файл
	Filename string
	Mime     string
	Kind     string
	By       string
	Data     []byte
}

func errNoMediaDB() error {
	return errs.NewInternalError("Медиатека без базы данных не работает").WithCause(repositories.ErrNoDatabase)
}

func errMediaNotFound() error {
	return errs.NewNotFoundError("Запись медиатеки не найдена")
}

func mediaStatus(files []domain.MediaFile) string {
	if len(files) == 0 {
		return domain.MediaMissing
	}
	return domain.MediaReady
}

// List — все записи с файлами и числом ссылок. status — missing|ready|"" (все).
func (s *MediaService) List(ctx context.Context, status string) ([]MediaItem, error) {
	if !s.repos.Enabled() {
		return []MediaItem{}, nil
	}
	list, err := s.repos.Media.List(ctx)
	if err != nil {
		return nil, err
	}
	files, err := s.repos.Media.AllFiles(ctx)
	if err != nil {
		return nil, err
	}
	byMedia := map[uuid.UUID][]domain.MediaFile{}
	for _, f := range files {
		byMedia[f.MediaID] = append(byMedia[f.MediaID], f)
	}
	used, err := s.usage(ctx, list)
	if err != nil {
		return nil, err
	}
	out := make([]MediaItem, 0, len(list))
	for _, m := range list {
		it := MediaItem{Media: m, Files: byMedia[m.ID], UsedIn: used[m.ID]}
		it.Status = mediaStatus(it.Files)
		if status != "" && it.Status != status {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// Get — одна запись. Ошибка 404, если нет.
func (s *MediaService) Get(ctx context.Context, id uuid.UUID) (*MediaItem, error) {
	if !s.repos.Enabled() {
		return nil, errMediaNotFound()
	}
	m, err := s.repos.Media.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errMediaNotFound()
	}
	return s.item(ctx, *m)
}

func (s *MediaService) item(ctx context.Context, m domain.Media) (*MediaItem, error) {
	files, err := s.repos.Media.Files(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	used, err := s.usage(ctx, []domain.Media{m})
	if err != nil {
		return nil, err
	}
	return &MediaItem{Media: m, Files: files, Status: mediaStatus(files), UsedIn: used[m.ID]}, nil
}

// Create — новая запись с первым файлом. Имя занято заглушкой без файлов (импорт ТЗ) — файл
// ложится в неё; занято записью с файлами — 409.
func (s *MediaService) Create(ctx context.Context, u MediaUpload) (*MediaItem, error) {
	if !s.repos.Enabled() {
		return nil, errNoMediaDB()
	}
	var saved domain.Media
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		m, err := tx.Media.ByNameKey(ctx, domain.MediaNameKey(u.Name))
		if err != nil {
			return err
		}
		if m != nil {
			files, err := tx.Media.Files(ctx, m.ID)
			if err != nil {
				return err
			}
			if len(files) > 0 {
				return errs.NewConflictError(fmt.Sprintf("В медиатеке уже есть «%s». Замените файл в этой записи или выберите другое имя", m.Name))
			}
		} else {
			m = &domain.Media{Name: u.Name, NameKey: domain.MediaNameKey(u.Name), Aliases: domain.StringList{}}
		}
		m.Kind, m.UpdatedBy, m.UpdatedAt = u.Kind, u.By, s.now()
		if err := tx.Media.Save(ctx, m); err != nil {
			return err
		}
		if err := tx.Media.PutFile(ctx, s.fileOf(m.ID, u), u.Data); err != nil {
			return err
		}
		saved = *m
		return nil
	})
	if repositories.IsUniqueViolation(err) {
		return nil, errs.NewConflictError("Запись с таким именем только что создали. Обновите список").WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	return s.item(ctx, saved)
}

func (s *MediaService) fileOf(mediaID uuid.UUID, u MediaUpload) *domain.MediaFile {
	sum := sha256.Sum256(u.Data)
	return &domain.MediaFile{
		MediaID: mediaID, Site: u.Site, Filename: u.Filename, Mime: u.Mime, Size: int64(len(u.Data)),
		SHA256: hex.EncodeToString(sum[:]), UpdatedAt: s.now(),
	}
}

// PutFile — добавить или заменить файл площадки (u.Site, "" — общий). Вид файла должен
// совпадать с видом записи, если у неё уже есть файлы: картинка и PDF под одним именем бот
// отправил бы по-разному на разных площадках.
func (s *MediaService) PutFile(ctx context.Context, id uuid.UUID, u MediaUpload) (*MediaItem, error) {
	if !s.repos.Enabled() {
		return nil, errNoMediaDB()
	}
	var saved domain.Media
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		m, err := tx.Media.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if m == nil {
			return errMediaNotFound()
		}
		files, err := tx.Media.Files(ctx, id)
		if err != nil {
			return err
		}
		others := 0
		for _, f := range files {
			if f.Site != u.Site {
				others++
			}
		}
		if others > 0 && m.Kind != u.Kind {
			msg := "У записи уже есть картинки — загрузите картинку (PNG, JPG, GIF, WEBP)"
			if m.Kind == domain.MediaKindFile {
				msg = "У записи уже есть документы — загрузите документ (PDF, DOCX, XLSX, PPTX)"
			}
			return errs.NewValidationError(msg).WithDetails(errs.FieldError{Field: "file", Code: errs.CodeInvalidFormat, Message: msg})
		}
		m.Kind, m.UpdatedBy, m.UpdatedAt = u.Kind, u.By, s.now()
		if err := tx.Media.Save(ctx, m); err != nil {
			return err
		}
		if err := tx.Media.PutFile(ctx, s.fileOf(id, u), u.Data); err != nil {
			return err
		}
		saved = *m
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.item(ctx, saved)
}

// DeleteFile — убрать файл площадки ("" — общий).
func (s *MediaService) DeleteFile(ctx context.Context, id uuid.UUID, site string) (*MediaItem, error) {
	if !s.repos.Enabled() {
		return nil, errNoMediaDB()
	}
	m, err := s.repos.Media.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errMediaNotFound()
	}
	ok, err := s.repos.Media.DeleteFile(ctx, id, site)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errs.NewNotFoundError("Такого файла у записи нет")
	}
	return s.item(ctx, *m)
}

// Content — файл для просмотра: площадки site, иначе общий, иначе первый. nil — файлов нет.
func (s *MediaService) Content(ctx context.Context, id uuid.UUID, site string) (*domain.MediaFile, []byte, error) {
	if !s.repos.Enabled() {
		return nil, nil, errMediaNotFound()
	}
	files, err := s.repos.Media.Files(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	f := pickFile(files, site)
	if f == nil && site == "" && len(files) > 0 {
		f = &files[0]
	}
	if f == nil {
		return nil, nil, errs.NewNotFoundError("Файла нет")
	}
	data, err := s.repos.Media.FileData(ctx, f.ID)
	if err != nil {
		return nil, nil, err
	}
	if data == nil {
		return nil, nil, errs.NewNotFoundError("Файла нет")
	}
	return f, data, nil
}

// pickFile — файл площадки site, иначе общий; nil — ни того, ни другого.
func pickFile(files []domain.MediaFile, site string) *domain.MediaFile {
	var common *domain.MediaFile
	for i := range files {
		switch files[i].Site {
		case site:
			if site != "" {
				return &files[i]
			}
			common = &files[i]
		case "":
			common = &files[i]
		}
	}
	return common
}

// RenameResult — запись после переименования и сколько ссылок в черновиках поправлено.
type RenameResult struct {
	Item    *MediaItem
	Updated int
}

// Rename — новое имя записи. Ссылки черновиков (шаги, сообщения веток, send_file) на запись —
// по id или по прежнему имени — получают новое имя и id. Опубликованные версии неизменяемы:
// прежнее имя остаётся в Aliases, и бот находит запись и по нему.
func (s *MediaService) Rename(ctx context.Context, id uuid.UUID, name, by string) (*RenameResult, error) {
	if !s.repos.Enabled() {
		return nil, errNoMediaDB()
	}
	key := domain.MediaNameKey(name)
	var saved domain.Media
	updated := 0
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		m, err := tx.Media.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if m == nil {
			return errMediaNotFound()
		}
		if other, err := tx.Media.ByNameKey(ctx, key); err != nil {
			return err
		} else if other != nil && other.ID != id {
			return errs.NewConflictError(fmt.Sprintf("Имя «%s» уже занято другой записью медиатеки", other.Name))
		}
		before := *m
		before.Aliases = append(domain.StringList{}, m.Aliases...)
		if m.NameKey != key && !m.Aliases.Has(m.NameKey) {
			m.Aliases = append(m.Aliases, m.NameKey)
		}
		aliases := domain.StringList{}
		for _, a := range m.Aliases {
			if a != key {
				aliases = append(aliases, a)
			}
		}
		m.Name, m.NameKey, m.Aliases, m.UpdatedBy, m.UpdatedAt = name, key, aliases, by, s.now()
		if err := tx.Media.Save(ctx, m); err != nil {
			return err
		}
		steps, err := tx.Media.DraftSteps(ctx)
		if err != nil {
			return err
		}
		for _, st := range steps {
			n := renameStepRefs(&st, before, *m)
			if n == 0 {
				continue
			}
			if err := tx.Media.UpdateStepRefs(ctx, st.ID, st.Media, st.Buttons); err != nil {
				return err
			}
			updated += n
		}
		saved = *m
		return nil
	})
	if repositories.IsUniqueViolation(err) {
		return nil, errs.NewConflictError("Имя только что заняли. Обновите список").WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	it, err := s.item(ctx, saved)
	if err != nil {
		return nil, err
	}
	return &RenameResult{Item: it, Updated: updated}, nil
}

// renameStepRefs — ссылки шага на запись before → after (имя и id). Возвращает число правок.
func renameStepRefs(st *domain.ScenarioStep, before, after domain.Media) int {
	n := 0
	id := after.ID
	fix := func(refs domain.MediaRefList) domain.MediaRefList {
		for i, r := range refs {
			if before.Refers(r) {
				refs[i] = domain.MediaRef{MediaID: &id, Name: after.Name}
				n++
			}
		}
		return refs
	}
	st.Media = fix(st.Media)
	for bi := range st.Buttons {
		br := st.Buttons[bi].Branch
		if br == nil {
			continue
		}
		for mi := range br.Messages {
			br.Messages[mi].Media = fix(br.Messages[mi].Media)
		}
		for ai := range br.Actions {
			a := &br.Actions[ai]
			if a.Type == "send_file" && before.MatchesName(a.Media) {
				a.Media = after.Name
				n++
			}
		}
	}
	return n
}

// Delete — удалить запись, если на неё не ссылается ни один шаг или ветка.
func (s *MediaService) Delete(ctx context.Context, id uuid.UUID) error {
	if !s.repos.Enabled() {
		return errNoMediaDB()
	}
	return s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		m, err := tx.Media.ByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if m == nil {
			return errMediaNotFound()
		}
		used, err := mediaUsageIn(ctx, tx, []domain.Media{*m})
		if err != nil {
			return err
		}
		if n := used[id]; n > 0 {
			return errs.NewConflictError(fmt.Sprintf("«%s» используется в сценариях (%d). Сначала уберите ссылки из шагов", m.Name, n))
		}
		return tx.Media.Delete(ctx, id)
	})
}

// EnsureResult — записи для имён (существующие и новые заглушки) и сколько создано.
type EnsureResult struct {
	Items   []MediaItem
	Created int
}

// Ensure — заглушки missing для имён, которых в медиатеке нет (ни текущим, ни прежним именем).
// Для импорта ТЗ: ссылки шагов по имени сразу видны в медиатеке как «нет файла». kind —
// вид новых заглушек; пусто — по имени («PDF», «файл», «документ» → file, иначе image).
func (s *MediaService) Ensure(ctx context.Context, names []string, kind, by string) (*EnsureResult, error) {
	if !s.repos.Enabled() {
		return nil, errNoMediaDB()
	}
	out := &EnsureResult{Items: []MediaItem{}}
	seen := map[string]bool{}
	var ids []uuid.UUID
	err := s.repos.Transaction(ctx, func(tx *repositories.Repos) error {
		for _, name := range names {
			key := domain.MediaNameKey(name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			m, err := tx.Media.ByNameKey(ctx, key)
			if err != nil {
				return err
			}
			if m == nil {
				if m, err = tx.Media.ByAlias(ctx, key); err != nil {
					return err
				}
			}
			if m == nil {
				k := kind
				if k == "" {
					k = guessMediaKind(name)
				}
				m = &domain.Media{Name: strings.Join(strings.Fields(name), " "), NameKey: key, Kind: k, Aliases: domain.StringList{}, UpdatedBy: by, UpdatedAt: s.now()}
				if err := tx.Media.Create(ctx, m); err != nil {
					return err
				}
				out.Created++
			}
			ids = append(ids, m.ID)
		}
		return nil
	})
	if repositories.IsUniqueViolation(err) {
		return nil, errs.NewConflictError("Медиатеку одновременно правят. Повторите").WithCause(err)
	}
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		it, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, *it)
	}
	return out, nil
}

// guessMediaKind — вид заглушки по имени из ТЗ.
func guessMediaKind(name string) string {
	n := strings.ToLower(name)
	for _, w := range []string{"pdf", "файл", "документ", "памятк", "docx", "xlsx", "pptx"} {
		if strings.Contains(n, w) {
			return domain.MediaKindFile
		}
	}
	return domain.MediaKindImage
}

// usage — «используется в N» для записей list.
func (s *MediaService) usage(ctx context.Context, list []domain.Media) (map[uuid.UUID]int, error) {
	return mediaUsageIn(ctx, s.repos, list)
}

// mediaUsageIn — число мест, ссылающихся на запись: шаг (своими вложениями) и ветка кнопки
// (сообщениями или send_file) — каждый по разу. Смотрим черновики и последние опубликованные
// версии; шаг, который есть и там и там, считается один раз.
func mediaUsageIn(ctx context.Context, r *repositories.Repos, list []domain.Media) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(list) == 0 {
		return out, nil
	}
	drafts, err := r.Media.DraftSteps(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := r.Media.LatestVersions(ctx)
	if err != nil {
		return nil, err
	}
	places := map[uuid.UUID]map[string]bool{}
	add := func(scenario uuid.UUID, st domain.ScenarioStep) {
		for _, u := range mediaUnits(st) {
			for _, m := range list {
				if !u.refers(m) {
					continue
				}
				if places[m.ID] == nil {
					places[m.ID] = map[string]bool{}
				}
				places[m.ID][scenario.String()+"|"+st.ID.String()+"|"+u.key] = true
			}
		}
	}
	for _, st := range drafts {
		add(st.ScenarioID, st)
	}
	for _, v := range versions {
		for _, st := range v.Steps {
			add(v.ScenarioID, st)
		}
	}
	for id, p := range places {
		out[id] = len(p)
	}
	return out, nil
}

// mediaUnit — место ссылки: сам шаг (key "") или ветка кнопки (key "b:<подпись>").
type mediaUnit struct {
	key   string
	refs  []domain.MediaRef
	names []string // send_file по имени
}

func (u mediaUnit) refers(m domain.Media) bool {
	for _, r := range u.refs {
		if m.Refers(r) {
			return true
		}
	}
	for _, n := range u.names {
		if m.MatchesName(n) {
			return true
		}
	}
	return false
}

func mediaUnits(st domain.ScenarioStep) []mediaUnit {
	out := []mediaUnit{{key: "", refs: st.Media}}
	for i, b := range st.Buttons {
		if b.Branch == nil {
			continue
		}
		u := mediaUnit{key: fmt.Sprintf("b:%d:%s", i, b.Label)}
		for _, msg := range b.Branch.Messages {
			u.refs = append(u.refs, msg.Media...)
		}
		for _, a := range b.Branch.Actions {
			if a.Type == "send_file" && a.Media != "" {
				u.names = append(u.names, a.Media)
			}
		}
		out = append(out, u)
	}
	return out
}

// sortedSites — площадки файлов в порядке показа: общий, затем GrPP, Luzhniki, GTP.
func sortedSites(files []domain.MediaFile) []domain.MediaFile {
	order := map[string]int{"": 0}
	for i, c := range domain.SiteCodes {
		order[c] = i + 1
	}
	out := append([]domain.MediaFile(nil), files...)
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Site] < order[out[j].Site] })
	return out
}

// SortedFiles — файлы записи в порядке показа (общий, GrPP, Luzhniki, GTP).
func (it MediaItem) SortedFiles() []domain.MediaFile { return sortedSites(it.Files) }
