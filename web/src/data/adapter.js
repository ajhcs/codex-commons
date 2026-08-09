import { contractFixtures } from "./fixtures.js";

function wait(value, signal) {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => resolve(structuredClone(value)), 110);
    signal?.addEventListener("abort", () => {
      window.clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    }, { once: true });
  });
}

function timestampLabel(value) {
  const date = new Date(value);
  const delta = Math.max(0, Date.parse("2026-08-09T12:00:00Z") - date.getTime());
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
  return {
    id: record.id,
    severity: record.severity,
    title: record.title,
    project: record.project_name || record.project || "—",
    source: { kind: record.source_kind, ref: record.source_ref },
    owner: record.owner || "Unassigned",
    ownerSession: record.owner || "",
    nextAction: record.next_action,
    updated: timestampLabel(record.updated_at),
    destination: record.destination || null,
    untrusted: record.untrusted,
  };
}

export const fixtureAdapter = {
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
};
