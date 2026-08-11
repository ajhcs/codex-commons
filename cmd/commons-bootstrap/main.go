package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"codex-commons/internal/bootstrap"
)

func main() { os.Exit(run()) }

func run() int {
	manifestPath := flag.String("manifest", "dogfood/codex-commons/manifest.json", "curated bootstrap manifest")
	baseURL := flag.String("base-url", "", "Commons HTTP origin (required with --apply)")
	apply := flag.Bool("apply", false, "publish through authenticated HTTP and verify; default is offline dry-run")
	allowInsecure := flag.Bool("allow-insecure-http", false, "acknowledge exposing the bootstrap login secret over non-loopback plaintext HTTP")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "bootstrap: positional arguments are not accepted; use --manifest")
		return 2
	}
	file, err := os.Open(*manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: open manifest: %v\n", err)
		return 2
	}
	manifest, err := bootstrap.DecodeManifest(file)
	closeErr := file.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		return 2
	}
	if closeErr != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: close manifest: %v\n", closeErr)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	receipt, err := bootstrap.Run(ctx, manifest, bootstrap.Config{BaseURL: *baseURL, Secret: os.Getenv("COMMONS_BOOTSTRAP_ADMIN_SECRET"), Apply: *apply, AllowInsecureLAN: *allowInsecure})
	output := os.Stdout
	if err != nil {
		output = os.Stderr
	}
	enc := json.NewEncoder(output)
	enc.SetIndent("", "  ")
	_ = enc.Encode(receipt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		return 1
	}
	return 0
}
