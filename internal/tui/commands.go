package tui

import (
	"bufio"
	"io"
	"os/exec"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Nifri2/nup/internal/config"
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/update"
)

// loadPackages evaluates the configuration in the background so the UI stays
// responsive while nix works.
func (m *Model) loadPackages() tea.Cmd {
	return func() tea.Msg {
		pkgs, err := m.deps.Lister.List(m.ctx, m.deps.Lock, m.deps.MainRev,
			pkgset.Options{Refresh: m.deps.Refresh})
		return packagesMsg{packages: pkgs, err: err}
	}
}

// checkOutdated resolves the latest version of every package, streaming each
// result into the LATEST column as it arrives.
func (m *Model) checkOutdated() tea.Cmd {
	pkgs := append([]pkgset.Package(nil), m.packages...)
	branch := m.deps.Config.Branch
	engine := m.deps.Engine
	ctx := m.ctx

	return func() tea.Msg {
		const workers = 8
		type job struct{ p pkgset.Package }
		jobs := make(chan job)
		results := make(chan latestMsg, len(pkgs))

		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range jobs {
					latest, _, err := engine.Latest(ctx, j.p.Attr, branch)
					if err != nil || latest == "" {
						continue
					}
					results <- latestMsg{name: j.p.Name, latest: latest}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, p := range pkgs {
				select {
				case <-ctx.Done():
					return
				case jobs <- job{p: p}:
				}
			}
		}()
		wg.Wait()
		close(results)

		found := map[string]string{}
		for r := range results {
			found[r.name] = r.latest
		}
		return outdatedResultMsg{latest: found, checked: len(pkgs)}
	}
}

// preparePlans builds the update plans for the given packages.
func (m *Model) preparePlans(pkgs []pkgset.Package) tea.Cmd {
	engine := m.deps.Engine
	branch := m.deps.Config.Branch
	ctx := m.ctx
	return func() tea.Msg {
		var plans []*update.Plan
		for _, p := range pkgs {
			plan, err := engine.Prepare(ctx, update.Request{
				Name:    p.Name,
				Attr:    p.Attr,
				Branch:  branch,
				Current: p,
			}, nil)
			if err != nil {
				return planMsg{err: err}
			}
			if plan.Unchanged {
				continue
			}
			plans = append(plans, plan)
		}
		return planMsg{plans: plans}
	}
}

// applyPlans writes the lock file.
func (m *Model) applyPlans() tea.Cmd {
	engine := m.deps.Engine
	plans := m.plans
	return func() tea.Msg { return appliedMsg{err: engine.Apply(plans)} }
}

// sudoPreauth asks for the sudo password on the real terminal before the TUI
// takes stdin back for the streamed rebuild log.
func (m *Model) sudoPreauth() tea.Cmd {
	return tea.ExecProcess(exec.Command("sudo", "-v"), func(err error) tea.Msg {
		return sudoDoneMsg{err: err}
	})
}

// needsSudoPassword reports whether a cached sudo ticket is missing.
func needsSudoPassword(argv []string) bool {
	if len(argv) == 0 || argv[0] != "sudo" {
		return false
	}
	return exec.Command("sudo", "-n", "true").Run() != nil
}

// startRebuild runs the rebuild, feeding its output into a channel the UI
// drains one line at a time.
func (m *Model) startRebuild(action config.Action) tea.Cmd {
	m.logCh = make(chan logMsg, 256)
	m.logDone = make(chan error, 1)

	engine := m.deps.Engine
	cfg := m.deps.Config
	ctx := m.ctx
	ch := m.logCh
	done := m.logDone

	go func() {
		pr, pw := io.Pipe()
		go func() {
			scanner := bufio.NewScanner(pr)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				ch <- logMsg(strings.TrimRight(scanner.Text(), "\r"))
			}
			close(ch)
		}()
		err := engine.Rebuild(ctx, cfg, action, nil, pw, pw)
		_ = pw.Close()
		done <- err
	}()

	return tea.Batch(m.waitForLog(), m.spinner.Tick)
}

// waitForLog returns the next log line, or the final result when the stream
// ends.
func (m *Model) waitForLog() tea.Cmd {
	ch := m.logCh
	done := m.logDone
	return func() tea.Msg {
		line, ok := <-ch
		if ok {
			return line
		}
		return rebuildDoneMsg{err: <-done}
	}
}
