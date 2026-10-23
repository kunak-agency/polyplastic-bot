package max

import "encoding/json"

// Сырой разбор тела GET /updates — только то, что SDK теряет. В SDK-модели Attachment нет поля
// transcription, хотя в его же schema.yaml (v0.0.30) оно есть у AudioAttachment — рядом с
// payload, а не внутри. Payload.transcription читаем тоже: ресерч ссылается на v0.0.33, и где
// поле окажется в живом ответе, не проверено.
type rawUpdates struct {
	Updates []struct {
		UpdateType string `json:"update_type"`
		Message    struct {
			Body struct {
				Mid         string          `json:"mid"`
				Attachments []rawAttachment `json:"attachments"`
			} `json:"body"`
		} `json:"message"`
	} `json:"updates"`
}

type rawAttachment struct {
	Type          string  `json:"type"`
	Transcription *string `json:"transcription"`
	Duration      int     `json:"duration"`
	Payload       struct {
		URL           string  `json:"url"`
		Token         string  `json:"token"`
		Transcription *string `json:"transcription"`
	} `json:"payload"`
}

// audioByMid — аудиовложения сообщений из тела GET /updates, по mid. Связь с событиями SDK —
// по mid, а не по номеру в массиве: так разбор не зависит от того, что SDK делает с порядком.
// Тело не разобралось — пустая карта: голосовые тогда придут без расшифровки, но придут.
func audioByMid(body []byte) map[string][]Audio {
	out := map[string][]Audio{}
	if len(body) == 0 {
		return out
	}
	var raw rawUpdates
	if err := json.Unmarshal(body, &raw); err != nil {
		return out
	}
	for _, u := range raw.Updates {
		mid := u.Message.Body.Mid
		if mid == "" {
			continue
		}
		for _, a := range u.Message.Body.Attachments {
			if a.Type != "audio" {
				continue
			}
			out[mid] = append(out[mid], Audio{
				URL:           a.Payload.URL,
				Token:         a.Payload.Token,
				Duration:      a.Duration,
				Transcription: firstNonEmpty(a.Transcription, a.Payload.Transcription),
			})
		}
	}
	return out
}

func firstNonEmpty(vals ...*string) string {
	for _, v := range vals {
		if v != nil && *v != "" {
			return *v
		}
	}
	return ""
}
