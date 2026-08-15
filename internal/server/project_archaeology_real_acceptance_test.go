package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"codex-commons/internal/application"
	"codex-commons/internal/codexauth"
	"codex-commons/internal/domain"
	commonsstore "codex-commons/internal/store"
)

const realHistorianAcceptanceOptIn = "GO_ONE_READ_ONLY_HISTORIAN"
const realHistorianCodexSHA256 = "cb0a15567e9a60a5820d54b0f6ae86d504dc3805c1eab21a47f70e3eb7b73a40"
const realHistorianCodeModeHostSHA256 = "00ecf5d040865b97884c488883abd342581c2a432debe7a54e4646bceee3d2d6"
const realHistorianBwrapSHA256 = "77360cb751ccedc5971391444ac86a8a33c15b04d6b4a6fe45f5d25496e62c4c"
const realHistorianZshSHA256 = "67faaaa89242c4a332e16e508a1977cffc24bf7fca31d4411cdfd101f3831ef3"
const realHistorianRgSHA256 = "e62198eb19b136b88c330af83647b5a962cb99b6b1f066758568f12de1974849"
const realHistorianPackageSHA256 = "00f66f11cc7d5c4133d500b4aae6ed4975608d6b040eefd56dc1ff343566e8cf"
const realRuntimePreflightOptIn = "GO_NO_TASK_RUNTIME_PREFLIGHT"

var realHistorianRuntimeHashes = acceptanceRuntimeBundleHashes{
	Codex: realHistorianCodexSHA256, Host: realHistorianCodeModeHostSHA256,
	Bwrap: realHistorianBwrapSHA256, Zsh: realHistorianZshSHA256,
	Rg: realHistorianRgSHA256, Package: realHistorianPackageSHA256,
}

