package opsfs

import "testing"

func TestValidAbsPath(t *testing.T) {
	good := []string{"/tmp/foo", "/home/user/db.sqlite3", "/var/lib/a-b_c.d"}
	for _, path := range good {
		if err := ValidAbsPath(path); err != nil {
			t.Fatalf("ValidAbsPath(%q) = %v", path, err)
		}
	}
	bad := []string{
		"", "relative", "/tmp/../etc/passwd", "/tmp/./foo", "/tmp//foo",
		"/tmp/foo/", "/tmp/foo bar", "/tmp/foo'bar", "/tmp/foo;bar",
		"/tmp/foo\nbar", "/", "/tmp/foo\"bar", "/tmp/foo`bar", "/tmp/foo$bar",
		"/tmp/.", "/tmp/..", "/tmp/foo/../bar",
	}
	for _, path := range bad {
		if err := ValidAbsPath(path); err == nil {
			t.Fatalf("ValidAbsPath(%q) accepted", path)
		}
	}
	// ".backup" as a full component is rejected only when it is "." or "..".
	// Hidden names like ".bundle.x" are used for private temps and must pass.
	if err := ValidAbsPath("/tmp/.bundle.aabbccdd"); err != nil {
		t.Fatalf("private bundle name rejected: %v", err)
	}
}

func TestParseAndFormatSHA256Sum(t *testing.T) {
	path := "/tmp/receipt.json"
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	good, err := FormatSHA256Sum(digest, path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseSHA256Sum(good, path)
	if err != nil || got != digest {
		t.Fatalf("good record: digest=%q err=%v", got, err)
	}
	for _, body := range [][]byte{
		[]byte(digest + "  " + path + "\nextra\n"),
		[]byte(digest + "  " + path + " extra\n"),
		[]byte(digest + "  /tmp/other.json\n"),
		[]byte(digest + " *" + path + "\n"),
		[]byte(digest + " " + path + "\n"),
		[]byte(digest + "  " + path),
		append([]byte(digest+"  "+path+"\n"), 0),
		[]byte("ABCDEF" + digest[6:] + "  " + path + "\n"),
	} {
		if _, err := ParseSHA256Sum(body, path); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
	if _, err := FormatSHA256Sum("not-a-digest", path); err == nil {
		t.Fatal("formatted invalid digest")
	}
}
