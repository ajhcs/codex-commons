//go:build linux

package privatefile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const testLabel = "private test file"

func TestOpenRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := writePrivate(t, dir, "target", []byte("pinned-contents"), 0o600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Open(link, testLabel, 4096); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink accepted: %v", err)
	}
	assertNoPayload(t, Open, link, "pinned-contents")
}

func TestOpenRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir, testLabel, 4096); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory accepted: %v", err)
	}
}

func TestOpenRejectsNonRegular(t *testing.T) {
	path := filepath.Join(t.TempDir(), "socket")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if _, err := Open(path, testLabel, 4096); err == nil {
		t.Fatal("socket accepted")
	} else if strings.Contains(err.Error(), "regular file") || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENXIO) {
		return
	} else if !strings.Contains(err.Error(), "private test file") {
		t.Fatalf("non-regular socket poorly reported: %v", err)
	}
}

func TestOpenRejectsFIFOWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := Open(path, testLabel, 4096)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
		if !strings.Contains(err.Error(), "regular file") && !errors.Is(err, unix.ENXIO) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			t.Fatalf("FIFO poorly reported: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO open blocked")
	}
}

func TestOpenRejectsNon0600Mode(t *testing.T) {
	for _, mode := range []os.FileMode{0o400, 0o700, 0o640, 0o604, 0o644, 0o660} {
		path := writePrivate(t, t.TempDir(), "wide", []byte("mode-secret"), mode)
		err := mustOpenError(t, path)
		if !strings.Contains(err.Error(), "mode 0600") {
			t.Fatalf("mode %04o accepted or poorly reported: %v", mode, err)
		}
		if strings.Contains(err.Error(), "mode-secret") {
			t.Fatal("mode error leaked file contents")
		}
	}
}

func TestOpenRejectsOversizedFile(t *testing.T) {
	path := writePrivate(t, t.TempDir(), "big", bytes.Repeat([]byte("x"), 65), 0o600)
	err := mustOpenError(t, path)
	if !strings.Contains(err.Error(), "exceeds 64 bytes") {
		t.Fatalf("oversized file accepted or poorly reported: %v", err)
	}
}

func TestOpenReadsExactMode0600File(t *testing.T) {
	want := []byte("owner-only-config")
	path := writePrivate(t, t.TempDir(), "ok", want, 0o600)
	got, err := Read(path, testLabel, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("read bytes did not match the opened file")
	}
}

func TestReadAcceptsExactMaxSize(t *testing.T) {
	want := bytes.Repeat([]byte("n"), 64)
	path := writePrivate(t, t.TempDir(), "exact", want, 0o600)
	got, err := Read(path, testLabel, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("exact maxSize read failed")
	}
}

func TestReadFromOpenedRejectsSameInodeGrowth(t *testing.T) {
	dir := t.TempDir()
	path := writePrivate(t, dir, "grow", []byte("small"), 0o600)
	file, err := Open(path, testLabel, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("too-big-now")); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Sync(); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := readFromOpened(file, testLabel, 8)
	if err == nil || !strings.Contains(err.Error(), "exceeds 8 bytes") {
		t.Fatalf("same-inode growth accepted or poorly reported: %v", err)
	}
	if len(body) != 0 {
		t.Fatal("oversized growth returned truncated contents")
	}
}

func TestReadStaysOnOpenedInodeAfterReplacement(t *testing.T) {
	dir := t.TempDir()
	path := writePrivate(t, dir, "config", []byte("original-inode"), 0o600)
	file, err := Open(path, testLabel, 4096)
	if err != nil {
		t.Fatal(err)
	}
	replaced := filepath.Join(dir, "config.replaced")
	if err := os.Rename(path, replaced); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement-inode"), 0o600); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	got, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if string(got) != "original-inode" {
		t.Fatal("replacement after open was visible on the opened descriptor")
	}
}

func TestDecodeJSONFromOpenedDescriptorPreservesTrailingAndMalformedBehavior(t *testing.T) {
	dir := t.TempDir()
	valid := writePrivate(t, dir, "ok.json", []byte(`{"value":"pinned"}`), 0o600)
	file, err := Open(valid, testLabel, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(valid, valid+".old"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(valid, []byte(`{"value":"replaced"} {"value":"trailing"}`), 0o600); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	var payload struct {
		Value string `json:"value"`
	}
	body, err := readFromOpened(file, testLabel, 4096)
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("pinned JSON unexpectedly had trailing data: %v", err)
	}
	if payload.Value != "pinned" {
		t.Fatal("JSON decode did not stay on the opened inode")
	}

	trailing := writePrivate(t, dir, "trailing.json", []byte(`{"value":"one"} {"value":"two"}`), 0o600)
	if err := decodeOneJSON(t, trailing); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	malformed := writePrivate(t, dir, "bad.json", []byte(`{"value":`), 0o600)
	if err := decodeOneJSON(t, malformed); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	unknown := writePrivate(t, dir, "unknown.json", []byte(`{"value":"one","other":true}`), 0o600)
	if err := decodeOneJSON(t, unknown); err == nil {
		t.Fatal("unknown JSON field accepted")
	}
}

func TestOpenRejectsWrongOwner(t *testing.T) {
	// Unprivileged processes cannot chown the fixture to a foreign uid, so
	// this ownership check is skipped unless the test is running as root.
	if unix.Geteuid() != 0 {
		t.Skip("wrong-owner rejection requires chown; skipped when unprivileged")
	}
	path := writePrivate(t, t.TempDir(), "foreign", []byte("foreign-owned"), 0o600)
	if err := unix.Chown(path, 65534, -1); err != nil {
		t.Skipf("chown unavailable: %v", err)
	}
	err := mustOpenError(t, path)
	if !strings.Contains(err.Error(), "owned by the effective user") {
		t.Fatalf("wrong owner accepted or poorly reported: %v", err)
	}
	if strings.Contains(err.Error(), "foreign-owned") {
		t.Fatal("owner error leaked file contents")
	}
}

func TestOpenErrorsOmitFileContents(t *testing.T) {
	secret := "super-secret-value"
	path := writePrivate(t, t.TempDir(), "secret", []byte(`{"token":"`+secret+`"}`), 0o644)
	err := mustOpenError(t, path)
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "token") {
		t.Fatalf("error leaked file contents: %v", err)
	}
}

func writePrivate(t *testing.T, dir, name string, body []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustOpenError(t *testing.T, path string) error {
	t.Helper()
	_, err := Open(path, testLabel, 64)
	if err == nil {
		t.Fatal("expected open to fail")
	}
	return err
}

func decodeOneJSON(t *testing.T, path string) error {
	t.Helper()
	file, err := Open(path, testLabel, 4096)
	if err != nil {
		return err
	}
	body, err := readFromOpened(file, testLabel, 4096)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var payload struct {
		Value string `json:"value"`
	}
	if err := decoder.Decode(&payload); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func assertNoPayload(t *testing.T, open func(string, string, int64) (*os.File, error), path, payload string) {
	t.Helper()
	_, err := open(path, testLabel, 4096)
	if err == nil {
		t.Fatal("expected rejection")
	}
	if strings.Contains(err.Error(), payload) {
		t.Fatal("error leaked file contents")
	}
}
