// Package appserverbridge contains a passive, stdio JSONL observer for the
// documented Codex App Server protocol. It never starts, resumes, or writes to
// a Codex thread and it grants no Commons authority.
package appserverbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

const MaxLineBytes = 1 << 20

var ErrInvalidMessage = errors.New("invalid app-server message")

type Direction string

const (
	ToServer   Direction = "to_server"
	FromServer Direction = "from_server"
)

type Thread struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name,omitempty"`
	Preview   string `json:"preview,omitempty"`
	Status    Status `json:"status"`
}

// Purpose is deliberately derived only from the user-visible thread name. The
// preview is retained for diagnostics but is never published as a purpose.
func (t Thread) Purpose() string { return t.Name }

type Status struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags,omitempty"`
}

type Event struct {
	Kind      string
	Threads   []Thread
	ThreadIDs []string
	ThreadID  string
	Status    Status
}

type envelope struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

type Observer struct {
	mu      sync.Mutex
	pending map[string]string
}

func NewObserver() *Observer { return &Observer{pending: make(map[string]string)} }

func (o *Observer) Process(direction Direction, line []byte) ([]Event, error) {
	if o == nil || (direction != ToServer && direction != FromServer) || len(line) == 0 || len(line) > MaxLineBytes || bytes.ContainsAny(line, "\r\n") {
		return nil, ErrInvalidMessage
	}
	var message envelope
	decoder := json.NewDecoder(bytes.NewReader(line))
	if err := decoder.Decode(&message); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	if decoder.Decode(&struct{}{}) == nil {
		return nil, ErrInvalidMessage
	}
	if direction == ToServer {
		if len(message.ID) == 0 || message.Method == "" {
			return nil, nil
		}
		if message.Method != "thread/list" && message.Method != "thread/loaded/list" {
			return nil, nil
		}
		key, err := requestKey(message.ID)
		if err != nil {
			return nil, err
		}
		o.mu.Lock()
		o.pending[key] = message.Method
		o.mu.Unlock()
		return nil, nil
	}
	if message.Method == "thread/status/changed" {
		var value struct {
			ThreadID string `json:"threadId"`
			Status   Status `json:"status"`
		}
		if err := json.Unmarshal(message.Params, &value); err != nil || value.ThreadID == "" || !validStatus(value.Status) {
			return nil, ErrInvalidMessage
		}
		return []Event{{Kind: "thread_status", ThreadID: value.ThreadID, Status: value.Status}}, nil
	}
	if len(message.ID) == 0 {
		return nil, nil
	}
	key, err := requestKey(message.ID)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	method := o.pending[key]
	delete(o.pending, key)
	o.mu.Unlock()
	if method == "" || len(message.Error) != 0 && string(message.Error) != "null" {
		return nil, nil
	}
	switch method {
	case "thread/list":
		var value struct {
			Data []Thread `json:"data"`
		}
		if err := json.Unmarshal(message.Result, &value); err != nil {
			return nil, ErrInvalidMessage
		}
		for _, thread := range value.Data {
			if thread.ID == "" || thread.SessionID == "" || !validStatus(thread.Status) {
				return nil, ErrInvalidMessage
			}
		}
		return []Event{{Kind: "thread_inventory", Threads: value.Data}}, nil
	case "thread/loaded/list":
		var value struct {
			Data []string `json:"data"`
		}
		if err := json.Unmarshal(message.Result, &value); err != nil {
			return nil, ErrInvalidMessage
		}
		for _, id := range value.Data {
			if id == "" {
				return nil, ErrInvalidMessage
			}
		}
		return []Event{{Kind: "loaded_inventory", ThreadIDs: value.Data}}, nil
	}
	return nil, nil
}

func requestKey(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", ErrInvalidMessage
	}
	switch value.(type) {
	case string, json.Number:
	default:
		return "", ErrInvalidMessage
	}
	return string(raw), nil
}

func validStatus(status Status) bool {
	switch status.Type {
	case "notLoaded", "idle", "systemError":
		return len(status.ActiveFlags) == 0
	case "active":
		return true
	default:
		return false
	}
}
