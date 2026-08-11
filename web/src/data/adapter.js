import { ATTACHMENT_KINDS, ATTENTION_SEVERITIES, COMMENT_INTENTS, EXECUTION_STATES, MAX_BROWSE_LIMIT, MAX_OVERVIEW_LIMIT, POST_KINDS, POST_STATES } from "../contracts/commons.js";
import { contractFixtures } from "./fixtures.js";
import { postFixtures } from "./postFixtures.js";
import { createProjectCoreHTTPMethods, projectCoreFixtureMethods } from "./projectCoreAdapter.js";
import { normalizeProvenance } from "./provenance.js";
import { CommonsAPIError, createHTTPTransport } from "./transport.js";

function wait(value, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason || new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = globalThis.setTimeout(() => resolve(structuredClone(value)), 110);
    signal?.addEventListener("abort", () => {
      globalThis.clearTimeout(timer);
      reject(signal.reason || new DOMException("Aborted", "AbortError"));
    }, { once: true });
  });
}

function timestampLabel(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) throw new TypeError("invalid timestamp");
  const delta = Math.max(0, Date.now() - date.getTime());
  const minutes = Math.floor(delta / 60000);
  const relative = minutes < 1 ? "Just now" : minutes < 60 ? `${minutes}m ago` : minutes < 1440 ? `${Math.floor(minutes / 60)}h ago` : `${Math.floor(minutes / 1440)}d ago`;
  return {
    iso: date.toISOString(),
    relative,
    absolute: new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: "UTC" }).format(date),
  };
}

function page(records, cursor, limit) {
  const offset = cursor ? Number(cursor.split(":").at(-1)) || 0 : 0;
  const items = records.slice(offset, offset + limit);
  const nextOffset = offset + items.length;
  return { items, nextCursor: nextOffset < records.length ? `fixture:${nextOffset}` : "" };
}

function normalizeAttention(record) {
  record = requireRecord(record);
  if (!ATTENTION_SEVERITIES.includes(record.severity)) throw invalidPayload();
  let destination = null;
  if (record.destination != null) {
    const rawDestination = requireRecord(record.destination);
    destination = { kind: requireString(rawDestination.kind), ref: requireString(rawDestination.ref) };
  }
  return {
    id: requireString(record.id),
    severity: record.severity,
    title: requireString(record.title),
    project: record.project_name || record.project || "—",
    source: { kind: requireString(record.source_kind), ref: requireString(record.source_ref) },
    owner: record.owner || "Unassigned",
    ownerSession: record.owner || "",
    nextAction: requireString(record.next_action),
    updated: timestampLabel(record.updated_at),
    destination,
    untrusted: Boolean(record.untrusted),
  };
}

function invalidPayload() {
  return new CommonsAPIError("Commons returned an invalid response.", { code: "invalid_payload" });
}

function requireRecord(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw invalidPayload();
  return value;
}

function requireString(value) {
  if (typeof value !== "string" || !value) throw invalidPayload();
  return value;
}

function requireInteger(value, minimum = 0) {
  if (!Number.isInteger(value) || value < minimum) throw invalidPayload();
  return value;
}

function requirePage(data, maximum) {
  data = requireRecord(data);
  const limit = requireInteger(data.limit, 1);
  if (limit > maximum || !Array.isArray(data.items) || data.items.length > limit) throw invalidPayload();
  return data;
}

function normalizeFacetList(value) {
  if (!Array.isArray(value)) throw invalidPayload();
  return value.map((facet) => {
    facet = requireRecord(facet);
    return {
      value: requireString(facet.value),
      label: typeof facet.label === "string" && facet.label ? facet.label : facet.value,
      count: requireInteger(facet.count),
    };
  });
}

function normalizeAttentionPage(data) {
  data = requirePage(data, MAX_BROWSE_LIMIT);
  const facets = requireRecord(data.facets);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "",
    items: data.items.map((item) => {
      item = requireRecord(item);
      if (!ATTENTION_SEVERITIES.includes(item.severity)) throw invalidPayload();
      requireString(item.id);
      requireString(item.title);
      requireString(item.source_kind);
      requireString(item.source_ref);
      requireString(item.next_action);
      return normalizeAttention(item);
    }),
    facets: {
      sources: normalizeFacetList(facets.sources),
      owners: normalizeFacetList(facets.owners),
      severities: normalizeFacetList(facets.severities),
      projects: normalizeFacetList(facets.projects),
      owners_truncated: Boolean(facets.owners_truncated),
      projects_truncated: Boolean(facets.projects_truncated),
    },
  };
}

