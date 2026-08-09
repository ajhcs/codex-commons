package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBrowseRoutesAreAuthenticatedAndIdentityAttested(t *testing.T) {
	backend := &fakeBackend{}
	handler := testHandler(backend, 0)
	for _, path := range []string{"/v1/attention", "/v1/projects", "/v1/people"} {
		if rec := request(handler, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("path=%s unauthenticated code=%d", path, rec.Code)
		}
		rec := request(handler, http.MethodGet, path, "", "bearer-secret")
		if rec.Code != http.StatusOK || backend.last.Actor != "agent-7" || backend.last.Session != "S-7" || backend.last.Host != "plumbob" {
			t.Fatalf("path=%s code=%d meta=%+v body=%s", path, rec.Code, backend.last, rec.Body.String())
		}
	}
	attention := request(handler, http.MethodGet, "/v1/attention", "", "bearer-secret")
	if !strings.Contains(attention.Body.String(), `"untrusted":true`) {
		t.Fatalf("attention provenance meta=%s", attention.Body.String())
	}
}

func TestBrowseQueryParsingBoundsAndCanonicalTimes(t *testing.T) {
	from := "2026-08-01T01:00:00-04:00"
	to := "2026-08-02T05:00:00Z"
	attention, err := parseAttentionBrowseQuery(url.Values{"limit": {"50"}, "q": {"  Rollback  "}, "source": {"task"}, "updated_from": {from}, "updated_to": {to}})
	if err != nil || attention.Limit != 50 || attention.Search != "Rollback" || attention.Source != "task" || !attention.UpdatedFrom.Equal(time.Date(2026, 8, 1, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("attention=%+v err=%v", attention, err)
	}
	connected := true
	people, err := parsePeopleBrowseQuery(url.Values{"limit": {"100"}, "host_connected": {"true"}, "execution": {"executing"}})
	if err != nil || people.HostConnected == nil || *people.HostConnected != connected {
		t.Fatalf("people=%+v err=%v", people, err)
	}
	for _, target := range []string{
		"/v1/attention?limit=101",
		"/v1/attention?updated_from=bad",
		"/v1/attention?q=" + strings.Repeat("x", 201),
		"/v1/attention?updated_from=2026-08-02T00:00:00Z&updated_to=2026-08-01T00:00:00Z",
		"/v1/projects?limit=0",
		"/v1/people?host_connected=maybe",
	} {
		rec := request(testHandler(&fakeBackend{}, 0), http.MethodGet, target, "", "bearer-secret")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("target=%s code=%d body=%s", target, rec.Code, rec.Body.String())
		}
	}
}

func TestProjectPathSegmentsDecodeExactlyOnceAndAllowEncodedSlashIDs(t *testing.T) {
	handler := testHandler(&fakeBackend{}, 0)
	tests := []struct {
		path, marker string
	}{
		{path: "/v1/context/team%2Falpha", marker: `"project":"team/alpha"`},
		{path: "/v1/context/team%252Falpha", marker: `"project":"team%2Falpha"`},
		{path: "/v1/projects/team%2Falpha/overview", marker: `"id":"team/alpha"`},
		{path: "/v1/projects/team%252Falpha/overview", marker: `"id":"team%2Falpha"`},
	}
	for _, tc := range tests {
		rec := request(handler, http.MethodGet, tc.path, "", "bearer-secret")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.marker) {
			t.Fatalf("path=%s code=%d marker=%s body=%s", tc.path, rec.Code, tc.marker, rec.Body.String())
		}
	}
}
