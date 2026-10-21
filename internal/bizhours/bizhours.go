// Package bizhours does "business hour" arithmetic: elapsed clock hours that
// only count on working days (Mon-Fri, minus configured holidays) in a
// store's time zone. 24 business hours from Friday 18:00 is Monday 18:00.
package bizhours

import "time"

type Calendar struct {
	Loc      *time.Location
	Holidays map[string]bool // "2006-01-02" in Loc
}

func (c Calendar) loc() *time.Location {
	if c.Loc == nil {
		return time.UTC
	}
	return c.Loc
}

func (c Calendar) isBusinessDay(t time.Time) bool {
	t = t.In(c.loc())
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return !c.Holidays[t.Format("2006-01-02")]
}

func nextMidnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, t.Location())
}

// Add returns t advanced by h hours, counting only business days.
func (c Calendar) Add(t time.Time, h float64) time.Time {
	cur := t.In(c.loc())
	remaining := time.Duration(h * float64(time.Hour))
	for i := 0; i < 3660; i++ { // hard stop: ~10 years of days
		end := nextMidnight(cur)
		if !c.isBusinessDay(cur) {
			cur = end
			continue
		}
		seg := end.Sub(cur)
		if remaining <= seg {
			return cur.Add(remaining)
		}
		remaining -= seg
		cur = end
	}
	return cur
}

// Between counts business hours elapsed from a to b (b >= a), the inverse of Add.
func (c Calendar) Between(a, b time.Time) float64 {
	if !b.After(a) {
		return 0
	}
	cur := a.In(c.loc())
	end := b.In(c.loc())
	var total time.Duration
	for cur.Before(end) {
		next := nextMidnight(cur)
		if next.After(end) {
			next = end
		}
		if c.isBusinessDay(cur) {
			total += next.Sub(cur)
		}
		cur = next
	}
	return total.Hours()
}

// Window holds the local hours in which customer emails may go out.
type Window struct{ Start, End int } // [Start, End)

// Shift moves t forward to the next moment inside the window (t itself if inside).
func (w Window) Shift(t time.Time, loc *time.Location) time.Time {
	if w.End <= w.Start {
		return t
	}
	l := t.In(loc)
	y, m, d := l.Date()
	start := time.Date(y, m, d, w.Start, 0, 0, 0, loc)
	end := time.Date(y, m, d, w.End, 0, 0, 0, loc)
	switch {
	case l.Before(start):
		return start
	case l.Before(end):
		return t
	default:
		return time.Date(y, m, d+1, w.Start, 0, 0, 0, loc)
	}
}
