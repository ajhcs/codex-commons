package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func testManifest() Manifest {
	return Manifest{SchemaVersion: 1, Namespace: "dogfood-test", Project: Project{Key: "project", ID: "codex-commons", Name: "Codex Commons", Status: "active", Purpose: "Dogfood", Now: "Bootstrap"}, Milestones: []Milestone{{Key: "m1", Title: "First", Status: "active", Position: 1}}, Tasks: []Task{{Key: "t1", Title: "Foundation", State: "ready", Priority: 1, MilestoneKey: "m1"}, {Key: "t2", Title: "Dependent", State: "blocked", DependencyKeys: []string{"t1"}}}, WikiPages: []WikiPage{{Key: "w1", Slug: "start", Title: "Start", Summary: "Initial", Body: "Body"}}, Posts: []Post{{Key: "p1", Kind: "notice", Title: "Hello", Body: "World", Basis: "Curated", TaskKey: "t2", Attachments: []Attachment{{Kind: "link", URL: "https://example.com", Title: "Source"}}}}}
}

type countingTransport struct{ calls int }

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls++
	return nil, fmt.Errorf("unexpected network")
}

func TestDryRunPerformsNoHTTP(t *testing.T) {
	m := testManifest()
	transport := &countingTransport{}
	receipt, err := Run(context.Background(), m, Config{Client: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	if transport.calls != 0 {
		t.Fatalf("network calls=%d", transport.calls)
	}
	if receipt.Mode != "dry-run" || receipt.Phase != "validated" || receipt.Verified {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestApplyReplayConflictAndLogout(t *testing.T) {
	fake := newFakeAPI()
	server := httptest.NewServer(fake)
	defer server.Close()
	m := testManifest()
	cfg := Config{Apply: true, BaseURL: server.URL, Secret: "correct"}
	first, err := Run(context.Background(), m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Verified {
		t.Fatalf("receipt=%+v", first)
	}
	writes := fake.uniqueWrites()
	second, err := Run(context.Background(), m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Verified || fake.uniqueWrites() != writes {
		t.Fatalf("replay created rows: before=%d after=%d", writes, fake.uniqueWrites())
	}
	if fake.logouts != 2 {
		t.Fatalf("logouts=%d", fake.logouts)
	}
	m.Tasks[0].Title = "Changed"
	receipt, err := Run(context.Background(), m, cfg)
	if err == nil || !strings.Contains(err.Error(), "HTTP 409 conflict") {
		t.Fatalf("err=%v", err)
	}
	if receipt.Phase != "milestones" {
		t.Fatalf("partial receipt phase=%q", receipt.Phase)
	}
	if fake.uniqueWrites() != writes {
		t.Fatalf("conflict created a row")
	}
	if fake.logouts != 3 {
		t.Fatalf("logout after failure=%d", fake.logouts)
	}
}

func TestFailureAfterTasksRerunsWithoutDuplicates(t *testing.T) {
	fake := newFakeAPI()
	fake.failOnce = "/wiki/"
	server := httptest.NewServer(fake)
	defer server.Close()
	m := testManifest()
	cfg := Config{Apply: true, BaseURL: server.URL, Secret: "correct"}
	receipt, err := Run(context.Background(), m, cfg)
	if err == nil || receipt.Phase != "tasks" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	before := fake.uniqueWrites()
	receipt, err = Run(context.Background(), m, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Verified {
		t.Fatalf("receipt=%+v", receipt)
	}
	want := 1 + len(m.Milestones) + len(m.Tasks) + len(m.WikiPages) + len(m.Posts)
	if fake.uniqueWrites() != want || before >= want {
		t.Fatalf("writes before=%d after=%d want=%d", before, fake.uniqueWrites(), want)
	}
	if fake.logouts != 2 {
		t.Fatalf("logouts=%d", fake.logouts)
	}
}

func TestManifestRejectsTransportMismatches(t *testing.T) {
	m := testManifest()
	m.Milestones[0].TargetDate = "2026-99-99"
	if err := Validate(m); err == nil {
		t.Fatal("impossible date accepted")
	}
	m = testManifest()
	m.Posts[0].Kind = "handoff"
	if err := Validate(m); err == nil {
		t.Fatal("noncanonical kind accepted")
	}
	m = testManifest()
	m.Posts[0].Attachments[0].URL = "https://example.com/" + strings.Repeat("x", 2049)
	if err := Validate(m); err == nil {
		t.Fatal("oversize URL accepted")
	}
	if _, err := DecodeManifest(bytes.NewReader(append([]byte(`{}`), bytes.Repeat([]byte(" "), maxManifestBytes)...))); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize manifest err=%v", err)
	}
}

type storedWrite struct {
	path   string
	body   map[string]any
	result writeResult
}
type fakeAPI struct {
	mu       sync.Mutex
	writes   map[string]storedWrite
	logouts  int
	failOnce string
	failed   bool
}

func newFakeAPI() *fakeAPI           { return &fakeAPI{writes: map[string]storedWrite{}} }
func (f *fakeAPI) uniqueWrites() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.writes) }
func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/auth/login" {
		var v map[string]string
		_ = json.NewDecoder(r.Body).Decode(&v)
		if v["secret"] != "correct" {
			f.err(w, 401, "unauthorized", "bad login")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "commons_human_session", Value: "session", Path: "/"})
		f.ok(w, map[string]any{"authenticated": true, "csrf_token": "csrf"})
		return
	}
	if r.URL.Path == "/v1/auth/logout" {
		f.logouts++
		f.ok(w, map[string]any{"authenticated": false})
		return
	}
	if r.Method == http.MethodGet {
		f.read(w, r)
		return
	}
	if r.Header.Get("Origin") == "" || r.Header.Get("X-Commons-CSRF") != "csrf" {
		f.err(w, 403, "forbidden", "missing csrf")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		f.err(w, 400, "invalid", "missing key")
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	if old, ok := f.writes[key]; ok {
		oldRaw, _ := json.Marshal(old.body)
		newRaw, _ := json.Marshal(body)
		if !bytes.Equal(oldRaw, newRaw) {
			f.err(w, 409, "conflict", "idempotency payload changed")
			return
		}
		f.ok(w, old.result)
		return
	}
	if f.failOnce != "" && !f.failed && strings.Contains(r.URL.Path, f.failOnce) {
		f.failed = true
		f.err(w, 503, "unavailable", "injected failure")
		return
	}
	id := ""
	switch {
	case r.URL.Path == "/v1/projects":
		id = body["id"].(string)
	case strings.HasSuffix(r.URL.Path, "/milestones"):
		id = "MS-" + suffix(key)
	case strings.HasSuffix(r.URL.Path, "/tasks"):
		id = "T-" + suffix(key)
	case strings.Contains(r.URL.Path, "/wiki/"):
		id = "W-" + suffix(key)
	case r.URL.Path == "/v1/posts":
		id = "P-" + suffix(key)
	}
	result := writeResult{ID: id, Revision: 1, Persisted: true}
	f.writes[key] = storedWrite{r.URL.Path, body, result}
	f.ok(w, result)
}
func suffix(key string) string { parts := strings.Split(key, ":"); return parts[len(parts)-1] }
func (f *fakeAPI) ok(w http.ResponseWriter, data any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]any{"request_id": "test"}})
}
func (f *fakeAPI) err(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (f *fakeAPI) read(w http.ResponseWriter, r *http.Request) {
	project := f.findPath("/v1/projects")
	switch {
	case r.URL.Path == "/v1/projects/codex-commons":
		b := project.body
		f.ok(w, map[string]any{"project": map[string]any{"id": b["id"], "name": b["name"], "status": b["status"], "purpose": b["purpose"], "now": b["now"]}})
	case strings.HasSuffix(r.URL.Path, "/milestones"):
		items := []any{}
		for _, v := range f.writes {
			if strings.HasSuffix(v.path, "/milestones") {
				items = append(items, map[string]any{"id": v.result.ID, "title": v.body["title"], "status": v.body["status"], "position": v.body["position"], "target_date": v.body["target_date"]})
			}
		}
		f.ok(w, map[string]any{"items": items})
	case strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		v := f.findID(id)
		b := v.body
		f.ok(w, map[string]any{"task": map[string]any{"id": id, "project": "codex-commons", "title": b["title"], "description": b["description"], "acceptance": b["acceptance"], "state": b["state"], "priority": b["priority"], "milestone_id": b["milestone_id"], "dependencies": dependencyViews(b["dependency_ids"])}})
	case strings.Contains(r.URL.Path, "/wiki/"):
		slug := strings.TrimPrefix(r.URL.Path, "/v1/projects/codex-commons/wiki/")
		v := f.findContains("/wiki/" + slug + "/revisions")
		b := v.body
		f.ok(w, map[string]any{"page": map[string]any{"id": v.result.ID, "slug": slug, "title": b["title"], "summary": b["summary"], "body": b["body"]}})
	case strings.HasPrefix(r.URL.Path, "/v1/posts/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/posts/")
		v := f.findID(id)
		b := v.body
		f.ok(w, map[string]any{"post": map[string]any{"id": id, "kind": b["kind"], "title": b["title"], "body": b["body"], "basis": b["basis"], "related_ref": b["ref"], "topic": map[string]string{"id": "codex-commons"}, "attachments": b["attachments"]}})
	default:
		f.err(w, 404, "not_found", "missing")
	}
}
func dependencyViews(value any) []map[string]any {
	raw, _ := value.([]any)
	out := make([]map[string]any, len(raw))
	for i, v := range raw {
		out[i] = map[string]any{"id": v}
	}
	return out
}
func (f *fakeAPI) findPath(path string) storedWrite {
	for _, v := range f.writes {
		if v.path == path {
			return v
		}
	}
	return storedWrite{}
}
func (f *fakeAPI) findContains(part string) storedWrite {
	for _, v := range f.writes {
		if strings.Contains(v.path, part) {
			return v
		}
	}
	return storedWrite{}
}
func (f *fakeAPI) findID(id string) storedWrite {
	for _, v := range f.writes {
		if v.result.ID == id {
			return v
		}
	}
	return storedWrite{}
}
