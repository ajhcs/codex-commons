import { MAX_BROWSE_LIMIT, MAX_TASK_DEPENDENCIES, MAX_TASK_EVENTS, MAX_TASK_LIST, MILESTONE_STATES, TASK_STATES } from "../contracts/commons.js";
import { CommonsAPIError } from "./transport.js";
import { normalizeContributors, normalizeProvenance } from "./provenance.js";
import { projectCoreFixtures } from "./projectCoreFixtures.js";

function invalidPayload() {
  return new CommonsAPIError("Commons returned an invalid response.", { code: "invalid_payload" });
}

function notFound(label) {
  return new CommonsAPIError(`The requested ${label} was not found.`, { code: "not_found", status: 404 });
}

function requireRecord(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) throw invalidPayload();
  return value;
}

function requireString(value) {
  if (typeof value !== "string" || !value) throw invalidPayload();
  return value;
}

function optionalString(value) {
  if (value == null || value === "") return "";
  return requireString(value);
}

function requireInteger(value, minimum = 0) {
  if (!Number.isInteger(value) || value < minimum) throw invalidPayload();
  return value;
}

function timestampLabel(value) {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) throw invalidPayload();
  const delta = Math.max(0, Date.now() - date.getTime());
  const minutes = Math.floor(delta / 60000);
  const relative = minutes < 1 ? "Just now" : minutes < 60 ? `${minutes}m ago` : minutes < 1440 ? `${Math.floor(minutes / 60)}h ago` : `${Math.floor(minutes / 1440)}d ago`;
  return {
    iso: date.toISOString(),
    relative,
    absolute: new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZone: "UTC" }).format(date),
  };
}

function optionalTimestamp(value) {
  return value == null || value === "" ? null : timestampLabel(value);
}

function requirePage(data, maximum = MAX_BROWSE_LIMIT) {
  data = requireRecord(data);
  const limit = requireInteger(data.limit, 1);
  if (limit > maximum || !Array.isArray(data.items) || data.items.length > limit) throw invalidPayload();
  return data;
}

function fixturePage(records, cursor, limit) {
  const offset = cursor ? Number(cursor.split(":").at(-1)) || 0 : 0;
  const items = records.slice(offset, offset + limit);
  const nextOffset = offset + items.length;
  return { items, next_cursor: nextOffset < records.length ? `fixture:${nextOffset}` : "" };
}

function wait(value, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason || new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = globalThis.setTimeout(() => resolve(structuredClone(value)), 90);
    signal?.addEventListener("abort", () => {
      globalThis.clearTimeout(timer);
      reject(signal.reason || new DOMException("Aborted", "AbortError"));
    }, { once: true });
  });
}

function normalizeTaskCounts(value) {
  value = requireRecord(value);
  const result = Object.fromEntries(TASK_STATES.map((state) => [state, requireInteger(value[state])]));
  result.total = requireInteger(value.total);
  if (TASK_STATES.reduce((sum, state) => sum + result[state], 0) !== result.total) throw invalidPayload();
  return result;
}

function normalizeMilestone(value) {
  value = requireRecord(value);
  if (!MILESTONE_STATES.includes(value.status)) throw invalidPayload();
  return {
    id: requireString(value.id),
    projectID: optionalString(value.project),
    title: requireString(value.title),
    status: value.status,
    position: requireInteger(value.position),
    targetDate: optionalString(value.target_date),
    revision: value.revision == null ? 0 : requireInteger(value.revision),
    created: value.created_at ? optionalTimestamp(value.created_at) : null,
    updated: value.updated_at ? optionalTimestamp(value.updated_at) : null,
  };
}

function normalizeDurableActivity(value) {
  if (value == null) return null;
  value = requireRecord(value);
  return {
    kind: requireString(value.kind),
    ref: requireString(value.ref),
    title: requireString(value.title),
    occurred: timestampLabel(value.occurred_at),
  };
}

