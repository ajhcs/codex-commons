import { ATTACHMENT_KINDS, ATTENTION_SEVERITIES, AUTH_PAIRING_STATES, COMMENT_INTENTS, DEFAULT_PROJECT_ARCHAEOLOGY_HISTORY_PAGE, EXECUTION_STATES, HUMAN_HANDLE_PATTERN, isValidHumanDisplayName, isValidHumanHandle, PERSPECTIVE_SCOPES, MAX_BROWSE_LIMIT, MAX_NOTIFICATIONS, MAX_OVERVIEW_LIMIT, MAX_PROJECT_ARCHAEOLOGY_CATALOG_PAGE, MAX_PROJECT_ARCHAEOLOGY_HISTORY_PAGE, MAX_PROJECT_ARCHAEOLOGY_OUTCOME_PAGE, MAX_PROJECT_ARCHAEOLOGY_REVIEW_MEMBERS, MAX_PROJECT_ARCHAEOLOGY_REVIEW_OUTCOMES, MAX_PROJECT_ARCHAEOLOGY_SELECTION, POST_KINDS, POST_STATES, PROJECT_ARCHAEOLOGY_DEPTHS, PROJECT_ARCHAEOLOGY_DISCOVERY_STATES, PROJECT_ARCHAEOLOGY_RUN_STATES, PROJECT_ARCHAEOLOGY_STATES } from "../contracts/commons.js";
import { contractFixtures, codexAuthFixtures } from "./fixtures.js";
import { postFixtures, slice13FixtureTimes } from "./postFixtures.js";
import { createProjectCoreHTTPMethods, projectCoreFixtureMethods } from "./projectCoreAdapter.js";
import { normalizeProvenance } from "./provenance.js";
import { confirmedHistoricalImportRequest, normalizeArchaeologyCapabilities, normalizeArchaeologyHandoff, normalizeArchaeologyImportPreview, normalizeHistoricalImportResult } from "./projectArchaeologyAdapter.js";
import { archaeologyHandoffFixture, archaeologyReadyFixture, archaeologyReviewFixture } from "../features/project-archaeology/projectArchaeologyFixtures.js";
import { CommonsAPIError, createHTTPTransport } from "./transport.js";
const PROJECT_ARCHAEOLOGY_DISCOVERY_STAGES = new Set(["idle", "queued", "reading_codex_metadata", "persisting_catalog", "ready", "failed"]);


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
  const hasTypedIdentity = value.kind != null || value.principal != null || value.display_name != null;
  const kind = value.kind == null ? "agent" : value.kind;
  if (!['human', 'agent'].includes(kind)) throw invalidPayload();
  const principal = value.principal == null ? "" : requireString(value.principal);
  const displayName = value.display_name == null ? "" : requireString(value.display_name);
  const session = value.session == null ? "" : requireString(value.session);
  if (hasTypedIdentity && !principal) throw invalidPayload();
  if (kind === "human" && !displayName) throw invalidPayload();
  if (!hasTypedIdentity && !session) throw invalidPayload();
  return {
    kind,
    principal,
    displayName,
    handle: typeof value.handle === "string" ? value.handle : "",
    session,
    purpose: typeof value.purpose === "string" ? value.purpose : "",
    actor: typeof value.actor === "string" ? value.actor : "",
    provenance: normalizeProvenance(value.provenance, { actor: value.actor, session, purpose: value.purpose }),
  };
}

function normalizePrincipalTarget(value) {
  value = requireRecord(value);
  if (!['human', 'agent'].includes(value.kind)) throw invalidPayload();
  const principal = requireString(value.principal);
  const session = typeof value.session === "string" ? value.session : "";
  const purpose = typeof value.purpose === "string" ? value.purpose : "";
  return {
    kind: value.kind,
    principal,
    session,
    handle: typeof value.handle === "string" ? value.handle : "",
    displayName: typeof value.display_name === "string" ? value.display_name : "",
    purpose,
    provenance: value.provenance == null ? null : normalizeProvenance(value.provenance, { session, purpose }),
  };
}

function normalizeSession(value) {
  value = requireRecord(value);
  if (typeof value.authenticated !== "boolean") throw invalidPayload();
  if (!value.authenticated) {
    return { authenticated: false, principal: null, csrfToken: "", authMethod: "", profileRevision: 0 };
  }
  const principal = requireRecord(value.principal);
  if (principal.kind !== "human") throw invalidPayload();
  const authMethod = value.auth_method == null ? "recovery" : value.auth_method;
  if (!['codex', 'recovery'].includes(authMethod)) throw invalidPayload();
  const profileRevision = value.profile_revision == null ? 0 : value.profile_revision;
  if (!Number.isInteger(profileRevision) || profileRevision < 0) throw invalidPayload();
  const handle = typeof principal.handle === "string" ? principal.handle : "";
  if (handle && !isValidHumanHandle(handle)) throw invalidPayload();
  if (!isValidHumanDisplayName(principal.display_name)) throw invalidPayload();
  return {
    authenticated: true,
    principal: {
      kind: "human",
      principal: requireString(principal.principal),
      handle,
      displayName: requireString(principal.display_name),
    },
    csrfToken: requireString(value.csrf_token),
    authMethod,
    profileRevision,
  };
}

function normalizeCodexStatus(value) {
  value = requireRecord(value);
  if (typeof value.available !== "boolean"
    || !['unbound', 'bound'].includes(value.binding_state)
    || !['signed_in', 'signed_out', 'unknown', 'unavailable'].includes(value.account_state)
    || typeof value.first_bind_allowed !== "boolean") throw invalidPayload();
  return {
    available: value.available,
    bindingState: value.binding_state,
    accountState: value.account_state,
    firstBindAllowed: value.first_bind_allowed,
  };
}

function normalizeVerificationURL(value) {
  if (typeof value !== "string" || value.length < 1 || value.length > 2048 || /[\r\n]/.test(value)) throw invalidPayload();
  let parsed;
  try { parsed = new URL(value); } catch { throw invalidPayload(); }
  if (parsed.protocol !== "https:" || parsed.hostname !== "auth.openai.com" || parsed.port || parsed.username || parsed.password || parsed.hash || !parsed.pathname) throw invalidPayload();
  return value;
}

function normalizeCodexStart(value) {
  value = requireRecord(value);
  const attemptID = requireString(value.attempt_id);
  const userCode = requireString(value.user_code);
  if (attemptID.length > 200 || userCode.length > 64 || userCode.trim() !== userCode || [...userCode].some((character) => character < "!" || character > "~")) throw invalidPayload();
  if (typeof value.expires_at !== "string") throw invalidPayload();
  const expiresAt = new Date(value.expires_at);
  if (!Number.isFinite(expiresAt.getTime())) throw invalidPayload();
  if (!Number.isInteger(value.poll_after_ms) || value.poll_after_ms < 250 || value.poll_after_ms > 60000) throw invalidPayload();
  return {
    attemptID,
    verificationURL: normalizeVerificationURL(value.verification_url),
    userCode,
    expiresAt: expiresAt.toISOString(),
    pollAfterMS: value.poll_after_ms,
    destinationBehavior: (() => { const behavior = value.destination_behavior == null ? "manual_code_required" : requireString(value.destination_behavior); if (behavior !== "manual_code_required") throw invalidPayload(); return behavior; })(),
  };
}

function normalizeCodexPoll(value) {
  value = requireRecord(value);
  if (value.authenticated === true) {
    return { state: "authenticated", session: normalizeSession(value) };
  }
  const state = requireString(value.state);
  if (!AUTH_PAIRING_STATES.includes(state)) throw invalidPayload();
  const pollAfterMS = value.poll_after_ms == null ? 0 : value.poll_after_ms;
  if (!Number.isInteger(pollAfterMS) || pollAfterMS < 0 || pollAfterMS > 60000) throw invalidPayload();
  const code = value.code == null ? "" : requireString(value.code);
  const message = value.message == null ? "" : requireString(value.message);
  return { state, code, message, pollAfterMS };
}

