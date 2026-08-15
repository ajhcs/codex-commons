import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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
import { ProjectArchaeologyHistory } from "./ProjectArchaeologyHistory.jsx";
import {
  ARCHAEOLOGY_DEPTHS,
  MAX_PROJECT_ARCHAEOLOGY_SELECTION,
  archaeologyConfigVersion,
  archaeologyBatchIsTerminal,
  archaeologyTaskIsActive,
  archaeologyTaskPresentation,
  archaeologyView,
  canSubmitArchaeologyConfig,
  configFromModel,
  freshManualArchaeologyConfig,
  projectArchaeologyOperationState,
  formatDurationRange,
  memberFacts,
  reconcileConfigAfterCatalogRefresh,
  discoveryProgressText,
  handoffProgress,
  isProjectCandidateSelectable,
  sortProjectCandidates,
  shouldPreserveLocalArchaeologyConfig,
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
      <div className="archaeology-privacy-note"><strong>You stay in control</strong><span>Starting creates one read-only Codex task in each selected project. It may inspect that project; results remain proposals until you review them.</span></div>
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
      <p id="archaeology-description">{failed ? error || "Commons could not finish the metadata-only project check." : "Checking names, Git and documentation signals, Codex-history availability, and recent activity. Codex 0.147 may send preview bytes on the inventory wire; Commons immediately discards them and retains only sanitized workspace metadata."}</p>
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

function CandidateList({ candidates, catalog, discovery, config, disabled, refreshing, onChange, onRefresh, onCatalogQuery, onLoadMore }) {
  const [query, setQuery] = useState(catalog?.query || "");
  const [sort, setSort] = useState(catalog?.sort || "recent");
  const elapsed = useElapsed(refreshing, discovery?.startedAt);
  const normalizedQuery = query.trim().toLowerCase();
  const visible = candidates.filter((candidate) => !normalizedQuery || [candidate.name, candidate.repositoryLabel].some((value) => value?.toLowerCase().includes(normalizedQuery)));
  const sorted = sortProjectCandidates(visible, sort);
  const selected = new Set(config.selectedProjectIds);
  const selectable = visible.filter((candidate) => isProjectCandidateSelectable(candidate));
  const allSelectable = candidates.filter((candidate) => isProjectCandidateSelectable(candidate));
  const selectedVisible = selectable.filter((candidate) => selected.has(candidate.id));
  const selectionAtLimit = selected.size >= MAX_PROJECT_ARCHAEOLOGY_SELECTION;
  const listRef = useRef(null);
  const scrollTopRef = useRef(0);
  useEffect(() => {
    if (query === catalog?.query && sort === catalog?.sort) return undefined;
    const timer = globalThis.setTimeout(() => onCatalogQuery?.(query, sort), 250);
    return () => globalThis.clearTimeout(timer);
  }, [query, sort, catalog?.query, catalog?.sort]);
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = scrollTopRef.current;
  }, [candidates]);
  const toggleVisible = () => {
    const next = new Set(config.selectedProjectIds);
    if (selectable.length && selectedVisible.length === selectable.length) selectable.forEach((candidate) => next.delete(candidate.id));
    else selectable.forEach((candidate) => {
      if (next.size < MAX_PROJECT_ARCHAEOLOGY_SELECTION) next.add(candidate.id);
    });
    onChange({ ...config, selectedProjectIds: [...next] });
  };
  const selectAll = () => onChange({ ...config, selectedProjectIds: allSelectable.slice(0, MAX_PROJECT_ARCHAEOLOGY_SELECTION).map((candidate) => candidate.id) });
  const toggle = (id) => {
    if (!selected.has(id) && selectionAtLimit) return;
    onChange({ ...config, selectedProjectIds: selected.has(id) ? config.selectedProjectIds.filter((value) => value !== id) : [...config.selectedProjectIds, id] });
  };
  const refreshLabel = refreshing ? ({ queued: "Refresh queued", reading_codex_metadata: "Reading Codex task metadata", persisting_catalog: "Organizing projects", failed: "Refresh needs attention" })[discovery?.stage] || "Refreshing projects" : "Refresh Codex projects";
  const grouped = sort === "recent" && !normalizedQuery && sorted.length > 6
    ? [["Recent", sorted.slice(0, 6)], ["All projects", sorted.slice(6)]]
    : [["All projects", sorted]];
  return (
    <section className="archaeology-projects" aria-labelledby="archaeology-projects-title">
      <div className="archaeology-catalog-toolbar">
        <label className="archaeology-search"><Search aria-hidden="true" /><span className="sr-only">Search projects</span><input type="search" name="project-search" aria-label="Search projects" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search Codex projects" /></label>
        <label className="archaeology-sort"><span>Sort:</span><select name="project-sort" aria-label="Sort projects" value={sort} onChange={(event) => setSort(event.target.value)}><option value="recent">Recent activity</option><option value="tasks">Most Codex tasks</option><option value="name">Name</option></select></label>
        <span>{config.selectedProjectIds.length} of {MAX_PROJECT_ARCHAEOLOGY_SELECTION} selected</span>
        <div className="archaeology-selection-actions"><button type="button" disabled={disabled || !selectable.length || (selectionAtLimit && selectedVisible.length !== selectable.length)} onClick={toggleVisible}>{selectable.length && selectedVisible.length === selectable.length ? "Clear visible" : "Select visible"}</button><button type="button" disabled={disabled || !allSelectable.length} onClick={selectAll}>{allSelectable.length > MAX_PROJECT_ARCHAEOLOGY_SELECTION ? `Select first ${MAX_PROJECT_ARCHAEOLOGY_SELECTION}` : "Select all"}</button></div>
      </div>
      <div className="archaeology-section-heading"><div><h3 id="archaeology-projects-title">Codex projects</h3><p>{visible.length} loaded of {catalog?.total || candidates.length} matching</p></div><button type="button" aria-disabled={disabled || refreshing} onClick={() => { if (!disabled && !refreshing) onRefresh(); }} aria-busy={refreshing}><History aria-hidden="true" />{refreshLabel}</button></div>
      <div className={`archaeology-operation-status${refreshing ? " is-active" : " is-ready"}`}><span className="archaeology-operation-icon"><History aria-hidden="true" /></span><strong role="status" aria-live="polite" aria-atomic="true">{discoveryProgressText(discovery)}{refreshing ? <span aria-hidden="true"> · {elapsed}s</span> : null}</strong><small>{refreshing ? "Keep selecting from this list. Starting becomes available when the refresh finishes." : discovery?.updatedAt?.relative ? `Updated ${discovery.updatedAt.relative}` : "Catalog ready"}</small></div>
      {catalog?.telemetry ? <dl className="archaeology-discovery-facts"><div><dt>Tasks checked</dt><dd>{catalog.telemetry.tasksExamined.toLocaleString()}</dd></div><div><dt>Projects grouped</dt><dd>{catalog.telemetry.projectsGrouped.toLocaleString()}</dd></div><div><dt>Coverage</dt><dd>{catalog.telemetry.truncated ? "Bound reached" : "Complete"}</dd></div><div><dt>App Server</dt><dd>{catalog.telemetry.appServerIdentity || "Paired Codex"}</dd></div></dl> : null}
      {catalog?.error ? <p className="archaeology-message archaeology-message--error" role="alert">{catalog.error}</p> : null}
      {sorted.length ? <div ref={listRef} className="archaeology-candidate-list" onScroll={(event) => { scrollTopRef.current = event.currentTarget.scrollTop; }}>{grouped.map(([group, rows]) => <section className="archaeology-candidate-group" key={group} aria-labelledby={`archaeology-group-${group.replaceAll(" ", "-").toLowerCase()}`}><h4 id={`archaeology-group-${group.replaceAll(" ", "-").toLowerCase()}`}>{group}</h4>{rows.map((candidate) => {
        const checked = selected.has(candidate.id);
        const sources = sourceLabels(candidate.signals);
        const stale = !isProjectCandidateSelectable(candidate);
        return (
          <label key={candidate.id} className={["archaeology-candidate", checked ? "is-selected" : ""].filter(Boolean).join(" ")}>
            <input type="checkbox" checked={checked} disabled={disabled || stale || (!checked && selectionAtLimit)} onChange={() => toggle(candidate.id)} />
            <span className="archaeology-candidate-mark" aria-hidden="true"><Folder /></span>
            <span className="archaeology-candidate-copy"><strong>{candidate.name}</strong><small>{candidate.repositoryLabel || "Codex project"}</small><span>{stale ? "Legacy reference · refresh metadata to select" : sources.join(" · ") || "Metadata only"} · {candidate.codexThreadCount} Codex task{candidate.codexThreadCount === 1 ? "" : "s"}</span></span>
            <span className="archaeology-candidate-estimate"><span><Clock aria-hidden="true" />{formatDurationRange(candidate.estimate)}</span><small>{candidate.lastActivity ? `Active ${candidate.lastActivity.relative}` : costCopy[candidate.estimate?.relativeCost] || "Time varies"}</small></span>
          </label>
        );
      })}</section>)}</div> : <div className="archaeology-empty"><strong>{catalog?.loading ? "Searching Codex projects…" : "No matching projects"}</strong><span>{catalog?.loading ? "Your existing selection stays available." : "Try another name or clear the search."}</span></div>}
      {catalog?.nextCursor ? <button className="secondary-button archaeology-catalog-more" type="button" disabled={catalog.loading} onClick={onLoadMore}>{catalog.loading ? "Loading more projects…" : `Load more · ${Math.max(0, catalog.total - candidates.length)} remaining`}</button> : null}
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
        <legend>Evidence allowed in the review</legend>
        <div className="archaeology-source-options">
          {sourceOptions.map(([key, label, Icon]) => <label key={key}><input type="checkbox" checked={config.sources[key]} onChange={(event) => updateSources(key, event.target.checked)} /><Icon aria-hidden="true" /><span>{label}</span></label>)}
        </div>
        <p>These choices govern admissible cited evidence. The historian runs read-only in the selected project; results remain proposals, never automatic imports.</p>
      </fieldset>
    </div>
  );
}

