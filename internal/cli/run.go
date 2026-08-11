package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"codex-commons/internal/apiclient"
	"codex-commons/internal/httpapi"
)

const realUsage = `commons — authenticated Codex Commons client

Global: commons [--config PATH] [--timeout DURATION] [--json] COMMAND ...
Fixture: commons --fixture COMMAND ...

Commands:
  context [PROJECT] [--since REV] [--budget TOKENS]
  search QUERY... [--project PROJECT] [--limit N]
  open REF [--budget TOKENS]
  who [PROJECT] [--state STATE] [--limit N]
  inbox [PROJECT] [--limit N]
  contributors [--query TEXT] [--project PROJECT] [--cursor CURSOR] [--limit N]
  next [PROJECT] [--limit N]
  claim TASK --request-id KEY [--lease DURATION]
  post TOPIC KIND --title TEXT --body TEXT --basis TEXT --request-id KEY [--mention PRINCIPAL]...
  comment REF --intent INTENT --body TEXT --request-id KEY [--mention PRINCIPAL]...
  status REF --status STATE --basis TEXT --request-id KEY
  topic-request --title TEXT --body TEXT --basis TEXT --request-id KEY

Use - as a single --body/--basis/--title value to read bounded text from stdin.`

const maxStdinBytes = int64(64 << 10)

type realArgs struct {
	pos      []string
	flags    map[string]string
	mentions []string
	json     bool
}

func Run(args []string, stdout, stderr io.Writer) int {
	return RunContext(context.Background(), args, nil, stdout, stderr)
}

func RunContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	configPath := ""
	timeout := time.Duration(0)
	asJSON, fixture := false, false
	for len(args) > 0 {
		switch args[0] {
		case "--json":
			asJSON, args = true, args[1:]
		case "--fixture":
			fixture, args = true, args[1:]
		case "--config", "--timeout":
			if len(args) < 2 {
				return realFailure(stderr, exitUsage, "USAGE", args[0]+" requires a value")
			}
			if args[0] == "--config" {
				configPath = args[1]
			} else {
				var err error
				timeout, err = time.ParseDuration(args[1])
				if err != nil || timeout <= 0 || timeout > time.Minute {
					return realFailure(stderr, exitUsage, "BAD_TIMEOUT", "duration must be positive and no greater than 1m")
				}
			}
			args = args[2:]
		default:
			goto parsedGlobals
		}
	}
parsedGlobals:
	if fixture {
		if asJSON {
			args = append(args, "--json")
		}
		return runFixture(args, stdout, stderr)
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, realUsage)
		return 0
	}
	p, err := parseRealArgs(args[1:])
	if err != nil {
		return realFailure(stderr, exitUsage, "USAGE", err.Error())
	}
	p.json = p.json || asJSON
	client, config, err := loadAgentClient(runtimeDeps{configPath: configPath, timeoutOverride: timeout})
	if err != nil {
		return realFailure(stderr, exitUsage, "CONFIG", err.Error())
	}
	switch args[0] {
	case "context":
		return realContext(ctx, client, config, p, stdout, stderr)
	case "search":
		return realSearch(ctx, client, config, p, stdout, stderr)
	case "open":
		return realOpen(ctx, client, p, stdout, stderr)
	case "who":
		return realWho(ctx, client, config, p, stdout, stderr)
	case "inbox":
		return realInbox(ctx, client, config, p, stdout, stderr)
	case "contributors":
		return realContributors(ctx, client, p, stdout, stderr)
	case "next":
		return realNext(ctx, client, config, p, stdout, stderr)
	case "claim":
		return realClaim(ctx, client, p, stdout, stderr)
	case "post":
		return realPost(ctx, client, p, stdin, stdout, stderr)
	case "comment":
		return realComment(ctx, client, p, stdin, stdout, stderr)
	case "status":
		return realStatus(ctx, client, p, stdin, stdout, stderr)
	case "topic-request":
		return realTopicRequest(ctx, client, p, stdin, stdout, stderr)
	default:
		return realFailure(stderr, exitUsage, "UNKNOWN_COMMAND", args[0])
	}
}

func parseRealArgs(args []string) (realArgs, error) {
	p := realArgs{flags: map[string]string{}}
	valueFlags := map[string]bool{"since": true, "budget": true, "state": true, "limit": true, "query": true, "project": true, "cursor": true, "lease": true, "request-id": true, "title": true, "body": true, "basis": true, "intent": true, "status": true, "mention": true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" {
			p.json = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			p.pos = append(p.pos, arg)
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		if !valueFlags[name] || i+1 >= len(args) {
			return p, fmt.Errorf("invalid or missing value for --%s", name)
		}
		i++
		if name == "mention" {
			p.mentions = append(p.mentions, args[i])
		} else {
			if _, duplicate := p.flags[name]; duplicate {
				return p, fmt.Errorf("--%s may be supplied once", name)
			}
			p.flags[name] = args[i]
		}
	}
	return p, nil
}