function normalizeProfileInput(input) {
  const value = requireRecord(input);
  const displayName = typeof value.display_name === "string" ? value.display_name.trim() : "";
  const handle = typeof value.handle === "string" ? value.handle.trim().toLowerCase() : "";
  if (!isValidHumanDisplayName(displayName) || !HUMAN_HANDLE_PATTERN.test(handle) || !isValidHumanHandle(handle)) throw invalidPayload();
  return { displayName, handle };
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
  const scope = value.perspective_scope == null ? { value: "closed", revision: 0 } : requireRecord(value.perspective_scope);
  if (!PERSPECTIVE_SCOPES.includes(scope.value)) throw invalidPayload();
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
    perspectiveScope: { value: scope.value, revision: requireInteger(scope.revision) },
    mentions: normalizeMentions(value.mentions),
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
      items: comments.items.map(normalizeComment),
    },
  };
}

function normalizeComment(value) {
  value = requireRecord(value);
  if (!COMMENT_INTENTS.includes(value.intent)) throw invalidPayload();
  return {
    id: requireString(value.id),
    body: requireString(value.body),
    intent: value.intent,
    author: normalizeAuthor(value.author),
    created: timestampLabel(value.created_at),
    mentions: normalizeMentions(value.mentions),
  };
}

function normalizeCommentSource(data) {
  data = requireRecord(data);
  return { postRef: requireString(data.post_ref), comment: normalizeComment(data.comment) };
}

function normalizeMentions(value) {
  if (value == null) return [];
  if (!Array.isArray(value) || value.length > 5) throw invalidPayload();
  const mentions = value.map(normalizePrincipalTarget);
  if (new Set(mentions.map((mention) => mention.principal)).size !== mentions.length) throw invalidPayload();
  return mentions;
}

function normalizeContributors(data) {
  data = requirePage(data, 20);
  return { limit: data.limit, nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "", items: data.items.map((value) => {
    value = requireRecord(value);
    const target = normalizePrincipalTarget(value);
    if (value.addressable !== true || typeof value.reachable !== "boolean" || typeof value.host_connected !== "boolean" || ![...EXECUTION_STATES, "not_applicable"].includes(value.execution) || !["none", "project", "same_project"].includes(value.project_relationship)) throw invalidPayload();
    return { ...target, handle: requireString(value.handle), host: typeof value.host === "string" ? value.host : "", project: value.project == null ? null : normalizeTopic(value.project), projectRelationship: value.project_relationship, addressable: true, reachable: value.reachable, interpretation: requireString(value.interpretation), connected: value.host_connected, execution: value.execution, lastActivity: value.last_activity ? timestampLabel(value.last_activity) : null };
  }) };
}

function normalizeNotifications(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || data.items.length > MAX_NOTIFICATIONS) throw invalidPayload();
  return {
    nextCursor: typeof data.next_cursor === "string" ? data.next_cursor : "",
    unreadCount: requireInteger(data.unread_count),
    items: data.items.map((value) => {
      value = requireRecord(value);
      const source = requireRecord(value.source);
      if (!['post', 'comment'].includes(source.kind)) throw invalidPayload();
      return {
        id: requireString(value.id),
        recipient: normalizePrincipalTarget(value.recipient),
        source: {
          kind: source.kind,
          postRef: requireString(source.post_ref),
          commentRef: typeof source.comment_ref === "string" ? source.comment_ref : "",
        },
        actor: normalizePrincipalTarget(value.actor),
        snippet: requireString(value.snippet),
        created: timestampLabel(value.created_at),
        readAt: value.read_at ? timestampLabel(value.read_at) : null,
      };
    }),
  };
}

function archaeologyStringList(value, maximum = 12) {
  if (!Array.isArray(value) || value.length > maximum) throw invalidPayload();
  return value.map(requireString);
}

function normalizeArchaeologyMember(value) {
  value = requireRecord(value);
  if (value.reachability !== "historical_or_unknown" || value.execution !== "not_attested" || value.authority !== "provenance_only") throw invalidPayload();
  return {
    sessionId: requireString(value.session_id),
    displayName: typeof value.display_name === "string" ? value.display_name : "",
    reachability: value.reachability,
    execution: value.execution,
    authority: value.authority,
    contributionCount: requireInteger(value.contribution_count),
    sourceCount: requireInteger(value.source_count),
    collaborationCount: requireInteger(value.collaboration_count),
    demonstratedStrengths: archaeologyStringList(value.demonstrated_strengths),
    uncertainties: archaeologyStringList(value.uncertainties),
  };
}

function normalizeArchaeologyProvenance(value) {
  value = requireRecord(value);
  return {
    sourceKind: requireString(value.source_kind),
    sourceLabel: requireString(value.source_label),
    digest: requireString(value.digest),
    recordedAt: value.recorded_at == null ? null : timestampLabel(value.recorded_at),
  };
}

function normalizeArchaeologyOutcome(value) {
  value = requireRecord(value);
  if (!Array.isArray(value.provenance) || value.provenance.length > 4 || !Array.isArray(value.member_sessions) || value.member_sessions.length > 1) throw invalidPayload();
  const sourceDigest = value.source_digest == null ? "" : requireString(value.source_digest);
  if (sourceDigest && !/^sha256:[0-9a-f]{64}$/.test(sourceDigest)) throw invalidPayload();
  const sourceCount = requireInteger(value.source_count);
  if (value.provenance.length > sourceCount) throw invalidPayload();
  return {
    id: requireString(value.id),
    title: requireString(value.title),
    summary: requireString(value.summary),
    projectId: requireString(value.project_id),
    sourceDigest,
    sourceCount,
    provenance: value.provenance.map(normalizeArchaeologyProvenance),
    memberSessions: value.member_sessions.map(normalizeArchaeologyMember),
  };
}

function normalizeArchaeologyCandidate(rawCandidate) {
  const candidate = requireRecord(rawCandidate);
  const signals = requireRecord(candidate.signals);
  const estimate = requireRecord(candidate.estimate);
  if (typeof signals.git !== "boolean" || typeof signals.docs !== "boolean" || typeof signals.codex_history !== "boolean" || !["low", "medium", "high"].includes(estimate.relative_cost) || typeof candidate.selected_by_default !== "boolean" || !Array.isArray(candidate.sources) || candidate.sources.length > 2 || !candidate.sources.every((source) => ["codex_metadata", "configured_root"].includes(source))) throw invalidPayload();
  const durationSecondsMin = requireInteger(estimate.duration_seconds_min);
  const durationSecondsMax = requireInteger(estimate.duration_seconds_max);
  if (durationSecondsMax < durationSecondsMin) throw invalidPayload();
  return {
    id: requireString(candidate.id),
    candidateId: requireString(candidate.id),
    name: requireString(candidate.name),
    repositoryLabel: candidate.repository_label == null ? "" : requireString(candidate.repository_label),
    lastActivity: candidate.last_activity_at == null ? null : timestampLabel(candidate.last_activity_at),
    signals: { git: signals.git, docs: signals.docs, codexHistory: signals.codex_history },
    estimate: { durationSecondsMin, durationSecondsMax, relativeCost: estimate.relative_cost },
    privacyNote: requireString(candidate.privacy_note),
    selectedByDefault: candidate.selected_by_default,
    sources: candidate.sources.map(requireString),
    codexThreadCount: requireInteger(candidate.codex_thread_count),
  };
}

function normalizeDiscoveryTelemetry(raw, fallback = {}) {
  raw = requireRecord(raw || {});
  return {
    tasksExamined: requireInteger(raw.tasks_examined ?? raw.codex_threads_examined ?? fallback.tasksExamined ?? 0),
    projectsGrouped: requireInteger(raw.projects_grouped ?? raw.workspaces_grouped ?? fallback.projectsGrouped ?? 0),
    truncated: raw.truncated === true,
    completedAt: raw.completed_at == null ? null : timestampLabel(raw.completed_at),
    appServerIdentity: raw.app_server_identity == null ? "" : requireString(raw.app_server_identity),
  };
}

