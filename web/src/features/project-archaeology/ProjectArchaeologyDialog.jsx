import { useEffect, useMemo, useRef, useState } from "react";
import { copyText, manualCopyShortcut } from "../../browser/copyText.js";
import CommonsMark from "../../components/CommonsMark.jsx";
import BookOpen from "../../icons/BookOpen.tsx";
import Branch from "../../icons/Branch.tsx";
import CheckCircle from "../../icons/CheckCircle.tsx";
import Clock from "../../icons/Clock.tsx";
import Copy from "../../icons/Copy.tsx";
import Search from "../../icons/Search.tsx";
import FileDocument from "../../icons/FileDocument.tsx";
import Folder from "../../icons/Folder.tsx";
import Pause from "../../icons/Pause.tsx";
import Play from "../../icons/Play.tsx";
import Stop from "../../icons/Stop.tsx";
import {
  ARCHAEOLOGY_DEPTHS,
  archaeologyView,
  canStartArchaeology,
  configFromModel,
  formatDurationRange,
  memberFacts,
  runProgressText,
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

function phaseStatus(state) {
  return ({ queued: "Queued", running: "Exploring", pause_requested: "Pausing", paused: "Paused", cancel_requested: "Canceling", canceled: "Canceled", completed: "Ready to review", failed: "Needs attention" })[state] || "Preparing";
}

function launchStatus(state) {
  return ({ preparing: "Preparing", starting_codex: "Starting in Codex", task_created: "Task created", claimed: "Claimed", running: "Running", report_ready: "Report ready", completed: "Complete", failed: "Needs attention", uncertain: "Status uncertain" })[state] || "Preparing";
}

function launchDetail(state) {
  if (state === "uncertain") return "Commons will not retry automatically because Codex may have accepted the task.";
  if (state === "failed") return "This task did not reach a confirmed running state. Refresh before taking another action.";
  if (state === "preparing" || state === "starting_codex") return "Commons is durably tracking this launch.";
  return "Canonical import still requires your review.";
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

function CandidateList({ candidates, config, disabled, onChange }) {
  const [query, setQuery] = useState("");
  const normalizedQuery = query.trim().toLowerCase();
  const visible = candidates.filter((candidate) => !normalizedQuery || [candidate.name, candidate.repositoryLabel].some((value) => value?.toLowerCase().includes(normalizedQuery)));
  const selected = new Set(config.selectedProjectIds);
  const selectedVisible = visible.filter((candidate) => selected.has(candidate.id));
  const toggleVisible = () => {
    const next = new Set(config.selectedProjectIds);
    if (visible.length && selectedVisible.length === visible.length) visible.forEach((candidate) => next.delete(candidate.id));
    else visible.forEach((candidate) => next.add(candidate.id));
    onChange({ ...config, selectedProjectIds: [...next] });
  };
  const toggle = (id) => onChange({ ...config, selectedProjectIds: selected.has(id) ? config.selectedProjectIds.filter((value) => value !== id) : [...config.selectedProjectIds, id] });
  const sorted = [...visible].sort((a, b) => (b.lastActivity?.iso || "").localeCompare(a.lastActivity?.iso || "") || a.name.localeCompare(b.name));
  return (
    <section className="archaeology-projects" aria-labelledby="archaeology-projects-title">
      <div className="archaeology-catalog-toolbar">
        <label className="archaeology-search"><Search aria-hidden="true" /><span className="sr-only">Search projects</span><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search Codex projects" /></label>
        <span>{config.selectedProjectIds.length} selected</span>
        <button type="button" disabled={disabled || !visible.length} onClick={toggleVisible}>{visible.length && selectedVisible.length === visible.length ? "Clear visible" : "Select visible"}</button>
      </div>
      <div className="archaeology-section-heading"><div><h3 id="archaeology-projects-title">Codex projects</h3><p>{visible.length} of {candidates.length} shown</p></div></div>
      {sorted.length ? <div className="archaeology-candidate-list">{sorted.map((candidate) => {
        const checked = selected.has(candidate.id);
        const sources = sourceLabels(candidate.signals);
        return (
          <label key={candidate.id} className={["archaeology-candidate", checked ? "is-selected" : ""].filter(Boolean).join(" ")}>
            <input type="checkbox" checked={checked} disabled={disabled} onChange={() => toggle(candidate.id)} />
            <span className="archaeology-candidate-mark" aria-hidden="true"><Folder /></span>
            <span className="archaeology-candidate-copy"><strong>{candidate.name}</strong><small>{candidate.repositoryLabel || "Codex project"}</small><span>{sources.join(" · ") || "Metadata only"}{candidate.codexThreadCount ? ` · ${candidate.codexThreadCount} Codex task${candidate.codexThreadCount === 1 ? "" : "s"}` : ""}</span></span>
            <span className="archaeology-candidate-estimate"><span><Clock aria-hidden="true" />{formatDurationRange(candidate.estimate)}</span><small>{candidate.lastActivity ? `Active ${candidate.lastActivity.relative}` : costCopy[candidate.estimate?.relativeCost] || "Time varies"}</small></span>
          </label>
        );
      })}</div> : <div className="archaeology-empty"><strong>No matching projects</strong><span>Try another name or clear the search.</span></div>}
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

function Configure({ model, config, busy, error, onChange, onStart, onSkip }) {
  const candidates = model?.discovery?.candidates || [];
  const valid = canStartArchaeology(config, candidates);
  const launch = model?.capabilities?.taskLaunch;
  return (
    <div className="archaeology-configure">
      <header className="archaeology-content-heading"><div><span>Project Archaeology</span><h2 id="archaeology-title">Choose your Codex projects</h2><p id="archaeology-description">Select the work Commons should understand. One ordinary Codex task starts per project; nothing is imported automatically.</p></div></header>
      <div className="archaeology-workspace">
        <aside className="archaeology-trust-panel">
          <CommonsMark state="offered" size="large" />
          <h3>Codex stays in control</h3>
          <p>Projects come from the App Server paired with this browser. Commons receives labels and capability facts here—not raw paths or thread bodies.</p>
          <dl><div><dt>Selected</dt><dd>{config.selectedProjectIds.length} projects</dd></div><div><dt>Mode</dt><dd>{config.depth} review</dd></div><div><dt>Sources</dt><dd>{selectedSourceCount(config)} enabled</dd></div></dl>
        </aside>
        <div className="archaeology-catalog-panel"><CandidateList candidates={candidates} config={config} disabled={busy} onChange={onChange} /></div>
      </div>
      <details className="archaeology-advanced"><summary>Advanced settings <span>{config.depth} · {selectedSourceCount(config)} sources · {config.maxConcurrency} at once</span></summary><Configuration config={config} disabled={busy} onChange={onChange} /></details>
      <div className="archaeology-truth-note"><strong>Results become reviewable proposals</strong><span>Tasks may take several minutes. Commons reports literal task states and never simulates a percentage or applies history without your confirmation.</span></div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {launch?.available === false ? <p className="archaeology-message" role="status">{launch.reason || "Direct Codex task launch is not available on this installation."}</p> : null}
      <footer className="archaeology-footer archaeology-footer--between"><button className="secondary-button" type="button" disabled={busy} onClick={onSkip}>Not now</button><button className="primary-button" type="button" disabled={busy || !valid || launch?.available !== true} onClick={() => onStart(config)}>{busy ? "Starting tasks…" : `Start Codex tasks · ${config.selectedProjectIds.length}`}</button></footer>
    </div>
  );
}

function Handoff({ model, busy, error, onRefresh, onClose }) {
  const handoff = model?.handoff || {};
  const tasks = handoff.tasks || [];
  const candidates = model?.discovery?.candidates || [];
  const [copyState, setCopyState] = useState("");
  const readyCount = tasks.filter((task) => ["report_ready", "completed"].includes(task.state)).length;
  async function copyID(value, label) {
    const copied = await copyText(value);
    setCopyState(copied ? `${label} copied.` : `Copy did not work. Select the ID and press ${manualCopyShortcut()}.`);
  }
  return (
    <div className="archaeology-runs archaeology-handoff">
      <header className="archaeology-content-heading"><div><span>Codex tasks</span><h2 id="archaeology-title">{readyCount ? `${readyCount} report${readyCount === 1 ? "" : "s"} ready` : "Project history is underway"}</h2><p id="archaeology-description">These are ordinary tasks running in your paired Codex App Server. Exact task identities appear only as secondary provenance.</p></div></header>
      <div className="archaeology-run-list" aria-live="polite">{tasks.map((task) => (
        <article key={task.projectId} className={["archaeology-run", `archaeology-run--${task.state}`].join(" ")}>
          <span className="archaeology-run-mark" aria-hidden="true">{["report_ready", "completed"].includes(task.state) ? <CheckCircle /> : <BookOpen />}</span>
          <div><strong>{candidateName(candidates, task.projectId)}</strong><span>{launchStatus(task.state)}</span><small>{launchDetail(task.state)}</small></div>
          <div className="archaeology-run-facts"><span>{launchStatus(task.state)}</span>{task.threadId ? <span className="archaeology-task-identity"><code>{task.threadId}</code><button type="button" className="archaeology-id-copy" onClick={() => copyID(task.threadId, "Thread ID")} aria-label="Copy thread ID"><Copy aria-hidden="true" /> Copy</button></span> : <small>Waiting for task identity</small>}</div>
        </article>
      ))}</div>
      {!tasks.length ? <div className="archaeology-empty"><strong>No task rows yet</strong><span>Refresh to read the durable launch state.</span></div> : null}
      {copyState ? <p className="archaeology-message" role="status">{copyState}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer archaeology-footer--between"><button className="secondary-button" type="button" onClick={onClose}>Close</button><button className="primary-button" type="button" disabled={busy} onClick={onRefresh}>{busy ? "Checking…" : "Refresh status"}</button></footer>
    </div>
  );
}

function Runs({ model, busy, error, onPause, onResume, onCancel, onClose }) {
  const candidates = model?.discovery?.candidates || [];
  const paused = model.state === "paused";
  return (
    <div className="archaeology-runs">
      <header className="archaeology-content-heading">
        <div><span>Project archaeology</span><h2 id="archaeology-title">{paused ? "Exploration paused" : "Exploring project history"}</h2><p id="archaeology-description">Each project has one historian. Counts below come from completed source work; unknown totals stay unlabeled.</p></div>
      </header>
      <div className="archaeology-run-list" aria-live="polite">
        {(model.runs || []).map((run) => (
          <article key={run.id} className={`archaeology-run archaeology-run--${run.state}`}>
            <span className="archaeology-run-mark" aria-hidden="true">{run.state === "completed" ? <CheckCircle /> : <BookOpen />}</span>
            <div><strong>{candidateName(candidates, run.projectId)}</strong><span>{run.phaseLabel || phaseStatus(run.state)}</span><small>{runProgressText(run)}</small></div>
            <div className="archaeology-run-facts"><span>{phaseStatus(run.state)}</span><small>{Number(run.outcomesFound) || 0} outcomes found</small></div>
          </article>
        ))}
      </div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <p className="archaeology-background-note">You can close this window while historians work. Closing does not pause or cancel exploration.</p>
      <footer className="archaeology-footer archaeology-footer--between">
        <button className="secondary-button" type="button" onClick={onClose}>Close</button>
        <div>
          {model.controls?.canCancel ? <button className="secondary-button" type="button" disabled={busy} onClick={onCancel}><Stop aria-hidden="true" />Cancel</button> : null}
          {paused && model.controls?.canResume ? <button className="primary-button" type="button" disabled={busy} onClick={onResume}><Play aria-hidden="true" />Resume</button> : null}
          {!paused && model.controls?.canPause ? <button className="primary-button" type="button" disabled={busy} onClick={onPause}><Pause aria-hidden="true" />Pause</button> : null}
        </div>
      </footer>
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

export function ProjectArchaeologyDialog({ open, identity, archaeology, busy = false, error = "", onDiscover, onStart, onPause, onResume, onCancel, onRefresh, onReview, onSkip, onClose }) {
  const dialogRef = useRef(null);
  const modelIDRef = useRef("");
  const [config, setConfig] = useState(() => configFromModel(archaeology?.config));
  const view = !archaeology && (busy || error) ? "reading" : archaeologyView(archaeology);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    const modelID = archaeology?.id || "";
    if (modelID !== modelIDRef.current) {
      modelIDRef.current = modelID;
      setConfig(configFromModel(archaeology?.config));
    }
  }, [archaeology?.id, archaeology?.config]);

  const selectedSummary = useMemo(() => {
    const projects = config.selectedProjectIds.length;
    const sources = selectedSourceCount(config);
    return `${projects} project${projects === 1 ? "" : "s"} and ${sources} source${sources === 1 ? "" : "s"} selected`;
  }, [config]);
  function close() { onClose?.(); }
  function skip() { onSkip?.(); }

  return (
    <dialog ref={dialogRef} className={`archaeology-dialog archaeology-dialog--${view}`} aria-labelledby="archaeology-title" aria-describedby="archaeology-description" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      <span className="sr-only" aria-live="polite">{selectedSummary}</span>
      {view !== "intro" && view !== "reading" && view !== "discovering" && view !== "discovery_failed" ? <button className="archaeology-close" type="button" onClick={close}>Close</button> : null}
      {view === "intro" ? <Intro identity={identity} model={archaeology} busy={busy} error={error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "reading" ? <ReadingState error={error} onRetry={onRefresh} onClose={close} /> : null}
      {view === "discovering" || view === "discovery_failed" ? <DiscoveryState failed={view === "discovery_failed"} error={error || archaeology?.discovery?.error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "handoff" ? <Handoff model={archaeology} busy={busy} error={error} onRefresh={onRefresh} onClose={close} /> : null}
      {view === "configure" ? <Configure model={archaeology} config={config} busy={busy} error={error} onChange={setConfig} onStart={onStart} onSkip={skip} /> : null}
      {view === "running" || view === "paused" ? <Runs model={archaeology} busy={busy} error={error} onPause={onPause} onResume={onResume} onCancel={onCancel} onClose={close} /> : null}
      {view === "review" ? <Review model={archaeology} busy={busy} error={error} onReview={onReview} onClose={close} /> : null}
      {view === "failed" ? <DiscoveryState failed error={error || "Project exploration stopped before a review was ready."} onDiscover={() => onStart(config)} onSkip={close} /> : null}
    </dialog>
  );
}
