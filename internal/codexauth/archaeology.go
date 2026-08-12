package codexauth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const maxWorkspaceInventory = 10000

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
	for page := 0; page < 100 && len(out) < maxWorkspaceInventory; page++ {
		params := map[string]any{"limit": 100, "useStateDbOnly": true, "sortKey": "updated_at", "sortDirection": "desc", "sourceKinds": []string{
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
		if err := decodeOne(raw, &value); err != nil || len(value.Data) > 100 {
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

func (c *ClientImpl) LaunchTask(ctx context.Context, cwd, model, effort, prompt, clientUserMessageID string) (TaskLaunch, error) {
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd || model == "" || effort == "" ||
		strings.TrimSpace(prompt) == "" || len(prompt) > 24<<10 || strings.TrimSpace(clientUserMessageID) == "" ||
		len(clientUserMessageID) > 200 || strings.ContainsAny(clientUserMessageID, "\r\n\x00") {
		return TaskLaunch{}, ErrProtocol
	}
	threadRaw, err := c.call(ctx, "thread/start", map[string]any{
		"cwd": cwd, "model": model, "ephemeral": false, "sandbox": "workspace-write",
		"threadSource": "codex_commons_project_archaeology",
	}, false)
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
	prompt = strings.ReplaceAll(prompt, "{{CODEX_THREAD_ID}}", threadValue.Thread.ID)
	prompt = strings.ReplaceAll(prompt, "{{CODEX_SESSION_ID}}", threadValue.Thread.SessionID)
	turnRaw, err := c.call(ctx, "turn/start", map[string]any{
		"threadId": threadValue.Thread.ID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
		"cwd":      cwd, "model": model, "effort": effort,
		"clientUserMessageId": clientUserMessageID,
	}, false)
	if err != nil {
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type}, err
	}
	var turnValue struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}
	if err := decodeOne(turnRaw, &turnValue); err != nil || turnValue.Turn.ID == "" {
		return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, ThreadStatus: threadValue.Thread.Status.Type}, ErrProtocol
	}
	return TaskLaunch{ThreadID: threadValue.Thread.ID, SessionID: threadValue.Thread.SessionID, TurnID: turnValue.Turn.ID, ThreadStatus: threadValue.Thread.Status.Type, TurnStatus: turnValue.Turn.Status}, nil
}

func launchAccepted(value TaskLaunch, err error) bool {
	return value.ThreadID != "" && (err == nil || !errors.Is(err, context.Canceled))
}
