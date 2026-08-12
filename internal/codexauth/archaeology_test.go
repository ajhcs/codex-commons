package codexauth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArchaeologyInventoryUsesStateDBOnlyAndDoesNotDeserializePrompts(t *testing.T) {
	client, transport := newTestClient(t)
	done := make(chan struct {
		items []Workspace
		err   error
	}, 1)
	go func() {
		items, err := client.ListWorkspaces(context.Background())
		done <- struct {
			items []Workspace
			err   error
		}{items, err}
	}()
	request := nextRequest(t, transport)
	if request.Method != "thread/list" {
		t.Fatalf("method=%s", request.Method)
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params["useStateDbOnly"] != true || params["limit"] != float64(workspacePageSize) {
		t.Fatalf("params=%v", params)
	}
	if kinds, ok := params["sourceKinds"].([]any); !ok || len(kinds) != 10 {
		t.Fatalf("sourceKinds=%v", params["sourceKinds"])
	}
	origin := "https://github.com/acme/widgets.git"
	if err := transport.respondResult(request.ID, map[string]any{"data": []any{map[string]any{"id": "ignored", "cwd": "/home/plumbob/widgets", "updatedAt": int64(1770000000), "preview": "secret prompt must be ignored", "name": "private", "gitInfo": map[string]any{"originUrl": origin, "branch": "main"}}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || len(result.items) != 1 || result.items[0].CWD != "/home/plumbob/widgets" || result.items[0].GitOrigin != origin {
		t.Fatalf("result=%+v err=%v", result.items, result.err)
	}
}

func TestArchaeologyModelValidationAndDirectTaskLaunchWireContract(t *testing.T) {
	client, transport := newTestClient(t)
	modelDone := make(chan struct {
		ok  bool
		err error
	}, 1)
	go func() {
		ok, err := client.SupportsModel(context.Background(), "gpt-5.6-luna", "max")
		modelDone <- struct {
			ok  bool
			err error
		}{ok, err}
	}()
	modelRequest := nextRequest(t, transport)
	if modelRequest.Method != "model/list" {
		t.Fatalf("method=%s", modelRequest.Method)
	}
	if err := transport.respondResult(modelRequest.ID, map[string]any{"data": []any{map[string]any{"id": "gpt-5.6-luna", "model": "gpt-5.6-luna", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "max"}}}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	if result := <-modelDone; !result.ok || result.err != nil {
		t.Fatalf("model=%+v", result)
	}

	launchDone := make(chan struct {
		value TaskLaunch
		err   error
	}, 1)
	go func() {
		value, err := client.LaunchTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "thread={{CODEX_THREAD_ID}} session={{CODEX_SESSION_ID}}", "commons-launch")
		launchDone <- struct {
			value TaskLaunch
			err   error
		}{value, err}
	}()
	threadRequest := nextRequest(t, transport)
	if threadRequest.Method != "thread/start" {
		t.Fatalf("method=%s", threadRequest.Method)
	}
	assertParams(t, threadRequest.Params, map[string]any{"cwd": "/home/plumbob/widgets", "model": "gpt-5.6-luna", "ephemeral": false, "sandbox": "workspace-write", "threadSource": "codex_commons_project_archaeology"})
	if err := transport.respondResult(threadRequest.ID, map[string]any{"thread": map[string]any{"id": "thread-1", "sessionId": "session-1", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}}); err != nil {
		t.Fatal(err)
	}
	turnRequest := nextRequest(t, transport)
	if turnRequest.Method != "turn/start" {
		t.Fatalf("method=%s", turnRequest.Method)
	}
	var turnParams struct {
		ThreadID string `json:"threadId"`
		Input    []struct {
			Text string `json:"text"`
		} `json:"input"`
		Model    string `json:"model"`
		Effort   string `json:"effort"`
		ClientID string `json:"clientUserMessageId"`
	}
	if err := json.Unmarshal(turnRequest.Params, &turnParams); err != nil {
		t.Fatal(err)
	}
	if turnParams.ThreadID != "thread-1" || turnParams.Model != "gpt-5.6-luna" || turnParams.Effort != "max" || turnParams.ClientID != "commons-launch" || len(turnParams.Input) != 1 || !strings.Contains(turnParams.Input[0].Text, "thread=thread-1 session=session-1") {
		t.Fatalf("params=%+v", turnParams)
	}
	if err := transport.respondResult(turnRequest.ID, map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-launchDone:
		if result.err != nil || result.value.ThreadID != "thread-1" || result.value.SessionID != "session-1" || result.value.TurnID != "turn-1" {
			t.Fatalf("launch=%+v err=%v", result.value, result.err)
		}
	case <-time.After(testTimeout):
		t.Fatal("launch timed out")
	}
}