function normalizeArchaeologyCatalogPage(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || data.items.length > MAX_PROJECT_ARCHAEOLOGY_CATALOG_PAGE) throw invalidPayload();
  const items = data.items.map(normalizeArchaeologyCandidate);
  if (new Set(items.map((item) => item.id)).size !== items.length) throw invalidPayload();
  return {
    items,
    nextCursor: data.next_cursor == null ? "" : typeof data.next_cursor === "string" ? data.next_cursor : (() => { throw invalidPayload(); })(),
    total: requireInteger(data.total),
  };
}

function normalizeArchaeologyReview(rawReview) {
  if (rawReview == null) return null;
  rawReview = requireRecord(rawReview);
  if (!Array.isArray(rawReview.proposed_outcomes) || rawReview.proposed_outcomes.length > MAX_PROJECT_ARCHAEOLOGY_REVIEW_OUTCOMES || !Array.isArray(rawReview.member_sessions) || rawReview.member_sessions.length > MAX_PROJECT_ARCHAEOLOGY_REVIEW_MEMBERS || typeof rawReview.can_apply !== "boolean" || rawReview.requires_explicit_approval !== true) throw invalidPayload();
  const proposedOutcomes = rawReview.proposed_outcomes.map(normalizeArchaeologyOutcome);
  if (rawReview.can_apply && proposedOutcomes.some((outcome) => !outcome.sourceDigest)) throw invalidPayload();
  return {
    proposedOutcomes,
    batchId: rawReview.batch_id == null ? "" : requireString(rawReview.batch_id),
    memberSessions: rawReview.member_sessions.map(normalizeArchaeologyMember),
    provenanceSummary: requireString(rawReview.provenance_summary),
    canApply: rawReview.can_apply,
    requiresExplicitApproval: true,
  };
}

function normalizeArchaeologyBatchSummary(raw) {
  raw = requireRecord(raw);
  if (!["queued", "running", "cancel_requested", "canceled", "completed", "attention"].includes(raw.state) || !PROJECT_ARCHAEOLOGY_DEPTHS.includes(raw.depth) || ![1, 2].includes(raw.concurrency)) throw invalidPayload();
  const sources = requireRecord(raw.sources);
  if (typeof sources.git !== "boolean" || typeof sources.docs !== "boolean" || typeof sources.codex_history !== "boolean" || typeof raw.has_report !== "boolean") throw invalidPayload();
  return {
    batchId: requireString(raw.batch_id),
    state: raw.state,
    mode: raw.mode == null ? "" : requireString(raw.mode),
    depth: raw.depth,
    sources: { git: sources.git, docs: sources.docs, codexHistory: sources.codex_history },
    concurrency: raw.concurrency,
    selectedTotal: requireInteger(raw.selected_total),
    queuedCount: requireInteger(raw.queued_count),
    activeCount: requireInteger(raw.active_count),
    completedCount: requireInteger(raw.completed_count),
    attentionCount: requireInteger(raw.attention_count),
    createdAt: timestampLabel(raw.created_at),
    updatedAt: timestampLabel(raw.updated_at),
    hasReport: raw.has_report,
  };
}

function normalizeArchaeologyBatchPage(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || data.items.length > MAX_PROJECT_ARCHAEOLOGY_HISTORY_PAGE) throw invalidPayload();
  return { items: data.items.map(normalizeArchaeologyBatchSummary), nextCursor: data.next_cursor == null ? "" : typeof data.next_cursor === "string" ? data.next_cursor : (() => { throw invalidPayload(); })() };
}

function normalizeArchaeologyBatchTask(raw, batchID) {
  raw = requireRecord(raw);
  const allowedStates = new Set(["queued", "starting", "active", "report_ready", "cancel_requested", "completed", "failed", "interrupted", "canceled", "uncertain", "attention"]);
  if (!allowedStates.has(raw.state) || raw.batch_id !== batchID || !Array.isArray(raw.available_actions) || raw.available_actions.length > 8) throw invalidPayload();
  return {
    jobId: requireString(raw.job_id),
    batchId: batchID,
    candidateId: requireString(raw.candidate_id),
    projectId: requireString(raw.project_id),
    projectName: raw.project_name == null ? "" : requireString(raw.project_name),
    state: raw.state,
    mode: raw.mode == null ? "" : requireString(raw.mode),
    phaseLabel: raw.phase_label == null ? "" : requireString(raw.phase_label),
    sourcesExamined: requireInteger(raw.sources_examined ?? 0),
    durationMs: raw.duration_ms == null ? null : requireInteger(raw.duration_ms),
    launchId: raw.launch_id == null ? "" : requireString(raw.launch_id),
    threadId: raw.thread_id == null ? "" : requireString(raw.thread_id),
    turnId: raw.turn_id == null ? "" : requireString(raw.turn_id),
    createdAt: timestampLabel(raw.created_at),
    updatedAt: timestampLabel(raw.updated_at),
    error: raw.error == null ? "" : requireString(raw.error),
    availableActions: raw.available_actions.map(requireString),
  };
}

function normalizeArchaeologyBatchDetail(data) {
  data = requireRecord(data);
  if (!["queued", "running", "cancel_requested", "canceled", "completed", "attention"].includes(data.state) || !PROJECT_ARCHAEOLOGY_DEPTHS.includes(data.depth) || ![1, 2].includes(data.concurrency)) throw invalidPayload();
  const sources = requireRecord(data.sources);
  if (typeof sources.git !== "boolean" || typeof sources.docs !== "boolean" || typeof sources.codex_history !== "boolean") throw invalidPayload();
  const summary = {
    batchId: requireString(data.batch_id), state: data.state, mode: data.mode == null ? "" : requireString(data.mode), depth: data.depth,
    sources: { git: sources.git, docs: sources.docs, codexHistory: sources.codex_history },
    concurrency: data.concurrency, selectedTotal: requireInteger(data.selected_total),
    queuedCount: requireInteger(data.queued_count ?? 0),
    activeCount: requireInteger(data.active_count ?? 0),
    completedCount: requireInteger(data.completed_count ?? 0),
    attentionCount: requireInteger(data.attention_count ?? 0),
    hasReport: data.has_report === true,
    createdAt: timestampLabel(data.created_at), updatedAt: timestampLabel(data.updated_at),
  };
  if (!Array.isArray(data.tasks) || data.tasks.length > MAX_PROJECT_ARCHAEOLOGY_SELECTION) throw invalidPayload();
  const tasks = data.tasks.map((task) => normalizeArchaeologyBatchTask(task, summary.batchId));
  if (new Set(tasks.map((task) => task.jobId)).size !== tasks.length) throw invalidPayload();
  const review = normalizeArchaeologyReview(data.review);
  if (review && review.batchId !== summary.batchId) throw invalidPayload();
  return { ...summary, tasks, review, outcomesNextCursor: data.outcomes_next_cursor == null ? "" : typeof data.outcomes_next_cursor === "string" ? data.outcomes_next_cursor : (() => { throw invalidPayload(); })() };
}

function normalizeArchaeologyOutcomePage(data) {
  data = requireRecord(data);
  if (!Array.isArray(data.items) || data.items.length > MAX_PROJECT_ARCHAEOLOGY_OUTCOME_PAGE) throw invalidPayload();
  const items = data.items.map(normalizeArchaeologyOutcome);
  if (new Set(items.map((item) => item.id)).size !== items.length) throw invalidPayload();
  return { items, nextCursor: data.next_cursor == null ? "" : typeof data.next_cursor === "string" ? data.next_cursor : (() => { throw invalidPayload(); })() };
}

