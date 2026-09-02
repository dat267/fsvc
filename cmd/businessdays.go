// Package cmd is the CLI application package for fsvc.
package cmd

import "time"

// NowInTZAt converts t to the given IANA location, returning t unchanged when
// loc is empty or invalid. time.LoadLocation caches locations internally, so
// repeated calls are cheap.
func NowInTZAt(loc string, t time.Time) time.Time {
	if loc == "" {
		return t
	}
	l, err := time.LoadLocation(loc)
	if err != nil {
		return t
	}
	return t.In(l)
}

// AddBusinessDays adds n weekdays, skipping weekends.
func AddBusinessDays(t time.Time, n int) time.Time {
	for i := 0; i < n; {
		t = t.AddDate(0, 0, 1)
		if isWeekday(t) {
			i++
		}
	}
	return t
}

// roundUpQuarterHour rounds t up to the next quarter-hour boundary (:00, :15,
// :30, :45) in t's own location. Times already exactly on a boundary are
// returned unchanged.
func roundUpQuarterHour(t time.Time) time.Time {
	h, m, s := t.Clock()
	total := h*60 + m
	if s != 0 || total%15 != 0 {
		total = (total/15 + 1) * 15
	}
	// time.Date normalizes overflow (60 minutes, 24 hours) into the next
	// hour/day, which handles rollover cases like 23:59:59 -> 00:00:00.
	return time.Date(t.Year(), t.Month(), t.Day(), total/60, total%60, 0, 0, t.Location())
}

// BusinessDaysBetween returns the number of weekdays between from and to,
// as a float. Weekends are skipped; partial start/end days count
// fractionally.
func BusinessDaysBetween(from, to time.Time) float64 {
	if to.Before(from) {
		from, to = to, from
	}

	loc := to.Location()
	from = from.In(loc)
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, loc)

	// Whole weekdays on [start, end).
	full := 0.0
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		if isWeekday(d) {
			full++
		}
	}

	// The start day is partially elapsed; the end day is partially complete.
	fracFrom := 0.0
	if isWeekday(from) {
		fracFrom = from.Sub(start).Minutes() / 1440
	}
	fracTo := 0.0
	if isWeekday(to) {
		fracTo = to.Sub(end).Minutes() / 1440
	}
	return full - fracFrom + fracTo
}

func isWeekday(t time.Time) bool {
	return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday
}
