// Package codexauth is the active, narrowly scoped Codex App Server client
// used by Commons human authentication. It intentionally has no thread or
// turn capabilities and never accepts externally managed OAuth tokens.
package codexauth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxLineBytes        = 1 << 20
	maxPendingRequests  = 16
	maxLoginCompletions = 32
)

var (
	ErrUnavailable         = errors.New("codex app server unavailable")
	ErrProtocol            = errors.New("invalid codex app server protocol message")
	ErrLineTooLarge        = errors.New("codex app server response line too large")
	ErrPendingLimit        = errors.New("codex app server request limit reached")
	ErrProcessExited       = errors.New("codex app server process exited")
	ErrLoginFailed         = errors.New("codex login failed")
	ErrLoginCancelled      = errors.New("codex login cancelled")
	ErrIdentityUnavailable = errors.New("codex account identity unavailable")
)

type Account struct {
	Type     string
	Email    *string
	PlanType string
}

type DeviceCode struct {
	LoginID         string
	VerificationURL string
	UserCode        string
}

type AccountState string

const (
	AccountSignedIn  AccountState = "signed_in"
	AccountSignedOut AccountState = "signed_out"
	AccountUnknown   AccountState = "unknown"
)

type LoginResult struct {
	State   string // pending, success, failed, cancelled
	Account *Account
}

type Event struct {
	Kind     string
	LoginID  string
	Success  bool
	AuthMode string
}

// Client is the small capability surface consumed by the Commons HTTP layer.
// Close is part of the interface so the server can stop the subprocess with
// its lifecycle.
type Client interface {
	Available() bool
	StartDeviceCode(context.Context) (DeviceCode, error)
	PollLogin(context.Context, string) (LoginResult, error)
	CancelLogin(context.Context, string) error
	AccountState(context.Context) (AccountState, error)
	SetEventHandler(func(Event))
	Close() error
}

// UnavailableClient is the safe server-side fallback when the optional Codex
// CLI is not installed or its App Server cannot be initialized. Keeping the
// capability present lets the HTTP status endpoint explain the unavailable
// state without making Commons startup depend on a separate executable.
type UnavailableClient struct{}

func NewUnavailable() Client { return &UnavailableClient{} }

func (*UnavailableClient) Available() bool { return false }

func (*UnavailableClient) StartDeviceCode(context.Context) (DeviceCode, error) {
	return DeviceCode{}, ErrUnavailable
}

func (*UnavailableClient) PollLogin(context.Context, string) (LoginResult, error) {
	return LoginResult{}, ErrUnavailable
}

func (*UnavailableClient) CancelLogin(context.Context, string) error { return ErrUnavailable }

func (*UnavailableClient) AccountState(context.Context) (AccountState, error) {
	return AccountUnknown, ErrUnavailable
}

func (*UnavailableClient) SetEventHandler(func(Event)) {}
func (*UnavailableClient) Close() error                { return nil }

type wireMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type response struct {
	result json.RawMessage
	err    error
}

type pendingRequest struct {
	result chan response
}

type loginCompletion struct {
	success bool
	error   string
}

type ClientImpl struct {
	transport io.ReadWriteCloser

	mu          sync.Mutex
	pending     map[string]pendingRequest
	completions map[string]loginCompletion
	cancelled   map[string]bool
	ready       bool
	closed      bool
	failure     error
	handler     func(Event)
	nextID      atomic.Uint64
	writeMu     sync.Mutex
	closeOnce   sync.Once
	done        chan struct{}
	readerDone  chan struct{}
	waitDone    chan struct{}
}