function normalizeInstallationStatus(data) {
  data = requireRecord(data);
  const service = requireRecord(data.service);
  const database = requireRecord(data.database);
  const codex = requireRecord(data.codex);
  const archaeology = requireRecord(data.archaeology);
  const backup = requireRecord(data.backup);
  const reconciliation = requireRecord(data.reconciliation);
  const evidence = requireRecord(data.evidence);
  const restoreDrill = requireRecord(evidence.restore_drill);
  const verification = (value) => {
    value = requireRecord(value);
    return {
      status: ["unknown", "verified", "attention"].includes(value.status) ? value.status : (() => { throw invalidPayload(); })(),
      violations: requireInteger(value.violations),
      checkedAt: value.checked_at == null ? null : timestampLabel(value.checked_at),
    };
  };
  if (typeof codex.configured !== "boolean" || typeof codex.available !== "boolean") throw invalidPayload();
  const backupStatus = ["unknown", "verified", "failed"].includes(backup.status) ? backup.status : (() => { throw invalidPayload(); })();
  const reconciliationStatus = ["unknown", "healthy", "attention", "failed"].includes(reconciliation.status) ? reconciliation.status : (() => { throw invalidPayload(); })();
  return {
    service: { version: requireString(service.version), startedAt: service.started_at == null ? null : timestampLabel(service.started_at) },
    database: { schemaVersion: requireInteger(database.schema_version) },
    codex: {
      configured: codex.configured,
      available: codex.available,
      version: requireString(codex.version),
      accountState: requireString(codex.account_state),
      sessionRevocationPending: typeof codex.session_revocation_pending === "boolean" ? codex.session_revocation_pending : (() => { throw invalidPayload(); })(),
      compatibilityStatus: ["unknown", "compatible", "incompatible", "unavailable"].includes(codex.compatibility_status) ? codex.compatibility_status : (() => { throw invalidPayload(); })(),
      compatibilityCheckedAt: codex.compatibility_checked_at == null ? null : timestampLabel(codex.compatibility_checked_at),
    },
    archaeology: { catalogCompletedAt: archaeology.catalog_completed_at == null ? null : timestampLabel(archaeology.catalog_completed_at), activeCount: requireInteger(archaeology.active_count), uncertainCount: requireInteger(archaeology.uncertain_count) },
    backup: { lastVerifiedAt: backup.last_verified_at == null ? null : timestampLabel(backup.last_verified_at), status: backupStatus },
    reconciliation: { lastAt: reconciliation.last_at == null ? null : timestampLabel(reconciliation.last_at), status: reconciliationStatus },
    evidence: {
      completedHistorians: requireInteger(evidence.completed_historians),
      failedHistorians: requireInteger(evidence.failed_historians),
      uncertainHistorians: requireInteger(evidence.uncertain_historians),
      distinctProjects: requireInteger(evidence.distinct_projects),
      reportsReceived: requireInteger(evidence.reports_received),
      lostReports: requireInteger(evidence.lost_reports),
      reviewedImports: requireInteger(evidence.reviewed_imports),
      cancellations: requireInteger(evidence.cancellations),
      reportRecovery: verification(evidence.report_recovery),
      duplicateLaunchCheck: verification(evidence.duplicate_launch_check),
      repositoryImmutability: verification(evidence.repository_immutability),
      canonicalImmutability: verification(evidence.canonical_immutability),
      restoreDrill: {
        status: ["unknown", "verified", "failed"].includes(restoreDrill.status) ? restoreDrill.status : (() => { throw invalidPayload(); })(),
        lastVerifiedAt: restoreDrill.last_verified_at == null ? null : timestampLabel(restoreDrill.last_verified_at),
      },
      betaPrerequisitesMet: evidence.beta_prerequisites_met === true ? true : evidence.beta_prerequisites_met === false ? false : (() => { throw invalidPayload(); })(),
    },
  };
}

function normalizeArchaeologyBatchImport(raw, expectedBatchID, expectedOutcomeIDs, expectedReviewSessionToken = "") {
  raw = requireRecord(raw);
  if (raw.batch_id !== expectedBatchID
    || !Array.isArray(raw.outcome_ids)
    || raw.outcome_ids.length !== expectedOutcomeIDs.length
    || raw.outcome_ids.some((id, index) => id !== expectedOutcomeIDs[index])
    || !/^sha256:[0-9a-f]{64}$/.test(raw.selection_digest)
    || !/^sha256:[0-9a-f]{64}$/.test(raw.manifest_digest)
    || raw.applied === true
    || !Array.isArray(raw.projects)
    || raw.projects.length < 1
    || raw.projects.length > 5) throw invalidPayload();
  const projects = raw.projects.map((project) => {
    const bridge = { ...normalizeArchaeologyImportPreview(project, timestampLabel), outcomeId: requireString(project.outcome_id) };
    if (bridge.preview.applied !== false) throw invalidPayload();
    return bridge;
  });
  if (new Set(projects.map((project) => project.outcomeId)).size !== projects.length
    || projects.some((project) => !expectedOutcomeIDs.includes(project.outcomeId))) throw invalidPayload();
  const keys = new Set();
  const tasks = [];
  const previewTasks = [];
  const aliases = [];
  const counts = { projectThreadAliases: 0, tasks: 0, attributions: 0, events: 0, created: 0, skippedCurrent: 0, replayed: 0 };
  projects.forEach((project) => {
    project.proposal.tasks.forEach((task) => {
      const qualifiedKey = `${project.projectId}:${project.outcomeId}:${task.key}`;
      if (keys.has(qualifiedKey)) throw invalidPayload();
      keys.add(qualifiedKey);
      tasks.push({ ...task, key: qualifiedKey, originalKey: task.key, projectId: project.projectId, outcomeId: project.outcomeId });
    });
    project.preview.tasks.forEach((task) => previewTasks.push({ ...task, key: `${project.projectId}:${project.outcomeId}:${task.key}`, originalKey: task.key, projectId: project.projectId, outcomeId: project.outcomeId }));
    project.proposal.projectThreadAliases.forEach((alias) => aliases.push({ ...alias, projectId: project.projectId, outcomeId: project.outcomeId }));
    Object.keys(counts).forEach((key) => { counts[key] += project.preview.counts[key] || 0; });
  });
  if (counts.tasks !== tasks.length || previewTasks.length !== tasks.length) throw invalidPayload();
  const nextCursor = raw.next_cursor == null ? "" : typeof raw.next_cursor === "string" ? raw.next_cursor : (() => { throw invalidPayload(); })();
  const reviewSessionToken = requireString(raw.review_session_token);
  const reviewCompletionToken = raw.review_completion_token == null ? "" : requireString(raw.review_completion_token);
  if (!/^[A-Za-z0-9_-]{43}$/.test(reviewSessionToken)
    || (expectedReviewSessionToken && reviewSessionToken !== expectedReviewSessionToken)
    || (reviewCompletionToken && !/^[A-Za-z0-9_-]{43}$/.test(reviewCompletionToken))
    || (nextCursor ? Boolean(reviewCompletionToken) : !reviewCompletionToken)) throw invalidPayload();
  return {
    batchId: raw.batch_id,
    outcomeIds: [...raw.outcome_ids],
    selectionDigest: raw.selection_digest,
    manifestDigest: raw.manifest_digest,
    projects,
    proposal: { projectThreadAliases: aliases, tasks },
    preview: {
      batchId: raw.batch_id,
      selectionDigest: raw.selection_digest,
      manifestDigest: raw.manifest_digest,
      collisionPolicy: "current_wins",
      state: "preview",
      applied: false,
      tasks: previewTasks,
      counts,
    },
    applied: false,
    nextCursor,
    reviewSessionToken,
    reviewCompletionToken,
    reviewExpiresAt: timestampLabel(raw.review_expires_at),
  };
}

function normalizeArchaeologyBatchApplyReceipt(raw, bridge) {
  raw = requireRecord(raw);
  if (raw.batch_id !== bridge.batchId
    || !Array.isArray(raw.outcome_ids)
    || raw.outcome_ids.length !== bridge.outcomeIds.length
    || raw.outcome_ids.some((id, index) => id !== bridge.outcomeIds[index])
    || raw.selection_digest !== bridge.selectionDigest
    || raw.manifest_digest !== bridge.manifestDigest
    || raw.applied !== true) throw invalidPayload();
  return {
    batchId: raw.batch_id,
    outcomeIds: [...raw.outcome_ids],
    selectionDigest: raw.selection_digest,
    manifestDigest: raw.manifest_digest,
    applied: true,
    auditId: requireString(raw.audit_id),
  };
}

