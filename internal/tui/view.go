package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/ui"
)

const (
	headerHeight = 2
	footerHeight = 2
	markerWidth  = 1
	// cellPadding is the one space bubbles' table puts on either side of a cell.
	cellPadding = 2
)

func shortRev(rev string) string { return lock.ShortRev(rev) }

// layout recomputes widget sizes for the current terminal size.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	body := m.height - headerHeight - footerHeight
	if body < 3 {
		body = 3
	}

	// LATEST only appears once `u` has filled it in.
	showLatest := false
	for _, p := range m.shown {
		if p.Latest != "" {
			showLatest = true
			break
		}
	}

	name, ver, latest := 12, 7, 6
	for _, p := range m.shown {
		name = maxInt(name, lipgloss.Width(p.Name))
		ver = maxInt(ver, lipgloss.Width(p.Version))
		latest = maxInt(latest, lipgloss.Width(p.Latest))
	}
	// A short revision plus the stale marker.
	const pinned, source = 8, 6

	cols := []table.Column{
		{Title: " ", Width: markerWidth},
		{Title: "NAME", Width: name},
		{Title: "VERSION", Width: ver},
	}
	if showLatest {
		cols = append(cols, table.Column{Title: "LATEST", Width: latest})
	}
	cols = append(cols,
		table.Column{Title: "PINNED", Width: pinned},
		table.Column{Title: "SOURCE", Width: source},
	)
	m.showLatest = showLatest

	// Each column is rendered with one space of padding on either side; the
	// name column gives back whatever does not fit.
	total := 0
	for _, c := range cols {
		total += c.Width + cellPadding
	}
	if over := total - m.width; over > 0 {
		shrink := minInt(over, name-12)
		cols[1].Width -= shrink
		total -= shrink
	}

	m.table.SetColumns(cols)
	m.table.SetHeight(body)
	// Sizing the table to its columns keeps the rows from being padded wider
	// than the header rule.
	m.table.SetWidth(minInt(total, m.width))

	m.viewport.Width = m.width
	m.viewport.Height = body
	m.help.Width = m.width
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// View implements tea.Model.
func (m *Model) View() string {
	s := ui.S()
	switch m.state {
	case stateLoading:
		return m.centered(m.spinner.View() + " " + m.status)

	case statePreparing:
		return m.centered(m.spinner.View() + " " + m.status)

	case stateFatal:
		return m.frame(
			s.Red.Render("error"),
			s.Red.Render(m.errMsg),
			s.Dim.Render("esc to go back · q to quit"),
		)

	case stateHelp:
		return m.frame(s.Bold.Render("nup — keys"), m.help.FullHelpView(m.keys.FullHelp()),
			s.Dim.Render("any key to go back"))

	case stateConfirm:
		n := len(m.plans)
		return m.frame(
			s.Bold.Render(fmt.Sprintf("%d package(s) to update", n)),
			m.viewport.View(),
			s.Bold.Render("y")+s.Dim.Render(" apply · ")+s.Bold.Render("n/esc")+
				s.Dim.Render(" cancel · ↑/↓ scroll"),
		)

	case stateRebuild:
		title := m.spinner.View() + " rebuilding"
		hint := s.Dim.Render("↑/↓ scroll")
		if m.rebuildDone {
			title = m.status
			hint = s.Dim.Render("esc to go back · q to quit")
		}
		return m.frame(title, m.viewport.View(), hint)
	}

	return m.frame(m.headerLine(), m.table.View(), m.footerLine())
}

func (m *Model) headerLine() string {
	s := ui.S()
	if m.filtering || m.filter.Value() != "" {
		return m.filter.View()
	}
	left := s.Bold.Render("nup")
	right := s.Dim.Render(fmt.Sprintf("%s · %d shown", m.status, len(m.shown)))
	if n := len(m.selected); n > 0 {
		right = s.Selected.Render(fmt.Sprintf("%d selected", n)) + s.Dim.Render(" · ") + right
	}
	return left + "  " + right
}

func (m *Model) footerLine() string {
	return ui.S().Dim.Render(m.help.ShortHelpView(m.keys.ShortHelp()))
}

// frame stacks header, body and footer without ever exceeding the window.
func (m *Model) frame(header, body, footer string) string {
	return strings.Join([]string{header, "", body, footer}, "\n")
}

func (m *Model) centered(text string) string {
	if m.width == 0 || m.height == 0 {
		return text
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, text)
}
