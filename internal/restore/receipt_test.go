package restore

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	"codex-commons/internal/domain"
)

const (
	testRecordedAt = "2026-08-20T00:00:00Z"
	testReleaseID  = "continuous-dogfood-test"
	testDrillID    = "drill-1"
	// Independent SHA-256 of the v1 preimage: length-prefixed domain, version,
	// hash algorithm "sha256", then the typed receipt fields. Recomputed below
	// without calling Fingerprint or writeFramed.
	goldenDigest = "509513eca6cad375cbbbd9586196ec12ade857c1c56c5307e85f805393c34368"
)

func testInstallationID() [16]byte {
	var id [16]byte
	for i := range id {
		id[i] = byte(i)
	}
	return id
}

func testBackupDigest() [32]byte {
	var digest [32]byte
	for i := range digest {
		digest[i] = 0xbb
	}
	return digest
}

func testInstallationHex() string {
	id := testInstallationID()
	return hex.EncodeToString(id[:])
}

func testBackupHex() string {
	digest := testBackupDigest()
	return hex.EncodeToString(digest[:])
}

func canonicalReceiptJSON(drill, release, recorded, installationHex, backupHex string, schema int) []byte {
	return []byte(`{"schema_version":` + strconv.Itoa(schema) + `,"release_id":"` + release + `","drill_id":"` + drill + `","recorded_at":"` + recorded + `","installation_id":"` + installationHex + `","restored_backup_digest":"` + backupHex + `"}`)
}

func canonicalInput() []byte {
	return canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, testInstallationHex(), testBackupHex(), 17)
}

func mustParse(t *testing.T, input []byte) Receipt {
	t.Helper()
	receipt, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%s): %v", input, err)
	}
	return receipt
}

func TestParseValidCanonicalReceipt(t *testing.T) {
	receipt := mustParse(t, canonicalInput())
	if receipt.SchemaVersion != 17 || receipt.ReleaseID != testReleaseID || receipt.DrillID != testDrillID || receipt.RecordedAt != testRecordedAt {
		t.Fatalf("receipt=%+v", receipt)
	}
	if receipt.InstallationID != testInstallationID() || receipt.RestoredBackupDigest != testBackupDigest() {
		t.Fatalf("identity or backup digest mismatch: id=%x backup=%x", receipt.InstallationID, receipt.RestoredBackupDigest)
	}
	reordered := []byte(`{"restored_backup_digest":"` + testBackupHex() + `","installation_id":"` + testInstallationHex() + `","recorded_at":"` + testRecordedAt + `","drill_id":"` + testDrillID + `","release_id":"` + testReleaseID + `","schema_version":17}` + "\n")
	if mustParse(t, reordered).Fingerprint() != receipt.Fingerprint() {
		t.Fatal("key order or trailing newline changed the fingerprint")
	}
}

func TestParseTrailingBytesAllowOnlyRFC8259JSONWhitespace(t *testing.T) {
	base := canonicalInput()
	accept := []struct {
		name string
		tail []byte
	}{
		{name: "none", tail: nil},
		{name: "space", tail: []byte(" ")},
		{name: "tab", tail: []byte("\t")},
		{name: "lf", tail: []byte("\n")},
		{name: "cr", tail: []byte("\r")},
		{name: "mixed rfc8259", tail: []byte(" \t\r\n")},
	}
	for _, test := range accept {
		t.Run("accept/"+test.name, func(t *testing.T) {
			input := append(append([]byte{}, base...), test.tail...)
			if mustParse(t, input).Fingerprint() != goldenDigest {
				t.Fatalf("accepted trailing %q changed the fingerprint", test.tail)
			}
		})
	}
	reject := []struct {
		name string
		tail []byte
	}{
		{name: "nbsp u+00a0", tail: []byte("\u00a0")},
		{name: "nel u+0085", tail: []byte("\u0085")},
		{name: "line separator u+2028", tail: []byte("\u2028")},
		{name: "paragraph separator u+2029", tail: []byte("\u2029")},
		{name: "ideographic space u+3000", tail: []byte("\u3000")},
		{name: "nbsp after rfc whitespace", tail: []byte(" \u00a0")},
		{name: "nel after lf", tail: []byte("\n\u0085")},
		{name: "letter", tail: []byte("x")},
	}
	for _, test := range reject {
		t.Run("reject/"+test.name, func(t *testing.T) {
			input := append(append([]byte{}, base...), test.tail...)
			_, err := Parse(input)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Parse trailing %s err=%v", test.name, err)
			}
		})
	}
}