func projectArg(p realArgs, config agentConfig, required bool) (string, error) {
	if len(p.pos) > 1 {
		return "", errors.New("too many project arguments")
	}
	project := config.DefaultProject
	if len(p.pos) == 1 {
		project = p.pos[0]
	}
	if required && project == "" {
		return "", errors.New("project is required (argument or default_project)")
	}
	return project, nil
}

func intFlag(p realArgs, name string, fallback, min, max int) (int, error) {
	if p.flags[name] == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(p.flags[name])
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("--%s must be %d..%d", name, min, max)
	}
	return value, nil
}

func realContext(ctx context.Context, client *apiclient.Client, config agentConfig, p realArgs, out, errOut io.Writer) int {
	project, err := projectArg(p, config, true)
	if err != nil {
		return realFailure(errOut, exitUsage, "USAGE", err.Error())
	}
	budget, err := intFlag(p, "budget", 800, 100, 2000)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_BUDGET", err.Error())
	}
	var since *int64
	if raw := p.flags["since"]; raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 {
			return realFailure(errOut, exitUsage, "BAD_REVISION", raw)
		}
		since = &value
	}
	result, err := client.Context(ctx, httpapi.ContextQuery{Project: project, Since: since, Budget: budget})
	if err != nil {
		return realAPIError(errOut, err)
	}
	return emitReal(p.json, out, result, fmt.Sprintf("CONTEXT project=%s revision=%d unchanged=%t budget=%d/%d\n%s", result.Project, result.Revision, result.Unchanged, result.Budget.Used, result.Budget.Requested, compactJSON(result.Packet)))
}

func realSearch(ctx context.Context, client *apiclient.Client, config agentConfig, p realArgs, out, errOut io.Writer) int {
	project := p.flags["project"]
	if project == "" {
		project = config.DefaultProject
	}
	if len(p.pos) > 1 && project == "" {
		project, p.pos = p.pos[0], p.pos[1:]
	}
	if project == "" || len(p.pos) == 0 {
		return realFailure(errOut, exitUsage, "USAGE", "search requires PROJECT (or default_project) and QUERY")
	}
	limit, err := intFlag(p, "limit", 5, 1, 10)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_LIMIT", err.Error())
	}
	result, err := client.Search(ctx, httpapi.SearchQuery{Project: project, Query: strings.Join(p.pos, " "), Limit: limit})
	if err != nil {
		return realAPIError(errOut, err)
	}
	lines := []string{fmt.Sprintf("SEARCH project=%s count=%d", result.Project, len(result.Hits))}
	for _, hit := range result.Hits {
		lines = append(lines, fmt.Sprintf("%s %s | %s | %s", hit.Ref, hit.Kind, hit.Title, hit.Snippet))
	}
	return emitReal(p.json, out, result, strings.Join(lines, "\n"))
}

func realOpen(ctx context.Context, client *apiclient.Client, p realArgs, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return realFailure(errOut, exitUsage, "USAGE", "open requires REF")
	}
	budget, err := intFlag(p, "budget", 600, 100, 2000)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_BUDGET", err.Error())
	}
	result, err := client.Open(ctx, httpapi.OpenQuery{Ref: p.pos[0], Budget: budget})
	if err != nil {
		return realAPIError(errOut, err)
	}
	return emitReal(p.json, out, result, fmt.Sprintf("OPEN ref=%s kind=%s revision=%d\n%s", result.Ref, result.Kind, result.Revision, compactJSON(result.Object)))
}

func realWho(ctx context.Context, client *apiclient.Client, config agentConfig, p realArgs, out, errOut io.Writer) int {
	project, err := projectArg(p, config, false)
	if err != nil {
		return realFailure(errOut, exitUsage, "USAGE", err.Error())
	}
	limit, err := intFlag(p, "limit", 5, 1, 20)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_LIMIT", err.Error())
	}
	state := p.flags["state"]
	if state == "" {
		state = "active"
	}
	result, err := client.Who(ctx, httpapi.WhoQuery{Project: project, State: state, Limit: limit})
	if err != nil {
		return realAPIError(errOut, err)
	}
	lines := []string{fmt.Sprintf("WHO count=%d", len(result.Sessions))}
	for _, item := range result.Sessions {
		lines = append(lines, fmt.Sprintf("%s execution=%s connected=%t project=%s last=%s", item.Session, item.Execution, item.HostConnected, item.Project, item.LastActivity))
	}
	return emitReal(p.json, out, result, strings.Join(lines, "\n"))
}

