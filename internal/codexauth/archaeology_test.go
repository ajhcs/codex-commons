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

	"codex-commons/internal/domain"
)

func testHistorianPolicy() HistorianPolicy {
	return historianPolicyForTest("standard", true, true, true)
}

func historianThreadStartResult(id, session string) map[string]any {
	return map[string]any{
		// SessionSource describes the session that originated the task and need
		// not echo the client analytics threadSource sent as appServer.
		"thread": map[string]any{"id": id, "sessionId": session, "cwd": "/workspace/widgets", "source": "vscode", "ephemeral": false, "status": map[string]any{"type": "active"}},
		"model":  "gpt-5.6-luna", "approvalPolicy": "never", "sandbox": map[string]any{"type": "readOnly", "networkAccess": false},
	}
}

func respondHistorianSettings(transport *memoryTransport, threadID string) error {
	return transport.respond(map[string]any{"method": "thread/settings/updated", "params": map[string]any{
		"threadId": threadID, "threadSettings": map[string]any{"model": "gpt-5.6-luna", "effort": "max", "cwd": "/workspace/widgets", "approvalPolicy": "never", "multiAgentMode": "explicitRequestOnly", "sandboxPolicy": map[string]any{"type": "readOnly", "networkAccess": false}},
	}})
}

func historianPolicyForTest(depth string, git, docs, history bool) HistorianPolicy {
	policy := HistorianPolicy{Depth: depth, Git: git, Docs: docs, CodexHistory: history}
	limits, ok := (domain.ArchaeologyExecutionPolicy{Depth: depth, Sources: domain.ArchaeologySources{Git: git, Docs: docs, CodexHistory: history}}).Limits()
	if ok {
		policy.MaxOutcomes, policy.MaxProvenance, policy.MaxContributors = limits.MaxOutcomes, limits.MaxProvenancePerOutcome, limits.MaxContributorsPerOutcome
		policy.MaxHistoricalAliases, policy.MaxHistoricalTasks, policy.MaxSourcesExamined = limits.MaxHistoricalAliases, limits.MaxHistoricalTasks, limits.MaxSourcesExamined
	}
	return policy
}

func TestHistorianPolicySchemasCoverEverySourceCombinationAndDepth(t *testing.T) {
	for _, depth := range []string{"quick", "standard", "deep"} {
		for mask := 1; mask < 8; mask++ {
			policy := historianPolicyForTest(depth, mask&1 != 0, mask&2 != 0, mask&4 != 0)
			if !validHistorianPolicy(policy) {
				t.Fatalf("valid policy rejected: %+v", policy)
			}
			tools := historianDynamicTools(policy)
			report := tools[1]["inputSchema"].(map[string]any)
			outcomes := report["properties"].(map[string]any)["outcomes"].(map[string]any)
			if outcomes["maxItems"] != policy.MaxOutcomes {
				t.Fatalf("depth=%s mask=%d max outcomes=%v", depth, mask, outcomes["maxItems"])
			}
			outcome := outcomes["items"].(map[string]any)
			properties := outcome["properties"].(map[string]any)
			if properties["source_count"].(map[string]any)["maximum"] != policy.MaxSourcesExamined {
				t.Fatalf("depth=%s mask=%d source cap=%v", depth, mask, properties["source_count"])
			}
			provenance := properties["provenance"].(map[string]any)["items"].(map[string]any)["oneOf"].([]map[string]any)
			if len(provenance) != bitsInMask(mask) {
				t.Fatalf("depth=%s mask=%d variants=%d", depth, mask, len(provenance))
			}
			historical := properties["historical_import"].(map[string]any)
			if _, supplied := historical["properties"].(map[string]any)["batch_id"]; supplied {
				t.Fatal("historical batch identity must be server-bound")
			}
			tasks := historical["properties"].(map[string]any)["tasks"].(map[string]any)
			task := tasks["items"].(map[string]any)["properties"].(map[string]any)
			if task["attributions"].(map[string]any)["maxItems"].(int)*policy.MaxHistoricalTasks > 200 || task["events"].(map[string]any)["maxItems"].(int)*policy.MaxHistoricalTasks > 500 {
				t.Fatalf("schema totals exceed canonical caps: %+v", policy)
			}
		}
	}
	if validHistorianPolicy(historianPolicyForTest("standard", false, false, false)) {
		t.Fatal("all-disabled source policy accepted")
	}
}

