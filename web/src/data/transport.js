import { MAX_API_RESPONSE_BYTES, MAX_BROWSE_LIMIT, MAX_NOTIFICATIONS, MAX_OVERVIEW_LIMIT, MAX_TASK_EVENTS, MAX_TASK_LIST, MAX_WIKI_REVISIONS } from "../contracts/commons.js";

const encoder = new TextEncoder();

export class CommonsAPIError extends Error {
  constructor(message, { code = "client_error", status = 0, requestID = "" } = {}) {
    super(message);
    this.name = "CommonsAPIError";
    this.code = code;
    this.status = status;
    this.requestID = requestID;
  }
}

function invalidResponse(detail = "") {
  return new CommonsAPIError("Commons returned an invalid response.", {
    code: detail ? `invalid_${detail}` : "invalid_response",
  });
}

function boundedText(value, maximumBytes = 200) {
  const trimmed = typeof value === "string" ? value.trim() : "";
  if (encoder.encode(trimmed).byteLength <= maximumBytes) return trimmed;
  let result = "";
  for (const character of trimmed) {
    if (encoder.encode(result + character).byteLength > maximumBytes) break;
    result += character;
  }
  return result;
}

function boundedLimit(value, maximum) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1 || parsed > maximum) {
    throw new CommonsAPIError(`Page size must be between 1 and ${maximum}.`, { code: "invalid_limit" });
  }
  return parsed;
}

function appendQuery(path, query) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") continue;
    params.set(key, String(value));
  }
  const encoded = params.toString();
  return encoded ? `${path}?${encoded}` : path;
}

async function readBoundedJSON(response, maximumBytes) {
  const contentLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(contentLength) && contentLength > maximumBytes) {
    throw new CommonsAPIError("Commons returned too much data.", {
      code: "response_too_large", status: response.status,
    });
  }
  const reader = response.body?.getReader();
  if (!reader) {
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (bytes.byteLength > maximumBytes) {
      throw new CommonsAPIError("Commons returned too much data.", {
        code: "response_too_large", status: response.status,
      });
    }
    try {
      return JSON.parse(new TextDecoder().decode(bytes));
    } catch {
      throw invalidResponse("json");
    }
  }

  const decoder = new TextDecoder();
  let size = 0;
  let text = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > maximumBytes) {
      await reader.cancel();
      throw new CommonsAPIError("Commons returned too much data.", {
        code: "response_too_large", status: response.status,
      });
    }
    text += decoder.decode(value, { stream: true });
  }
  text += decoder.decode();
  try {
    return JSON.parse(text);
  } catch {
    throw invalidResponse("json");
  }
}

function responseError(response, envelope) {
  const requestID = envelope?.meta?.request_id || "";
  const code = envelope?.error?.code || `http_${response.status}`;
  let message = "Commons could not complete this request.";
  if (response.status === 400) message = "Commons rejected this request.";
  if (response.status === 401) message = "Your writing session has expired. Sign in and try again.";
  if (response.status === 403) message = code === "csrf_failed"
    ? "Your writing session changed. Sign in and try again."
    : "Commons access is not available for this browser.";
  if (response.status === 404) message = "The requested Commons record was not found.";
  if (response.status === 409) message = "This request conflicts with newer Commons activity.";
  if (response.status === 429) message = "Too many write attempts. Wait a moment and try again.";
  if (response.status === 503) message = "Commons is temporarily unavailable.";
  if (code === "codex_unavailable") message = "Codex sign-in is unavailable on this Commons installation.";
  if (code === "account_mismatch") message = "This Commons installation is already linked to another Codex account.";
  if (code === "codex_identity_unavailable") message = "Codex did not provide an account identity that Commons can use.";
  if (code === "first_bind_lan_forbidden") message = "The first Codex link must begin on this machine or with explicit LAN acknowledgement.";
  if (code === "profile_conflict") message = "This Commons installation is already linked to another account or handle.";
  if (code === "profile_unavailable") message = "This Codex setup step is no longer available. Start again.";
  if (code === "authorization_cancelled") message = "Codex sign-in was cancelled.";
  if (code === "authorization_failed") message = "Codex sign-in was not completed.";
  if (code === "pairing_not_found") message = "That Codex sign-in attempt is no longer available.";
  if (code === "pairing_attempt_active") message = "A Codex sign-in is already active in this browser. Return to its code or cancel it before starting again.";
  return new CommonsAPIError(message, { code, status: response.status, requestID });
}

