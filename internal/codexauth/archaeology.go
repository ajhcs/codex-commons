package codexauth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const maxWorkspaceInventory = 10000
const workspacePageSize = 10
const taskVisibilityPageLimit = 10

type workspaceThread struct {
	CWD       string `json:"cwd"`
	UpdatedAt int64  `json:"updatedAt"`
	GitInfo   *struct {
		OriginURL *string `json:"originUrl"`
		Branch    *string `json:"branch"`
	} `json:"gitInfo"`
}

func (c *ClientImpl) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	cursor := ""
	out := make([]Workspace, 0, 100)
	for page := 0; page < maxWorkspaceInventory/workspacePageSize && len(out) < maxWorkspaceInventory; page++ {
		params := map[string]any{"limit": workspacePageSize, "useStateDbOnly": true, "sortKey": "updated_at", "sortDirection": "desc", "sourceKinds": []string{
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
			if len(out) == maxWorkspaceInventory {
				break
			}
		}
		if value.NextCursor == nil || *value.NextCursor == "" {
			return out, nil
		}
		cursor = *value.NextCursor
		if len(cursor) > 4096 {
			return nil, ErrProtocol
		}
	}
	return out, nil
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

func (c *ClientImpl) LaunchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID string) (TaskLaunch, error) {
	return c.launchTask(ctx, cwd, model, effort, prompt, clientUserMessageID, "", nil, nil, false)
}

func (c *ClientImpl) LaunchHistorianTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, dynamic DynamicToolHandler, terminal TurnTerminalHandler) (TaskLaunch, error) {
	if !c.ExperimentalDynamicTools() {
		return TaskLaunch{}, ErrUnavailable
	}
	return c.launchTask(ctx, cwd, model, effort, prompt, clientUserMessageID, title, dynamic, terminal, true)
}

