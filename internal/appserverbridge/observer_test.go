package appserverbridge

import "testing"

func TestObservesDocumentedInventoryLoadedAndStatusWithoutThreadActions(t *testing.T) {
	o := NewObserver()
	for _, tc := range []struct {
		direction Direction
		line      string
		kind      string
	}{
		{ToServer, `{"jsonrpc":"2.0","id":1,"method":"thread/list","params":{"limit":20}}`, ""},
		{FromServer, `{"jsonrpc":"2.0","id":1,"result":{"data":[{"id":"0198-thread","sessionId":"0198-session","name":"Scope audit","preview":"private prose","status":{"type":"idle"}}]}}`, "thread_inventory"},
		{ToServer, `{"jsonrpc":"2.0","id":"loaded","method":"thread/loaded/list","params":{"limit":20}}`, ""},
		{FromServer, `{"jsonrpc":"2.0","id":"loaded","result":{"data":["0198-thread"]}}`, "loaded_inventory"},
		{FromServer, `{"jsonrpc":"2.0","method":"thread/status/changed","params":{"threadId":"0198-thread","status":{"type":"active","activeFlags":[]}}}`, "thread_status"},
	} {
		events, err := o.Process(tc.direction, []byte(tc.line))
		if err != nil {
			t.Fatal(err)
		}
		if tc.kind == "" && len(events) != 0 {
			t.Fatalf("unexpected events=%+v", events)
		}
		if tc.kind != "" && (len(events) != 1 || events[0].Kind != tc.kind) {
			t.Fatalf("events=%+v", events)
		}
		if tc.kind == "thread_inventory" && (events[0].Threads[0].Purpose() != "Scope audit" || events[0].Threads[0].Purpose() == events[0].Threads[0].Preview) {
			t.Fatalf("purpose leaked preview: %+v", events[0])
		}
	}
}

func TestIgnoresWriteAndResumeMethods(t *testing.T) {
	o := NewObserver()
	for _, method := range []string{"thread/read", "thread/resume", "turn/start", "turn/steer", "shellCommand"} {
		events, err := o.Process(ToServer, []byte(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{}}`))
		if err != nil || len(events) != 0 {
			t.Fatalf("method=%s events=%+v err=%v", method, events, err)
		}
	}
}

func TestRejectsMalformedOrUnboundedLines(t *testing.T) {
	o := NewObserver()
	for _, line := range [][]byte{nil, []byte("{}\n{}"), make([]byte, MaxLineBytes+1), []byte(`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"threadId":"T","status":{"type":"invented"}}}`)} {
		if _, err := o.Process(FromServer, line); err == nil {
			t.Fatalf("accepted malformed line len=%d", len(line))
		}
	}
}
