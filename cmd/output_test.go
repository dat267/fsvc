package cmd

import (
	"strings"
	"testing"
)

func TestParseRows_List(t *testing.T) {
	body := []byte(`{"tickets":[{"id":1},{"id":2}],"meta":{}}`)
	rows, err := ParseRows(body, "tickets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["id"] != float64(1) {
		t.Errorf("expected id 1, got %v", rows[0]["id"])
	}
}

func TestParseRows_Single(t *testing.T) {
	body := []byte(`{"user":{"id":7,"name":"Sal"}}`)
	rows, err := ParseRows(body, "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["name"] != "Sal" {
		t.Errorf("expected name Sal, got %v", rows[0]["name"])
	}
}

func TestParseRows_MissingKey(t *testing.T) {
	_, err := ParseRows([]byte(`{"tickets":[]}`), "users")
	if err == nil || !strings.Contains(err.Error(), "users") {
		t.Errorf("expected missing key error, got %v", err)
	}
}

func TestParseRows_Malformed(t *testing.T) {
	_, err := ParseRows([]byte(`not json`), "tickets")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestLookup(t *testing.T) {
	row := map[string]any{
		"id": float64(1),
		"requester": map[string]any{
			"name": "Omar",
		},
	}
	if Lookup(row, "id") != float64(1) {
		t.Error("expected id lookup")
	}
	if Lookup(row, "requester.name") != "Omar" {
		t.Error("expected nested lookup")
	}
	if Lookup(row, "missing") != nil {
		t.Error("expected nil for missing path")
	}
	if Lookup(row, "requester.missing") != nil {
		t.Error("expected nil for missing nested path")
	}
}

func TestFormatValue(t *testing.T) {
	if FormatValue(nil) != "" {
		t.Error("expected empty string for nil")
	}
	if FormatValue("x") != "x" {
		t.Error("expected string passthrough")
	}
	if FormatValue(float64(10100)) != "10100" {
		t.Error("expected integer float to render without decimals")
	}
	if FormatValue(float64(1.5)) != "1.5" {
		t.Error("expected float to render")
	}
	if FormatValue(true) != "true" {
		t.Error("expected bool to render")
	}
}

func TestRenderTable_Markdown(t *testing.T) {
	columns := []Column{
		{Header: "ID", Path: "id"},
		{Header: "Name", Path: "requester.name"},
	}
	rows := []map[string]any{
		{"id": float64(10100), "requester": map[string]any{"name": "Omar"}},
		{"id": float64(10101), "requester": map[string]any{"name": "Lina"}},
	}

	got := RenderTable(columns, rows)
	want := `| ID    | Name |
| ----- | ---- |
| 10100 | Omar |
| 10101 | Lina |
`
	if got != want {
		t.Errorf("unexpected table:\n%s", got)
	}
}

func TestRenderTable_MissingValue(t *testing.T) {
	columns := []Column{{Header: "ID", Path: "id"}}
	rows := []map[string]any{{"other": float64(1)}}
	got := RenderTable(columns, rows)
	if !strings.Contains(got, "|    |\n") {
		t.Errorf("expected empty padded cell for missing value:\n%s", got)
	}
}

func TestRenderCSV(t *testing.T) {
	columns := []Column{{Header: "ID", Path: "id"}, {Header: "Name", Path: "name"}}
	rows := []map[string]any{
		{"id": float64(1), "name": "Omar"},
		{"id": float64(2), "name": "Lina, Noor"},
	}
	got := RenderCSV(columns, rows)
	want := "ID,Name\n1,Omar\n2,\"Lina, Noor\"\n"
	if got != want {
		t.Errorf("unexpected csv:\n%s", got)
	}
}

func TestPrettyJSON(t *testing.T) {
	pretty, err := PrettyJSON([]byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(pretty), "\n") {
		t.Errorf("expected indented JSON, got %q", pretty)
	}
	if _, err := PrettyJSON([]byte(`bad`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 40); got != "short" {
		t.Errorf("expected short unchanged, got %q", got)
	}
	if got := truncate("this is a very long subject line that should be truncated", 20); got != "this is a very lo..." {
		t.Errorf("expected truncated with ellipsis, got %q", got)
	}
}
