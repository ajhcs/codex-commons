package opsfs

import (
	"bytes"
	"fmt"
	"strings"
)

func invalidChecksum(reason string) error {
	return fmt.Errorf("invalid checksum: %s", reason)
}

// ParseSHA256Sum requires exactly one canonical GNU text-mode record:
// 64 lowercase hex digits, two spaces, the exact path, one newline.
func ParseSHA256Sum(body []byte, path string) (string, error) {
	if len(body) == 0 || len(body) > MaxChecksumBytes {
		return "", invalidChecksum("size")
	}
	if bytes.IndexByte(body, 0) >= 0 {
		return "", invalidChecksum("control")
	}
	if bytes.Count(body, []byte{'\n'}) != 1 || body[len(body)-1] != '\n' {
		return "", invalidChecksum("line count")
	}
	line := body[:len(body)-1]
	if bytes.IndexByte(line, '\n') >= 0 || bytes.IndexByte(line, '\r') >= 0 {
		return "", invalidChecksum("line count")
	}
	digest, rest, ok := strings.Cut(string(line), "  ")
	if !ok {
		return "", invalidChecksum("grammar")
	}
	if strings.Contains(rest, " ") || strings.Contains(rest, "\t") {
		return "", invalidChecksum("extra field")
	}
	if rest != path {
		return "", invalidChecksum("path")
	}
	if err := validDigest(digest); err != nil {
		return "", err
	}
	return digest, nil
}

func validDigest(digest string) error {
	if len(digest) != 64 {
		return invalidChecksum("digest")
	}
	for i := 0; i < len(digest); i++ {
		c := digest[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return invalidChecksum("digest")
		}
	}
	return nil
}

// FormatSHA256Sum writes one canonical GNU text-mode record for path.
func FormatSHA256Sum(digest, path string) ([]byte, error) {
	if err := ValidAbsPath(path); err != nil {
		return nil, err
	}
	if err := validDigest(digest); err != nil {
		return nil, err
	}
	rec := digest + "  " + path + "\n"
	if _, err := ParseSHA256Sum([]byte(rec), path); err != nil {
		return nil, err
	}
	return []byte(rec), nil
}
