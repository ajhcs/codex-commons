package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	historicalimport "codex-commons/historical-import"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("commons-history-preview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "historical-import/manifests/codex-commons.v1.json", "historical import manifest")
	snapshotPath := flags.String("current-snapshot", "historical-import/snapshots/codex-commons-current.v1.json", "offline snapshot of current canonical tasks")
	requireEligible := flags.Bool("require-apply-eligible", false, "exit non-zero when evidence or redaction blockers remain")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "history preview: positional arguments are not accepted")
		return 2
	}
	manifestFile, err := os.Open(*manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "history preview: open manifest: %v\n", err)
		return 2
	}
	manifest, _, decodeErr := historicalimport.Decode(manifestFile)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		fmt.Fprintf(stderr, "history preview: %v\n", decodeErr)
		return 2
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "history preview: close manifest: %v\n", closeErr)
		return 2
	}

	snapshotFile, err := os.Open(*snapshotPath)
	if err != nil {
		fmt.Fprintf(stderr, "history preview: open current snapshot: %v\n", err)
		return 2
	}
	snapshot, decodeErr := historicalimport.DecodeSnapshot(snapshotFile)
	closeErr = snapshotFile.Close()
	if decodeErr != nil {
		fmt.Fprintf(stderr, "history preview: %v\n", decodeErr)
		return 2
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "history preview: close current snapshot: %v\n", closeErr)
		return 2
	}
	sourceIssues := historicalimport.VerifySourceFiles(manifest, ".")
	report, err := historicalimport.BuildPreview(manifest, snapshot, sourceIssues...)
	if err != nil {
		fmt.Fprintf(stderr, "history preview: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "history preview: encode report: %v\n", err)
		return 1
	}
	if *requireEligible && !report.ApplyEligible {
		return 1
	}
	return 0
}
