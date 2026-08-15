import assert from "node:assert/strict";
import test from "node:test";
import { contractFixtures } from "./fixtures.js";
import { createHTTPAdapter, fixtureAdapter } from "./adapter.js";
import { mergeCommentPages } from "./commentPages.js";

globalThis.window = { setTimeout, clearTimeout };

test("attention filters preserve typed task destinations and bounded pages", async () => {
  const result = await fixtureAdapter.readAttention({ q: "", severity: "high", source: "task", owner: "", project: "", updated_from: "2026-07-10T12:00:00Z", cursor: "", limit: 1 });
  assert.equal(result.items.length, 1);
  assert.equal(result.items[0].severity, "high");
  assert.equal(typeof result.items[0].destination.kind, "string");
  assert.ok(result.nextCursor);
  assert.equal(result.facets.owners_truncated, false);
  assert.equal(result.facets.projects_truncated, false);
});

test("people keep execution and connectivity as separate facts", async () => {
  const result = await fixtureAdapter.readPeople({ q: "", project: "", execution: "not_running", host: "", host_connected: true, cursor: "", limit: 10 });
  assert.ok(result.items.length > 0);
  assert.ok(result.items.every((item) => item.execution === "not_running" && item.connected));
});

test("project overview returns fourteen explicit activity days", async () => {
  const result = await fixtureAdapter.readProjectOverview("billing-orchestrator", { attention_limit: 3, work_limit: 4 });
  assert.equal(result.activity.days.length, 14);
  assert.equal(result.metrics.merged_pull_requests.available, false);
  assert.deepEqual(result.work.items.map((item) => [item.state, item.priority]), [
    ["in_progress", 2],
    ["in_progress", 3],
    ["blocked", 1],
    ["ready", 2],
  ], "adapter preserves canonical state order and lower numeric priority within a state");
});

function apiResponse(data, { status = 200, ok = status >= 200 && status < 300, error, requestID = "request-test" } = {}) {
  return new Response(JSON.stringify({
    ok,
    ...(ok ? { data } : { error: error || { code: "request_failed", message: "failed" } }),
    meta: { request_id: requestID, untrusted: false },
  }), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function projectsPayload() {
  return {
    total: 1,
    limit: 10,
    items: [{
      id: "team/alpha",
      name: "Team Alpha",
      status: "active",
      purpose: "Test live transport",
      active_milestone: { id: "M-1", title: "First milestone", status: "active", position: 1 },
      task_counts: { ready: 1, in_progress: 1, blocked: 0, done: 2, cancelled: 0, total: 4 },
      current_work: { id: "T-1", title: "Wire the browser", state: "in_progress", priority: 1 },
      open_tasks: 2,
      active_sessions: 1,
      last_activity: "2026-08-09T12:00:00Z",
      last_durable_activity: { kind: "task_status_changed", ref: "T-1", title: "Browser wiring began", occurred_at: "2026-08-09T12:00:00Z" },
      destination: { kind: "project", ref: "team/alpha" },
    }],
  };
}

test("HTTP adapter uses credential-free same-origin GETs and encoded queries", async () => {
  const calls = [];
  const controller = new AbortController();
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      return apiResponse(projectsPayload());
    },
  });

  const result = await adapter.readProjects({
    q: "  team & alpha  ", cursor: "cursor/+=", limit: 10,
  }, controller.signal);

  assert.equal(result.items[0].id, "team/alpha");
  assert.equal(calls.length, 1);
  const requestURL = new URL(calls[0].url, "https://commons.test");
  assert.equal(requestURL.pathname, "/v1/projects");
  assert.equal(requestURL.searchParams.get("q"), "team & alpha");
  assert.equal(requestURL.searchParams.get("cursor"), "cursor/+=");
  assert.equal(requestURL.searchParams.get("limit"), "10");
  assert.equal(calls[0].options.method, "GET");
  assert.equal(calls[0].options.credentials, "same-origin");
  assert.equal(calls[0].options.signal, controller.signal);
  assert.deepEqual(calls[0].options.headers, { Accept: "application/json" });
  assert.equal(calls[0].options.headers.Authorization, undefined);
});

test("HTTP adapter path-encodes a selected project exactly once", async () => {
  let requested = "";
  const raw = structuredClone(contractFixtures.projectOverview);
  raw.project.id = "team/alpha%2Fpilot";
  raw.project.name = "Team Alpha";
  const adapter = createHTTPAdapter({
    fetchImpl: async (url) => {
      requested = url;
      return apiResponse(raw);
    },
  });

  const result = await adapter.readProjectOverview("team/alpha%2Fpilot", {
    attention_limit: 3,
    work_limit: 4,
  });

  assert.equal(new URL(requested, "https://commons.test").pathname, "/v1/projects/team%2Falpha%252Fpilot/overview");
  assert.equal(result.project.id, "team/alpha%2Fpilot");
  assert.equal(result.activity.days.length, 14);
});

test("HTTP adapter encodes explicit false filters", async () => {
  let requested = "";
  const adapter = createHTTPAdapter({
    fetchImpl: async (url) => {
      requested = url;
      return apiResponse({ total: 0, limit: 10, items: [], facets: {
        projects: [], execution: [], hosts: [], connectivity: [],
      } });
    },
  });

  await adapter.readPeople({
    q: "", project: "", execution: "", host: "", host_connected: false,
    cursor: "", limit: 10,
  });

  assert.equal(new URL(requested, "https://commons.test").searchParams.get("host_connected"), "false");
});

test("HTTP adapter distinguishes authorization and availability failures", async () => {
  for (const scenario of [
    { status: 401, code: "unauthorized", message: "Your writing session has expired. Sign in and try again." },
    { status: 503, code: "unavailable", message: "Commons is temporarily unavailable." },
  ]) {
    const adapter = createHTTPAdapter({
      fetchImpl: async () => apiResponse(null, {
        status: scenario.status,
        ok: false,
        error: { code: scenario.code, message: "server detail" },
        requestID: "request-85",
      }),
    });
    await assert.rejects(
      adapter.readProjects({ q: "", cursor: "", limit: 10 }),
      (error) => error.status === scenario.status
        && error.code === scenario.code
        && error.requestID === "request-85"
        && error.message === scenario.message,
    );
  }
});

