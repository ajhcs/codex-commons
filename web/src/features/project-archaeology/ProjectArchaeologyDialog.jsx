import { useEffect, useMemo, useRef, useState } from "react";
import { copyText, manualCopyShortcut } from "../../browser/copyText.js";
import CommonsMark from "../../components/CommonsMark.jsx";
import BookOpen from "../../icons/BookOpen.tsx";
import Branch from "../../icons/Branch.tsx";
import CheckCircle from "../../icons/CheckCircle.tsx";
import Clock from "../../icons/Clock.tsx";
import Copy from "../../icons/Copy.tsx";
import Search from "../../icons/Search.tsx";
import History from "../../icons/History.tsx";
import FileDocument from "../../icons/FileDocument.tsx";
import Folder from "../../icons/Folder.tsx";
import Stop from "../../icons/Stop.tsx";
import {
  ARCHAEOLOGY_DEPTHS,
  archaeologyConfigVersion,
  archaeologyBatchIsTerminal,
  archaeologyTaskIsActive,
  archaeologyTaskPresentation,
  archaeologyView,
  canStartArchaeology,
  configFromModel,
  formatDurationRange,
  memberFacts,
  discoveryProgressText,
  handoffProgress,
  isProjectCandidateSelectable,
  sortProjectCandidates,
  selectedSourceCount,
  sourceLabels,
} from "./projectArchaeologyState.js";
import "./project-archaeology.css";

const depthCopy = {
  quick: "A light pass over the clearest outcomes and recent history.",
  standard: "A balanced review of outcomes, sources, and contributing sessions.",
  deep: "A broader, slower review that follows more history and uncertain links.",
};
const costCopy = { low: "Lower model use", medium: "Typical model use", high: "Higher model use" };

const sourceOptions = [
  ["git", "Git", Branch],
  ["docs", "Documentation", FileDocument],
  ["codexHistory", "Codex history", BookOpen],
];

function identityLabel(identity) {
  const name = identity?.displayName || "Commons member";
  return identity?.handle ? `${name} · @${identity.handle}` : name;
}

function candidateName(candidates, id) {
  return candidates.find((candidate) => candidate.id === id)?.name || id;
}

function taskCandidateName(candidates, task) {
  return candidateName(candidates, task.candidateId || task.projectId);
}

function Intro({ identity, model, busy, error, onDiscover, onSkip }) {
  const discovery = model?.capabilities?.discovery;
  return (
    <div className="archaeology-intro">
      <div className="archaeology-identity" aria-label={`Signed in as ${identityLabel(identity)}`}>
        <CommonsMark state="offered" size="large" />
        <div><strong>{identityLabel(identity)}</strong><small>Codex identity connected</small></div>
      </div>
      <div className="archaeology-intro-copy">
        <span>Project Archaeology</span>
        <h2 id="archaeology-title">Bring your Codex work into Commons</h2>
        <p id="archaeology-description">Choose from projects known to your paired Codex App Server. Commons reads only bounded metadata until you explicitly start tasks.</p>
      </div>
      <div className="archaeology-privacy-note"><strong>You stay in control</strong><span>Starting creates one ordinary Codex task per selected project. Results remain proposals until you review and apply them.</span></div>
      {!error && discovery?.available === false ? <p className="archaeology-message" role="status">{discovery.reason || "Project discovery is not configured on this installation."}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer">
        <button className="secondary-button" type="button" onClick={onSkip}>Not now</button>
        <button className="primary-button" type="button" disabled={busy || discovery?.available !== true} onClick={onDiscover}>{busy ? "Finding projects…" : "Choose projects"}</button>
      </footer>
    </div>
  );
}

function DiscoveryState({ failed, error, onDiscover, onSkip }) {
  return (
    <div className="archaeology-centered-state" role={failed ? "alert" : "status"} aria-live="polite">
      <span className={`archaeology-state-mark${failed ? " is-error" : ""}`} aria-hidden="true"><Folder /></span>
      <h2 id="archaeology-title">{failed ? "Project discovery stopped" : "Looking for project signals"}</h2>
      <p id="archaeology-description">{failed ? error || "Commons could not finish the metadata-only project check." : "Checking names, Git and documentation signals, Codex-history availability, and recent activity. No file or conversation contents are being read."}</p>
      <div className="archaeology-inline-actions">
        <button className="secondary-button" type="button" onClick={onSkip}>Skip for now</button>
        {failed ? <button className="primary-button" type="button" onClick={onDiscover}>Try again</button> : null}
      </div>
    </div>
  );
}

function ReadingState({ error, onRetry, onClose }) {
  return (
    <div className="archaeology-centered-state" role={error ? "alert" : "status"} aria-live="polite">
      <span className={`archaeology-state-mark${error ? " is-error" : ""}`} aria-hidden="true"><Folder /></span>
      <h2 id="archaeology-title">{error ? "Project history is unavailable" : "Checking project history"}</h2>
      <p id="archaeology-description">{error || "Reading the current server state and capability facts. No discovery or import has started."}</p>
      <div className="archaeology-inline-actions">
        <button className="secondary-button" type="button" onClick={onClose}>Close</button>
        {error ? <button className="primary-button" type="button" onClick={onRetry}>Try again</button> : null}
      </div>
    </div>
  );
}


function useElapsed(active, startedAt) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return undefined;
    setNow(Date.now());
    const timer = globalThis.setInterval(() => setNow(Date.now()), 1000);
    return () => globalThis.clearInterval(timer);
  }, [active, startedAt]);
  const start = Date.parse(startedAt?.iso || startedAt || "");
  return Number.isFinite(start) ? Math.max(0, Math.floor((now - start) / 1000)) : 0;
}

