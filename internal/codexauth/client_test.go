package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

type memoryTransport struct {
	requests chan []byte
	reader   *io.PipeReader
	writer   *io.PipeWriter
	closed   chan struct{}
	closeOne sync.Once
}

func newMemoryTransport() *memoryTransport {
	reader, writer := io.Pipe()
	return &memoryTransport{
		requests: make(chan []byte, 64),
		reader:   reader,
		writer:   writer,
		closed:   make(chan struct{}),
	}
}

func (t *memoryTransport) Read(p []byte) (int, error) {
	return t.reader.Read(p)
}

func (t *memoryTransport) Write(p []byte) (int, error) {
	payload := append([]byte(nil), p...)
	select {
	case t.requests <- payload:
		return len(p), nil
	case <-t.closed:
		return 0, io.ErrClosedPipe
	}
}

func (t *memoryTransport) Close() error {
	t.closeOne.Do(func() {
		close(t.closed)
		_ = t.writer.Close()
		_ = t.reader.Close()
	})
	return nil
}

func (t *memoryTransport) nextRequest(ctx context.Context) (testRequest, error) {
	select {
	case raw := <-t.requests:
		var request testRequest
		if err := json.Unmarshal(bytes.TrimSpace(raw), &request); err != nil {
			return testRequest{}, err
		}
		return request, nil
	case <-ctx.Done():
		return testRequest{}, ctx.Err()
	}
}

func (t *memoryTransport) respond(value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	_, err = t.writer.Write(body)
	return err
}

func (t *memoryTransport) respondResult(id json.RawMessage, result any) error {
	return t.respond(struct {
		ID     json.RawMessage `json:"id"`
		Result any             `json:"result"`
	}{ID: id, Result: result})
}

func (t *memoryTransport) respondRaw(body []byte) error {
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body = append(append([]byte(nil), body...), '\n')
	}
	_, err := t.writer.Write(body)
	return err
}

type testRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func newTestClient(t *testing.T) (*ClientImpl, *memoryTransport) {
	t.Helper()
	transport := newMemoryTransport()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	type result struct {
		client *ClientImpl
		err    error
	}
	ready := make(chan result, 1)
	go func() {
		client, err := NewWithTransport(ctx, transport)
		ready <- result{client: client, err: err}
	}()

	requestCtx, requestCancel := context.WithTimeout(context.Background(), testTimeout)
	request, err := transport.nextRequest(requestCtx)
	requestCancel()
	if err != nil {
		t.Fatalf("receive initialize request: %v", err)
	}
	if request.Method != "initialize" {
		t.Fatalf("first request method = %q, want initialize", request.Method)
	}
	if id, ok := wireIDKey(request.ID); !ok || id != "1" {
		t.Fatalf("initialize id = %s, want 1", request.ID)
	}
	var params struct {
		ClientInfo struct {
			Name    string `json:"name"`
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		t.Fatalf("decode initialize params: %v", err)
	}
	if got := params.ClientInfo; got.Name != "codex_commons" || got.Title != "Codex Commons" || got.Version == "" {
		t.Fatalf("initialize clientInfo = %+v", got)
	}
	if err := transport.respondResult(request.ID, map[string]any{"serverInfo": map[string]string{"version": "test"}}); err != nil {
		t.Fatalf("respond to initialize: %v", err)
	}

	initializedCtx, initializedCancel := context.WithTimeout(context.Background(), testTimeout)
	initialized, err := transport.nextRequest(initializedCtx)
	initializedCancel()
	if err != nil {
		t.Fatalf("receive initialized notification: %v", err)
	}
	if initialized.Method != "initialized" {
		t.Fatalf("second message method = %q, want initialized", initialized.Method)
	}
	if len(initialized.ID) != 0 && !bytes.Equal(bytes.TrimSpace(initialized.ID), []byte("null")) {
		t.Fatalf("initialized notification unexpectedly has id %s", initialized.ID)
	}

	select {
	case result := <-ready:
		if result.err != nil {
			_ = transport.Close()
			t.Fatalf("complete client handshake: %v", result.err)
		}
		t.Cleanup(func() { _ = result.client.Close() })
		return result.client, transport
	case <-time.After(testTimeout):
		_ = transport.Close()
		t.Fatal("client handshake did not complete")
		return nil, nil
	}
}

func nextRequest(t *testing.T, transport *memoryTransport) testRequest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	request, err := transport.nextRequest(ctx)
	if err != nil {
		t.Fatalf("receive request: %v", err)
	}
	return request
}