test("HTTP adapter rejects invalid envelopes and oversized bodies", async () => {
  const invalid = createHTTPAdapter({
    fetchImpl: async () => new Response(JSON.stringify({ projects: [] }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  });
  await assert.rejects(
    invalid.readProjects({ q: "", cursor: "", limit: 10 }),
    (error) => error.code === "invalid_envelope",
  );

  const oversized = createHTTPAdapter({
    maximumResponseBytes: 32,
    fetchImpl: async () => new Response("x".repeat(64), {
      status: 200,
      headers: { "content-length": "64", "content-type": "application/json" },
    }),
  });
  await assert.rejects(
    oversized.readProjects({ q: "", cursor: "", limit: 10 }),
    (error) => error.code === "response_too_large",
  );
});

test("HTTP adapter propagates cancellation without turning it into a network error", async () => {
  const controller = new AbortController();
  const adapter = createHTTPAdapter({
    fetchImpl: async (_url, { signal }) => new Promise((_resolve, reject) => {
      signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
    }),
  });
  const pending = adapter.readProjects({ q: "", cursor: "", limit: 10 }, controller.signal);
  controller.abort();
  await assert.rejects(pending, (error) => error.name === "AbortError");
});


test("posts feed stays compact and explicit open returns durable content", async () => {
  const feed = await fixtureAdapter.readPosts({
    q: "retry", topic: "billing-orchestrator", project: "", kind: "finding",
    created_from: "", created_to: "", cursor: "", limit: 10,
  });
  assert.equal(feed.items.length, 1);
  assert.equal(feed.items[0].id, "POST-2409");
  assert.equal(Object.hasOwn(feed.items[0], "body"), false);
  assert.equal(feed.items[0].attachments[0].kind, "github");

  const opened = await fixtureAdapter.readPost("POST-2409", {
    comments_cursor: "", comments_limit: 20,
  });
  assert.match(opened.post.body, /duplicate payout events/i);
  assert.ok(opened.post.basis);
  assert.equal(opened.comments.items.length, 5);
});

test("canonical topics are independent of the current feed page", async () => {
  const topics = await fixtureAdapter.readTopics(100);
  assert.deepEqual(topics.items.map((topic) => topic.id), ["general", "agent-evals", "billing-orchestrator"]);
  assert.equal(topics.truncated, false);

  let requested = "";
  const adapter = createHTTPAdapter({
    fetchImpl: async (url) => {
      requested = url;
      return apiResponse({ items: [{ id: "general", name: "General" }], truncated: false });
    },
  });
  const liveTopics = await adapter.readTopics(100);
  assert.equal(new URL(requested, "https://commons.test").pathname, "/v1/topics");
  assert.equal(new URL(requested, "https://commons.test").searchParams.get("limit"), "100");
  assert.equal(liveTopics.items[0].name, "General");
});

test("comment pages merge oldest-first and deduplicate overlapping records", async () => {
  const first = await fixtureAdapter.readPost("POST-2409", { comments_cursor: "", comments_limit: 2 });
  const second = await fixtureAdapter.readPost("POST-2409", { comments_cursor: first.comments.nextCursor, comments_limit: 2 });
  const overlap = { ...second.comments, items: [first.comments.items[1], ...second.comments.items] };
  const merged = mergeCommentPages(first.comments, overlap);

  assert.deepEqual(merged.items.map((comment) => comment.id), [
    ...first.comments.items.map((comment) => comment.id),
    ...second.comments.items.map((comment) => comment.id),
  ]);
  assert.equal(merged.nextCursor, second.comments.nextCursor);
});

test("HTTP posts transport keeps feed compact, path-encodes open, and posts same-origin JSON", async () => {
  const calls = [];
  const feedPayload = {
    total: 1,
    limit: 10,
    items: [{
      id: "POST/team alpha",
      title: "A durable finding",
      preview: "Bounded preview",
      topic: { id: "general", name: "General" },
      kind: "finding",
      author: { kind: "human", principal: "human:local-admin", session: "human-local-admin", handle: "alex", display_name: "Alex Lee" },
      created_at: "2026-08-09T12:00:00Z",
      comment_count: 0,
      state: "open",
      attachments: [],
      destination: { kind: "post", ref: "POST/team alpha" },
    }],
  };
  const openedPayload = {
    post: {
      ...feedPayload.items[0],
      body: "Full durable body",
      basis: "Test evidence",
      related_ref: "TASK-1",
    },
    comments: { limit: 20, items: [] },
  };
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      if (options.method === "POST") return apiResponse({ id: "POST-2", revision: 1, persisted: true });
      if (String(url).startsWith("/v1/posts/")) return apiResponse(openedPayload);
      return apiResponse(feedPayload);
    },
  });

  const feed = await adapter.readPosts({
    q: " durable & exact ", topic: "general", project: "", kind: "finding",
    created_from: "", created_to: "", cursor: "post/next", limit: 10,
  });
  const opened = await adapter.readPost("POST/team alpha", { comments_cursor: "", comments_limit: 20 });
  const created = await adapter.createPost({
    topic: "general", kind: "finding", title: "Title", body: "Body", basis: "Basis", attachments: [],
  }, { csrfToken: "csrf-test", idempotencyKey: "idempotency-test" });

  assert.equal(feed.items[0].preview, "Bounded preview");
  assert.deepEqual({
    kind: feed.items[0].author.kind,
    principal: feed.items[0].author.principal,
    displayName: feed.items[0].author.displayName,
    handle: feed.items[0].author.handle,
    session: feed.items[0].author.session,
  }, {
    kind: "human",
    principal: "human:local-admin",
    displayName: "Alex Lee",
    handle: "alex",
    session: "human-local-admin",
  });
  const feedURL = new URL(calls[0].url, "https://commons.test");
  assert.equal(feedURL.pathname, "/v1/posts");
  assert.equal(feedURL.searchParams.get("q"), "durable & exact");
  assert.equal(feedURL.searchParams.get("kind"), "finding");
  assert.equal(new URL(calls[1].url, "https://commons.test").pathname, "/v1/posts/POST%2Fteam%20alpha");
  assert.equal(opened.post.body, "Full durable body");
  assert.equal(calls[2].options.method, "POST");
  assert.equal(calls[2].options.credentials, "same-origin");
  assert.equal(calls[2].options.headers.Authorization, undefined);
  assert.equal(calls[2].options.headers["Content-Type"], "application/json");
  assert.equal(calls[2].options.headers["Idempotency-Key"], "idempotency-test");
  assert.equal(calls[2].options.headers["X-Commons-CSRF"], "csrf-test");
  assert.equal(JSON.parse(calls[2].options.body).topic, "general");
  assert.equal(created.id, "POST-2");
});

test("HTTP auth uses only same-origin cookies and validates the bounded session DTO", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      if (url === "/v1/auth/login") return apiResponse({
        authenticated: true,
        principal: { kind: "human", principal: "human:local-admin", handle: "alex", display_name: "Alex Lee" },
        csrf_token: "csrf-rotated",
      });
      return apiResponse({ authenticated: false });
    },
  });

  const initial = await adapter.readSession();
  const authenticated = await adapter.login("local-secret", "login-idempotency");
  const loggedOut = await adapter.logout("csrf-rotated", "logout-idempotency");

  assert.equal(initial.authenticated, false);
  assert.equal(authenticated.principal.displayName, "Alex Lee");
  assert.equal(authenticated.principal.principal, "human:local-admin");
  assert.equal(authenticated.principal.handle, "alex");
  assert.equal(authenticated.csrfToken, "csrf-rotated");
  assert.equal(loggedOut.authenticated, false);
  assert.equal(calls[0].url, "/v1/auth/session");
  assert.equal(calls[0].options.credentials, "same-origin");
  assert.equal(calls[1].options.headers["Idempotency-Key"], "login-idempotency");
  assert.equal(calls[1].options.headers.Authorization, undefined);
  assert.equal(JSON.parse(calls[1].options.body).secret, "local-secret");
  assert.equal(calls[2].options.headers["X-Commons-CSRF"], "csrf-rotated");
  assert.equal(calls[2].options.headers["Idempotency-Key"], "logout-idempotency");
});

test("HTTP comment and state writes preserve durable intent and append-only supersession", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      return apiResponse({ id: url === "/v1/comments" ? "C-1" : "P-1", revision: 2, persisted: true });
    },
  });
  const writeOptions = { csrfToken: "csrf-test", idempotencyKey: "mutation-idempotency" };

  await adapter.createComment({ ref: "P-1", intent: "add_evidence", body: "The check is green." }, writeOptions);
  await adapter.changePostState({ ref: "P-1", state: "superseded", superseded_by: "P-2" }, writeOptions);

  assert.equal(calls[0].url, "/v1/comments");
  assert.deepEqual(JSON.parse(calls[0].options.body), { ref: "P-1", intent: "add_evidence", body: "The check is green." });
  assert.equal(calls[0].options.headers["X-Commons-CSRF"], "csrf-test");
  assert.equal(calls[1].url, "/v1/post-states");
  assert.deepEqual(JSON.parse(calls[1].options.body), { ref: "P-1", state: "superseded", superseded_by: "P-2" });
});

test("HTTP adapter rejects malformed authenticated sessions and mutation receipts", async () => {
  const invalidSession = createHTTPAdapter({ fetchImpl: async () => apiResponse({ authenticated: true, principal: { kind: "human", display_name: "Alex" } }) });
  await assert.rejects(invalidSession.readSession(), (error) => error.code === "invalid_payload");

  const invalidMutation = createHTTPAdapter({ fetchImpl: async () => apiResponse({ id: "P-1", revision: 1, persisted: false }) });
  await assert.rejects(
    invalidMutation.createPost({ topic: "general", kind: "notice", title: "Title", body: "Body", basis: "Basis" }, { csrfToken: "csrf", idempotencyKey: "idempotency" }),
    (error) => error.code === "invalid_payload",
  );
});

function projectActivityPayload() {
  return {
    timezone: "UTC",
    start: "2026-07-27",
    end_exclusive: "2026-08-10",
    days: Array.from({ length: 14 }, (_, index) => ({
      day: `2026-${index < 5 ? "07" : "08"}-${String(index < 5 ? 27 + index : index - 4).padStart(2, "0")}`,
      count: index % 3,
    })),
  };
}