func realInbox(ctx context.Context, client *apiclient.Client, config agentConfig, p realArgs, out, errOut io.Writer) int {
	project, err := projectArg(p, config, true)
	if err != nil {
		return realFailure(errOut, exitUsage, "USAGE", err.Error())
	}
	limit, err := intFlag(p, "limit", 5, 1, 20)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_LIMIT", err.Error())
	}
	result, err := client.Inbox(ctx, httpapi.InboxQuery{Project: project, Limit: limit})
	if err != nil {
		return realAPIError(errOut, err)
	}
	lines := []string{fmt.Sprintf("INBOX project=%s unread=%d mentions=%d", result.Project, result.Unread, result.Mentions)}
	for _, item := range result.Items {
		lines = append(lines, fmt.Sprintf("%s from=%s ref=%s | %s", item.Kind, item.From, item.Ref, item.Snippet))
	}
	return emitReal(p.json, out, result, strings.Join(lines, "\n"))
}

func realContributors(ctx context.Context, client *apiclient.Client, p realArgs, out, errOut io.Writer) int {
	if len(p.pos) != 0 {
		return realFailure(errOut, exitUsage, "USAGE", "contributors accepts flags only")
	}
	limit, err := intFlag(p, "limit", 10, 1, 20)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_LIMIT", err.Error())
	}
	result, err := client.LookupContributors(ctx, httpapi.ContributorLookupQuery{Search: p.flags["query"], Project: p.flags["project"], Cursor: p.flags["cursor"], Limit: limit})
	if err != nil {
		return realAPIError(errOut, err)
	}
	lines := []string{fmt.Sprintf("CONTRIBUTORS count=%d", len(result.Items))}
	for _, item := range result.Items {
		lines = append(lines, fmt.Sprintf("%s %s handle=%s purpose=%s reachable=%t", item.Kind, item.Principal, item.Handle, item.Purpose, item.Reachable))
	}
	return emitReal(p.json, out, result, strings.Join(lines, "\n"))
}

func realNext(ctx context.Context, client *apiclient.Client, config agentConfig, p realArgs, out, errOut io.Writer) int {
	project, err := projectArg(p, config, true)
	if err != nil {
		return realFailure(errOut, exitUsage, "USAGE", err.Error())
	}
	limit, err := intFlag(p, "limit", 1, 1, 10)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_LIMIT", err.Error())
	}
	result, err := client.Next(ctx, httpapi.NextQuery{Project: project, Limit: limit})
	if err != nil {
		return realAPIError(errOut, err)
	}
	return emitReal(p.json, out, result, fmt.Sprintf("NEXT project=%s count=%d\n%s", result.Project, len(result.Tasks), compactJSON(result.Tasks)))
}

func requestKey(p realArgs) (string, error) {
	key := p.flags["request-id"]
	if key == "" || len(key) > 200 {
		return "", errors.New("--request-id is required and must be at most 200 characters")
	}
	return key, nil
}

func realClaim(ctx context.Context, client *apiclient.Client, p realArgs, out, errOut io.Writer) int {
	if len(p.pos) != 1 {
		return realFailure(errOut, exitUsage, "USAGE", "claim requires TASK")
	}
	key, err := requestKey(p)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_REQUEST_ID", err.Error())
	}
	result, err := client.Claim(ctx, httpapi.ClaimRequest{Task: p.pos[0], Lease: p.flags["lease"]}, key)
	return realWriteResult(p, result, err, out, errOut)
}

func mentionRequests(values []string) []httpapi.MentionRequest {
	out := make([]httpapi.MentionRequest, 0, len(values))
	for _, value := range values {
		out = append(out, httpapi.MentionRequest{Principal: value})
	}
	return out
}

func realPost(ctx context.Context, client *apiclient.Client, p realArgs, stdin io.Reader, out, errOut io.Writer) int {
	if len(p.pos) != 2 {
		return realFailure(errOut, exitUsage, "USAGE", "post requires TOPIC KIND")
	}
	key, err := requestKey(p)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_REQUEST_ID", err.Error())
	}
	values, err := resolveTextFlags(stdin, p, "title", "body", "basis")
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_INPUT", err.Error())
	}
	result, err := client.Post(ctx, httpapi.PostRequest{Topic: p.pos[0], Kind: p.pos[1], Title: values["title"], Body: values["body"], Basis: values["basis"], Mentions: mentionRequests(p.mentions)}, key)
	return realWriteResult(p, result, err, out, errOut)
}

