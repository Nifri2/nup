package nix

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Call records one invocation made against a FakeRunner.
type Call struct {
	Name string
	Args []string
}

// String renders the call the way it would appear on a command line.
func (c Call) String() string { return c.Name + " " + strings.Join(c.Args, " ") }

// Response is a canned answer for a matched command.
type Response struct {
	Result *Result
	Err    error
}

// FakeRunner is a Runner for tests. Responses are looked up by the first
// matching substring in Matches, which is checked against the full command
// line; Handler takes precedence when set.
type FakeRunner struct {
	// Matches maps a substring of the command line to a response.
	Matches map[string]Response
	// Handler, when set, answers everything Matches does not.
	Handler func(name string, args []string) (*Result, error)
	// StreamOutput is written to stdout by Stream.
	StreamOutput string
	// StreamErr is returned by Stream.
	StreamErr error

	mu    sync.Mutex
	calls []Call
}

// NewFake builds a FakeRunner from substring/stdout pairs.
func NewFake(responses map[string]string) *FakeRunner {
	m := make(map[string]Response, len(responses))
	for k, v := range responses {
		m[k] = Response{Result: &Result{Stdout: v}}
	}
	return &FakeRunner{Matches: m}
}

// Run implements Runner.
func (f *FakeRunner) Run(_ context.Context, name string, args []string) (*Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Name: name, Args: append([]string(nil), args...)})
	f.mu.Unlock()

	line := name + " " + strings.Join(args, " ")
	// Prefer the most specific match so general patterns can act as fallbacks.
	best, bestLen := Response{}, -1
	for pat, resp := range f.Matches {
		if strings.Contains(line, pat) && len(pat) > bestLen {
			best, bestLen = resp, len(pat)
		}
	}
	if bestLen >= 0 {
		if best.Err != nil {
			return best.Result, best.Err
		}
		return best.Result, nil
	}
	if f.Handler != nil {
		return f.Handler(name, args)
	}
	return &Result{ExitCode: 1, Stderr: "no fake response for: " + line},
		&Error{Name: name, Args: args, Code: 1, Stderr: "no fake response for: " + line,
			Err: fmt.Errorf("unexpected command")}
}

// Stream implements Runner.
func (f *FakeRunner) Stream(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Name: name, Args: append([]string(nil), args...)})
	f.mu.Unlock()
	if f.StreamOutput != "" && stdout != nil {
		_, _ = io.WriteString(stdout, f.StreamOutput)
	}
	return f.StreamErr
}

// Calls returns every recorded invocation.
func (f *FakeRunner) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Called reports whether any recorded call contains the given substring.
func (f *FakeRunner) Called(substr string) bool {
	for _, c := range f.Calls() {
		if strings.Contains(c.String(), substr) {
			return true
		}
	}
	return false
}