function normalizeProjectArchaeology(data) {
  data = requireRecord(data);
  if (typeof data.id !== "string" || !PROJECT_ARCHAEOLOGY_STATES.includes(data.state)) throw invalidPayload();
  const discovery = requireRecord(data.discovery);
  if (!PROJECT_ARCHAEOLOGY_DISCOVERY_STATES.includes(discovery.state) || discovery.metadata_only !== true || !Array.isArray(discovery.candidates) || discovery.candidates.length > 100) throw invalidPayload();
  const candidates = discovery.candidates.map(normalizeArchaeologyCandidate);
  if (new Set(candidates.map((candidate) => candidate.id)).size !== candidates.length) throw invalidPayload();

  const rawConfig = requireRecord(data.config);
  const rawSources = requireRecord(rawConfig.sources);
  if (!PROJECT_ARCHAEOLOGY_DEPTHS.includes(rawConfig.depth) || typeof rawSources.git !== "boolean" || typeof rawSources.docs !== "boolean" || typeof rawSources.codex_history !== "boolean" || ![1, 2].includes(rawConfig.max_concurrency)) throw invalidPayload();
  const selectedProjectIds = archaeologyStringList(rawConfig.selected_project_ids, MAX_PROJECT_ARCHAEOLOGY_SELECTION);
  if (new Set(selectedProjectIds).size !== selectedProjectIds.length) throw invalidPayload();

  if (!Array.isArray(data.runs) || data.runs.length > 100) throw invalidPayload();
  const runs = data.runs.map((rawRun) => {
    const run = requireRecord(rawRun);
    if (!PROJECT_ARCHAEOLOGY_RUN_STATES.includes(run.state)) throw invalidPayload();
    const totalUnits = run.total_units == null ? null : requireInteger(run.total_units);
    const completedUnits = requireInteger(run.completed_units);
    if (totalUnits != null && completedUnits > totalUnits) throw invalidPayload();
    return {
      id: requireString(run.id),
      projectId: requireString(run.project_id),
      state: run.state,
      phaseLabel: requireString(run.phase_label),
      completedUnits,
      totalUnits,
      outcomesFound: requireInteger(run.outcomes_found),
      sourcesExamined: requireInteger(run.sources_examined),
      error: typeof run.error === "string" ? run.error : "",
    };
  });

  let review = null;
  if (data.review != null) {
    review = normalizeArchaeologyReview(data.review);
  }

  const handoff = normalizeArchaeologyHandoff(data.handoff, timestampLabel);
  const updatedAt = data.updated_at == null ? null : timestampLabel(data.updated_at);
  const time = (value) => Date.parse(value?.iso || "");
  if (handoff?.batchId) {
    const modelUpdated = time(updatedAt);
    const handoffCreated = time(handoff.createdAt);
    const handoffUpdated = time(handoff.updatedAt);
    const progressUpdated = time(handoff.progress?.updatedAt);
    if (!Number.isFinite(modelUpdated)
      || !Number.isFinite(handoffCreated)
      || !Number.isFinite(handoffUpdated)
      || handoffCreated > handoffUpdated
      || handoffUpdated > modelUpdated
      || (handoff.progress?.updatedAt && (!Number.isFinite(progressUpdated) || progressUpdated > handoffUpdated))
      || handoff.tasks.some((task) => {
        const created = time(task.createdAt);
        const updated = time(task.updatedAt);
        return !Number.isFinite(created) || !Number.isFinite(updated) || created > updated || updated > handoffUpdated;
      })) throw invalidPayload();
  }
  if (review) {
    review.batchRelation = review.batchId
      ? review.batchId === handoff?.batchId ? "current" : "prior"
      : "unbound";
  }

  const controls = requireRecord(data.controls);
  if (typeof controls.can_start !== "boolean" || typeof controls.can_pause !== "boolean" || typeof controls.can_resume !== "boolean" || typeof controls.can_cancel !== "boolean") throw invalidPayload();
  return {
    id: data.id,
    state: data.state,
    discovery: {
      state: discovery.state,
      stage: (() => { const stage = discovery.stage || (discovery.state === "discovering" ? "reading_codex_metadata" : discovery.state); if (!PROJECT_ARCHAEOLOGY_DISCOVERY_STAGES.has(stage)) throw invalidPayload(); return stage; })(),
      startedAt: discovery.started_at == null ? null : timestampLabel(discovery.started_at),
      updatedAt: discovery.updated_at == null ? null : timestampLabel(discovery.updated_at),
      codexThreadsExamined: discovery.codex_threads_examined == null ? 0 : requireInteger(discovery.codex_threads_examined),
      workspacesGrouped: discovery.workspaces_grouped == null ? 0 : requireInteger(discovery.workspaces_grouped),
      telemetry: normalizeDiscoveryTelemetry(discovery.telemetry || discovery, { tasksExamined: discovery.codex_threads_examined, projectsGrouped: discovery.workspaces_grouped }),
      candidates,
      discoveredAt: discovery.discovered_at == null ? null : timestampLabel(discovery.discovered_at),
      sourceRootsScanned: requireInteger(discovery.source_roots_scanned),
      metadataOnly: true,
      error: typeof discovery.error === "string" ? discovery.error : "",
    },
    config: {
      selectedProjectIds,
      depth: rawConfig.depth,
      sources: { git: rawSources.git, docs: rawSources.docs, codexHistory: rawSources.codex_history },
      maxConcurrency: rawConfig.max_concurrency,
    },
    runs,
    review,
    capabilities: normalizeArchaeologyCapabilities(data.capabilities),
    handoff,
    controls: { canStart: controls.can_start, canPause: controls.can_pause, canResume: controls.can_resume, canCancel: controls.can_cancel },
    revision: requireInteger(data.revision),
    updatedAt,
  };
}

function archaeologyRevision(value) {
  if (!Number.isInteger(value) || value < 0) throw new CommonsAPIError("A current archaeology revision is required.", { code: "invalid_archaeology_revision" });
  return value;
}

function archaeologyConfigInput(config, baseRevision) {
  if (config === null || typeof config !== "object" || Array.isArray(config) || !Array.isArray(config.selectedProjectIds) || config.selectedProjectIds.length > MAX_PROJECT_ARCHAEOLOGY_SELECTION || !PROJECT_ARCHAEOLOGY_DEPTHS.includes(config.depth) || ![1, 2].includes(config.maxConcurrency)) throw new CommonsAPIError("Choose valid Project Archaeology settings.", { code: "invalid_archaeology_config" });
  const sources = config.sources;
  if (sources === null || typeof sources !== "object" || typeof sources.git !== "boolean" || typeof sources.docs !== "boolean" || typeof sources.codexHistory !== "boolean" || !config.selectedProjectIds.every((id) => typeof id === "string" && id)) throw new CommonsAPIError("Choose valid Project Archaeology settings.", { code: "invalid_archaeology_config" });
  return {
    selected_project_ids: [...config.selectedProjectIds],
    depth: config.depth,
    sources: { git: sources.git, docs: sources.docs, codex_history: sources.codexHistory },
    max_concurrency: config.maxConcurrency,
    base_revision: archaeologyRevision(baseRevision),
  };
}

