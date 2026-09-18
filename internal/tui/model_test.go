package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/pkgset"
)

func testModel(t *testing.T, pkgs []pkgset.Package) *Model {
	t.Helper()
	m := New(context.Background(), Deps{Config: config.Default(), Lock: lock.New()})
	m.width, m.height = 100, 30
	m.packages = pkgs
	m.state = stateTable
	m.applyFilter()
	return m
}

func samples() []pkgset.Package {
	return []pkgset.Package{
		{Name: "go-task", Attr: "go-task", Version: "3.38.0", Source: pkgset.SourceSystem},
		{Name: "ripgrep", Attr: "ripgrep", Version: "14.1.1", PinnedRev: "a1b2c3d4", Stale: true, Source: pkgset.SourceSystem},
		{Name: "requests", Attr: "python3Packages.requests", Version: "2.32.3", Source: pkgset.SourceHome},
	}
}

func press(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestFilterNarrowsRows(t *testing.T) {
	m := testModel(t, samples())
	if len(m.shown) != 3 {
		t.Fatalf("expected all rows, got %d", len(m.shown))
	}

	m.Update(press("/"))
	if !m.filtering {
		t.Fatal("/ should open the filter")
	}
	m.Update(press("r"))
	m.Update(press("e"))
	m.Update(press("q"))
	if len(m.shown) != 1 || m.shown[0].Name != "requests" {
		t.Fatalf("filter did not narrow correctly: %+v", m.shown)
	}

	m.Update(press("esc"))
	if m.filtering {
		t.Error("esc should close the filter")
	}
	if len(m.shown) != 3 {
		t.Errorf("esc should clear the filter, got %d rows", len(m.shown))
	}
}

// The filter matches the attribute path too, so a nested package can be found
// by the set it lives in.
func TestFilterMatchesAttrPath(t *testing.T) {
	m := testModel(t, samples())
	m.filter.SetValue("python3Packages")
	m.applyFilter()
	if len(m.shown) != 1 || m.shown[0].Name != "requests" {
		t.Fatalf("got %+v", m.shown)
	}
}

func TestSelectionToggle(t *testing.T) {
	m := testModel(t, samples())
	m.table.SetCursor(0)

	m.Update(press(" "))
	if !m.selected["go-task"] {
		t.Fatal("space should select the row under the cursor")
	}
	// The cursor advances, so repeated presses select consecutive rows.
	if m.table.Cursor() != 1 {
		t.Errorf("cursor = %d, want 1", m.table.Cursor())
	}

	m.table.SetCursor(0)
	m.Update(press(" "))
	if m.selected["go-task"] {
		t.Error("space should toggle the selection off again")
	}
}

func TestSelectAllAndClear(t *testing.T) {
	m := testModel(t, samples())
	m.Update(press("a"))
	if len(m.selected) != 3 {
		t.Fatalf("a should select every visible row, got %d", len(m.selected))
	}

	// "a" only selects what is visible.
	m = testModel(t, samples())
	m.filter.SetValue("ripgrep")
	m.applyFilter()
	m.Update(press("a"))
	if len(m.selected) != 1 {
		t.Fatalf("a should only select filtered rows, got %d", len(m.selected))
	}

	m.Update(press("A"))
	if len(m.selected) != 0 {
		t.Errorf("A should clear the selection, got %d", len(m.selected))
	}
}

// enter acts on the selection, or on the cursor row when nothing is selected.
func TestTargets(t *testing.T) {
	m := testModel(t, samples())
	m.table.SetCursor(1)
	got := m.targets()
	if len(got) != 1 || got[0].Name != "ripgrep" {
		t.Fatalf("without a selection, the cursor row is the target: %+v", got)
	}

	m.selected["go-task"] = true
	m.selected["requests"] = true
	got = m.targets()
	if len(got) != 2 || got[0].Name != "go-task" || got[1].Name != "requests" {
		t.Fatalf("with a selection, only selected rows are targets: %+v", got)
	}
}

func columnIndex(t *testing.T, m *Model, title string) int {
	t.Helper()
	for i, c := range m.table.Columns() {
		if c.Title == title {
			return i
		}
	}
	t.Fatalf("column %q not found", title)
	return -1
}

func TestRowsShowPinAndStaleMarker(t *testing.T) {
	m := testModel(t, samples())
	i := columnIndex(t, m, "PINNED")
	rows := m.table.Rows()
	if rows[0][i] != "-" {
		t.Errorf("an unpinned package should show a dash, got %q", rows[0][i])
	}
	// ripgrep is pinned to a revision other than the flake's own nixpkgs.
	if rows[1][i] != "a1b2c3d*" {
		t.Errorf("a stale pin should be marked, got %q", rows[1][i])
	}
}

// LATEST stays hidden until `u` has actually resolved a version.
func TestLatestColumnAppearsOnlyWhenKnown(t *testing.T) {
	m := testModel(t, samples())
	for _, c := range m.table.Columns() {
		if c.Title == "LATEST" {
			t.Fatal("LATEST should be hidden while unknown")
		}
	}

	pkgs := samples()
	pkgs[0].Latest = "3.39.0"
	m = testModel(t, pkgs)
	i := columnIndex(t, m, "LATEST")
	if m.table.Rows()[0][i] != "3.39.0" {
		t.Errorf("got %q", m.table.Rows()[0][i])
	}
}

// The rows must not be padded wider than the header rule.
func TestTableWidthMatchesColumns(t *testing.T) {
	m := testModel(t, samples())
	total := 0
	for _, c := range m.table.Columns() {
		total += c.Width + cellPadding
	}
	if m.table.Width() != total {
		t.Errorf("table width = %d, want the sum of its columns %d", m.table.Width(), total)
	}
}

func TestSelectionMarkerIsVisible(t *testing.T) {
	m := testModel(t, samples())
	m.table.SetCursor(0)
	m.Update(press(" "))
	if m.table.Rows()[0][0] != "●" {
		t.Errorf("the selected row should carry a marker, got %q", m.table.Rows()[0][0])
	}
}

func TestHelpToggles(t *testing.T) {
	m := testModel(t, samples())
	m.Update(press("?"))
	if m.state != stateHelp {
		t.Fatal("? should open the help")
	}
	m.Update(press("q"))
	if m.state != stateTable {
		t.Error("any key should close the help")
	}
}

func TestQuitFromTable(t *testing.T) {
	m := testModel(t, samples())
	_, cmd := m.Update(press("q"))
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected a quit message")
	}
}

// The table widget binds space and u by default; nup needs them for select and
// outdated, so those bindings must be cleared.
func TestTableKeyMapFreesNupKeys(t *testing.T) {
	km := tableKeyMap()
	for _, k := range km.PageDown.Keys() {
		if k == " " {
			t.Error("space must not page down")
		}
	}
	for _, k := range km.HalfPageUp.Keys() {
		if k == "u" {
			t.Error("u must not scroll")
		}
	}
}

func TestViewDoesNotPanicInEveryState(t *testing.T) {
	for _, st := range []state{stateLoading, stateTable, statePreparing, stateConfirm, stateRebuild, stateHelp, stateFatal} {
		m := testModel(t, samples())
		m.state = st
		m.errMsg = "boom"
		if m.View() == "" {
			t.Errorf("state %d rendered nothing", st)
		}
	}
}
