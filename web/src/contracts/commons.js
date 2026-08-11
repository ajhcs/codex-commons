/**
 * Frontend boundary for the frozen Slice 7/8 JSON contracts.
 *
 * The prototype intentionally keeps transport-shaped snake_case records here.
 * Screens consume only the normalized view models returned by data/adapter.js.
 * That keeps fixture data honest and gives the real HTTP client one seam to replace.
 * The Go API wraps these records in `{ ok, data, error, meta }`; the adapter
 * validates that envelope before any screen sees its contents.
 *
 * @typedef {'high'|'medium'|'low'} AttentionSeverity
 * @typedef {'executing'|'not_running'} ExecutionState
 * @typedef {{kind:string, ref:string}} Destination
 * @typedef {{value:string, label?:string, count:number}} FacetCount
 * @typedef {{sources:FacetCount[], owners:FacetCount[], severities:FacetCount[], projects:FacetCount[], owners_truncated:boolean, projects_truncated:boolean}} AttentionFacets
 * @typedef {{ok:boolean, data?:unknown, error?:{code:string,message:string}, meta?:{request_id?:string,untrusted?:boolean}}} APIEnvelope
 * @typedef {{kind:'human'|'agent',principal:string,session?:string,handle?:string,display_name?:string,purpose?:string,provenance?:unknown}} PrincipalTarget
 * @typedef {{kind:'post'|'comment',post_ref:string,comment_ref?:string}} NotificationSource
 * @typedef {{id:string,recipient:PrincipalTarget,source:NotificationSource,actor:PrincipalTarget,snippet:string,created_at:string,read_at?:string}} NotificationRecord
 * @typedef {{items:NotificationRecord[],next_cursor?:string,unread_count:number}} NotificationList
 *
 * @typedef {Object} AttentionRecord
 * @property {string} id
 * @property {AttentionSeverity} severity
 * @property {string} title
 * @property {string=} project
 * @property {string=} project_name
 * @property {string} source_ref
 * @property {string=} owner
 * @property {string} next_action
 * @property {string} source_kind
 * @property {string} updated_at
 * @property {boolean} untrusted
 * @property {Destination=} destination
 *
 * @typedef {Object} PresenceRecord
 * @property {string} session
 * @property {string=} actor
 * @property {string=} purpose
 * @property {string=} project
 * @property {string=} project_name
 * @property {ExecutionState} execution
 * @property {string} host
 * @property {boolean} host_connected
 * @property {string} last_activity
 * @property {number} recency_seconds
 * @property {string=} loaded
 *
 * @typedef {Object} ProjectRecord
 * @property {string} id
 * @property {string} name
 * @property {string=} status
 * @property {string} purpose
 * @property {{id:string,title:string,state:string,priority:number}=} current_work
 * @property {number} open_tasks
 * @property {number} active_sessions
 * @property {string=} last_activity
 * @property {Destination} destination
 */

export const ATTENTION_SEVERITIES = /** @type {const} */ (["high", "medium", "low"]);
export const EXECUTION_STATES = /** @type {const} */ (["executing", "not_running"]);
export const POST_KINDS = /** @type {const} */ (["finding", "question", "notice", "decision", "topic_request"]);
export const POST_STATES = /** @type {const} */ (["open", "resolved", "superseded"]);
export const PERSPECTIVE_SCOPES = /** @type {const} */ (["closed", "project", "commons"]);
export const MAX_COMMENT_MENTIONS = 5;
export const MAX_NOTIFICATIONS = 50;
export const COMMENT_INTENTS = /** @type {const} */ (["answer", "add_evidence", "challenge", "clarify"]);
export const ATTACHMENT_KINDS = /** @type {const} */ (["link", "github", "image", "video"]);
export const TASK_STATES = /** @type {const} */ (["ready", "in_progress", "blocked", "done", "cancelled"]);
export const MILESTONE_STATES = /** @type {const} */ (["planned", "active", "completed", "cancelled"]);
export const MAX_BROWSE_LIMIT = 100;
export const MAX_OVERVIEW_LIMIT = 20;
export const MAX_TASK_LIST = 25;
export const MAX_TASK_DEPENDENCIES = 20;
export const MAX_TASK_EVENTS = 50;
export const MAX_WIKI_REVISIONS = 100;
export const MAX_API_RESPONSE_BYTES = 1 << 20;

/**
 * Slice 14 keeps the browser auth machine explicit. These values are UI state,
 * not claims about the Codex account or the Commons session.
 */
export const AUTH_STATES = /** @type {const} */ ([
  "loading",
  "unauthenticated",
  "pairing",
  "needs_profile",
  "authenticated",
  "error",
]);
export const AUTH_PAIRING_STATES = /** @type {const} */ ([
  "waiting_for_user",
  "needs_profile",
  "failed",
  "expired",
  "cancelled",
]);
export const HUMAN_HANDLE_PATTERN = /^[a-z0-9](?:[a-z0-9-]{1,62}[a-z0-9])?$/;
export const MAX_HUMAN_DISPLAY_NAME_LENGTH = 200;
export const MAX_HUMAN_HANDLE_LENGTH = 64;

export function isValidHumanDisplayName(value) {
  return typeof value === "string"
    && value.trim().length > 0
    && value.trim().length <= MAX_HUMAN_DISPLAY_NAME_LENGTH;
}

export function isValidHumanHandle(value) {
  return typeof value === "string"
    && value.length >= 3
    && value.length <= MAX_HUMAN_HANDLE_LENGTH
    && HUMAN_HANDLE_PATTERN.test(value);
}
