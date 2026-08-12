package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	assertParams(t, threadRequest.Params, map[string]any{"cwd": "/home/plumbob/widgets", "model": "gpt-5.6-luna", "ephemeral": false, "sandbox": "workspace-write", "threadSource": "appServer"})
	if err := transport.respondResult(threadRequest.ID, map[string]any{"thread": map[string]any{"id": "thread-1", "sessionId": "session-1", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}}); err != nil {
		t.Fatal(err)
	}
	readRequest := nextRequest(t, transport)
	if readRequest.Method != "thread/list" {
		t.Fatalf("method=%s", readRequest.Method)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "thread-1", "cwd": "/home/plumbob/widgets", "source": "appServer"}}, "nextCursor": nil}); err != nil {
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

func TestExperimentalHistorianLaunchIsSecretFreeAndRoutesExactDynamicCall(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	if !client.ExperimentalDynamicTools() {
		_, err := client.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-safe-launch", "Project history · Widgets", nil, nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("disabled experimental launch err=%v", err)
		}
		return
	}
	calls := make(chan DynamicToolCall, 1)
	terminals := make(chan TurnTerminal, 1)
	launchDone := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "Inspect this project and use the Commons project-history tools. No canonical history may be applied.", "commons-safe-launch", "Project history · Widgets", func(_ context.Context, call DynamicToolCall) DynamicToolResponse {
			calls <- call
			return DynamicToolResponse{Success: true, ContentItems: []DynamicToolContent{{Type: "inputText", Text: `{"accepted":true}`}}}
		}, func(terminal TurnTerminal) { terminals <- terminal })
		launchDone <- err
	}()
	thread := nextRequest(t, transport)
	if thread.Method != "thread/start" {
		t.Fatalf("method=%s", thread.Method)
	}
	var threadParams map[string]any
	if err := json.Unmarshal(thread.Params, &threadParams); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(threadParams)
	if strings.Contains(strings.ToLower(string(body)), "grant") || strings.Contains(strings.ToLower(string(body)), "token") {
		t.Fatalf("thread params expose credential: %s", body)
	}
	tools, ok := threadParams["dynamicTools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("dynamicTools=%#v", threadParams["dynamicTools"])
	}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		schema := tool["inputSchema"].(map[string]any)
		if schema["additionalProperties"] != false {
			t.Fatalf("open schema=%#v", schema)
		}
	}
	if err := transport.respondResult(thread.ID, map[string]any{"thread": map[string]any{"id": "thread-safe", "sessionId": "session-safe", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}}); err != nil {
		t.Fatal(err)
	}
	nameRequest := nextRequest(t, transport)
	if nameRequest.Method != "thread/name/set" {
		t.Fatalf("method=%s", nameRequest.Method)
	}
	if err := transport.respondResult(nameRequest.ID, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	proofRequest := nextRequest(t, transport)
	if proofRequest.Method != "thread/read" {
		t.Fatalf("method=%s", proofRequest.Method)
	}
	if err := transport.respondResult(proofRequest.ID, map[string]any{"thread": map[string]any{"id": "thread-safe", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false}}); err != nil {
		t.Fatal(err)
	}
	readRequest := nextRequest(t, transport)
	if readRequest.Method != "thread/list" {
		t.Fatalf("method=%s", readRequest.Method)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "other-thread", "cwd": "/home/plumbob/widgets", "source": "vscode", "name": "Other task", "ephemeral": false}}, "nextCursor": "page-2"}); err != nil {
		t.Fatal(err)
	}
	readRequest = nextRequest(t, transport)
	var listParams map[string]any
	if err := json.Unmarshal(readRequest.Params, &listParams); err != nil || listParams["cursor"] != "page-2" {
		t.Fatalf("second page params=%v err=%v", listParams, err)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "thread-safe", "cwd": "/home/plumbob/widgets", "source": "vscode", "name": "Project history · Widgets", "ephemeral": false}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	turn := nextRequest(t, transport)
	var turnParams map[string]any
	if err := json.Unmarshal(turn.Params, &turnParams); err != nil {
		t.Fatal(err)
	}
	turnBody, _ := json.Marshal(turnParams)
	if strings.Contains(strings.ToLower(string(turnBody)), "grant") || strings.Contains(strings.ToLower(string(turnBody)), "token") || strings.Contains(string(turnBody), "/task/claim") {
		t.Fatalf("turn exposes credential: %s", turnBody)
	}
	if err := transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-safe", "status": "inProgress"}}); err != nil {
		t.Fatal(err)
	}
	if err := <-launchDone; err != nil {
		t.Fatal(err)
	}
	if err := transport.respond(map[string]any{"id": 991, "method": "item/tool/call", "params": map[string]any{"callId": "call-1", "threadId": "thread-safe", "turnId": "turn-safe", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 2, "note": "Reading metadata"}}}); err != nil {
		t.Fatal(err)
	}
	call := <-calls
	if call.ThreadID != "thread-safe" || call.TurnID != "turn-safe" || call.Tool != "commons_project_history_progress" {
		t.Fatalf("call=%+v", call)
	}
	response := nextRequest(t, transport)
	if string(bytes.TrimSpace(response.ID)) != "991" || response.Method != "" {
		t.Fatalf("response=%+v", response)
	}
	if err := transport.respond(map[string]any{
		"method": "turn/completed",
		"params": map[string]any{
			"threadId": "thread-safe",
			"turn": map[string]any{
				"id": "turn-safe", "status": "interrupted", "durationMs": 68800,
				"items": []any{map[string]any{"secret": "ignored"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	terminal := <-terminals
	if terminal.Status != "interrupted" || terminal.DurationMS == nil || *terminal.DurationMS != 68800 {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestExperimentalHistorianRejectsCredentialBearingPrompt(t *testing.T) {
	client, _ := newTestClient(t)
	_, err := client.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "single-purpose report grant: secret", "commons-safe-launch", "Project history · Widgets", func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(TurnTerminal) {})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestExperimentalHistorianBuffersImmediateTerminalAndSignalsTransportLoss(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	terminals := make(chan TurnTerminal, 2)
	done := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "Inspect this project and submit a bounded proposal.", "commons-race", "Project history · Widgets", func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(value TurnTerminal) { terminals <- value })
		done <- err
	}()
	thread := nextRequest(t, transport)
	if err := transport.respondResult(thread.ID, map[string]any{"thread": map[string]any{"id": "thread-race", "sessionId": "session-race", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}}); err != nil {
		t.Fatal(err)
	}
	name := nextRequest(t, transport)
	if err := transport.respondResult(name.ID, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	read := nextRequest(t, transport)
	if err := transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-race", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false}}); err != nil {
		t.Fatal(err)
	}
	list := nextRequest(t, transport)
	if err := transport.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-race", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false, "source": "vscode"}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	turn := nextRequest(t, transport)
	if err := transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-race", "status": "inProgress"}}); err != nil {
		t.Fatal(err)
	}
	if err := transport.respond(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-race", "turn": map[string]any{"id": "turn-race", "status": "interrupted", "durationMs": int64(5)}}}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case terminal := <-terminals:
		if terminal.Status != "interrupted" {
			t.Fatalf("terminal=%+v", terminal)
		}
	case <-time.After(testTimeout):
		t.Fatal("immediate terminal dropped")
	}

	// A second active launch receives a bounded unavailable callback when the
	// single App Server transport dies. The callback itself never performs an RPC.
	client2, transport2 := newExperimentalTestClient(t)
	done2 := make(chan error, 1)
	term2 := make(chan TurnTerminal, 1)
	go func() {
		_, err := client2.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-loss", "Project history · Widgets", func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(v TurnTerminal) { term2 <- v })
		done2 <- err
	}()
	thread = nextRequest(t, transport2)
	_ = transport2.respondResult(thread.ID, map[string]any{"thread": map[string]any{"id": "thread-loss", "sessionId": "session-loss", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}})
	name = nextRequest(t, transport2)
	_ = transport2.respondResult(name.ID, map[string]any{})
	read = nextRequest(t, transport2)
	_ = transport2.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-loss", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false}})
	list = nextRequest(t, transport2)
	_ = transport2.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-loss", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false, "source": "appServer"}}, "nextCursor": nil})
	turn = nextRequest(t, transport2)
	_ = transport2.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-loss", "status": "inProgress"}})
	if err := <-done2; err != nil {
		t.Fatal(err)
	}
	_ = transport2.Close()
	select {
	case terminal := <-term2:
		if terminal.Status != "unavailable" {
			t.Fatalf("loss=%+v", terminal)
		}
	case <-time.After(testTimeout):
		t.Fatal("transport loss callback missing")
	}
}

func TestExperimentalHistorianBuffersToolUntilExactTurnBinding(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	calls := make(chan DynamicToolCall, 1)
	events := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/home/plumbob/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-tool-race", "Project history · Widgets", func(_ context.Context, call DynamicToolCall) DynamicToolResponse {
			events <- "tool"
			calls <- call
			return DynamicToolResponse{Success: true, ContentItems: []DynamicToolContent{{Type: "inputText", Text: `{"accepted":true}`}}}
		}, func(TurnTerminal) { events <- "terminal" })
		done <- err
	}()
	thread := nextRequest(t, transport)
	_ = transport.respondResult(thread.ID, map[string]any{"thread": map[string]any{"id": "thread-tool", "sessionId": "session-tool", "cwd": "/home/plumbob/widgets", "status": map[string]any{"type": "active"}}})
	name := nextRequest(t, transport)
	_ = transport.respondResult(name.ID, map[string]any{})
	read := nextRequest(t, transport)
	_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-tool", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false}})
	list := nextRequest(t, transport)
	_ = transport.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-tool", "name": "Project history · Widgets", "cwd": "/home/plumbob/widgets", "ephemeral": false, "source": "vscode"}}, "nextCursor": nil})
	turn := nextRequest(t, transport)
	_ = transport.respond(map[string]any{"id": 991, "method": "item/tool/call", "params": map[string]any{"callId": "early", "threadId": "thread-tool", "turnId": "turn-tool", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "metadata"}}})
	_ = transport.respond(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-tool", "turn": map[string]any{"id": "turn-tool", "status": "completed", "durationMs": int64(5)}}})
	_ = transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-tool", "status": "inProgress"}})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-calls:
		if call.CallID != "early" {
			t.Fatalf("call=%+v", call)
		}
	case <-time.After(testTimeout):
		t.Fatal("early tool call dropped")
	}
	for _, want := range []string{"tool", "terminal"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event=%q want %q", got, want)
			}
		case <-time.After(testTimeout):
			t.Fatalf("missing %s event", want)
		}
	}
	response := nextRequest(t, transport)
	if string(bytes.TrimSpace(response.ID)) != "991" || len(response.Error) != 0 {
		t.Fatalf("response=%+v", response)
	}
	_ = transport.respond(map[string]any{"id": 992, "method": "item/tool/call", "params": map[string]any{"callId": "wrong", "threadId": "thread-tool", "turnId": "wrong-turn", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "metadata"}}})
	wrong := nextRequest(t, transport)
	if string(bytes.TrimSpace(wrong.ID)) != "992" {
		t.Fatalf("wrong=%+v", wrong)
	}
}

