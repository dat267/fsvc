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
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	threshold := now.Add(-24 * time.Hour)
	created := now.Add(-72 * time.Hour)

	unassigned := int64(-1)
	assigned := int64(5)

	tests := []struct {
		name      string
		responder *int64
		lastMsg   time.Time
		incoming  bool
		created   time.Time
		threshold time.Time
		want      Category
	}{
		{"unassigned", &unassigned, time.Time{}, false, created, threshold, CategoryUnassigned},
		{"assigned no msg old created", &assigned, time.Time{}, false, created, threshold, CategoryStaleAgent},
		{"assigned no msg recent created", &assigned, time.Time{}, false, now, threshold, CategoryNone},
		{"customer replied", &assigned, now, true, created, threshold, CategoryCustomer},
		{"stale agent reply", &assigned, created, false, created, threshold, CategoryStaleAgent},
		{"recent agent reply", &assigned, now, false, created, threshold, CategoryNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.responder, tt.lastMsg, tt.incoming, tt.created, tt.threshold)
			if got != tt.want {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}