func assertParams(t *testing.T, got json.RawMessage, want any) {
	t.Helper()
	var actual any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatalf("decode params %s: %v", got, err)
	}
	wantBody, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode expected params: %v", err)
	}
	var expected any
	if err := json.Unmarshal(wantBody, &expected); err != nil {
		t.Fatalf("decode expected params: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("params = %#v, want %#v", actual, expected)
	}
}

func TestInitializeOrderingAndAvailability(t *testing.T) {
	client, _ := newTestClient(t)
	if !client.Available() {
		t.Fatal("client is unavailable after initialize/initialized handshake")
	}
}

func TestAccountReadStates(t *testing.T) {
	tests := []struct {
		name   string
		result any
		want   AccountState
	}{
		{name: "signed out", result: map[string]any{"account": nil}, want: AccountSignedOut},
		{
			name: "chatgpt signed in",
			result: map[string]any{"account": map[string]any{
				"type": "chatgpt", "email": "person@example.com", "planType": "pro",
			}},
			want: AccountSignedIn,
		},
		{
			name: "unknown account type",
			result: map[string]any{"account": map[string]any{
				"type": "apiKey", "email": nil, "planType": "",
			}},
			want: AccountUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := newTestClient(t)
			stateCh := make(chan struct {
				state AccountState
				err   error
			}, 1)
			go func() {
				state, err := client.AccountState(context.Background())
				stateCh <- struct {
					state AccountState
					err   error
				}{state: state, err: err}
			}()

			request := nextRequest(t, transport)
			if request.Method != "account/read" {
				t.Fatalf("method = %q, want account/read", request.Method)
			}
			assertParams(t, request.Params, map[string]bool{"refreshToken": false})
			if err := transport.respondResult(request.ID, tt.result); err != nil {
				t.Fatalf("respond to account/read: %v", err)
			}

			select {
			case result := <-stateCh:
				if result.err != nil {
					t.Fatalf("AccountState: %v", result.err)
				}
				if result.state != tt.want {
					t.Fatalf("AccountState = %q, want %q", result.state, tt.want)
				}
			case <-time.After(testTimeout):
				t.Fatal("AccountState did not complete")
			}
		})
	}
}

func TestStartDeviceCodeStrictResponseParsing(t *testing.T) {
	tests := []struct {
		name       string
		result     any
		wantDevice DeviceCode
	}{
		{
			name: "valid",
			result: map[string]string{
				"type": "chatgptDeviceCode", "loginId": "login-1",
				"verificationUrl": "https://auth.openai.com/device", "userCode": "ABCD-EFGH",
			},
			wantDevice: DeviceCode{
				LoginID: "login-1", VerificationURL: "https://auth.openai.com/device", UserCode: "ABCD-EFGH",
			},
		},
		{
			name:   "wrong result type",
			result: map[string]string{"type": "oauth", "loginId": "login-1", "verificationUrl": "https://auth.openai.com/device", "userCode": "ABCD-EFGH"},
		},
		{
			name:   "missing login id",
			result: map[string]string{"type": "chatgptDeviceCode", "verificationUrl": "https://auth.openai.com/device", "userCode": "ABCD-EFGH"},
		},
		{
			name:   "null result",
			result: nil,
		},
		{
			name:   "wrong field type",
			result: map[string]any{"type": "chatgptDeviceCode", "loginId": 42, "verificationUrl": "https://auth.openai.com/device", "userCode": "ABCD-EFGH"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := newTestClient(t)
			resultCh := make(chan struct {
				device DeviceCode
				err    error
			}, 1)
			go func() {
				device, err := client.StartDeviceCode(context.Background())
				resultCh <- struct {
					device DeviceCode
					err    error
				}{device: device, err: err}
			}()

			request := nextRequest(t, transport)
			if request.Method != "account/login/start" {
				t.Fatalf("method = %q, want account/login/start", request.Method)
			}
			assertParams(t, request.Params, map[string]string{"type": "chatgptDeviceCode"})
			if err := transport.respondResult(request.ID, tt.result); err != nil {
				t.Fatalf("respond to account/login/start: %v", err)
			}

			select {
			case result := <-resultCh:
				if tt.wantDevice != (DeviceCode{}) {
					if result.err != nil {
						t.Fatalf("StartDeviceCode: %v", result.err)
					}
					if result.device != tt.wantDevice {
						t.Fatalf("device = %+v, want %+v", result.device, tt.wantDevice)
					}
					return
				}
				if !errors.Is(result.err, ErrProtocol) {
					t.Fatalf("StartDeviceCode error = %v, want ErrProtocol", result.err)
				}
			case <-time.After(testTimeout):
				t.Fatal("StartDeviceCode did not complete")
			}
		})
	}
}

