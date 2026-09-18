// Package output renders results as a kubectl-style table, JSON or YAML.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// Format is one of the supported output formats.
type Format string

const (
	// FormatTable is the default human-readable format.
	FormatTable Format = "table"
	// FormatJSON emits a JSON array.
	FormatJSON Format = "json"
	// FormatYAML emits a YAML list.
	FormatYAML Format = "yaml"
)

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON, FormatYAML:
		return Format(s), nil
	default:
		return "", fmt.Errorf("unknown output format %q: expected table, json or yaml", s)
	}
}

// Cell is one table cell with an optional style.
type Cell struct {
	Text  string
	Style lipgloss.Style
}

// Plain makes an unstyled cell.
func Plain(s string) Cell { return Cell{Text: s} }

// Styled makes a styled cell.
func Styled(s string, st lipgloss.Style) Cell { return Cell{Text: s, Style: st} }

// Table is a headed, borderless table.
type Table struct {
	Headers []string
	Rows    [][]Cell
}

// columnGap matches kubectl: three spaces, no borders.
const columnGap = "   "

// Render writes the table. Widths are measured on the unstyled text so escape
// sequences never shift the alignment.
func (t *Table) Render(w io.Writer) error {
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range t.Rows {
		for i, c := range row {
			if i >= len(widths) {
				continue
			}
			if n := lipgloss.Width(c.Text); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []Cell) {
		parts := make([]string, 0, len(cells))
		for i, c := range cells {
			text := c.Style.Render(c.Text)
			// The last column is never padded, so lines have no trailing space.
			if i < len(cells)-1 && i < len(widths) {
				text += strings.Repeat(" ", widths[i]-lipgloss.Width(c.Text))
			}
			parts = append(parts, text)
		}
		b.WriteString(strings.TrimRight(strings.Join(parts, columnGap), " "))
		b.WriteByte('\n')
	}

	header := make([]Cell, len(t.Headers))
	for i, h := range t.Headers {
		header[i] = Plain(strings.ToUpper(h))
	}
	writeRow(header)
	for _, row := range t.Rows {
		writeRow(row)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Render writes data in the requested format. For FormatTable the caller
// supplies the already-built table.
func Render(w io.Writer, format Format, table *Table, data any) error {
	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	case FormatYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return err
		}
		return enc.Close()
	default:
		if table == nil {
			return nil
		}
		return table.Render(w)
	}
}

// Dash is what an empty table cell shows.
const Dash = "-"

// OrDash returns s, or "-" when it is empty.
func OrDash(s string) string {
	if s == "" {
		return Dash
	}
	return s
}