// NewWithTransport performs the required initialize/initialized handshake on
// an already connected JSONL transport. Tests can use this with in-memory
// pipes; production uses NewProcess below.
func NewWithTransport(ctx context.Context, transport io.ReadWriteCloser) (*ClientImpl, error) {
	if transport == nil {
		return nil, ErrUnavailable
	}
	c := &ClientImpl{
		transport:   transport,
		pending:     make(map[string]pendingRequest),
		completions: make(map[string]loginCompletion),
		cancelled:   make(map[string]bool),
		done:        make(chan struct{}),
		readerDone:  make(chan struct{}),
	}
	go c.readLoop()
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

type ProcessConfig struct {
	Executable string
	Env        []string
}

// NewProcess starts exactly `codex app-server --listen stdio://` without a
// shell. Env must be an explicitly approved environment assembled by the
// caller; nil is rejected to prevent accidental inheritance of secrets.
func NewProcess(ctx context.Context, config ProcessConfig) (*ClientImpl, error) {
	if !filepath.IsAbs(config.Executable) || strings.ContainsAny(config.Executable, "\r\n\x00") || len(config.Env) == 0 {
		return nil, ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, config.Executable, "app-server", "--listen", "stdio://")
	cmd.Env = append([]string(nil), config.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, err
	}
	// Drain stderr before the handshake so a noisy but valid App Server cannot
	// block on its pipe while Commons waits for initialize.
	go func() { _, _ = io.Copy(io.Discard, stderr); _ = stderr.Close() }()
	transport := &processTransport{stdin: stdin, stdout: stdout}
	c, err := NewWithTransport(ctx, transport)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	c.waitDone = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(c.waitDone)
		c.fail(ErrProcessExited)
	}()
	return c, nil
}

type processTransport struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (p *processTransport) Read(_ []byte) (int, error)      { return 0, io.ErrClosedPipe }
func (p *processTransport) Write(value []byte) (int, error) { return p.stdin.Write(value) }
func (p *processTransport) Close() error {
	stdinErr := p.stdin.Close()
	stdoutErr := p.stdout.Close()
	if stdinErr != nil {
		return stdinErr
	}
	return stdoutErr
}

// processTransport separates stdout reads from stdin writes while retaining a
// single Close handle. ClientImpl uses the concrete stdout reader through the
// readCloser interface below.
func (p *processTransport) stdoutReader() io.Reader { return p.stdout }

type readWriteCloserWithReader interface {
	io.ReadWriteCloser
	stdoutReader() io.Reader
}

func (c *ClientImpl) initialize(ctx context.Context) error {
	result, err := c.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "codex_commons",
			"title":   "Codex Commons",
			"version": "0.1.0",
		},
	}, true)
	if err != nil {
		return err
	}
	if len(result) == 0 || bytes.Equal(bytes.TrimSpace(result), []byte("null")) {
		return ErrProtocol
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	return nil
}

func (c *ClientImpl) Available() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready && !c.closed && c.failure == nil
}

func (c *ClientImpl) StartDeviceCode(ctx context.Context) (DeviceCode, error) {
	result, err := c.call(ctx, "account/login/start", map[string]string{"type": "chatgptDeviceCode"}, false)
	if err != nil {
		return DeviceCode{}, err
	}
	var value struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
	}
	if err := decodeOne(result, &value); err != nil || value.Type != "chatgptDeviceCode" || value.LoginID == "" || value.VerificationURL == "" || value.UserCode == "" {
		return DeviceCode{}, ErrProtocol
	}
	return DeviceCode{LoginID: value.LoginID, VerificationURL: value.VerificationURL, UserCode: value.UserCode}, nil
}

