// Package presence tracks live, process-local presence facts. It does not
// infer reachability and performs no polling or background work.
package presence

import (
	"sort"
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Registry struct {
	mu       sync.RWMutex
	clock    Clock
	sessions map[string]record
}

type record struct {
	Session, Actor, Host, Project string
	HostConnected                 bool
	LastActivity                  time.Time
	LoadedFact                    *string
	LeaseExpires                  time.Time
}

type Snapshot struct {
	Session       string     `json:"session"`
	Actor         string     `json:"actor"`
	Host          string     `json:"host"`
	Project       string     `json:"project,omitempty"`
	HostConnected bool       `json:"host_connected"`
	Execution     string     `json:"execution"`
	LeaseExpires  *time.Time `json:"lease_expires,omitempty"`
	LastActivity  time.Time  `json:"last_activity"`
	LoadedFact    *string    `json:"loaded_fact,omitempty"`
}

type Session struct{ ID, Actor, Host, Project string }

func New(clock Clock) *Registry {
	if clock == nil {
		clock = realClock{}
	}
	return &Registry{clock: clock, sessions: make(map[string]record)}
}

func (r *Registry) Connect(s Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.sessions[s.ID]
	rec.Session, rec.Actor, rec.Host, rec.Project = s.ID, s.Actor, s.Host, s.Project
	rec.HostConnected = true
	rec.LastActivity = r.clock.Now().UTC()
	r.sessions[s.ID] = rec
}

// Disconnect changes connectivity only; it does not revoke an execution lease.
func (r *Registry) Disconnect(session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[session]
	if !ok {
		return
	}
	rec.HostConnected = false
	rec.LastActivity = r.clock.Now().UTC()
	r.sessions[session] = rec
}

func (r *Registry) Touch(session string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[session]
	if !ok {
		return false
	}
	rec.LastActivity = r.clock.Now().UTC()
	r.sessions[session] = rec
	return true
}

// SetLoaded records an optional observation, never an authoritative instruction.
func (r *Registry) SetLoaded(session string, fact *string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[session]
	if !ok {
		return false
	}
	if fact == nil {
		rec.LoadedFact = nil
	} else {
		value := *fact
		rec.LoadedFact = &value
	}
	rec.LastActivity = r.clock.Now().UTC()
	r.sessions[session] = rec
	return true
}

func (r *Registry) LeaseExecution(session string, duration time.Duration) bool {
	if duration <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[session]
	if !ok {
		return false
	}
	now := r.clock.Now().UTC()
	rec.LeaseExpires = now.Add(duration)
	rec.LastActivity = now
	r.sessions[session] = rec
	return true
}

func (r *Registry) Get(session string) (Snapshot, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.sessions[session]
	if !ok {
		return Snapshot{}, false
	}
	return snapshot(rec, r.clock.Now().UTC()), true
}

func (r *Registry) List(project string) []Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := r.clock.Now().UTC()
	out := make([]Snapshot, 0, len(r.sessions))
	for _, rec := range r.sessions {
		if project == "" || rec.Project == project {
			out = append(out, snapshot(rec, now))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out
}

func snapshot(rec record, now time.Time) Snapshot {
	execution := "idle"
	var expires *time.Time
	if !rec.LeaseExpires.IsZero() && now.Before(rec.LeaseExpires) {
		execution = "executing"
		value := rec.LeaseExpires
		expires = &value
	}
	var loaded *string
	if rec.LoadedFact != nil {
		value := *rec.LoadedFact
		loaded = &value
	}
	return Snapshot{Session: rec.Session, Actor: rec.Actor, Host: rec.Host, Project: rec.Project, HostConnected: rec.HostConnected, Execution: execution, LeaseExpires: expires, LastActivity: rec.LastActivity, LoadedFact: loaded}
}