function Configure({ model, catalog, config, operations, launchingCount, refreshing, error, onChange, onDiscover, onCatalogQuery, onCatalogMore, onStart, onSkip }) {
  const candidates = catalog?.items?.length || catalog?.query ? catalog.items : model?.discovery?.candidates || [];
  const knownCandidates = [...new Map([...(model?.discovery?.candidates || []), ...(catalog?.items || [])].map((candidate) => [candidate.id, candidate])).values()];
  const knownByID = new Map(knownCandidates.map((candidate) => [candidate.id, candidate]));
  const unavailableSelected = config.selectedProjectIds.filter((id) => !isProjectCandidateSelectable(knownByID.get(id)));
  const valid = canSubmitArchaeologyConfig(config, { ...model, discovery: { ...model?.discovery, candidates: knownCandidates } });
  const launch = model?.capabilities?.taskLaunch;
  const projectCount = config.selectedProjectIds.length;
  const selectedProjectNames = config.selectedProjectIds.map((id) => knownByID.get(id)?.name || id);
  const projectNoun = projectCount === 1 ? "project" : "projects";
  const refreshingCatalog = refreshing && candidates.length > 0;
  const operation = projectArchaeologyOperationState(operations);
  const interactionDisabled = operation.selectionLocked;
  const startBusy = launchingCount > 0 || operation.startBlocked;
  const startLabel = projectCount ? `Add ${projectCount} ${projectNoun} to Commons · start ${projectCount} named Codex task${projectCount === 1 ? "" : "s"}` : "Choose a project to continue";
  const startButtonLabel = launchingCount > 0
    ? `Preparing ${launchingCount} ${launchingCount === 1 ? "project" : "projects"}`
    : refreshingCatalog
      ? `Refreshing projects · ${projectCount} selected`
      : startLabel;
  const [confirmingLargeBatch, setConfirmingLargeBatch] = useState(false);
  const [largeBatchAcknowledged, setLargeBatchAcknowledged] = useState(false);
  const configureRef = useRef(null);
  const confirmationRef = useRef(null);
  const confirmationHeadingRef = useRef(null);
  const returnFocusRef = useRef(null);
  const largeBatchIdentity = `${config.selectedProjectIds.join("\n")}|${config.depth}|${sourceOptions.filter(([key]) => config.sources[key]).map(([key]) => key).join(",")}`;
  useEffect(() => {
    setLargeBatchAcknowledged(false);
    setConfirmingLargeBatch(false);
  }, [largeBatchIdentity]);
  useEffect(() => {
    if (!confirmingLargeBatch) return undefined;
    const root = configureRef.current;
    const dialog = confirmationRef.current;
    const background = [...(root?.children || [])].filter((node) => !node.classList.contains("archaeology-confirm-backdrop"));
    background.forEach((node) => node.setAttribute("inert", ""));
    queueMicrotask(() => confirmationHeadingRef.current?.focus());
    function handleKeyDown(event) {
      if (event.key === "Escape") {
        event.preventDefault();
        setConfirmingLargeBatch(false);
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = [...dialog.querySelectorAll('button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])')];
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable.at(-1);
      if (event.shiftKey && (document.activeElement === first || document.activeElement === confirmationHeadingRef.current || !dialog.contains(document.activeElement))) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || document.activeElement === confirmationHeadingRef.current || !dialog.contains(document.activeElement))) { event.preventDefault(); first.focus(); }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      background.forEach((node) => node.removeAttribute("inert"));
      globalThis.requestAnimationFrame(() => returnFocusRef.current?.focus());
    };
  }, [confirmingLargeBatch]);
  function requestStart(event) {
    if (projectCount > 5) {
      returnFocusRef.current = event.currentTarget;
      setLargeBatchAcknowledged(false);
      setConfirmingLargeBatch(true);
      return;
    }
    onStart(config, false);
  }
  return (
    <div ref={configureRef} className="archaeology-configure">
      <header className="archaeology-content-heading"><div><span>Project Archaeology</span><h2 id="archaeology-title" tabIndex="-1">Choose your Codex projects</h2><p id="archaeology-description">Choose recent projects from this App Server. Nothing is imported until you review it.</p></div></header>
      <div className="archaeology-workspace">
        <aside className="archaeology-trust-panel">
          <CommonsMark state="offered" size="large" />
          <h3>Codex stays in control</h3>
          <p>Projects come from the App Server paired with this browser. Commons receives labels and capability facts here—not raw paths or thread bodies.</p>
          <dl><div><dt>Selected</dt><dd>{config.selectedProjectIds.length} {config.selectedProjectIds.length === 1 ? "project" : "projects"}</dd></div><div><dt>Mode</dt><dd>{config.depth} review</dd></div><div><dt>Sources</dt><dd>{selectedSourceCount(config)} enabled</dd></div><div><dt>Execution</dt><dd>Codex-managed</dd></div></dl>
        </aside>
        <div className="archaeology-catalog-panel"><CandidateList candidates={candidates} catalog={catalog} discovery={{ ...model.discovery, codexThreadsExamined: catalog?.telemetry?.tasksExamined ?? model.discovery?.codexThreadsExamined, workspacesGrouped: catalog?.telemetry?.projectsGrouped ?? model.discovery?.workspacesGrouped }} config={config} disabled={interactionDisabled} refreshing={refreshing || catalog?.loading} onChange={onChange} onRefresh={onDiscover} onCatalogQuery={onCatalogQuery} onLoadMore={onCatalogMore} /></div>
      </div>
      <details className="archaeology-advanced"><summary>Advanced settings <span>{config.depth} · {selectedSourceCount(config)} sources</span></summary><Configuration config={config} disabled={interactionDisabled} onChange={onChange} /></details>
      <div className="archaeology-truth-note"><strong>{projectCount ? startLabel : "Choose at least one project to continue"}</strong><span>Each historian runs read-only in its selected project. Evidence choices govern admissible citations; no history is imported until you review and apply it.</span></div>
      {projectCount ? <p className="archaeology-message" role="status"><strong>Selected to launch · {projectCount}</strong> {selectedProjectNames.join(" · ")}</p> : null}
      {unavailableSelected.length ? <p className="archaeology-message" role="status"><strong>{unavailableSelected.length} selected project{unavailableSelected.length === 1 ? " is" : "s are"} unavailable in the refreshed catalog.</strong> The selection is preserved for review, but starting stays disabled unless a later catalog page proves the project is still present.</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {launch?.available === false ? <p className="archaeology-message" role="status">{launch.reason || "Direct Codex task launch is not available on this installation."}</p> : null}
      {launchingCount > 0 ? <div className="archaeology-launch-ack" role="status" aria-live="polite" aria-atomic="true"><span className="archaeology-operation-icon"><BookOpen aria-hidden="true" /></span><div><strong>Preparing {launchingCount} {launchingCount === 1 ? "project" : "projects"}</strong><small>Your selection is frozen while Commons reserves one durable scheduler row per project.</small></div></div> : null}
      <footer className="archaeology-footer archaeology-footer--between"><button className="secondary-button" type="button" disabled={interactionDisabled} onClick={onSkip}>Not now</button><button className="primary-button archaeology-start-button" type="button" aria-busy={startBusy} disabled={operation.startBlocked || refreshing || !valid} onClick={requestStart}>{startButtonLabel}</button></footer>
      {confirmingLargeBatch ? <div className="archaeology-confirm-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setConfirmingLargeBatch(false); }}><section ref={confirmationRef} className="archaeology-confirm" role="dialog" aria-modal="true" aria-labelledby="archaeology-confirm-title">
        <span>Usage confirmation</span><h3 ref={confirmationHeadingRef} id="archaeology-confirm-title" tabIndex="-1">Start {projectCount} named Codex tasks?</h3>
        <p>Commons will submit one read-only Luna Max historian task per selected project. Codex governs execution capacity, and the tasks may use your signed-in Codex allowance.</p>
        <p><strong>Selected projects:</strong> {selectedProjectNames.join(" · ")}</p>
        <dl><div><dt>Tasks</dt><dd>{projectCount}</dd></div><div><dt>Model</dt><dd>Luna Max</dd></div><div><dt>Execution</dt><dd>Codex-managed</dd></div><div><dt>Depth</dt><dd>{config.depth}</dd></div><div><dt>Evidence</dt><dd>{sourceOptions.filter(([key]) => config.sources[key]).map(([, label]) => label === "Documentation" ? "Project docs" : label).join(" + ")}</dd></div></dl>
        <label><input type="checkbox" checked={largeBatchAcknowledged} onChange={(event) => setLargeBatchAcknowledged(event.target.checked)} /><span>I understand this starts {projectCount} Codex tasks and may use my Codex allowance.</span></label>
        <footer><button className="secondary-button" type="button" onClick={() => setConfirmingLargeBatch(false)}>Back</button><button className="primary-button" type="button" disabled={!largeBatchAcknowledged} onClick={() => { setConfirmingLargeBatch(false); onStart(config, true); }}>Start {projectCount} tasks</button></footer>
      </section></div> : null}
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

function Handoff({ model, busy, error, updateStatus, onRefresh, onCancel, onResolve, onInspectReview, onNewRun, onClose }) {
  const handoff = model?.handoff || {};
  const tasks = handoff.tasks || [];
  const candidates = model?.discovery?.candidates || [];
  const progress = handoffProgress(handoff);
  const nativeBatch = Boolean(handoff.batchId);
  const active = tasks.some((task) => task.jobId && archaeologyTaskIsActive(task));
  const terminal = archaeologyBatchIsTerminal(handoff);
  const startingTotal = progress.queued + progress.preparing + progress.starting + progress.created;
  const activeTotal = Math.max(progress.active, progress.claimed + progress.running);
  const runSummary = startingTotal
    ? `Starting ${startingTotal} of ${progress.total} historian task${progress.total === 1 ? "" : "s"}`
    : progress.ready
      ? `${progress.ready} report${progress.ready === 1 ? "" : "s"} received`
      : activeTotal
        ? `${activeTotal} historian${activeTotal === 1 ? "" : "s"} active`
        : `${progress.total} ${progress.total === 1 ? "project" : "projects"} in this run`;
  const elapsed = useElapsed(active, handoff.createdAt);
  const [copyState, setCopyState] = useState("");
  const [resolveJobID, setResolveJobID] = useState("");
  const [resolveConfirmed, setResolveConfirmed] = useState(false);
  const resolvableTasks = handoff.allowedActions?.includes("resolve") ? tasks.filter((task) => task.state === "uncertain" && task.threadId && task.turnId && task.availableActions?.includes("resolve")) : [];
  const unidentifiedUncertainTasks = tasks.filter((task) => task.state === "uncertain" && (!task.threadId || !task.turnId));
  const uncertainTaskCount = tasks.filter((task) => task.state === "uncertain").length;
  const terminalAttentionTaskCount = tasks.filter((task) => task.state === "attention").length;
  const resolveTarget = resolvableTasks.find((task) => task.jobId === resolveJobID) || null;
  const heading = !nativeBatch ? "Legacy historian records" : progress.queued + progress.starting > 0 ? "Starting Codex tasks" : terminal ? (handoff.state === "canceled" ? "Project history run canceled" : handoff.state === "attention" ? "Project history needs attention" : "Project history run complete") : "Project history is underway";
  async function copyID(value, label) {
    const copied = await copyText(value);
    setCopyState(copied ? `${label} copied.` : `Copy isn’t available here. Select the ID and press ${manualCopyShortcut()} to copy it.`);
  }
  const canCancel = nativeBatch && model?.controls?.canCancel === true && ["queued", "running"].includes(handoff.state);
  const reviewIsCurrent = model.review?.batchRelation === "current";
  return (
    <div className="archaeology-runs archaeology-handoff auth-content-transition">
      <header className="archaeology-content-heading"><div><span>Codex tasks</span><h2 id="archaeology-title" tabIndex="-1">{heading}</h2><p id="archaeology-description">{!nativeBatch ? "These pre-scheduler rows are retained for audit only. Their earlier status is not a current execution claim." : progress.queued + progress.starting > 0 ? "Commons is creating one ordinary named Codex historian task for each selected project." : "You can close this window. Commons preserves every literal scheduler state and imports nothing without human review."}</p></div></header>
      {nativeBatch ? <div className="archaeology-launch-summary" role="status" aria-live="polite" aria-atomic="true"><strong>{runSummary}</strong><span>{progress.queued + progress.preparing} queued · {progress.active} active · {progress.ready} reports · {progress.attention} attention<span aria-hidden="true">{elapsed ? ` · ${formatTaskDuration(elapsed * 1000)}` : ""}</span></span></div> : <div className="archaeology-launch-summary"><strong>{progress.total} legacy historian records</strong><span>Status not reconciled</span></div>}
      {nativeBatch && handoff.policyAttested === false ? <div className="archaeology-attention-note" role="alert"><strong>Legacy run — execution settings were not recorded.</strong><span>{handoff.state === "attention" ? "This batch is quarantined. Installation or operator reconciliation is required before Commons can treat it as execution evidence." : "This terminal audit record is retained without inferred depth or source policy."}</span></div> : null}
      <p className="archaeology-usage-note">{nativeBatch && handoff.policyAttested !== false ? "Commons submitted every manually confirmed task in this run. Codex governs execution capacity, and these tasks may use your signed-in Codex allowance." : nativeBatch ? "Execution settings are unavailable for this quarantined run." : "No retry, pause, cancel, or archive controls are available for legacy rows."}</p>
      {nativeBatch ? <UpdateStatus status={updateStatus} /> : null}
      {uncertainTaskCount > 0 ? <div className="archaeology-attention-note" role="alert"><strong>Human review is needed before another queue can start.</strong><span>Commons will not retry, restart, or reinterpret uncertain work automatically.</span></div> : null}
      {terminalAttentionTaskCount > 0 && uncertainTaskCount === 0 ? <div className="archaeology-attention-note" role="status"><strong>{terminalAttentionTaskCount} {terminalAttentionTaskCount === 1 ? "task ended" : "tasks ended"} without a review-ready report.</strong><span>Audit evidence is preserved, and Commons will not retry these tasks automatically. You can choose a fresh manual run.</span></div> : null}
      <div className="archaeology-run-list">{tasks.map((task) => (
        <article key={task.jobId || task.launchId || task.projectId} className={["archaeology-run", `archaeology-run--${archaeologyTaskPresentation(task).tone}`].join(" ")}>
          <span key={task.state} className="archaeology-run-mark" data-state={task.state} aria-hidden="true">{["report_ready", "completed"].includes(task.state) ? <CheckCircle /> : <BookOpen />}</span>
          <div className="archaeology-run-copy"><strong>{task.jobId ? `Project history · ${taskCandidateName(candidates, task)}` : taskCandidateName(candidates, task)}</strong><span>{archaeologyTaskPresentation(task).primary}</span><small>{archaeologyTaskPresentation(task).secondary}</small>{task.threadId ? <small>Created as a named Codex task. Open Codex to follow it.</small> : null}{task.jobId && (task.sourcesExamined > 0 || task.durationMs != null) ? <small>{task.sourcesExamined} source{task.sourcesExamined === 1 ? "" : "s"} examined{task.durationMs != null ? ` · ${formatTaskDuration(task.durationMs)}` : ""}</small> : null}</div>
          <div className="archaeology-run-facts"><span>{task.jobId ? task.state.replaceAll("_", " ") : "Legacy"}</span><small>{task.updatedAt ? `Updated ${task.updatedAt.relative}` : "No reconciled update"}</small><details className="archaeology-task-provenance"><summary>Task provenance</summary><dl>{[["Job", task.jobId], ["Batch", task.batchId || handoff.batchId], ["Candidate", task.candidateId], ["Project", task.projectId], ["Thread", task.threadId], ["Turn", task.turnId], ["Legacy launch", task.launchId]].filter(([, value]) => value).map(([label, value]) => <div key={label}><dt>{label}</dt><dd><code tabIndex="0">{value}</code><button type="button" className="archaeology-id-copy" onClick={() => copyID(value, `${label} ID`)} aria-label={`Copy ${label.toLowerCase()} ID`}><Copy aria-hidden="true" /> Copy</button></dd></div>)}</dl></details></div>
        </article>
      ))}</div>
      {unidentifiedUncertainTasks.length ? <div className="archaeology-attention-note" role="status"><strong>Task identity is not yet reconcilable.</strong><span>Commons cannot ask you to attest an unseen task. Installation or operator reconciliation is required; no retry will start automatically.</span></div> : null}
      {resolvableTasks.length && !resolveTarget ? <div className="archaeology-resolution-available"><strong>One or more tasks need an explicit stop check.</strong><span>Open the exact Codex task first. Commons will not retry uncertain work.</span>{resolvableTasks.map((task) => <button key={task.jobId} className="secondary-button" type="button" disabled={busy} onClick={() => { setResolveJobID(task.jobId); setResolveConfirmed(false); }}>Check Codex task · {task.jobId}</button>)}</div> : null}
      {resolveTarget ? (
        <section className="archaeology-resolution-panel" aria-labelledby="archaeology-resolution-title">
          <div><span>Human reconciliation</span><h3 id="archaeology-resolution-title">Confirm this exact Codex task is stopped</h3><p>Open Codex and verify that this task is no longer running. Commons cannot prove that from its current connection and will never retry it automatically.</p></div>
          <dl>
            <div><dt>Job</dt><dd><code>{resolveTarget.jobId}</code></dd></div>
            <div><dt>Thread</dt><dd><code>{resolveTarget.threadId || "No thread ID recorded"}</code></dd></div>
            <div><dt>Turn</dt><dd><code>{resolveTarget.turnId || "No turn ID recorded"}</code></dd></div>
          </dl>
          <label><input type="checkbox" checked={resolveConfirmed} onChange={(event) => setResolveConfirmed(event.target.checked)} /><span>I checked Codex and confirmed this exact task is stopped.</span></label>
          <footer>
            <button className="secondary-button" type="button" disabled={busy} onClick={() => { setResolveJobID(""); setResolveConfirmed(false); }}>Cancel</button>
            <button className="primary-button" type="button" disabled={busy || !resolveConfirmed || typeof onResolve !== "function"} onClick={() => onResolve?.(resolveTarget)}>{busy ? "Reconciling…" : "Confirm stopped"}</button>
          </footer>
        </section>
      ) : null}
      {!tasks.length ? <div className="archaeology-empty"><strong>Preparing task rows</strong><span>Commons accepted your selection and is reserving one row per project.</span></div> : null}
      {copyState ? <p className="archaeology-message" role="status">{copyState}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {model.review?.proposedOutcomes?.length ? (
        <section className="archaeology-ledger-review" aria-labelledby="archaeology-ledger-review-title">
          <div><div><h3 id="archaeology-ledger-review-title">{reviewIsCurrent ? "Report from this run" : "Retained report from an earlier run"}</h3><p>{model.review.proposedOutcomes.length} review-only outcome{model.review.proposedOutcomes.length === 1 ? "" : "s"}{model.review.batchId ? ` · batch ${model.review.batchId}` : ""}. Nothing has been imported automatically.</p></div><button className="secondary-button" type="button" disabled={busy} onClick={onInspectReview}>Review report details</button></div>
        </section>
      ) : null}
      {canCancel ? <div className="archaeology-cancel-note"><span>Cancel stops queued work and requests interruption for active tasks. Audit history is preserved, and work never silently restarts.</span><button className="secondary-button" type="button" disabled={busy} onClick={onCancel}><Stop aria-hidden="true" />Cancel run</button></div> : null}
      <p className="archaeology-background-note">You can close this window. Accepted work continues while Commons is running.</p>
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
  const retained = outcome.provenance?.length || 0;
  return (
    <details className="archaeology-provenance">
      <summary>Inspect retained provenance · {retained} citation{retained === 1 ? "" : "s"}</summary>
      {outcome.sourceCount > retained ? <p>{outcome.sourceCount} sources were examined; this bounded report retains {retained} exact citation{retained === 1 ? "" : "s"}.</p> : null}
      <ul>{(outcome.provenance || []).map((source, index) => <li key={`${source.sourceKind}-${source.digest || index}`}><strong><code>{source.sourceKind}:{source.sourceLabel}</code></strong><code>{source.digest}</code><span>{source.recordedAt?.absolute || "Recorded time unavailable"}</span></li>)}</ul>
    </details>
  );
}

function OutcomeMembers({ outcome }) {
  if (!outcome.memberSessions?.length) return null;
  return (
    <details className="archaeology-outcome-members">
      <summary>Historical members · {outcome.memberSessions.length}</summary>
      <ul>{outcome.memberSessions.map((rawMember) => {
        const member = memberFacts(rawMember);
        return <li key={member.sessionID}><strong>{member.displayName || member.sessionID}</strong><code>{member.sessionID}</code><span>{member.contributionCount} recorded contribution{member.contributionCount === 1 ? "" : "s"} · historical or unknown reachability</span></li>;
      })}</ul>
    </details>
  );
}
function Review({ model, busy, error, onReview, onClose, backToRun = false }) {
  const review = model.review || {};
  const outcomes = review.proposedOutcomes || [];
  const [selectedOutcomeIDs, setSelectedOutcomeIDs] = useState([]);
  useEffect(() => setSelectedOutcomeIDs((current) => current.filter((id) => outcomes.some((outcome) => outcome.id === id))), [review.batchId, outcomes]);
  const sourceKey = (outcome) => outcome?.projectId && outcome?.sourceDigest ? `${outcome.projectId}\0${outcome.sourceDigest}` : "";
  const sourceCounts = outcomes.reduce((counts, outcome) => {
    const key = sourceKey(outcome);
    if (key) counts.set(key, (counts.get(key) || 0) + 1);
    return counts;
  }, new Map());
  const alternativeSourceKeys = new Set([...sourceCounts].filter(([, count]) => count > 1).map(([key]) => key));
  const selectOutcome = (outcome) => setSelectedOutcomeIDs((current) => {
    if (current.includes(outcome.id)) return current.filter((id) => id !== outcome.id);
    const key = sourceKey(outcome);
    const retained = alternativeSourceKeys.has(key)
      ? current.filter((id) => sourceKey(outcomes.find((candidate) => candidate.id === id)) !== key)
      : current;
    return [...retained, outcome.id];
  });
  const members = review.memberSessions || [];
  const applyCapability = model.capabilities?.canonicalApply;
  const canReviewExactProposal = review.canApply === true && applyCapability?.available === true;
  return (
    <div className="archaeology-review">
      <header className="archaeology-content-heading">
        <div><span>Exploration complete</span><h2 id="archaeology-title">Review what Commons found</h2><p id="archaeology-description">These outcomes are review-only until Commons can present their exact task-and-evidence manifest for human confirmation. Nothing has been applied.</p></div>
      </header>
      <section className="archaeology-review-section" aria-labelledby="archaeology-outcomes-title">
        <div className="archaeology-section-heading"><div><h3 id="archaeology-outcomes-title">Proposed outcomes</h3><p>{outcomes.length} found across selected projects{review.batchId ? ` · batch ${review.batchId}` : ""}</p></div></div>
        <div className="archaeology-outcomes">{outcomes.map((outcome) => {
          const alternative = alternativeSourceKeys.has(sourceKey(outcome));
          return <article key={outcome.id}>{canReviewExactProposal ? <input className="archaeology-outcome-select" type="checkbox" aria-label={`Select ${outcome.title}`} checked={selectedOutcomeIDs.includes(outcome.id)} onChange={() => selectOutcome(outcome)} /> : <span className="archaeology-outcome-mark" aria-hidden="true"><CheckCircle /></span>}<div><strong>{outcome.title}</strong><p>{outcome.summary}</p><small>{candidateName(model.discovery?.candidates || [], outcome.projectId)}</small>{alternative ? <p className="archaeology-review-only"><strong>Alternative from the same evidence snapshot</strong><span>Choose this proposal or the other proposal with the same project and source digest—not both.</span></p> : null}<Provenance outcome={outcome} /><OutcomeMembers outcome={outcome} />{!canReviewExactProposal ? <p className="archaeology-review-only"><strong>Review only</strong><span>{applyCapability?.reason || "Exact canonical apply is unavailable for this proposal."}</span></p> : null}</div></article>;
        })}</div>
        {canReviewExactProposal && alternativeSourceKeys.size ? <p className="archaeology-review-note"><strong>Some proposals are alternatives.</strong> Commons preserves one canonical import per project and source digest. Selecting one same-source proposal replaces the other selection.</p> : null}
        {canReviewExactProposal ? <div className="archaeology-selected-review"><span>{selectedOutcomeIDs.length} of {outcomes.length} proposals selected</span><button className="primary-button" type="button" disabled={busy || !selectedOutcomeIDs.length || !review.batchId} onClick={() => onReview(review.batchId, selectedOutcomeIDs)}>{selectedOutcomeIDs.length === 1 ? "Review exact diff" : "Review exact combined diff"}</button></div> : null}
      </section>
      {members.length ? <section className="archaeology-review-section" aria-labelledby="archaeology-members-title">
        <div className="archaeology-section-heading"><div><h3 id="archaeology-members-title">Session members in this history</h3><p>Durable membership is separate from reachability, execution, and authority.</p></div></div>
        <div className="archaeology-members">{members.map((rawMember) => {
          const member = memberFacts(rawMember);
          return <details key={member.sessionID}><summary><span><strong>{member.displayName || member.sessionID}</strong><small>{member.displayName ? `${member.sessionID} · ` : ""}Commons member</small></span><span>{member.contributionCount} recorded contribution{member.contributionCount === 1 ? "" : "s"}</span></summary><dl><div><dt>Membership</dt><dd>Recorded in Commons history</dd></div><div><dt>Reachability</dt><dd>Historical or unknown</dd></div><div><dt>Execution</dt><dd>Not attested</dd></div><div><dt>Authority</dt><dd>Provenance only</dd></div><div><dt>Recorded collaboration links</dt><dd>{member.collaborationCount}</dd></div>{member.strengths.length ? <div><dt>Demonstrated strengths</dt><dd>{member.strengths.join(" · ")}</dd></div> : null}{member.uncertainties.length ? <div><dt>Uncertainties</dt><dd>{member.uncertainties.join(" · ")}</dd></div> : null}</dl></details>;
        })}</div>
      </section> : null}
      <p className="archaeology-review-note">{review.provenanceSummary}</p>
      <p className="archaeology-review-note">The canonical preview deduplicates reviewed history. Current Commons records win when history collides with newer work.</p>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer"><button className="secondary-button" type="button" onClick={onClose}>{backToRun ? "Back to run" : "Review later"}</button></footer>
    </div>
  );
}

export function ProjectArchaeologyDialog({ open, identity, archaeology, catalog, history, batch, installationStatus, busy = false, operations = {}, launchingCount = 0, refreshingProjects = false, updateStatus = null, error = "", onDiscover, onCatalogQuery, onCatalogMore, onHistoryMore, onSelectBatch, onBatchOutcomesMore, onRefreshInstallation, onStart, onCancel, onResolve, onRefresh, onReview, onSkip, onClose }) {
  const dialogRef = useRef(null);
  const modelConfigVersionRef = useRef("");
  const configDirtyRef = useRef(false);
  const refreshingProjectsRef = useRef(refreshingProjects);
  const batchIdentityRef = useRef(archaeology?.handoff?.batchId || "");
  const freshDraftSessionRef = useRef("");
  const [config, setConfig] = useState(() => configFromModel(archaeology?.config));
  const [configureAgain, setConfigureAgain] = useState(false);
  const serverView = !archaeology && (busy || error) ? "reading" : archaeologyView(archaeology);
  const [reviewingResults, setReviewingResults] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const view = historyOpen ? "history" : reviewingResults && archaeology?.review ? "review" : configureAgain ? "configure" : serverView;
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
      const preserveLocalConfig = shouldPreserveLocalArchaeologyConfig({ dirty: configDirtyRef.current });
      setConfig((current) => preserveLocalConfig
        ? reconcileConfigAfterCatalogRefresh(current, archaeology)
        : configFromModel(archaeology?.config));
      if (!preserveLocalConfig) configDirtyRef.current = false;
    }
  }, [archaeology?.id, archaeology?.revision, archaeology?.config]);

  useLayoutEffect(() => {
    if (!open) {
      freshDraftSessionRef.current = "";
      return;
    }
    const sessionID = archaeology?.id || "";
    if (!sessionID || archaeology?.state !== "draft" || archaeology?.handoff?.tasks?.length || freshDraftSessionRef.current === sessionID) return;
    freshDraftSessionRef.current = sessionID;
    configDirtyRef.current = true;
    setConfig(freshManualArchaeologyConfig(archaeology?.config));
  }, [open, archaeology?.id, archaeology?.state]);


  const catalogCandidateIdentity = (archaeology?.discovery?.candidates || []).map((candidate) => `${candidate.id}:${candidate.catalogState || "current"}`).join("|");
  useEffect(() => {
    if (!configDirtyRef.current) return;
    setConfig((current) => reconcileConfigAfterCatalogRefresh(current, archaeology));
  }, [catalogCandidateIdentity]);

  useEffect(() => {
    refreshingProjectsRef.current = refreshingProjects;
  }, [refreshingProjects]);

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
  function changeConfig(next) { configDirtyRef.current = true; setConfig(next); }
  function close() { setConfigureAgain(false); setReviewingResults(false); setHistoryOpen(false); onClose?.(); }
  function skip() { onSkip?.(); }
  function beginNewRun() { configDirtyRef.current = true; setConfig(freshManualArchaeologyConfig(archaeology?.config)); setConfigureAgain(true); }

  return (
    <dialog ref={dialogRef} className={`archaeology-dialog archaeology-dialog--${view}`} aria-labelledby="archaeology-title" aria-describedby="archaeology-description" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      <span className="sr-only" aria-live="polite">{selectedSummary}</span>
      {view !== "intro" && view !== "reading" && view !== "discovering" && view !== "discovery_failed" ? <><button className="archaeology-history-open" type="button" aria-pressed={historyOpen} onClick={() => setHistoryOpen((value) => !value)}><History aria-hidden="true" />{historyOpen ? "Current run" : "Run history"}</button><button className="archaeology-close" type="button" onClick={close}>Close</button></> : null}
      {view === "intro" ? <Intro identity={identity} model={archaeology} busy={busy} error={error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "reading" ? <ReadingState error={error} onRetry={onRefresh} onClose={close} /> : null}
      {view === "discovering" || view === "discovery_failed" ? <DiscoveryState failed={view === "discovery_failed"} error={error || archaeology?.discovery?.error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "handoff" ? <Handoff model={archaeology} busy={busy} error={error} updateStatus={updateStatus} onRefresh={onRefresh} onCancel={onCancel} onResolve={onResolve} onInspectReview={() => setReviewingResults(true)} onNewRun={beginNewRun} onClose={close} /> : null}
      {view === "configure" ? <Configure model={archaeology} catalog={catalog} config={config} operations={operations} launchingCount={launchingCount} refreshing={refreshingProjects} error={error} onChange={changeConfig} onDiscover={onDiscover} onCatalogQuery={onCatalogQuery} onCatalogMore={onCatalogMore} onStart={onStart} onSkip={skip} /> : null}
      {view === "running" || view === "paused" ? <LegacyRuns model={archaeology} error={error} onClose={close} /> : null}
      {view === "review" ? <Review model={archaeology} busy={busy} error={error} onReview={onReview} backToRun={reviewingResults} onClose={reviewingResults ? () => setReviewingResults(false) : close} /> : null}
      {view === "history" ? <ProjectArchaeologyHistory history={history} batch={batch} status={installationStatus} candidates={archaeology?.discovery?.candidates || []} error={history?.error} onSelectBatch={onSelectBatch} onLoadMore={onHistoryMore} onLoadMoreOutcomes={onBatchOutcomesMore} onRefreshStatus={onRefreshInstallation} onReview={onReview} onBack={() => setHistoryOpen(false)} /> : null}
      {view === "failed" ? <DiscoveryState failed error={error || "Project exploration stopped before a review was ready."} onDiscover={() => onStart(config)} onSkip={close} /> : null}
    </dialog>
  );
}