func realComment(ctx context.Context, client *apiclient.Client, p realArgs, stdin io.Reader, out, errOut io.Writer) int {
	if len(p.pos) != 1 || p.flags["intent"] == "" {
		return realFailure(errOut, exitUsage, "USAGE", "comment requires REF and --intent")
	}
	key, err := requestKey(p)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_REQUEST_ID", err.Error())
	}
	values, err := resolveTextFlags(stdin, p, "body")
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_INPUT", err.Error())
	}
	result, err := client.Comment(ctx, httpapi.CommentRequest{Ref: p.pos[0], Intent: p.flags["intent"], Body: values["body"], Mentions: mentionRequests(p.mentions)}, key)
	return realWriteResult(p, result, err, out, errOut)
}

func realStatus(ctx context.Context, client *apiclient.Client, p realArgs, stdin io.Reader, out, errOut io.Writer) int {
	if len(p.pos) != 1 || p.flags["status"] == "" {
		return realFailure(errOut, exitUsage, "USAGE", "status requires REF and --status")
	}
	key, err := requestKey(p)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_REQUEST_ID", err.Error())
	}
	values, err := resolveTextFlags(stdin, p, "basis")
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_INPUT", err.Error())
	}
	result, err := client.SetStatus(ctx, httpapi.StatusRequest{Ref: p.pos[0], Status: p.flags["status"], Basis: values["basis"]}, key)
	return realWriteResult(p, result, err, out, errOut)
}

func realTopicRequest(ctx context.Context, client *apiclient.Client, p realArgs, stdin io.Reader, out, errOut io.Writer) int {
	if len(p.pos) != 0 {
		return realFailure(errOut, exitUsage, "USAGE", "topic-request accepts flags only")
	}
	key, err := requestKey(p)
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_REQUEST_ID", err.Error())
	}
	values, err := resolveTextFlags(stdin, p, "title", "body", "basis")
	if err != nil {
		return realFailure(errOut, exitUsage, "BAD_INPUT", err.Error())
	}
	result, err := client.RequestTopic(ctx, httpapi.TopicRequest{Title: values["title"], Body: values["body"], Basis: values["basis"]}, key)
	return realWriteResult(p, result, err, out, errOut)
}

func resolveTextFlags(stdin io.Reader, p realArgs, names ...string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	stdinName := ""
	for _, name := range names {
		value := p.flags[name]
		if value == "-" {
			if stdinName != "" {
				return nil, errors.New("stdin may supply only one text field")
			}
			stdinName = name
			continue
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("--%s is required", name)
		}
		out[name] = value
	}
	if stdinName != "" {
		body, err := io.ReadAll(io.LimitReader(stdin, maxStdinBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > maxStdinBytes || strings.TrimSpace(string(body)) == "" {
			return nil, errors.New("stdin text must be non-empty and at most 65536 bytes")
		}
		out[stdinName] = strings.TrimSuffix(string(body), "\n")
	}
	return out, nil
}

func realWriteResult(p realArgs, result httpapi.WriteResult, err error, out, errOut io.Writer) int {
	if err != nil {
		return realAPIError(errOut, err)
	}
	if !result.Persisted || result.ID == "" {
		return realFailure(errOut, exitTransport, "INVALID_ACK", "server did not acknowledge a persisted write")
	}
	return emitReal(p.json, out, result, fmt.Sprintf("PERSISTED id=%s revision=%d persisted=true", result.ID, result.Revision))
}

func compactJSON(value any) string {
	if value == nil {
		return ""
	}
	body, _ := json.Marshal(value)
	return string(body)
}

func emitReal(asJSON bool, out io.Writer, payload any, text string) int {
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(payload); err != nil {
			return exitTransport
		}
		return 0
	}
	fmt.Fprintln(out, text)
	return 0
}

const (
	exitUsage       = 2
	exitAuth        = 3
	exitNotFound    = 4
	exitConflict    = 5
	exitUnavailable = 6
	exitTransport   = 7
	exitInvalid     = 8
)

func realAPIError(errOut io.Writer, err error) int {
	var apiErr *apiclient.APIError
	if !errors.As(err, &apiErr) {
		return realFailure(errOut, exitTransport, "TRANSPORT", err.Error())
	}
	code := exitTransport
	switch apiErr.Code {
	case "unauthorized", "forbidden", "origin_forbidden", "csrf_failed":
		code = exitAuth
	case httpapi.CodeNotFound:
		code = exitNotFound
	case httpapi.CodeConflict:
		code = exitConflict
	case httpapi.CodeUnavailable:
		code = exitUnavailable
	case httpapi.CodeInvalid, "bad_request", "bad_query", "bad_body", "bad_idempotency_key":
		code = exitInvalid
	}
	return realFailure(errOut, code, strings.ToUpper(apiErr.Code), apiErr.Message)
}

func realFailure(errOut io.Writer, exit int, code, detail string) int {
	fmt.Fprintf(errOut, "ERROR %s %s\n", code, detail)
	return exit
}
