package cmd

import (
	"os"

	"golang.org/x/term"
)

func isTerminal(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}
