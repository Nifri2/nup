package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case packagesMsg:
		if msg.err != nil {
			m.state, m.errMsg = stateFatal, msg.err.Error()
			return m, nil
		}
		m.packages = msg.packages
		m.state = stateTable
		m.status = fmt.Sprintf("%d packages", len(m.packages))
		m.applyFilter()
		return m, nil

	case outdatedResultMsg:
		for i := range m.packages {
			if v, ok := msg.latest[m.packages[i].Name]; ok {
				m.packages[i].Latest = v
			}
		}
		m.status = fmt.Sprintf("checked %d packages", msg.checked)
		m.applyFilter()
		return m, nil

	case progressMsg:
		m.status = msg.name + ": " + msg.text
		return m, nil

	case planMsg:
		if msg.err != nil {
			m.state, m.errMsg = stateFatal, msg.err.Error()
			return m, nil
		}
		if len(msg.plans) == 0 {
			m.state = stateTable
			m.status = "everything selected is already up to date"
			return m, nil
		}
		m.plans = msg.plans
		m.state = stateConfirm
		m.viewport.SetContent(ui.RenderSummary(m.plans, ui.SummaryOptions{}))
		m.viewport.GotoTop()
		return m, nil

	case appliedMsg:
		if msg.err != nil {
			m.state, m.errMsg = stateFatal, msg.err.Error()
			return m, nil
		}
		m.status = "wrote " + m.deps.LockPath
		action := m.deps.Config.AfterUpdate
		if action == config.ActionAsk {
			action = config.ActionSwitch
		}
		if action == config.ActionNone {
			m.state = stateTable
			return m, m.reload()
		}
		m.pendingAction = action
		m.logs = nil
		m.state = stateRebuild
		if needsSudoPassword(m.deps.Config.RebuildArgs(action, m.deps.Engine.FlakeDir, m.deps.Engine.Host)) {
			m.status = "waiting for sudo…"
			return m, m.sudoPreauth()
		}
		return m, m.startRebuild(action)

	case sudoDoneMsg:
		if msg.err != nil {
			m.state, m.errMsg = stateFatal, "sudo: "+msg.err.Error()
			return m, nil
		}
		return m, m.startRebuild(m.pendingAction)

	case logMsg:
		m.logs = append(m.logs, string(msg))
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		m.viewport.GotoBottom()
		return m, m.waitForLog()

	case rebuildDoneMsg:
		if msg.err != nil {
			m.status = ui.S().Red.Render("rebuild failed: " + msg.err.Error())
		} else {
			m.status = ui.S().Green.Render("rebuild finished")
		}
		m.rebuildDone = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The filter input swallows every key except escape and enter.
	if m.filtering {
		switch msg.String() {
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.applyFilter()
			return m, nil
		case "enter":
			m.filtering = false
			m.filter.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
		return m, cmd
	}

	switch m.state {
	case stateHelp:
		m.state = m.prev
		return m, nil

	case stateConfirm:
		switch {
		case key.Matches(msg, m.keys.Confirm):
			m.state = statePreparing
			m.status = "writing lock file…"
			return m, m.applyPlans()
		case key.Matches(msg, m.keys.Deny), key.Matches(msg, m.keys.Quit):
			m.state = stateTable
			m.status = "cancelled"
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case stateRebuild:
		if m.rebuildDone && (key.Matches(msg, m.keys.Quit) || key.Matches(msg, m.keys.Escape)) {
			m.state = stateTable
			m.rebuildDone = false
			return m, m.reload()
		}
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case stateFatal:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Escape) {
			m.state, m.errMsg = stateTable, ""
			return m, nil
		}
		return m, nil

	case stateLoading, statePreparing:
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.prev, m.state = m.state, stateHelp
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filter.Focus()
		return m, nil

	case key.Matches(msg, m.keys.Escape):
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
		}
		return m, nil

	case key.Matches(msg, m.keys.Toggle):
		if p, ok := m.cursorPackage(); ok {
			if m.selected[p.Name] {
				delete(m.selected, p.Name)
			} else {
				m.selected[p.Name] = true
			}
			m.refreshRows()
			m.table.MoveDown(1)
		}
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		for _, p := range m.shown {
			m.selected[p.Name] = true
		}
		m.refreshRows()
		return m, nil

	case key.Matches(msg, m.keys.ClearAll):
		m.selected = map[string]bool{}
		m.refreshRows()
		return m, nil

	case key.Matches(msg, m.keys.Outdated):
		m.status = "checking for newer versions…"
		return m, tea.Batch(m.checkOutdated(), m.spinner.Tick)

	case key.Matches(msg, m.keys.Refresh):
		m.state = stateLoading
		m.status = "re-evaluating…"
		return m, tea.Batch(m.reload(), m.spinner.Tick)

	case key.Matches(msg, m.keys.Reset):
		targets := m.targets()
		if len(targets) == 0 {
			return m, nil
		}
		names := make([]string, 0, len(targets))
		for _, p := range targets {
			names = append(names, p.Attr)
		}
		removed, err := m.deps.Engine.Unpin(names)
		if err != nil {
			m.state, m.errMsg = stateFatal, err.Error()
			return m, nil
		}
		if len(removed) == 0 {
			m.status = "none of the selected packages is pinned"
			return m, nil
		}
		m.status = "removed pin for " + strings.Join(removed, ", ")
		m.selected = map[string]bool{}
		return m, m.reload()

	case key.Matches(msg, m.keys.Update):
		targets := m.targets()
		if len(targets) == 0 {
			return m, nil
		}
		m.state = statePreparing
		m.status = fmt.Sprintf("preparing %d update(s)…", len(targets))
		return m, tea.Batch(m.preparePlans(targets), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// targets returns the selected packages, or the row under the cursor when
// nothing is selected.
func (m *Model) targets() []pkgset.Package {
	if len(m.selected) > 0 {
		var out []pkgset.Package
		for _, p := range m.packages {
			if m.selected[p.Name] {
				out = append(out, p)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}
	if p, ok := m.cursorPackage(); ok {
		return []pkgset.Package{p}
	}
	return nil
}

func (m *Model) cursorPackage() (pkgset.Package, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.shown) {
		return pkgset.Package{}, false
	}
	return m.shown[i], true
}

// reload re-runs the package listing, bypassing the cache so a just-written
// lock file is reflected.
func (m *Model) reload() tea.Cmd {
	m.deps.Refresh = true
	return m.loadPackages()
}

// applyFilter narrows the visible rows to the current filter text.
func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.shown = m.shown[:0]
	for _, p := range m.packages {
		if q == "" || strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Attr), q) {
			m.shown = append(m.shown, p)
		}
	}
	m.refreshRows()
}

// refreshRows rebuilds the table rows, keeping the cursor in range.
func (m *Model) refreshRows() {
	// layout decides whether the LATEST column exists, so it runs first; the
	// widget also indexes into the column list while rendering rows.
	m.layout()

	rows := make([]table.Row, 0, len(m.shown))
	for _, p := range m.shown {
		marker := " "
		if m.selected[p.Name] {
			marker = "●"
		}
		pinned := "-"
		if p.Pinned() {
			pinned = shortRev(p.PinnedRev)
			if p.Stale {
				pinned += "*"
			}
		}
		latest := "-"
		if p.Latest != "" {
			latest = p.Latest
		}
		row := table.Row{marker, p.Name, orDash(p.Version)}
		if m.showLatest {
			row = append(row, latest)
		}
		rows = append(rows, append(row, pinned, string(p.Source)))
	}
	m.table.SetRows(rows)
	if c := m.table.Cursor(); c >= len(rows) {
		m.table.SetCursor(maxInt(0, len(rows)-1))
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