function CandidateList({ candidates, discovery, config, disabled, refreshing, onChange, onRefresh }) {
  const [query, setQuery] = useState("");
  const [sort, setSort] = useState("recent");
  const elapsed = useElapsed(refreshing, discovery?.startedAt);
  const normalizedQuery = query.trim().toLowerCase();
  const visible = candidates.filter((candidate) => !normalizedQuery || [candidate.name, candidate.repositoryLabel].some((value) => value?.toLowerCase().includes(normalizedQuery)));
  const sorted = sortProjectCandidates(visible, sort);
  const selected = new Set(config.selectedProjectIds);
  const selectable = visible.filter((candidate) => isProjectCandidateSelectable(candidate));
  const allSelectable = candidates.filter((candidate) => isProjectCandidateSelectable(candidate));
  const selectedVisible = selectable.filter((candidate) => selected.has(candidate.id));
  const listRef = useRef(null);
  const scrollTopRef = useRef(0);
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = scrollTopRef.current;
  }, [candidates]);
  const toggleVisible = () => {
    const next = new Set(config.selectedProjectIds);
    if (selectable.length && selectedVisible.length === selectable.length) selectable.forEach((candidate) => next.delete(candidate.id));
    else selectable.forEach((candidate) => next.add(candidate.id));
    onChange({ ...config, selectedProjectIds: [...next] });
  };
  const selectAll = () => onChange({ ...config, selectedProjectIds: allSelectable.map((candidate) => candidate.id) });
  const toggle = (id) => onChange({ ...config, selectedProjectIds: selected.has(id) ? config.selectedProjectIds.filter((value) => value !== id) : [...config.selectedProjectIds, id] });
  const refreshLabel = refreshing ? ({ queued: "Refresh queued", reading_codex_metadata: "Reading Codex task metadata", persisting_catalog: "Organizing projects", failed: "Refresh needs attention" })[discovery?.stage] || "Refreshing projects" : "Refresh Codex projects";
  const grouped = sort === "recent" && !normalizedQuery && sorted.length > 6
    ? [["Recent", sorted.slice(0, 6)], ["All projects", sorted.slice(6)]]
    : [["All projects", sorted]];
  return (
    <section className="archaeology-projects" aria-labelledby="archaeology-projects-title">
      <div className="archaeology-catalog-toolbar">
        <label className="archaeology-search"><Search aria-hidden="true" /><span className="sr-only">Search projects</span><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search Codex projects" /></label>
        <label className="archaeology-sort"><span>Sort:</span><select aria-label="Sort projects" value={sort} onChange={(event) => setSort(event.target.value)}><option value="recent">Recent activity</option><option value="tasks">Most Codex tasks</option><option value="name">Name</option></select></label>
        <span>{config.selectedProjectIds.length} selected</span>
        <div className="archaeology-selection-actions"><button type="button" disabled={disabled || !selectable.length} onClick={toggleVisible}>{selectable.length && selectedVisible.length === selectable.length ? "Clear visible" : "Select visible"}</button><button type="button" disabled={disabled || !allSelectable.length} onClick={selectAll}>Select all</button></div>
      </div>
      <div className="archaeology-section-heading"><div><h3 id="archaeology-projects-title">Codex projects</h3><p>{visible.length} of {candidates.length} shown</p></div><button type="button" aria-disabled={disabled || refreshing} onClick={() => { if (!disabled && !refreshing) onRefresh(); }} aria-busy={refreshing}><History aria-hidden="true" />{refreshLabel}</button></div>
      <div className={`archaeology-operation-status${refreshing ? " is-active" : " is-ready"}`}><span className="archaeology-operation-icon"><History aria-hidden="true" /></span><strong role="status" aria-live="polite" aria-atomic="true">{discoveryProgressText(discovery)}{refreshing ? <span aria-hidden="true"> · {elapsed}s</span> : null}</strong><small>{refreshing ? "Your current project list stays visible while Commons refreshes it." : discovery?.updatedAt?.relative ? `Updated ${discovery.updatedAt.relative}` : "Catalog ready"}</small></div>
      {sorted.length ? <div ref={listRef} className="archaeology-candidate-list" onScroll={(event) => { scrollTopRef.current = event.currentTarget.scrollTop; }}>{grouped.map(([group, rows]) => <section className="archaeology-candidate-group" key={group} aria-labelledby={`archaeology-group-${group.replaceAll(" ", "-").toLowerCase()}`}><h4 id={`archaeology-group-${group.replaceAll(" ", "-").toLowerCase()}`}>{group}</h4>{rows.map((candidate) => {
        const checked = selected.has(candidate.id);
        const sources = sourceLabels(candidate.signals);
        const stale = !isProjectCandidateSelectable(candidate);
        return (
          <label key={candidate.id} className={["archaeology-candidate", checked ? "is-selected" : ""].filter(Boolean).join(" ")}>
            <input type="checkbox" checked={checked} disabled={disabled || stale} onChange={() => toggle(candidate.id)} />
            <span className="archaeology-candidate-mark" aria-hidden="true"><Folder /></span>
            <span className="archaeology-candidate-copy"><strong>{candidate.name}</strong><small>{candidate.repositoryLabel || "Codex project"}</small><span>{stale ? "Legacy reference · refresh metadata to select" : sources.join(" · ") || "Metadata only"} · {candidate.codexThreadCount} Codex task{candidate.codexThreadCount === 1 ? "" : "s"}</span></span>
            <span className="archaeology-candidate-estimate"><span><Clock aria-hidden="true" />{formatDurationRange(candidate.estimate)}</span><small>{candidate.lastActivity ? `Active ${candidate.lastActivity.relative}` : costCopy[candidate.estimate?.relativeCost] || "Time varies"}</small></span>
          </label>
        );
      })}</section>)}</div> : <div className="archaeology-empty"><strong>No matching projects</strong><span>Try another name or clear the search.</span></div>}
    </section>
  );
}

