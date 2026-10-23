package services

import (
	"testing"
	"time"

	"main/domain"
)

var gtp = domain.SiteOf(domain.SiteGTP)

// msk — момент по Москве.
func msk(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, gtp.Loc)
}

// date — дата выхода так, как её отдаёт колонка date (полночь UTC).
func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestFireAt(t *testing.T) {
	cases := []struct {
		name      string
		start     time.Time
		day       int
		hhmm      string
		workHours bool
		want      time.Time
	}{
		{"D0 во вторник", date(2026, 11, 3), 0, "10:00", true, msk(2026, 11, 3, 10, 0)},
		{"время по умолчанию 10:00", date(2026, 11, 3), 0, "", true, msk(2026, 11, 3, 10, 0)},
		{"D−3 в субботу → понедельник, он раньше D0", date(2026, 11, 3), -3, "10:00", true, msk(2026, 11, 2, 10, 0)},
		{"D−1 в воскресенье → не в D0, а в пятницу", date(2026, 11, 9), -1, "10:00", true, msk(2026, 11, 6, 10, 0)},
		{"D+5 в субботу → понедельник", date(2026, 11, 2), 5, "10:00", true, msk(2026, 11, 9, 10, 0)},
		{"пятница 18:00 → 17:00", date(2026, 11, 6), 0, "18:00", true, msk(2026, 11, 6, 17, 0)},
		{"пятница 17:30 → 17:00", date(2026, 11, 2), 4, "17:30", true, msk(2026, 11, 6, 17, 0)},
		{"четверг 18:00 остаётся", date(2026, 11, 5), 0, "18:00", true, msk(2026, 11, 5, 18, 0)},
		{"рано утром → 9:00", date(2026, 11, 3), 0, "07:30", true, msk(2026, 11, 3, 9, 0)},
		{"вне рабочих часов разрешено", date(2026, 11, 6), 0, "18:30", false, msk(2026, 11, 6, 18, 30)},
		{"выходной переносится и без рабочих часов", date(2026, 11, 2), 5, "10:00", false, msk(2026, 11, 9, 10, 0)},
	}
	for _, c := range cases {
		if got := FireAt(c.start, c.day, c.hhmm, gtp, c.workHours); !got.Equal(c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNextWorkdayAtSkipsWeekend(t *testing.T) {
	got := NextWorkdayAt(msk(2026, 11, 6, 15, 0), "17:30", gtp, true)
	if want := msk(2026, 11, 9, 17, 30); !got.Equal(want) {
		t.Fatalf("напоминание с пятницы: %v, want %v", got, want)
	}
}

func TestDayNumber(t *testing.T) {
	start := date(2026, 11, 3)
	for now, want := range map[time.Time]int{
		msk(2026, 10, 31, 23, 59): -3,
		msk(2026, 11, 3, 0, 1):    0,
		msk(2026, 11, 17, 12, 0):  14,
	} {
		if got := DayNumber(start, now, gtp); got != want {
			t.Errorf("%v: день %d, want %d", now, got, want)
		}
	}
}

func TestParseWhen(t *testing.T) {
	cases := map[string]struct {
		day  int
		hhmm string
		ok   bool
	}{
		"D−3":          {-3, "", true},
		"D0 10:00":     {0, "10:00", true},
		"Д+2 17:30":    {2, "17:30", true},
		"D +14":        {14, "", true},
		"-1":           {-1, "", true},
		"D":            {0, "", true},
		"9.00 D+30":    {30, "09:00", true},
		"когда-нибудь": {0, "", false},
		"День 5":       {0, "", false},
		"D-":           {0, "", false},
	}
	for in, want := range cases {
		day, hhmm, ok := parseWhen(in)
		if ok != want.ok || (ok && (day != want.day || hhmm != want.hhmm)) {
			t.Errorf("%q: (%d, %q, %v), want %+v", in, day, hhmm, ok, want)
		}
	}
}
