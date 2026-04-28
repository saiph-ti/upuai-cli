package ui

import (
	"fmt"
	"strings"
)

type Table struct {
	headers []string
	rows    [][]string
}

func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

func (t *Table) AddRow(values ...string) {
	t.rows = append(t.rows, values)
}

func (t *Table) Render() string {
	if len(t.headers) == 0 {
		return ""
	}

	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder

	// Header
	for i, h := range t.headers {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(Label.Render(fmt.Sprintf("%-*s", widths[i], strings.ToUpper(h))))
	}
	b.WriteString("\n")

	// Separator
	for i, w := range widths {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(Dim.Render(strings.Repeat("─", w)))
	}
	b.WriteString("\n")

	// Rows
	for _, row := range t.rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			if i > 0 {
				b.WriteString("  ")
			}
			fmt.Fprintf(&b, "%-*s", widths[i], cell)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (t *Table) Print() {
	fmt.Print(t.Render())
}