function projectTaskPayload(overrides = {}) {
  return {
    id: "TASK/core 1",
    project: "team/alpha",
    title: "Carry canonical context",
    description: "Keep durable work coherent.",
    acceptance: "The next agent can resume from canonical records.",
    state: "in_progress",
    priority: 1,
    milestone_id: "M-1",
    milestone: { id: "M-1", title: "First milestone", status: "active" },
    owner_session: "SES-AGENT-ONLY",
    dependencies: [],
    dependencies_truncated: false,
    revision: 4,
    ...overrides,
  };
}

test("Project Core reads stay bounded, preserve provenance without implying ownership, and keep bodies explicit", async () => {
  const calls = [];
  const task = projectTaskPayload();
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      const parsed = new URL(url, "https://commons.test");
      if (parsed.pathname === "/v1/projects/team%2Falpha") return apiResponse({
        project: { id: "team/alpha", name: "Team Alpha", status: "active", purpose: "Durable tests", now: "Finish Project Core", revision: 7 },
        counts: { tasks: 1, milestones: 1, wiki_pages: 1 },
        active_milestone: { id: "M-1", project: "team/alpha", title: "First milestone", status: "active", position: 1, revision: 2 },
        snapshot_at: "2026-08-10T00:15:00Z",
        activity: projectActivityPayload(),
      });
      if (parsed.pathname === "/v1/projects/team%2Falpha/tasks") return apiResponse({
        total: 1,
        limit: 20,
        next_cursor: "task-cursor",
        state_counts: { ready: 0, in_progress: 1, blocked: 0, done: 0, cancelled: 0, total: 1 },
        items: [task],
      });
      if (parsed.pathname === "/v1/tasks/TASK%2Fcore%201/events") return apiResponse({
        limit: 20,
        items: [{ id: "EV-2", kind: "task_status_changed", summary: "Work began.", from_state: "ready", to_state: "in_progress", revision: 4, actor: "Codex", session: "SES-REVIEW" }],
      });
      if (parsed.pathname === "/v1/tasks/TASK%2Fcore%201") return apiResponse({
        task,
        events: [{ id: "EV-1", kind: "task_created", summary: "Task created.", revision: 1, provenance: { kind: "historical", session: "SES-HISTORY", role: "originator", confidence: "verified", recorded_at: "2026-08-10T00:15:00Z" } }],
        events_next_cursor: "event-cursor",
      });
      if (parsed.pathname === "/v1/projects/team%2Falpha/wiki") return apiResponse({
        total: 1,
        limit: 20,
        items: [{ id: "W-1", project: "team/alpha", slug: "architecture", title: "Architecture", current_revision: 2, summary: "Canonical boundaries." }],
      });
      if (parsed.pathname === "/v1/projects/team%2Falpha/wiki/architecture/revisions") return apiResponse({
        limit: 20,
        items: [{ revision: 2, summary: "Clarified boundaries.", author_session_id: "SES-WIKI-HISTORY" }],
      });
      if (parsed.pathname === "/v1/projects/team%2Falpha/wiki/architecture") return apiResponse({
        page: { id: "W-1", project: "team/alpha", slug: "architecture", title: "Architecture", revision: 2, summary: "Canonical boundaries.", body: "# Boundaries\n\nExplicit body.", author_session_id: "SES-WIKI" },
      });
      throw new Error(`Unexpected route ${parsed.pathname}`);
    },
  });

  const project = await adapter.readProject("team/alpha");
  const tasks = await adapter.readProjectTasks("team/alpha", { state: "", milestone: "", cursor: "", limit: 20 });
  const openedTask = await adapter.readTask("TASK/core 1", 20);
  const events = await adapter.readTaskEvents("TASK/core 1", { cursor: "event-cursor", limit: 20 });
  const wiki = await adapter.readWikiPages("team/alpha", { q: " architecture ", cursor: "", limit: 20 });
  const openedWiki = await adapter.readWikiPage("team/alpha", "architecture");
  const history = await adapter.readWikiRevisions("team/alpha", "architecture", { cursor: "", limit: 20 });

  assert.equal(project.activity.days.length, 14);
  assert.equal(project.project.created, null, "unknown migrated timestamps stay unknown");
  assert.equal(tasks.items[0].updated, null);
  assert.equal(tasks.items[0].milestone.title, "First milestone");
  assert.equal(tasks.items[0].ownerSession, "SES-AGENT-ONLY");
  assert.equal(tasks.items[0].ownerProvenance.session, "SES-AGENT-ONLY", "current claim provenance remains distinct from human assignment");
  assert.equal(openedTask.events[0].provenance.kind, "historical");
  assert.equal(events.items[0].provenance.actor, "Codex");
  assert.equal(openedWiki.page.provenance.session, "SES-WIKI");
  assert.equal(history.items[0].provenance.session, "SES-WIKI-HISTORY");
  assert.equal(tasks.nextCursor, "task-cursor");
  assert.equal(openedTask.eventsNextCursor, "event-cursor");
  assert.equal(events.items[0].created, null);
  assert.equal(Object.hasOwn(wiki.items[0], "body"), false, "wiki lists remain metadata-only");
  assert.match(openedWiki.page.body, /Explicit body/);
  assert.equal(Object.hasOwn(history.items[0], "body"), false, "history cannot carry body content");
  assert.equal(new URL(calls[1].url, "https://commons.test").searchParams.get("limit"), "20");
  assert.equal(new URL(calls[4].url, "https://commons.test").searchParams.get("q"), "architecture");
});

test("Project Core writes preserve exact routes, methods, idempotency, and base revisions", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      return apiResponse({ id: "saved", revision: 8, persisted: true });
    },
  });
  const writeOptions = { csrfToken: "csrf-project", idempotencyKey: "idempotency-project" };

  await adapter.createProject({ id: "team-alpha", name: "Team Alpha", purpose: "Durable work", status: "active" }, writeOptions);
  await adapter.updateProject("team/alpha", { name: "Team Alpha", purpose: "Durable work", status: "paused", base_revision: 7 }, writeOptions);
  await adapter.updateMilestone("M/1", { title: "First milestone", status: "active", position: 1, base_revision: 2 }, writeOptions);
  await adapter.createTask("team/alpha", { title: "Canonical task", state: "ready", priority: 1, dependency_ids: ["D-1"] }, writeOptions);
  await adapter.updateTask("TASK/core 1", { title: "Canonical task", description: "Updated", acceptance: "Done", priority: 1, milestone_id: "", dependency_ids: [], base_revision: 4 }, writeOptions);
  await adapter.changeTaskState("TASK/core 1", { state: "done", basis: "Acceptance evidence passed.", base_revision: 5 }, writeOptions);
  await adapter.createWikiRevision("team/alpha", "architecture notes", { title: "Architecture", summary: "Clarified", body: "Body", base_revision: 2 }, writeOptions);

  assert.deepEqual(calls.map((call) => [call.url, call.options.method]), [
    ["/v1/projects", "POST"],
    ["/v1/projects/team%2Falpha", "PUT"],
    ["/v1/milestones/M%2F1", "PUT"],
    ["/v1/projects/team%2Falpha/tasks", "POST"],
    ["/v1/tasks/TASK%2Fcore%201", "PUT"],
    ["/v1/tasks/TASK%2Fcore%201/state", "POST"],
    ["/v1/projects/team%2Falpha/wiki/architecture%20notes/revisions", "POST"],
  ]);
  assert.equal(JSON.parse(calls[1].options.body).base_revision, 7);
  assert.equal(Object.hasOwn(JSON.parse(calls[3].options.body), "owner_session"), false);
  assert.equal(JSON.parse(calls[4].options.body).base_revision, 4);
  assert.equal(JSON.parse(calls[4].options.body).milestone_id, "", "empty optional relation explicitly clears the milestone");
  assert.deepEqual(JSON.parse(calls[4].options.body).dependency_ids, []);
  assert.deepEqual(JSON.parse(calls[5].options.body), { state: "done", basis: "Acceptance evidence passed.", base_revision: 5 });
  assert.equal(JSON.parse(calls[6].options.body).base_revision, 2);
  assert.ok(calls.every((call) => call.options.headers["Idempotency-Key"] === "idempotency-project"));
  assert.ok(calls.every((call) => call.options.headers["X-Commons-CSRF"] === "csrf-project"));
  assert.ok(calls.every((call) => call.options.headers.Authorization === undefined));
});

