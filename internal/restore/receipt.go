// Package restore validates sanitized restore-evidence receipts and derives
// installation-bound fingerprints. It has no database or filesystem access and
// never inspects secrets, paths, prompts, or arbitrary payloads.
package restore

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"codex-commons/internal/domain"
)

const (
	MaxBytes                 = 4096
	maxStableIDBytes         = 200
	maxTimestampBytes        = 100
	minSchemaVersion         = 1
	maxSchemaVersion         = 10000
	fingerprintDomain        = "codex-commons.installation.restore-evidence"
	fingerprintVersion       = uint32(1)
	fingerprintHashAlgorithm = "sha256"
	installationIDBytes      = 16
	digestBytes              = 32
)

var (
	stableIDPattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,199}$`)
	installationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sha256DigestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	schemaVersionPattern  = regexp.MustCompile(`^[1-9][0-9]{0,4}$`)
	requiredReceiptFields = []string{"schema_version", "release_id", "drill_id", "recorded_at", "installation_id", "restored_backup_digest"}
)

// Receipt is a validated restore-evidence object. Fingerprinting uses these
// typed fields, never the original JSON bytes.
type Receipt struct {
	InstallationID       [installationIDBytes]byte
	DrillID              string
	RecordedAt           string
	RestoredBackupDigest [digestBytes]byte
	SchemaVersion        int
	ReleaseID            string
}

func invalid(reason string) error {
	return fmt.Errorf("%w: restore receipt: %s", domain.ErrInvalid, reason)
}

// Parse strictly decodes one JSON object. Duplicate keys, unknown fields,
// trailing data, control characters, wrong types, and oversized input fail closed.
// Trailing bytes after the object may be only RFC 8259 JSON whitespace
// (SP, HT, LF, CR).
func Parse(input []byte) (Receipt, error) {
	var out Receipt
	if err := precheck(input); err != nil {
		return Receipt{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.UseNumber()
	start, err := dec.Token()
	if err != nil {
		return Receipt{}, invalid("malformed JSON")
	}
	if start != json.Delim('{') {
		return Receipt{}, invalid("must be a JSON object")
	}
	seen := make(map[string]struct{}, len(requiredReceiptFields))
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return Receipt{}, invalid("malformed JSON")
		}
		key, ok := keyTok.(string)
		if !ok {
			return Receipt{}, invalid("malformed JSON")
		}
		if _, dup := seen[key]; dup {
			return Receipt{}, invalid("duplicate key")
		}
		seen[key] = struct{}{}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return Receipt{}, invalid("malformed JSON")
		}
		switch key {
		case "schema_version":
			out.SchemaVersion, err = parseSchemaVersion(raw)
		case "release_id":
			out.ReleaseID, err = parseStableID(raw, "release_id")
		case "drill_id":
			out.DrillID, err = parseStableID(raw, "drill_id")
		case "recorded_at":
			out.RecordedAt, err = parseRecordedAt(raw)
		case "installation_id":
			out.InstallationID, err = parseInstallationID(raw)
		case "restored_backup_digest":
			out.RestoredBackupDigest, err = parseBackupDigest(raw)
		default:
			return Receipt{}, invalid("unknown field")
		}
		if err != nil {
			return Receipt{}, err
		}
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim('}') {
		return Receipt{}, invalid("malformed JSON")
	}
	if rest := input[dec.InputOffset():]; !onlyRFC8259JSONWhitespace(rest) {
		return Receipt{}, invalid("trailing data")
	}
	for _, field := range requiredReceiptFields {
		if _, ok := seen[field]; !ok {
			return Receipt{}, invalid("missing field")
		}
	}
	return out, nil
}

func precheck(input []byte) error {
	if len(input) == 0 {
		return invalid("empty")
	}
	if len(input) > MaxBytes {
		return invalid("oversized")
	}
	if !utf8.Valid(input) {
		return invalid("invalid UTF-8")
	}
	for _, b := range input {
		if b == 0x7f || (b < 0x20 && b != '\t' && b != '\n' && b != '\r') {
			return invalid("control character")
		}
	}
	return nil
}

func onlyRFC8259JSONWhitespace(rest []byte) bool {
	for _, b := range rest {
		switch b {
		case ' ', '\t', '\n', '\r':
		default:
			return false
		}
	}
	return true
}

func parseSchemaVersion(raw json.RawMessage) (int, error) {
	if !schemaVersionPattern.Match(raw) {
		return 0, invalid("schema_version")
	}
	value, err := strconv.Atoi(string(raw))
	if err != nil || value < minSchemaVersion || value > maxSchemaVersion {
		return 0, invalid("schema_version")
	}
	if strconv.Itoa(value) != string(raw) {
		return 0, invalid("schema_version")
	}
	return value, nil
}

func parseJSONString(raw json.RawMessage) (string, error) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", invalid("wrong type")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", invalid("malformed JSON")
	}
	if !utf8.ValidString(value) {
		return "", invalid("invalid UTF-8")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", invalid("control character")
		}
	}
	return value, nil
}

func parseStableID(raw json.RawMessage, field string) (string, error) {
	value, err := parseJSONString(raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) != value || len(value) > maxStableIDBytes || !stableIDPattern.MatchString(value) {
		return "", invalid(field)
	}
	return value, nil
}

func parseRecordedAt(raw json.RawMessage) (string, error) {
	value, err := parseJSONString(raw)
	if err != nil {
		return "", err
	}
	if len(value) < 20 || len(value) > maxTimestampBytes || !strings.HasSuffix(value, "Z") {
		return "", invalid("recorded_at")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return "", invalid("recorded_at")
	}
	parsed = parsed.UTC()
	encoded := parsed.Format(time.RFC3339Nano)
	if parsed.Nanosecond() == 0 {
		encoded = parsed.Format(time.RFC3339)
	}
	if encoded != value {
		return "", invalid("recorded_at")
	}
	return value, nil
}

func parseHex(raw json.RawMessage, pattern *regexp.Regexp, size int, field string) ([]byte, error) {
	value, err := parseJSONString(raw)
	if err != nil {
		return nil, err
	}
	if !pattern.MatchString(value) {
		return nil, invalid(field)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != size {
		return nil, invalid(field)
	}
	return decoded, nil
}

func parseInstallationID(raw json.RawMessage) ([installationIDBytes]byte, error) {
	var out [installationIDBytes]byte
	decoded, err := parseHex(raw, installationIDPattern, installationIDBytes, "installation_id")
	if err != nil {
		return out, err
	}
	copy(out[:], decoded)
	return out, nil
}

func parseBackupDigest(raw json.RawMessage) ([digestBytes]byte, error) {
	var out [digestBytes]byte
	decoded, err := parseHex(raw, sha256DigestPattern, digestBytes, "restored_backup_digest")
	if err != nil {
		return out, err
	}
	copy(out[:], decoded)
	return out, nil
}

// Bind requires the receipt identity to equal the live 16-byte installation_id.
func (r Receipt) Bind(installationID []byte) error {
	if len(installationID) != installationIDBytes {
		return fmt.Errorf("%w: installation identity length %d", domain.ErrInvalid, len(installationID))
	}
	if subtle.ConstantTimeCompare(r.InstallationID[:], installationID) != 1 {
		return invalid("installation identity mismatch")
	}
	return nil
}

// Fingerprint is a domain-separated SHA-256 over a versioned length-prefixed
// preimage. v1 explicitly frames domain, version, and hash algorithm "sha256"
// before the typed receipt fields. It does not hash JSON text, review_secret,
// paths, prompts, or payloads.
func (r Receipt) Fingerprint() string {
	h := sha256.New()
	writeFramed(h, "domain", []byte(fingerprintDomain))
	var version [4]byte
	binary.BigEndian.PutUint32(version[:], fingerprintVersion)
	writeFramed(h, "version", version[:])
	writeFramed(h, "hash_algorithm", []byte(fingerprintHashAlgorithm))
	writeFramed(h, "installation_id", r.InstallationID[:])
	writeFramed(h, "drill_id", []byte(r.DrillID))
	writeFramed(h, "recorded_at", []byte(r.RecordedAt))
	writeFramed(h, "restored_backup_digest", r.RestoredBackupDigest[:])
	var schema [4]byte
	binary.BigEndian.PutUint32(schema[:], uint32(r.SchemaVersion))
	writeFramed(h, "schema_version", schema[:])
	writeFramed(h, "release_id", []byte(r.ReleaseID))
	return hex.EncodeToString(h.Sum(nil))
}

func writeFramed(h io.Writer, name string, value []byte) {
	var header [8]byte
	binary.BigEndian.PutUint64(header[:], uint64(len(name)))
	_, _ = h.Write(header[:])
	_, _ = io.WriteString(h, name)
	binary.BigEndian.PutUint64(header[:], uint64(len(value)))
	_, _ = h.Write(header[:])
	_, _ = h.Write(value)
}

// BackupDigestHex returns the restored backup SHA-256 as lowercase hex.
func (r Receipt) BackupDigestHex() string {
	return hex.EncodeToString(r.RestoredBackupDigest[:])
}
