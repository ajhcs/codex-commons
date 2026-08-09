/**
 * Frontend boundary for the frozen Slice 7/8 JSON contracts.
 *
 * The prototype intentionally keeps transport-shaped snake_case records here.
 * Screens consume only the normalized view models returned by data/adapter.js.
 * That keeps fixture data honest and gives the real HTTP client one seam to replace.
 *
 * @typedef {'high'|'medium'|'low'} AttentionSeverity
 * @typedef {'executing'|'not_running'} ExecutionState
 * @typedef {{kind:string, ref:string}} Destination
 * @typedef {{value:string, label?:string, count:number}} FacetCount
 * @typedef {{sources:FacetCount[], owners:FacetCount[], severities:FacetCount[], projects:FacetCount[], owners_truncated:boolean, projects_truncated:boolean}} AttentionFacets
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