test("Project Core rejects task pages that exceed the dependency display bound", async () => {
  const adapter = createHTTPAdapter({
    fetchImpl: async () => apiResponse({
      total: 1,
      limit: 20,
      state_counts: { ready: 1, in_progress: 0, blocked: 0, done: 0, cancelled: 0, total: 1 },
      items: [projectTaskPayload({
        state: "ready",
        dependencies: Array.from({ length: 21 }, (_, index) => ({ id: `D-${index}`, title: `Dependency ${index}`, state: "done" })),
      })],
    }),
  });
  await assert.rejects(
    adapter.readProjectTasks("team/alpha", { state: "", milestone: "", cursor: "", limit: 20 }),
    (error) => error.code === "invalid_payload",
  );
});

test("Project Core rejects task and task-event requests above their response budgets", async () => {
  const adapter = createHTTPAdapter({ fetchImpl: async () => { throw new Error("fetch should not run"); } });
  await assert.rejects(
    adapter.readProjectTasks("team/alpha", { state: "", milestone: "", cursor: "", limit: 26 }),
    (error) => error.code === "invalid_limit" && /25/.test(error.message),
  );
  await assert.rejects(
    adapter.readTaskEvents("TASK-1", { cursor: "", limit: 51 }),
    (error) => error.code === "invalid_limit" && /50/.test(error.message),
  );
});

test("Slice 13 contributor lookup and structured writes preserve exact principal targets", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    calls.push({ url, options });
    const path = new URL(url, "https://commons.test").pathname;
    if (path === "/v1/contributors") return apiResponse({ limit: 8, items: [{ kind: "agent", principal: "SES-exact", handle: "agent-000042", session: "SES-exact", purpose: "Review evidence", host: "plumbob", project: { id: "alpha", name: "Alpha" }, project_relationship: "same_project", addressable: true, reachable: false, interpretation: "Addressable registry session; no current reachability evidence.", host_connected: false, execution: "not_running" }] });
    return apiResponse({ id: "write-1", revision: 3, persisted: true });
  }});
  const contributors = await adapter.readContributors({ q: "agent-42", project: "alpha", cursor: "", limit: 8 });
  assert.equal(contributors.items[0].session, "SES-exact");
  assert.equal(contributors.items[0].principal, "SES-exact");
  assert.equal(contributors.items[0].reachable, false);
  await adapter.createComment({ ref: "P-1", body: "plain @text and selected mention", intent: "clarify", mentions: [{ principal: "SES-exact" }] }, { csrfToken: "csrf", idempotencyKey: "comment-key" });
  await adapter.changePerspectiveScope({ ref: "P-1", scope: "commons", base_revision: 2 }, { csrfToken: "csrf", idempotencyKey: "scope-key" });
  assert.equal(new URL(calls[0].url, "https://commons.test").pathname, "/v1/contributors");
  assert.deepEqual(JSON.parse(calls[1].options.body).mentions, [{ principal: "SES-exact" }]);
  assert.deepEqual(JSON.parse(calls[2].options.body), { ref: "P-1", scope: "commons", base_revision: 2 });
});

test("Slice 12 rejects malformed contributor facts and structured mentions", async () => {
  const malformedContributor = createHTTPAdapter({ fetchImpl: async () => apiResponse({ limit: 8, items: [{ kind: "agent", principal: "SES-exact", handle: "agent-000042", session: "SES-exact", host: "plumbob", project_relationship: "same_project", addressable: true, reachable: "false", interpretation: "Addressable only.", host_connected: false, execution: "not_running" }] }) });
  await assert.rejects(
    malformedContributor.readContributors({ q: "agent", project: "alpha", cursor: "", limit: 8 }),
    (error) => error.code === "invalid_payload",
  );

  const malformedMentions = createHTTPAdapter({ fetchImpl: async () => apiResponse({
    post: {
      id: "P-1", title: "Post", body: "Body", basis: "Basis", topic: { id: "general", name: "General" }, kind: "notice",
      author: { session: "SES-author" }, created_at: "2026-08-11T00:00:00Z", comment_count: 1, state: "open", attachments: [], destination: { kind: "post", ref: "P-1" },
    },
    comments: { limit: 20, items: [{ id: "C-1", body: "Body", intent: "clarify", author: { session: "SES-author" }, created_at: "2026-08-11T00:00:00Z", mentions: "SES-spoofed" }] },
  }) });
  await assert.rejects(
    malformedMentions.readPost("P-1", { comments_cursor: "", comments_limit: 20 }),
    (error) => error.code === "invalid_payload",
  );
});

test("Slice 13 rejects typed human authors without authoritative identity fields", async () => {
  for (const author of [
    { kind: "human", display_name: "Alex Lee", session: "human-local-admin" },
    { kind: "human", principal: "human:local-admin", session: "human-local-admin" },
    { kind: "agent", principal: "SES-indexer", display_name: 42 },
    { kind: "service", principal: "service:indexer", display_name: "Indexer" },
  ]) {
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse({
      total: 1,
      limit: 10,
      items: [{
        id: "P-identity", title: "Post", preview: "Preview", topic: { id: "general", name: "General" }, kind: "notice",
        author, created_at: "2026-08-11T00:00:00Z", comment_count: 0, state: "open", attachments: [], destination: { kind: "post", ref: "P-identity" },
      }],
    }) });
    await assert.rejects(
      adapter.readPosts({ q: "", topic: "", project: "", kind: "", created_from: "", created_to: "", cursor: "", limit: 10 }),
      (error) => error.code === "invalid_payload",
    );
  }
});

test("Slice 13 notifications stay metadata-only and exact source reads precede explicit receipts", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    calls.push({ url, options });
    const path = new URL(url, "https://commons.test").pathname;
    if (path === "/v1/notifications") return apiResponse({
      items: [{
        id: "N-1",
        recipient: { kind: "human", principal: "human:local-admin", handle: "alex", display_name: "Alex Lee" },
        source: { kind: "comment", post_ref: "P-1", comment_ref: "C-9" },
        actor: { kind: "agent", principal: "SES-release", handle: "release-scout", purpose: "Release scout" },
        snippet: "@alex, can you verify the maintenance window?",
        created_at: "2026-08-11T13:52:00Z",
      }],
      next_cursor: "notification-next",
      unread_count: 1,
    });
    if (path === "/v1/comments/C-9") return apiResponse({
      post_ref: "P-1",
      comment: { id: "C-9", body: "@alex, can you verify the maintenance window?", intent: "clarify", author: { session: "SES-release", handle: "release-scout", purpose: "Release scout" }, created_at: "2026-08-11T13:52:00Z", mentions: [{ kind: "human", principal: "human:local-admin", handle: "alex", display_name: "Alex Lee" }] },
    });
    return apiResponse({ id: "N-1", revision: 0, persisted: true });
  }});

  const notifications = await adapter.readNotifications({ unread: true, cursor: "", limit: 20 });
  const source = await adapter.readCommentSource("C-9");
  await adapter.markNotificationRead({ id: "N-1" }, { csrfToken: "csrf", idempotencyKey: "read-N-1" });

  assert.equal(notifications.unreadCount, 1);
  assert.equal(notifications.items[0].source.postRef, "P-1");
  assert.equal(notifications.items[0].recipient.principal, "human:local-admin");
  assert.equal(source.postRef, "P-1");
  assert.equal(source.comment.mentions[0].principal, "human:local-admin");
  const listURL = new URL(calls[0].url, "https://commons.test");
  assert.equal(listURL.searchParams.get("unread"), "true");
  assert.equal(listURL.searchParams.get("limit"), "20");
  assert.equal(new URL(calls[1].url, "https://commons.test").pathname, "/v1/comments/C-9");
  assert.equal(calls[2].url, "/v1/notification-reads");
  assert.deepEqual(JSON.parse(calls[2].options.body), { id: "N-1" });
  assert.equal(calls[2].options.headers["X-Commons-CSRF"], "csrf");
  assert.equal(calls[2].options.headers["Idempotency-Key"], "read-N-1");
});