function normalizeProjectsPage(data) {
  data = requirePage(data);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: optionalString(data.next_cursor),
    items: data.items.map((raw) => {
      raw = requireRecord(raw);
      const destination = requireRecord(raw.destination);
      const currentWork = raw.current_work == null ? null : requireRecord(raw.current_work);
      return {
        id: requireString(raw.id),
        name: requireString(raw.name),
        status: optionalString(raw.status),
        purpose: requireString(raw.purpose),
        activeMilestone: raw.active_milestone == null ? null : normalizeMilestone(raw.active_milestone),
        taskCounts: normalizeTaskCounts(raw.task_counts),
        currentWork: currentWork == null ? null : {
          id: requireString(currentWork.id), title: requireString(currentWork.title), state: requireString(currentWork.state), priority: requireInteger(currentWork.priority),
        },
        openTasks: requireInteger(raw.open_tasks),
        lastActivity: raw.last_activity ? timestampLabel(raw.last_activity) : null,
        lastDurableActivity: normalizeDurableActivity(raw.last_durable_activity),
        destination: { kind: requireString(destination.kind), ref: requireString(destination.ref) },
      };
    }),
  };
}

function normalizeProjectActivity(value) {
  value = requireRecord(value);
  if (!Array.isArray(value.days) || value.days.length !== 14) throw invalidPayload();
  return {
    timezone: requireString(value.timezone),
    start: requireString(value.start),
    endExclusive: requireString(value.end_exclusive),
    days: value.days.map((day) => {
      day = requireRecord(day);
      return { day: requireString(day.day), count: requireInteger(day.count) };
    }),
  };
}

function normalizeProject(data) {
  data = requireRecord(data);
  const project = requireRecord(data.project);
  const counts = requireRecord(data.counts);
  return {
    project: {
      id: requireString(project.id),
      name: requireString(project.name),
      status: requireString(project.status),
      purpose: requireString(project.purpose),
      now: optionalString(project.now),
      revision: requireInteger(project.revision),
      created: optionalTimestamp(project.created_at),
      updated: optionalTimestamp(project.updated_at),
    },
    counts: {
      tasks: requireInteger(counts.tasks), milestones: requireInteger(counts.milestones), wikiPages: requireInteger(counts.wiki_pages),
    },
    activeMilestone: data.active_milestone == null ? null : normalizeMilestone(data.active_milestone),
    snapshot: timestampLabel(data.snapshot_at),
    activity: normalizeProjectActivity(data.activity),
  };
}

function normalizeTaskMilestone(value) {
  if (value == null) return null;
  value = requireRecord(value);
  if (!MILESTONE_STATES.includes(value.status)) throw invalidPayload();
  return { id: requireString(value.id), title: requireString(value.title), status: value.status };
}

function normalizeTask(value) {
  value = requireRecord(value);
  if (!TASK_STATES.includes(value.state) || !Array.isArray(value.dependencies) || value.dependencies.length > MAX_TASK_DEPENDENCIES) throw invalidPayload();
  return {
    id: requireString(value.id),
    projectID: requireString(value.project),
    title: requireString(value.title),
    description: optionalString(value.description),
    acceptance: optionalString(value.acceptance),
    state: value.state,
    priority: requireInteger(value.priority),
    milestoneID: optionalString(value.milestone_id),
    milestone: normalizeTaskMilestone(value.milestone),
    ownerSession: optionalString(value.owner_session),
    ownerProvenance: value.owner_provenance == null && !value.owner_session ? null : normalizeProvenance(value.owner_provenance, { session: value.owner_session }),
    contributors: normalizeContributors(value.contributors),
    contributorsTruncated: Boolean(value.contributors_truncated),
    dependencies: value.dependencies.map((dependency) => {
      dependency = requireRecord(dependency);
      const state = requireString(dependency.state);
      if (!TASK_STATES.includes(state)) throw invalidPayload();
      return { id: requireString(dependency.id), title: requireString(dependency.title), state };
    }),
    dependenciesTruncated: Boolean(value.dependencies_truncated),
    revision: requireInteger(value.revision),
    created: optionalTimestamp(value.created_at),
    updated: optionalTimestamp(value.updated_at),
  };
}

