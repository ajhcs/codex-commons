package presence

import (
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestLeaseExpiryIsDeterministicAndSeparateFromConnectivity(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	r := New(clock)
	r.Connect(Session{ID: "S-1", Actor: "agent-1", Host: "plumbob", Project: "commons-lab"})
	if !r.LeaseExecution("S-1", 2*time.Minute) {
		t.Fatal("lease rejected")
	}
	r.Disconnect("S-1")
	got, _ := r.Get("S-1")
	if got.HostConnected || got.Execution != "executing" {
		t.Fatalf("facts were conflated: %#v", got)
	}
	if !got.LastActivity.Equal(clock.now) {
		t.Fatalf("last activity=%s", got.LastActivity)
	}

	clock.now = clock.now.Add(2 * time.Minute)
	got, _ = r.Get("S-1")
	if got.Execution != "not_running" || got.LeaseExpires != nil {
		t.Fatalf("lease did not expire at boundary: %#v", got)
	}
}

func TestLoadedFactIsOptionalAndCopied(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}
	r := New(clock)
	r.Connect(Session{ID: "S-1", Actor: "agent-1", Host: "plumbob"})
	fact := "opened D-7"
	if !r.SetLoaded("S-1", &fact) {
		t.Fatal("set failed")
	}
	fact = "mutated by caller"
	got, _ := r.Get("S-1")
	if got.LoadedFact == nil || *got.LoadedFact != "opened D-7" {
		t.Fatalf("loaded=%v", got.LoadedFact)
	}
	if !r.SetLoaded("S-1", nil) {
		t.Fatal("clear failed")
	}
	got, _ = r.Get("S-1")
	if got.LoadedFact != nil {
		t.Fatalf("loaded not cleared")
	}
}