func (c *ClientImpl) PollLogin(ctx context.Context, loginID string) (LoginResult, error) {
	if loginID == "" {
		return LoginResult{}, ErrProtocol
	}
	c.mu.Lock()
	if c.cancelled[loginID] {
		delete(c.cancelled, loginID)
		delete(c.completions, loginID)
		c.mu.Unlock()
		return LoginResult{State: "cancelled"}, nil
	}
	completion, ok := c.completions[loginID]
	c.mu.Unlock()
	if !ok {
		return LoginResult{State: "pending"}, nil
	}
	if !completion.success {
		c.mu.Lock()
		delete(c.completions, loginID)
		c.mu.Unlock()
		if completion.error == "cancelled" {
			return LoginResult{State: "cancelled"}, nil
		}
		return LoginResult{State: "failed"}, nil
	}
	account, err := c.readAccount(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	c.mu.Lock()
	if current, exists := c.completions[loginID]; exists && current == completion {
		delete(c.completions, loginID)
	}
	c.mu.Unlock()
	return LoginResult{State: "success", Account: account}, nil
}

func (c *ClientImpl) CancelLogin(ctx context.Context, loginID string) error {
	if loginID == "" {
		return ErrProtocol
	}
	_, err := c.call(ctx, "account/login/cancel", map[string]string{"loginId": loginID}, false)
	if err == nil {
		c.mu.Lock()
		if !c.cancelled[loginID] && len(c.cancelled) >= maxLoginCompletions {
			victim := ""
			for candidate := range c.cancelled {
				if victim == "" || candidate < victim {
					victim = candidate
				}
			}
			delete(c.cancelled, victim)
		}
		c.cancelled[loginID] = true
		delete(c.completions, loginID)
		c.mu.Unlock()
	}
	return err
}

func (c *ClientImpl) AccountState(ctx context.Context) (AccountState, error) {
	account, err := c.readAccount(ctx)
	if err != nil {
		return AccountUnknown, err
	}
	if account == nil {
		return AccountSignedOut, nil
	}
	if account.Type == "chatgpt" {
		return AccountSignedIn, nil
	}
	return AccountUnknown, nil
}

func (c *ClientImpl) readAccount(ctx context.Context) (*Account, error) {
	result, err := c.call(ctx, "account/read", map[string]bool{"refreshToken": false}, false)
	if err != nil {
		return nil, err
	}
	var value struct {
		Account *struct {
			Type     string  `json:"type"`
			Email    *string `json:"email"`
			PlanType string  `json:"planType"`
		} `json:"account"`
	}
	if err := decodeOne(result, &value); err != nil {
		return nil, ErrProtocol
	}
	if value.Account == nil {
		return nil, nil
	}
	if value.Account.Type == "" {
		return nil, ErrProtocol
	}
	return &Account{Type: value.Account.Type, Email: value.Account.Email, PlanType: value.Account.PlanType}, nil
}

func (c *ClientImpl) SetEventHandler(handler func(Event)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.handler = handler
	c.mu.Unlock()
}

func (c *ClientImpl) notify(method string, params any) error {
	return c.writeMessage(map[string]any{"method": method, "params": params})
}

func (c *ClientImpl) call(ctx context.Context, method string, params any, beforeReady bool) (json.RawMessage, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	c.mu.Lock()
	if c.closed || c.failure != nil || !beforeReady && !c.ready {
		err := c.failure
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, ErrUnavailable
	}
	if len(c.pending) >= maxPendingRequests {
		c.mu.Unlock()
		return nil, ErrPendingLimit
	}
	id := c.nextID.Add(1)
	key := strconv.FormatUint(id, 10)
	pending := pendingRequest{result: make(chan response, 1)}
	c.pending[key] = pending
	c.mu.Unlock()

	if err := c.writeMessage(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		c.removePending(key)
		return nil, err
	}
	select {
	case result := <-pending.result:
		return result.result, result.err
	case <-ctx.Done():
		c.removePending(key)
		return nil, ctx.Err()
	case <-c.done:
		c.removePending(key)
		c.mu.Lock()
		err := c.failure
		c.mu.Unlock()
		if err == nil {
			err = ErrUnavailable
		}
		return nil, err
	}
}

func (c *ClientImpl) removePending(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *ClientImpl) writeMessage(message any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(body) >= MaxLineBytes {
		return ErrLineTooLarge
	}
	body = append(body, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.transport.Write(body); err != nil {
		c.fail(ErrProcessExited)
		return err
	}
	return nil
}

func (c *ClientImpl) readLoop() {
	defer close(c.readerDone)
	reader := bufio.NewReaderSize(c.transport, 4096)
	if process, ok := c.transport.(readWriteCloserWithReader); ok {
		reader = bufio.NewReaderSize(process.stdoutReader(), 4096)
	}
	for {
		line, err := readBoundedLine(reader)
		if err != nil {
			c.fail(err)
			return
		}
		c.handleLine(line)
	}
}

func readBoundedLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		line = append(line, part...)
		if len(line) > MaxLineBytes {
			return nil, ErrLineTooLarge
		}
		if err == nil {
			line = bytes.TrimSuffix(line, []byte{'\n'})
			line = bytes.TrimSuffix(line, []byte{'\r'})
			if len(line) == 0 {
				return nil, ErrProtocol
			}
			return line, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return nil, err
	}
}

func (c *ClientImpl) handleLine(line []byte) {
	var message wireMessage
	if err := decodeOne(line, &message); err != nil {
		c.fail(ErrProtocol)
		return
	}
	if len(message.ID) != 0 && !bytes.Equal(bytes.TrimSpace(message.ID), []byte("null")) {
		key, ok := wireIDKey(message.ID)
		if !ok {
			c.fail(ErrProtocol)
			return
		}
		c.mu.Lock()
		pending, exists := c.pending[key]
		if exists {
			delete(c.pending, key)
		}
		c.mu.Unlock()
		if !exists {
			return
		}
		if len(message.Error) != 0 && !bytes.Equal(bytes.TrimSpace(message.Error), []byte("null")) {
			pending.result <- response{err: ErrProtocol}
			return
		}
		pending.result <- response{result: append(json.RawMessage(nil), message.Result...)}
		return
	}
	if message.Method == "account/login/completed" {
		var params struct {
			LoginID string `json:"loginId"`
			Success bool   `json:"success"`
			Error   string `json:"error"`
		}
		if err := decodeOne(message.Params, &params); err != nil || params.LoginID == "" {
			c.fail(ErrProtocol)
			return
		}
		c.mu.Lock()
		if len(c.completions) < maxLoginCompletions {
			if _, exists := c.completions[params.LoginID]; !exists {
				category := ""
				if !params.Success {
					category = "failed"
					if strings.EqualFold(params.Error, "cancelled") || strings.EqualFold(params.Error, "canceled") {
						category = "cancelled"
					}
				}
				c.completions[params.LoginID] = loginCompletion{success: params.Success, error: category}
			}
		}
		handler := c.handler
		c.mu.Unlock()
		if handler != nil {
			handler(Event{Kind: "login_completed", LoginID: params.LoginID, Success: params.Success})
		}
		return
	}
	if message.Method == "account/updated" {
		var params struct {
			AuthMode *string `json:"authMode"`
		}
		if err := decodeOne(message.Params, &params); err != nil {
			c.fail(ErrProtocol)
			return
		}
		mode := ""
		if params.AuthMode != nil {
			mode = *params.AuthMode
		}
		c.mu.Lock()
		handler := c.handler
		c.mu.Unlock()
		if handler != nil {
			handler(Event{Kind: "account_updated", AuthMode: mode})
		}
	}
}

func (c *ClientImpl) fail(err error) {
	if err == nil {
		err = ErrUnavailable
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.failure = err
		pending := c.pending
		c.pending = make(map[string]pendingRequest)
		c.mu.Unlock()
		for _, request := range pending {
			request.result <- response{err: err}
		}
		close(c.done)
		_ = c.transport.Close()
	})
}