function normalizeProjectsPage(data) {
  data = requirePage(data, MAX_BROWSE_LIMIT);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "",
    items: data.items.map((item) => {
      item = requireRecord(item);
      const destination = requireRecord(item.destination);
      return {
        id: requireString(item.id),
        name: requireString(item.name),
        purpose: requireString(item.purpose),
        currentWork: item.current_work?.title || "No active work",
        openTasks: requireInteger(item.open_tasks),
        activeSessions: requireInteger(item.active_sessions),
        lastActivity: item.last_activity ? timestampLabel(item.last_activity) : null,
        destination: { kind: requireString(destination.kind), ref: requireString(destination.ref) },
      };
    }),
  };
}

function normalizePeoplePage(data) {
  data = requirePage(data, MAX_BROWSE_LIMIT);
  const facets = requireRecord(data.facets);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "",
    facets: {
      projects: normalizeFacetList(facets.projects),
      execution: normalizeFacetList(facets.execution),
      hosts: normalizeFacetList(facets.hosts),
      connectivity: normalizeFacetList(facets.connectivity),
    },
    items: data.items.map((item) => {
      item = requireRecord(item);
      if (!EXECUTION_STATES.includes(item.execution)) throw invalidPayload();
      return {
        session: requireString(item.session),
        actor: item.actor || item.session,
        purpose: item.purpose || "Purpose not reported",
        project: item.project_name || item.project || "No project",
        execution: item.execution,
        host: requireString(item.host),
        connected: Boolean(item.host_connected),
        updated: timestampLabel(item.last_activity),
        loaded: item.loaded || "Nothing reported",
      };
    }),
  };
}

function normalizeOverview(data) {
  data = requireRecord(data);
  const project = requireRecord(data.project);
  const activity = requireRecord(data.activity);
  const metrics = requireRecord(data.metrics);
  const attention = requirePage(data.needs_attention, MAX_OVERVIEW_LIMIT);
  const work = requirePage(data.current_work, MAX_OVERVIEW_LIMIT);
  const mergedPullRequests = requireRecord(metrics.merged_pull_requests);
  if (!Array.isArray(activity.days) || activity.days.length !== 14 || typeof mergedPullRequests.available !== "boolean") throw invalidPayload();
  if (mergedPullRequests.available) requireInteger(mergedPullRequests.count);
  return {
    project: {
      ...project,
      id: requireString(project.id),
      name: requireString(project.name),
      purpose: requireString(project.purpose),
      revision: requireInteger(project.revision),
    },
    activity: {
      ...activity,
      timezone: requireString(activity.timezone),
      days: activity.days.map((day) => {
        day = requireRecord(day);
        return { day: requireString(day.day), count: requireInteger(day.count) };
      }),
    },
    metrics: {
      attention_total: requireInteger(metrics.attention_total),
      attention_high: requireInteger(metrics.attention_high),
      open_work: requireInteger(metrics.open_work),
      merged_pull_requests: mergedPullRequests,
      active_sessions: requireInteger(metrics.active_sessions),
    },
    attention: {
      ...attention,
      total: requireInteger(attention.total),
      items: attention.items.map((item) => {
        item = requireRecord(item);
        if (!ATTENTION_SEVERITIES.includes(item.severity)) throw invalidPayload();
        return normalizeAttention(item);
      }),
    },
    work: {
      ...work,
      total: requireInteger(work.total),
      items: work.items.map((item) => {
        item = requireRecord(item);
        const target = requireRecord(item.target);
        return {
          ...item,
          id: requireString(item.id),
          title: requireString(item.title),
          state: requireString(item.state),
          priority: requireInteger(item.priority),
          target: { kind: requireString(target.kind), ref: requireString(target.ref) },
          updated: item.updated_at ? timestampLabel(item.updated_at) : null,
        };
      }),
    },
    snapshot: timestampLabel(data.snapshot_at),
  };
}

