package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHumanLoginAcceptsSameOriginRefererFallback(t *testing.T) {
	handler := humanTestHandler(&fakeBackend{})
	req := httptest.NewRequest(http.MethodPost, "http://commons.test/v1/auth/login", strings.NewReader(`{"secret":"`+testAdminSecret+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "http://commons.test/posts")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("same-origin referer code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