test("Slice 13 fixture mode keeps human identity dynamic and project lookup bounded", async () => {
  const session = await fixtureAdapter.readSession();
  const global = await fixtureAdapter.readContributors({ q: session.principal.handle, project: "", cursor: "", limit: 8 });
  const project = await fixtureAdapter.readContributors({ q: session.principal.handle, project: "billing-orchestrator", cursor: "", limit: 8 });
  const release = await fixtureAdapter.readContributors({ q: "re", project: "", cursor: "", limit: 8 });
  const notifications = await fixtureAdapter.readNotifications({ unread: true, cursor: "", limit: 20 });
  const source = await fixtureAdapter.readCommentSource(notifications.items[0].source.commentRef);

  assert.equal(global.items[0].kind, "human");
  assert.equal(global.items[0].principal, session.principal.principal);
  assert.equal(global.items[0].displayName, session.principal.displayName);
  assert.equal(project.items.some((item) => item.kind === "human"), false);
  assert.equal(release.items[0].handle, "release-scout");
  assert.equal(release.items[0].reachable, true);
  assert.equal(release.items[1].handle, "research-indexer");
  assert.equal(notifications.unreadCount, 1);
  assert.equal(source.postRef, notifications.items[0].source.postRef);
  assert.equal(source.comment.id, notifications.items[0].source.commentRef);
  assert.equal(source.comment.mentions[0].principal, session.principal.principal);
  assert.equal(source.comment.author.kind, "agent");

  const opened = await fixtureAdapter.readPost("POST-2411", { comments_cursor: "", comments_limit: 20 });
  const humanReply = opened.comments.items.find((comment) => comment.id === "COMMENT-60");
  assert.equal(humanReply.author.kind, "human");
  assert.equal(humanReply.author.principal, session.principal.principal);
  assert.equal(humanReply.author.displayName, session.principal.displayName);
});

function archaeologyPayload() {
  const member = { session_id: "SES-4168", display_name: "Integration historian", reachability: "historical_or_unknown", execution: "not_attested", authority: "provenance_only", contribution_count: 6, source_count: 4, collaboration_count: 2, demonstrated_strengths: ["Provenance design"], uncertainties: ["Current reachability was not observed"] };
  return {
    id: "ARCH-1",
    state: "completed",
    discovery: {
      state: "ready", metadata_only: true, source_roots_scanned: 4, discovered_at: "2026-08-12T12:00:00Z",
      candidates: [{
        id: "codex-commons", name: "Codex Commons", path_label: "~/codex-commons", repository_label: "codex-commons",
        last_activity_at: "2026-08-12T11:00:00Z", signals: { git: true, docs: true, codex_history: true },
        estimate: { duration_seconds_min: 240, duration_seconds_max: 480, relative_cost: "medium" },
        privacy_note: "Evidence choices govern admissible citations.", selected_by_default: false,
        sources: ["codex_metadata"], codex_thread_count: 12,
      }],
    },
    config: { selected_project_ids: ["codex-commons"], depth: "standard", sources: { git: true, docs: true, codex_history: true }, max_concurrency: 2 },
    runs: [{ id: "RUN-1", project_id: "codex-commons", state: "completed", phase_label: "Review ready", completed_units: 8, total_units: 8, outcomes_found: 1, sources_examined: 8 }],
    review: {
      batch_id: "BATCH-1",
      proposed_outcomes: [{
        id: "OUT-1", title: "Exact-session provenance", summary: "Connected durable work to its sessions.", project_id: "codex-commons", source_digest: `sha256:${"f".repeat(64)}`, source_count: 1,
        provenance: [{ source_kind: "git", source_label: `commit:${"a".repeat(40)}`, digest: `sha256:${"a".repeat(64)}`, recorded_at: "2026-08-12T11:00:00Z" }], member_sessions: [member],
      }],
      member_sessions: [member], provenance_summary: "Exact digests retained; explicit human approval is required.", can_apply: false, requires_explicit_approval: true,
    },
    capabilities: {
      project_catalog: { configured: true, available: true, mode: "codex_metadata" },
      task_launch: { configured: true, available: true, mode: "app_server_stdio" },
      discovery: { configured: true, available: true, mode: "allowlisted_metadata" },
      historian_handoff: { configured: true, available: true, mode: "exact_task_claim_report" },
      review: { configured: true, available: true, mode: "validated_manifest" },
      canonical_apply: { configured: true, available: false, mode: "preview_manifest_confirm", reason: "Review the exact task-and-evidence diff, then confirm both the source and server manifest digests." },
    },
    handoff: {
      id: "", batch_id: "BATCH-1", state: "completed", created_at: "2026-08-12T12:00:00Z", updated_at: "2026-08-12T12:30:00Z", depth: "standard", sources: { git: true, docs: true, codex_history: true }, concurrency: 2, candidate_ids: ["codex-commons"],
      tasks: [{ job_id: "JOB-1", batch_id: "BATCH-1", candidate_id: "codex-commons", project_id: "project-codex-commons", launch_id: "JOB-1", mode: "app_server_dynamic_tools", state: "completed", phase_label: "Report accepted", sources_examined: 8, duration_ms: 93000, thread_id: "019ff-task", turn_id: "turn-1", created_at: "2026-08-12T12:00:00Z", updated_at: "2026-08-12T12:30:00Z", available_actions: [] }],
      policy_attested: true,
      progress: { queued_count: 0, active_count: 0, attention_count: 0, selected_total: 1, preparing_count: 0, starting_count: 0, task_created_count: 0, claimed_count: 0, running_count: 0, report_ready_count: 0, completed_count: 1, failed_count: 0, uncertain_count: 0, updated_at: "2026-08-12T12:30:00Z" },
      allowed_actions: [],
    },
    controls: { can_start: false, can_pause: false, can_resume: false, can_cancel: false },
    revision: 7, updated_at: "2026-08-12T12:30:00Z",
  };
}

test("Project Archaeology normalizes once and transports explicit human controls", async () => {
  const calls = [];
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    calls.push({ url, options });
    return apiResponse(archaeologyPayload());
  }});
  const model = await adapter.readProjectArchaeology();
  await adapter.discoverProjectArchaeology({ csrfToken: "csrf", idempotencyKey: "arch-discover" });
  await adapter.updateProjectArchaeologyConfig({ selectedProjectIds: ["codex-commons"], depth: "deep", sources: { git: true, docs: false, codexHistory: true }, maxConcurrency: 1 }, 7, { csrfToken: "csrf", idempotencyKey: "arch-config" });
  await adapter.startProjectArchaeology(8, true, { csrfToken: "csrf", idempotencyKey: "arch-start" });
  await adapter.pauseProjectArchaeology(8, { csrfToken: "csrf", idempotencyKey: "arch-pause" });
  await adapter.resumeProjectArchaeology(9, { csrfToken: "csrf", idempotencyKey: "arch-resume" });
  await adapter.cancelProjectArchaeology(10, { csrfToken: "csrf", idempotencyKey: "arch-cancel" });
  await adapter.resolveProjectArchaeology({ jobId: "JOB-1", threadId: "019ff-task", turnId: "turn-1" }, 11, { csrfToken: "csrf", idempotencyKey: "arch-resolve" });

  assert.equal(model.discovery.metadataOnly, true);
  assert.equal(model.discovery.candidates[0].pathLabel, undefined, "raw path labels never enter the frontend model");
  assert.equal(model.discovery.candidates[0].candidateId, "codex-commons");
  assert.equal(model.discovery.candidates[0].signals.codexHistory, true);
  assert.equal(model.review.proposedOutcomes[0].memberSessions[0].sessionId, "SES-4168");
  assert.equal(model.review.proposedOutcomes[0].sourceDigest, `sha256:${"f".repeat(64)}`);
  assert.equal(model.review.memberSessions[0].demonstratedStrengths[0], "Provenance design");
  assert.equal(model.review.memberSessions[0].reachable, undefined, "membership does not synthesize reachability");
  assert.equal(model.review.batchId, "BATCH-1");
  assert.equal(model.review.batchRelation, "current");
  assert.equal(model.handoff.batchId, "BATCH-1");
  assert.equal(model.handoff.policyAttested, true);
  assert.equal(model.handoff.tasks[0].candidateId, "codex-commons");
  assert.equal(model.handoff.tasks[0].projectId, "project-codex-commons");
  assert.equal(model.handoff.tasks[0].jobId, "JOB-1");
  assert.equal(model.handoff.tasks[0].sourcesExamined, 8);
  assert.equal(model.handoff.tasks[0].durationMs, 93000);
  assert.equal(model.handoff.progress.completedCount, 1);
  assert.equal(calls[0].url, "/v1/project-archaeology");
  assert.equal(calls[0].options.method, "GET");
  assert.deepEqual(calls.slice(1).map((call) => [call.options.method, call.url]), [
    ["POST", "/v1/project-archaeology/discover"],
    ["PUT", "/v1/project-archaeology/config"],
    ["POST", "/v1/project-archaeology/start"],
    ["POST", "/v1/project-archaeology/pause"],
    ["POST", "/v1/project-archaeology/resume"],
    ["POST", "/v1/project-archaeology/cancel"],
    ["POST", "/v1/project-archaeology/resolve"],
  ]);
  assert.deepEqual(JSON.parse(calls[2].options.body), { selected_project_ids: ["codex-commons"], depth: "deep", sources: { git: true, docs: false, codex_history: true }, max_concurrency: 1, base_revision: 7 });
  assert.deepEqual(JSON.parse(calls[3].options.body), { base_revision: 8, acknowledge_large_batch: true });
  assert.equal(calls[3].options.headers["Idempotency-Key"], "arch-start");
  assert.equal(calls[2].options.headers["X-Commons-CSRF"], "csrf");
  assert.equal(calls[2].options.headers["Idempotency-Key"], "arch-config");
  assert.deepEqual(JSON.parse(calls[4].options.body), { base_revision: 8 });
  assert.deepEqual(JSON.parse(calls[7].options.body), { base_revision: 11, job_id: "JOB-1", thread_id: "019ff-task", turn_id: "turn-1", resolution: "confirmed_stopped" });
  assert.equal(calls[7].options.headers["X-Commons-CSRF"], "csrf");
  assert.equal(calls[7].options.headers["Idempotency-Key"], "arch-resolve");
});