func TestLoginCompletedAndAccountUpdatedNotifications(t *testing.T) {
	client, transport := newTestClient(t)
	events := make(chan Event, 2)
	client.SetEventHandler(func(event Event) { events <- event })

	if err := transport.respond(map[string]any{
		"method": "account/login/completed",
		"params": map[string]any{"loginId": "login-1", "success": true},
	}); err != nil {
		t.Fatalf("send login completed notification: %v", err)
	}
	select {
	case event := <-events:
		if event != (Event{Kind: "login_completed", LoginID: "login-1", Success: true}) {
			t.Fatalf("login event = %+v", event)
		}
	case <-time.After(testTimeout):
		t.Fatal("login completed event not delivered")
	}

	pending, err := client.PollLogin(context.Background(), "not-yet-completed")
	if err != nil {
		t.Fatalf("pending PollLogin: %v", err)
	}
	if pending.State != "pending" {
		t.Fatalf("pending PollLogin state = %q, want pending", pending.State)
	}

	resultCh := make(chan struct {
		result LoginResult
		err    error
	}, 1)
	go func() {
		result, err := client.PollLogin(context.Background(), "login-1")
		resultCh <- struct {
			result LoginResult
			err    error
		}{result: result, err: err}
	}()
	request := nextRequest(t, transport)
	if request.Method != "account/read" {
		t.Fatalf("method = %q, want account/read after login completion", request.Method)
	}
	if err := transport.respondResult(request.ID, map[string]any{"account": map[string]any{
		"type": "chatgpt", "email": "person@example.com", "planType": "plus",
	}}); err != nil {
		t.Fatalf("respond to account/read: %v", err)
	}
	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatalf("PollLogin: %v", result.err)
		}
		if result.result.State != "success" || result.result.Account == nil || result.result.Account.Email == nil || *result.result.Account.Email != "person@example.com" {
			t.Fatalf("login result = %+v", result.result)
		}
	case <-time.After(testTimeout):
		t.Fatal("PollLogin did not complete")
	}

	if err := transport.respond(map[string]any{
		"method": "account/updated",
		"params": map[string]string{"authMode": "chatgpt"},
	}); err != nil {
		t.Fatalf("send account updated notification: %v", err)
	}
	select {
	case event := <-events:
		if event != (Event{Kind: "account_updated", AuthMode: "chatgpt"}) {
			t.Fatalf("account update event = %+v", event)
		}
	case <-time.After(testTimeout):
		t.Fatal("account updated event not delivered")
	}
}

