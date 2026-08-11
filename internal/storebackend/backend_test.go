package storebackend

import (
	"testing"
	"time"

	"codex-commons/internal/presence"
)

func TestMatchesPresenceState(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	live := presence.Snapshot{Execution: "executing", LastActivity: now.Add(-2 * time.Hour)}
	idle := presence.Snapshot{Execution: "not_running", LastActivity: now.Add(-30 * time.Minute)}
	inactive := presence.Snapshot{Execution: "not_running", LastActivity: now.Add(-2 * time.Hour)}

	tests := []struct {
		name  string
		state string
		item  presence.Snapshot
		want  bool
	}{
		{"all includes inactive", "all", inactive, true},
		{"active includes live", "active", live, true},
		{"active includes idle", "active", idle, true},
		{"active excludes inactive", "active", inactive, false},
		{"live includes executing", "live", live, true},
		{"live excludes idle", "live", idle, false},
		{"idle includes recent not-running", "idle", idle, true},
		{"idle excludes executing", "idle", live, false},
		{"inactive includes old not-running", "inactive", inactive, true},
		{"inactive excludes recent not-running", "inactive", idle, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesPresenceState(test.state, test.item, now); got != test.want {
				t.Fatalf("matchesPresenceState(%q)=%v want %v", test.state, got, test.want)
			}
		})
	}
}
