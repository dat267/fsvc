package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Column struct {
	Header string
	Path   string
	// Format optionally renders the looked-up value; nil falls back to
	// FormatValue. Used by both table and CSV rendering.
	Format func(any) string
}

// format renders a cell value through the column's formatter.
func (c Column) format(v any) string {
	if c.Format != nil {
		return c.Format(v)
	}
	return FormatValue(v)
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
		widths[i] = utf8.RuneCountInString(col.Header)
	}
	for _, row := range rows {
		for i, col := range columns {
			if w := utf8.RuneCountInString(col.format(Lookup(row, col.Path))); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("| ")
		for i, cell := range cells {
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)))
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
			cells[i] = col.format(Lookup(row, col.Path))
		}
		writeRow(cells)
	}

	return b.String()
}