function normalizeTasksPage(data) {
  data = requirePage(data, MAX_TASK_LIST);
  return {
    total: requireInteger(data.total),
    limit: data.limit,
    nextCursor: optionalString(data.next_cursor),
    stateCounts: normalizeTaskCounts(data.state_counts),
    items: data.items.map(normalizeTask),
  };
}

function normalizeTaskEvent(event) {
  event = requireRecord(event);
  return {
    id: requireString(event.id), kind: requireString(event.kind), summary: requireString(event.summary),
    fromState: optionalString(event.from_state), toState: optionalString(event.to_state),
    revision: requireInteger(event.revision), created: optionalTimestamp(event.created_at),
    actor: optionalString(event.actor),
    session: optionalString(event.session),
    provenance: normalizeProvenance(event.provenance, { actor: event.actor, session: event.session, recorded_at: event.created_at }),
  };
}

function normalizeOpenedTask(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.events) || data.events.length > MAX_TASK_EVENTS) throw invalidPayload();
  return {
    task: normalizeTask(data.task),
    eventsNextCursor: optionalString(data.events_next_cursor),
    events: data.events.map(normalizeTaskEvent),
  };
}

function normalizeTaskEvents(data) {
  data = requirePage(data, MAX_TASK_EVENTS);
  return { limit: data.limit, nextCursor: optionalString(data.next_cursor), items: data.items.map(normalizeTaskEvent) };
}

function normalizeMilestones(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || typeof data.truncated !== "boolean") throw invalidPayload();
  const limit = requireInteger(data.limit, 1);
  if (limit > MAX_BROWSE_LIMIT || data.items.length > limit) throw invalidPayload();
  return { total: requireInteger(data.total), limit, truncated: data.truncated, items: data.items.map(normalizeMilestone) };
}

function normalizeWikiPageSummary(value) {
  value = requireRecord(value);
  return {
    id: requireString(value.id), projectID: requireString(value.project), slug: requireString(value.slug),
    title: requireString(value.title), revision: requireInteger(value.current_revision, 1), summary: requireString(value.summary),
    updated: optionalTimestamp(value.updated_at),
  };
}

function normalizeWikiPages(data) {
  data = requirePage(data);
  return {
    total: requireInteger(data.total), limit: data.limit, nextCursor: optionalString(data.next_cursor), items: data.items.map(normalizeWikiPageSummary),
  };
}

function normalizeOpenedWikiPage(data) {
  data = requireRecord(data);
  const page = requireRecord(data.page);
  return {
    page: {
      id: requireString(page.id), projectID: requireString(page.project), slug: requireString(page.slug), title: requireString(page.title),
      revision: requireInteger(page.revision, 1), summary: requireString(page.summary), body: requireString(page.body),
      authorSession: optionalString(page.author_session_id),
      provenance: normalizeProvenance(page.provenance, { session: page.author_session_id, recorded_at: page.created_at }),
      created: optionalTimestamp(page.created_at),
    },
  };
}

function normalizeWikiRevisions(data) {
  data = requirePage(data);
  return {
    limit: data.limit,
    nextCursor: optionalString(data.next_cursor),
    items: data.items.map((item) => {
      item = requireRecord(item);
      return {
        revision: requireInteger(item.revision, 1),
        summary: requireString(item.summary),
        authorSession: optionalString(item.author_session_id),
        provenance: normalizeProvenance(item.provenance, { session: item.author_session_id, recorded_at: item.created_at }),
        created: optionalTimestamp(item.created_at),
      };
    }),
  };
}

function normalizeMutation(data) {
  data = requireRecord(data);
  if (data.persisted !== true) throw invalidPayload();
  return { id: requireString(data.id), revision: requireInteger(data.revision), persisted: true };
}

