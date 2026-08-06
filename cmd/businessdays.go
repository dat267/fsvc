// Package biz holds pure business logic for the CLI: business-day math,
// priority matrices, and ticket classification. It has no HTTP or I/O
// dependencies so it can be tested in isolation.
package cmd

import "time"

// NowInTZ returns the current time in the given IANA location, or local time
// when loc is empty or invalid.
func NowInTZ(loc string) time.Time {
	return NowInTZAt(loc, time.Now())
}

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