function Configuration({ config, disabled, onChange }) {
  function updateSources(source, value) {
    onChange({ ...config, sources: { ...config.sources, [source]: value } });
  }
  return (
    <div className="archaeology-configuration">
      <fieldset disabled={disabled}>
        <legend>Depth</legend>
        <div className="archaeology-segmented">
          {ARCHAEOLOGY_DEPTHS.map((depth) => <button key={depth} type="button" aria-pressed={config.depth === depth} onClick={() => onChange({ ...config, depth })}>{depth[0].toUpperCase() + depth.slice(1)}</button>)}
        </div>
        <p>{depthCopy[config.depth]}</p>
      </fieldset>
      <fieldset disabled={disabled}>
        <legend>Sources to read after you start</legend>
        <div className="archaeology-source-options">
          {sourceOptions.map(([key, label, Icon]) => <label key={key}><input type="checkbox" checked={config.sources[key]} onChange={(event) => updateSources(key, event.target.checked)} /><Icon aria-hidden="true" /><span>{label}</span></label>)}
        </div>
        <p>Selected project content stays inside this Commons exploration and becomes a proposal, never an automatic import.</p>
      </fieldset>
      <fieldset disabled={disabled}>
        <legend>Historians at once</legend>
        <div className="archaeology-segmented archaeology-segmented--short">
          {[1, 2].map((count) => <button key={count} type="button" aria-pressed={config.maxConcurrency === count} onClick={() => onChange({ ...config, maxConcurrency: count })}>{count}</button>)}
        </div>
        <p>One historian explores each project. At most two run at the same time.</p>
      </fieldset>
    </div>
  );
}

