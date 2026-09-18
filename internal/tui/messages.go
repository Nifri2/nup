package tui

import (
	"github.com/Nifri2/nup/internal/pkgset"
	"github.com/Nifri2/nup/internal/update"
)

// packagesMsg carries the result of the initial (or refreshed) package listing.
type packagesMsg struct {
	packages []pkgset.Package
	err      error
}

// latestMsg carries one resolved "latest version" lookup.
type latestMsg struct {
	name   string
	latest string
}

// planMsg carries the prepared update plans.
type planMsg struct {
	plans []*update.Plan
	err   error
}

// progressMsg updates the status line while plans are being prepared.
type progressMsg struct {
	name string
	text string
}

// appliedMsg reports that the lock file was written.
type appliedMsg struct{ err error }

// logMsg is one line of rebuild output.
type logMsg string

// rebuildDoneMsg reports the end of the rebuild.
type rebuildDoneMsg struct{ err error }

// sudoDoneMsg reports the result of the interactive sudo pre-authentication.
type sudoDoneMsg struct{ err error }

// outdatedResultMsg carries the finished outdated scan.
type outdatedResultMsg struct {
	latest  map[string]string
	checked int
}
