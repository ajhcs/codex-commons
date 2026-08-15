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

	"codex-commons/internal/domain"
)

const (
	MaxLineBytes           = 1 << 20
	MaxReadLineBytes       = 16 << 20
	maxPendingRequests     = 16
	maxLoginCompletions    = 32
	maxDynamicCallsPerTurn = 64
)

var processExitWait = 2 * time.Second

var (
	ErrUnavailable         = errors.New("codex app server unavailable")
	ErrProtocol            = errors.New("invalid codex app server protocol message")
	ErrLineTooLarge        = errors.New("codex app server response line too large")
	ErrPendingLimit        = errors.New("codex app server request limit reached")
	ErrProcessExited       = errors.New("codex app server process exited")
	ErrProcessExitTimeout  = errors.New("codex app server process did not exit before shutdown timeout")
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

type DynamicToolCall struct {
	CallID, ThreadID, TurnID, Tool string
	Arguments                      json.RawMessage
}
type DynamicToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type DynamicToolResponse struct {
	Success      bool                 `json:"success"`
	ContentItems []DynamicToolContent `json:"contentItems"`
}
type TurnTerminal struct {
	ThreadID, TurnID, Status string
	DurationMS               *int64
}
type DynamicToolHandler func(context.Context, DynamicToolCall) DynamicToolResponse
type TurnTerminalHandler func(TurnTerminal)

type HistorianPolicy struct {
	Depth                                                        string
	Git, Docs, CodexHistory                                      bool
	MaxOutcomes, MaxProvenance, MaxContributors                  int
	MaxHistoricalAliases, MaxHistoricalTasks, MaxSourcesExamined int
}

type ExperimentalArchaeologyClient interface {
	ArchaeologyClient
	ExperimentalDynamicTools() bool
	LaunchHistorianTask(context.Context, string, string, string, string, string, string, HistorianPolicy, DynamicToolHandler, TurnTerminalHandler) (TaskLaunch, error)
	InterruptTurn(context.Context, string, string) error
}

// HistorianTaskFinder is an optional, read-only recovery capability. It is
// deliberately separate from ExperimentalArchaeologyClient so older App Server
// adapters remain source-compatible while native schedulers can recover an
// accepted task whose launch response was lost.
type HistorianTaskFinder interface {
	FindHistorianTask(context.Context, string, string) (TaskLaunch, bool, error)
}

type HistorianTaskRenamer interface {
	RenameHistorianTask(context.Context, string, string) error
}

type TaskIdentity struct {
	ThreadID, SessionID, Source, Name string
	Ephemeral                         bool
}

// HistorianTaskInventory is a bounded metadata-only workspace inventory used
// by isolated acceptance to prove that one requested task did not create
// additional parent or subagent threads.
type HistorianTaskInventory interface {
	ListHistorianTasks(context.Context, string) ([]TaskIdentity, error)
	VerifiedHistorianSettings(string) (TaskLaunch, bool)
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

// Workspace is the metadata-only subset of a Codex thread inventory that
// Commons may use to build a project catalog. Codex 0.147 may send protocol
// preview bytes, but prompt previews and thread names are never represented,
// retained, persisted, projected, or logged by Commons.
type Workspace struct {
	CWD       string
	UpdatedAt time.Time
	GitOrigin string
	GitBranch string
}

type TaskLaunch struct {
	ThreadID     string
	SessionID    string
	TurnID       string
	ThreadStatus string
	TurnStatus   string
	Model        string
	Effort       string
	Sandbox      string
	Approval     string
	Network      bool
	MultiAgent   string
	Stage        string
}

type threadSettings struct {
	ThreadID, Model, Effort, CWD, Approval, Sandbox, MultiAgent string
	Network                                                     bool
}

// ArchaeologyClient is an optional capability implemented by the managed
// App Server client. Keeping it separate preserves the narrow authentication
// test doubles while allowing Commons to truthfully advertise task launch.
type ArchaeologyClient interface {
	ListWorkspaces(context.Context) ([]Workspace, error)
	SupportsModel(context.Context, string, string) (bool, error)
	LaunchTask(context.Context, string, string, string, string, string) (TaskLaunch, error)
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

	mu               sync.Mutex
	pending          map[string]pendingRequest
	completions      map[string]loginCompletion
	cancelled        map[string]bool
	ready            bool
	closed           bool
	failure          error
	handler          func(Event)
	dynamicHandlers  map[string]DynamicToolHandler
	terminalHandlers map[string]TurnTerminalHandler
	pendingTerminals map[string]TurnTerminal
	pendingTools     map[string][]wireMessage
	dynamicTurns     map[string]string
	dynamicCalls     map[string]map[string]bool
	settingsWaiters  map[string]chan threadSettings
	pendingSettings  map[string]threadSettings
	verifiedSettings map[string]TaskLaunch
	nextID           atomic.Uint64
	writeMu          sync.Mutex
	asyncMu          sync.Mutex
	asyncWG          sync.WaitGroup
	asyncClosing     bool
	closeOnce        sync.Once
	experimental     bool
	callbackCtx      context.Context
	callbackCancel   context.CancelFunc
	done             chan struct{}
	readerDone       chan struct{}
	waitDone         chan struct{}
	// pid is deliberately metadata only. It is never used as a process
	// authority and is exposed to the managed supervisor only for safe
	// diagnostics.
	pid int
}

// NewWithTransport performs the required initialize/initialized handshake on
// an already connected JSONL transport. Tests can use this with in-memory
// pipes; production uses NewProcess below.
func NewWithTransport(ctx context.Context, transport io.ReadWriteCloser) (*ClientImpl, error) {
	return NewWithTransportConfig(ctx, transport, false)
}

func NewWithTransportConfig(ctx context.Context, transport io.ReadWriteCloser, experimental bool) (*ClientImpl, error) {
	if transport == nil {
		return nil, ErrUnavailable
	}
	callbackCtx, callbackCancel := context.WithCancel(context.Background())
	c := &ClientImpl{
		transport:        transport,
		pending:          make(map[string]pendingRequest),
		completions:      make(map[string]loginCompletion),
		cancelled:        make(map[string]bool),
		dynamicHandlers:  make(map[string]DynamicToolHandler),
		terminalHandlers: make(map[string]TurnTerminalHandler),
		pendingTerminals: make(map[string]TurnTerminal),
		pendingTools:     make(map[string][]wireMessage),
		dynamicTurns:     make(map[string]string),
		dynamicCalls:     make(map[string]map[string]bool),
		settingsWaiters:  make(map[string]chan threadSettings),
		pendingSettings:  make(map[string]threadSettings),
		verifiedSettings: make(map[string]TaskLaunch),
		experimental:     experimental,
		callbackCtx:      callbackCtx,
		callbackCancel:   callbackCancel,
		done:             make(chan struct{}),
		readerDone:       make(chan struct{}),
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

// Done is the child-lifecycle signal used by the managed supervisor. It is
// closed when the transport becomes unusable, whether that was observed by
// the reader, a failed write, or the process wait path. Request traffic is not
// required for an exit to become visible to the supervisor.
//
// The channel is intentionally read-only and carries no payload. Consumers
// that need a safe diagnostic may use ExitReason; no protocol payload is
// exposed here.
func (c *ClientImpl) Done() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.done
}

// ExitSignal is a descriptive alias used by internal lifecycle adapters.
func (c *ClientImpl) ExitSignal() <-chan struct{} { return c.Done() }

// LifecycleDone is kept as a private-package-friendly spelling for adapters
// that avoid depending on the public Client surface.
func (c *ClientImpl) LifecycleDone() <-chan struct{} { return c.Done() }

// PID returns the operating-system child PID when this client was created by
// NewProcess. In-memory transports have no child and return zero.
func (c *ClientImpl) PID() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pid
}

// ProcessID is a descriptive alias for PID.
func (c *ClientImpl) ProcessID() int { return c.PID() }

// ExitReason returns the internal failure category for lifecycle reporting.
// Callers should treat the value as an error sentinel only; it is not a
// process stderr or protocol payload.
func (c *ClientImpl) ExitReason() error {
	if c == nil {
		return ErrUnavailable
	}
	c.mu.Lock()
	err := c.failure
	c.mu.Unlock()
	if err == nil {
		return ErrUnavailable
	}
	return err
}

// Failure is a descriptive alias for ExitReason.
func (c *ClientImpl) Failure() error { return c.ExitReason() }

func (c *ClientImpl) beginAsync() bool {
	c.asyncMu.Lock()
	defer c.asyncMu.Unlock()
	if c.asyncClosing {
		return false
	}
	c.asyncWG.Add(1)
	return true
}

type ProcessConfig struct {
	Executable                     string
	Env                            []string
	EnableExperimentalDynamicTools bool
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
	c, err := NewWithTransportConfig(ctx, transport, config.EnableExperimentalDynamicTools)
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	c.mu.Lock()
	c.pid = cmd.Process.Pid
	c.mu.Unlock()
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
	params := map[string]any{
		"clientInfo": map[string]string{
			"name": "codex_commons", "title": "Codex Commons", "version": "0.1.0",
		},
	}
	if c.experimental {
		params["capabilities"] = map[string]bool{"experimentalApi": true}
	}
	result, err := c.call(ctx, "initialize", params, true)
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
		if len(line) > MaxReadLineBytes {
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
	if len(message.ID) != 0 && !bytes.Equal(bytes.TrimSpace(message.ID), []byte("null")) && message.Method != "" {
		if !c.ExperimentalDynamicTools() {
			_ = c.writeMessage(map[string]any{"id": json.RawMessage(message.ID), "error": map[string]any{"code": -32601, "message": "Method not found"}})
			return
		}
		c.handleServerRequest(message)
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
		return
	}
	if message.Method == "turn/completed" {
		c.handleTurnCompleted(message.Params)
		return
	}
	if message.Method == "thread/settings/updated" {
		c.handleThreadSettings(message.Params)
	}
}

func (c *ClientImpl) handleThreadSettings(raw json.RawMessage) {
	var value struct {
		ThreadID string `json:"threadId"`
		Settings struct {
			Model      string  `json:"model"`
			Effort     *string `json:"effort"`
			CWD        string  `json:"cwd"`
			Approval   any     `json:"approvalPolicy"`
			MultiAgent any     `json:"multiAgentMode"`
			Sandbox    struct {
				Type          string `json:"type"`
				NetworkAccess bool   `json:"networkAccess"`
			} `json:"sandboxPolicy"`
		} `json:"threadSettings"`
	}
	if decodeOne(raw, &value) != nil || value.ThreadID == "" || value.Settings.Model == "" ||
		value.Settings.Effort == nil || *value.Settings.Effort == "" || !filepath.IsAbs(value.Settings.CWD) ||
		value.Settings.Sandbox.Type == "" {
		return
	}
	approval, ok := value.Settings.Approval.(string)
	if !ok || approval == "" {
		return
	}
	multiAgent, ok := value.Settings.MultiAgent.(string)
	if !ok || multiAgent == "" {
		return
	}
	settings := threadSettings{ThreadID: value.ThreadID, Model: value.Settings.Model, Effort: *value.Settings.Effort,
		CWD: filepath.Clean(value.Settings.CWD), Approval: approval, Sandbox: value.Settings.Sandbox.Type,
		Network: value.Settings.Sandbox.NetworkAccess, MultiAgent: multiAgent}
	c.mu.Lock()
	if waiter := c.settingsWaiters[value.ThreadID]; waiter != nil {
		select {
		case waiter <- settings:
		default:
		}
	} else if len(c.pendingSettings) < 32 {
		c.pendingSettings[value.ThreadID] = settings
	}
	c.mu.Unlock()
}

func (c *ClientImpl) handleServerRequest(message wireMessage) {
	c.handleServerRequestMode(message, false)
}

func (c *ClientImpl) handleServerRequestSync(message wireMessage) {
	c.handleServerRequestMode(message, true)
}

func (c *ClientImpl) handleServerRequestMode(message wireMessage, synchronous bool) {
	key, ok := wireIDKey(message.ID)
	if !ok {
		c.fail(ErrProtocol)
		return
	}
	if message.Method != "item/tool/call" {
		_ = c.writeMessage(map[string]any{"id": json.RawMessage(message.ID), "error": map[string]any{"code": -32601, "message": "Method not found"}})
		return
	}
	if len(message.Params) == 0 || len(message.Params) > 64<<10 {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
		return
	}
	var call struct {
		CallID    string          `json:"callId"`
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Namespace *string         `json:"namespace"`
	}
	if decodeOne(message.Params, &call) != nil {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
		return
	}
	argumentLimit := 4 << 10
	if call.Tool == "commons_project_history_report" {
		argumentLimit = domain.ArchaeologyNativeReportMaxBytes
	}
	if call.CallID == "" || len(call.CallID) > 200 || call.ThreadID == "" || len(call.ThreadID) > 120 || call.TurnID == "" || len(call.TurnID) > 120 || (call.Tool != "commons_project_history_progress" && call.Tool != "commons_project_history_report") || call.Namespace != nil || len(call.Arguments) == 0 || len(call.Arguments) > argumentLimit {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
		return
	}
	c.mu.Lock()
	handler := c.dynamicHandlers[call.ThreadID]
	exactTurn := c.dynamicTurns[call.ThreadID]
	if handler != nil && exactTurn == "" {
		pending := c.pendingTools[call.ThreadID]
		if len(pending) < 2 {
			c.pendingTools[call.ThreadID] = append(pending, message)
			c.mu.Unlock()
			time.AfterFunc(2*time.Second, func() { c.expirePendingTool(call.ThreadID, message.ID) })
			return
		}
		c.mu.Unlock()
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
		return
	}
	used := c.dynamicCalls[call.ThreadID]
	duplicate := used != nil && used[call.CallID]
	atLimit := used != nil && len(used) >= maxDynamicCallsPerTurn
	if handler != nil && exactTurn == call.TurnID && !duplicate && !atLimit {
		if used == nil {
			used = make(map[string]bool)
			c.dynamicCalls[call.ThreadID] = used
		}
		used[call.CallID] = true
	}
	c.mu.Unlock()
	if handler == nil || exactTurn != call.TurnID || duplicate || atLimit {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
		return
	}
	run := func() {
		ctx, cancel := context.WithTimeout(c.callbackCtx, 30*time.Second)
		defer cancel()
		response := handler(ctx, DynamicToolCall{CallID: call.CallID, ThreadID: call.ThreadID, TurnID: call.TurnID, Tool: call.Tool, Arguments: append(json.RawMessage(nil), call.Arguments...)})
		c.respondDynamicTool(message.ID, response)
	}
	if synchronous {
		run()
	} else if c.beginAsync() {
		go func() {
			defer c.asyncWG.Done()
			run()
		}()
	} else {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
	}
	_ = key
}

func (c *ClientImpl) respondDynamicTool(id json.RawMessage, response DynamicToolResponse) {
	valid := response.Success && len(response.ContentItems) <= 4
	for _, item := range response.ContentItems {
		if item.Type != "inputText" || len(item.Text) > 4096 || strings.ContainsRune(item.Text, 0) {
			valid = false
		}
	}
	if !valid {
		response = DynamicToolResponse{Success: false, ContentItems: []DynamicToolContent{{Type: "inputText", Text: "Commons rejected this bounded project-history update."}}}
	}
	_ = c.writeMessage(map[string]any{"id": json.RawMessage(id), "result": response})
}

func (c *ClientImpl) handleTurnCompleted(raw json.RawMessage) {
	var value struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			DurationMS *int64 `json:"durationMs"`
		} `json:"turn"`
	}
	if decodeOne(raw, &value) != nil || value.ThreadID == "" || value.Turn.ID == "" || (value.Turn.Status != "completed" && value.Turn.Status != "interrupted" && value.Turn.Status != "failed") || (value.Turn.DurationMS != nil && (*value.Turn.DurationMS < 0 || *value.Turn.DurationMS > 7*24*60*60*1000)) {
		return
	}
	terminal := TurnTerminal{ThreadID: value.ThreadID, TurnID: value.Turn.ID, Status: value.Turn.Status, DurationMS: value.Turn.DurationMS}
	c.mu.Lock()
	handler := c.terminalHandlers[value.ThreadID]
	exactTurn := c.dynamicTurns[value.ThreadID]
	if handler != nil && exactTurn == "" && len(c.pendingTerminals) < 32 {
		c.pendingTerminals[value.ThreadID] = terminal
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if handler != nil && exactTurn == value.Turn.ID {
		c.dispatchTurnTerminal(terminal)
	}
}

func (c *ClientImpl) clearDynamicThread(threadID string) {
	c.mu.Lock()
	pending := append([]wireMessage(nil), c.pendingTools[threadID]...)
	delete(c.dynamicHandlers, threadID)
	delete(c.terminalHandlers, threadID)
	delete(c.dynamicTurns, threadID)
	delete(c.dynamicCalls, threadID)
	delete(c.pendingTerminals, threadID)
	delete(c.pendingTools, threadID)
	delete(c.settingsWaiters, threadID)
	delete(c.pendingSettings, threadID)
	c.mu.Unlock()
	for _, message := range pending {
		c.respondDynamicTool(message.ID, DynamicToolResponse{})
	}
}

func (c *ClientImpl) dispatchTurnTerminal(terminal TurnTerminal) {
	c.mu.Lock()
	handler := c.terminalHandlers[terminal.ThreadID]
	exact := c.dynamicTurns[terminal.ThreadID]
	if handler != nil && exact == terminal.TurnID {
		delete(c.dynamicHandlers, terminal.ThreadID)
		delete(c.terminalHandlers, terminal.ThreadID)
		delete(c.dynamicTurns, terminal.ThreadID)
		delete(c.dynamicCalls, terminal.ThreadID)
		delete(c.pendingTerminals, terminal.ThreadID)
	}
	c.mu.Unlock()
	if handler != nil && exact == terminal.TurnID {
		handler(terminal)
	}
}

func (c *ClientImpl) fail(err error) {
	if err == nil {
		err = ErrUnavailable
	}
	c.closeOnce.Do(func() {
		c.callbackCancel()
		c.mu.Lock()
		c.closed = true
		c.failure = err
		pending := c.pending
		terminals := make([]struct {
			handler  TurnTerminalHandler
			terminal TurnTerminal
		}, 0, len(c.terminalHandlers))
		for threadID, handler := range c.terminalHandlers {
			if turnID := c.dynamicTurns[threadID]; handler != nil && turnID != "" {
				terminals = append(terminals, struct {
					handler  TurnTerminalHandler
					terminal TurnTerminal
				}{handler, TurnTerminal{ThreadID: threadID, TurnID: turnID, Status: "unavailable"}})
			}
		}
		c.pending = make(map[string]pendingRequest)
		c.dynamicHandlers = make(map[string]DynamicToolHandler)
		c.terminalHandlers = make(map[string]TurnTerminalHandler)
		c.dynamicTurns = make(map[string]string)
		c.dynamicCalls = make(map[string]map[string]bool)
		c.pendingTerminals = make(map[string]TurnTerminal)
		c.pendingTools = make(map[string][]wireMessage)
		c.settingsWaiters = make(map[string]chan threadSettings)
		c.pendingSettings = make(map[string]threadSettings)
		c.verifiedSettings = make(map[string]TaskLaunch)
		c.mu.Unlock()
		for _, request := range pending {
			request.result <- response{err: err}
		}
		for _, item := range terminals {
			item.handler(item.terminal)
		}
		close(c.done)
		_ = c.transport.Close()
	})
}

func (c *ClientImpl) Close() error {
	if c == nil {
		return nil
	}
	c.asyncMu.Lock()
	c.asyncClosing = true
	c.asyncMu.Unlock()
	c.fail(ErrUnavailable)
	c.asyncWG.Wait()
	select {
	case <-c.readerDone:
	case <-time.After(2 * time.Second):
	}
	if c.waitDone != nil {
		select {
		case <-c.waitDone:
		case <-time.After(processExitWait):
			return ErrProcessExitTimeout
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

func (c *ClientImpl) expirePendingTool(threadID string, id json.RawMessage) {
	key, _ := wireIDKey(id)
	c.mu.Lock()
	pending := c.pendingTools[threadID]
	kept := pending[:0]
	expired := false
	for _, message := range pending {
		messageKey, _ := wireIDKey(message.ID)
		if !expired && messageKey == key {
			expired = true
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) == 0 {
		delete(c.pendingTools, threadID)
	} else {
		c.pendingTools[threadID] = kept
	}
	c.mu.Unlock()
	if expired {
		c.respondDynamicTool(id, DynamicToolResponse{})
	}
}
