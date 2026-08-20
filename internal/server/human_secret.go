package server

import (
	"errors"
	"strings"

	"codex-commons/internal/privatefile"
)

func readCodexBindingKey(path string) ([32]byte, error) {
	var key [32]byte
	if strings.TrimSpace(path) == "" {
		return key, errors.New("Codex binding-key file is required")
	}
	body, err := privatefile.Read(path, "Codex binding-key file", int64(len(key)))
	if err != nil {
		return key, err
	}
	if len(body) != len(key) {
		return key, errors.New("Codex binding-key file must contain exactly 32 bytes")
	}
	copy(key[:], body)
	var zero [32]byte
	if key == zero {
		return [32]byte{}, errors.New("Codex binding-key file does not contain high-entropy key material")
	}
	return key, nil
}

func readHumanSecret(environmentValue, path string) (string, error) {
	if environmentValue != "" && path != "" {
		return "", errors.New("configure only one of COMMONS_HUMAN_ADMIN_SECRET and COMMONS_HUMAN_ADMIN_SECRET_FILE")
	}
	secret := environmentValue
	if path != "" {
		body, err := privatefile.Read(path, "human admin secret file", 4096)
		if err != nil {
			return "", err
		}
		secret = strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	}
	if strings.ContainsAny(secret, "\r\n\x00") {
		return "", errors.New("human admin secret must be a single non-NUL line")
	}
	return secret, nil
}
