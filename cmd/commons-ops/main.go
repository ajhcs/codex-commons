package main

import (
	"fmt"
	"io"
	"os"
)

// releaseID is set by the repository-owned release builder. The helper is
// intentionally dormant until a later phase gives it a reviewed operation.
var releaseID = "dev"

const usage = `commons-ops is the packaged Commons operations boundary.

No operational commands are enabled in this release.

Usage:
  commons-ops --help
  commons-ops --version
  commons-ops --build-id
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		_, _ = io.WriteString(stdout, usage)
		return 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--build-id":
			_, _ = fmt.Fprintln(stdout, releaseID)
			return 0
		case "--version":
			_, _ = fmt.Fprintf(stdout, "commons-ops %s\n", releaseID)
			return 0
		}
	}
	_, _ = fmt.Fprintln(stderr, "commons-ops: no operational command is enabled")
	return 2
}