func TestHistorianSchemaBindsProjectServerSideAndAdvertisesCanonicalDoneTasks(t *testing.T) {
	tools := historianDynamicTools()
	report := tools[1]["inputSchema"].(map[string]any)
	outcomes := report["properties"].(map[string]any)["outcomes"].(map[string]any)
	outcome := outcomes["items"].(map[string]any)
	properties := outcome["properties"].(map[string]any)
	if _, ok := properties["project_id"]; ok {
		t.Fatal("project_id must be server-bound")
	}
	if outcome["additionalProperties"] != false {
		t.Fatal("outcome schema is open")
	}
	historical := properties["historical_import"].(map[string]any)
	tasks := historical["properties"].(map[string]any)["tasks"].(map[string]any)
	task := tasks["items"].(map[string]any)
	state := task["properties"].(map[string]any)["state"].(map[string]any)
	if state["const"] != "done" {
		t.Fatalf("state=%v", state)
	}
	provenance := properties["provenance"].(map[string]any)["items"].(map[string]any)
	if _, ok := provenance["properties"].(map[string]any)["recorded_at"]; !ok {
		t.Fatal("recorded_at missing")
	}
	if _, ok := properties["contributors"]; !ok {
		t.Fatal("contributors missing")
	}
}

func TestExperimentalHistorianPendingToolBufferIsBoundedAndExpires(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	called := make(chan struct{}, 1)
	client.mu.Lock()
	client.dynamicHandlers["thread-pending"] = func(context.Context, DynamicToolCall) DynamicToolResponse {
		called <- struct{}{}
		return DynamicToolResponse{}
	}
	client.mu.Unlock()
	for id := 1; id <= 3; id++ {
		if err := transport.respond(map[string]any{"id": 1000 + id, "method": "item/tool/call", "params": map[string]any{"callId": fmt.Sprintf("call-%d", id), "threadId": "thread-pending", "turnId": "turn-pending", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": id, "note": "bounded"}}}); err != nil {
			t.Fatal(err)
		}
	}
	first := nextRequest(t, transport)
	if string(bytes.TrimSpace(first.ID)) != "1003" {
		t.Fatalf("overflow response=%+v", first)
	}
	ids := map[string]bool{}
	deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for len(ids) < 2 {
		response, err := transport.nextRequest(deadline)
		if err != nil {
			t.Fatalf("expired responses=%v: %v", ids, err)
		}
		ids[string(bytes.TrimSpace(response.ID))] = true
	}
	if !ids["1001"] || !ids["1002"] {
		t.Fatalf("expired ids=%v", ids)
	}
	select {
	case <-called:
		t.Fatal("unbound pending tool reached handler")
	default:
	}
}

func TestExperimentalHistorianBoundedCallsRejectOverflow(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	called := make(chan struct{}, 1)
	client.mu.Lock()
	client.dynamicHandlers["thread-limit"] = func(context.Context, DynamicToolCall) DynamicToolResponse {
		called <- struct{}{}
		return DynamicToolResponse{Success: true}
	}
	client.dynamicTurns["thread-limit"] = "turn-limit"
	client.dynamicCalls["thread-limit"] = make(map[string]bool, maxDynamicCallsPerTurn)
	for index := 0; index < maxDynamicCallsPerTurn; index++ {
		client.dynamicCalls["thread-limit"][fmt.Sprintf("used-%d", index)] = true
	}
	client.mu.Unlock()
	if err := transport.respond(map[string]any{"id": 2001, "method": "item/tool/call", "params": map[string]any{"callId": "overflow", "threadId": "thread-limit", "turnId": "turn-limit", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "bounded"}}}); err != nil {
		t.Fatal(err)
	}
	response := nextRequest(t, transport)
	if string(bytes.TrimSpace(response.ID)) != "2001" {
		t.Fatalf("response=%+v", response)
	}
	select {
	case <-called:
		t.Fatal("overflow tool reached handler")
	default:
	}
}
