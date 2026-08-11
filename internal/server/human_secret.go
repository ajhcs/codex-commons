package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func readHumanSecret(environmentValue, path string) (string, error) {
	if environmentValue != "" && path != "" {
		return "", errors.New("configure only one of COMMONS_HUMAN_ADMIN_SECRET and COMMONS_HUMAN_ADMIN_SECRET_FILE")
	}
	secret := environmentValue
	if path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("human admin secret file: %w", err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			return "", errors.New("human admin secret file must not be accessible by group or other users")
		}
		if info.Size() > 4096 {
			return "", errors.New("human admin secret file exceeds 4096 bytes")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("human admin secret file: %w", err)
		}
		secret = strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	}
	if strings.ContainsAny(secret, "\r\n\x00") {
		return "", errors.New("human admin secret must be a single non-NUL line")
	}
	return secret, nil
}