func TestParseRejectsMalformedDuplicateUnknownTrailingAndOversized(t *testing.T) {
	id := testInstallationHex()
	backup := testBackupHex()
	valid := string(canonicalInput())
	oversizedField := strings.Repeat("d", maxStableIDBytes+1)
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "empty", input: []byte{}},
		{name: "malformed", input: []byte(`{"schema_version":`)},
		{name: "array", input: []byte(`[]`)},
		{name: "trailing comma", input: []byte(strings.Replace(valid, `"}`, `",}`, 1))},
		{name: "duplicate key", input: []byte(`{"schema_version":17,"schema_version":17,"release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `","restored_backup_digest":"` + backup + `"}`)},
		{name: "unknown field", input: []byte(`{"schema_version":17,"release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `","restored_backup_digest":"` + backup + `","file":"commons.sqlite3"}`)},
		{name: "trailing data", input: append(canonicalInput(), []byte(`{"x":1}`)...)},
		{name: "trailing token", input: append(canonicalInput(), '1')},
		{name: "oversized input", input: bytes.Repeat([]byte("a"), MaxBytes+1)},
		{name: "oversized drill", input: canonicalReceiptJSON(oversizedField, testReleaseID, testRecordedAt, id, backup, 17)},
		{name: "oversized release", input: canonicalReceiptJSON(testDrillID, oversizedField, testRecordedAt, id, backup, 17)},
		{name: "control character", input: []byte("{\x01}")},
		{name: "escaped control", input: []byte(`{"schema_version":17,"release_id":"` + testReleaseID + `","drill_id":"drill\u0001","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `","restored_backup_digest":"` + backup + `"}`)},
		{name: "schema string", input: []byte(`{"schema_version":"17","release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `","restored_backup_digest":"` + backup + `"}`)},
		{name: "schema float", input: []byte(`{"schema_version":17.0,"release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `","restored_backup_digest":"` + backup + `"}`)},
		{name: "identity number", input: []byte(`{"schema_version":17,"release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":1,"restored_backup_digest":"` + backup + `"}`)},
		{name: "missing field", input: []byte(`{"schema_version":17,"release_id":"` + testReleaseID + `","drill_id":"` + testDrillID + `","recorded_at":"` + testRecordedAt + `","installation_id":"` + id + `"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.input)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Parse(%s) err=%v", test.name, err)
			}
		})
	}
}

func TestParseRejectsUppercaseNonhexAndInvalidFieldValues(t *testing.T) {
	id := testInstallationHex()
	backup := testBackupHex()
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "uppercase backup digest", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, id, strings.ToUpper(backup), 17)},
		{name: "nonhex backup digest", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, id, strings.Repeat("g", 64), 17)},
		{name: "short backup digest", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, id, strings.Repeat("b", 63), 17)},
		{name: "uppercase installation id", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, strings.ToUpper(id), backup, 17)},
		{name: "schema zero", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, id, backup, 0)},
		{name: "schema overflow", input: canonicalReceiptJSON(testDrillID, testReleaseID, testRecordedAt, id, backup, 10001)},
		{name: "blank drill", input: canonicalReceiptJSON("   ", testReleaseID, testRecordedAt, id, backup, 17)},
		{name: "empty release", input: canonicalReceiptJSON(testDrillID, "", testRecordedAt, id, backup, 17)},
		{name: "offset timestamp", input: canonicalReceiptJSON(testDrillID, testReleaseID, "2026-08-20T00:00:00+00:00", id, backup, 17)},
		{name: "lowercase z timestamp", input: canonicalReceiptJSON(testDrillID, testReleaseID, "2026-08-20T00:00:00z", id, backup, 17)},
		{name: "padded fractional timestamp", input: canonicalReceiptJSON(testDrillID, testReleaseID, "2026-08-20T00:00:00.000Z", id, backup, 17)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse(test.input)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("Parse(%s) err=%v", test.name, err)
			}
		})
	}
}

func TestParseAcceptsReorderedKeysAndFractionalTimestamp(t *testing.T) {
	reordered := []byte(`{"restored_backup_digest":"` + testBackupHex() + `","installation_id":"` + testInstallationHex() + `","recorded_at":"2026-08-20T00:00:00.123Z","drill_id":"` + testDrillID + `","release_id":"` + testReleaseID + `","schema_version":17}`)
	receipt := mustParse(t, reordered)
	if receipt.RecordedAt != "2026-08-20T00:00:00.123Z" {
		t.Fatalf("recorded_at=%q", receipt.RecordedAt)
	}
	canonical := mustParse(t, canonicalInput())
	if receipt.Fingerprint() == canonical.Fingerprint() {
		t.Fatal("fractional timestamp produced the same fingerprint as the canonical receipt")
	}
}

func TestFingerprintIsDeterministicAndInstallationBound(t *testing.T) {
	first := mustParse(t, canonicalInput())
	second := mustParse(t, canonicalInput())
	independent := independentFingerprintV1(first)
	if first.Fingerprint() != goldenDigest || second.Fingerprint() != goldenDigest || independent != goldenDigest {
		t.Fatalf("fingerprint=%q independent=%q want %q", first.Fingerprint(), independent, goldenDigest)
	}
	id := testInstallationID()
	if err := first.Bind(id[:]); err != nil {
		t.Fatal(err)
	}
	foreign := bytes.Repeat([]byte{0x22}, 16)
	if err := first.Bind(foreign); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bind foreign identity err=%v", err)
	}
}

func independentFingerprintV1(r Receipt) string {
	var buf bytes.Buffer
	frame := func(name string, value []byte) {
		var header [8]byte
		binary.BigEndian.PutUint64(header[:], uint64(len(name)))
		_, _ = buf.Write(header[:])
		_, _ = buf.WriteString(name)
		binary.BigEndian.PutUint64(header[:], uint64(len(value)))
		_, _ = buf.Write(header[:])
		_, _ = buf.Write(value)
	}
	frame("domain", []byte("codex-commons.installation.restore-evidence"))
	var version [4]byte
	binary.BigEndian.PutUint32(version[:], 1)
	frame("version", version[:])
	frame("hash_algorithm", []byte("sha256"))
	frame("installation_id", r.InstallationID[:])
	frame("drill_id", []byte(r.DrillID))
	frame("recorded_at", []byte(r.RecordedAt))
	frame("restored_backup_digest", r.RestoredBackupDigest[:])
	var schema [4]byte
	binary.BigEndian.PutUint32(schema[:], uint32(r.SchemaVersion))
	frame("schema_version", schema[:])
	frame("release_id", []byte(r.ReleaseID))
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

func TestFingerprintResistsFramingAmbiguity(t *testing.T) {
	base := mustParse(t, canonicalInput())
	left := base
	left.DrillID = "ab"
	left.ReleaseID = "c"
	right := base
	right.DrillID = "a"
	right.ReleaseID = "bc"
	if left.Fingerprint() == right.Fingerprint() {
		t.Fatal("length-prefix framing collapsed drill_id+release_id concatenations")
	}
	schemaLeft := base
	schemaLeft.SchemaVersion = 1
	schemaRight := base
	schemaRight.SchemaVersion = 257
	if schemaLeft.Fingerprint() == schemaRight.Fingerprint() {
		t.Fatal("schema_version integer framing collapsed")
	}

	// Swap the 16-byte identity with the digest prefix so the 48-byte
	// identity||digest concatenation is a redistribution of the same bytes.
	swapped := base
	copy(swapped.InstallationID[:], base.RestoredBackupDigest[:16])
	copy(swapped.RestoredBackupDigest[:16], base.InstallationID[:])
	var baseConcat, swappedConcat [48]byte
	copy(baseConcat[:16], base.InstallationID[:])
	copy(baseConcat[16:], base.RestoredBackupDigest[:])
	copy(swappedConcat[:16], swapped.InstallationID[:])
	copy(swappedConcat[16:], swapped.RestoredBackupDigest[:])
	if bytes.Equal(baseConcat[:], swappedConcat[:]) {
		t.Fatal("redistributed identity/digest concatenation was unchanged")
	}
	if !bytesEqualAsSet(baseConcat[:], swappedConcat[:]) {
		t.Fatal("redistributed identity/digest did not preserve the 48-byte multiset")
	}
	if swapped.Fingerprint() == base.Fingerprint() {
		t.Fatal("field-name framing collapsed redistributed installation_id and restored_backup_digest")
	}

	for _, test := range []struct {
		name                     string
		delimiter                string
		leftDrill, leftRelease   string
		rightDrill, rightRelease string
	}{
		{name: "pipe delimiter", delimiter: "|", leftDrill: "a|b", leftRelease: "c", rightDrill: "a", rightRelease: "b|c"},
		{name: "quote", delimiter: "\"", leftDrill: "a\"b", leftRelease: "c", rightDrill: "a", rightRelease: "b\"c"},
		{name: "newline", delimiter: "\n", leftDrill: "a\nb", leftRelease: "c", rightDrill: "a", rightRelease: "b\nc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.leftDrill+test.delimiter+test.leftRelease != test.rightDrill+test.delimiter+test.rightRelease {
				t.Fatalf("delimiter case %s is not a join-preserving split", test.name)
			}
			left := base
			left.DrillID = test.leftDrill
			left.ReleaseID = test.leftRelease
			right := base
			right.DrillID = test.rightDrill
			right.ReleaseID = test.rightRelease
			if left.Fingerprint() == right.Fingerprint() {
				t.Fatalf("length-prefix framing collapsed %s field split", test.name)
			}
		})
	}
}

func bytesEqualAsSet(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make([]int, 256)
	for _, b := range left {
		counts[b]++
	}
	for _, b := range right {
		counts[b]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}
