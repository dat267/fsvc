package fsapi

import (
	"encoding/json"
	"testing"
)

func TestBuildBody(t *testing.T) {
	tests := []struct {
		name  string
		pairs []string
		want  map[string]any
	}{
		{"string value", []string{"subject=hello"}, map[string]any{"subject": "hello"}},
		{"number value", []string{"priority=1"}, map[string]any{"priority": float64(1)}},
		{"float value", []string{"urgency=1.5"}, map[string]any{"urgency": 1.5}},
		{"bool value", []string{"escalated=true"}, map[string]any{"escalated": true}},
		{"null value", []string{"description=null"}, map[string]any{"description": nil}},
		{"array value", []string{`tags=["a","b"]`}, map[string]any{"tags": []any{"a", "b"}}},
		{"object value", []string{`meta={"a":1}`}, map[string]any{"meta": map[string]any{"a": float64(1)}}},
		{"quoted string with space", []string{`subject="hello world"`}, map[string]any{"subject": "hello world"}},
		{"bare string with space", []string{"subject=hello world"}, map[string]any{"subject": "hello world"}},
		{"equals inside value", []string{"cookie=a=b"}, map[string]any{"cookie": "a=b"}},
		{"nested dotted key", []string{"custom_fields.type_of_ticket_received=Duplicate"}, map[string]any{
			"custom_fields": map[string]any{"type_of_ticket_received": "Duplicate"},
		}},
		{"deep dotted key", []string{"a.b.c=1"}, map[string]any{
			"a": map[string]any{"b": map[string]any{"c": float64(1)}},
		}},
		{"last pair wins", []string{"priority=1", "priority=2"}, map[string]any{"priority": float64(2)}},
		{"empty pairs", nil, map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := BuildBody(tt.pairs)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("invalid JSON output %q: %v", data, err)
			}
			if !jsonEqual(t, got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestBuildBody_InvalidPair(t *testing.T) {
	_, err := BuildBody([]string{"nofield"})
	if err == nil {
		t.Fatal("expected error for pair without '=', got nil")
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