func TestCancelLogin(t *testing.T) {
	client, transport := newTestClient(t)
	errorCh := make(chan error, 1)
	go func() { errorCh <- client.CancelLogin(context.Background(), "login-2") }()

	request := nextRequest(t, transport)
	if request.Method != "account/login/cancel" {
		t.Fatalf("method = %q, want account/login/cancel", request.Method)
	}
	assertParams(t, request.Params, map[string]string{"loginId": "login-2"})
	if err := transport.respondResult(request.ID, nil); err != nil {
		t.Fatalf("respond to account/login/cancel: %v", err)
	}
	select {
	case err := <-errorCh:
		if err != nil {
			t.Fatalf("CancelLogin: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("CancelLogin did not complete")
	}
	cancelled, err := client.PollLogin(context.Background(), "login-2")
	if err != nil || cancelled.State != "cancelled" {
		t.Fatalf("cancelled PollLogin = %+v, err=%v", cancelled, err)
	}

	if err := client.CancelLogin(context.Background(), ""); !errors.Is(err, ErrProtocol) {
		t.Fatalf("empty login id error = %v, want ErrProtocol", err)
	}
}
func TestCancelledLoginTrackingIsBounded(t *testing.T) {
	client, transport := newTestClient(t)
	for i := 0; i < maxLoginCompletions+8; i++ {
		loginID := fmt.Sprintf("login-%03d", i)
		done := make(chan error, 1)
		go func() { done <- client.CancelLogin(context.Background(), loginID) }()
		request := nextRequest(t, transport)
		if err := transport.respondResult(request.ID, nil); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		client.mu.Lock()
		cancelled := len(client.cancelled)
		client.mu.Unlock()
		if cancelled > maxLoginCompletions {
			t.Fatalf("cancelled login tombstones=%d, max=%d", cancelled, maxLoginCompletions)
		}
	}
}

func TestSuccessfulLoginCompletionSurvivesTransientAccountReadFailure(t *testing.T) {
	client, transport := newTestClient(t)
	events := make(chan Event, 1)
	client.SetEventHandler(func(event Event) { events <- event })
	if err := transport.respond(map[string]any{
		"method": "account/login/completed",
		"params": map[string]any{"loginId": "login-retry", "success": true},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(testTimeout):
		t.Fatal("login completion notification not delivered")
	}

	first := make(chan error, 1)
	go func() {
		_, err := client.PollLogin(context.Background(), "login-retry")
		first <- err
	}()
	request := nextRequest(t, transport)
	if err := transport.respond(map[string]any{
		"id":    request.ID,
		"error": map[string]any{"code": -32000, "message": "temporary account read failure"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-first; !errors.Is(err, ErrProtocol) {
		t.Fatalf("first poll error=%v, want %v", err, ErrProtocol)
	}

	second := make(chan LoginResult, 1)
	secondErr := make(chan error, 1)
	go func() {
		result, err := client.PollLogin(context.Background(), "login-retry")
		second <- result
		secondErr <- err
	}()
	request = nextRequest(t, transport)
	if err := transport.respondResult(request.ID, map[string]any{"account": map[string]any{"type": "chatgpt", "email": "retry@example.com"}, "requiresOpenaiAuth": true}); err != nil {
		t.Fatal(err)
	}
	if err := <-secondErr; err != nil {
		t.Fatal(err)
	}
	result := <-second
	if result.State != "success" || result.Account == nil || result.Account.Email == nil || *result.Account.Email != "retry@example.com" {
		t.Fatalf("second poll result=%+v", result)
	}
}

func TestResponseCorrelation(t *testing.T) {
	client, transport := newTestClient(t)
	type callResult struct {
		label string
		body  json.RawMessage
		err   error
	}
	results := make(chan callResult, 2)
	for _, label := range []string{"first", "second"} {
		label := label
		go func() {
			body, err := client.call(context.Background(), "test/correlation", map[string]string{"label": label}, false)
			results <- callResult{label: label, body: body, err: err}
		}()
	}

	requests := make(map[string]testRequest, 2)
	for len(requests) < 2 {
		request := nextRequest(t, transport)
		if request.Method != "test/correlation" {
			t.Fatalf("method = %q, want test/correlation", request.Method)
		}
		var params struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Fatalf("decode correlation params: %v", err)
		}
		requests[params.Label] = request
	}

	// Send the responses in the opposite order from the request labels. The
	// result body makes it possible to prove that each waiter received its own
	// response rather than merely observing two successful calls.
	for _, label := range []string{"second", "first"} {
		if err := transport.respondResult(requests[label].ID, map[string]string{"label": label}); err != nil {
			t.Fatalf("respond to %s correlation call: %v", label, err)
		}
	}

	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("%s correlation call: %v", result.label, result.err)
		}
		var body struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(result.body, &body); err != nil {
			t.Fatalf("decode %s response: %v", result.label, err)
		}
		if body.Label != result.label {
			t.Fatalf("%s received response for %q", result.label, body.Label)
		}
	}
}

func TestMalformedAndTrailingJSONFailPendingRequest(t *testing.T) {
	tests := []struct {
		name string
		line func(json.RawMessage) []byte
	}{
		{name: "malformed json", line: func(json.RawMessage) []byte { return []byte("{not-json") }},
		{name: "trailing json", line: func(id json.RawMessage) []byte {
			line := responseResultLine(id, `{"type":"chatgptDeviceCode","loginId":"login-1","verificationUrl":"https://auth.openai.com/device","userCode":"ABCD-EFGH"}`)
			return append(bytes.TrimSuffix(line, []byte{'\n'}), []byte(" trailing\n")...)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, transport := newTestClient(t)
			errorCh := make(chan error, 1)
			go func() {
				_, err := client.StartDeviceCode(context.Background())
				errorCh <- err
			}()
			request := nextRequest(t, transport)
			line := tt.line(request.ID)
			if err := transport.respondRaw(line); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("send invalid response: %v", err)
			}
			select {
			case err := <-errorCh:
				if !errors.Is(err, ErrProtocol) {
					t.Fatalf("request error = %v, want ErrProtocol", err)
				}
			case <-time.After(testTimeout):
				t.Fatal("pending request did not fail")
			}
			if client.Available() {
				t.Fatal("client remained available after protocol failure")
			}
		})
	}
}

func responseResultLine(id json.RawMessage, rawResult string) []byte {
	line := append([]byte(`{"id":`), id...)
	line = append(line, []byte(`,"result":`)...)
	line = append(line, rawResult...)
	line = append(line, []byte("}\n")...)
	return line
}

func TestOversizedLineFailsPendingRequest(t *testing.T) {
	client, transport := newTestClient(t)
	errorCh := make(chan error, 1)
	go func() {
		_, err := client.StartDeviceCode(context.Background())
		errorCh <- err
	}()
	nextRequest(t, transport)
	oversized := append(bytes.Repeat([]byte{'x'}, MaxLineBytes+1), '\n')
	go func() { _ = transport.respondRaw(oversized) }()

	select {
	case err := <-errorCh:
		if !errors.Is(err, ErrLineTooLarge) {
			t.Fatalf("request error = %v, want ErrLineTooLarge", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("oversized request did not fail")
	}
	if client.Available() {
		t.Fatal("client remained available after oversized line")
	}
}

func TestPendingRequestCap(t *testing.T) {
	client, transport := newTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, maxPendingRequests)
	for range maxPendingRequests {
		go func() {
			_, err := client.StartDeviceCode(ctx)
			results <- err
		}()
	}

	for range maxPendingRequests {
		request := nextRequest(t, transport)
		if request.Method != "account/login/start" {
			t.Fatalf("method = %q, want account/login/start", request.Method)
		}
	}
	if _, err := client.StartDeviceCode(context.Background()); !errors.Is(err, ErrPendingLimit) {
		t.Fatalf("request over cap error = %v, want ErrPendingLimit", err)
	}

	_ = client.Close()
	for range maxPendingRequests {
		select {
		case <-results:
		case <-time.After(testTimeout):
			t.Fatal("pending request did not terminate after client close")
		}
	}
}

func TestApprovedEnvironmentFiltersToAllowlist(t *testing.T) {
	source := []string{
		"HOME=/home/test",
		"CODEX_HOME=/home/test/.codex",
		"PATH=/usr/bin",
		"TMPDIR=/tmp/test",
		"HTTP_PROXY=http://proxy.example",
		"HTTPS_PROXY=https://proxy.example",
		"NO_PROXY=localhost",
		"XDG_CONFIG_HOME=/home/test/.config",
		"COMMONS_HUMAN_SECRET=do-not-forward",
		"OPENAI_API_KEY=do-not-forward",
		"DATABASE_URL=do-not-forward",
		"COMMONS_CODEX_BINDING_KEY_FILE=/run/secrets/key",
		"MALFORMED_WITHOUT_EQUALS",
	}
	want := []string{
		"HOME=/home/test",
		"CODEX_HOME=/home/test/.codex",
		"PATH=/usr/bin",
		"TMPDIR=/tmp/test",
		"HTTP_PROXY=http://proxy.example",
		"HTTPS_PROXY=https://proxy.example",
		"NO_PROXY=localhost",
		"XDG_CONFIG_HOME=/home/test/.config",
	}
	if got := ApprovedEnvironment(source); !reflect.DeepEqual(got, want) {
		t.Fatalf("ApprovedEnvironment = %#v, want %#v", got, want)
	}
}