test("Project Archaeology preserves a prior review batch without rebinding it to the current run", async () => {
  const payload = archaeologyPayload();
  payload.review.batch_id = "BATCH-PRIOR";
  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
  const model = await adapter.readProjectArchaeology();
  assert.equal(model.review.batchId, "BATCH-PRIOR");
  assert.equal(model.review.batchRelation, "prior");
  assert.equal(model.handoff.batchId, "BATCH-1");
});

test("Project Archaeology rejects native poll snapshots with impossible timestamp order", async () => {
  for (const mutate of [
    (payload) => { payload.updated_at = "2026-08-12T12:29:59Z"; },
    (payload) => { payload.handoff.updated_at = "2026-08-12T12:29:59Z"; },
    (payload) => { payload.handoff.progress.updated_at = "2026-08-12T12:30:01Z"; },
    (payload) => { payload.handoff.tasks[0].created_at = "2026-08-12T12:30:01Z"; },
  ]) {
    const payload = archaeologyPayload();
    mutate(payload);
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
    await assert.rejects(adapter.readProjectArchaeology(), (error) => error.code === "invalid_payload");
  }
});

test("Project Archaeology rejects automatic candidate selection and unreviewed manifests", async () => {
  for (const mutate of [
    (payload) => { payload.discovery.candidates[0].sources = ["filesystem_scan"]; },
    (payload) => { payload.review.requires_explicit_approval = false; },
  ]) {
    const payload = archaeologyPayload();
    mutate(payload);
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
    await assert.rejects(adapter.readProjectArchaeology(), (error) => error.code === "invalid_payload");
  }
});

function setNativeTaskState(payload, state) {
  payload.handoff.tasks[0].state = state;
  const progress = payload.handoff.progress;
  Object.assign(progress, {
    queued_count: 0, active_count: 0, attention_count: 0, selected_total: 1,
    preparing_count: 0, starting_count: 0, task_created_count: 0, claimed_count: 0,
    running_count: 0, report_ready_count: 0, completed_count: 0, failed_count: 0, uncertain_count: 0,
  });
  if (state === "queued") progress.queued_count = 1;
  else if (state === "starting") { progress.starting_count = 1; progress.active_count = 1; }
  else if (state === "active") { progress.running_count = 1; progress.active_count = 1; }
  else if (state === "cancel_requested") progress.active_count = 1;
  else if (state === "attention") progress.attention_count = 1;
  else if (state === "uncertain") progress.uncertain_count = 1;
  else if (state === "completed") progress.completed_count = 1;
  else if (["canceled", "interrupted", "failed"].includes(state)) progress.failed_count = 1;
}

test("Project Archaeology rejects unbounded native scheduler facts", async () => {
  for (const mutate of [
    (payload) => { payload.handoff.tasks[0].phase_label = "x".repeat(121); },
    (payload) => { payload.handoff.policy_attested = false; },
    (payload) => { payload.handoff.sources = { git: false, docs: false, codex_history: false }; },
    (payload) => { payload.handoff.concurrency = 0; },
    (payload) => { payload.handoff.policy_attested = false; payload.handoff.depth = ""; payload.handoff.sources = { git: false, docs: false, codex_history: false }; payload.handoff.state = "attention"; payload.handoff.tasks[0].state = "queued"; },
    (payload) => { payload.handoff.policy_attested = false; payload.handoff.depth = ""; payload.handoff.sources = { git: false, docs: false, codex_history: false }; payload.handoff.state = "attention"; payload.handoff.tasks = []; },
    (payload) => { payload.handoff.tasks[0].duration_ms = 604800001; },
    (payload) => { payload.handoff.tasks[0].state = "claimed_native"; },
    (payload) => { payload.handoff.tasks[0].batch_id = "BATCH-OTHER"; },
    (payload) => { payload.handoff.tasks[0].job_id = ""; },
    (payload) => { payload.handoff.tasks[0].candidate_id = "codex-other"; },
    (payload) => { payload.handoff.candidate_ids = []; },
    (payload) => { payload.handoff.progress.selected_total = 2; },
    (payload) => { payload.handoff.progress.completed_count = 0; },
  ]) {
    const payload = archaeologyPayload();
    mutate(payload);
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
    await assert.rejects(adapter.readProjectArchaeology(), (error) => error.code === "invalid_payload");
  }
});
test("Project Archaeology preserves quarantined native runs without inferring execution policy", async () => {
  const payload = archaeologyPayload();
  payload.handoff.policy_attested = false;
  payload.handoff.depth = "";
  payload.handoff.sources = { git: false, docs: false, codex_history: false };
  payload.handoff.state = "attention";
  setNativeTaskState(payload, "interrupted");
  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
  const model = await adapter.readProjectArchaeology();
  assert.equal(model.handoff.policyAttested, false);
  assert.equal(model.handoff.depth, "");
  assert.deepEqual(model.handoff.sources, { git: false, docs: false, codexHistory: false });
  assert.equal(model.handoff.state, "attention");
});

test("Project Archaeology preserves schema-14 terminal unattested audit history", async () => {
  for (const [batchState, taskState] of [
    ["attention", "attention"],
    ["attention", "uncertain"],
    ["completed", "completed"],
    ["canceled", "canceled"],
  ]) {
    const payload = archaeologyPayload();
    payload.handoff.policy_attested = false;
    payload.handoff.depth = "";
    payload.handoff.sources = { git: false, docs: false, codex_history: false };
    payload.handoff.state = batchState;
    setNativeTaskState(payload, taskState);
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
    const model = await adapter.readProjectArchaeology();
    assert.equal(model.handoff.policyAttested, false);
    assert.equal(model.handoff.depth, "");
    assert.deepEqual(model.handoff.sources, { git: false, docs: false, codexHistory: false });
    assert.equal(model.handoff.state, batchState);
    assert.equal(model.handoff.tasks[0].state, taskState);
  }
});

test("Project Archaeology rejects active claims in every unattested native audit state", async () => {
  for (const taskState of ["queued", "starting", "active", "report_ready", "cancel_requested"]) {
    const payload = archaeologyPayload();
    payload.handoff.policy_attested = false;
    payload.handoff.depth = "";
    payload.handoff.sources = { git: false, docs: false, codex_history: false };
    payload.handoff.state = "attention";
    payload.handoff.tasks[0].state = taskState;
    const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
    await assert.rejects(adapter.readProjectArchaeology(), (error) => error.code === "invalid_payload");
  }
});