function Configure({ model, config, busy, launchingCount, refreshing, error, onChange, onDiscover, onStart, onSkip }) {
  const candidates = model?.discovery?.candidates || [];
  const valid = canStartArchaeology(config, candidates) && model?.controls?.canStart === true;
  const launch = model?.capabilities?.taskLaunch;
  const projectCount = config.selectedProjectIds.length;
  const projectNoun = projectCount === 1 ? "project" : "projects";
  const startLabel = `Add ${projectCount} ${projectNoun} to Commons and start up to 2 named Codex tasks`;
  return (
    <div className="archaeology-configure">
      <header className="archaeology-content-heading"><div><span>Project Archaeology</span><h2 id="archaeology-title" tabIndex="-1">Choose your Codex projects</h2><p id="archaeology-description">Select the work Commons should understand. Starting adds only empty project and topic shells, then queues named Codex tasks. It imports no Tasks or history.</p></div></header>
      <div className="archaeology-workspace">
        <aside className="archaeology-trust-panel">
          <CommonsMark state="offered" size="large" />
          <h3>Codex stays in control</h3>
          <p>Projects come from the App Server paired with this browser. Commons receives labels and capability facts here—not raw paths or thread bodies.</p>
          <dl><div><dt>Selected</dt><dd>{config.selectedProjectIds.length} projects</dd></div><div><dt>Mode</dt><dd>{config.depth} review</dd></div><div><dt>Sources</dt><dd>{selectedSourceCount(config)} enabled</dd></div><div><dt>Scheduler</dt><dd>Max 2 active</dd></div></dl>
        </aside>
        <div className="archaeology-catalog-panel"><CandidateList candidates={candidates} discovery={model.discovery} config={config} disabled={busy} refreshing={refreshing} onChange={onChange} onRefresh={onDiscover} /></div>
      </div>
      <details className="archaeology-advanced"><summary>Advanced settings <span>{config.depth} · {selectedSourceCount(config)} sources · {config.maxConcurrency} at once</span></summary><Configuration config={config} disabled={busy} onChange={onChange} /></details>
      <div className="archaeology-truth-note"><strong>{startLabel}</strong><span>Only empty Commons shells are added at start. No Tasks or history are imported; human review remains required before any canonical history import.</span></div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {launch?.available === false ? <p className="archaeology-message" role="status">{launch.reason || "Direct Codex task launch is not available on this installation."}</p> : null}
      {launchingCount > 0 ? <div className="archaeology-launch-ack" role="status" aria-live="polite" aria-atomic="true"><span className="archaeology-operation-icon"><BookOpen aria-hidden="true" /></span><div><strong>Preparing {launchingCount} {launchingCount === 1 ? "project" : "projects"}</strong><small>Your selection is frozen while Commons reserves one durable scheduler row per project.</small></div></div> : null}
      <footer className="archaeology-footer archaeology-footer--between"><button className="secondary-button" type="button" disabled={busy} onClick={onSkip}>Not now</button><button className="primary-button archaeology-start-button" type="button" aria-busy={launchingCount > 0} disabled={busy || refreshing || !valid || launch?.available !== true} onClick={() => onStart(config)}>{launchingCount > 0 ? `Preparing ${launchingCount} ${launchingCount === 1 ? "project" : "projects"}` : startLabel}</button></footer>
    </div>
  );
}


