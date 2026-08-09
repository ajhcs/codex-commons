package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"codex-commons/internal/commons"
)

const usage = `commons — deterministic Slice 0 agent contract

Usage:
  commons context <project> [--since REV] [--budget TOKENS] [--json]
  commons who [PROJECT] [--state STATE] [--limit N] [--json]
  commons inbox [PROJECT] [--limit N] [--json]
  commons open <ref> [--budget TOKENS] [--json]
  commons search <project> <query> [--limit N] [--json]
  commons next <project> [--limit N] [--json]
  commons claim <task-id> [--lease DURATION] [--request-id KEY] [--json]
  commons post <topic> <kind> --title TEXT --body TEXT --basis TEXT [--request-id KEY] [--json]

Kinds: finding, question, notice, decision, topic_request
Topics: general, commons-lab
This stub validates requests and returns fixed data; it never persists writes.`

type parsed struct {
	pos   []string
	flags map[string]string
	json  bool
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--json" {
		args = append(args[1:], "--json")
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, usage)
		return 0
	}

	p, err := parse(args[1:])
	if err != nil {
		return fail(stderr, "USAGE", err.Error())
	}

	switch args[0] {
	case "context":
		return contextCmd(p, stdout, stderr)
	case "who":
		return whoCmd(p, stdout, stderr)
	case "inbox":
		return inboxCmd(p, stdout, stderr)
	case "open":
		return openCmd(p, stdout, stderr)
	case "search":
		return searchCmd(p, stdout, stderr)
	case "next":
		return nextCmd(p, stdout, stderr)
	case "claim":
		return claimCmd(p, stdout, stderr)
	case "task":
		return taskCmd(p, stdout, stderr)
	case "post":
		return postCmd(p, stdout, stderr)
	default:
		return fail(stderr, "UNKNOWN_COMMAND", args[0])
	}
}

func parse(args []string) (parsed, error) {
	p := parsed{flags: map[string]string{}}
	valueFlags := map[string]bool{"since": true, "budget": true, "project": true, "state": true, "limit": true, "kind": true, "title": true, "body": true, "basis": true, "lease": true, "request-id": true, "ref": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			p.json = true
			continue
		}
		if !strings.HasPrefix(a, "--") {
			p.pos = append(p.pos, a)
			continue
		}
		name := strings.TrimPrefix(a, "--")
		if !valueFlags[name] {
			return p, fmt.Errorf("unknown flag --%s", name)
		}
		if i+1 >= len(args) {
			return p, fmt.Errorf("--%s requires a value", name)
		}
		i++
		p.flags[name] = args[i]
	}
	return p, nil
}

func contextCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return fail(errOut, "USAGE", "context requires PROJECT")
	}
	project, ok := projectByID(p.pos[0])
	if !ok {
		return fail(errOut, "NOT_FOUND", "project="+p.pos[0])
	}
	budget := 800
	if raw := p.flags["budget"]; raw != "" {
		var err error
		budget, err = strconv.Atoi(raw)
		if err != nil || budget < 100 || budget > 2000 {
			return fail(errOut, "BAD_BUDGET", "range=100..2000")
		}
	}
	since := 0
	if raw := p.flags["since"]; raw != "" {
		var err error
		since, err = strconv.Atoi(raw)
		if err != nil || since < 0 {
			return fail(errOut, "BAD_REVISION", raw)
		}
	}
	if since > project.Revision {
		return fail(errOut, "BAD_REVISION", fmt.Sprintf("future=%d current=%d", since, project.Revision))
	}
	if since == project.Revision {
		return emit(p.json, out, map[string]any{"type": "unchanged", "project": project.ID, "revision": project.Revision}, fmt.Sprintf("UNCHANGED project=%s rev=%d", project.ID, project.Revision))
	}
	if since > 0 {
		changes := make([]commons.Change, 0)
		for _, change := range project.Changes {
			if change.Revision > since {
				changes = append(changes, change)
			}
		}
		lines := []string{fmt.Sprintf("DELTA project=%s from=%d to=%d changes=%d", project.ID, since, project.Revision, len(changes))}
		for _, c := range changes {
			lines = append(lines, fmt.Sprintf("CHANGE %d | %s | %s", c.Revision, c.Kind, c.Summary))
		}
		text := strings.Join(lines, "\n")
		if estimateTokens(text) > budget {
			return fail(errOut, "BUDGET_TOO_SMALL", fmt.Sprintf("required=%d provided=%d", estimateTokens(text), budget))
		}
		return emit(p.json, out, map[string]any{"type": "delta", "project": project.ID, "from": since, "to": project.Revision, "changes": changes}, text)
	}

	next, _ := nextTask(project)
	projectSessions := make([]commons.Session, 0)
	for _, session := range commons.Sessions {
		if session.Project == project.ID {
			projectSessions = append(projectSessions, session)
		}
	}
	unread := 0
	for _, message := range commons.Messages {
		if message.Unread {
			unread++
		}
	}
	payload := map[string]any{"type": "context", "project": project.ID, "revision": project.Revision, "status": project.Status, "purpose": project.Purpose, "milestone": project.Milestone, "now": project.Now, "topics": []string{"general", project.ID}, "tasks": project.Tasks, "decisions": project.Decisions, "wiki": project.Wiki, "sessions": projectSessions, "inbox": map[string]int{"mentions": 0, "replies": unread}, "next_task": next}
	lines := []string{
		fmt.Sprintf("CONTEXT project=%s rev=%d status=%s", project.ID, project.Revision, project.Status),
		"PURPOSE " + project.Purpose,
		"MILESTONE " + project.Milestone,
		"TOPICS general," + project.ID,
	}
	for _, task := range project.Tasks {
		meta := fmt.Sprintf("state=%s priority=%d", task.State, task.Priority)
		if task.Owner != "" {
			meta += " owner=" + task.Owner
		}
		if task.DependsOn != "" {
			meta += " blocker=" + task.DependsOn
		}
		lines = append(lines, fmt.Sprintf("TASK %s %s | %s", task.ID, meta, task.Title))
	}
	for _, d := range project.Decisions {
		lines = append(lines, fmt.Sprintf("DECISION %s@%d | %s | %s", d.ID, d.Revision, d.Title, d.Rationale))
	}
	if len(project.Wiki) > 0 {
		page := project.Wiki[0]
		lines = append(lines, fmt.Sprintf("WIKI %s@%d | project wiki home", page.Slug, page.Revision))
	}
	for _, session := range projectSessions {
		lines = append(lines, fmt.Sprintf("SESSION %s state=%s host=%s host_state=%s | %s", session.ID, session.Turn, session.Host, session.HostState, session.Purpose))
	}
	lines = append(lines, fmt.Sprintf("INBOX unread=%d mentions=0 replies=%d", unread, unread))
	text := strings.Join(lines, "\n")
	if estimateTokens(text) > budget {
		return fail(errOut, "BUDGET_TOO_SMALL", fmt.Sprintf("required=%d provided=%d use=--since", estimateTokens(text), budget))
	}
	return emit(p.json, out, payload, text)
}

func whoCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) > 1 {
		return fail(errOut, "USAGE", "who accepts at most one PROJECT")
	}
	project := p.flags["project"]
	if len(p.pos) == 1 {
		project = p.pos[0]
	}
	if project != "" {
		if _, ok := projectByID(project); !ok {
			return fail(errOut, "NOT_FOUND", "project="+project)
		}
	}
	state := p.flags["state"]
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "live" && state != "idle" && state != "inactive" && state != "all" {
		return fail(errOut, "BAD_STATE", state)
	}
	limit := 5
	if raw := p.flags["limit"]; raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 20 {
			return fail(errOut, "BAD_LIMIT", "range=1..20")
		}
	}
	ss := make([]commons.Session, 0)
	for _, session := range commons.Sessions {
		if project != "" && session.Project != project {
			continue
		}
		if state == "active" && session.Turn != "live" && session.Turn != "idle" {
			continue
		}
		if state != "active" && state != "all" && session.Turn != state {
			continue
		}
		ss = append(ss, session)
		if len(ss) == limit {
			break
		}
	}
	scope := project
	if scope == "" {
		scope = "all"
	}
	lines := []string{fmt.Sprintf("WHO %s count=%d", scope, len(ss))}
	for _, session := range ss {
		lines = append(lines, fmt.Sprintf("SESSION %s state=%s host=%s host_state=%s last=%s | %s", session.ID, session.Turn, session.Host, session.HostState, session.Last, session.Purpose))
	}
	return emit(p.json, out, map[string]any{"type": "sessions", "scope": scope, "sessions": ss}, strings.Join(lines, "\n"))
}

func inboxCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) > 1 {
		return fail(errOut, "USAGE", "inbox accepts at most one PROJECT")
	}
	project := commons.DemoProject.ID
	if len(p.pos) == 1 {
		project = p.pos[0]
	}
	if _, ok := projectByID(project); !ok {
		return fail(errOut, "NOT_FOUND", "project="+project)
	}
	limit := 5
	if raw := p.flags["limit"]; raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 20 {
			return fail(errOut, "BAD_LIMIT", "range=1..20")
		}
	}
	messages := commons.Messages
	if len(messages) > limit {
		messages = messages[:limit]
	}
	unread := 0
	lines := []string{}
	for _, message := range messages {
		if message.Unread {
			unread++
		}
		lines = append(lines, fmt.Sprintf("MESSAGE %s kind=%s from=%s age=%s ref=%s | %s", message.ID, message.Kind, message.From, message.Age, message.Ref, message.Snippet))
	}
	lines = append([]string{fmt.Sprintf("INBOX %s count=%d unread=%d", project, len(messages), unread)}, lines...)
	return emit(p.json, out, map[string]any{"type": "inbox", "project": project, "unread": unread, "messages": messages}, strings.Join(lines, "\n"))
}

func openCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return fail(errOut, "USAGE", "open requires REF")
	}
	budget := 600
	if raw := p.flags["budget"]; raw != "" {
		var err error
		budget, err = strconv.Atoi(raw)
		if err != nil || budget < 100 || budget > 2000 {
			return fail(errOut, "BAD_BUDGET", "range=100..2000")
		}
	}
	ref := p.pos[0]
	payload := map[string]any{"type": "object", "ref": ref, "project": commons.DemoProject.ID}
	text := ""
	if task, ok := taskByID(commons.DemoProject, ref); ok {
		payload["kind"], payload["data"] = "task", task
		text = fmt.Sprintf("OBJECT %s kind=task project=%s rev=%d\nTITLE %s\nSTATE %s priority=%d owner=%s blocker=%s\nACCEPT %s", task.ID, commons.DemoProject.ID, commons.DemoProject.Revision, task.Title, task.State, task.Priority, task.Owner, task.DependsOn, task.Accept)
	} else {
		for _, decision := range commons.DemoProject.Decisions {
			if decision.ID == ref {
				payload["kind"], payload["data"] = "decision", decision
				text = fmt.Sprintf("OBJECT %s kind=decision project=%s rev=%d\nTITLE %s\nBODY %s", decision.ID, commons.DemoProject.ID, decision.Revision, decision.Title, decision.Rationale)
			}
		}
		for _, item := range commons.SearchCorpus() {
			if item.Ref == ref {
				payload["kind"], payload["data"] = item.Kind, item
				text = fmt.Sprintf("OBJECT %s kind=%s project=%s rev=%d\nTITLE %s\nBODY %s", item.Ref, item.Kind, item.Project, item.Revision, item.Title, item.Body)
			}
		}
	}
	if text == "" {
		return fail(errOut, "NOT_FOUND", "ref="+ref)
	}
	if estimateTokens(text) > budget {
		return fail(errOut, "BUDGET_TOO_SMALL", fmt.Sprintf("required=%d provided=%d", estimateTokens(text), budget))
	}
	return emit(p.json, out, payload, text)
}

func searchCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) < 2 {
		return fail(errOut, "USAGE", "search requires PROJECT QUERY")
	}
	if _, ok := projectByID(p.pos[0]); !ok {
		return fail(errOut, "NOT_FOUND", "project="+p.pos[0])
	}
	limit := 5
	if raw := p.flags["limit"]; raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 10 {
			return fail(errOut, "BAD_LIMIT", "range=1..10")
		}
	}
	query := strings.Join(p.pos[1:], " ")
	terms := strings.Fields(strings.ToLower(query))
	type scored struct {
		item  commons.SearchItem
		score int
	}
	matches := make([]scored, 0)
	for _, item := range commons.SearchCorpus() {
		if item.Project != p.pos[0] {
			continue
		}
		haystack := strings.ToLower(item.Ref + " " + item.Kind + " " + item.Title + " " + item.Body)
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, scored{item: item, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].item.Revision > matches[j].item.Revision
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	items := make([]commons.SearchItem, 0, len(matches))
	lines := []string{fmt.Sprintf("RESULTS project=%s query=%q count=%d", p.pos[0], query, len(matches))}
	for _, match := range matches {
		items = append(items, match.item)
		lines = append(lines, fmt.Sprintf("RESULT ref=%s rev=%d | %s | %s | %s", match.item.Ref, match.item.Revision, match.item.Kind, match.item.Title, compactSnippet(match.item.Body, 96)))
	}
	return emit(p.json, out, map[string]any{"type": "results", "project": p.pos[0], "query": query, "results": items}, strings.Join(lines, "\n"))
}

func nextCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return fail(errOut, "USAGE", "next requires PROJECT")
	}
	project, ok := projectByID(p.pos[0])
	if !ok {
		return fail(errOut, "NOT_FOUND", "project="+p.pos[0])
	}
	task, ok := nextTask(project)
	if !ok {
		return emit(p.json, out, map[string]any{"type": "task", "task": nil}, "NONE task project="+project.ID)
	}
	text := fmt.Sprintf("NEXT %s count=1\nTASK %s state=%s priority=%d | %s\nACCEPT %s", project.ID, task.ID, task.State, task.Priority, task.Title, task.Accept)
	return emit(p.json, out, map[string]any{"type": "next", "project": project.ID, "tasks": []commons.Task{task}}, text)
}

func claimCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return fail(errOut, "USAGE", "claim requires TASK_ID")
	}
	task, ok := taskByID(commons.DemoProject, p.pos[0])
	if !ok {
		return fail(errOut, "NOT_FOUND", "task="+p.pos[0])
	}
	if task.State != "ready" {
		return fail(errOut, "CONFLICT", "task="+task.ID+" state="+task.State)
	}
	lease := p.flags["lease"]
	if lease == "" {
		lease = "2h"
	}
	ack := map[string]any{"type": "claim_simulation", "stub": true, "task": task.ID, "owner": "SIM-LOCAL", "lease": lease, "persisted": false, "request_id": p.flags["request-id"]}
	return emit(p.json, out, ack, fmt.Sprintf("WOULD_CLAIM task=%s owner=SIM-LOCAL lease=%s stub=true persisted=false", task.ID, lease))
}

func taskCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) < 1 {
		return fail(errOut, "USAGE", "task requires next or claim")
	}
	alias := parsed{pos: p.pos[1:], flags: p.flags, json: p.json}
	switch p.pos[0] {
	case "next":
		return nextCmd(alias, out, errOut)
	case "claim":
		return claimCmd(alias, out, errOut)
	default:
		return fail(errOut, "UNKNOWN_TASK_COMMAND", p.pos[0])
	}
}

func postCmd(p parsed, out, errOut io.Writer) int {
	if len(p.pos) != 2 {
		return fail(errOut, "USAGE", "post requires TOPIC KIND")
	}
	topic := p.pos[0]
	if topic != "general" && topic != commons.DemoProject.ID {
		return fail(errOut, "NOT_FOUND", "topic="+topic)
	}
	validKinds := map[string]bool{"finding": true, "question": true, "notice": true, "decision": true, "topic_request": true}
	if !validKinds[p.pos[1]] {
		return fail(errOut, "BAD_KIND", p.pos[1])
	}
	for _, field := range []string{"title", "body", "basis"} {
		if strings.TrimSpace(p.flags[field]) == "" {
			return fail(errOut, "MISSING_FIELD", "--"+field)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{topic, p.pos[1], p.flags["title"], p.flags["body"], p.flags["basis"]}, "\x00")))
	id := "sim-" + hex.EncodeToString(sum[:4])
	ack := map[string]any{"type": "post_simulation", "stub": true, "persisted": false, "id": id, "topic": topic, "kind": p.pos[1], "request_id": p.flags["request-id"]}
	return emit(p.json, out, ack, fmt.Sprintf("WOULD_POST id=%s topic=%s kind=%s stub=true persisted=false", id, topic, p.pos[1]))
}

func compactSnippet(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func projectByID(id string) (commons.Project, bool) {
	if id == commons.DemoProject.ID {
		return commons.DemoProject, true
	}
	return commons.Project{}, false
}

func nextTask(project commons.Project) (commons.Task, bool) {
	for _, task := range project.Tasks {
		if task.State == "ready" {
			return task, true
		}
	}
	return commons.Task{}, false
}

func taskByID(project commons.Project, id string) (commons.Task, bool) {
	for _, task := range project.Tasks {
		if task.ID == id {
			return task, true
		}
	}
	return commons.Task{}, false
}

func estimateTokens(s string) int {
	// Conservative dependency-free upper estimate used only for a response ceiling.
	return (len([]byte(s)) + 2) / 3
}

func emit(asJSON bool, out io.Writer, payload any, text string) int {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(payload); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintln(out, text)
	return 0
}

func fail(errOut io.Writer, code, detail string) int {
	fmt.Fprintf(errOut, "ERROR code=%s detail=%q\n", code, detail)
	return 2
}
