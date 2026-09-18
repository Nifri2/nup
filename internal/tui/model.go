// Package tui implements the interactive interface. It owns no update logic:
// everything it does goes through internal/update, exactly like the CLI.
package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/lock"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/ui"
	"github.com/Nifri2/nup/internal/update"
)

type state int

const (
	stateLoading state = iota
	stateTable
	statePreparing
	stateConfirm
	stateRebuild
	stateHelp
	stateFatal
)

// Deps is everything the TUI needs from the outside.
type Deps struct {
	Engine *update.Engine
	Lister *pkgset.Lister
	Config *config.Config
	Lock   *lock.File
	// MainRev is the nixpkgs revision of the flake input, for staleness.
	MainRev string
	// LockPath is shown after a successful write.
	LockPath string
	// Refresh forces a fresh evaluation on start.
	Refresh bool
}

// Model is the bubbletea model.
type Model struct {
	deps Deps
	ctx  context.Context

	state    state
	prev     state
	keys     KeyMap
	spinner  spinner.Model
	table    table.Model
	filter   textinput.Model
	viewport viewport.Model
	help     help.Model

	packages []pkgset.Package
	shown    []pkgset.Package
	selected map[string]bool

	plans     []*update.Plan
	status    string
	errMsg    string
	filtering bool

	logCh         chan logMsg
	logDone       chan error
	logs          []string
	rebuildDone   bool
	showLatest    bool
	pendingAction config.Action

	width  int
	height int
}

// New builds the model.
func New(ctx context.Context, deps Deps) *Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = ui.S().Blue

	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter"
	fi.CharLimit = 64

	tbl := table.New(table.WithFocused(true))
	tbl.KeyMap = tableKeyMap()
	tbl.SetStyles(tableStyles())

	return &Model{
		deps:     deps,
		ctx:      ctx,
		state:    stateLoading,
		keys:     DefaultKeyMap(),
		spinner:  sp,
		table:    tbl,
		filter:   fi,
		viewport: viewport.New(80, 20),
		help:     help.New(),
		selected: map[string]bool{},
		status:   "evaluating the configuration…",
		// Sensible defaults until the first WindowSizeMsg arrives.
		width:  80,
		height: 24,
	}
}

// tableKeyMap frees the keys nup needs: bubbles binds space to page-down and u
// to half-page-up by default, which would collide with select and outdated.
func tableKeyMap() table.KeyMap {
	km := table.DefaultKeyMap()
	km.PageDown = key.NewBinding(key.WithKeys("f", "pgdown"))
	km.PageUp = key.NewBinding(key.WithKeys("b", "pgup"))
	km.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	km.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	km.GotoTop = key.NewBinding(key.WithKeys("g", "home"))
	km.GotoBottom = key.NewBinding(key.WithKeys("G", "end"))
	return km
}

func tableStyles() table.Styles {
	st := table.DefaultStyles()
	s := ui.S()
	st.Header = st.Header.BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).BorderForeground(lipgloss.Color("240")).Bold(true)
	st.Selected = s.Selected.Reverse(true)
	return st
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadPackages())
}

// Run starts the program.
func Run(ctx context.Context, deps Deps) error {
	m := New(ctx, deps)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}
