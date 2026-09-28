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

func TestMinUrgencyImpactForPriority(t *testing.T) {
	tests := []struct {
		priority int
		wantU, I int
		wantOK   bool
	}{
		{1, 1, 1, true},
		{2, 3, 1, true},
		{3, 3, 2, true},
		{4, 3, 3, true},
		{5, 0, 0, false},
	}
	for _, tt := range tests {
		u, i, ok := MinUrgencyImpactForPriority(tt.priority)
		if u != tt.wantU || i != tt.I || ok != tt.wantOK {
			t.Errorf("MinUrgencyImpactForPriority(%d) = %d,%d,%v want %d,%d,%v", tt.priority, u, i, ok, tt.wantU, tt.I, tt.wantOK)
		}
	}
}

func TestPriorityFor(t *testing.T) {
	if PriorityFor(3, 3) != 4 {
		t.Error("urgency 3 impact 3 should be priority 4")
	}
	if PriorityFor(1, 3) != 2 {
		t.Error("urgency 1 impact 3 should be priority 2")
	}
	if PriorityFor(3, 1) != 2 {
		t.Error("urgency 3 impact 1 should be priority 2")
	}
	if PriorityFor(0, 0) != 0 {
		t.Error("urgency 0 impact 0 should return 0")
	}
	if PriorityFor(4, 1) != 0 {
		t.Error("urgency 4 impact 1 should return 0")
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
	tueNoon := time.Date(2026, 8, 4, 12, 7, 30, 0, time.UTC) // Tuesday

	tests := []struct {
		name    string
		now     time.Time
		days    int
		endHour int
		want    string
	}{
		{"default keeps time, rounds quarter", tueNoon, 3, -1, "2026-08-07T12:15:00Z"},
		{"endHour overrides time of day", tueNoon, 3, 17, "2026-08-07T17:00:00Z"},
		{"endHour midnight", tueNoon, 3, 0, "2026-08-07T00:00:00Z"},
		{"zero days same day", tueNoon, 0, 9, "2026-08-04T09:00:00Z"},
		{"weekend skipped", time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC), 1, -1, "2026-08-10T15:00:00Z"}, // Fri + 1 = Mon
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TargetEndDate(tt.now, tt.days, tt.endHour)
			if got.Format(time.RFC3339) != tt.want {
				t.Errorf("TargetEndDate(%v, %d, %d) = %s, want %s", tt.now, tt.days, tt.endHour, got.Format(time.RFC3339), tt.want)
			}
		})
	}
}

func TestShouldPushEnd(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	future := now.Add(72 * time.Hour)
	soon := now.Add(6 * time.Hour)
	past := now.Add(-24 * time.Hour)

	tests := []struct {
		name        string
		plannedEnd  *time.Time
		now         time.Time
		withinHours int
		want        bool
	}{
		{"nil date always pushes", nil, now, 0, true},
		{"past date always pushes", &past, now, 0, true},
		{"future date without window skips", &future, now, 0, false},
		{"future inside window pushes", &soon, now, 24, true},
		{"future beyond window skips", &future, now, 24, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldPushEnd(tt.plannedEnd, tt.now, tt.withinHours); got != tt.want {
				t.Errorf("ShouldPushEnd(%v, %v, %d) = %v, want %v", tt.plannedEnd, tt.now, tt.withinHours, got, tt.want)
			}
		})
	}
}
