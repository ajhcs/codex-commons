package codexauth

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

const maxWorkspaceInventory = 10000
const workspacePageSize = 100
const taskVisibilityPageLimit = 10
const taskVisibilityTimeout = 5 * time.Second
const taskVisibilityPollInterval = 100 * time.Millisecond
const historianRecoveryPageLimit = 10

type workspaceThread struct {
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	UpdatedAt int64  `json:"updatedAt"`
	GitInfo   *struct {
		OriginURL *string `json:"originUrl"`
		Branch    *string `json:"branch"`
	} `json:"gitInfo"`
}

func (c *ClientImpl) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	out := make([]Workspace, 0, 100)
	seenThreads := make(map[string]bool, 100)
	for _, archived := range []bool{false, true} {
		cursor := ""
		for page := 0; page < maxWorkspaceInventory/workspacePageSize && len(out) < maxWorkspaceInventory; page++ {
			params := map[string]any{"limit": workspacePageSize, "archived": archived, "useStateDbOnly": true, "sortKey": "updated_at", "sortDirection": "desc", "sourceKinds": []string{
				"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown",
			}}
			if cursor != "" {
				params["cursor"] = cursor
			}
			raw, err := c.call(ctx, "thread/list", params, false)
			if err != nil {
				return nil, err
			}
			var value struct {
				Data       []workspaceThread `json:"data"`
				NextCursor *string           `json:"nextCursor"`
			}
			if err := decodeOne(raw, &value); err != nil || len(value.Data) > workspacePageSize {
				return nil, ErrProtocol
			}
			for _, thread := range value.Data {
				if len(thread.ID) > 120 || strings.ContainsAny(thread.ID, "\r\n\x00") {
					return nil, ErrProtocol
				}
				if thread.ID != "" && seenThreads[thread.ID] {
					continue
				}
				cwd := filepath.Clean(thread.CWD)
				if !filepath.IsAbs(cwd) || strings.ContainsAny(cwd, "\r\n\x00") || thread.UpdatedAt < 0 {
					return nil, ErrProtocol
				}
				item := Workspace{CWD: cwd, UpdatedAt: time.Unix(thread.UpdatedAt, 0).UTC()}
				if thread.GitInfo != nil {
					if thread.GitInfo.OriginURL != nil {
						item.GitOrigin = strings.TrimSpace(*thread.GitInfo.OriginURL)
					}
					if thread.GitInfo.Branch != nil {
						item.GitBranch = strings.TrimSpace(*thread.GitInfo.Branch)
					}
				}
				out = append(out, item)
				if thread.ID != "" {
					seenThreads[thread.ID] = true
				}
				if len(out) == maxWorkspaceInventory {
					break
				}
			}
			if value.NextCursor == nil || *value.NextCursor == "" {
				break
			}
			cursor = *value.NextCursor
			if len(cursor) > 4096 {
				return nil, ErrProtocol
			}
		}
	}
	return out, nil
}