function safely(normalize, value) {
  try {
    return normalize(value);
  } catch (error) {
    if (error instanceof CommonsAPIError) throw error;
    throw invalidPayload();
  }
}

export function createProjectCoreHTTPMethods(transport) {
  return {
    async readProjects(query, signal) { return safely(normalizeProjectsPage, await transport.readProjects(query, signal)); },
    async readProject(projectID, signal) { return safely(normalizeProject, await transport.readProject(projectID, signal)); },
    async readProjectMilestones(projectID, limit, signal) { return safely(normalizeMilestones, await transport.readProjectMilestones(projectID, limit, signal)); },
    async readProjectTasks(projectID, query, signal) { return safely(normalizeTasksPage, await transport.readProjectTasks(projectID, query, signal)); },
    async readTask(taskID, eventsLimit, signal) { return safely(normalizeOpenedTask, await transport.readTask(taskID, eventsLimit, signal)); },
    async readTaskEvents(taskID, query, signal) { return safely(normalizeTaskEvents, await transport.readTaskEvents(taskID, query, signal)); },
    async readWikiPages(projectID, query, signal) { return safely(normalizeWikiPages, await transport.readWikiPages(projectID, query, signal)); },
    async readWikiPage(projectID, slug, signal) { return safely(normalizeOpenedWikiPage, await transport.readWikiPage(projectID, slug, signal)); },
    async readWikiRevisions(projectID, slug, query, signal) { return safely(normalizeWikiRevisions, await transport.readWikiRevisions(projectID, slug, query, signal)); },
    async readWikiRevision(projectID, slug, revision, signal) { return safely(normalizeOpenedWikiPage, await transport.readWikiRevision(projectID, slug, revision, signal)); },
    async createProject(input, options, signal) { return safely(normalizeMutation, await transport.createProject(input, options, signal)); },
    async updateProject(projectID, input, options, signal) { return safely(normalizeMutation, await transport.updateProject(projectID, input, options, signal)); },
    async createMilestone(projectID, input, options, signal) { return safely(normalizeMutation, await transport.createMilestone(projectID, input, options, signal)); },
    async updateMilestone(milestoneID, input, options, signal) { return safely(normalizeMutation, await transport.updateMilestone(milestoneID, input, options, signal)); },
    async createTask(projectID, input, options, signal) { return safely(normalizeMutation, await transport.createTask(projectID, input, options, signal)); },
    async updateTask(taskID, input, options, signal) { return safely(normalizeMutation, await transport.updateTask(taskID, input, options, signal)); },
    async changeTaskState(taskID, input, options, signal) { return safely(normalizeMutation, await transport.changeTaskState(taskID, input, options, signal)); },
    async createWikiRevision(projectID, slug, input, options, signal) { return safely(normalizeMutation, await transport.createWikiRevision(projectID, slug, input, options, signal)); },
  };
}

function fixtureTaskCounts(tasks) {
  const result = Object.fromEntries(TASK_STATES.map((state) => [state, tasks.filter((task) => task.state === state).length]));
  return { ...result, total: tasks.length };
}

