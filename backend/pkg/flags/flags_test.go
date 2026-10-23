package flags

import (
	"slices"
	"testing"
)

// Фразы синтетические: так могли бы написать новички в первые недели.
func TestCodes(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		// F7 — увольнение.
		{"Наверное, уволюсь после испытательного", []string{CodeQuit}},
		{"Меня уволят, если опоздаю ещё раз?", []string{CodeQuit}},
		{"Как оформить УВОЛЬНЕНИЕ по собственному", []string{CodeQuit}},
		{"у нас в цеху всех увольняют", []string{CodeQuit}},
		{"Уволь меня, я больше не могу", []string{CodeQuit}},
		{"его уволили вчера", []string{CodeQuit}},
		// F5 — удар.
		{"Мастер ударил меня в раздевалке", []string{CodeViolence}},
		{"старший смены ударила стажёра", []string{CodeViolence}},
		{"Ударили по плечу, было больно", []string{CodeViolence}},
		// F9 — взятка, откат.
		{"Попросили взятку за допуск", []string{CodeCorruption}},
		{"без взяток тут ничего не решить", []string{CodeCorruption}},
		{"он взяточник", []string{CodeCorruption}},
		{"Поставщик предлагал откат", []string{CodeCorruption}},
		{"с откатом в 10%", []string{CodeCorruption}},
		{"откаты, как обычно", []string{CodeCorruption}},
		// Несколько флагов в одном сообщении — по одному на код, в порядке правил.
		{"Требуют откат, а то уволят", []string{CodeQuit, CodeCorruption}},
		{"уволюсь, уволюсь, уволюсь", []string{CodeQuit}},
		{"ёлки, уволился бригадир", []string{CodeQuit}},

		// Обычные фразы — без срабатываний.
		{"Когда можно в отпуск?", nil},
		{"Где получить спецодежду и каску", nil},
		{"Нужно откатить тележку к складу", nil},
		{"откатили паллеты, закончили смену", nil},
		{"Поставь ударение правильно", nil},
		{"ударный труд на линии экструзии", nil},
		{"Осторожно, можно получить удар током", nil},
		{"Пропуск взят на проходной", nil},
		{"взятый образец отдал в лабораторию", nil},
		{"Привольно у вас на территории", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := Codes(c.text)
		if !slices.Equal(got, c.want) {
			t.Errorf("Codes(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestDetectTrigger(t *testing.T) {
	hits := Detect("Говорят, за допуск нужна взятка.")
	if len(hits) != 1 || hits[0].Code != CodeCorruption || hits[0].Trigger != "взятка" {
		t.Fatalf("Detect = %+v, want F9 на «взятка»", hits)
	}
}
