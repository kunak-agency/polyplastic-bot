package max

import "encoding/json"

// imagesByMid — фото во входящих сообщениях из тела GET /updates, по mid (SDK-модель их
// отдаёт, но разбирать сырое тело проще и так же, как аудио).
func imagesByMid(body []byte) map[string][]Image {
	out := map[string][]Image{}
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
			if a.Type != "image" {
				continue
			}
			out[mid] = append(out[mid], Image{URL: a.Payload.URL, Token: a.Payload.Token})
		}
	}
	return out
}