func (c *ClientImpl) launchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID, title string, dynamic DynamicToolHandler, terminal TurnTerminalHandler, experimental bool) (TaskLaunch, error) {
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd || model == "" || effort == "" ||
		strings.TrimSpace(prompt) == "" || len(prompt) > 24<<10 || strings.TrimSpace(clientUserMessageID) == "" ||
		len(clientUserMessageID) > 200 || strings.ContainsAny(clientUserMessageID, "\r\n\x00") {
		return TaskLaunch{}, ErrProtocol
	}
	if experimental && (strings.TrimSpace(title) != title || title == "" || len(title) > 200 || strings.ContainsAny(title, "\r\n\x00")) {
		return TaskLaunch{}, ErrProtocol
	}
	threadParams := map[string]any{
		"cwd": cwd, "model": model, "ephemeral": false, "sandbox": "workspace-write",
		"threadSource": "appServer",
	}
	if experimental {
		threadParams["dynamicTools"] = historianDynamicTools()
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
			CWD string `json:"cwd"`
		} `json:"thread"`
	}
	if err := decodeOne(threadRaw, &threadValue); err != nil || threadValue.Thread.ID == "" ||
		threadValue.Thread.SessionID == "" || filepath.Clean(threadValue.Thread.CWD) != cwd {
		return TaskLaunch{}, ErrProtocol
	}
	if experimental {
		if _, err = c.call(ctx, "thread/name/set", map[string]any{"threadId": threadValue.Thread.ID, "name": title}, false); err != nil {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, err
		}
		readRaw, readErr := c.call(ctx, "thread/read", map[string]any{"threadId": threadValue.Thread.ID, "includeTurns": false}, false)
		if readErr != nil {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, readErr
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
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, ErrProtocol
		}
	}
	sourceKinds := []string{"appServer"}
	if experimental {
		sourceKinds = []string{"appServer", "vscode"}
	}
	visible := false
	cursor := ""
	for page := 0; page < taskVisibilityPageLimit && !visible; page++ {
		params := map[string]any{"limit": workspacePageSize, "useStateDbOnly": true, "sortKey": "updated_at", "sortDirection": "desc", "cwd": cwd, "sourceKinds": sourceKinds}
		if cursor != "" {
			params["cursor"] = cursor
		}
		visibleRaw, visibleErr := c.call(ctx, "thread/list", params, false)
		if visibleErr != nil {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, visibleErr
		}
		var visibleValue struct {
			Data []struct {
				ID        string `json:"id"`
				CWD       string `json:"cwd"`
				Source    string `json:"source"`
				Name      string `json:"name"`
				Ephemeral bool   `json:"ephemeral"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if decodeOne(visibleRaw, &visibleValue) != nil || len(visibleValue.Data) > workspacePageSize {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, ErrProtocol
		}
		for _, item := range visibleValue.Data {
			if item.ID == threadValue.Thread.ID && filepath.Clean(item.CWD) == cwd && (!experimental || item.Name == title && !item.Ephemeral && (item.Source == "appServer" || item.Source == "vscode")) {
				visible = true
				break
			}
		}
		if visibleValue.NextCursor == nil || *visibleValue.NextCursor == "" {
			break
		}
		cursor = *visibleValue.NextCursor
		if len(cursor) > 4096 {
			return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, ErrProtocol
		}
	}
	if !visible {
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID}, ErrProtocol
	}
	if experimental {
		c.mu.Lock()
		c.dynamicHandlers[threadValue.Thread.ID] = dynamic
		c.terminalHandlers[threadValue.Thread.ID] = terminal
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
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type}, err
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
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type}, ErrProtocol
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
		for _, pendingTool := range pendingTools {
			c.handleServerRequestSync(pendingTool)
		}
		if hasPendingTerminal && pendingTerminal.TurnID == turnValue.Turn.ID {
			c.dispatchTurnTerminal(pendingTerminal)
		}
	}
	return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID, ThreadStatus: threadValue.Thread.Status.Type, TurnStatus: turnValue.Turn.Status}, nil
}

func historianDynamicTools() []map[string]any {
	object := func(properties map[string]any, required []string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	}
	text := func(max int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": max}
	}
	progress := object(map[string]any{
		"phase":            map[string]any{"type": "string", "enum": []string{"inspecting_sources", "building_proposals", "ready_to_report"}},
		"sources_examined": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
		"note":             map[string]any{"type": "string", "maxLength": 500},
	}, []string{"phase", "sources_examined", "note"})
	source := object(map[string]any{
		"kind":      map[string]any{"type": "string", "enum": []string{"git", "docs", "codex_history"}},
		"stable_id": text(300), "digest": map[string]any{"type": "string", "pattern": `^sha256:[a-f0-9]{64}$`},
		"occurred_at": map[string]any{"type": "string", "format": "date-time", "maxLength": 40},
	}, []string{"kind", "stable_id", "digest", "occurred_at"})
	alias := object(map[string]any{"alias": text(300), "session": text(200), "source": source}, []string{"alias", "session", "source"})
	attribution := object(map[string]any{
		"session": text(200), "role": text(120), "confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}}, "source": source,
	}, []string{"session", "role", "confidence", "source"})
	event := object(map[string]any{
		"key": text(200), "kind": text(120), "summary": text(1000), "session": map[string]any{"type": "string", "maxLength": 200},
		"confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}}, "source": source,
	}, []string{"key", "kind", "summary", "confidence", "source"})
	task := object(map[string]any{
		"key": text(200), "title": text(300), "description": map[string]any{"type": "string", "maxLength": 4000}, "acceptance": map[string]any{"type": "string", "maxLength": 4000},
		"state": map[string]any{"type": "string", "const": "done"}, "source": source,
		"attributions": map[string]any{"type": "array", "maxItems": 100, "items": attribution}, "events": map[string]any{"type": "array", "maxItems": 100, "items": event},
	}, []string{"key", "title", "state", "source", "attributions", "events"})
	historicalImport := object(map[string]any{
		"schema_version": map[string]any{"type": "integer", "const": 1}, "batch_id": text(200), "source_digest": map[string]any{"type": "string", "pattern": `^sha256:[a-f0-9]{64}$`},
		"collision_policy": map[string]any{"type": "string", "const": "current_wins"}, "project_thread_aliases": map[string]any{"type": "array", "maxItems": 100, "items": alias},
		"tasks": map[string]any{"type": "array", "minItems": 1, "maxItems": 500, "items": task},
	}, []string{"schema_version", "batch_id", "source_digest", "collision_policy", "project_thread_aliases", "tasks"})
	provenance := object(map[string]any{
		"source_kind": map[string]any{"type": "string", "enum": []string{"git", "docs", "codex_history"}}, "source_label": text(300),
		"digest": map[string]any{"type": "string", "pattern": `^sha256:[a-f0-9]{64}$`}, "recorded_at": map[string]any{"type": "string", "format": "date-time", "maxLength": 40},
	}, []string{"source_kind", "source_label", "digest", "recorded_at"})
	contributor := object(map[string]any{
		"session_id": text(200), "contribution": text(1000), "demonstrated_strength": map[string]any{"type": "string", "maxLength": 300},
		"uncertainty": map[string]any{"type": "string", "maxLength": 500}, "confidence": map[string]any{"type": "string", "enum": []string{"verified", "supported", "uncertain"}},
	}, []string{"session_id", "contribution", "confidence"})
	// project_id is deliberately absent: Commons binds it from the durable job.
	outcome := object(map[string]any{
		"title": text(300), "summary": text(4000), "source_count": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
		"provenance": map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": provenance}, "contributors": map[string]any{"type": "array", "maxItems": 100, "items": contributor},
		"historical_import": historicalImport,
	}, []string{"title", "summary", "source_count", "provenance", "contributors", "historical_import"})
	report := object(map[string]any{"outcomes": map[string]any{"type": "array", "minItems": 1, "maxItems": 20, "items": outcome}}, []string{"outcomes"})
	return []map[string]any{
		{"type": "function", "name": "commons_project_history_progress", "description": "Record bounded, non-secret project-history progress for the current Commons-launched historian.", "inputSchema": progress},
		{"type": "function", "name": "commons_project_history_report", "description": "Submit complete source-grounded history proposals for explicit human review; this never applies history.", "inputSchema": report},
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