// FindHistorianTask performs a bounded, read-only identity recovery. The
// deterministic title contains the durable Commons job ID, while cwd and
// source constraints prevent a same-title task in another project from being
// bound. Thread turns are requested with itemsView=notLoaded so prompts,
// transcripts, token usage, and item bodies are neither decoded nor returned.
func (c *ClientImpl) FindHistorianTask(ctx context.Context, cwd, title string) (TaskLaunch, bool, error) {
	if !c.ExperimentalDynamicTools() || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd ||
		title == "" || strings.TrimSpace(title) != title || len(title) > 200 || strings.ContainsAny(title, "\r\n\x00") {
		return TaskLaunch{}, false, ErrUnavailable
	}
	type match struct {
		ID, SessionID, Status string
	}
	matches := map[string]match{}
	complete := true
	for _, archived := range []bool{false, true} {
		cursor := ""
		for page := 0; page < historianRecoveryPageLimit; page++ {
			params := map[string]any{
				"limit": workspacePageSize, "archived": archived, "useStateDbOnly": true,
				"sortKey": "updated_at", "sortDirection": "desc", "cwd": cwd,
				"sourceKinds": []string{"appServer", "vscode"},
			}
			if cursor != "" {
				params["cursor"] = cursor
			}
			raw, err := c.call(ctx, "thread/list", params, false)
			if err != nil {
				return TaskLaunch{}, false, err
			}
			var value struct {
				Data []struct {
					ID, SessionID, CWD, Source, Name string
					Ephemeral                        bool
					Status                           struct {
						Type string `json:"type"`
					} `json:"status"`
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			if decodeOne(raw, &value) != nil || len(value.Data) > workspacePageSize {
				return TaskLaunch{}, false, ErrProtocol
			}
			for _, item := range value.Data {
				if item.Name != title || filepath.Clean(item.CWD) != cwd || item.Ephemeral ||
					(item.Source != "appServer" && item.Source != "vscode") {
					continue
				}
				if item.ID == "" || len(item.ID) > 120 || strings.ContainsAny(item.ID, "\r\n\x00") ||
					item.SessionID == "" || len(item.SessionID) > 120 || strings.ContainsAny(item.SessionID, "\r\n\x00") {
					return TaskLaunch{}, false, ErrProtocol
				}
				matches[item.ID] = match{ID: item.ID, SessionID: item.SessionID, Status: item.Status.Type}
				if len(matches) > 1 {
					return TaskLaunch{}, false, nil
				}
			}
			if value.NextCursor == nil || *value.NextCursor == "" {
				break
			}
			cursor = *value.NextCursor
			if len(cursor) > 4096 {
				return TaskLaunch{}, false, ErrProtocol
			}
			if page == historianRecoveryPageLimit-1 {
				complete = false
			}
		}
	}
	if !complete || len(matches) != 1 {
		return TaskLaunch{}, false, nil
	}
	var candidate match
	for _, item := range matches {
		candidate = item
	}

	// Re-read the unique candidate to close the title/cwd/source race before
	// binding it. includeTurns=false keeps turn items out of this response.
	raw, err := c.call(ctx, "thread/read", map[string]any{"threadId": candidate.ID, "includeTurns": false}, false)
	if err != nil {
		return TaskLaunch{}, false, err
	}
	var read struct {
		Thread struct {
			ID, SessionID, CWD, Source, Name string
			Ephemeral                        bool
		} `json:"thread"`
	}
	if decodeOne(raw, &read) != nil {
		return TaskLaunch{}, false, ErrProtocol
	}
	if read.Thread.ID != candidate.ID || read.Thread.SessionID != candidate.SessionID || read.Thread.Name != title ||
		filepath.Clean(read.Thread.CWD) != cwd || read.Thread.Ephemeral ||
		(read.Thread.Source != "appServer" && read.Thread.Source != "vscode") {
		return TaskLaunch{}, false, nil
	}

	turnRaw, err := c.call(ctx, "thread/turns/list", map[string]any{
		"threadId": candidate.ID, "limit": 2, "sortDirection": "asc", "itemsView": "notLoaded",
	}, false)
	if err != nil {
		return TaskLaunch{}, false, err
	}
	var turns struct {
		Data []struct {
			ID        string            `json:"id"`
			Status    string            `json:"status"`
			Items     []json.RawMessage `json:"items"`
			ItemsView string            `json:"itemsView"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if decodeOne(turnRaw, &turns) != nil || len(turns.Data) > 2 {
		return TaskLaunch{}, false, ErrProtocol
	}
	if len(turns.Data) != 1 || turns.NextCursor != nil && *turns.NextCursor != "" ||
		turns.Data[0].ID == "" || len(turns.Data[0].ID) > 120 || strings.ContainsAny(turns.Data[0].ID, "\r\n\x00") ||
		len(turns.Data[0].Items) != 0 || turns.Data[0].ItemsView != "notLoaded" {
		return TaskLaunch{}, false, nil
	}
	return TaskLaunch{ThreadID: candidate.ID, SessionID: candidate.SessionID, TurnID: turns.Data[0].ID, ThreadStatus: candidate.Status, TurnStatus: turns.Data[0].Status}, true, nil
}

func (c *ClientImpl) ListHistorianTasks(ctx context.Context, cwd string) ([]TaskIdentity, error) {
	if !c.ExperimentalDynamicTools() || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return nil, ErrUnavailable
	}
	sources := []string{"cli", "vscode", "exec", "appServer", "subAgent", "subAgentReview", "subAgentCompact", "subAgentThreadSpawn", "subAgentOther", "unknown"}
	seen := map[string]bool{}
	out := make([]TaskIdentity, 0, 4)
	for _, archived := range []bool{false, true} {
		cursor := ""
		for page := 0; page < historianRecoveryPageLimit; page++ {
			params := map[string]any{"limit": workspacePageSize, "archived": archived, "useStateDbOnly": true,
				"sortKey": "updated_at", "sortDirection": "desc", "cwd": cwd, "sourceKinds": sources}
			if cursor != "" {
				params["cursor"] = cursor
			}
			raw, err := c.call(ctx, "thread/list", params, false)
			if err != nil {
				return nil, err
			}
			var value struct {
				Data []struct {
					ID, SessionID, CWD, Source, Name string
					Ephemeral                        bool
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			if decodeOne(raw, &value) != nil || len(value.Data) > workspacePageSize {
				return nil, ErrProtocol
			}
			for _, item := range value.Data {
				if filepath.Clean(item.CWD) != cwd {
					continue
				}
				if item.ID == "" || item.SessionID == "" || len(item.ID) > 120 || len(item.SessionID) > 120 ||
					strings.ContainsAny(item.ID+item.SessionID+item.Source+item.Name, "\r\n\x00") {
					return nil, ErrProtocol
				}
				if !seen[item.ID] {
					seen[item.ID] = true
					out = append(out, TaskIdentity{ThreadID: item.ID, SessionID: item.SessionID, Source: item.Source, Name: item.Name, Ephemeral: item.Ephemeral})
				}
			}
			if value.NextCursor == nil || *value.NextCursor == "" {
				break
			}
			cursor = *value.NextCursor
			if len(cursor) > 4096 || page == historianRecoveryPageLimit-1 {
				return nil, ErrProtocol
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ThreadID < out[j].ThreadID })
	return out, nil
}

func (c *ClientImpl) VerifiedHistorianSettings(threadID string) (TaskLaunch, bool) {
	if c == nil || threadID == "" {
		return TaskLaunch{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.verifiedSettings[threadID]
	return value, ok
}

func (c *ClientImpl) SupportsModel(ctx context.Context, model, effort string) (bool, error) {
	if model == "" || effort == "" {
		return false, ErrProtocol
	}
	cursor := ""
	for page := 0; page < 5; page++ {
		params := map[string]any{"limit": 100, "includeHidden": true}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "model/list", params, false)
		if err != nil {
			return false, err
		}
		var value struct {
			Data []struct {
				ID      string `json:"id"`
				Model   string `json:"model"`
				Efforts []struct {
					Value string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := decodeOne(raw, &value); err != nil || len(value.Data) > 100 {
			return false, ErrProtocol
		}
		for _, candidate := range value.Data {
			if candidate.ID != model && candidate.Model != model {
				continue
			}
			for _, option := range candidate.Efforts {
				if option.Value == effort {
					return true, nil
				}
			}
			return false, nil
		}
		if value.NextCursor == nil || *value.NextCursor == "" {
			return false, nil
		}
		cursor = *value.NextCursor
		if len(cursor) > 4096 {
			return false, ErrProtocol
		}
	}
	return false, nil
}

func (c *ClientImpl) ExperimentalDynamicTools() bool { return c != nil && c.experimental }

func (c *ClientImpl) RenameHistorianTask(ctx context.Context, threadID, title string) error {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(title) == "" || len(title) > 200 {
		return ErrProtocol
	}
	if _, err := c.call(ctx, "thread/name/set", map[string]any{"threadId": threadID, "name": title}, false); err != nil {
		return err
	}
	raw, err := c.call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": false}, false)
	if err != nil {
		return err
	}
	var value struct {
		Thread struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Ephemeral bool   `json:"ephemeral"`
		} `json:"thread"`
	}
	if decodeOne(raw, &value) != nil || value.Thread.ID != threadID || value.Thread.Name != title || value.Thread.Ephemeral {
		return ErrProtocol
	}
	return nil
}

func (c *ClientImpl) LaunchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID string) (TaskLaunch, error) {
	return c.launchTask(ctx, cwd, model, effort, prompt, clientUserMessageID, "", HistorianPolicy{}, nil, nil, false)
}

func (c *ClientImpl) LaunchHistorianTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, policy HistorianPolicy, dynamic DynamicToolHandler, terminal TurnTerminalHandler) (TaskLaunch, error) {
	if !c.ExperimentalDynamicTools() || !validHistorianPolicy(policy) {
		return TaskLaunch{}, ErrUnavailable
	}
	return c.launchTask(ctx, cwd, model, effort, prompt, clientUserMessageID, title, policy, dynamic, terminal, true)
}

func (c *ClientImpl) launchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, policy HistorianPolicy, dynamic DynamicToolHandler, terminal TurnTerminalHandler, experimental bool) (TaskLaunch, error) {
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd || model == "" || effort == "" ||
		strings.TrimSpace(prompt) == "" || len(prompt) > 24<<10 || strings.TrimSpace(clientUserMessageID) == "" ||
		len(clientUserMessageID) > 200 || strings.ContainsAny(clientUserMessageID, "\r\n\x00") {
		return TaskLaunch{}, ErrProtocol
	}
	if experimental && (strings.TrimSpace(title) != title || title == "" || len(title) > 200 || strings.ContainsAny(title, "\r\n\x00")) {
		return TaskLaunch{}, ErrProtocol
	}
	sandbox := "workspace-write"
	threadParams := map[string]any{
		"cwd": cwd, "model": model, "ephemeral": false, "sandbox": sandbox,
		"threadSource": "appServer",
	}
	if experimental {
		threadParams["sandbox"] = "read-only"
		threadParams["approvalPolicy"] = "never"
		threadParams["dynamicTools"] = historianDynamicTools(policy)
	}
	threadRaw, err := c.call(ctx, "thread/start", threadParams, false)
	if err != nil {
		return TaskLaunch{}, err
	}
	var threadValue struct {
		Thread struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
			Status    struct {
				Type string `json:"type"`
			} `json:"status"`
			CWD       string `json:"cwd"`
			Ephemeral bool   `json:"ephemeral"`
		} `json:"thread"`
		Model          string `json:"model"`
		ApprovalPolicy any    `json:"approvalPolicy"`
		Sandbox        struct {
			Type          string `json:"type"`
			NetworkAccess bool   `json:"networkAccess"`
		} `json:"sandbox"`
	}
	if err := decodeOne(threadRaw, &threadValue); err != nil || threadValue.Thread.ID == "" ||
		threadValue.Thread.SessionID == "" || filepath.Clean(threadValue.Thread.CWD) != cwd {
		return TaskLaunch{}, ErrProtocol
	}
	if experimental {
		approval, ok := threadValue.ApprovalPolicy.(string)
		if !ok || threadValue.Model != model || approval != "never" || threadValue.Sandbox.Type != "readOnly" ||
			threadValue.Sandbox.NetworkAccess || threadValue.Thread.Ephemeral {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, Stage: "thread_effective"}, ErrProtocol
		}
	}
	if experimental {
		if _, err = c.call(ctx, "thread/name/set", map[string]any{"threadId": threadValue.Thread.ID, "name": title}, false); err != nil {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, Stage: "name_set"}, err
		}
		readRaw, readErr := c.call(ctx, "thread/read", map[string]any{"threadId": threadValue.Thread.ID, "includeTurns": false}, false)
		if readErr != nil {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, Stage: "thread_read"}, readErr
		}
		var readValue struct {
			Thread struct {
				ID        string `json:"id"`
				Name      string `json:"name"`
				CWD       string `json:"cwd"`
				Ephemeral bool   `json:"ephemeral"`
			} `json:"thread"`
		}
		if decodeOne(readRaw, &readValue) != nil || readValue.Thread.ID != threadValue.Thread.ID || readValue.Thread.Name != title || readValue.Thread.Ephemeral || filepath.Clean(readValue.Thread.CWD) != cwd {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, Stage: "thread_read"}, ErrProtocol
		}
	}
	if experimental {
		c.mu.Lock()
		c.dynamicHandlers[threadValue.Thread.ID] = dynamic
		c.terminalHandlers[threadValue.Thread.ID] = terminal
		settingsCh := make(chan threadSettings, 1)
		c.settingsWaiters[threadValue.Thread.ID] = settingsCh
		// Only a settings notification emitted after this waiter is installed
		// can attest the turn/start overrides. Discard any earlier thread-level
		// settings notification rather than accepting stale defaults.
		delete(c.pendingSettings, threadValue.Thread.ID)
		c.mu.Unlock()
	}
	prompt = strings.ReplaceAll(prompt, "{{CODEX_THREAD_ID}}", threadValue.Thread.ID)
	prompt = strings.ReplaceAll(prompt, "{{CODEX_SESSION_ID}}", threadValue.Thread.SessionID)
	turnRaw, err := c.call(ctx, "turn/start", map[string]any{
		"threadId": threadValue.Thread.ID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
		"cwd":      cwd, "model": model, "effort": effort,
		"clientUserMessageId": clientUserMessageID,
	}, false)
	if err != nil {
		if experimental {
			c.clearDynamicThread(threadValue.Thread.ID)
		}
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type, Stage: "turn_start"}, err
	}
	var turnValue struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := decodeOne(turnRaw, &turnValue); err != nil || turnValue.Turn.ID == "" {
		if experimental {
			c.clearDynamicThread(threadValue.Thread.ID)
		}
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type, Stage: "turn_start"}, ErrProtocol
	}
	var effective threadSettings
	if experimental {
		c.mu.Lock()
		settingsCh := c.settingsWaiters[threadValue.Thread.ID]
		c.mu.Unlock()
		settingsCtx, settingsCancel := context.WithTimeout(ctx, 3*time.Second)
		defer settingsCancel()
		select {
		case effective = <-settingsCh:
		case <-settingsCtx.Done():
			c.clearDynamicThread(threadValue.Thread.ID)
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID, Stage: "settings_wait"}, ErrProtocol
		}
		c.mu.Lock()
		delete(c.settingsWaiters, threadValue.Thread.ID)
		delete(c.pendingSettings, threadValue.Thread.ID)
		c.mu.Unlock()
		if effective.ThreadID != threadValue.Thread.ID || effective.Model != model || effective.Effort != effort ||
			effective.CWD != cwd || effective.Approval != "never" || effective.Sandbox != "readOnly" || effective.Network || effective.MultiAgent != "explicitRequestOnly" {
			c.clearDynamicThread(threadValue.Thread.ID)
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID,
				Model: effective.Model, Effort: effective.Effort, Sandbox: effective.Sandbox, Approval: effective.Approval, Network: effective.Network, MultiAgent: effective.MultiAgent, Stage: "settings_exact"}, ErrProtocol
		}
		// Settings attestation is authoritative independently of eventual state-DB
		// visibility. Retain it through a later visibility timeout and scheduler
		// interrupt so acceptance evidence does not misreport a policy failure.
		c.mu.Lock()
		c.verifiedSettings[threadValue.Thread.ID] = TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID,
			TurnID: turnValue.Turn.ID, ThreadStatus: threadValue.Thread.Status.Type, TurnStatus: turnValue.Turn.Status,
			Model: effective.Model, Effort: effective.Effort, Sandbox: effective.Sandbox,
			Approval: effective.Approval, Network: effective.Network, MultiAgent: effective.MultiAgent, Stage: "settings_exact"}
		c.mu.Unlock()
	}
	if err = c.verifyTaskVisible(ctx, cwd, title, threadValue.Thread.ID, experimental); err != nil {
		if experimental {
			c.clearDynamicThread(threadValue.Thread.ID)
		}
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID,
			Model: effective.Model, Effort: effective.Effort, Sandbox: effective.Sandbox, Approval: effective.Approval,
			Network: effective.Network, MultiAgent: effective.MultiAgent, Stage: "visibility"}, err
	}
	if experimental {
		c.mu.Lock()
		c.dynamicTurns[threadValue.Thread.ID] = turnValue.Turn.ID
		pendingTools := c.pendingTools[threadValue.Thread.ID]
		delete(c.pendingTools, threadValue.Thread.ID)
		pendingTerminal, hasPendingTerminal := c.pendingTerminals[threadValue.Thread.ID]
		if hasPendingTerminal {
			delete(c.pendingTerminals, threadValue.Thread.ID)
		}
		c.mu.Unlock()
		if len(pendingTools) > 0 || hasPendingTerminal && pendingTerminal.TurnID == turnValue.Turn.ID {
			// Return the accepted launch before invoking callbacks. The application
			// must durably bind the exact thread/turn first. A single goroutine then
			// preserves App Server order: buffered tools FIFO, terminal last.
			if c.beginAsync() {
				go func(tools []wireMessage, terminal TurnTerminal, hasTerminal bool) {
					defer c.asyncWG.Done()
					for _, pendingTool := range tools {
						c.handleServerRequestSync(pendingTool)
					}
					if hasTerminal && terminal.TurnID == turnValue.Turn.ID {
						c.dispatchTurnTerminal(terminal)
					}
				}(append([]wireMessage(nil), pendingTools...), pendingTerminal, hasPendingTerminal)
			} else {
				for _, pendingTool := range pendingTools {
					c.respondDynamicTool(pendingTool.ID, DynamicToolResponse{})
				}
			}
		}
		c.mu.Lock()
		verified := c.verifiedSettings[threadValue.Thread.ID]
		verified.Stage = "accepted"
		c.verifiedSettings[threadValue.Thread.ID] = verified
		c.mu.Unlock()
	}
	return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID, ThreadStatus: threadValue.Thread.Status.Type, TurnStatus: turnValue.Turn.Status,
		Model: effective.Model, Effort: effective.Effort, Sandbox: effective.Sandbox, Approval: effective.Approval, Network: effective.Network, MultiAgent: effective.MultiAgent, Stage: "accepted"}, nil
}

func (c *ClientImpl) verifyTaskVisible(ctx context.Context, cwd, title, threadID string, experimental bool) error {
	visibilityCtx, cancel := context.WithTimeout(ctx, taskVisibilityTimeout)
	defer cancel()
	for {
		visible, err := c.taskVisibleOnce(visibilityCtx, cwd, title, threadID, experimental)
		if err != nil {
			return err
		}
		if visible {
			return nil
		}
		timer := time.NewTimer(taskVisibilityPollInterval)
		select {
		case <-visibilityCtx.Done():
			timer.Stop()
			return ErrProtocol
		case <-timer.C:
		}
	}
}

func (c *ClientImpl) taskVisibleOnce(ctx context.Context, cwd, title, threadID string, experimental bool) (bool, error) {
	sourceKinds := []string{"appServer"}
	if experimental {
		sourceKinds = []string{"appServer", "vscode"}
	}
	visible, cursor := false, ""
	for page := 0; page < taskVisibilityPageLimit && !visible; page++ {
		params := map[string]any{"limit": workspacePageSize, "useStateDbOnly": true, "sortKey": "updated_at", "sortDirection": "desc", "cwd": cwd, "sourceKinds": sourceKinds}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call(ctx, "thread/list", params, false)
		if err != nil {
			return false, err
		}
		var value struct {
			Data []struct {
				ID, CWD, Source, Name string
				Ephemeral             bool
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if decodeOne(raw, &value) != nil || len(value.Data) > workspacePageSize {
			return false, ErrProtocol
		}
		for _, item := range value.Data {
			if item.ID == threadID && filepath.Clean(item.CWD) == cwd && (!experimental || item.Name == title && !item.Ephemeral && (item.Source == "appServer" || item.Source == "vscode")) {
				visible = true
				break
			}
		}
		if value.NextCursor == nil || *value.NextCursor == "" {
			break
		}
		cursor = *value.NextCursor
		if len(cursor) > 4096 {
			return false, ErrProtocol
		}
	}
	return visible, nil
}

func validHistorianPolicy(policy HistorianPolicy) bool {
	limits, ok := (domain.ArchaeologyExecutionPolicy{Depth: policy.Depth, Sources: domain.ArchaeologySources{Git: policy.Git, Docs: policy.Docs, CodexHistory: policy.CodexHistory}}).Limits()
	if !ok {
		return false
	}
	return policy.MaxOutcomes == limits.MaxOutcomes &&
		policy.MaxProvenance == limits.MaxProvenancePerOutcome &&
		policy.MaxContributors == limits.MaxContributorsPerOutcome &&
		policy.MaxHistoricalAliases == limits.MaxHistoricalAliases &&
		policy.MaxHistoricalTasks == limits.MaxHistoricalTasks &&
		policy.MaxSourcesExamined == limits.MaxSourcesExamined
}

func historianDynamicTools(policy HistorianPolicy) []map[string]any {
	object := func(properties map[string]any, required []string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	}
	text := func(max int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
	}
	progress := object(map[string]any{
		"phase":            map[string]any{"type": "string", "enum": []string{"inspecting_sources", "building_proposals", "ready_to_report"}},
		"sources_examined": map[string]any{"type": "integer", "minimum": 0, "maximum": policy.MaxSourcesExamined},
		"note":             map[string]any{"type": "string", "maxLength": 500},
	}, []string{"phase", "sources_examined", "note"})
	sourceVariant := func(kind, pattern, description string) map[string]any {
		return object(map[string]any{
			"kind":        map[string]any{"type": "string", "const": kind},
			"stable_id":   map[string]any{"type": "string", "minLength": 1, "maxLength": 300, "pattern": pattern, "description": description},
			"digest":      map[string]any{"type": "string", "pattern": `^sha256:[a-f0-9]{64}$`},
			"occurred_at": map[string]any{"type": "string", "format": "date-time", "maxLength": 40},
		}, []string{"kind", "stable_id", "digest", "occurred_at"})
	}
	variants := make([]map[string]any, 0, 3)
	if policy.Git {
		variants = append(variants, sourceVariant("git", `^((commit|tree|blob|tag):[0-9a-f]{40}([0-9a-f]{24})?|ref:refs/[A-Za-z0-9._/+\-]+)$`, "An immutable Git object as commit|tree|blob|tag:<40-or-64-lowercase-hex>, or a full ref as ref:refs/<name>. Refs cannot contain traversal, repeated slash, hidden/dot segments, @{, trailing dot/slash, or .lock segments."))
	}
	if policy.Docs {
		variants = append(variants, sourceVariant("docs", `^[A-Za-z0-9][A-Za-z0-9._ /-]{0,299}$`, "A normalized repository-relative document path. Absolute paths, traversal, dot-segments, hidden/private credential names, and prompt text are forbidden."))
	}
	if policy.CodexHistory {
		variants = append(variants, sourceVariant("codex_history", `^(task|thread):[A-Za-z0-9][A-Za-z0-9_-]{7,119}$`, "A bounded Codex identifier as task:<id> or thread:<id>; never a title, prompt, transcript, or filesystem path."))
	}
	source := map[string]any{"oneOf": variants}
	key := map[string]any{"type": "string", "pattern": `^[a-z0-9][a-z0-9._-]{0,63}$`, "maxLength": 64}
	alias := object(map[string]any{"alias": key, "session": text(50), "source": source}, []string{"alias", "session", "source"})
	attribution := object(map[string]any{
		"session": text(50), "role": map[string]any{"type": "string", "enum": []string{"originator", "implementer", "reviewer", "evaluator"}}, "confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}}, "source": source,
	}, []string{"session", "role", "confidence", "source"})
	event := object(map[string]any{
		"key": key, "kind": map[string]any{"type": "string", "enum": []string{"completed", "reviewed", "failed", "retried", "remediated", "evaluated"}}, "summary": text(200), "session": map[string]any{"type": "string", "maxLength": 50},
		"confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}}, "source": source,
	}, []string{"key", "kind", "summary", "confidence", "source"})
	maxAttributionsPerTask := 2
	maxEventsPerTask := 1
	task := object(map[string]any{
		"key": key, "title": text(75), "description": map[string]any{"type": "string", "maxLength": 200}, "acceptance": map[string]any{"type": "string", "maxLength": 200},
		"state": map[string]any{"type": "string", "const": "done"}, "source": source,
		"attributions": map[string]any{"type": "array", "minItems": 1, "maxItems": maxAttributionsPerTask, "items": attribution}, "events": map[string]any{"type": "array", "maxItems": maxEventsPerTask, "items": event},
	}, []string{"key", "title", "state", "source", "attributions", "events"})
	historicalImport := object(map[string]any{
		"schema_version": map[string]any{"type": "integer", "const": 1}, "source_digest": map[string]any{"type": "string", "pattern": `^sha256:[a-f0-9]{64}$`},
		"collision_policy": map[string]any{"type": "string", "const": "current_wins"}, "project_thread_aliases": map[string]any{"type": "array", "maxItems": policy.MaxHistoricalAliases, "items": alias},
		"tasks": map[string]any{"type": "array", "minItems": 1, "maxItems": policy.MaxHistoricalTasks, "items": task},
	}, []string{"schema_version", "source_digest", "collision_policy", "project_thread_aliases", "tasks"})
	historicalImport["description"] = "Canonical import proposal below 32 KiB. Task, event, and alias keys must be unique in their arrays. Alias sessions must be unique and must not appear in task attributions. Any event.session must exactly match an attribution session on the same task and must never name an alias session. Every nested source must exactly match one source_kind/source_label/digest/recorded_at record in this outcome's outer provenance array."
	historicalImport["properties"].(map[string]any)["project_thread_aliases"].(map[string]any)["description"] = "Optional unique aliases. An alias session cannot be reused by any attribution or event. Prefer an empty array unless an explicit allowed source proves the alias."
	provenanceVariants := make([]map[string]any, 0, len(variants))
	for _, raw := range variants {
		properties := raw["properties"].(map[string]any)
		provenanceVariants = append(provenanceVariants, object(map[string]any{
			"source_kind": properties["kind"], "source_label": properties["stable_id"],
			"digest": properties["digest"], "recorded_at": properties["occurred_at"],
		}, []string{"source_kind", "source_label", "digest", "recorded_at"}))
	}
	provenance := map[string]any{"oneOf": provenanceVariants}
	contributor := object(map[string]any{
		"session_id": text(50), "contribution": text(250), "demonstrated_strength": map[string]any{"type": "string", "maxLength": 75},
		"uncertainty": map[string]any{"type": "string", "maxLength": 125}, "confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}},
	}, []string{"session_id", "contribution", "confidence"})
	// project_id is deliberately absent: Commons binds it from the durable job.
	outcome := object(map[string]any{
		"title": text(75), "summary": text(300), "source_count": map[string]any{"type": "integer", "minimum": 1, "maximum": policy.MaxSourcesExamined},
		"provenance": map[string]any{"type": "array", "minItems": 1, "maxItems": policy.MaxProvenance, "items": provenance}, "contributors": map[string]any{"type": "array", "maxItems": policy.MaxContributors, "items": contributor},
		"historical_import": historicalImport,
	}, []string{"title", "summary", "source_count", "provenance", "contributors", "historical_import"})
	report := object(map[string]any{"outcomes": map[string]any{"type": "array", "minItems": 1, "maxItems": policy.MaxOutcomes, "items": outcome}}, []string{"outcomes"})
	return []map[string]any{
		{"type": "function", "name": "commons_project_history_progress", "description": "Record bounded, non-secret project-history progress for the current Commons-launched historian.", "inputSchema": progress},
		{"type": "function", "name": "commons_project_history_report", "description": "Submit complete source-grounded history proposals for explicit human review; this never applies history. The entire arguments object must serialize below 60 KiB and each historical_import below 32 KiB. Outcome titles, task keys, event keys, aliases, outer provenance records, and contributor session IDs must be unique in their applicable arrays. Every nested source must exactly match an outer provenance record. Alias sessions cannot be attributions; event sessions must match same-task attributions and cannot be aliases.", "inputSchema": report},
	}
}
func launchAccepted(value TaskLaunch, err error) bool {
	return value.ThreadID != "" && (err == nil || !errors.Is(err, context.Canceled))
}

func (c *ClientImpl) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if !c.ExperimentalDynamicTools() || threadID == "" || len(threadID) > 120 || turnID == "" || len(turnID) > 120 {
		return ErrUnavailable
	}
	_, err := c.call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, false)
	return err
}
