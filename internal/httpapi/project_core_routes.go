package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func strictPathSegments(requestURL *url.URL) ([]string, bool) {
	if requestURL == nil {
		return nil, false
	}
	escaped := strings.Trim(requestURL.EscapedPath(), "/")
	if escaped == "" {
		return nil, false
	}
	raw := strings.Split(escaped, "/")
	out := make([]string, len(raw))
	for i, segment := range raw {
		value, err := url.PathUnescape(segment)
		if err != nil || value == "" || strings.ContainsRune(value, 0) {
			return nil, false
		}
		out[i] = value
	}
	return out, true
}

func classifyProjectCoreRoute(method string, parts []string) string {
	if len(parts) < 2 || parts[0] != "v1" {
		return ""
	}
	switch {
	case len(parts) == 2 && parts[1] == "projects" && method == http.MethodPost:
		return "project-create"
	case len(parts) == 3 && parts[1] == "projects" && method == http.MethodGet:
		return "project-open"
	case len(parts) == 3 && parts[1] == "projects" && method == http.MethodPut:
		return "project-update"
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "milestones" && method == http.MethodGet:
		return "milestone-list"
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "milestones" && method == http.MethodPost:
		return "milestone-create"
	case len(parts) == 3 && parts[1] == "milestones" && method == http.MethodPut:
		return "milestone-update"
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "tasks" && method == http.MethodGet:
		return "task-list"
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "tasks" && method == http.MethodPost:
		return "task-create"
	case len(parts) == 3 && parts[1] == "tasks" && method == http.MethodGet:
		return "task-open"
	case len(parts) == 3 && parts[1] == "tasks" && method == http.MethodPut:
		return "task-update"
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "state" && method == http.MethodPost:
		return "task-state"
	case len(parts) == 4 && parts[1] == "tasks" && parts[3] == "events" && method == http.MethodGet:
		return "task-events"
	case len(parts) == 4 && parts[1] == "projects" && parts[3] == "wiki" && method == http.MethodGet:
		return "wiki-list"
	case len(parts) == 5 && parts[1] == "projects" && parts[3] == "wiki" && method == http.MethodGet:
		return "wiki-open"
	case len(parts) == 6 && parts[1] == "projects" && parts[3] == "wiki" && parts[5] == "revisions" && method == http.MethodGet:
		return "wiki-history"
	case len(parts) == 6 && parts[1] == "projects" && parts[3] == "wiki" && parts[5] == "revisions" && method == http.MethodPost:
		return "wiki-append"
	case len(parts) == 7 && parts[1] == "projects" && parts[3] == "wiki" && parts[5] == "revisions" && method == http.MethodGet:
		return "wiki-revision-open"
	case len(parts) == 5 && parts[1] == "projects" && parts[3] == "historical-imports" && parts[4] == "preview" && method == http.MethodPost:
		return "historical-import-preview"
	case len(parts) == 5 && parts[1] == "projects" && parts[3] == "historical-imports" && parts[4] == "apply" && method == http.MethodPost:
		return "historical-import-apply"
	case len(parts) == 6 && parts[1] == "projects" && parts[3] == "historical-imports" && parts[5] == "supersede" && method == http.MethodPost:
		return "historical-import-supersede"
	default:
		return ""
	}
}

