// Package privatefile opens private configuration files without check/use
// races. On Linux it opens with O_NOFOLLOW and validates the descriptor;
// other platforms fail closed and do not open the path.
package privatefile

import "strings"

func normalizeLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "private configuration file"
	}
	return label
}
