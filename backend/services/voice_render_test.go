package services

import (
	"errors"
	"testing"

	"main/config"
	"main/externalApi/rag"
)

func TestVoiceAnswerText(t *testing.T) {
	str := func(s string) *string { return &s }
	page := 3
	cases := []struct {
		name      string
		resp      *rag.AskResponse
		err       error
		kind      string
		text      string
		wantLabel string
	}{
		{"ПВТР", &rag.AskResponse{Answer: str("Отпуск можно делить на части"), Sources: []rag.Source{
			{Doc: "ПВТР П 3.11-07", ClauseID: "6.7.4", Page: &page, Quote: "q"}, {Doc: "ПВТР П 3.11-07", ClauseID: "6.7.4", Quote: "q2"}}},
			nil, VoiceKindAnswer, "Отпуск можно делить на части. Это пункт 6.7.4 правил внутреннего трудового распорядка.", "ПВТР 6.7.4"},
		{"разметка и точка", &rag.AskResponse{Answer: str("**Да**, можно!"), Sources: []rag.Source{{Doc: "Антикоррупционная политика", ClauseID: "3.1", Quote: "q"}}},
			nil, VoiceKindAnswer, "Да, можно! Это пункт 3.1 антикоррупционной политики.", "Антикоррупционная политика 3.1"},
		{"незнакомый документ без пункта", &rag.AskResponse{Answer: str("Так."), Sources: []rag.Source{{Doc: "Памятка / Лист1", Quote: "q"}}},
			nil, VoiceKindAnswer, "Так. Это из документа «Памятка».", "Памятка"},
		{"отказ", &rag.AskResponse{NeedsEscalation: true}, nil, VoiceKindAbstain, VoiceAbstain, ""},
		{"ошибка", nil, errors.New("timeout"), VoiceKindError, VoiceError, ""},
	}
	for _, c := range cases {
		kind, text, srcs := VoiceAnswerText(c.resp, c.err)
		if kind != c.kind || text != c.text {
			t.Errorf("%s: %s %q", c.name, kind, text)
		}
		if c.wantLabel != "" && (len(srcs) != 1 || srcs[0].Label != c.wantLabel) {
			t.Errorf("%s: источники %+v", c.name, srcs)
		}
	}
}

func TestCallSiteAndAcquire(t *testing.T) {
	if CallSite("luzhniki") != "Luzhniki" || CallSite("Марс") != "" || CallSite("") != "" {
		t.Fatal("CallSite")
	}
	s := NewCallService(nil, config.Call{MaxConcurrent: 1}, true)
	r1, ok := s.Acquire()
	if !ok {
		t.Fatal("первая линия занята")
	}
	if _, ok := s.Acquire(); ok {
		t.Fatal("вторая линия при лимите 1")
	}
	r1()
	r1() // повтор безвреден
	r2, ok := s.Acquire()
	if !ok {
		t.Fatal("линия не освободилась")
	}
	r2()
	if s.Answer(t.Context(), "вопрос", "").Kind != VoiceKindError {
		t.Fatal("без rag — ошибка")
	}
}
