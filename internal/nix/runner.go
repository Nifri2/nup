// Package nix wraps every external command nup runs. Everything goes through
// the Runner interface so tests can drive the whole tool without a real nix.
package nix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Result is the captured outcome of a command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes external commands.
type Runner interface {
	// Run captures stdout and stderr.
	Run(ctx context.Context, name string, args []string) (*Result, error)
	// Stream forwards output live, for long-running commands like a rebuild.
	Stream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Error carries the failing command together with nix's own stderr, which is
// usually where the real explanation lives.
type Error struct {
	Name   string
	Args   []string
	Stderr string
	Code   int
	Err    error
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("command failed: %s", shortCommand(e.Name, e.Args))
	if e.Code != 0 {
		msg = fmt.Sprintf("%s (exit %d)", msg, e.Code)
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		msg += "\n" + indent(tail(s, 40))
	} else if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

// shortCommand renders the command line for an error message. Long arguments --
// nup passes whole Nix expressions to --apply -- are abbreviated so the message
// stays readable; the interesting part is nix's stderr below it.
func shortCommand(name string, args []string) string {
	const maxArg = 120
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, name)
	for _, a := range args {
		if len(a) > maxArg {
			a = a[:maxArg-1] + "…"
		}
		if strings.ContainsAny(a, " \t\n") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

// tail keeps the last n lines; nix traces can be very long.
func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return "... (" + fmt.Sprint(len(lines)-n) + " earlier lines omitted)\n" + strings.Join(lines[len(lines)-n:], "\n")
}

// ExecRunner runs commands for real.
type ExecRunner struct {
	// Timeout bounds a captured run. Zero means no timeout.
	Timeout time.Duration
	// Env replaces the inherited environment when non-nil.
	Env []string
}

// NewExecRunner returns a runner with the given per-command timeout.
func NewExecRunner(timeout time.Duration) *ExecRunner { return &ExecRunner{Timeout: timeout} }

// env returns the child environment. Captured runs get NO_COLOR because nix
// colours even machine-readable output in some versions and parsers should
// never have to guess; streamed runs keep colour, since the user reads them.
func (r *ExecRunner) env(noColor bool) []string {
	base := r.Env
	if base == nil {
		base = os.Environ()
	}
	out := append([]string(nil), base...)
	if noColor {
		out = append(out, "NO_COLOR=1")
	}
	return out
}

// Run implements Runner.
func (r *ExecRunner) Run(ctx context.Context, name string, args []string) (*Result, error) {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = r.env(true)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: cmd.ProcessState.ExitCode()}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return res, &Error{Name: name, Args: args, Stderr: res.Stderr, Code: res.ExitCode,
				Err: fmt.Errorf("timed out after %s", r.Timeout)}
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return res, &Error{Name: name, Args: args, Stderr: res.Stderr, Code: res.ExitCode, Err: err}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return res, &Error{Name: name, Args: args, Err: fmt.Errorf("%q not found in PATH", name)}
		}
		return res, &Error{Name: name, Args: args, Stderr: res.Stderr, Err: err}
	}
	return res, nil
}

// Stream implements Runner.
func (r *ExecRunner) Stream(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = r.env(false)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		return &Error{Name: name, Args: args, Code: code, Err: err}
	}
	return nil
}