function normalizeAttachment(value) {
  value = requireRecord(value);
  if (!ATTACHMENT_KINDS.includes(value.kind)) throw invalidPayload();
  const url = requireString(value.url);
  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    throw invalidPayload();
  }
  if (parsed.protocol !== "https:") throw invalidPayload();
  return {
    kind: value.kind,
    url,
    title: typeof value.title === "string" && value.title ? value.title : parsed.hostname,
  };
}

function normalizeTopic(value) {
  value = requireRecord(value);
  return { id: requireString(value.id), name: requireString(value.name) };
}

function normalizeTopics(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || data.items.length > 100 || typeof data.truncated !== "boolean") throw invalidPayload();
  return {
    items: data.items.map((value) => {
      value = requireRecord(value);
      return {
        id: requireString(value.id),
        name: requireString(value.name),
        projectID: value.project_id == null ? "" : requireString(value.project_id),
      };
    }),
    truncated: data.truncated,
  };
}

function normalizeAuthor(value) {
  value = requireRecord(value);
  return {
    session: requireString(value.session),
    purpose: typeof value.purpose === "string" ? value.purpose : "",
    actor: typeof value.actor === "string" ? value.actor : "",
    provenance: normalizeProvenance(value.provenance, { actor: value.actor, session: value.session, purpose: value.purpose }),
  };
}

function normalizeSession(value) {
  value = requireRecord(value);
  if (typeof value.authenticated !== "boolean") throw invalidPayload();
  if (!value.authenticated) {
    return { authenticated: false, principal: null, csrfToken: "" };
  }
  const principal = requireRecord(value.principal);
  if (principal.kind !== "human") throw invalidPayload();
  return {
    authenticated: true,
    principal: { kind: "human", displayName: requireString(principal.display_name) },
    csrfToken: requireString(value.csrf_token),
  };
}

function normalizeMutation(value) {
  value = requireRecord(value);
  if (value.persisted !== true) throw invalidPayload();
  return {
    id: requireString(value.id),
    revision: requireInteger(value.revision),
    persisted: true,
  };
}

function normalizePostSummary(value, bodyFallback = "") {
  value = requireRecord(value);
  if (!POST_KINDS.includes(value.kind) || !POST_STATES.includes(value.state)) throw invalidPayload();
  if (!Array.isArray(value.attachments) || value.attachments.length > 8) throw invalidPayload();
  const destination = requireRecord(value.destination);
  return {
    id: requireString(value.id),
    title: requireString(value.title),
    preview: typeof value.preview === "string" ? value.preview : bodyFallback.slice(0, 320),
    topic: normalizeTopic(value.topic),
    project: value.project == null ? null : normalizeTopic(value.project),
    kind: value.kind,
    author: normalizeAuthor(value.author),
    created: timestampLabel(value.created_at),
    commentCount: requireInteger(value.comment_count),
    state: value.state,
    supersededBy: typeof value.superseded_by === "string" ? value.superseded_by : "",
    attachments: value.attachments.map(normalizeAttachment),
    destination: { kind: requireString(destination.kind), ref: requireString(destination.ref) },
  };
}

function normalizePostsPage(data) {
  data = requirePage(data, 50);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "",
    items: data.items.map((item) => normalizePostSummary(item)),
  };
}

function normalizeOpenedPost(data) {
  data = requireRecord(data);
  const rawPost = requireRecord(data.post);
  const body = requireString(rawPost.body);
  const comments = requirePage(data.comments, 20);
  return {
    post: {
      ...normalizePostSummary(rawPost, body),
      body,
      basis: requireString(rawPost.basis),
      relatedRef: typeof rawPost.related_ref === "string" ? rawPost.related_ref : "",
    },
    comments: {
      limit: comments.limit,
      nextCursor: typeof comments.next_cursor === "string" ? comments.next_cursor : "",
      items: comments.items.map((value) => {
        value = requireRecord(value);
        if (!COMMENT_INTENTS.includes(value.intent)) throw invalidPayload();
        return {
          id: requireString(value.id),
          body: requireString(value.body),
          intent: value.intent,
          author: normalizeAuthor(value.author),
          created: timestampLabel(value.created_at),
        };
      }),
    },
  };
}

function safelyNormalize(normalize, data) {
  try {
    return normalize(data);
  } catch (error) {
    if (error instanceof CommonsAPIError) throw error;
    throw invalidPayload();
  }
}

