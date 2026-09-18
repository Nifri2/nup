// Package ui holds the lipgloss styles and renderers shared by the CLI and the
// TUI, so an update summary looks identical wherever it is printed.
package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Styles is the palette nup renders with.
type Styles struct {
	Header    lipgloss.Style
	Dim       lipgloss.Style
	Bold      lipgloss.Style
	Red       lipgloss.Style
	Green     lipgloss.Style
	Yellow    lipgloss.Style
	Blue      lipgloss.Style
	Magenta   lipgloss.Style
	Warning   lipgloss.Style
	Selected  lipgloss.Style
	colorized bool
}

var current = New(true)

// New builds a palette. With color disabled every style is a no-op, which keeps
// call sites free of conditionals.
func New(color bool) *Styles {
	if !color {
		plain := lipgloss.NewStyle()
		return &Styles{
			Header: plain, Dim: plain, Bold: plain, Red: plain, Green: plain,
			Yellow: plain, Blue: plain, Magenta: plain, Warning: plain, Selected: plain,
		}
	}
	return &Styles{
		Header:    lipgloss.NewStyle().Bold(true),
		Dim:       lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
		Bold:      lipgloss.NewStyle().Bold(true),
		Red:       lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		Green:     lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		Yellow:    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		Blue:      lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		Magenta:   lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		Warning:   lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		Selected:  lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		colorized: true,
	}
}

// Color reports whether the current palette emits escape sequences.
func (s *Styles) Color() bool { return s.colorized }

// S returns the palette in use.
func S() *Styles { return current }

// SetColor switches the global palette.
func SetColor(enabled bool) {
	current = New(enabled)
	if !enabled {
		lipgloss.SetColorProfile(termenvASCII)
	}
}

// ShouldColor decides whether to colourize: an explicit --no-color wins, then
// NO_COLOR, then whether the stream is a terminal at all.
func ShouldColor(noColorFlag bool, f *os.File) bool {
	if noColorFlag {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
