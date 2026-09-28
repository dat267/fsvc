package cmd

import (
	"testing"
	"time"
)

func TestNowInTZAt(t *testing.T) {
	at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)

	if got := NowInTZAt("", at); !got.Equal(at) {
		t.Errorf("empty loc should return at unchanged, got %v", got)
	}
	if got := NowInTZAt("Invalid/Loc", at); !got.Equal(at) {
		t.Errorf("invalid loc should return at unchanged, got %v", got)
	}
	got := NowInTZAt("America/New_York", at)
	if got.Location().String() != "America/New_York" {
		t.Errorf("expected New York location, got %v", got.Location())
	}
}

func TestRoundUpQuarterHour(t *testing.T) {
	utc := time.UTC
	kolkata, _ := time.LoadLocation("Asia/Kolkata")

	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"exact boundary unchanged", time.Date(2026, 8, 4, 12, 15, 0, 0, utc), time.Date(2026, 8, 4, 12, 15, 0, 0, utc)},
		{"mid-quarter rounds up", time.Date(2026, 8, 4, 12, 7, 30, 0, utc), time.Date(2026, 8, 4, 12, 15, 0, 0, utc)},
		{"boundary with seconds rounds up", time.Date(2026, 8, 4, 12, 15, 30, 0, utc), time.Date(2026, 8, 4, 12, 30, 0, 0, utc)},
		{"hour rollover", time.Date(2026, 8, 4, 12, 59, 59, 0, utc), time.Date(2026, 8, 4, 13, 0, 0, 0, utc)},
		{"day rollover", time.Date(2026, 8, 4, 23, 59, 59, 0, utc), time.Date(2026, 8, 5, 0, 0, 0, 0, utc)},
		{"tz offset preserved", time.Date(2026, 8, 4, 12, 7, 30, 0, kolkata), time.Date(2026, 8, 4, 12, 15, 0, 0, kolkata)},
		{"top of hour unchanged", time.Date(2026, 8, 4, 9, 0, 0, 0, utc), time.Date(2026, 8, 4, 9, 0, 0, 0, utc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundUpQuarterHour(tt.in)
			if !got.Equal(tt.want) {
				t.Errorf("roundUpQuarterHour(%v) = %v, want %v", tt.in.Format(time.RFC3339), got.Format(time.RFC3339), tt.want.Format(time.RFC3339))
			}
		})
	}
}

func TestBusinessDaysBetween(t *testing.T) {
	mon := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)      // Monday noon
	fri := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)      // Friday noon
	nextMon := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) // next Monday noon

	tests := []struct {
		from, to time.Time
		want     float64
	}{
		{mon, mon, 0},
		{mon, mon.AddDate(0, 0, 1), 1},
		{mon, fri, 4},
		{fri, nextMon, 1},
		{mon, nextMon, 5},
		{fri, fri, 0},
	}

	for _, tt := range tests {
		got := BusinessDaysBetween(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("BusinessDaysBetween(%v, %v) = %v, want %v", tt.from.Format(time.RFC3339), tt.to.Format(time.RFC3339), got, tt.want)
		}
	}
}

func TestClassify(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) // Tuesday
	created := now.Add(-72 * time.Hour)                 // Saturday

	unassigned := int64(-1)
	assigned := int64(5)
	other := int64(9)

	tests := []struct {
		name          string
		responder     *int64
		lastMsg       time.Time
		lastUserID    int64
		created       time.Time
		olderThanDays float64
		want          Category
	}{
		{"unassigned", &unassigned, time.Time{}, 0, created, 1, CategoryUnassigned},
		{"assigned no msg old created", &assigned, time.Time{}, 0, created, 1, CategoryStaleAgent},
		{"assigned no msg recent created", &assigned, time.Time{}, 0, now, 1, CategoryNone},
		{"someone else replied", &assigned, now, other, created, 1, CategoryCustomer},
		{"stale agent reply", &assigned, created, assigned, created, 1, CategoryStaleAgent},
		{"recent agent reply", &assigned, now, assigned, created, 1, CategoryNone},
		{"recent within threshold", &assigned, now.Add(-30 * time.Minute), assigned, created, 1, CategoryNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.responder, tt.lastMsg, tt.lastUserID, tt.created, tt.olderThanDays, now)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTargetEndDate(t *testing.T) {
	tue := time.Date(2026, 8, 4, 12, 7, 30, 0, time.UTC) // Tuesday

	tests := []struct {
		name string
		base time.Time
		days int
		hour int
		want string
	}{
		{"base + 3 business days at the target hour", tue, 3, 17, "2026-08-07T17:00:00Z"},
		{"zero days keeps the day", tue, 0, 9, "2026-08-04T09:00:00Z"},
		{"midnight hour", tue, 3, 0, "2026-08-07T00:00:00Z"},
		{"weekend skipped", time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC), 1, 17, "2026-08-10T17:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TargetEndDate(tt.base, tt.days, tt.hour, time.Time{})
			if got.Format(time.RFC3339) != tt.want {
				t.Errorf("TargetEndDate(%s, %d, %d) = %s, want %s",
					tt.base.Format(time.RFC3339), tt.days, tt.hour, got.Format(time.RFC3339), tt.want)
			}
		})
	}
}

// A target that would land in the past is clamped to the nearest future slot:
// the same day at the target hour, else the next business day at that hour.
func TestTargetEndDateClampsToTheFuture(t *testing.T) {
	old := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"before the target hour clamps to today", time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC), "2026-08-04T17:00:00Z"},
		{"after the target hour moves to the next business day", time.Date(2026, 8, 4, 18, 0, 0, 0, time.UTC), "2026-08-05T17:00:00Z"},
		{"friday evening moves to monday", time.Date(2026, 8, 7, 18, 0, 0, 0, time.UTC), "2026-08-10T17:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TargetEndDate(old, 3, 17, tt.now)
			if got.Format(time.RFC3339) != tt.want {
				t.Errorf("clamped target = %s, want %s", got.Format(time.RFC3339), tt.want)
			}
		})
	}
}

func TestTargetEndDateKeepsTheAccountOffset(t *testing.T) {
	dubai := time.FixedZone("+04:00", 4*60*60)
	base := time.Date(2026, 8, 4, 12, 7, 30, 0, dubai)

	got := TargetEndDate(base, 3, 17, time.Time{})
	if want := "2026-08-07T17:00:00+04:00"; got.Format(time.RFC3339) != want {
		t.Errorf("expected the account offset preserved: want %s, got %s", want, got.Format(time.RFC3339))
	}
}

func TestEndDateNeedsUpdate(t *testing.T) {
	target := time.Date(2026, 8, 7, 17, 0, 0, 0, time.FixedZone("+04:00", 4*60*60))

	if !EndDateNeedsUpdate(nil, target) {
		t.Error("a missing planned_end_date needs an update")
	}
	equal := time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC) // same instant, other offset
	if EndDateNeedsUpdate(&equal, target) {
		t.Error("an equal instant in another offset must be skipped")
	}
	different := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	if !EndDateNeedsUpdate(&different, target) {
		t.Error("a different instant needs an update")
	}
}