test("Project Archaeology accepts the non-mutating initial virtual draft", async () => {
  const payload = archaeologyPayload();
  payload.id = "";
  payload.state = "draft";
  payload.discovery = { state: "idle", metadata_only: true, source_roots_scanned: 0, candidates: [] };
  payload.config = { selected_project_ids: [], depth: "standard", sources: { git: true, docs: true, codex_history: false }, max_concurrency: 2 };
  payload.runs = [];
  payload.review = null;
  payload.handoff = null;
  payload.revision = 0;
  delete payload.updated_at;
  const adapter = createHTTPAdapter({ fetchImpl: async () => apiResponse(payload) });
  const model = await adapter.readProjectArchaeology();
  assert.equal(model.id, "");
  assert.equal(model.discovery.state, "idle");
  assert.equal(model.updatedAt, null);
});

test("Project Archaeology bridges one validated outcome into exact-digest canonical apply", async () => {
  const digest = `sha256:${"a".repeat(64)}`;
  const manifestDigest = `sha256:${"b".repeat(64)}`;
  const occurredAt = "2026-08-12T13:00:00Z";
  const source = { kind: "git", stable_id: "git:commit:abc123", digest, occurred_at: occurredAt };
  const session = "019ff-historian-session";
  const calls = [];
  const previewResult = {
    batch_id: "archaeology-batch", source_digest: digest, manifest_digest: manifestDigest, collision_policy: "current_wins",
    state: "preview", applied: false,
    tasks: [{ key: "history-task-1", id: "TASK-PREVIEW-1", disposition: "created" }],
    counts: { project_thread_aliases: 0, tasks: 1, attributions: 1, events: 1, created: 1, skipped_current: 0, replayed: 0 },
  };
  const request = {
    schema_version: 1,
    batch_id: "archaeology-batch",
    source_digest: digest,
    confirm_source_digest: "",
    confirm_manifest_digest: "",
    collision_policy: "current_wins",
    project_thread_aliases: [],
    tasks: [{
      key: "history-task-1",
      priority: 0,
      title: "Preserve exact historian evidence",
      description: "Bind this completed work to the exact durable sources reviewed by the human.",
      acceptance: "The canonical task retains source identity and attribution.",
      state: "done",
      source,
      attributions: [{ session, role: "implementer", confidence: "verified", source: { ...source, kind: "codex_turn", stable_id: `thread:${session}/turn:turn-1` } }],
      events: [{ key: "history-event-1", kind: "completed", summary: "Historian verified the durable result.", session, confidence: "verified", source }],
    }],
  };
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    calls.push({ url, options });
    if (String(url) === "/v1/project-archaeology/import-preview") return apiResponse({ project_id: "codex-commons", request, preview: previewResult });
    if (String(url) === "/v1/projects/codex-commons/historical-imports/apply") return apiResponse({ ...previewResult, state: "applied", applied: true, recorded_at: "2026-08-12T14:00:00Z" });
    throw new Error(`unexpected URL ${url}`);
  }});

  const bridge = await adapter.previewProjectArchaeologyImport("OUT-1", { csrfToken: "csrf", idempotencyKey: "preview-key" });
  assert.equal(bridge.preview.collisionPolicy, "current_wins");
  assert.equal(bridge.proposal.tasks[0].title, "Preserve exact historian evidence");
  assert.equal(bridge.proposal.tasks[0].attributions[0].source.stableId, `thread:${session}/turn:turn-1`);
  assert.equal(bridge.proposal.tasks[0].events[0].summary, "Historian verified the durable result.");
  assert.equal(Object.isFrozen(bridge.request), true);
  await assert.rejects(() => adapter.applyHistoricalImport(bridge, digest, { csrfToken: "csrf", idempotencyKey: "apply-bad" }), /server manifest digest/i);
  const applied = await adapter.applyHistoricalImport(bridge, manifestDigest, { csrfToken: "csrf", idempotencyKey: "apply-key" });
  assert.equal(applied.applied, true);
  assert.deepEqual(calls.map((call) => [call.options.method, call.url]), [["POST", "/v1/project-archaeology/import-preview"], ["POST", "/v1/projects/codex-commons/historical-imports/apply"]]);
  assert.deepEqual(
    {
      source: JSON.parse(calls[1].options.body).confirm_source_digest,
      manifest: JSON.parse(calls[1].options.body).confirm_manifest_digest,
    },
    { source: digest, manifest: manifestDigest },
  );
});

test("Project Archaeology consumes bounded catalog, history, detail, and installation status contracts", async () => {
  const payload = archaeologyPayload();
  const summary = {
    batch_id: "BATCH-1", state: "completed", mode: "app_server_dynamic_tools", depth: "standard",
    sources: { git: true, docs: true, codex_history: true }, concurrency: 2, selected_total: 1,
    queued_count: 0, active_count: 0, completed_count: 1, attention_count: 0, has_report: true,
    created_at: "2026-08-12T12:00:00Z", updated_at: "2026-08-12T12:30:00Z",
  };
  const calls = [];
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    const parsed = new URL(String(url), "https://commons.example");
    calls.push({ parsed, options });
    if (parsed.pathname === "/v1/project-archaeology/catalog") return apiResponse({ items: payload.discovery.candidates, next_cursor: "cursor-2", total: 101 });
    if (parsed.pathname === "/v1/project-archaeology/batches/BATCH-1/outcomes") return apiResponse({ items: payload.review.proposed_outcomes, next_cursor: "" });
    if (parsed.pathname === "/v1/project-archaeology/batches/BATCH-1") return apiResponse({ ...summary, tasks: payload.handoff.tasks.map((task) => ({ ...task, project_name: "Archived Codex Commons" })), review: { ...payload.review, proposed_outcomes: [] }, outcomes_next_cursor: "opaque-5" });
    if (parsed.pathname === "/v1/project-archaeology/batches") return apiResponse({ items: [summary], next_cursor: "older" });
    if (parsed.pathname === "/v1/installation-status") return apiResponse({
      service: { version: "dogfood.15" },
      database: { schema_version: 15 },
      codex: { configured: true, available: true, version: "0.147.0", account_state: "signed_in", compatibility_status: "compatible", compatibility_checked_at: "2026-08-12T12:20:00Z", session_revocation_pending: false },
      archaeology: { catalog_completed_at: "2026-08-12T12:00:00Z", active_count: 0, uncertain_count: 0 },
      backup: { last_verified_at: "2026-08-12T11:00:00Z", status: "verified" },
      reconciliation: { last_at: "2026-08-12T12:25:00Z", status: "healthy" },
      evidence: { completed_historians: 12, failed_historians: 1, uncertain_historians: 0, distinct_projects: 7, reports_received: 11, lost_reports: 0, reviewed_imports: 3, cancellations: 2, report_recovery: { status: "verified", violations: 0, checked_at: "2026-08-12T10:10:00Z" }, duplicate_launch_check: { status: "unknown", violations: 0 }, repository_immutability: { status: "verified", violations: 0, checked_at: "2026-08-12T10:15:00Z" }, canonical_immutability: { status: "attention", violations: 1, checked_at: "2026-08-12T10:20:00Z" }, restore_drill: { status: "verified", last_verified_at: "2026-08-12T10:00:00Z" }, beta_prerequisites_met: false },
    });
    throw new Error(`unexpected URL ${url}`);
  }});
  const catalog = await adapter.readProjectArchaeologyCatalog({ cursor: "cursor-1", limit: 100, q: "codex", sort: "tasks" });
  const history = await adapter.readProjectArchaeologyBatches({ cursor: "", limit: 20 });
  const detail = await adapter.readProjectArchaeologyBatch("BATCH-1");
  const outcomes = await adapter.readProjectArchaeologyBatchOutcomes("BATCH-1", detail.outcomesNextCursor);
  const status = await adapter.readInstallationStatus();
  assert.equal(catalog.total, 101);
  assert.equal(catalog.nextCursor, "cursor-2");
  assert.equal(calls[0].parsed.searchParams.get("limit"), "100");
  assert.equal(calls[0].parsed.searchParams.get("cursor"), "cursor-1");
  assert.equal(calls[0].parsed.searchParams.get("sort"), "tasks");
  assert.equal(calls[0].parsed.searchParams.get("q"), "codex");
  assert.equal(history.items[0].hasReport, true);
  assert.equal(history.nextCursor, "older");
  assert.equal(detail.tasks[0].jobId, "JOB-1");
  assert.equal(detail.tasks[0].projectName, "Archived Codex Commons");
  assert.equal(detail.review.batchId, "BATCH-1");
  assert.equal(detail.review.proposedOutcomes.length, 0);
  assert.equal(detail.outcomesNextCursor, "opaque-5");
  assert.equal(outcomes.items[0].id, "OUT-1");
  assert.equal(outcomes.nextCursor, "");
  assert.equal(calls[3].parsed.searchParams.get("cursor"), "opaque-5");
  assert.equal(status.database.schemaVersion, 15);
  assert.equal(status.backup.status, "verified");
  assert.equal(status.reconciliation.status, "healthy");
  assert.equal(status.codex.compatibilityStatus, "compatible");
  assert.equal(status.codex.sessionRevocationPending, false);
  assert.equal(status.evidence.completedHistorians, 12);
  assert.equal(status.evidence.reportsReceived, 11);
  assert.equal(status.evidence.duplicateLaunchCheck.status, "unknown");
  assert.equal(status.evidence.canonicalImmutability.violations, 1);
  assert.equal(status.evidence.betaPrerequisitesMet, false);
  assert.equal(status.evidence.restoreDrill.status, "verified");
});

