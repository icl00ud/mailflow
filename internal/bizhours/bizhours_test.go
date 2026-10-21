package bizhours

import (
	"testing"
	"time"
)

var ny, _ = time.LoadLocation("America/New_York")

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, ny)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAddSkipsWeekend(t *testing.T) {
	c := Calendar{Loc: ny}
	cases := []struct {
		from string
		h    float64
		want string
	}{
		{"2026-10-21 10:00", 24, "2026-10-22 10:00"}, // Wed -> Thu
		{"2026-10-23 18:00", 24, "2026-10-26 18:00"}, // Fri -> Mon
		{"2026-10-24 12:00", 6, "2026-10-26 06:00"},  // Sat noon + 6h -> Mon 06:00
		{"2026-10-22 12:00", 90, "2026-10-28 06:00"}, // Thu+90h: 12h Thu, 24 Fri, skip wknd, 24 Mon, 24 Tue, 6 Wed
	}
	for _, tc := range cases {
		got := c.Add(at(tc.from), tc.h)
		if !got.Equal(at(tc.want)) {
			t.Errorf("Add(%s,%v)=%s want %s", tc.from, tc.h, got.Format("2006-01-02 15:04"), tc.want)
		}
	}
}

func TestHolidayAndBetween(t *testing.T) {
	c := Calendar{Loc: ny, Holidays: map[string]bool{"2026-11-26": true}}
	got := c.Add(at("2026-11-25 12:00"), 24) // Wed + 24h, Thu is holiday -> Fri 12:00
	if !got.Equal(at("2026-11-27 12:00")) {
		t.Errorf("holiday: got %s", got)
	}
	if h := c.Between(at("2026-10-23 18:00"), at("2026-10-26 18:00")); h != 24 {
		t.Errorf("Between=%v want 24", h)
	}
}

func TestWindowShift(t *testing.T) {
	w := Window{Start: 8, End: 21}
	cases := [][2]string{
		{"2026-10-21 03:00", "2026-10-21 08:00"},
		{"2026-10-21 12:00", "2026-10-21 12:00"},
		{"2026-10-21 22:30", "2026-10-22 08:00"},
	}
	for _, c := range cases {
		if got := w.Shift(at(c[0]), ny); !got.Equal(at(c[1])) {
			t.Errorf("Shift(%s)=%s want %s", c[0], got, c[1])
		}
	}
}