func (h *handler) projectCoreRoute(w http.ResponseWriter, r *http.Request, meta RequestMeta) bool {
	parts, ok := strictPathSegments(r.URL)
	if !ok {
		return false
	}
	route := classifyProjectCoreRoute(r.Method, parts)
	if route == "" {
		return false
	}
	backend, ok := h.backend.(ProjectCoreBackend)
	if !ok {
		h.finish(w, meta, nil, NewError(CodeUnavailable, "project core unavailable"), false)
		return true
	}
	project := ""
	if len(parts) > 2 && parts[1] == "projects" {
		project = parts[2]
	}

	switch route {
	case "project-open":
		out, err := backend.ProjectCoreDetail(r.Context(), project, meta)
		h.finish(w, meta, out, err, true)
	case "milestone-list":
		limit, err := intParam(r.URL.Query().Get("limit"), 100, 1, 100)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		out, err := backend.ListProjectMilestones(r.Context(), ProjectMilestoneListQuery{Project: project, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case "task-list":
		limit, err := intParam(r.URL.Query().Get("limit"), 25, 1, 25)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		out, err := backend.ListProjectTasks(r.Context(), ProjectTaskListQuery{
			Project: project, Cursor: r.URL.Query().Get("cursor"), State: r.URL.Query().Get("state"),
			Milestone: r.URL.Query().Get("milestone"), Limit: limit,
		}, meta)
		h.finish(w, meta, out, err, true)
	case "task-open":
		limit, err := intParam(r.URL.Query().Get("events_limit"), 20, 1, 50)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		out, err := backend.OpenProjectTask(r.Context(), ProjectTaskOpenQuery{Task: parts[2], EventsLimit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case "task-events":
		limit, err := intParam(r.URL.Query().Get("limit"), 20, 1, 50)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		out, err := backend.ListProjectTaskEvents(r.Context(), ProjectTaskEventListQuery{Task: parts[2], Cursor: r.URL.Query().Get("cursor"), Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case "wiki-list":
		limit, err := intParam(r.URL.Query().Get("limit"), 25, 1, 100)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		search := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(search) > 200 {
			h.badQuery(w, meta, errors.New("q maximum length is 200"))
			return true
		}
		out, err := backend.ListProjectWiki(r.Context(), ProjectWikiListQuery{Project: project, Cursor: r.URL.Query().Get("cursor"), Search: search, Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case "wiki-open":
		out, err := backend.OpenProjectWiki(r.Context(), ProjectWikiOpenQuery{Project: project, Slug: parts[4]}, meta)
		h.finish(w, meta, out, err, true)
	case "wiki-history":
		limit, err := intParam(r.URL.Query().Get("limit"), 25, 1, 100)
		if err != nil {
			h.badQuery(w, meta, err)
			return true
		}
		out, err := backend.ListProjectWikiRevisions(r.Context(), ProjectWikiHistoryQuery{Project: project, Slug: parts[4], Cursor: r.URL.Query().Get("cursor"), Limit: limit}, meta)
		h.finish(w, meta, out, err, true)
	case "wiki-revision-open":
		revision, err := strconv.ParseInt(parts[6], 10, 64)
		if err != nil || revision < 1 {
			h.badQuery(w, meta, errors.New("revision must be a positive integer"))
			return true
		}
		out, err := backend.OpenProjectWiki(r.Context(), ProjectWikiOpenQuery{Project: project, Slug: parts[4], Revision: revision}, meta)
		h.finish(w, meta, out, err, true)
	case "project-create":
		var in CreateCoreProjectRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.CreateCoreProject(r.Context(), in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "project-update":
		var in UpdateCoreProjectRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.UpdateCoreProject(r.Context(), project, in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "milestone-create":
		var in CreateCoreMilestoneRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.CreateCoreMilestone(r.Context(), project, in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "milestone-update":
		var in UpdateCoreMilestoneRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.UpdateCoreMilestone(r.Context(), parts[2], in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "task-create":
		var in CreateCoreTaskRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.CreateCoreTask(r.Context(), project, in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "task-update":
		var in UpdateCoreTaskRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.UpdateCoreTask(r.Context(), parts[2], in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "task-state":
		var in ChangeCoreTaskStateRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.ChangeCoreTaskState(r.Context(), parts[2], in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "wiki-append":
		var in AppendCoreWikiRevisionRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.AppendCoreWikiRevision(r.Context(), project, parts[4], in, meta)
		h.finishWrite(w, meta, out, err, false)
	case "historical-import-preview":
		var in HistoricalImportRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.PreviewHistoricalImport(r.Context(), project, in, meta)
		h.finish(w, meta, out, err, true)
	case "historical-import-apply":
		var in HistoricalImportRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.ApplyHistoricalImport(r.Context(), project, in, meta)
		h.finish(w, meta, out, err, true)
	case "historical-import-supersede":
		var in SupersedeHistoricalImportRequest
		if !h.decodeCoreWrite(w, r, meta, &in) {
			return true
		}
		out, err := backend.SupersedeHistoricalImport(r.Context(), project, parts[4], in, meta)
		h.finish(w, meta, out, err, true)
	}

	return true
}

func (h *handler) decodeCoreWrite(w http.ResponseWriter, r *http.Request, meta RequestMeta, dst any) bool {
	if meta.IdempotencyKey == "" {
		h.badBody(w, meta, "Idempotency-Key required")
		return false
	}
	return h.decode(w, r, meta, dst)
}
