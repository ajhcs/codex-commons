package restore

import (
	"bytes"
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
	goldenDigest   = "5582f18b296b59b6889d058a1605f7d0b683608a922597e7ce513b4c82892f4b"
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
	if first.Fingerprint() != goldenDigest || second.Fingerprint() != goldenDigest {
		t.Fatalf("fingerprint=%q want %q", first.Fingerprint(), goldenDigest)
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
	swapped := base
	copy(swapped.InstallationID[:], base.RestoredBackupDigest[:16])
	if swapped.Fingerprint() == base.Fingerprint() {
		t.Fatal("field-name framing collapsed installation_id and backup digest prefix")
	}
}