func bitsInMask(mask int) int {
	count := 0
	for mask > 0 {
		count += mask & 1
		mask >>= 1
	}
	return count
}

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
	if params["useStateDbOnly"] != true || params["limit"] != float64(workspacePageSize) || params["archived"] != false {
		t.Fatalf("params=%v", params)
	}
	if kinds, ok := params["sourceKinds"].([]any); !ok || len(kinds) != 10 {
		t.Fatalf("sourceKinds=%v", params["sourceKinds"])
	}
	origin := "https://github.com/acme/widgets.git"
	if err := transport.respondResult(request.ID, map[string]any{"data": []any{map[string]any{"id": "ignored", "cwd": "/workspace/widgets", "updatedAt": int64(1770000000), "preview": "secret prompt must be ignored", "name": "private", "gitInfo": map[string]any{"originUrl": origin, "branch": "main"}}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	archived := nextRequest(t, transport)
	var archivedParams map[string]any
	if err := json.Unmarshal(archived.Params, &archivedParams); err != nil || archived.Method != "thread/list" || archivedParams["archived"] != true {
		t.Fatalf("archived request=%+v params=%v err=%v", archived, archivedParams, err)
	}
	if err := transport.respondResult(archived.ID, map[string]any{"data": []any{}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || len(result.items) != 1 || result.items[0].CWD != "/workspace/widgets" || result.items[0].GitOrigin != origin {
		t.Fatalf("result=%+v err=%v", result.items, result.err)
	}
}

func TestArchaeologyInventoryAcceptsRealisticPreviewHeavyPageOfOneHundred(t *testing.T) {
	client, transport := newTestClient(t)
	readRequest := func() testRequest {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		request, err := transport.nextRequest(ctx)
		if err != nil {
			t.Fatalf("receive preview-heavy inventory request: %v", err)
		}
		return request
	}
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
	request := readRequest()
	rows := make([]any, 100)
	preview := strings.Repeat("private-preview-byte", 5000)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprintf("thread-%03d", i), "cwd": fmt.Sprintf("/workspace/project-%03d", i), "updatedAt": int64(1770000000 + i), "preview": preview, "name": "private-name"}
	}
	if err := transport.respondResult(request.ID, map[string]any{"data": rows, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	archived := readRequest()
	_ = transport.respondResult(archived.ID, map[string]any{"data": []any{}, "nextCursor": nil})
	got := <-done
	if got.err != nil || len(got.items) != 100 {
		t.Fatalf("items=%d err=%v", len(got.items), got.err)
	}
	for _, item := range got.items {
		if strings.Contains(fmt.Sprintf("%+v", item), "private") {
			t.Fatalf("preview/name escaped metadata projection: %+v", item)
		}
	}
}

func TestRenameHistorianTaskRequiresExactDurableReadback(t *testing.T) {
	for _, test := range []struct {
		name, readName string
		ephemeral      bool
		wantErr        bool
	}{{"exact", "Project history · Commons", false, false}, {"stale name", "provisional", false, true}, {"ephemeral", "Project history · Commons", true, true}} {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newExperimentalTestClient(t)
			done := make(chan error, 1)
			go func() {
				done <- client.RenameHistorianTask(context.Background(), "thread-1", "Project history · Commons")
			}()
			set := nextRequest(t, transport)
			if set.Method != "thread/name/set" {
				t.Fatalf("method=%s", set.Method)
			}
			_ = transport.respondResult(set.ID, map[string]any{})
			read := nextRequest(t, transport)
			if read.Method != "thread/read" {
				t.Fatalf("method=%s", read.Method)
			}
			_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-1", "name": test.readName, "ephemeral": test.ephemeral}})
			err := <-done
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestManagedProcessHistorianRenameReadbackAndFailurePropagation(t *testing.T) {
	for _, test := range []struct {
		name, readName string
		wantErr        error
	}{
		{name: "exact readback", readName: "Project history · Commons"},
		{name: "mismatched readback", readName: "provisional", wantErr: ErrProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newExperimentalTestClient(t)
			managed := &ManagedProcessClient{ctx: context.Background(), client: client}
			done := make(chan error, 1)
			go func() {
				done <- managed.RenameHistorianTask(context.Background(), "thread-managed", "Project history · Commons")
			}()

			set := nextRequest(t, transport)
			if set.Method != "thread/name/set" {
				t.Fatalf("method=%s, want thread/name/set", set.Method)
			}
			if err := transport.respondResult(set.ID, map[string]any{}); err != nil {
				t.Fatal(err)
			}
			read := nextRequest(t, transport)
			if read.Method != "thread/read" {
				t.Fatalf("method=%s, want thread/read", read.Method)
			}
			if err := transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-managed", "name": test.readName, "ephemeral": false}}); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, test.wantErr) || test.wantErr == nil && err != nil {
				t.Fatalf("err=%v want=%v", err, test.wantErr)
			}
		})
	}
}

func TestArchaeologyInventoryUsesHundredItemPagesForLargeCodexHistory(t *testing.T) {
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
	const total = 2431
	calls := 0
	for offset := 0; offset < total; offset += workspacePageSize {
		request := nextRequest(t, transport)
		if request.Method != "thread/list" {
			t.Fatalf("method=%s", request.Method)
		}
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatal(err)
		}
		if params["limit"] != float64(100) || params["useStateDbOnly"] != true || params["archived"] != false {
			t.Fatalf("params=%v", params)
		}
		count := workspacePageSize
		if total-offset < count {
			count = total - offset
		}
		data := make([]any, 0, count)
		for index := 0; index < count; index++ {
			data = append(data, map[string]any{
				"id": fmt.Sprintf("thread-%04d", offset+index), "cwd": fmt.Sprintf("/workspace/project-%04d", offset+index), "updatedAt": int64(1770000000 + offset + index),
			})
		}
		var next any
		if offset+count < total {
			next = fmt.Sprintf("cursor-%04d", offset+count)
		}
		if err := transport.respondResult(request.ID, map[string]any{"data": data, "nextCursor": next}); err != nil {
			t.Fatal(err)
		}
		calls++
	}
	archived := nextRequest(t, transport)
	var archivedParams map[string]any
	if err := json.Unmarshal(archived.Params, &archivedParams); err != nil || archivedParams["archived"] != true {
		t.Fatalf("archived params=%v err=%v", archivedParams, err)
	}
	if err := transport.respondResult(archived.ID, map[string]any{"data": []any{}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	calls++
	result := <-done
	if result.err != nil || len(result.items) != total {
		t.Fatalf("items=%d err=%v", len(result.items), result.err)
	}
	if want := (total+workspacePageSize-1)/workspacePageSize + 1; calls != want || calls != 26 {
		t.Fatalf("thread/list calls=%d want=%d", calls, want)
	}
}

func TestArchaeologyInventoryIncludesArchivedOnlyProjectsAndDeduplicatesThreadIDs(t *testing.T) {
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
	active := nextRequest(t, transport)
	var activeParams map[string]any
	if err := json.Unmarshal(active.Params, &activeParams); err != nil || activeParams["archived"] != false {
		t.Fatalf("active params=%v err=%v", activeParams, err)
	}
	if err := transport.respondResult(active.ID, map[string]any{"data": []any{map[string]any{"id": "shared-thread", "cwd": "/workspace/active", "updatedAt": int64(1770000000)}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	archived := nextRequest(t, transport)
	var archivedParams map[string]any
	if err := json.Unmarshal(archived.Params, &archivedParams); err != nil || archivedParams["archived"] != true {
		t.Fatalf("archived params=%v err=%v", archivedParams, err)
	}
	if err := transport.respondResult(archived.ID, map[string]any{"data": []any{
		map[string]any{"id": "shared-thread", "cwd": "/workspace/duplicate-must-not-count", "updatedAt": int64(1770000001)},
		map[string]any{"id": "archived-only-thread", "cwd": "/workspace/archived-only", "updatedAt": int64(1760000000)},
	}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || len(result.items) != 2 || result.items[0].CWD != "/workspace/active" || result.items[1].CWD != "/workspace/archived-only" {
		t.Fatalf("items=%+v err=%v", result.items, result.err)
	}
}

func TestArchaeologyInventoryGlobalTenThousandBoundSkipsArchivedAfterCapacity(t *testing.T) {
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
	for page := 0; page < maxWorkspaceInventory/workspacePageSize; page++ {
		request := nextRequest(t, transport)
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err != nil || params["archived"] != false {
			t.Fatalf("page=%d params=%v err=%v", page, params, err)
		}
		data := make([]any, 0, workspacePageSize)
		for index := 0; index < workspacePageSize; index++ {
			ordinal := page*workspacePageSize + index
			data = append(data, map[string]any{"id": fmt.Sprintf("thread-%05d", ordinal), "cwd": fmt.Sprintf("/workspace/project-%05d", ordinal), "updatedAt": int64(1770000000 + ordinal)})
		}
		if err := transport.respondResult(request.ID, map[string]any{"data": data, "nextCursor": fmt.Sprintf("cursor-%03d", page+1)}); err != nil {
			t.Fatal(err)
		}
	}
	result := <-done
	if result.err != nil || len(result.items) != maxWorkspaceInventory {
		t.Fatalf("items=%d err=%v", len(result.items), result.err)
	}
}

func TestHistorianTaskRecoveryRequiresOneExactNamedThreadAndOneMetadataOnlyTurn(t *testing.T) {
	const cwd = "/workspace/widgets"
	const title = "Project history · Widgets · ARJ-exact"
	for _, test := range []struct {
		name          string
		active        []any
		wantFound     bool
		wantThreadID  string
		wantListCalls int
	}{
		{name: "zero", active: []any{}, wantListCalls: 2},
		{name: "multiple", active: []any{
			map[string]any{"id": "thread-one", "sessionId": "session-one", "cwd": cwd, "source": "appServer", "name": title, "ephemeral": false},
			map[string]any{"id": "thread-two", "sessionId": "session-two", "cwd": cwd, "source": "vscode", "name": title, "ephemeral": false},
		}, wantListCalls: 1},
		{name: "one exact", active: []any{
			map[string]any{"id": "thread-exact", "sessionId": "session-exact", "cwd": cwd, "source": "appServer", "name": title, "ephemeral": false, "preview": "private prompt ignored", "status": map[string]any{"type": "active"}},
			map[string]any{"id": "thread-nearby", "sessionId": "session-nearby", "cwd": cwd, "source": "appServer", "name": title + " extra", "ephemeral": false},
		}, wantFound: true, wantThreadID: "thread-exact", wantListCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, transport := newExperimentalTestClient(t)
			done := make(chan struct {
				launch TaskLaunch
				found  bool
				err    error
			}, 1)
			go func() {
				launch, found, err := client.FindHistorianTask(context.Background(), cwd, title)
				done <- struct {
					launch TaskLaunch
					found  bool
					err    error
				}{launch, found, err}
			}()
			for call := 0; call < test.wantListCalls; call++ {
				request := nextRequest(t, transport)
				if request.Method != "thread/list" {
					t.Fatalf("method=%s", request.Method)
				}
				var params map[string]any
				if err := json.Unmarshal(request.Params, &params); err != nil {
					t.Fatal(err)
				}
				// App Server's text-search index can lag the state DB after a
				// task becomes visible. Recovery must enumerate the bounded exact
				// cwd and perform the title comparison locally instead of relying
				// on searchTerm.
				if _, present := params["searchTerm"]; present || params["useStateDbOnly"] != true || params["cwd"] != cwd || params["limit"] != float64(workspacePageSize) || params["archived"] != (call == 1) {
					t.Fatalf("params=%v", params)
				}
				data := []any{}
				if call == 0 {
					data = test.active
				}
				if err := transport.respondResult(request.ID, map[string]any{"data": data, "nextCursor": nil}); err != nil {
					t.Fatal(err)
				}
			}
			if test.wantFound {
				read := nextRequest(t, transport)
				assertParams(t, read.Params, map[string]any{"threadId": "thread-exact", "includeTurns": false})
				if read.Method != "thread/read" {
					t.Fatalf("method=%s", read.Method)
				}
				if err := transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-exact", "sessionId": "session-exact", "cwd": cwd, "source": "appServer", "name": title, "ephemeral": false, "preview": "private prompt ignored"}}); err != nil {
					t.Fatal(err)
				}
				turns := nextRequest(t, transport)
				if turns.Method != "thread/turns/list" {
					t.Fatalf("method=%s", turns.Method)
				}
				assertParams(t, turns.Params, map[string]any{"threadId": "thread-exact", "limit": float64(2), "sortDirection": "asc", "itemsView": "notLoaded"})
				if err := transport.respondResult(turns.ID, map[string]any{"data": []any{map[string]any{"id": "turn-exact", "status": "inProgress", "items": []any{}, "itemsView": "notLoaded"}}, "nextCursor": nil}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case result := <-done:
				if result.err != nil || result.found != test.wantFound || result.launch.ThreadID != test.wantThreadID {
					t.Fatalf("launch=%+v found=%v err=%v", result.launch, result.found, result.err)
				}
				if test.wantFound && (result.launch.SessionID != "session-exact" || result.launch.TurnID != "turn-exact") {
					t.Fatalf("launch=%+v", result.launch)
				}
			case <-time.After(testTimeout):
				t.Fatal("recovery lookup timed out")
			}
		})
	}
}

func TestHistorianTaskRecoveryRejectsLoadedItemsAndAmbiguousTurns(t *testing.T) {
	const cwd = "/workspace/widgets"
	const title = "Project history · Widgets · ARJ-exact"
	for _, turnsResult := range []map[string]any{
		{"data": []any{map[string]any{"id": "turn-exact", "status": "inProgress", "items": []any{map[string]any{"secret": "must-not-be-loaded"}}, "itemsView": "full"}}, "nextCursor": nil},
		{"data": []any{map[string]any{"id": "turn-one", "status": "completed", "items": []any{}, "itemsView": "notLoaded"}, map[string]any{"id": "turn-two", "status": "inProgress", "items": []any{}, "itemsView": "notLoaded"}}, "nextCursor": nil},
	} {
		client, transport := newExperimentalTestClient(t)
		done := make(chan bool, 1)
		go func() {
			_, found, _ := client.FindHistorianTask(context.Background(), cwd, title)
			done <- found
		}()
		active := nextRequest(t, transport)
		_ = transport.respondResult(active.ID, map[string]any{"data": []any{map[string]any{"id": "thread-exact", "sessionId": "session-exact", "cwd": cwd, "source": "appServer", "name": title, "ephemeral": false}}, "nextCursor": nil})
		archived := nextRequest(t, transport)
		_ = transport.respondResult(archived.ID, map[string]any{"data": []any{}, "nextCursor": nil})
		read := nextRequest(t, transport)
		_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-exact", "sessionId": "session-exact", "cwd": cwd, "source": "appServer", "name": title, "ephemeral": false}})
		turns := nextRequest(t, transport)
		_ = transport.respondResult(turns.ID, turnsResult)
		if <-done {
			t.Fatal("ambiguous or content-bearing turn metadata was bound")
		}
	}
}

func TestHistorianTaskInventoryIncludesAllSourcesAndDeduplicatesArchives(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	done := make(chan struct {
		items []TaskIdentity
		err   error
	}, 1)
	go func() {
		items, err := client.ListHistorianTasks(context.Background(), "/workspace/widgets")
		done <- struct {
			items []TaskIdentity
			err   error
		}{items: items, err: err}
	}()
	active := nextRequest(t, transport)
	var params map[string]any
	if json.Unmarshal(active.Params, &params) != nil || params["cwd"] != "/workspace/widgets" || params["archived"] != false {
		t.Fatalf("active params=%#v", params)
	}
	sources := params["sourceKinds"].([]any)
	foundSubagent := false
	for _, source := range sources {
		foundSubagent = foundSubagent || source == "subAgentThreadSpawn"
	}
	if !foundSubagent {
		t.Fatalf("all-source inventory omitted subagent sources: %#v", sources)
	}
	thread := map[string]any{"id": "thread-one", "sessionId": "session-one", "cwd": "/workspace/widgets", "source": "appServer", "name": "One", "ephemeral": false}
	_ = transport.respondResult(active.ID, map[string]any{"data": []any{thread}, "nextCursor": nil})
	archived := nextRequest(t, transport)
	_ = transport.respondResult(archived.ID, map[string]any{"data": []any{thread, map[string]any{"id": "thread-child", "sessionId": "session-child", "cwd": "/workspace/widgets", "source": "subAgentThreadSpawn", "name": "Child", "ephemeral": false}}, "nextCursor": nil})
	result := <-done
	if result.err != nil || len(result.items) != 2 || result.items[0].ThreadID != "thread-child" || result.items[1].ThreadID != "thread-one" {
		t.Fatalf("items=%+v err=%v", result.items, result.err)
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
		value, err := client.LaunchTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "thread={{CODEX_THREAD_ID}} session={{CODEX_SESSION_ID}}", "commons-launch")
		launchDone <- struct {
			value TaskLaunch
			err   error
		}{value, err}
	}()
	threadRequest := nextRequest(t, transport)
	if threadRequest.Method != "thread/start" {
		t.Fatalf("method=%s", threadRequest.Method)
	}
	assertParams(t, threadRequest.Params, map[string]any{"cwd": "/workspace/widgets", "model": "gpt-5.6-luna", "ephemeral": false, "sandbox": "workspace-write", "threadSource": "appServer"})
	if err := transport.respondResult(threadRequest.ID, map[string]any{"thread": map[string]any{"id": "thread-1", "sessionId": "session-1", "cwd": "/workspace/widgets", "status": map[string]any{"type": "active"}}}); err != nil {
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
	readRequest := nextRequest(t, transport)
	if readRequest.Method != "thread/list" {
		t.Fatalf("method=%s", readRequest.Method)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "thread-1", "cwd": "/workspace/widgets", "source": "appServer"}}, "nextCursor": nil}); err != nil {
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
		_, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-safe-launch", "Project history · Widgets", testHistorianPolicy(), nil, nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("disabled experimental launch err=%v", err)
		}
		return
	}
	calls := make(chan DynamicToolCall, 1)
	terminals := make(chan TurnTerminal, 1)
	type launchResult struct {
		value TaskLaunch
		err   error
	}
	launchDone := make(chan launchResult, 1)
	go func() {
		value, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project and use the Commons project-history tools. No canonical history may be applied.", "commons-safe-launch", "Project history · Widgets", testHistorianPolicy(), func(_ context.Context, call DynamicToolCall) DynamicToolResponse {
			calls <- call
			return DynamicToolResponse{Success: true, ContentItems: []DynamicToolContent{{Type: "inputText", Text: `{"accepted":true}`}}}
		}, func(terminal TurnTerminal) { terminals <- terminal })
		launchDone <- launchResult{value: value, err: err}
	}()
	thread := nextRequest(t, transport)
	if thread.Method != "thread/start" {
		t.Fatalf("method=%s", thread.Method)
	}
	var threadParams map[string]any
	if err := json.Unmarshal(thread.Params, &threadParams); err != nil {
		t.Fatal(err)
	}
	if threadParams["sandbox"] != "read-only" || threadParams["approvalPolicy"] != "never" || threadParams["ephemeral"] != false || threadParams["threadSource"] != "appServer" {
		t.Fatalf("historian thread/start is not exact read-only contract: %#v", threadParams)
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
	if err := transport.respondResult(thread.ID, historianThreadStartResult("thread-safe", "session-safe")); err != nil {
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
	if err := transport.respondResult(proofRequest.ID, map[string]any{"thread": map[string]any{"id": "thread-safe", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "vscode", "ephemeral": false}}); err != nil {
		t.Fatal(err)
	}
	turn := nextRequest(t, transport)
	if turn.Method != "turn/start" {
		t.Fatalf("pre-turn state-db visibility request: method=%s", turn.Method)
	}
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
	if err := respondHistorianSettings(transport, "thread-safe"); err != nil {
		t.Fatal(err)
	}
	readRequest := nextRequest(t, transport)
	if readRequest.Method != "thread/list" {
		t.Fatalf("method=%s", readRequest.Method)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "other-thread", "cwd": "/workspace/widgets", "source": "vscode", "name": "Other task", "ephemeral": false}}, "nextCursor": "page-2"}); err != nil {
		t.Fatal(err)
	}
	readRequest = nextRequest(t, transport)
	var listParams map[string]any
	if err := json.Unmarshal(readRequest.Params, &listParams); err != nil || listParams["cursor"] != "page-2" {
		t.Fatalf("second page params=%v err=%v", listParams, err)
	}
	if err := transport.respondResult(readRequest.ID, map[string]any{"data": []any{map[string]any{"id": "thread-safe", "cwd": "/workspace/widgets", "source": "vscode", "name": "Project history · Widgets", "ephemeral": false}}, "nextCursor": nil}); err != nil {
		t.Fatal(err)
	}
	launched := <-launchDone
	if launched.err != nil {
		t.Fatal(launched.err)
	}
	if launched.value.Model != "gpt-5.6-luna" || launched.value.Effort != "max" || launched.value.Sandbox != "readOnly" || launched.value.Approval != "never" || launched.value.Network || launched.value.MultiAgent != "explicitRequestOnly" {
		t.Fatalf("effective settings=%+v", launched.value)
	}
	if verified, ok := client.VerifiedHistorianSettings("thread-safe"); !ok || verified != launched.value {
		t.Fatalf("verified=%+v ok=%v", verified, ok)
	}
	quietCtx, quietCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer quietCancel()
	if duplicate, err := transport.nextRequest(quietCtx); err == nil {
		t.Fatalf("duplicate post-acceptance request=%+v", duplicate)
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

func TestHistorianLaunchRequiresExperimentalDynamicTools(t *testing.T) {
	client, _ := newTestClient(t)
	_, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "single-purpose report grant: secret", "commons-safe-launch", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(TurnTerminal) {})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestExperimentalHistorianBuffersImmediateTerminalAndSignalsTransportLoss(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	terminals := make(chan TurnTerminal, 2)
	done := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project and submit a bounded proposal.", "commons-race", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(value TurnTerminal) { terminals <- value })
		done <- err
	}()
	thread := nextRequest(t, transport)
	if err := transport.respondResult(thread.ID, historianThreadStartResult("thread-race", "session-race")); err != nil {
		t.Fatal(err)
	}
	name := nextRequest(t, transport)
	if err := transport.respondResult(name.ID, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	read := nextRequest(t, transport)
	if err := transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-race", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "appServer", "ephemeral": false}}); err != nil {
		t.Fatal(err)
	}
	turn := nextRequest(t, transport)
	if err := transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-race", "status": "inProgress"}}); err != nil {
		t.Fatal(err)
	}
	if err := respondHistorianSettings(transport, "thread-race"); err != nil {
		t.Fatal(err)
	}
	list := nextRequest(t, transport)
	if err := transport.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-race", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "ephemeral": false, "source": "vscode"}}, "nextCursor": nil}); err != nil {
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
		_, err := client2.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-loss", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(v TurnTerminal) { term2 <- v })
		done2 <- err
	}()
	thread = nextRequest(t, transport2)
	_ = transport2.respondResult(thread.ID, historianThreadStartResult("thread-loss", "session-loss"))
	name = nextRequest(t, transport2)
	_ = transport2.respondResult(name.ID, map[string]any{})
	read = nextRequest(t, transport2)
	_ = transport2.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-loss", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "appServer", "ephemeral": false}})
	turn = nextRequest(t, transport2)
	_ = transport2.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-loss", "status": "inProgress"}})
	_ = respondHistorianSettings(transport2, "thread-loss")
	list = nextRequest(t, transport2)
	_ = transport2.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-loss", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "ephemeral": false, "source": "appServer"}}, "nextCursor": nil})
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
	bound := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-tool-race", "Project history · Widgets", testHistorianPolicy(), func(_ context.Context, call DynamicToolCall) DynamicToolResponse {
			<-bound
			events <- "tool"
			calls <- call
			return DynamicToolResponse{Success: true, ContentItems: []DynamicToolContent{{Type: "inputText", Text: `{"accepted":true}`}}}
		}, func(TurnTerminal) { events <- "terminal" })
		done <- err
	}()
	thread := nextRequest(t, transport)
	_ = transport.respondResult(thread.ID, historianThreadStartResult("thread-tool", "session-tool"))
	name := nextRequest(t, transport)
	_ = transport.respondResult(name.ID, map[string]any{})
	read := nextRequest(t, transport)
	_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-tool", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "appServer", "ephemeral": false}})
	turn := nextRequest(t, transport)
	_ = transport.respond(map[string]any{"id": 991, "method": "item/tool/call", "params": map[string]any{"callId": "early", "threadId": "thread-tool", "turnId": "turn-tool", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "metadata"}}})
	_ = transport.respond(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread-tool", "turn": map[string]any{"id": "turn-tool", "status": "completed", "durationMs": int64(5)}}})
	_ = transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-tool", "status": "inProgress"}})
	_ = respondHistorianSettings(transport, "thread-tool")
	list := nextRequest(t, transport)
	_ = transport.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-tool", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "ephemeral": false, "source": "vscode"}}, "nextCursor": nil})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("launch blocked behind pre-bind tool callback")
	}
	select {
	case event := <-events:
		t.Fatalf("callback ran before durable-bind gate: %s", event)
	default:
	}
	close(bound)
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
	tools := historianDynamicTools(testHistorianPolicy())
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
	variants, ok := provenance["oneOf"].([]map[string]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("policy-specific provenance=%#v", provenance)
	}
	if _, ok := variants[0]["properties"].(map[string]any)["recorded_at"]; !ok {
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

func TestExperimentalClientCloseCancelsAndJoinsDynamicCallback(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	entered := make(chan struct{})
	exited := make(chan struct{})
	client.mu.Lock()
	client.dynamicHandlers["thread-close"] = func(ctx context.Context, _ DynamicToolCall) DynamicToolResponse {
		close(entered)
		<-ctx.Done()
		close(exited)
		return DynamicToolResponse{}
	}
	client.dynamicTurns["thread-close"] = "turn-close"
	client.mu.Unlock()
	if err := transport.respond(map[string]any{"id": 1201, "method": "item/tool/call", "params": map[string]any{"callId": "close-call", "threadId": "thread-close", "turnId": "turn-close", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "bounded"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(testTimeout):
		t.Fatal("dynamic callback did not start")
	}
	closed := make(chan struct{})
	go func() {
		_ = client.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(testTimeout):
		t.Fatal("Close did not join the dynamic callback")
	}
	select {
	case <-exited:
	default:
		t.Fatal("Close returned before the dynamic callback exited")
	}
}

func TestExperimentalHistorianReportArgumentBoundary(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	called := make(chan int, 2)
	client.mu.Lock()
	client.dynamicHandlers["thread-size"] = func(_ context.Context, call DynamicToolCall) DynamicToolResponse {
		called <- len(call.Arguments)
		return DynamicToolResponse{Success: true, ContentItems: []DynamicToolContent{{Type: "inputText", Text: `{"accepted":true}`}}}
	}
	client.dynamicTurns["thread-size"] = "turn-size"
	client.mu.Unlock()
	argumentsOfSize := func(size int) []byte {
		prefix, suffix := []byte(`{"pad":"`), []byte(`"}`)
		return append(append(append([]byte(nil), prefix...), bytes.Repeat([]byte{'x'}, size-len(prefix)-len(suffix))...), suffix...)
	}
	near := argumentsOfSize(domain.ArchaeologyNativeReportMaxBytes)
	if err := transport.respond(map[string]any{"id": 1301, "method": "item/tool/call", "params": map[string]any{"callId": "near", "threadId": "thread-size", "turnId": "turn-size", "tool": "commons_project_history_report", "arguments": json.RawMessage(near)}}); err != nil {
		t.Fatal(err)
	}
	select {
	case size := <-called:
		if size != domain.ArchaeologyNativeReportMaxBytes {
			t.Fatalf("handler bytes=%d", size)
		}
	case <-time.After(testTimeout):
		t.Fatal("near-limit report did not reach handler")
	}
	if response := nextRequest(t, transport); string(bytes.TrimSpace(response.ID)) != "1301" {
		t.Fatalf("near response=%+v", response)
	}
	over := argumentsOfSize(domain.ArchaeologyNativeReportMaxBytes + 1)
	if err := transport.respond(map[string]any{"id": 1302, "method": "item/tool/call", "params": map[string]any{"callId": "over", "threadId": "thread-size", "turnId": "turn-size", "tool": "commons_project_history_report", "arguments": json.RawMessage(over)}}); err != nil {
		t.Fatal(err)
	}
	if response := nextRequest(t, transport); string(bytes.TrimSpace(response.ID)) != "1302" {
		t.Fatalf("over response=%+v", response)
	}
	select {
	case size := <-called:
		t.Fatalf("over-limit report reached handler with %d bytes", size)
	default:
	}
}

func TestExperimentalHistorianLaunchFailureRespondsOnceToBufferedTool(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	done := make(chan error, 1)
	go func() {
		_, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-tool-failure", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(TurnTerminal) {})
		done <- err
	}()
	thread := nextRequest(t, transport)
	_ = transport.respondResult(thread.ID, historianThreadStartResult("thread-failure", "session-failure"))
	name := nextRequest(t, transport)
	_ = transport.respondResult(name.ID, map[string]any{})
	read := nextRequest(t, transport)
	_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-failure", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "appServer", "ephemeral": false}})
	turn := nextRequest(t, transport)
	_ = transport.respond(map[string]any{"id": 1401, "method": "item/tool/call", "params": map[string]any{"callId": "buffered", "threadId": "thread-failure", "turnId": "turn-never-bound", "tool": "commons_project_history_progress", "arguments": map[string]any{"phase": "inspecting_sources", "sources_examined": 1, "note": "bounded"}}})
	deadline := time.Now().Add(testTimeout)
	for {
		client.mu.Lock()
		buffered := len(client.pendingTools["thread-failure"])
		client.mu.Unlock()
		if buffered == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("early tool was not buffered")
		}
		time.Sleep(time.Millisecond)
	}
	_ = transport.respond(map[string]any{"id": turn.ID, "error": map[string]any{"code": -32000, "message": "turn rejected"}})
	if err := <-done; err == nil {
		t.Fatal("turn failure was not returned")
	}
	response := nextRequest(t, transport)
	if string(bytes.TrimSpace(response.ID)) != "1401" {
		t.Fatalf("cleanup response=%+v", response)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if duplicate, err := transport.nextRequest(ctx); err == nil {
		t.Fatalf("duplicate JSON-RPC response=%+v", duplicate)
	}
}

func TestExperimentalHistorianVisibilityPollsUntilStateDBMaterializes(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	type result struct {
		launch TaskLaunch
		err    error
	}
	done := make(chan result, 1)
	go func() {
		launch, err := client.LaunchHistorianTask(context.Background(), "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-visibility", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(TurnTerminal) {})
		done <- result{launch: launch, err: err}
	}()
	thread := nextRequest(t, transport)
	_ = transport.respondResult(thread.ID, historianThreadStartResult("thread-visibility", "session-visibility"))
	name := nextRequest(t, transport)
	_ = transport.respondResult(name.ID, map[string]any{})
	read := nextRequest(t, transport)
	_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-visibility", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "vscode", "ephemeral": false}})
	turn := nextRequest(t, transport)
	if turn.Method != "turn/start" {
		t.Fatalf("unexpected pre-turn request=%s", turn.Method)
	}
	_ = transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-visibility", "status": "inProgress"}})
	_ = respondHistorianSettings(transport, "thread-visibility")
	for attempt := 0; attempt < 2; attempt++ {
		list := nextRequest(t, transport)
		if list.Method != "thread/list" {
			t.Fatalf("poll %d method=%s", attempt, list.Method)
		}
		_ = transport.respondResult(list.ID, map[string]any{"data": []any{}, "nextCursor": nil})
	}
	list := nextRequest(t, transport)
	_ = transport.respondResult(list.ID, map[string]any{"data": []any{map[string]any{"id": "thread-visibility", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "ephemeral": false, "source": "vscode"}}, "nextCursor": nil})
	got := <-done
	if got.err != nil || got.launch.ThreadID != "thread-visibility" || got.launch.SessionID != "session-visibility" || got.launch.TurnID != "turn-visibility" || got.launch.Stage != "accepted" {
		t.Fatalf("launch=%+v err=%v", got.launch, got.err)
	}
	if verified, ok := client.VerifiedHistorianSettings("thread-visibility"); !ok || verified.Stage != "accepted" {
		t.Fatalf("accepted receipt=%+v ok=%v", verified, ok)
	}
}

func TestExperimentalHistorianPermanentVisibilityAbsenceRetainsSettingsAndExactIdentity(t *testing.T) {
	client, transport := newExperimentalTestClient(t)
	type result struct {
		launch TaskLaunch
		err    error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 450*time.Millisecond)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		launch, err := client.LaunchHistorianTask(ctx, "/workspace/widgets", "gpt-5.6-luna", "max", "Inspect this project.", "commons-visibility-timeout", "Project history · Widgets", testHistorianPolicy(), func(context.Context, DynamicToolCall) DynamicToolResponse { return DynamicToolResponse{} }, func(TurnTerminal) {})
		done <- result{launch: launch, err: err}
	}()
	thread := nextRequest(t, transport)
	_ = transport.respondResult(thread.ID, historianThreadStartResult("thread-timeout", "session-timeout"))
	name := nextRequest(t, transport)
	_ = transport.respondResult(name.ID, map[string]any{})
	read := nextRequest(t, transport)
	_ = transport.respondResult(read.ID, map[string]any{"thread": map[string]any{"id": "thread-timeout", "name": "Project history · Widgets", "cwd": "/workspace/widgets", "source": "vscode", "ephemeral": false}})
	turn := nextRequest(t, transport)
	_ = transport.respondResult(turn.ID, map[string]any{"turn": map[string]any{"id": "turn-timeout", "status": "inProgress"}})
	_ = respondHistorianSettings(transport, "thread-timeout")
	polls := 0
	for {
		select {
		case got := <-done:
			if got.err == nil || got.launch.ThreadID != "thread-timeout" || got.launch.SessionID != "session-timeout" || got.launch.TurnID != "turn-timeout" || got.launch.Stage != "visibility" || polls < 2 {
				t.Fatalf("launch=%+v err=%v polls=%d", got.launch, got.err, polls)
			}
			verified, ok := client.VerifiedHistorianSettings("thread-timeout")
			if !ok || verified.Stage != "settings_exact" || verified.Model != "gpt-5.6-luna" || verified.Effort != "max" {
				t.Fatalf("retained settings=%+v ok=%v", verified, ok)
			}
			return
		case raw := <-transport.requests:
			var request testRequest
			if err := json.Unmarshal(bytes.TrimSpace(raw), &request); err != nil {
				t.Fatal(err)
			}
			if request.Method != "thread/list" {
				t.Fatalf("method=%s", request.Method)
			}
			polls++
			_ = transport.respondResult(request.ID, map[string]any{"data": []any{}, "nextCursor": nil})
		case <-time.After(testTimeout):
			t.Fatal("visibility timeout regression hung")
		}
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
