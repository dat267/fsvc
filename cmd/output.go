package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Column struct {
	Header string
	Path   string
}

// ParseRows extracts a row list from a JSON response body. arrayKey names the
// top-level field; it may hold an array (list endpoints) or a single object
// (show endpoints), in which case a one-row slice is returned.
func ParseRows(body []byte, arrayKey string) ([]map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	raw, ok := doc[arrayKey]
	if !ok {
		return nil, fmt.Errorf("response missing key %q", arrayKey)
	}
	switch t := raw.(type) {
	case []any:
		rows := make([]map[string]any, 0, len(t))
		for _, r := range t {
			if m, ok := r.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
		return rows, nil
	case map[string]any:
		return []map[string]any{t}, nil
	default:
		return nil, fmt.Errorf("unexpected type for %q: %T", arrayKey, raw)
	}
}

func Lookup(m map[string]any, path string) any {
	cur := any(m)
	for _, part := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = mm[part]
		if !ok {
			return nil
		}
	}
	return cur
}

func FormatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// RenderTable renders rows as a GitHub-flavored markdown table.
func RenderTable(columns []Column, rows []map[string]any) string {
	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = len(col.Header)
	}
	for _, row := range rows {
		for i, col := range columns {
			if w := len(FormatValue(Lookup(row, col.Path))); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("| ")
		for i, cell := range cells {
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
			if i < len(cells)-1 {
				b.WriteString(" | ")
			}
		}
		b.WriteString(" |\n")
	}

	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.Header
	}
	writeRow(headers)

	separators := make([]string, len(columns))
	for i := range columns {
		separators[i] = strings.Repeat("-", widths[i])
	}
	writeRow(separators)

	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, col := range columns {
			cells[i] = FormatValue(Lookup(row, col.Path))
		}
		writeRow(cells)
	}

	return b.String()
}

// RenderCSV renders rows as CSV with quoting for values containing
// commas, quotes, or newlines.
func RenderCSV(columns []Column, rows []map[string]any) string {
	var b strings.Builder
	for i, col := range columns {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(col.Header)
	}
	b.WriteString("\n")
	for _, row := range rows {
		for i, col := range columns {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(csvEscape(FormatValue(Lookup(row, col.Path))))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func PrettyJSON(body []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, err
	}
	return json.MarshalIndent(v, "", "  ")
}

// Print renders a response body per the requested format and writes it to
// stdout. json prints the raw body pretty-printed; table and csv are derived
// from the row list under arrayKey.
func Print(body []byte, arrayKey string, columns []Column, format string) error {
	switch format {
	case "json":
		pretty, err := PrettyJSON(body)
		if err != nil {
			return err
		}
		fmt.Println(string(pretty))
	case "csv":
		rows, err := ParseRows(body, arrayKey)
		if err != nil {
			return err
		}
		fmt.Print(RenderCSV(columns, rows))
	default:
		rows, err := ParseRows(body, arrayKey)
		if err != nil {
			return err
		}
		fmt.Print(RenderTable(columns, rows))
	}
	return nil
}