func (c *ClientImpl) Close() error {
	if c == nil {
		return nil
	}
	c.fail(ErrUnavailable)
	select {
	case <-c.readerDone:
	case <-time.After(2 * time.Second):
	}
	if c.waitDone != nil {
		select {
		case <-c.waitDone:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func decodeOne(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrProtocol
	}
	return nil
}

func wireIDKey(raw json.RawMessage) (string, bool) {
	value := bytes.TrimSpace(raw)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return "", false
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return "", false
	}
	switch item := decoded.(type) {
	case json.Number:
		number = item
	case string:
		return item, item != ""
	default:
		return "", false
	}
	return number.String(), true
}

// ApprovedEnvironment returns only the process settings needed to locate and
// run Codex. It deliberately drops all other Commons environment variables,
// including bearer tokens, host credentials, human secrets, and database
// secrets.
func ApprovedEnvironment(source []string) []string {
	allowed := map[string]bool{
		"HOME": true, "CODEX_HOME": true, "PATH": true, "TMPDIR": true,
		"TMP": true, "TEMP": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
		"XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true,
	}
	out := make([]string, 0, len(source))
	for _, item := range source {
		name, _, ok := stringsCut(item, "=")
		if ok && allowed[name] {
			out = append(out, item)
		}
	}
	return out
}

func stringsCut(value, sep string) (before, after string, found bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == sep[0] {
			return value[:i], value[i+1:], true
		}
	}
	return value, "", false
}

var _ Client = (*ClientImpl)(nil)
