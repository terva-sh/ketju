package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/terva-sh/ketju/internal/buildinfo"
)

// buildInfo is buildinfo.Get, a seam for TestVersionCommand.
var buildInfo = buildinfo.Get

func runVersion(args []string) error { return versionMain(os.Stdout, args) }

// versionMain prints the one line that names this build: its version, its
// commit, the Go version and the platform (TKT-01M29CXK3S).
func versionMain(w io.Writer, args []string) error {
	if len(args) != 0 {
		return errUsage{errors.New("usage: ketju version (it takes no arguments)")}
	}
	_, err := fmt.Fprintln(w, buildInfo().String())
	return err
}