export function createHTTPAdapter(options) {
  const transport = createHTTPTransport(options);
  return {
    async readSession(signal) {
      return safelyNormalize(normalizeSession, await transport.readSession(signal));
    },
    async login(secret, idempotencyKey, signal) {
      return safelyNormalize(normalizeSession, await transport.login(secret, idempotencyKey, signal));
    },
    async logout(csrfToken, idempotencyKey, signal) {
      return safelyNormalize(normalizeSession, await transport.logout(csrfToken, idempotencyKey, signal));
    },
    async readPosts(query, signal) {
      return safelyNormalize(normalizePostsPage, await transport.readPosts(query, signal));
    },
    async readTopics(limit, signal) {
      return safelyNormalize(normalizeTopics, await transport.readTopics(limit, signal));
    },
    async readPost(postID, query, signal) {
      return safelyNormalize(normalizeOpenedPost, await transport.readPost(postID, query, signal));
    },
    async createPost(input, writeOptions, signal) {
      return safelyNormalize(normalizeMutation, await transport.createPost(input, writeOptions, signal));
    },
    async createComment(input, writeOptions, signal) {
      return safelyNormalize(normalizeMutation, await transport.createComment(input, writeOptions, signal));
    },
    async changePostState(input, writeOptions, signal) {
      return safelyNormalize(normalizeMutation, await transport.changePostState(input, writeOptions, signal));
    },
    async readAttention(query, signal) {
      return safelyNormalize(normalizeAttentionPage, await transport.readAttention(query, signal));
    },
    async readProjects(query, signal) {
      return safelyNormalize(normalizeProjectsPage, await transport.readProjects(query, signal));
    },
    async readPeople(query, signal) {
      return safelyNormalize(normalizePeoplePage, await transport.readPeople(query, signal));
    },
    async readProjectOverview(projectID, query, signal) {
      return safelyNormalize(normalizeOverview, await transport.readProjectOverview(projectID, query, signal));
    },
    ...createProjectCoreHTTPMethods(transport),
  };
}

