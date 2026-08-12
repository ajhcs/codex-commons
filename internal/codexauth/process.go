package codexauth

import (
	"context"
	"errors"
	"sync"
)

// ManagedProcessClient gives the optional App Server one bounded recovery
// attempt. A failed process is never restarted indefinitely, and a failed
// restart leaves the HTTP layer with an unavailable capability instead of a
// restart loop.
type ManagedProcessClient struct {
	mu       sync.Mutex
	ctx      context.Context
	config   ProcessConfig
	client   *ClientImpl
	handler  func(Event)
	restarts int
	closed   bool
}

func NewManagedProcess(ctx context.Context, config ProcessConfig) (*ManagedProcessClient, error) {
	client, err := NewProcess(ctx, config)
	if err != nil {
		return nil, err
	}
	return &ManagedProcessClient{ctx: ctx, config: config, client: client}, nil
}

func (m *ManagedProcessClient) current() *ClientImpl {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	return m.client
}

func retryableProcessError(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrProcessExited) ||
		errors.Is(err, ErrProtocol) || errors.Is(err, ErrLineTooLarge)
}

func (m *ManagedProcessClient) restart() (*ClientImpl, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.restarts >= 1 {
		return nil, ErrUnavailable
	}
	m.restarts++
	old := m.client
	if old != nil {
		_ = old.Close()
	}
	client, err := NewProcess(m.ctx, m.config)
	if err != nil {
		m.client = nil
		return nil, err
	}
	client.SetEventHandler(m.handler)
	m.client = client
	return client, nil
}

func (m *ManagedProcessClient) withRetry(ctx context.Context, call func(*ClientImpl) error) error {
	client := m.current()
	if client == nil {
		return ErrUnavailable
	}
	if err := call(client); err == nil {
		return nil
	} else if !retryableProcessError(err) || ctx.Err() != nil {
		return err
	}
	client, err := m.restart()
	if err != nil {
		return err
	}
	return call(client)
}

func (m *ManagedProcessClient) Available() bool {
	client := m.current()
	if client != nil && client.Available() {
		return true
	}
	client, err := m.restart()
	return err == nil && client != nil && client.Available()
}

func (m *ManagedProcessClient) StartDeviceCode(ctx context.Context) (DeviceCode, error) {
	var result DeviceCode
	err := m.withRetry(ctx, func(client *ClientImpl) error {
		var callErr error
		result, callErr = client.StartDeviceCode(ctx)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) PollLogin(ctx context.Context, loginID string) (LoginResult, error) {
	var result LoginResult
	err := m.withRetry(ctx, func(client *ClientImpl) error {
		var callErr error
		result, callErr = client.PollLogin(ctx, loginID)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) CancelLogin(ctx context.Context, loginID string) error {
	return m.withRetry(ctx, func(client *ClientImpl) error { return client.CancelLogin(ctx, loginID) })
}

func (m *ManagedProcessClient) AccountState(ctx context.Context) (AccountState, error) {
	state := AccountUnknown
	err := m.withRetry(ctx, func(client *ClientImpl) error {
		var callErr error
		state, callErr = client.AccountState(ctx)
		return callErr
	})
	return state, err
}

func (m *ManagedProcessClient) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var result []Workspace
	err := m.withRetry(ctx, func(client *ClientImpl) error {
		var callErr error
		result, callErr = client.ListWorkspaces(ctx)
		return callErr
	})
	return result, err
}

func (m *ManagedProcessClient) SupportsModel(ctx context.Context, model, effort string) (bool, error) {
	var result bool
	err := m.withRetry(ctx, func(client *ClientImpl) error {
		var callErr error
		result, callErr = client.SupportsModel(ctx, model, effort)
		return callErr
	})
	return result, err
}

// LaunchTask deliberately does not use the managed retry path. Any transport
// failure after thread/start may mean Codex accepted the task, so callers must
// persist an uncertain state instead of silently launching a duplicate.
func (m *ManagedProcessClient) LaunchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID string) (TaskLaunch, error) {
	client := m.current()
	if client == nil {
		return TaskLaunch{}, ErrUnavailable
	}
	return client.LaunchTask(ctx, cwd, model, effort, prompt, clientUserMessageID)
}

func (m *ManagedProcessClient) ExperimentalDynamicTools() bool {
	client := m.current()
	return client != nil && client.ExperimentalDynamicTools()
}

func (m *ManagedProcessClient) LaunchHistorianTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, dynamic DynamicToolHandler, terminal TurnTerminalHandler) (TaskLaunch, error) {
	client := m.current()
	if client == nil || !client.ExperimentalDynamicTools() {
		return TaskLaunch{}, ErrUnavailable
	}
	return client.LaunchHistorianTask(ctx, cwd, model, effort, prompt, clientUserMessageID, title, dynamic, terminal)
}

func (m *ManagedProcessClient) SetEventHandler(handler func(Event)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.handler = handler
	client := m.client
	m.mu.Unlock()
	if client != nil {
		client.SetEventHandler(handler)
	}
}

func (m *ManagedProcessClient) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	client := m.client
	m.client = nil
	m.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

var _ Client = (*ManagedProcessClient)(nil)
var _ ArchaeologyClient = (*ManagedProcessClient)(nil)
var _ ExperimentalArchaeologyClient = (*ManagedProcessClient)(nil)

func (m *ManagedProcessClient) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	client := m.current()
	if client == nil {
		return ErrUnavailable
	}
	return client.InterruptTurn(ctx, threadID, turnID)
}