export function createHTTPTransport({
  fetchImpl = globalThis.fetch,
  maximumResponseBytes = MAX_API_RESPONSE_BYTES,
} = {}) {
  if (typeof fetchImpl !== "function") throw new TypeError("fetch implementation required");
  if (!Number.isInteger(maximumResponseBytes) || maximumResponseBytes < 1 || maximumResponseBytes > MAX_API_RESPONSE_BYTES) {
    throw new TypeError("invalid maximum response bytes");
  }

  async function read(path, query, signal) {
    let response;
    try {
      response = await fetchImpl(appendQuery(path, query), {
        method: "GET",
        headers: { Accept: "application/json" },
        credentials: "same-origin",
        signal,
      });
    } catch (error) {
      if (error?.name === "AbortError") throw error;
      throw new CommonsAPIError("Commons could not be reached.", { code: "network_error" });
    }
    const contentType = response.headers.get("content-type") || "";
    if (!contentType.toLowerCase().includes("application/json")) throw invalidResponse("content_type");
    const envelope = await readBoundedJSON(response, maximumResponseBytes);
    if (!response.ok || envelope?.ok === false) throw responseError(response, envelope);
    if (envelope?.ok !== true || !Object.hasOwn(envelope, "data") || !envelope.meta
      || typeof envelope.meta !== "object" || typeof envelope.meta.request_id !== "string" || !envelope.meta.request_id) {
      throw invalidResponse("envelope");
    }
    return envelope.data;
  }

  async function mutate(method, path, body, { csrfToken = "", idempotencyKey = "" } = {}, signal) {
    let response;
    try {
      response = await fetchImpl(path, {
        method,
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
          ...(idempotencyKey ? { "Idempotency-Key": idempotencyKey } : {}),
          ...(csrfToken ? { "X-Commons-CSRF": csrfToken } : {}),
        },
        credentials: "same-origin",
        body: JSON.stringify(body),
        signal,
      });
    } catch (error) {
      if (error?.name === "AbortError") throw error;
      throw new CommonsAPIError("Commons could not be reached.", { code: "network_error" });
    }
    const contentType = response.headers.get("content-type") || "";
    if (!contentType.toLowerCase().includes("application/json")) throw invalidResponse("content_type");
    const envelope = await readBoundedJSON(response, maximumResponseBytes);
    if (!response.ok || envelope?.ok === false) throw responseError(response, envelope);
    if (envelope?.ok !== true || !Object.hasOwn(envelope, "data")) throw invalidResponse("envelope");
    return envelope.data;
  }

  function write(path, body, options, signal) {
    return mutate("POST", path, body, options, signal);
  }

  function replace(path, body, options, signal) {
    return mutate("PUT", path, body, options, signal);
  }

  return {
    readSession(signal) {
      return read("/v1/auth/session", {}, signal);
    },
    readCodexStatus(signal) {
      return read("/v1/auth/codex/status", {}, signal);
    },
    startCodexPairing(signal) {
      return write("/v1/auth/codex/start", {}, {}, signal);
    },
    pollCodexPairing(attemptID, signal) {
      return write("/v1/auth/codex/poll", { attempt_id: attemptID }, {}, signal);
    },
    completeCodexProfile(input, signal) {
      return write("/v1/auth/codex/profile", input, {}, signal);
    },
    cancelCodexPairing(attemptID, signal) {
      return write("/v1/auth/codex/cancel", { attempt_id: attemptID }, {}, signal);
    },
    login(secret, idempotencyKey, signal) {
      return write("/v1/auth/login", { secret }, { idempotencyKey }, signal);
    },
    logout(csrfToken, idempotencyKey, signal) {
      return write("/v1/auth/logout", {}, { csrfToken, idempotencyKey }, signal);
    },
    updateProfile(input, csrfToken, idempotencyKey, signal) {
      return replace("/v1/auth/profile", input, { csrfToken, idempotencyKey }, signal);
    },
    readProjectArchaeology(signal) {
      return read("/v1/project-archaeology", {}, signal);
    },
    discoverProjectArchaeology(writeOptions, signal) {
      return write("/v1/project-archaeology/discover", {}, writeOptions, signal);
    },
    updateProjectArchaeologyConfig(input, writeOptions, signal) {
      return replace("/v1/project-archaeology/config", input, writeOptions, signal);
    },
    startProjectArchaeology(input, writeOptions, signal) {
      return write("/v1/project-archaeology/start", input, writeOptions, signal);
    },
    pauseProjectArchaeology(input, writeOptions, signal) {
      return write("/v1/project-archaeology/pause", input, writeOptions, signal);
    },
    resumeProjectArchaeology(input, writeOptions, signal) {
      return write("/v1/project-archaeology/resume", input, writeOptions, signal);
    },
    cancelProjectArchaeology(input, writeOptions, signal) {
      return write("/v1/project-archaeology/cancel", input, writeOptions, signal);
    },
    previewProjectArchaeologyImport(input, writeOptions, signal) {
      return write("/v1/project-archaeology/import-preview", input, writeOptions, signal);
    },
    applyHistoricalImport(projectID, input, writeOptions, signal) {
      return write(`/v1/projects/${encodeURIComponent(projectID)}/historical-imports/apply`, input, writeOptions, signal);
    },
    readPosts(query, signal) {
      return read("/v1/posts", {
        q: boundedText(query.q), topic: query.topic, project: query.project,
        kind: query.kind, created_from: query.created_from, created_to: query.created_to,
        cursor: query.cursor, limit: boundedLimit(query.limit, 50),
      }, signal);
    },
    readContributors(query, signal) {
      return read("/v1/contributors", { q: boundedText(query.q), project: query.project, cursor: query.cursor, limit: boundedLimit(query.limit, 20) }, signal);
    },
    readNotifications(query, signal) {
      return read("/v1/notifications", {
        unread: query.unread,
        cursor: query.cursor,
        limit: boundedLimit(query.limit, MAX_NOTIFICATIONS),
      }, signal);
    },
    markNotificationRead(input, writeOptions, signal) {
      return write("/v1/notification-reads", input, writeOptions, signal);
    },

    readTopics(limit = 100, signal) {
      return read("/v1/topics", { limit: boundedLimit(limit, 100) }, signal);
    },
    readPost(postID, query, signal) {
      if (typeof postID !== "string" || !postID) throw new CommonsAPIError("Post ID is required.", { code: "invalid_post" });
      return read(`/v1/posts/${encodeURIComponent(postID)}`, {
        comments_cursor: query.comments_cursor,
        comments_limit: boundedLimit(query.comments_limit, 20),
      }, signal);
    },
    readCommentSource(commentID, signal) {
      if (typeof commentID !== "string" || !commentID) throw new CommonsAPIError("Comment ID is required.", { code: "invalid_comment" });
      return read(`/v1/comments/${encodeURIComponent(commentID)}`, {}, signal);
    },
    createPost(input, writeOptions, signal) {
      return write("/v1/posts", input, writeOptions, signal);
    },
    createComment(input, writeOptions, signal) {
      return write("/v1/comments", input, writeOptions, signal);
    },
    changePostState(input, writeOptions, signal) {
      return write("/v1/post-states", input, writeOptions, signal);
    },
    changePerspectiveScope(input, writeOptions, signal) {
      return write("/v1/post-perspective-scopes", input, writeOptions, signal);
    },
    readAttention(query, signal) {
      return read("/v1/attention", {
        q: boundedText(query.q), source: query.source, owner: query.owner,
        severity: query.severity, project: query.project,
        updated_from: query.updated_from, updated_to: query.updated_to,
        cursor: query.cursor, limit: boundedLimit(query.limit, MAX_BROWSE_LIMIT),
      }, signal);
    },
    readProjects(query, signal) {
      return read("/v1/projects", {
        q: boundedText(query.q), cursor: query.cursor,
        limit: boundedLimit(query.limit, MAX_BROWSE_LIMIT),
      }, signal);
    },
    readPeople(query, signal) {
      return read("/v1/people", {
        q: boundedText(query.q), project: query.project, execution: query.execution,
        host: query.host, host_connected: query.host_connected,
        cursor: query.cursor, limit: boundedLimit(query.limit, MAX_BROWSE_LIMIT),
      }, signal);
    },
    readProjectOverview(projectID, query, signal) {
      if (typeof projectID !== "string" || !projectID) {
        throw new CommonsAPIError("Project ID is required.", { code: "invalid_project" });
      }
      return read(`/v1/projects/${encodeURIComponent(projectID)}/overview`, {
        attention_limit: boundedLimit(query.attention_limit, MAX_OVERVIEW_LIMIT),
        work_limit: boundedLimit(query.work_limit, MAX_OVERVIEW_LIMIT),
      }, signal);
    },
    readProject(projectID, signal) {
      if (typeof projectID !== "string" || !projectID) throw new CommonsAPIError("Project ID is required.", { code: "invalid_project" });
      return read(`/v1/projects/${encodeURIComponent(projectID)}`, {}, signal);
    },
    readProjectMilestones(projectID, limit = 100, signal) {
      if (typeof projectID !== "string" || !projectID) throw new CommonsAPIError("Project ID is required.", { code: "invalid_project" });
      return read(`/v1/projects/${encodeURIComponent(projectID)}/milestones`, { limit: boundedLimit(limit, MAX_BROWSE_LIMIT) }, signal);
    },
    readProjectTasks(projectID, query, signal) {
      if (typeof projectID !== "string" || !projectID) throw new CommonsAPIError("Project ID is required.", { code: "invalid_project" });
      return read(`/v1/projects/${encodeURIComponent(projectID)}/tasks`, {
        state: query.state, milestone: query.milestone, cursor: query.cursor,
        limit: boundedLimit(query.limit, MAX_TASK_LIST),
      }, signal);
    },
    readTask(taskID, eventsLimit = 20, signal) {
      if (typeof taskID !== "string" || !taskID) throw new CommonsAPIError("Task ID is required.", { code: "invalid_task" });
      return read(`/v1/tasks/${encodeURIComponent(taskID)}`, { events_limit: boundedLimit(eventsLimit, MAX_TASK_EVENTS) }, signal);
    },
    readTaskEvents(taskID, query, signal) {
      if (typeof taskID !== "string" || !taskID) throw new CommonsAPIError("Task ID is required.", { code: "invalid_task" });
      return read(`/v1/tasks/${encodeURIComponent(taskID)}/events`, {
        cursor: query.cursor,
        limit: boundedLimit(query.limit, MAX_TASK_EVENTS),
      }, signal);
    },
    readWikiPages(projectID, query, signal) {
      if (typeof projectID !== "string" || !projectID) throw new CommonsAPIError("Project ID is required.", { code: "invalid_project" });
      return read(`/v1/projects/${encodeURIComponent(projectID)}/wiki`, {
        q: boundedText(query.q), cursor: query.cursor,
        limit: boundedLimit(query.limit, MAX_BROWSE_LIMIT),
      }, signal);
    },
    readWikiPage(projectID, slug, signal) {
      if (typeof projectID !== "string" || !projectID || typeof slug !== "string" || !slug) {
        throw new CommonsAPIError("Project and wiki slug are required.", { code: "invalid_wiki_page" });
      }
      return read(`/v1/projects/${encodeURIComponent(projectID)}/wiki/${encodeURIComponent(slug)}`, {}, signal);
    },
    readWikiRevisions(projectID, slug, query, signal) {
      if (typeof projectID !== "string" || !projectID || typeof slug !== "string" || !slug) {
        throw new CommonsAPIError("Project and wiki slug are required.", { code: "invalid_wiki_page" });
      }
      return read(`/v1/projects/${encodeURIComponent(projectID)}/wiki/${encodeURIComponent(slug)}/revisions`, {
        cursor: query.cursor, limit: boundedLimit(query.limit, MAX_WIKI_REVISIONS),
      }, signal);
    },
    readWikiRevision(projectID, slug, revision, signal) {
      if (typeof projectID !== "string" || !projectID || typeof slug !== "string" || !slug || !Number.isInteger(revision) || revision < 1) {
        throw new CommonsAPIError("A valid wiki revision is required.", { code: "invalid_wiki_revision" });
      }
      return read(`/v1/projects/${encodeURIComponent(projectID)}/wiki/${encodeURIComponent(slug)}/revisions/${revision}`, {}, signal);
    },
    createProject(input, writeOptions, signal) {
      return write("/v1/projects", input, writeOptions, signal);
    },
    updateProject(projectID, input, writeOptions, signal) {
      return replace(`/v1/projects/${encodeURIComponent(projectID)}`, input, writeOptions, signal);
    },
    createMilestone(projectID, input, writeOptions, signal) {
      return write(`/v1/projects/${encodeURIComponent(projectID)}/milestones`, input, writeOptions, signal);
    },
    updateMilestone(milestoneID, input, writeOptions, signal) {
      return replace(`/v1/milestones/${encodeURIComponent(milestoneID)}`, input, writeOptions, signal);
    },
    createTask(projectID, input, writeOptions, signal) {
      return write(`/v1/projects/${encodeURIComponent(projectID)}/tasks`, input, writeOptions, signal);
    },
    updateTask(taskID, input, writeOptions, signal) {
      return replace(`/v1/tasks/${encodeURIComponent(taskID)}`, input, writeOptions, signal);
    },
    changeTaskState(taskID, input, writeOptions, signal) {
      return write(`/v1/tasks/${encodeURIComponent(taskID)}/state`, input, writeOptions, signal);
    },
    createWikiRevision(projectID, slug, input, writeOptions, signal) {
      return write(`/v1/projects/${encodeURIComponent(projectID)}/wiki/${encodeURIComponent(slug)}/revisions`, input, writeOptions, signal);
    },
  };
}
