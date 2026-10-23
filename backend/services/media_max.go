package services

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"main/domain"
	"main/externalApi/max"
	"main/pkg/logger"

	"github.com/google/uuid"
)

// Вложения для бота: ссылки шага на медиатеку → токены вложений MAX.
//
// Ограничения MAX (a_core_stack.md, SDK v2.4.6): картинок — до 12 в одном сообщении вместе с
// клавиатурой; файл — одно вложение на сообщение (+ клавиатура). Разбивку по сообщениям делает
// GroupAttachments.

// MediaUploader — что медиатеке нужно от MAX. max.Client подходит как есть (и max.Fake).
type MediaUploader interface {
	Upload(ctx context.Context, kind max.UploadKind, r io.Reader, name string, size int64) (string, error)
}

// MediaAttacher — медиатека глазами бота. Services.Media реализует его; бот держит поле этого
// типа, чтобы тесты бота подставляли свою подделку.
type MediaAttacher interface {
	// Attachments — вложения для ссылок refs новичку площадки site (GrPP|Luzhniki|GTP, "" —
	// неизвестна): ссылка по media_id, без него — по имени (текущему или прежнему); файл площадки,
	// иначе общий. Токен MAX берётся из кэша в БД, иначе файл загружается (POST /uploads) и токен
	// сохраняется. Чего нет (запись не найдена, missing, нет файла для площадки, отказ загрузки)
	// — пропускается, причина — строкой в warnings (без ПДн, можно в лог). Порядок — как в refs,
	// повтор одного файла отдаётся один раз.
	Attachments(ctx context.Context, refs []domain.MediaRef, site string) ([]max.AttachmentRef, []string)
	// ForgetTokens — MAX не принял эти токены (протухли): следующий Attachments загрузит файлы
	// заново.
	ForgetTokens(ctx context.Context, tokens []string)
}

var _ MediaAttacher = (*MediaService)(nil)

// Attachments — см. MediaAttacher.
func (s *MediaService) Attachments(ctx context.Context, refs []domain.MediaRef, site string) ([]max.AttachmentRef, []string) {
	out := []max.AttachmentRef{}
	var warns []string
	if len(refs) == 0 {
		return out, nil
	}
	if !s.repos.Enabled() {
		return out, []string{"медиатека: нет базы — вложения не отправлены"}
	}
	list, err := s.repos.Media.List(ctx)
	if err != nil {
		return out, []string{fmt.Sprintf("медиатека: список не прочитан: %v", err)}
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range refs {
		m := resolveMedia(list, ref)
		if m == nil {
			warns = append(warns, fmt.Sprintf("медиа «%s»: нет в медиатеке", ref.Name))
			continue
		}
		files, err := s.repos.Media.Files(ctx, m.ID)
		if err != nil {
			warns = append(warns, fmt.Sprintf("медиа «%s»: файлы не прочитаны: %v", m.Name, err))
			continue
		}
		if len(files) == 0 {
			warns = append(warns, fmt.Sprintf("медиа «%s»: файл не загружен (missing)", m.Name))
			continue
		}
		f := pickFile(files, site)
		if f == nil {
			warns = append(warns, fmt.Sprintf("медиа «%s»: нет файла для площадки %s и общего", m.Name, siteOrDash(site)))
			continue
		}
		if seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		token, err := s.token(ctx, *m, *f)
		if err != nil {
			warns = append(warns, fmt.Sprintf("медиа «%s»: загрузка в MAX не удалась: %v", m.Name, err))
			continue
		}
		out = append(out, max.AttachmentRef{Kind: uploadKind(m.Kind), Token: token})
	}
	return out, warns
}

// ForgetTokens — см. MediaAttacher.
func (s *MediaService) ForgetTokens(ctx context.Context, tokens []string) {
	if !s.repos.Enabled() || len(tokens) == 0 {
		return
	}
	n, err := s.repos.Media.ClearTokens(ctx, tokens)
	if err != nil {
		logger.Warn("медиатека: токены MAX не сброшены: %v", err)
		return
	}
	logger.Info("медиатека: сброшено токенов MAX %d — файлы загрузятся заново", n)
}

// token — токен вложения: из кэша, если свежий, иначе загрузка в MAX и запись в кэш.
func (s *MediaService) token(ctx context.Context, m domain.Media, f domain.MediaFile) (string, error) {
	if s.fresh(f) {
		return f.MaxToken, nil
	}
	if s.max == nil {
		return "", fmt.Errorf("MAX не подключён")
	}
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()
	// Пока ждали очереди, файл мог загрузить соседний вызов.
	if cur, err := s.repos.Media.Files(ctx, m.ID); err == nil {
		for _, c := range cur {
			if c.ID == f.ID && c.SHA256 == f.SHA256 && s.fresh(c) {
				return c.MaxToken, nil
			}
		}
	}
	data, err := s.repos.Media.FileData(ctx, f.ID)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", fmt.Errorf("байты файла не найдены")
	}
	token, err := s.max.Upload(ctx, uploadKind(m.Kind), bytes.NewReader(data), f.Filename, int64(len(data)))
	if err != nil {
		return "", err
	}
	if err := s.repos.Media.SetToken(ctx, f.ID, f.SHA256, token, s.now()); err != nil {
		logger.Warn("медиатека: токен MAX не сохранён (файл %s): %v", f.ID, err)
	}
	return token, nil
}

func (s *MediaService) fresh(f domain.MediaFile) bool {
	return f.MaxToken != "" && f.MaxTokenAt != nil && s.now().Sub(*f.MaxTokenAt) < s.tokenTTL
}

// resolveMedia — запись для ссылки: по id; без id (или id не найден) — по текущему имени, затем
// по прежнему.
func resolveMedia(list []domain.Media, ref domain.MediaRef) *domain.Media {
	if ref.MediaID != nil {
		for i := range list {
			if list[i].ID == *ref.MediaID {
				return &list[i]
			}
		}
	}
	key := domain.MediaNameKey(ref.Name)
	if key == "" {
		return nil
	}
	for i := range list {
		if list[i].NameKey == key {
			return &list[i]
		}
	}
	for i := range list {
		if list[i].Aliases.Has(key) {
			return &list[i]
		}
	}
	return nil
}

func uploadKind(kind string) max.UploadKind {
	if kind == domain.MediaKindFile {
		return max.UploadFile
	}
	return max.UploadImage
}

func siteOrDash(site string) string {
	if site == "" {
		return "—"
	}
	return site
}

// MaxImagesPerMessage — картинок в одном сообщении MAX.
const MaxImagesPerMessage = 12

// GroupAttachments — вложения по сообщениям под ограничения MAX: подряд идущие картинки — до 12
// в сообщении, каждый файл — отдельным сообщением. Порядок сохраняется. Клавиатуру шага бот
// вешает на последнее сообщение группы.
func GroupAttachments(atts []max.AttachmentRef) [][]max.AttachmentRef {
	var out [][]max.AttachmentRef
	var cur []max.AttachmentRef
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	for _, a := range atts {
		if a.Kind != max.UploadImage {
			flush()
			out = append(out, []max.AttachmentRef{a})
			continue
		}
		if len(cur) == MaxImagesPerMessage {
			flush()
		}
		cur = append(cur, a)
	}
	flush()
	return out
}