function archaeologyResolutionInput(task, baseRevision) {
  const boundedID = (value, { required = false } = {}) => {
    if (typeof value !== "string" || value.length > 120 || (required && !value)) {
      throw new CommonsAPIError("Choose an exact uncertain Codex task to reconcile.", { code: "invalid_archaeology_resolution" });
    }
    return value;
  };
  return {
    base_revision: archaeologyRevision(baseRevision),
    job_id: boundedID(task?.jobId, { required: true }),
    thread_id: boundedID(task?.threadId || "", { required: true }),
    turn_id: boundedID(task?.turnId || "", { required: true }),
    resolution: "confirmed_stopped",
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
    async readCodexStatus(signal) {
      return safelyNormalize(normalizeCodexStatus, await transport.readCodexStatus(signal));
    },
    async startCodexPairing(signal) {
      return safelyNormalize(normalizeCodexStart, await transport.startCodexPairing(signal));
    },
    async pollCodexPairing(attemptID, signal) {
      if (typeof attemptID !== "string" || !attemptID) throw new CommonsAPIError("Codex sign-in attempt is required.", { code: "invalid_attempt" });
      return safelyNormalize(normalizeCodexPoll, await transport.pollCodexPairing(attemptID, signal));
    },
    async completeCodexProfile(attemptID, profile, signal) {
      if (typeof attemptID !== "string" || !attemptID) throw new CommonsAPIError("Codex sign-in attempt is required.", { code: "invalid_attempt" });
      const normalized = normalizeProfileInput({ display_name: profile?.displayName, handle: profile?.handle });
      return safelyNormalize(normalizeSession, await transport.completeCodexProfile({ attempt_id: attemptID, display_name: normalized.displayName, handle: normalized.handle }, signal));
    },
    async cancelCodexPairing(attemptID, signal) {
      if (typeof attemptID !== "string" || !attemptID) throw new CommonsAPIError("Codex sign-in attempt is required.", { code: "invalid_attempt" });
      return safelyNormalize(normalizeSession, await transport.cancelCodexPairing(attemptID, signal));
    },
    async login(secret, idempotencyKey, signal) {
      return safelyNormalize(normalizeSession, await transport.login(secret, idempotencyKey, signal));
    },
    async logout(csrfToken, idempotencyKey, signal) {
      return safelyNormalize(normalizeSession, await transport.logout(csrfToken, idempotencyKey, signal));
    },
    async updateProfile(profile, profileRevision, csrfToken, idempotencyKey, signal) {
      const normalized = normalizeProfileInput({ display_name: profile?.displayName, handle: profile?.handle });
      if (!Number.isInteger(profileRevision) || profileRevision < 1) throw new CommonsAPIError("A current profile revision is required.", { code: "invalid_profile_revision" });
      return safelyNormalize(normalizeSession, await transport.updateProfile({ display_name: normalized.displayName, handle: normalized.handle, base_revision: profileRevision }, csrfToken, idempotencyKey, signal));
    },
    async readProjectArchaeology(signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.readProjectArchaeology(signal));
    },
    async readProjectArchaeologyCatalog(query = {}, signal) {
      const sort = ["recent", "tasks", "name"].includes(query.sort) ? query.sort : "recent";
      const limit = query.limit ?? MAX_PROJECT_ARCHAEOLOGY_CATALOG_PAGE;
      return safelyNormalize(normalizeArchaeologyCatalogPage, await transport.readProjectArchaeologyCatalog({ ...query, sort, limit }, signal));
    },
    async readProjectArchaeologyBatches(query = {}, signal) {
      const limit = query.limit ?? DEFAULT_PROJECT_ARCHAEOLOGY_HISTORY_PAGE;
      return safelyNormalize(normalizeArchaeologyBatchPage, await transport.readProjectArchaeologyBatches({ ...query, limit }, signal));
    },
    async readProjectArchaeologyBatch(batchID, signal) {
      if (typeof batchID !== "string" || !batchID) throw new CommonsAPIError("Choose a Project Archaeology run.", { code: "invalid_archaeology_batch" });
      return safelyNormalize(normalizeArchaeologyBatchDetail, await transport.readProjectArchaeologyBatch(batchID, signal));
    },
    async readProjectArchaeologyBatchOutcomes(batchID, cursor, signal) {
      if (typeof batchID !== "string" || !batchID || typeof cursor !== "string" || !cursor) throw new CommonsAPIError("Choose an older proposal page.", { code: "invalid_archaeology_batch" });
      return safelyNormalize(normalizeArchaeologyOutcomePage, await transport.readProjectArchaeologyBatchOutcomes(batchID, { cursor }, signal));
    },
    async readInstallationStatus(signal) {
      return safelyNormalize(normalizeInstallationStatus, await transport.readInstallationStatus(signal));
    },
    async discoverProjectArchaeology(writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.discoverProjectArchaeology(writeOptions, signal));
    },
    async updateProjectArchaeologyConfig(config, baseRevision, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.updateProjectArchaeologyConfig(archaeologyConfigInput(config, baseRevision), writeOptions, signal));
    },
    async startProjectArchaeology(baseRevision, acknowledgeLargeBatch, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.startProjectArchaeology({ base_revision: archaeologyRevision(baseRevision), acknowledge_large_batch: acknowledgeLargeBatch === true }, writeOptions, signal));
    },
    async pauseProjectArchaeology(baseRevision, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.pauseProjectArchaeology({ base_revision: archaeologyRevision(baseRevision) }, writeOptions, signal));
    },
    async resumeProjectArchaeology(baseRevision, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.resumeProjectArchaeology({ base_revision: archaeologyRevision(baseRevision) }, writeOptions, signal));
    },
    async cancelProjectArchaeology(baseRevision, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.cancelProjectArchaeology({ base_revision: archaeologyRevision(baseRevision) }, writeOptions, signal));
    },
    async resolveProjectArchaeology(task, baseRevision, writeOptions, signal) {
      return safelyNormalize(normalizeProjectArchaeology, await transport.resolveProjectArchaeology(archaeologyResolutionInput(task, baseRevision), writeOptions, signal));
    },
    async previewProjectArchaeologyImport(outcomeID, writeOptions, signal) {
      if (typeof outcomeID !== "string" || !outcomeID) throw new CommonsAPIError("Choose a proposed outcome to preview.", { code: "invalid_archaeology_outcome" });
      return safelyNormalize(
        (value) => normalizeArchaeologyImportPreview(value, timestampLabel),
        await transport.previewProjectArchaeologyImport({ outcome_id: outcomeID }, writeOptions, signal),
      );
    },
    async applyHistoricalImport(previewBridge, confirmation, writeOptions, signal) {
      if (typeof previewBridge?.projectId !== "string" || !previewBridge.projectId) throw new CommonsAPIError("A canonical project is required.", { code: "invalid_historical_import" });
      let input;
      try {
        input = confirmedHistoricalImportRequest(previewBridge.request, previewBridge.preview, confirmation);
      } catch {
        throw new CommonsAPIError("Enter the exact server manifest digest to approve this import.", { code: "manifest_confirmation_required" });
      }
      return safelyNormalize(
        (value) => normalizeHistoricalImportResult(value, timestampLabel),
        await transport.applyHistoricalImport(previewBridge.projectId, input, writeOptions, signal),
      );
    },
    async previewProjectArchaeologyBatchImport(batchID, outcomeIDs, writeOptions, signal) {
      if (typeof batchID !== "string" || !batchID || !Array.isArray(outcomeIDs) || outcomeIDs.length < 1 || outcomeIDs.length > MAX_PROJECT_ARCHAEOLOGY_REVIEW_OUTCOMES || !outcomeIDs.every((value) => typeof value === "string" && value)) throw new CommonsAPIError("Choose one or more proposals from this run.", { code: "invalid_archaeology_selection" });
      const canonicalOutcomeIDs = [...outcomeIDs].sort();
      if (new Set(canonicalOutcomeIDs).size !== canonicalOutcomeIDs.length) throw new CommonsAPIError("Choose each proposal only once.", { code: "invalid_archaeology_selection" });
      const value = await transport.previewProjectArchaeologyBatchImport(batchID, { outcome_ids: canonicalOutcomeIDs }, writeOptions, signal);
      return safelyNormalize((raw) => normalizeArchaeologyBatchImport(raw, batchID, canonicalOutcomeIDs), value);
    },
    async previewProjectArchaeologyBatchImportPage(bridge, cursor, writeOptions, signal) {
      if (!bridge?.batchId || !Array.isArray(bridge.outcomeIds) || typeof cursor !== "string" || !cursor) throw new CommonsAPIError("Choose the next exact-diff page.", { code: "invalid_archaeology_selection" });
      const value = await transport.previewProjectArchaeologyBatchImportPage(bridge.batchId, cursor, { outcome_ids: bridge.outcomeIds, review_session_token: bridge.reviewSessionToken }, writeOptions, signal);
      const page = safelyNormalize((raw) => normalizeArchaeologyBatchImport(raw, bridge.batchId, bridge.outcomeIds, bridge.reviewSessionToken), value);
      if (page.selectionDigest !== bridge.selectionDigest || page.manifestDigest !== bridge.manifestDigest) throw new CommonsAPIError("The exact diff changed while it was being reviewed.", { code: "archaeology_preview_changed" });
      const existingProjects = bridge.projects || [];
      const known = new Set(existingProjects.map((project) => project.outcomeId));
      if (page.projects.some((project) => known.has(project.outcomeId))) throw new CommonsAPIError("Commons returned a duplicate exact-diff page.", { code: "invalid_payload" });
      const projects = [...existingProjects, ...page.projects];
      if (!page.nextCursor && projects.length !== bridge.outcomeIds.length) throw new CommonsAPIError("Commons did not attest every selected exact-diff page.", { code: "invalid_payload" });
      const proposalTasks = [...bridge.proposal.tasks, ...page.proposal.tasks];
      const previewTasks = [...bridge.preview.tasks, ...page.preview.tasks];
      const aliases = [...bridge.proposal.projectThreadAliases, ...page.proposal.projectThreadAliases];
      const counts = Object.fromEntries(Object.keys(bridge.preview.counts).map((key) => [key, (bridge.preview.counts[key] || 0) + (page.preview.counts[key] || 0)]));
      return { ...bridge, projects, proposal: { projectThreadAliases: aliases, tasks: proposalTasks }, preview: { ...bridge.preview, tasks: previewTasks, counts }, nextCursor: page.nextCursor, reviewCompletionToken: page.reviewCompletionToken, reviewExpiresAt: page.reviewExpiresAt };
    },
    async applyProjectArchaeologyBatchImport(bridge, confirmation, reviewed, writeOptions, signal) {
      if (reviewed !== true || confirmation !== bridge?.preview?.manifestDigest || bridge?.nextCursor || !bridge?.reviewCompletionToken) throw new CommonsAPIError("Complete the server-attested review, acknowledge every selected proposal, and enter the exact manifest digest.", { code: "manifest_confirmation_required" });
      const value = await transport.applyProjectArchaeologyBatchImport(bridge.batchId, {
        outcome_ids: bridge.outcomeIds,
        selection_digest: bridge.selectionDigest,
        manifest_digest: confirmation,
        review_acknowledged: true,
        review_completion_token: bridge.reviewCompletionToken,
      }, writeOptions, signal);
      return safelyNormalize((raw) => normalizeArchaeologyBatchApplyReceipt(raw, bridge), value);
    },
    async readPosts(query, signal) {
      return safelyNormalize(normalizePostsPage, await transport.readPosts(query, signal));
    },
    async readContributors(query, signal) { return safelyNormalize(normalizeContributors, await transport.readContributors(query, signal)); },
    async readNotifications(query, signal) { return safelyNormalize(normalizeNotifications, await transport.readNotifications(query, signal)); },
    async markNotificationRead(input, writeOptions, signal) { return safelyNormalize(normalizeMutation, await transport.markNotificationRead(input, writeOptions, signal)); },

    async readTopics(limit, signal) {
      return safelyNormalize(normalizeTopics, await transport.readTopics(limit, signal));
    },
    async readPost(postID, query, signal) {
      return safelyNormalize(normalizeOpenedPost, await transport.readPost(postID, query, signal));
    },
    async readCommentSource(commentID, signal) {
      return safelyNormalize(normalizeCommentSource, await transport.readCommentSource(commentID, signal));
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
    async changePerspectiveScope(input, writeOptions, signal) { return safelyNormalize(normalizeMutation, await transport.changePerspectiveScope(input, writeOptions, signal)); },
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

const fixtureHuman = Object.freeze({
  kind: "human",
  principal: "human:fixture",
  handle: "taylor",
  display_name: "Taylor Reed",
});
let fixtureHumanIdentity = { ...fixtureHuman };
const fixtureCodexState = { attemptID: "", pollCount: 0, needsProfile: false, authMethod: "recovery", profileRevision: 0 };
let fixtureArchaeology = archaeologyReadyFixture;
const fixtureNotificationReads = new Map();
const fixtureNotificationRecords = Object.freeze([{
  id: "NOTIFICATION-2411-61",
  recipient: fixtureHuman,
  source: { kind: "comment", post_ref: "POST-2411", comment_ref: "COMMENT-61" },
  actor: { kind: "agent", principal: "SES-4213", session: "SES-4213", handle: "release-scout", purpose: "Release scout" },
  snippet: "@taylor, can you verify the maintenance window before indexing resumes?",
  created_at: slice13FixtureTimes.mention,
}]);
const fixtureSlice13Contributors = Object.freeze([
  { kind: "agent", principal: "SES-4213", handle: "release-scout", session: "SES-4213", purpose: "Release scout", host: "fixture", project_relationship: "none", addressable: true, reachable: true, interpretation: "Addressable and currently connected; delivery is not guaranteed.", host_connected: true, execution: "not_running", last_activity: slice13FixtureTimes.mention },
  { kind: "agent", principal: "SES-4212", handle: "research-indexer", session: "SES-4212", purpose: "Research indexer", host: "fixture", project_relationship: "none", addressable: true, reachable: false, interpretation: "Addressable registry session; no current reachability evidence.", host_connected: false, execution: "not_running", last_activity: slice13FixtureTimes.indexReady },
]);

export const fixtureAdapter = {
  async readCodexStatus(signal) {
    return wait(normalizeCodexStatus({
      ...codexAuthFixtures.status,
      binding_state: fixtureHumanIdentity.principal === "human:local-admin" ? "bound" : codexAuthFixtures.status.binding_state,
      account_state: fixtureHumanIdentity.principal === "human:local-admin" ? "signed_in" : codexAuthFixtures.status.account_state,
    }), signal);
  },
  async startCodexPairing(signal) {
    fixtureCodexState.attemptID = codexAuthFixtures.start.attempt_id;
    fixtureCodexState.pollCount = 0;
    fixtureCodexState.needsProfile = false;
    return wait(normalizeCodexStart(codexAuthFixtures.start), signal);
  },
  async pollCodexPairing(attemptID, signal) {
    if (attemptID !== fixtureCodexState.attemptID) throw new CommonsAPIError("That Codex sign-in attempt is no longer available.", { code: "pairing_not_found", status: 404 });
    fixtureCodexState.pollCount += 1;
    const result = fixtureCodexState.pollCount < 2 ? codexAuthFixtures.poll_waiting : codexAuthFixtures.poll_needs_profile;
    fixtureCodexState.needsProfile = fixtureCodexState.pollCount >= 2;
    return wait(normalizeCodexPoll(result), signal);
  },
  async completeCodexProfile(attemptID, profile, signal) {
    if (attemptID !== fixtureCodexState.attemptID || !fixtureCodexState.needsProfile) throw new CommonsAPIError("This Codex setup step is no longer available.", { code: "profile_unavailable", status: 409 });
    fixtureHumanIdentity = { ...fixtureHumanIdentity, principal: "human:local-admin", handle: profile.handle, display_name: profile.displayName };
    fixtureCodexState.authMethod = "codex";
    fixtureCodexState.profileRevision = 1;
    fixtureCodexState.attemptID = "";
    fixtureCodexState.needsProfile = false;
    return wait(normalizeSession({
      authenticated: true,
      principal: { kind: "human", principal: fixtureHumanIdentity.principal, handle: fixtureHumanIdentity.handle, display_name: fixtureHumanIdentity.display_name },
      csrf_token: codexAuthFixtures.profile_session.csrf_token,
      auth_method: "codex",
      profile_revision: 1,
    }), signal);
  },
  async cancelCodexPairing(_attemptID, signal) {
    fixtureCodexState.attemptID = "";
    fixtureCodexState.needsProfile = false;
    return wait(normalizeSession({ authenticated: false, principal: null }), signal);
  },
  async readContributors(query, signal) {
    let records = [...fixtureSlice13Contributors, ...contractFixtures.people.items.map((item, index) => ({ kind: "agent", principal: item.session, handle: "agent-" + String(index + 1).padStart(6, "0"), session: item.session, purpose: item.purpose || "", host: item.host, project: item.project ? { id: item.project, name: item.project_name || item.project } : null, project_relationship: query.project === item.project ? "same_project" : "project", addressable: true, reachable: Boolean(item.host_connected), interpretation: item.host_connected ? "Addressable and currently connected; delivery is not guaranteed." : "Addressable registry session; no current reachability evidence.", host_connected: Boolean(item.host_connected), execution: item.execution, last_activity: item.last_activity }))];
    if (!query.project) records.push({ ...fixtureHumanIdentity, project_relationship: "none", addressable: true, reachable: false, interpretation: "Stable local human principal; browser session state is not recipient identity.", host_connected: false, execution: "not_applicable" });
    if (query.q) { const term = query.q.toLowerCase(); records = records.filter((item) => (item.handle + " " + item.purpose).toLowerCase().includes(term)); }
    if (query.project) records = records.filter((item) => item.project?.id === query.project);
    const result = page(records, query.cursor, query.limit);
    return wait(normalizeContributors({ limit: query.limit, items: result.items, next_cursor: result.nextCursor }), signal);
  },
  async readSession(signal) {
    return wait(normalizeSession({ authenticated: true, principal: { ...fixtureHumanIdentity }, csrf_token: "fixture-csrf", auth_method: fixtureCodexState.authMethod, profile_revision: fixtureCodexState.profileRevision }), signal);
  },
  async login(_secret, _idempotencyKey, signal) {
    fixtureCodexState.authMethod = "recovery";
    fixtureCodexState.profileRevision = fixtureHumanIdentity.principal === "human:local-admin" ? 1 : 0;
    return wait(normalizeSession({ authenticated: true, principal: { ...fixtureHumanIdentity }, csrf_token: "fixture-csrf", auth_method: "recovery", profile_revision: fixtureCodexState.profileRevision }), signal);
  },
  async logout(_csrfToken, _idempotencyKey, signal) {
    return wait(normalizeSession({ authenticated: false, principal: null }), signal);
  },
  async updateProfile(profile, _profileRevision, _csrfToken, _idempotencyKey, signal) {
    fixtureHumanIdentity = { ...fixtureHumanIdentity, handle: profile.handle, display_name: profile.displayName };
    fixtureCodexState.authMethod = "codex";
    fixtureCodexState.profileRevision = Math.max(1, fixtureCodexState.profileRevision);
    return wait(normalizeSession({ authenticated: true, principal: { ...fixtureHumanIdentity }, csrf_token: "fixture-csrf", auth_method: "codex", profile_revision: fixtureCodexState.profileRevision }), signal);
  },
  async readProjectArchaeology(signal) {
    return wait(fixtureArchaeology, signal);
  },
  async discoverProjectArchaeology(_writeOptions, signal) {
    fixtureArchaeology = archaeologyReadyFixture;
    return wait(fixtureArchaeology, signal);
  },
  async updateProjectArchaeologyConfig(config, _baseRevision, _writeOptions, signal) {
    fixtureArchaeology = { ...fixtureArchaeology, config, revision: fixtureArchaeology.revision + 1 };
    return wait(fixtureArchaeology, signal);
  },
  async startProjectArchaeology(_baseRevision, _acknowledgeLargeBatch, _writeOptions, signal) {
    fixtureArchaeology = { ...archaeologyHandoffFixture, config: fixtureArchaeology.config };
    return wait(fixtureArchaeology, signal);
  },
  async pauseProjectArchaeology() { throw new CommonsAPIError("Direct Codex tasks cannot be paused from Commons.", { code: "unavailable", status: 503 }); },
  async resumeProjectArchaeology() { throw new CommonsAPIError("Direct Codex tasks cannot be resumed from Commons.", { code: "unavailable", status: 503 }); },
  async cancelProjectArchaeology() { throw new CommonsAPIError("Direct Codex tasks cannot be cancelled from Commons.", { code: "unavailable", status: 503 }); },
  async resolveProjectArchaeology(task, _baseRevision, _writeOptions, signal) {
    if (task?.state !== "uncertain" || !task.availableActions?.includes("resolve")) throw new CommonsAPIError("This task cannot be reconciled.", { code: "unavailable", status: 409 });
    const remaining = fixtureArchaeology.handoff.tasks.map((item) => item.jobId === task.jobId ? { ...item, state: "interrupted", error: "Human confirmed this exact Codex task is stopped.", availableActions: [] } : item);
    fixtureArchaeology = {
      ...fixtureArchaeology,
      state: "draft",
      revision: fixtureArchaeology.revision + 1,
      handoff: { ...fixtureArchaeology.handoff, state: "canceled", tasks: remaining, allowedActions: [] },
      controls: { ...fixtureArchaeology.controls, canStart: true },
    };
    return wait(fixtureArchaeology, signal);
  },

  async previewProjectArchaeologyImport(outcomeID, _writeOptions, signal) {
    if (outcomeID !== "OUT-1") throw new CommonsAPIError("Choose a proposed outcome to preview.", { code: "invalid_archaeology_outcome" });
    const sourceDigest = `sha256:${"a".repeat(64)}`;
    const manifestDigest = `sha256:${"b".repeat(64)}`;
    const occurredAt = "2026-08-12T12:30:00Z";
    const source = { kind: "git", stable_id: "git:commit:fixture", digest: sourceDigest, occurred_at: occurredAt };
    const request = {
      schema_version: 1, batch_id: "archaeology-codex-commons", source_digest: sourceDigest,
      confirm_source_digest: "", confirm_manifest_digest: "", collision_policy: "current_wins",
      project_thread_aliases: [],
      tasks: [{
        key: "history-task-1", priority: 0, title: "Preserve exact historian evidence",
        description: "Bind this durable result to the evidence reviewed by the human.",
        acceptance: "Canonical history retains exact source identity.", state: "done", source,
        attributions: [{ session: "019ff-fixture-historian", role: "implementer", confidence: "verified", source: { ...source, kind: "codex_turn", stable_id: "thread:019ff-fixture-historian/turn:turn-1" } }],
        events: [{ key: "history-event-1", kind: "completed", summary: "Historian verified this result.", session: "019ff-fixture-historian", confidence: "verified", source }],
      }],
    };
    const preview = {
      batch_id: "archaeology-codex-commons", source_digest: sourceDigest, manifest_digest: manifestDigest,
      collision_policy: "current_wins", state: "preview", applied: false,
      tasks: [{ key: "history-task-1", id: "TASK-PREVIEW-1", disposition: "created" }],
      counts: { project_thread_aliases: 0, tasks: 1, attributions: 1, events: 1, created: 1, skipped_current: 0, replayed: 0 },
    };
    return wait(normalizeArchaeologyImportPreview({ project_id: "codex-commons", request, preview }, timestampLabel), signal);
  },
  async applyHistoricalImport(bridge, confirmation, _writeOptions, signal) {
    confirmedHistoricalImportRequest(bridge.request, bridge.preview, confirmation);
    return wait({ ...bridge.preview, state: "applied", applied: true, recordedAt: timestampLabel(new Date().toISOString()) }, signal);
  },
  async readNotifications(query, signal) {
    let records = fixtureNotificationRecords.map((item) => fixtureNotificationReads.has(item.id) ? { ...item, read_at: fixtureNotificationReads.get(item.id) } : item);
    if (query.unread) records = records.filter((item) => !item.read_at);
    const result = page(records, query.cursor, query.limit);
    return wait(normalizeNotifications({ items: result.items, next_cursor: result.nextCursor, unread_count: records.filter((item) => !item.read_at).length }), signal);
  },
  async markNotificationRead(input, _writeOptions, signal) {
    if (!fixtureNotificationRecords.some((item) => item.id === input.id)) throw new CommonsAPIError("The requested Commons record was not found.", { code: "not_found", status: 404 });
    fixtureNotificationReads.set(input.id, new Date().toISOString());
    return wait({ id: input.id, revision: 0, persisted: true }, signal);
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
  async readCommentSource(commentID, signal) {
    for (const post of Object.values(postFixtures.opened)) {
      const comment = post.comments.find((item) => item.id === commentID);
      if (comment) return wait(normalizeCommentSource({ post_ref: post.id, comment }), signal);
    }
    throw new CommonsAPIError("The requested Commons record was not found.", { code: "not_found", status: 404 });
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
  async changePerspectiveScope(input, _writeOptions, signal) { return wait({ id: input.ref, revision: input.base_revision + 1, persisted: true }, signal); },

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
