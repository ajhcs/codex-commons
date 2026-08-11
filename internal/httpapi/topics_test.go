package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestTopicsRouteIsCanonicalAndBounded(t *testing.T) {
	backend := &fakeBackend{}
	handler := testHandler(backend, 0)
	recorder := request(handler, http.MethodGet, "/v1/topics?limit=100", "", "bearer-secret")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"general"`) ||
		!strings.Contains(recorder.Body.String(), `"untrusted":false`) {
		t.Fatalf("topics code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, target := range []string{"/v1/topics?limit=0", "/v1/topics?limit=101"} {
		recorder = request(handler, http.MethodGet, target, "", "bearer-secret")
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("target=%s code=%d body=%s", target, recorder.Code, recorder.Body.String())
		}
	}
}
