package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type notificationFake struct{ *fakeBackend }

func (f *notificationFake) Notifications(_ context.Context, _ NotificationListQuery, meta RequestMeta) (NotificationListResult, error) {
	f.seen("notifications", meta)
	if meta.PrincipalKind != "human" || meta.Principal != "human:local-admin" {
		return NotificationListResult{}, NewError(CodeForbidden, "human only")
	}
	return NotificationListResult{UnreadCount: 1}, nil
}

func (f *notificationFake) MarkNotificationRead(_ context.Context, request NotificationReadRequest, meta RequestMeta) (WriteResult, error) {
	f.seen("notification_read:"+request.ID, meta)
	if meta.PrincipalKind != "human" || meta.Principal != "human:local-admin" {
		return WriteResult{}, NewError(CodeForbidden, "human only")
	}
	return WriteResult{ID: "NR-1", Persisted: true}, nil
}

func TestNotificationAuthorityCSRFAndStableHumanPrincipal(t *testing.T) {
	backend := &notificationFake{fakeBackend: &fakeBackend{}}
	handler := humanTestHandler(backend)

	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "http://commons.test/v1/notifications", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous code=%d body=%s", anonymous.Code, anonymous.Body.String())
	}
	agentRequest := httptest.NewRequest(http.MethodGet, "http://commons.test/v1/notifications", nil)
	agentRequest.Header.Set("Authorization", "Bearer agent-secret")
	agent := httptest.NewRecorder()
	handler.ServeHTTP(agent, agentRequest)
	if agent.Code != http.StatusForbidden {
		t.Fatalf("agent code=%d body=%s", agent.Code, agent.Body.String())
	}

	cookie, csrf := loginHuman(t, handler)
	listed := authRequest(handler, http.MethodGet, "http://commons.test/v1/notifications?unread=true&limit=10", "", "", cookie, "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"unread_count":1`) || backend.last.Principal != "human:local-admin" {
		t.Fatalf("list code=%d meta=%+v body=%s", listed.Code, backend.last, listed.Body.String())
	}

	missingCSRF := authRequest(handler, http.MethodPost, "http://commons.test/v1/notification-reads", `{"id":"N-1"}`, "http://commons.test", cookie, "", "read-1")
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf code=%d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}
	read := authRequest(handler, http.MethodPost, "http://commons.test/v1/notification-reads", `{"id":"N-1"}`, "http://commons.test", cookie, csrf, "read-1")
	if read.Code != http.StatusOK || backend.last.IdempotencyKey != "read-1" || !strings.Contains(read.Body.String(), `"persisted":true`) {
		t.Fatalf("read code=%d meta=%+v body=%s", read.Code, backend.last, read.Body.String())
	}
}