export const fixtureAdapter = {
  async readSession(signal) {
    return wait({ authenticated: true, principal: { kind: "human", displayName: "Alex Lee" }, csrfToken: "fixture-csrf" }, signal);
  },
  async login(_secret, _idempotencyKey, signal) {
    return wait({ authenticated: true, principal: { kind: "human", displayName: "Alex Lee" }, csrfToken: "fixture-csrf" }, signal);
  },
  async logout(_csrfToken, _idempotencyKey, signal) {
    return wait({ authenticated: false, principal: null, csrfToken: "" }, signal);
  },
  async readPosts(query, signal) {
    let records = postFixtures.feed;
    if (query.topic) records = records.filter((item) => item.topic.id === query.topic);
    if (query.project) records = records.filter((item) => item.project?.id === query.project);
    if (query.kind) records = records.filter((item) => item.kind === query.kind);
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.title} ${item.preview} ${item.topic.name}`.toLowerCase().includes(term));
    }
    const result = page(records, query.cursor, query.limit);
    return wait(normalizePostsPage({ total: records.length, limit: query.limit, next_cursor: result.nextCursor, items: result.items }), signal);
  },

  async readTopics(limit = 100, signal) {
    const values = new Map();
    for (const item of postFixtures.feed) {
      if (item.topic.id !== "general") values.set(item.topic.id, {
        id: item.topic.id, name: item.topic.name, projectID: item.project?.id || "",
      });
    }
    const items = [
      { id: "general", name: "General" },
      ...[...values.values()].sort((left, right) => left.name.localeCompare(right.name, undefined, { sensitivity: "base" }) || left.id.localeCompare(right.id)),
    ];
    return wait({ items: items.slice(0, limit).map((item) => ({ ...item, projectID: item.projectID || "" })), truncated: items.length > limit }, signal);
  },

  async readPost(postID, query, signal) {
    const post = postFixtures.opened[postID];
    if (!post) throw new CommonsAPIError("The requested Commons record was not found.", { code: "not_found", status: 404 });
    const result = page(post.comments, query.comments_cursor, query.comments_limit);
    return wait(normalizeOpenedPost({
      post,
      comments: { limit: query.comments_limit, items: result.items, next_cursor: result.nextCursor },
    }), signal);
  },

  async createPost(input, _writeOptions, signal) {
    return wait({ id: `fixture-${input.kind}-${Date.now()}`, revision: 0, persisted: true }, signal);
  },
  async createComment(input, _writeOptions, signal) {
    return wait({ id: `fixture-comment-${input.intent}-${Date.now()}`, revision: 0, persisted: true }, signal);
  },
  async changePostState(input, _writeOptions, signal) {
    return wait({ id: input.ref, revision: 0, persisted: true }, signal);
  },

  async readAttention(query, signal) {
    let records = contractFixtures.attention.items;
    if (query.source) records = records.filter((item) => item.source_kind === query.source);
    if (query.owner) records = records.filter((item) => item.owner === query.owner);
    if (query.severity) records = records.filter((item) => item.severity === query.severity);
    if (query.project) records = records.filter((item) => item.project === query.project);
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.title} ${item.id} ${item.project} ${item.source_ref}`.toLowerCase().includes(term));
    }
    if (query.updated_from) records = records.filter((item) => Date.parse(item.updated_at) >= Date.parse(query.updated_from));
    if (query.updated_to) records = records.filter((item) => Date.parse(item.updated_at) <= Date.parse(query.updated_to));
    const result = page(records, query.cursor, query.limit);
    return wait({
      total: records.length,
      limit: query.limit,
      items: result.items.map(normalizeAttention),
      nextCursor: result.nextCursor,
      facets: structuredClone(contractFixtures.attention.facets),
    }, signal);
  },

  async readProjects(query, signal) {
    let records = contractFixtures.projects.items;
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.name} ${item.purpose} ${item.current_work?.title || ""}`.toLowerCase().includes(term));
    }
    const result = page(records, query.cursor, query.limit);
    return wait({
      total: records.length,
      limit: query.limit,
      nextCursor: result.nextCursor,
      items: result.items.map((record) => ({
        id: record.id,
        name: record.name,
        purpose: record.purpose,
        currentWork: record.current_work?.title || "No active work",
        openTasks: record.open_tasks,
        activeSessions: record.active_sessions,
        lastActivity: record.last_activity ? timestampLabel(record.last_activity) : null,
        destination: record.destination,
      })),
    }, signal);
  },

  async readPeople(query, signal) {
    let records = contractFixtures.people.items;
    if (query.project) records = records.filter((item) => item.project === query.project);
    if (query.execution) records = records.filter((item) => item.execution === query.execution);
    if (query.host) records = records.filter((item) => item.host === query.host);
    if (query.host_connected != null) records = records.filter((item) => item.host_connected === query.host_connected);
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.session} ${item.actor} ${item.purpose} ${item.project} ${item.host}`.toLowerCase().includes(term));
    }
    const result = page(records, query.cursor, query.limit);
    return wait({
      total: records.length,
      limit: query.limit,
      nextCursor: result.nextCursor,
      facets: structuredClone(contractFixtures.people.facets),
      items: result.items.map((record) => ({
        session: record.session,
        actor: record.actor || record.session,
        purpose: record.purpose || "Purpose not reported",
        project: record.project_name || record.project || "No project",
        execution: record.execution,
        host: record.host,
        connected: record.host_connected,
        updated: timestampLabel(record.last_activity),
        loaded: record.loaded || "Nothing reported",
      })),
    }, signal);
  },

  async readProjectOverview(_projectID, _query, signal) {
    const record = contractFixtures.projectOverview;
    return wait({
      project: record.project,
      activity: record.activity,
      metrics: record.metrics,
      attention: {
        ...record.needs_attention,
        items: record.needs_attention.items.map(normalizeAttention),
      },
      work: {
        ...record.current_work,
        items: record.current_work.items.map((item) => ({ ...item, updated: item.updated_at ? timestampLabel(item.updated_at) : null })),
      },
      snapshot: timestampLabel(record.snapshot_at),
    }, signal);
  },
  ...projectCoreFixtureMethods,
};

export const httpAdapter = createHTTPAdapter();

const configuredMode = import.meta.env?.VITE_COMMONS_DATA_MODE || "http";
if (configuredMode !== "http" && configuredMode !== "fixture") {
  throw new Error("VITE_COMMONS_DATA_MODE must be 'http' or 'fixture'");
}
export const commonsAdapter = configuredMode === "fixture" ? fixtureAdapter : httpAdapter;