export const projectCoreFixtureMethods = {
  async readProjects(query, signal) {
    let records = projectCoreFixtures.projects;
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.name} ${item.purpose} ${item.active_milestone?.title || ""}`.toLowerCase().includes(term));
    }
    const result = fixturePage(records, query.cursor, query.limit);
    return wait(normalizeProjectsPage({ total: records.length, limit: query.limit, next_cursor: result.next_cursor, items: result.items }), signal);
  },
  async readProject(projectID, signal) {
    const value = projectCoreFixtures.details[projectID];
    if (!value) throw notFound("project");
    return wait(normalizeProject(value), signal);
  },
  async readProjectMilestones(projectID, limit, signal) {
    const records = projectCoreFixtures.milestones[projectID] || [];
    return wait(normalizeMilestones({ total: records.length, limit, truncated: records.length > limit, items: records.slice(0, limit) }), signal);
  },
  async readProjectTasks(projectID, query, signal) {
    let records = projectCoreFixtures.tasks[projectID] || [];
    if (query.state) records = records.filter((task) => task.state === query.state);
    if (query.milestone) records = records.filter((task) => task.milestone_id === query.milestone);
    const all = projectCoreFixtures.tasks[projectID] || [];
    const result = fixturePage(records, query.cursor, query.limit);
    return wait(normalizeTasksPage({ total: records.length, limit: query.limit, next_cursor: result.next_cursor, state_counts: fixtureTaskCounts(all), items: result.items }), signal);
  },
  async readTask(taskID, eventsLimit, signal) {
    const task = Object.values(projectCoreFixtures.tasks).flat().find((item) => item.id === taskID);
    if (!task) throw notFound("task");
    const records = projectCoreFixtures.events[taskID] || [];
    const events = records.slice(0, eventsLimit);
    return wait(normalizeOpenedTask({ task, events, events_next_cursor: records.length > eventsLimit ? `fixture:${eventsLimit}` : "" }), signal);
  },
  async readTaskEvents(taskID, query, signal) {
    const records = projectCoreFixtures.events[taskID] || [];
    const result = fixturePage(records, query.cursor, query.limit);
    return wait(normalizeTaskEvents({ limit: query.limit, next_cursor: result.next_cursor, items: result.items }), signal);
  },
  async readWikiPages(projectID, query, signal) {
    let records = projectCoreFixtures.wiki[projectID] || [];
    if (query.q) {
      const term = query.q.toLowerCase();
      records = records.filter((item) => `${item.title} ${item.summary}`.toLowerCase().includes(term));
    }
    const result = fixturePage(records, query.cursor, query.limit);
    return wait(normalizeWikiPages({ total: records.length, limit: query.limit, next_cursor: result.next_cursor, items: result.items }), signal);
  },
  async readWikiPage(projectID, slug, signal) {
    const value = projectCoreFixtures.wikiPages[`${projectID}/${slug}`];
    if (!value) throw notFound("wiki page");
    return wait(normalizeOpenedWikiPage({ page: value }), signal);
  },
  async readWikiRevisions(projectID, slug, query, signal) {
    const records = projectCoreFixtures.wikiRevisions[`${projectID}/${slug}`] || [];
    const result = fixturePage(records, query.cursor, query.limit);
    return wait(normalizeWikiRevisions({ limit: query.limit, next_cursor: result.next_cursor, items: result.items }), signal);
  },
  async readWikiRevision(projectID, slug, revision, signal) {
    const value = projectCoreFixtures.wikiPages[`${projectID}/${slug}`];
    if (!value || value.revision !== revision) throw notFound("wiki revision");
    return wait(normalizeOpenedWikiPage({ page: value }), signal);
  },
  async createProject(_input, _options, signal) { return wait({ id: `fixture-project-${Date.now()}`, revision: 1, persisted: true }, signal); },
  async updateProject(projectID, input, _options, signal) { return wait({ id: projectID, revision: input.base_revision + 1, persisted: true }, signal); },
  async createMilestone(_projectID, _input, _options, signal) { return wait({ id: `fixture-milestone-${Date.now()}`, revision: 1, persisted: true }, signal); },
  async updateMilestone(milestoneID, input, _options, signal) { return wait({ id: milestoneID, revision: input.base_revision + 1, persisted: true }, signal); },
  async createTask(_projectID, _input, _options, signal) { return wait({ id: `fixture-task-${Date.now()}`, revision: 1, persisted: true }, signal); },
  async updateTask(taskID, input, _options, signal) { return wait({ id: taskID, revision: input.base_revision + 1, persisted: true }, signal); },
  async changeTaskState(taskID, input, _options, signal) { return wait({ id: taskID, revision: input.base_revision + 1, persisted: true }, signal); },
  async createWikiRevision(_projectID, slug, input, _options, signal) { return wait({ id: slug, revision: (input.base_revision || 0) + 1, persisted: true }, signal); },
};
