//go:build !linux

package main

import (
	"fmt"
	"os"
)

// commons-ops is packaged and verified only for Linux releases. Non-Linux
// builds fail closed here rather than compiling unix signal constants.
func main() {
	fmt.Fprintln(os.Stderr, "commons-ops is only supported on Linux")
	os.Exit(64)
}