function selectedImportProject(index, applied = false) {
  const number = String(index).padStart(2, "0");
  const digest = `sha256:${"a".repeat(64)}`;
  const manifestDigest = `sha256:${"b".repeat(64)}`;
  const source = { kind: "git", stable_id: `git:commit:${number}`, digest, occurred_at: "2026-08-12T13:00:00Z" };
  const session = `historian-session-${number}`;
  const key = `history-task-${number}`;
  const batch = `history-batch-${number}`;
  return {
    outcome_id: `OUT-${number}`,
    project_id: `project-${number}`,
    request: {
      schema_version: 1, batch_id: batch, source_digest: digest, confirm_source_digest: "", confirm_manifest_digest: "",
      collision_policy: "current_wins", project_thread_aliases: [],
      tasks: [{
        key, priority: 0, title: `Historical task ${number}`, description: "Exact evidence-bound history.", acceptance: "The reviewed record is durable.", state: "done", source,
        attributions: [{ session, role: "implementer", confidence: "verified", source: { ...source, kind: "codex_turn", stable_id: `thread:${session}/turn:1` } }],
        events: [{ key: `event-${number}`, kind: "completed", summary: "Verified completion.", session, confidence: "verified", source }],
      }],
    },
    preview: {
      batch_id: batch, source_digest: digest, manifest_digest: manifestDigest, collision_policy: "current_wins",
      state: applied ? "applied" : "preview", applied,
      ...(applied ? { recorded_at: "2026-08-12T14:00:00Z" } : {}),
      tasks: [{ key, id: `TASK-${number}`, disposition: "created" }],
      counts: { project_thread_aliases: 0, tasks: 1, attributions: 1, events: 1, created: 1, skipped_current: 0, replayed: 0 },
    },
  };
}

test("selected preview canonicalizes reverse order and traverses 31 projects through bounded exact-diff pages", async () => {
  const selectionDigest = `sha256:${"c".repeat(64)}`;
  const manifestDigest = `sha256:${"d".repeat(64)}`;
  const projects = Array.from({ length: 31 }, (_, index) => selectedImportProject(index + 1));
  const requested = projects.map((project) => project.outcome_id).reverse();
  const canonical = [...requested].sort();
  const calls = [];
  const reviewSessionToken = "s".repeat(43);
  const reviewCompletionToken = "c".repeat(43);
  const adapter = createHTTPAdapter({ fetchImpl: async (url, options) => {
    calls.push({ url: String(url), options });
    if (String(url).includes("/import-apply")) return apiResponse({ batch_id: "BATCH-SELECTED", outcome_ids: canonical, selection_digest: selectionDigest, manifest_digest: manifestDigest, applied: true, audit_id: "AUDIT-SELECTED" });
    const pageProjects = [...projects].sort((left, right) => left.outcome_id.localeCompare(right.outcome_id));
    const cursor = new URL(String(url), "https://commons.example").searchParams.get("cursor");
    const offset = cursor ? Number(cursor) : 0;
    if (String(url).includes("/import-preview")) return apiResponse({ batch_id: "BATCH-SELECTED", outcome_ids: canonical, selection_digest: selectionDigest, manifest_digest: manifestDigest, projects: pageProjects.slice(offset, offset + 5), review_session_token: reviewSessionToken, review_expires_at: "2026-08-12T14:30:00Z", ...(offset + 5 < pageProjects.length ? { next_cursor: String(offset + 5) } : { review_completion_token: reviewCompletionToken }) });
    throw new Error(`unexpected URL ${url}`);
  }});
  let bridge = await adapter.previewProjectArchaeologyBatchImport("BATCH-SELECTED", requested, { csrfToken: "csrf", idempotencyKey: "selected-preview" });
  assert.deepEqual(JSON.parse(calls[0].options.body).outcome_ids, canonical);
  await assert.rejects(() => adapter.applyProjectArchaeologyBatchImport(bridge, manifestDigest, true, { csrfToken: "csrf", idempotencyKey: "selected-apply-early" }), (error) => error.code === "manifest_confirmation_required");
  let pageNumber = 0;
  while (bridge.nextCursor) bridge = await adapter.previewProjectArchaeologyBatchImportPage(bridge, bridge.nextCursor, { csrfToken: "csrf", idempotencyKey: `selected-preview-page-${++pageNumber}` });
  assert.equal(bridge.projects.length, 31);
  assert.equal(bridge.proposal.tasks.length, 31);
  assert.equal(bridge.nextCursor, "");
  assert.deepEqual(bridge.outcomeIds, canonical);
  assert.equal(calls.length, 7);
  assert.equal(bridge.reviewCompletionToken, reviewCompletionToken);
  assert.equal(new URL(calls.at(-1).url, "https://commons.example").searchParams.get("cursor"), "30");
  assert.equal(calls.every((call) => JSON.parse(call.options.body).outcome_ids.join() === canonical.join()), true);
  assert.equal(calls.slice(1).every((call) => JSON.parse(call.options.body).review_session_token === reviewSessionToken), true);
  assert.equal(new Set(calls.slice(0, 7).map((call) => call.options.headers["Idempotency-Key"])).size, 7);
  const applied = await adapter.applyProjectArchaeologyBatchImport(bridge, manifestDigest, true, { csrfToken: "csrf", idempotencyKey: "selected-apply" });
  assert.equal(applied.auditId, "AUDIT-SELECTED");
  assert.equal(JSON.parse(calls.at(-1).options.body).review_completion_token, reviewCompletionToken);
  await assert.rejects(() => adapter.previewProjectArchaeologyBatchImport("BATCH-SELECTED", ["OUT-01", "OUT-01"], { csrfToken: "csrf", idempotencyKey: "duplicate" }), (error) => error.code === "invalid_archaeology_selection");
  assert.equal(calls.length, 8, "duplicate selections are rejected before transport");
});

test("selected preview rejects a digest change on a later server-attested page", async () => {
  const selectionDigest = `sha256:${"c".repeat(64)}`;
  const manifestDigest = `sha256:${"d".repeat(64)}`;
  const changedManifest = `sha256:${"e".repeat(64)}`;
  const projects = Array.from({ length: 6 }, (_, index) => selectedImportProject(index + 1));
  const outcomeIDs = projects.map((project) => project.outcome_id);
  const sessionToken = "s".repeat(43);
  const adapter = createHTTPAdapter({ fetchImpl: async (url) => {
    const page = String(url).includes("import-preview-page");
    return apiResponse({
      batch_id: "BATCH-CHANGED", outcome_ids: outcomeIDs, selection_digest: selectionDigest,
      manifest_digest: page ? changedManifest : manifestDigest,
      projects: page ? projects.slice(5) : projects.slice(0, 5),
      review_session_token: sessionToken, review_expires_at: "2026-08-12T14:30:00Z",
      ...(page ? { review_completion_token: "c".repeat(43) } : { next_cursor: "5" }),
    });
  }});
  const bridge = await adapter.previewProjectArchaeologyBatchImport("BATCH-CHANGED", outcomeIDs, { csrfToken: "csrf", idempotencyKey: "changed-first" });
  await assert.rejects(() => adapter.previewProjectArchaeologyBatchImportPage(bridge, bridge.nextCursor, { csrfToken: "csrf", idempotencyKey: "changed-next" }), (error) => error.code === "archaeology_preview_changed");
});