function UpdateStatus({ status }) {
  const checked = status?.lastCheckedAt instanceof Date
    ? status.lastCheckedAt.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" })
    : "not yet checked";
  const copy = {
    hidden: "Updates paused while this tab was hidden",
    checking: "Checking for task updates…",
    restored: "Updates restored",
    paused: "Updates paused",
  }[status?.state] || "Task updates are available";
  return <p className={`archaeology-update-status is-${status?.state || "idle"}`} role="status">{copy}<span>Last checked {checked}</span></p>;
}

function formatTaskDuration(durationMs) {
  if (!Number.isInteger(durationMs)) return "";
  if (durationMs < 1000) return `${durationMs} ms`;
  const seconds = Math.round(durationMs / 1000);
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

function Handoff({ model, busy, error, updateStatus, onRefresh, onCancel, onReview, onNewRun, onClose }) {
  const handoff = model?.handoff || {};
  const tasks = handoff.tasks || [];
  const candidates = model?.discovery?.candidates || [];
  const progress = handoffProgress(handoff);
  const nativeBatch = Boolean(handoff.batchId);
  const active = tasks.some((task) => task.jobId && archaeologyTaskIsActive(task));
  const terminal = archaeologyBatchIsTerminal(handoff);
  const elapsed = useElapsed(active, handoff.createdAt);
  const [copyState, setCopyState] = useState("");
  const heading = !nativeBatch ? "Legacy historian records" : progress.queued + progress.starting > 0 ? "Starting Codex tasks" : terminal ? (handoff.state === "canceled" ? "Project history run canceled" : handoff.state === "attention" ? "Project history needs attention" : "Project history run complete") : "Project history is underway";
  async function copyID(value, label) {
    const copied = await copyText(value);
    setCopyState(copied ? `${label} copied.` : `Copy isn’t available here. Select the ID and press ${manualCopyShortcut()} to copy it.`);
  }
  const canCancel = nativeBatch && model?.controls?.canCancel === true && ["queued", "running"].includes(handoff.state);
  return (
    <div className="archaeology-runs archaeology-handoff auth-content-transition">
      <header className="archaeology-content-heading"><div><span>Codex tasks</span><h2 id="archaeology-title" tabIndex="-1">{heading}</h2><p id="archaeology-description">{!nativeBatch ? "These pre-scheduler rows are retained for audit only. Their earlier status is not a current execution claim." : progress.queued + progress.starting > 0 ? "Commons is creating one ordinary named Codex historian task for each selected project." : "You can close this window. Commons preserves every literal scheduler state and imports nothing without human review."}</p></div></header>
      {nativeBatch ? <div className="archaeology-launch-summary" role="status" aria-live="polite" aria-atomic="true"><strong>{active ? `Preparing ${progress.total} projects` : `${progress.total} projects in this run`}</strong><span>{progress.queued + progress.preparing} queued · {progress.active} active · {progress.ready} reports · {progress.attention} attention<span aria-hidden="true">{elapsed ? ` · ${elapsed}s` : ""}</span></span></div> : <div className="archaeology-launch-summary"><strong>{progress.total} legacy historian records</strong><span>Status not reconciled</span></div>}
      <p className="archaeology-usage-note">{nativeBatch ? "Max 2 active. Task names are primary; job, batch, candidate, project, thread, and turn IDs remain copyable audit provenance." : "No retry, pause, cancel, or archive controls are available for legacy rows."}</p>
      {nativeBatch ? <UpdateStatus status={updateStatus} /> : null}
      {progress.attention > 0 || handoff.state === "attention" ? <div className="archaeology-attention-note" role="alert"><strong>Human review is needed before another queue can start.</strong><span>Commons will not retry, restart, or reinterpret uncertain work automatically.</span></div> : null}
      <div className="archaeology-run-list">{tasks.map((task) => (
        <article key={task.jobId || task.launchId || task.projectId} className={["archaeology-run", `archaeology-run--${archaeologyTaskPresentation(task).tone}`].join(" ")}>
          <span key={task.state} className="archaeology-run-mark" data-state={task.state} aria-hidden="true">{["report_ready", "completed"].includes(task.state) ? <CheckCircle /> : <BookOpen />}</span>
          <div className="archaeology-run-copy"><strong>{task.jobId ? `Project history · ${taskCandidateName(candidates, task)}` : taskCandidateName(candidates, task)}</strong><span>{archaeologyTaskPresentation(task).primary}</span><small>{archaeologyTaskPresentation(task).secondary}</small>{task.jobId && (task.sourcesExamined > 0 || task.durationMs != null) ? <small>{task.sourcesExamined} source{task.sourcesExamined === 1 ? "" : "s"} examined{task.durationMs != null ? ` · ${formatTaskDuration(task.durationMs)}` : ""}</small> : null}</div>
          <div className="archaeology-run-facts"><span>{task.jobId ? task.state.replaceAll("_", " ") : "Legacy"}</span><small>{task.updatedAt ? `Updated ${task.updatedAt.relative}` : "No reconciled update"}</small><details className="archaeology-task-provenance"><summary>Task provenance</summary><dl>{[["Job", task.jobId], ["Batch", task.batchId || handoff.batchId], ["Candidate", task.candidateId], ["Project", task.projectId], ["Thread", task.threadId], ["Turn", task.turnId], ["Legacy launch", task.launchId]].filter(([, value]) => value).map(([label, value]) => <div key={label}><dt>{label}</dt><dd><code tabIndex="0">{value}</code><button type="button" className="archaeology-id-copy" onClick={() => copyID(value, `${label} ID`)} aria-label={`Copy ${label.toLowerCase()} ID`}><Copy aria-hidden="true" /> Copy</button></dd></div>)}</dl></details></div>
        </article>
      ))}</div>
      {!tasks.length ? <div className="archaeology-empty"><strong>Preparing task rows</strong><span>Commons accepted your selection and is reserving one row per project.</span></div> : null}
      {copyState ? <p className="archaeology-message" role="status">{copyState}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {model.review?.proposedOutcomes?.length ? <section className="archaeology-ledger-review" aria-labelledby="archaeology-ledger-review-title"><h3 id="archaeology-ledger-review-title">Review-ready outcomes</h3><p>Nothing has been imported automatically. Open the canonical preview and approve an exact source digest before apply.</p>{model.review.proposedOutcomes.map((outcome) => <div key={outcome.id}><span><strong>{outcome.title}</strong><small>{candidateName(candidates, outcome.projectId)}</small></span><button className="secondary-button" type="button" disabled={busy || model.review.canApply !== true} onClick={() => onReview(outcome.id)}>Review import</button></div>)}</section> : null}
      {canCancel ? <div className="archaeology-cancel-note"><span>Cancel stops queued work and requests interruption for active tasks. Audit history is preserved, and work never silently restarts.</span><button className="secondary-button" type="button" disabled={busy} onClick={onCancel}><Stop aria-hidden="true" />Cancel run</button></div> : null}
      <p className="archaeology-background-note">Closing this window does not cancel accepted work.</p>
      <footer className="archaeology-footer archaeology-footer--between"><button className="secondary-button" type="button" onClick={onClose}>Close</button><div>{terminal && model.controls?.canStart === true ? <button className="secondary-button" type="button" disabled={busy} onClick={onNewRun}>Choose more projects</button> : null}<button className="primary-button" type="button" disabled={busy} onClick={onRefresh}>{busy ? "Checking status…" : "Check status"}</button></div></footer>
    </div>
  );
}

function LegacyRuns({ model, error, onClose }) {
  const candidates = model?.discovery?.candidates || [];
  return (
    <div className="archaeology-runs">
      <header className="archaeology-content-heading">
        <div><span>Project archaeology</span><h2 id="archaeology-title">Legacy historian records</h2><p id="archaeology-description">These rows predate the native scheduler. Their status is retained for audit and is not a current execution claim.</p></div>
      </header>
      <div className="archaeology-run-list">
        {(model.runs || []).map((run) => (
          <article key={run.id} className="archaeology-run archaeology-run--legacy">
            <span className="archaeology-run-mark" aria-hidden="true"><BookOpen /></span>
            <div><strong>{candidateName(candidates, run.projectId)}</strong><span>Legacy historian · status not reconciled</span><small>This row is audit history only. No retry, pause, cancel, or archive action is available.</small></div>
            <div className="archaeology-run-facts"><span>Legacy</span><code tabIndex="0">{run.id}</code></div>
          </article>
        ))}
      </div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer"><button className="secondary-button" type="button" onClick={onClose}>Close</button></footer>
    </div>
  );
}

function Provenance({ outcome }) {
  const total = outcome.sourceCount || outcome.provenance?.length || 0;
  return (
    <details className="archaeology-provenance">
      <summary>View provenance · {total} source{total === 1 ? "" : "s"}</summary>
      <ul>{(outcome.provenance || []).map((source, index) => <li key={`${source.sourceKind}-${source.digest || index}`}><strong>{source.sourceLabel || source.sourceKind}</strong><span>Digest retained</span></li>)}</ul>
    </details>
  );
}

function Review({ model, busy, error, onReview, onClose }) {
  const review = model.review || {};
  const outcomes = review.proposedOutcomes || [];
  const members = review.memberSessions || [];
  return (
    <div className="archaeology-review">
      <header className="archaeology-content-heading">
        <div><span>Exploration complete</span><h2 id="archaeology-title">Review what Commons found</h2><p id="archaeology-description">These are proposed outcomes, not canonical history. Open the import preview to review the exact manifest before any human-approved apply.</p></div>
      </header>
      <section className="archaeology-review-section" aria-labelledby="archaeology-outcomes-title">
        <div className="archaeology-section-heading"><div><h3 id="archaeology-outcomes-title">Proposed outcomes</h3><p>{outcomes.length} found across selected projects</p></div></div>
        <div className="archaeology-outcomes">{outcomes.map((outcome) => <article key={outcome.id}><span className="archaeology-outcome-mark" aria-hidden="true"><CheckCircle /></span><div><strong>{outcome.title}</strong><p>{outcome.summary}</p><small>{candidateName(model.discovery?.candidates || [], outcome.projectId)}</small><Provenance outcome={outcome} /><button className="secondary-button archaeology-outcome-review" type="button" disabled={busy || review.canApply !== true || model.capabilities?.canonicalApply?.available !== true} onClick={() => onReview(outcome.id)}>Review this import</button></div></article>)}</div>
      </section>
      {members.length ? <section className="archaeology-review-section" aria-labelledby="archaeology-members-title">
        <div className="archaeology-section-heading"><div><h3 id="archaeology-members-title">Session members in this history</h3><p>Durable membership is separate from reachability, execution, and authority.</p></div></div>
        <div className="archaeology-members">{members.map((rawMember) => {
          const member = memberFacts(rawMember);
          return <details key={member.sessionID}><summary><span><strong>{member.displayName || member.sessionID}</strong><small>{member.displayName ? `${member.sessionID} · ` : ""}Commons member</small></span><span>{member.contributionCount} contributions · {member.sourceCount} sources</span></summary><dl><div><dt>Membership</dt><dd>Recorded in Commons history</dd></div><div><dt>Reachability</dt><dd>Historical or unknown</dd></div><div><dt>Execution</dt><dd>Not attested</dd></div><div><dt>Authority</dt><dd>Provenance only</dd></div>{member.strengths.length ? <div><dt>Demonstrated strengths</dt><dd>{member.strengths.join(" · ")}</dd></div> : null}{member.uncertainties.length ? <div><dt>Uncertainties</dt><dd>{member.uncertainties.join(" · ")}</dd></div> : null}</dl></details>;
        })}</div>
      </section> : null}
      <p className="archaeology-review-note">{review.provenanceSummary}</p>
      <p className="archaeology-review-note">The canonical preview deduplicates reviewed history. Current Commons records win when history collides with newer work.</p>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer"><button className="secondary-button" type="button" onClick={onClose}>Review later</button></footer>
    </div>
  );
}

export function ProjectArchaeologyDialog({ open, identity, archaeology, busy = false, launchingCount = 0, refreshingProjects = false, updateStatus = null, error = "", onDiscover, onStart, onCancel, onRefresh, onReview, onSkip, onClose }) {
  const dialogRef = useRef(null);
  const modelConfigVersionRef = useRef("");
  const batchIdentityRef = useRef(archaeology?.handoff?.batchId || "");
  const [config, setConfig] = useState(() => configFromModel(archaeology?.config));
  const [configureAgain, setConfigureAgain] = useState(false);
  const serverView = !archaeology && (busy || error) ? "reading" : archaeologyView(archaeology);
  const view = configureAgain ? "configure" : serverView;
  const previousViewRef = useRef(view);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    const previous = previousViewRef.current;
    previousViewRef.current = view;
    if (open && previous !== view && (view === "configure" || view === "handoff")) queueMicrotask(() => dialogRef.current?.querySelector("#archaeology-title")?.focus());
  }, [open, view]);

  useEffect(() => {
    const modelConfigVersion = archaeologyConfigVersion(archaeology);
    if (modelConfigVersion !== modelConfigVersionRef.current) {
      modelConfigVersionRef.current = modelConfigVersion;
      setConfig(configFromModel(archaeology?.config));
    }
  }, [archaeology?.id, archaeology?.revision, archaeology?.config]);

  useEffect(() => {
    const batchIdentity = archaeology?.handoff?.batchId || "";
    if (batchIdentityRef.current && batchIdentity && batchIdentityRef.current !== batchIdentity) setConfigureAgain(false);
    batchIdentityRef.current = batchIdentity;
  }, [archaeology?.handoff?.batchId]);

  const selectedSummary = useMemo(() => {
    const projects = config.selectedProjectIds.length;
    const sources = selectedSourceCount(config);
    return `${projects} project${projects === 1 ? "" : "s"} and ${sources} source${sources === 1 ? "" : "s"} selected`;
  }, [config]);
  function close() { setConfigureAgain(false); onClose?.(); }
  function skip() { onSkip?.(); }

  return (
    <dialog ref={dialogRef} className={`archaeology-dialog archaeology-dialog--${view}`} aria-labelledby="archaeology-title" aria-describedby="archaeology-description" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      <span className="sr-only" aria-live="polite">{selectedSummary}</span>
      {view !== "intro" && view !== "reading" && view !== "discovering" && view !== "discovery_failed" ? <button className="archaeology-close" type="button" onClick={close}>Close</button> : null}
      {view === "intro" ? <Intro identity={identity} model={archaeology} busy={busy} error={error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "reading" ? <ReadingState error={error} onRetry={onRefresh} onClose={close} /> : null}
      {view === "discovering" || view === "discovery_failed" ? <DiscoveryState failed={view === "discovery_failed"} error={error || archaeology?.discovery?.error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "handoff" ? <Handoff model={archaeology} busy={busy} error={error} updateStatus={updateStatus} onRefresh={onRefresh} onCancel={onCancel} onReview={onReview} onNewRun={() => setConfigureAgain(true)} onClose={close} /> : null}
      {view === "configure" ? <Configure model={archaeology} config={config} busy={busy} launchingCount={launchingCount} refreshing={refreshingProjects} error={error} onChange={setConfig} onDiscover={onDiscover} onStart={onStart} onSkip={skip} /> : null}
      {view === "running" || view === "paused" ? <LegacyRuns model={archaeology} error={error} onClose={close} /> : null}
      {view === "review" ? <Review model={archaeology} busy={busy} error={error} onReview={onReview} onClose={close} /> : null}
      {view === "failed" ? <DiscoveryState failed error={error || "Project exploration stopped before a review was ready."} onDiscover={() => onStart(config)} onSkip={close} /> : null}
    </dialog>
  );
}