// TestRealHistorianIsolatedAcceptance is an opt-in, one-task production-path
// acceptance. Ordinary test runs always skip it. A successful run deliberately
// preserves its private synthetic repository and SQLite receipt for human
// inspection of the visible non-ephemeral Codex task.
func TestRealHistorianIsolatedAcceptance(t *testing.T) {
	if os.Getenv("COMMONS_REAL_HISTORIAN_ACCEPTANCE") != realHistorianAcceptanceOptIn {
		t.Skip("real historian acceptance is not explicitly authorized")
	}
	codexBin := strings.TrimSpace(os.Getenv("COMMONS_ACCEPTANCE_CODEX_BIN"))
	if !filepath.IsAbs(codexBin) || filepath.Clean(codexBin) != codexBin {
		t.Fatal("COMMONS_ACCEPTANCE_CODEX_BIN must be an absolute clean path")
	}
	repoRoot, err := acceptanceRepositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	outerBefore, err := acceptanceGitSnapshot(context.Background(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		t.Fatal("a private absolute user home is required")
	}
	runRoot, err := os.MkdirTemp(home, ".codex-commons-historian-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(runRoot, 0o700); err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	workspace := filepath.Join(runRoot, "synthetic-project")
	if err = os.Mkdir(workspace, 0o700); err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	const history = `# Synthetic project history

This repository exists only for isolated Codex Commons acceptance.

- Evidence time: 2026-08-13T00:00:00Z
- Historical task key: synthetic-history
- Historical task title: Document the synthetic project
- Historical task state: done
- Source session: synthetic-source-session
- Cite this repository-relative document with its exact SHA-256 digest.
`
	if err = os.WriteFile(filepath.Join(workspace, "HISTORY.md"), []byte(history), 0o600); err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 20*time.Second)
	if err = acceptanceInitializeGit(setupCtx, workspace); err != nil {
		setupCancel()
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	setupCancel()
	syntheticBefore, err := acceptanceGitSnapshot(context.Background(), workspace)
	if err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	workspaceBefore, err := acceptanceTreeDigest(workspace)
	if err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	repository, err := commonsstore.Open(ctx, filepath.Join(runRoot, "acceptance.sqlite3"))
	if err != nil {
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	runtimeClient, err := acceptanceRuntimeBundlePreflight(ctx, codexBin, realHistorianRuntimeHashes, func() (codexauth.Client, error) {
		return codexauth.NewManagedProcess(ctx, codexauth.ProcessConfig{Executable: codexBin, Env: codexauth.ApprovedEnvironment(os.Environ()), EnableExperimentalDynamicTools: true})
	})
	if err != nil {
		_ = repository.Close()
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	client, ok := runtimeClient.(*codexauth.ManagedProcessClient)
	if !ok {
		_ = runtimeClient.Close()
		_ = repository.Close()
		_ = os.RemoveAll(runRoot)
		t.Fatal("acceptance preflight did not return the production managed Codex client")
	}
	acceptanceEvidence(t, "pinned_runtime", "verified", map[string]any{"version": "codex-cli 0.147.0", "sha256": realHistorianCodexSHA256})
	service := application.New(repository, nil, nil)
	bridge := &codexArchaeologyBridge{client: client, catalog: map[string]string{"synthetic-candidate": workspace}}
	service.ConfigureProjectArchaeology(bridge, nil)
	if err = service.ConfigureNativeProjectArchaeology(ctx, bridge, domain.HumanLocalPrincipal); err != nil {
		_ = client.Close()
		_ = repository.Close()
		_ = os.RemoveAll(runRoot)
		t.Fatal(err)
	}
	queued := false
	cleanup := acceptanceCleanup{RunRoot: runRoot, Workspace: workspace, RepoRoot: repoRoot, SyntheticBefore: syntheticBefore, WorkspaceBefore: workspaceBefore, OuterBefore: outerBefore}
	defer func() {
		cleanup.Queued = queued
		cleanup.Run(t, service, repository, client)
	}()

	if err = bridge.Available(ctx); err != nil {
		t.Fatalf("required gpt-5.6-luna/max capability unavailable: %v", err)
	}
	authCtx, authCancel := context.WithTimeout(ctx, 5*time.Second)
	err = acceptanceRequireSignedIn(authCtx, client)
	authCancel()
	if err != nil {
		t.Fatal(err)
	}
	acceptanceEvidence(t, "account/read", "signed_in", nil)
	beforeInventory, err := client.ListHistorianTasks(ctx, workspace)
	if err != nil || len(beforeInventory) != 0 {
		t.Fatalf("synthetic workspace must have zero pre-existing tasks: count=%d err=%v", len(beforeInventory), err)
	}
	acceptanceEvidence(t, "model/list+thread/list", "supported_empty", map[string]any{"task_count": 0})

	value, err := repository.ReplaceArchaeologyDiscovery(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "isolated-acceptance-discover"}, domain.ArchaeologyDiscovery{SourceRootsScanned: 1, Candidates: []domain.ArchaeologyCandidate{{
		ID: "synthetic-candidate", CanonicalProjectID: "acceptance-synthetic", Name: "Synthetic historian acceptance", PathLabel: "Synthetic historian acceptance",
		HasGit: true, HasDocs: true, FromConfiguredRoot: true, DurationMinSeconds: 1, DurationMaxSeconds: 120, RelativeCost: "low", PrivacyNote: "Synthetic fixture only.",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	value, err = repository.ConfigureArchaeology(ctx, domain.ArchaeologyMutation{Principal: domain.HumanLocalPrincipal, RequestID: "isolated-acceptance-configure", BaseRevision: value.Revision, Config: domain.ArchaeologyConfig{
		SelectedProjectIDs: []string{"synthetic-candidate"}, Depth: "quick", Sources: domain.ArchaeologySources{Git: true, Docs: true}, MaxConcurrency: 1,
	}})
	if err != nil {
		t.Fatal(err)
	}
	beforeCanonical, err := acceptanceCanonicalDigest(ctx, repository.DB())
	if err != nil {
		t.Fatal(err)
	}
	// From this point onward cleanup must conservatively assume that the
	// non-idempotent launch boundary may have been crossed, even if Start returns
	// an error before its response reaches this caller.
	queued = true
	started, err := service.StartProjectArchaeology(ctx, domain.HumanLocalPrincipal, "isolated-acceptance-start", application.ArchaeologyTransitionRequest{BaseRevision: value.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if started.Handoff == nil || started.Handoff.Concurrency != 1 || len(started.Handoff.Tasks) != 1 {
		t.Fatal("expected exactly one queued job at concurrency one")
	}
	jobID := started.Handoff.Tasks[0].JobID

	bound := acceptanceWaitForJob(t, ctx, repository, 45*time.Second, func(job domain.ArchaeologyNativeJob, _ domain.ArchaeologySession) bool {
		return job.ThreadID != "" && job.CodexSessionID != "" && job.TurnID != ""
	})
	settings, ok := client.VerifiedHistorianSettings(bound.ThreadID)
	if !ok || settings.ThreadID != bound.ThreadID || settings.TurnID != bound.TurnID || settings.Model != "gpt-5.6-luna" || settings.Effort != "max" ||
		settings.Sandbox != "readOnly" || settings.Approval != "never" || settings.Network || settings.MultiAgent != "explicitRequestOnly" {
		t.Fatal("authoritative effective historian settings were not exact")
	}
	acceptanceEvidence(t, "thread/start+thread/settings/updated+turn/start", "bound_exact_policy", map[string]any{
		"job_id": acceptanceRedactedID(jobID), "thread_id": acceptanceRedactedID(bound.ThreadID), "session_id": acceptanceRedactedID(bound.CodexSessionID), "turn_id": acceptanceRedactedID(bound.TurnID),
		"model": settings.Model, "effort": settings.Effort, "sandbox": settings.Sandbox, "approval": settings.Approval, "network": settings.Network, "multi_agent": settings.MultiAgent,
	})
	terminal := acceptanceWaitForJob(t, ctx, repository, 4*time.Minute, func(job domain.ArchaeologyNativeJob, session domain.ArchaeologySession) bool {
		return job.State == "completed" && len(session.Outcomes) > 0
	})
	canonical, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
	if err != nil || len(canonical.Outcomes) == 0 || terminal.State != "completed" {
		t.Fatalf("historian did not reach report-backed completion: %v", err)
	}
	finalTitle := application.HistorianVisibleTitle("Synthetic historian acceptance")
	visible, err := acceptanceWaitForHistorianTask(ctx, client, workspace, finalTitle, codexauth.TaskLaunch{
		ThreadID: bound.ThreadID, SessionID: bound.CodexSessionID, TurnID: bound.TurnID,
	}, 15*time.Second, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("exact final named non-ephemeral historian identity was not recovered: %v", err)
	}
	afterInventory, err := client.ListHistorianTasks(ctx, workspace)
	if err != nil || len(afterInventory) != 1 || afterInventory[0].ThreadID != bound.ThreadID || afterInventory[0].SessionID != bound.CodexSessionID ||
		afterInventory[0].Name != finalTitle || afterInventory[0].Ephemeral || (afterInventory[0].Source != "appServer" && afterInventory[0].Source != "vscode") {
		t.Fatalf("workspace task delta was not exactly the bound historian: count=%d err=%v", len(afterInventory), err)
	}
	acceptanceEvidence(t, "thread/list+thread/read+thread/turns/list", "one_named_task_one_turn", map[string]any{"task_count": 1, "thread_id": acceptanceRedactedID(visible.ThreadID), "turn_id": acceptanceRedactedID(visible.TurnID), "final_title": finalTitle})

	view, err := service.ProjectArchaeology(ctx, domain.HumanLocalPrincipal)
	if err != nil || view.Review == nil || view.Review.CanApply || view.Capabilities.CanonicalApply.Available {
		t.Fatalf("native review unexpectedly exposed canonical Apply: %v", err)
	}
	afterCanonical, err := acceptanceCanonicalDigest(ctx, repository.DB())
	if err != nil || beforeCanonical != afterCanonical {
		t.Fatalf("canonical Tasks or historical-import application state changed: %v", err)
	}
	if got, snapErr := acceptanceGitSnapshot(ctx, workspace); snapErr != nil || got != syntheticBefore {
		t.Fatalf("synthetic Git HEAD/status/tree changed: %v", snapErr)
	}
	if got, treeErr := acceptanceTreeDigest(workspace); treeErr != nil || got != workspaceBefore {
		t.Fatalf("synthetic working tree changed: %v", treeErr)
	}
	if got, snapErr := acceptanceGitSnapshot(ctx, repoRoot); snapErr != nil || got != outerBefore {
		t.Fatalf("Codex Commons repository HEAD/status/tree changed: %v", snapErr)
	}
	acceptanceEvidence(t, "commons_project_history_report", "review_only_terminal_unchanged", map[string]any{
		"job_id": acceptanceRedactedID(jobID), "job_state": terminal.State, "batch_state": canonical.NativeBatches[0].State, "report_count": len(canonical.Outcomes),
		"tasks_digest": acceptanceRedactedID(afterCanonical), "workspace_digest": acceptanceRedactedID(workspaceBefore), "repo_digest": acceptanceRedactedID(outerBefore),
	})
}

type acceptanceRuntimeBundleHashes struct {
	Codex   string
	Host    string
	Bwrap   string
	Zsh     string
	Rg      string
	Package string
}

func acceptanceVerifyRuntimeBundle(codexPath string, expected acceptanceRuntimeBundleHashes) error {
	if err := acceptanceVerifyCodexBinary(codexPath, expected.Codex, expected.Host); err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(codexPath))
	if codexPath != filepath.Join(root, "bin", "codex") {
		return errors.New("acceptance Codex binary must use the vendor-relative bin/codex layout")
	}
	for _, spec := range []struct {
		name       string
		relative   string
		expected   string
		executable bool
	}{
		{name: "bwrap", relative: "codex-resources/bwrap", expected: expected.Bwrap, executable: true},
		{name: "zsh", relative: "codex-resources/zsh/bin/zsh", expected: expected.Zsh, executable: true},
		{name: "rg", relative: "codex-path/rg", expected: expected.Rg, executable: true},
		{name: "package metadata", relative: "codex-package.json", expected: expected.Package},
	} {
		path := filepath.Join(root, filepath.FromSlash(spec.relative))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || (spec.executable && info.Mode()&0o111 == 0) {
			suffix := ""
			if spec.executable {
				suffix = " executable"
			}
			return fmt.Errorf("acceptance %s must be a regular non-symlink%s file", spec.name, suffix)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot hash acceptance %s", spec.name)
		}
		digest := sha256.Sum256(contents)
		if hex.EncodeToString(digest[:]) != spec.expected {
			return fmt.Errorf("acceptance %s checksum mismatch", spec.name)
		}
	}
	return nil
}

func acceptanceSmokeRuntimeBundle(ctx context.Context, codexPath string) error {
	root := filepath.Dir(filepath.Dir(codexPath))
	bwrap := filepath.Join(root, "codex-resources", "bwrap")
	zsh := filepath.Join(root, "codex-resources", "zsh", "bin", "zsh")
	rg := filepath.Join(root, "codex-path", "rg")
	cmd := exec.CommandContext(ctx, bwrap,
		"--unshare-user", "--uid", "0", "--gid", "0", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--",
		zsh, "-c", `"$1" --version >/dev/null`, "runtime-smoke", rg)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("packaged bwrap/zsh/rg smoke failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// TestRealHistorianRuntimePreflight proves the immutable runtime bundle, its
// sandbox helpers, and managed App Server account/model inventory without
// creating a thread, turn, Commons batch, job, report, or canonical mutation.
func TestRealHistorianRuntimePreflight(t *testing.T) {
	if os.Getenv("COMMONS_REAL_RUNTIME_PREFLIGHT") != realRuntimePreflightOptIn {
		t.Skip("real no-task runtime preflight is not explicitly authorized")
	}
	codexBin := strings.TrimSpace(os.Getenv("COMMONS_ACCEPTANCE_CODEX_BIN"))
	if !filepath.IsAbs(codexBin) || filepath.Clean(codexBin) != codexBin {
		t.Fatal("COMMONS_ACCEPTANCE_CODEX_BIN must be an absolute clean path")
	}
	if err := acceptanceVerifyRuntimeBundle(codexBin, realHistorianRuntimeHashes); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := acceptanceSmokeRuntimeBundle(ctx, codexBin); err != nil {
		t.Fatal(err)
	}
	client, err := codexauth.NewManagedProcess(ctx, codexauth.ProcessConfig{Executable: codexBin, Env: codexauth.ApprovedEnvironment(os.Environ()), EnableExperimentalDynamicTools: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close no-task managed App Server: %v", closeErr)
		}
	}()
	if !client.Available() {
		t.Fatal("managed Codex App Server unavailable")
	}
	if err = acceptanceRequireSignedIn(ctx, client); err != nil {
		t.Fatal(err)
	}
	supported, err := client.SupportsModel(ctx, "gpt-5.6-luna", "max")
	if err != nil || !supported {
		t.Fatalf("managed App Server model inventory lacks gpt-5.6-luna/max: supported=%v err=%v", supported, err)
	}
	acceptanceEvidence(t, "runtime_bundle+bwrap+account/read+model/list", "verified_no_task", map[string]any{"version": "codex-cli 0.147.0"})
}

func acceptanceVerifyCodexBinary(path, expectedSHA, expectedHostSHA string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode()&0o111 == 0 {
		return errors.New("acceptance Codex binary must be an executable regular non-symlink file")
	}
	bin, err := os.ReadFile(path)
	if err != nil {
		return errors.New("cannot hash acceptance Codex binary")
	}
	digest := sha256.Sum256(bin)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		return errors.New("acceptance Codex binary checksum mismatch")
	}
	hostPath := filepath.Join(filepath.Dir(path), "codex-code-mode-host")
	hostInfo, hostErr := os.Lstat(hostPath)
	if hostErr != nil || !hostInfo.Mode().IsRegular() || hostInfo.Mode()&os.ModeSymlink != 0 || hostInfo.Mode()&0o111 == 0 {
		return errors.New("acceptance code-mode host must be an executable regular non-symlink sibling")
	}
	host, hostErr := os.ReadFile(hostPath)
	if hostErr != nil {
		return errors.New("cannot hash acceptance code-mode host")
	}
	hostDigest := sha256.Sum256(host)
	if hex.EncodeToString(hostDigest[:]) != expectedHostSHA {
		return errors.New("acceptance code-mode host checksum mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "codex-cli 0.147.0" {
		return errors.New("acceptance Codex binary version mismatch")
	}
	return nil
}

func acceptanceRuntimePreflight(ctx context.Context, path, expectedSHA, expectedHostSHA string, start func() (codexauth.Client, error)) (codexauth.Client, error) {
	if err := acceptanceVerifyCodexBinary(path, expectedSHA, expectedHostSHA); err != nil {
		return nil, err
	}
	if start == nil {
		return nil, errors.New("managed Codex process factory required")
	}
	client, err := start()
	if err != nil {
		return nil, err
	}
	if !client.Available() {
		_ = client.Close()
		return nil, errors.New("managed Codex App Server unavailable")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = acceptanceRequireSignedIn(checkCtx, client); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func acceptanceRuntimeBundlePreflight(ctx context.Context, path string, expected acceptanceRuntimeBundleHashes, start func() (codexauth.Client, error)) (codexauth.Client, error) {
	if err := acceptanceVerifyRuntimeBundle(path, expected); err != nil {
		return nil, err
	}
	return acceptanceRuntimePreflight(ctx, path, expected.Codex, expected.Host, start)
}

func TestAcceptanceRuntimeBundleRejectsMissingLinkedAndTamperedResources(t *testing.T) {
	root := t.TempDir()
	write := func(relative, contents string, mode fs.FileMode) string {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	codexPath := write("bin/codex", "#!/bin/sh\nprintf 'codex-cli 0.147.0\n'\n", 0o700)
	write("bin/codex-code-mode-host", "host", 0o700)
	write("codex-resources/bwrap", "bwrap", 0o700)
	write("codex-resources/zsh/bin/zsh", "zsh", 0o700)
	write("codex-path/rg", "rg", 0o700)
	write("codex-package.json", "{}\\n", 0o600)
	hash := func(relative string) string {
		contents, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		return hex.EncodeToString(digest[:])
	}
	hashes := acceptanceRuntimeBundleHashes{
		Codex: hash("bin/codex"), Host: hash("bin/codex-code-mode-host"),
		Bwrap: hash("codex-resources/bwrap"), Zsh: hash("codex-resources/zsh/bin/zsh"),
		Rg: hash("codex-path/rg"), Package: hash("codex-package.json"),
	}
	if err := acceptanceVerifyRuntimeBundle(codexPath, hashes); err != nil {
		t.Fatalf("valid bundle: %v", err)
	}
	repository, err := commonsstore.Open(context.Background(), filepath.Join(root, "bundle-preflight.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	processStarts := 0
	start := func() (codexauth.Client, error) {
		processStarts++
		return nil, errors.New("process must not start")
	}
	bwrap := filepath.Join(root, "codex-resources", "bwrap")
	if err := os.WriteFile(bwrap, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := acceptanceVerifyRuntimeBundle(codexPath, hashes); err == nil {
		t.Fatal("tampered bwrap accepted")
	}
	if _, err := acceptanceRuntimeBundlePreflight(context.Background(), codexPath, hashes, start); err == nil {
		t.Fatal("tampered bwrap crossed runtime boundary")
	}
	if err := os.WriteFile(bwrap, []byte("bwrap"), 0o700); err != nil {
		t.Fatal(err)
	}
	rg := filepath.Join(root, "codex-path", "rg")
	if err := os.Remove(rg); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bwrap, rg); err != nil {
		t.Fatal(err)
	}
	if err := acceptanceVerifyRuntimeBundle(codexPath, hashes); err == nil {
		t.Fatal("linked rg accepted")
	}
	if _, err := acceptanceRuntimeBundlePreflight(context.Background(), codexPath, hashes, start); err == nil {
		t.Fatal("linked rg crossed runtime boundary")
	}
	if err := os.Remove(rg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rg, []byte("rg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "codex-package.json")); err != nil {
		t.Fatal(err)
	}
	if err := acceptanceVerifyRuntimeBundle(codexPath, hashes); err == nil {
		t.Fatal("missing package metadata accepted")
	}
	if _, err := acceptanceRuntimeBundlePreflight(context.Background(), codexPath, hashes, start); err == nil {
		t.Fatal("missing package metadata crossed runtime boundary")
	}
	var batches, jobs int
	if err := repository.DB().QueryRow(`SELECT (SELECT count(*) FROM archaeology_native_batches),(SELECT count(*) FROM archaeology_native_jobs)`).Scan(&batches, &jobs); err != nil {
		t.Fatal(err)
	}
	if processStarts != 0 || batches != 0 || jobs != 0 {
		t.Fatalf("failed bundle preflight crossed boundary: processes=%d batches=%d jobs=%d", processStarts, batches, jobs)
	}
}

func TestAcceptancePinnedRuntimeRejectsWrongHashVersionAndSymlinkBeforeQueue(t *testing.T) {
	root := t.TempDir()
	write := func(name, version string) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+version+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	goodVersion := write("good-version", "codex-cli 0.147.0")
	hostPath := write("codex-code-mode-host", "code-mode-host fixture")
	bytes, err := os.ReadFile(goodVersion)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	hostBytes, _ := os.ReadFile(hostPath)
	hostDigest := sha256.Sum256(hostBytes)
	if err = acceptanceVerifyCodexBinary(goodVersion, hex.EncodeToString(digest[:]), hex.EncodeToString(hostDigest[:])); err != nil {
		t.Fatalf("valid fixture: %v", err)
	}
	repository, openErr := commonsstore.Open(context.Background(), filepath.Join(root, "preflight.sqlite3"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer repository.Close()
	processStarts := 0
	start := func() (codexauth.Client, error) {
		processStarts++
		return nil, errors.New("process must not start")
	}
	if _, err = acceptanceRuntimePreflight(context.Background(), goodVersion, strings.Repeat("0", 64), hex.EncodeToString(hostDigest[:]), start); err == nil {
		t.Fatal("wrong checksum accepted")
	}
	if _, err = acceptanceRuntimePreflight(context.Background(), goodVersion, hex.EncodeToString(digest[:]), strings.Repeat("0", 64), start); err == nil {
		t.Fatal("wrong code-mode host checksum accepted")
	}
	if err = os.Rename(hostPath, hostPath+".missing"); err != nil {
		t.Fatal(err)
	}
	if _, err = acceptanceRuntimePreflight(context.Background(), goodVersion, hex.EncodeToString(digest[:]), hex.EncodeToString(hostDigest[:]), start); err == nil {
		t.Fatal("missing code-mode host accepted")
	}
	if err = os.Rename(hostPath+".missing", hostPath); err != nil {
		t.Fatal(err)
	}
	wrongVersion := write("wrong-version", "codex-cli 0.148.0")
	wrongBytes, _ := os.ReadFile(wrongVersion)
	wrongDigest := sha256.Sum256(wrongBytes)
	if _, err = acceptanceRuntimePreflight(context.Background(), wrongVersion, hex.EncodeToString(wrongDigest[:]), hex.EncodeToString(hostDigest[:]), start); err == nil {
		t.Fatal("wrong version accepted")
	}
	link := filepath.Join(root, "codex-link")
	if err = os.Symlink(goodVersion, link); err != nil {
		t.Fatal(err)
	}
	if _, err = acceptanceRuntimePreflight(context.Background(), link, hex.EncodeToString(digest[:]), hex.EncodeToString(hostDigest[:]), start); err == nil {
		t.Fatal("symlink accepted")
	}
	var batches, jobs, projects int
	if err = repository.DB().QueryRow(`SELECT (SELECT count(*) FROM archaeology_native_batches),(SELECT count(*) FROM archaeology_native_jobs),(SELECT count(*) FROM projects)`).Scan(&batches, &jobs, &projects); err != nil {
		t.Fatal(err)
	}
	if processStarts != 0 || batches != 0 || jobs != 0 || projects != 0 {
		t.Fatalf("failed runtime preflight crossed boundary: processes=%d batches=%d jobs=%d projects=%d", processStarts, batches, jobs, projects)
	}
}

type acceptanceAccountReader interface {
	AccountState(context.Context) (codexauth.AccountState, error)
}

func acceptanceRequireSignedIn(ctx context.Context, client acceptanceAccountReader) error {
	if client == nil {
		return errors.New("managed Codex account is not signed in")
	}
	state, err := client.AccountState(ctx)
	if err != nil || state != codexauth.AccountSignedIn {
		return errors.New("managed Codex account is not signed in")
	}
	return nil
}

type acceptanceAccountStub struct {
	state     codexauth.AccountState
	err       error
	available bool
	closed    *int
}

func (s acceptanceAccountStub) AccountState(context.Context) (codexauth.AccountState, error) {
	return s.state, s.err
}
func (s acceptanceAccountStub) Available() bool { return s.available }
func (s acceptanceAccountStub) StartDeviceCode(context.Context) (codexauth.DeviceCode, error) {
	return codexauth.DeviceCode{}, codexauth.ErrUnavailable
}
func (s acceptanceAccountStub) PollLogin(context.Context, string) (codexauth.LoginResult, error) {
	return codexauth.LoginResult{}, codexauth.ErrUnavailable
}
func (s acceptanceAccountStub) CancelLogin(context.Context, string) error {
	return codexauth.ErrUnavailable
}
func (s acceptanceAccountStub) SetEventHandler(func(codexauth.Event)) {}
func (s acceptanceAccountStub) Close() error {
	if s.closed != nil {
		*s.closed++
	}
	return nil
}

func TestAcceptanceAccountPreflightStopsBeforeQueue(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' 'codex-cli 0.147.0'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostPath := filepath.Join(root, "codex-code-mode-host")
	if err := os.WriteFile(hostPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	hostContents, _ := os.ReadFile(hostPath)
	hostDigest := sha256.Sum256(hostContents)
	for _, test := range []struct {
		name  string
		state codexauth.AccountState
		err   error
		cross bool
	}{
		{name: "signed in", state: codexauth.AccountSignedIn, cross: true},
		{name: "signed out", state: codexauth.AccountSignedOut},
		{name: "unknown", state: codexauth.AccountUnknown},
		{name: "read error", state: codexauth.AccountSignedIn, err: errors.New("unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, err := commonsstore.Open(context.Background(), ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			crossedQueueBoundary := false
			workspaceTaskCreations := 0
			processStarts, processCloses := 0, 0
			client, preflightErr := acceptanceRuntimePreflight(context.Background(), bin, hex.EncodeToString(digest[:]), hex.EncodeToString(hostDigest[:]), func() (codexauth.Client, error) {
				processStarts++
				return acceptanceAccountStub{state: test.state, err: test.err, available: true, closed: &processCloses}, nil
			})
			if preflightErr == nil {
				crossedQueueBoundary = true
				workspaceTaskCreations++
				_ = client.Close()
			}
			if crossedQueueBoundary != test.cross {
				t.Fatalf("crossed queue boundary=%v", crossedQueueBoundary)
			}
			if processStarts != 1 {
				t.Fatalf("process starts=%d", processStarts)
			}
			if !test.cross {
				var batches, jobs int
				if err := repository.DB().QueryRow(`SELECT count(*) FROM archaeology_native_batches`).Scan(&batches); err != nil {
					t.Fatal(err)
				}
				if err := repository.DB().QueryRow(`SELECT count(*) FROM archaeology_native_jobs`).Scan(&jobs); err != nil {
					t.Fatal(err)
				}
				if batches != 0 || jobs != 0 || workspaceTaskCreations != 0 || processCloses != 1 {
					t.Fatalf("preflight failure created batches=%d jobs=%d workspace_tasks=%d closes=%d", batches, jobs, workspaceTaskCreations, processCloses)
				}
			}
		})
	}
}

type acceptanceCleanup struct {
	RunRoot, Workspace, RepoRoot                  string
	SyntheticBefore, WorkspaceBefore, OuterBefore string
	Queued                                        bool
}

type acceptanceManagedClient interface {
	ListHistorianTasks(context.Context, string) ([]codexauth.TaskIdentity, error)
	VerifiedHistorianSettings(string) (codexauth.TaskLaunch, bool)
	Close() error
}

func (c acceptanceCleanup) Run(t *testing.T, service *application.Service, repository *commonsstore.Store, client acceptanceManagedClient) {
	t.Helper()
	state, cancelState, reconcileState := "not_queued", "not_needed", "not_needed"
	var settingsEvidence map[string]any
	preserve := c.Queued
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cleanupCancel()
	if c.Queued {
		state = "preserved"
		value, err := repository.ArchaeologySession(cleanupCtx, domain.HumanLocalPrincipal)
		if err != nil {
			t.Errorf("cleanup canonical read failed: %v", err)
			cancelState = "read_failed"
		} else if len(value.NativeBatches) == 0 {
			// Start failed before the durable queue/launch boundary. This remains a
			// pre-acceptance failure and owns no visible task to inspect.
			preserve = false
			state = "pre_accept_failure"
			cancelState = "not_needed"
		} else if uncertainJob, uncertain := acceptanceUncertainJob(value); uncertain {
			// Uncertain is intentionally not terminal. Reconcile the exact accepted
			// identity when available, but retain durable uncertainty regardless of
			// lookup/interrupt outcome; only an independent stopped proof may resolve it.
			cancelState = "durable_uncertain"
			settingsEvidence = acceptanceSettingsEvidence(client, uncertainJob)
			reconcileState = acceptanceReconcileUncertain(cleanupCtx, client, c.Workspace, uncertainJob)
		} else if acceptanceHasLiveJob(value) {
			cancelState = "requested"
			_, cancelErr := service.CancelProjectArchaeology(cleanupCtx, domain.HumanLocalPrincipal, "isolated-acceptance-cleanup", application.ArchaeologyTransitionRequest{BaseRevision: value.Revision})
			if cancelErr != nil {
				t.Errorf("cleanup cancellation failed: %v", cancelErr)
				cancelState = "request_failed"
			} else {
				cancelState = acceptanceWaitCleanup(cleanupCtx, repository)
				if cancelState != "terminal" && cancelState != "durable_uncertain" {
					t.Errorf("cleanup did not reach terminal or durable uncertain state: %s", cancelState)
				}
			}
		} else {
			cancelState = "already_terminal"
		}
	}
	service.CloseProjectArchaeology()
	closeErr := client.Close()
	if closeErr != nil {
		t.Errorf("managed App Server did not prove clean child exit: %v", closeErr)
	}
	if err := repository.Close(); err != nil {
		t.Errorf("close acceptance database: %v", err)
	}
	if got, err := acceptanceGitSnapshot(context.Background(), c.Workspace); err != nil || got != c.SyntheticBefore {
		t.Errorf("cleanup synthetic Git invariant failed: %v", err)
	}
	if got, err := acceptanceTreeDigest(c.Workspace); err != nil || got != c.WorkspaceBefore {
		t.Errorf("cleanup synthetic tree invariant failed: %v", err)
	}
	if got, err := acceptanceGitSnapshot(context.Background(), c.RepoRoot); err != nil || got != c.OuterBefore {
		t.Errorf("cleanup outer repository invariant failed: %v", err)
	}
	receipt := map[string]any{"state": state, "cancel_state": cancelState, "reconcile_state": reconcileState, "settings_attestation": settingsEvidence, "app_server_exit": closeErr == nil, "root": c.RunRoot}
	body, _ := json.Marshal(receipt)
	if preserve {
		if err := os.WriteFile(filepath.Join(c.RunRoot, "acceptance-receipt.json"), append(body, '\n'), 0o600); err != nil {
			t.Errorf("write acceptance receipt: %v", err)
		}
		t.Log("preserved acceptance root: " + c.RunRoot)
	} else {
		if err := os.RemoveAll(c.RunRoot); err != nil {
			t.Errorf("remove pre-launch acceptance root: %v", err)
		}
	}
	acceptanceEvidence(t, "cleanup", cancelState, map[string]any{"app_server_exit": closeErr == nil, "preserved": preserve})
}

func acceptanceUncertainJob(value domain.ArchaeologySession) (domain.ArchaeologyNativeJob, bool) {
	for _, batch := range value.NativeBatches {
		for _, job := range batch.Jobs {
			if job.State == "uncertain" {
				return job, true
			}
		}
	}
	return domain.ArchaeologyNativeJob{}, false
}

func acceptanceReconcileUncertain(ctx context.Context, client acceptanceManagedClient, cwd string, job domain.ArchaeologyNativeJob) string {
	if job.ThreadID == "" || job.TurnID == "" {
		return "identity_missing"
	}
	inventory, listErr := client.ListHistorianTasks(ctx, cwd)
	found := false
	if listErr == nil {
		for _, item := range inventory {
			if item.ThreadID == job.ThreadID && item.SessionID == job.CodexSessionID {
				found = true
				break
			}
		}
	}
	if listErr != nil {
		return "lookup_failed"
	}
	if found {
		return "identity_found"
	}
	return "identity_absent"
}

func acceptanceSettingsEvidence(client acceptanceManagedClient, job domain.ArchaeologyNativeJob) map[string]any {
	if job.ThreadID == "" || job.TurnID == "" {
		return nil
	}
	settings, ok := client.VerifiedHistorianSettings(job.ThreadID)
	if !ok || settings.ThreadID != job.ThreadID || settings.TurnID != job.TurnID {
		return nil
	}
	return map[string]any{
		"stage": settings.Stage, "model": settings.Model, "effort": settings.Effort,
		"sandbox": settings.Sandbox, "approval": settings.Approval,
		"network": settings.Network, "multi_agent": settings.MultiAgent,
	}
}

type acceptanceCleanupClientStub struct {
	inventory []codexauth.TaskIdentity
	listErr   error
	lists     int
	settings  codexauth.TaskLaunch
	verified  bool
}

func (c *acceptanceCleanupClientStub) ListHistorianTasks(context.Context, string) ([]codexauth.TaskIdentity, error) {
	c.lists++
	return c.inventory, c.listErr
}

func (*acceptanceCleanupClientStub) Close() error { return nil }

func (c *acceptanceCleanupClientStub) VerifiedHistorianSettings(string) (codexauth.TaskLaunch, bool) {
	return c.settings, c.verified
}

func TestAcceptanceUncertainCleanupRemainsDurableAndReadOnly(t *testing.T) {
	exact := domain.ArchaeologyNativeJob{State: "uncertain", ThreadID: "thread", CodexSessionID: "session", TurnID: "turn"}
	value := domain.ArchaeologySession{NativeBatches: []domain.ArchaeologyNativeBatch{{Jobs: []domain.ArchaeologyNativeJob{exact}}}}
	job, uncertain := acceptanceUncertainJob(value)
	if !uncertain || job.ThreadID != "thread" || acceptanceHasLiveJob(value) {
		t.Fatalf("uncertain classification job=%+v uncertain=%v live=%v", job, uncertain, acceptanceHasLiveJob(value))
	}
	client := &acceptanceCleanupClientStub{inventory: []codexauth.TaskIdentity{{ThreadID: "thread", SessionID: "session"}}}
	if state := acceptanceReconcileUncertain(context.Background(), client, "/workspace", job); state != "identity_found" || client.lists != 1 {
		t.Fatalf("exact state=%q lists=%d", state, client.lists)
	}

	missing := domain.ArchaeologyNativeJob{State: "uncertain"}
	client = &acceptanceCleanupClientStub{}
	if state := acceptanceReconcileUncertain(context.Background(), client, "/workspace", missing); state != "identity_missing" || client.lists != 0 {
		t.Fatalf("identity-less state=%q lists=%d", state, client.lists)
	}

	client = &acceptanceCleanupClientStub{verified: true, settings: codexauth.TaskLaunch{ThreadID: "thread", TurnID: "turn", Stage: "settings_exact", Model: "gpt-5.6-luna", Effort: "max", Sandbox: "readOnly", Approval: "never", MultiAgent: "explicitRequestOnly"}}
	evidence := acceptanceSettingsEvidence(client, exact)
	if evidence["stage"] != "settings_exact" || evidence["model"] != "gpt-5.6-luna" || evidence["effort"] != "max" || evidence["sandbox"] != "readOnly" || evidence["approval"] != "never" || evidence["network"] != false || evidence["multi_agent"] != "explicitRequestOnly" {
		t.Fatalf("settings evidence=%#v", evidence)
	}
}

func acceptanceHasLiveJob(value domain.ArchaeologySession) bool {
	for _, batch := range value.NativeBatches {
		for _, job := range batch.Jobs {
			switch job.State {
			case "queued", "starting", "active", "report_ready", "cancel_requested":
				return true
			}
		}
	}
	return false
}

func acceptanceWaitCleanup(ctx context.Context, repository *commonsstore.Store) string {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if err == nil {
			uncertain := false
			live := false
			for _, batch := range value.NativeBatches {
				for _, job := range batch.Jobs {
					uncertain = uncertain || job.State == "uncertain"
					switch job.State {
					case "queued", "starting", "active", "report_ready", "cancel_requested":
						live = true
					}
				}
			}
			if uncertain {
				return "durable_uncertain"
			}
			if !live {
				return "terminal"
			}
		}
		select {
		case <-ctx.Done():
			return "timeout"
		case <-ticker.C:
		}
	}
}

type acceptanceHistorianFinder interface {
	FindHistorianTask(context.Context, string, string) (codexauth.TaskLaunch, bool, error)
}

func acceptanceWaitForHistorianTask(ctx context.Context, finder acceptanceHistorianFinder, cwd, title string, expected codexauth.TaskLaunch, maximum, interval time.Duration) (codexauth.TaskLaunch, error) {
	if finder == nil || maximum <= 0 || interval <= 0 {
		return codexauth.TaskLaunch{}, errors.New("invalid historian visibility wait")
	}
	deadline := time.NewTimer(maximum)
	defer deadline.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		visible, found, err := finder.FindHistorianTask(ctx, cwd, title)
		if err != nil {
			return codexauth.TaskLaunch{}, err
		}
		if found {
			if visible.ThreadID != expected.ThreadID || visible.SessionID != expected.SessionID || visible.TurnID != expected.TurnID {
				return codexauth.TaskLaunch{}, errors.New("historian final title resolved to an unexpected identity")
			}
			return visible, nil
		}
		select {
		case <-ctx.Done():
			return codexauth.TaskLaunch{}, ctx.Err()
		case <-deadline.C:
			return codexauth.TaskLaunch{}, errors.New("historian final title visibility wait expired")
		case <-ticker.C:
		}
	}
}

type acceptanceHistorianFinderResult struct {
	launch codexauth.TaskLaunch
	found  bool
	err    error
}

type acceptanceHistorianFinderStub struct {
	results []acceptanceHistorianFinderResult
	calls   int
}

func (s *acceptanceHistorianFinderStub) FindHistorianTask(context.Context, string, string) (codexauth.TaskLaunch, bool, error) {
	index := s.calls
	s.calls++
	if index >= len(s.results) {
		return codexauth.TaskLaunch{}, false, nil
	}
	result := s.results[index]
	return result.launch, result.found, result.err
}

func TestAcceptanceWaitForHistorianTaskRequiresExactFinalIdentity(t *testing.T) {
	exact := codexauth.TaskLaunch{ThreadID: "thread-exact", SessionID: "session-exact", TurnID: "turn-exact"}
	finder := &acceptanceHistorianFinderStub{results: []acceptanceHistorianFinderResult{{}, {launch: exact, found: true}}}
	got, err := acceptanceWaitForHistorianTask(context.Background(), finder, "/workspace/exact", "Project history · Exact", exact, 100*time.Millisecond, time.Millisecond)
	if err != nil || got != exact || finder.calls != 2 {
		t.Fatalf("launch=%+v calls=%d err=%v", got, finder.calls, err)
	}

	mismatch := &acceptanceHistorianFinderStub{results: []acceptanceHistorianFinderResult{{launch: codexauth.TaskLaunch{ThreadID: "thread-other", SessionID: "session-exact", TurnID: "turn-exact"}, found: true}}}
	if _, err = acceptanceWaitForHistorianTask(context.Background(), mismatch, "/workspace/exact", "Project history · Exact", exact, 100*time.Millisecond, time.Millisecond); err == nil {
		t.Fatal("mismatched final-title identity was accepted")
	}
}

func TestAcceptanceWaitForHistorianTaskIsBounded(t *testing.T) {
	finder := &acceptanceHistorianFinderStub{}
	if _, err := acceptanceWaitForHistorianTask(context.Background(), finder, "/workspace/missing", "Project history · Missing", codexauth.TaskLaunch{ThreadID: "thread", SessionID: "session", TurnID: "turn"}, 5*time.Millisecond, time.Millisecond); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err=%v", err)
	}
	if finder.calls < 1 {
		t.Fatal("bounded wait did not perform a read")
	}
}

func acceptanceWaitForJob(t *testing.T, ctx context.Context, repository *commonsstore.Store, maximum time.Duration, ready func(domain.ArchaeologyNativeJob, domain.ArchaeologySession) bool) domain.ArchaeologyNativeJob {
	t.Helper()
	deadline := time.NewTimer(maximum)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	last := domain.ArchaeologyNativeJob{}
	for {
		value, err := repository.ArchaeologySession(ctx, domain.HumanLocalPrincipal)
		if err == nil && len(value.NativeBatches) == 1 && len(value.NativeBatches[0].Jobs) == 1 {
			last = value.NativeBatches[0].Jobs[0]
			if ready(last, value) {
				return last
			}
			if last.State == "failed" || last.State == "attention" || last.State == "uncertain" || last.State == "interrupted" {
				t.Fatalf("historian entered terminal non-acceptance state %q (%q)", last.State, last.ErrorCode)
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("acceptance context ended while job state was %q", last.State)
		case <-deadline.C:
			t.Fatalf("bounded wait expired while job state was %q", last.State)
		case <-ticker.C:
		}
	}
}

func acceptanceInitializeGit(ctx context.Context, workspace string) error {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + filepath.Dir(workspace), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0"}
	commands := [][]string{{"init", "--quiet", "--initial-branch=main"}, {"add", "--", "HISTORY.md"}, {"-c", "user.name=Codex Commons Acceptance", "-c", "user.email=acceptance.invalid@example.invalid", "commit", "--quiet", "-m", "Synthetic historian fixture", "-m", "Codex-Session: synthetic-source-session"}}
	for _, args := range commands {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir, command.Env = workspace, env
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("synthetic git %s: %w (%s)", args[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func acceptanceRepositoryRoot() (string, error) {
	path, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil && !info.IsDir() {
			return path, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", errors.New("repository root not found")
		}
		path = parent
	}
}

func acceptanceGitSnapshot(ctx context.Context, root string) (string, error) {
	var all bytes.Buffer
	for _, args := range [][]string{{"rev-parse", "HEAD"}, {"rev-parse", "HEAD^{tree}"}, {"status", "--porcelain=v1", "-z", "--untracked-files=all"}} {
		command := exec.CommandContext(ctx, "git", args...)
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			return "", err
		}
		all.Write(output)
		all.WriteByte(0)
	}
	sum := sha256.Sum256(all.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func acceptanceCanonicalDigest(ctx context.Context, db *sql.DB) (string, error) {
	h := sha256.New()
	for _, query := range []string{
		`SELECT id,project_id,state,title,priority,coalesce(owner_session_id,''),accept_text FROM tasks ORDER BY id`,
		`SELECT task_id,depends_on_task_id FROM task_dependencies ORDER BY task_id,depends_on_task_id`,
		`SELECT id,project_id,batch_id,source_digest,manifest_digest,request_key FROM historical_import_batches ORDER BY id`,
		`SELECT id,batch_record_id,source_key,canonical_task_id,disposition FROM historical_import_tasks ORDER BY id`,
	} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return "", err
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return "", err
		}
		for rows.Next() {
			values, pointers := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err = rows.Scan(pointers...); err != nil {
				rows.Close()
				return "", err
			}
			_, _ = fmt.Fprintln(h, values)
		}
		if err = rows.Close(); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func acceptanceTreeDigest(root string) (string, error) {
	type item struct {
		path string
		mode fs.FileMode
		sum  [32]byte
	}
	var items []item
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(filepath.ToSlash(relative), ".git/") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("synthetic workspace must not contain symlinks")
		}
		var sum [32]byte
		if info.Mode().IsRegular() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum = sha256.Sum256(body)
		}
		items = append(items, item{path: filepath.ToSlash(relative), mode: info.Mode(), sum: sum})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].path < items[j].path })
	h := sha256.New()
	for _, value := range items {
		_, _ = fmt.Fprintf(h, "%s\x00%o\x00%x\n", value.path, value.mode, value.sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func acceptanceRedactedID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:6])
}

func acceptanceEvidence(t *testing.T, method, state string, fields map[string]any) {
	t.Helper()
	value := map[string]any{"method": method, "state": state}
	for key, field := range fields {
		value[key] = field
	}
	body, _ := json.Marshal(value)
	t.Log(string(body))
}
