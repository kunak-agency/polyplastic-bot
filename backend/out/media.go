package out

import (
	"time"

	"main/services"
)

// Медиатека (api_contract.md, «v4»).

// MediaFile — файл записи; site null — общий.
type MediaFile struct {
	Site      *string `json:"site"`
	Filename  string  `json:"filename"`
	Mime      string  `json:"mime"`
	Size      int64   `json:"size"`
	SHA256    string  `json:"sha256"`
	UpdatedAt string  `json:"updated_at"`
}

// Media — строка медиатеки.
type Media struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Kind      string      `json:"kind"`
	Files     []MediaFile `json:"files"`
	Status    string      `json:"status"`
	UsedIn    int         `json:"used_in"`
	Aliases   []string    `json:"aliases"`
	UpdatedBy *string     `json:"updated_by"`
	UpdatedAt string      `json:"updated_at"`
}

// MediaList — GET /api/media.
type MediaList struct {
	Items []Media `json:"items"`
	Total int     `json:"total"`
}

// MediaRenamed — PATCH /api/media/{id}: запись и сколько ссылок в черновиках поправлено.
type MediaRenamed struct {
	Media
	UpdatedRefs int `json:"updated_refs"`
}

// MediaEnsured — POST /api/media/ensure.
type MediaEnsured struct {
	Items   []Media `json:"items"`
	Total   int     `json:"total"`
	Created int     `json:"created"`
}

// MediaFrom — строка из services.MediaItem.
func MediaFrom(it services.MediaItem) Media {
	files := make([]MediaFile, 0, len(it.Files))
	for _, f := range it.SortedFiles() {
		files = append(files, MediaFile{
			Site: strOrNil(f.Site), Filename: f.Filename, Mime: f.Mime, Size: f.Size, SHA256: f.SHA256,
			UpdatedAt: f.UpdatedAt.Format(time.RFC3339),
		})
	}
	aliases := []string(it.Media.Aliases)
	if aliases == nil {
		aliases = []string{}
	}
	return Media{
		ID: it.Media.ID.String(), Name: it.Media.Name, Kind: it.Media.Kind, Files: files, Status: it.Status,
		UsedIn: it.UsedIn, Aliases: aliases, UpdatedBy: strOrNil(it.Media.UpdatedBy),
		UpdatedAt: it.Media.UpdatedAt.Format(time.RFC3339),
	}
}

func mediaItems(list []services.MediaItem) []Media {
	out := make([]Media, 0, len(list))
	for _, it := range list {
		out = append(out, MediaFrom(it))
	}
	return out
}

// MediaListFrom — список.
func MediaListFrom(list []services.MediaItem) MediaList {
	items := mediaItems(list)
	return MediaList{Items: items, Total: len(items)}
}

// MediaEnsuredFrom — результат ensure.
func MediaEnsuredFrom(r services.EnsureResult) MediaEnsured {
	items := mediaItems(r.Items)
	return MediaEnsured{Items: items, Total: len(items), Created: r.Created}
}
