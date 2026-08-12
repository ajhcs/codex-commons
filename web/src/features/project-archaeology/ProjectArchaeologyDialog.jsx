import { useEffect, useMemo, useRef, useState } from "react";
import BookOpen from "../../icons/BookOpen.tsx";
import Branch from "../../icons/Branch.tsx";
import CheckCircle from "../../icons/CheckCircle.tsx";
import Clock from "../../icons/Clock.tsx";
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
  taskPackText,
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

function Intro({ identity, model, busy, error, onDiscover, onSkip }) {
  const discovery = model?.capabilities?.discovery;
  return (
    <div className="archaeology-intro">
      <div className="archaeology-identity" aria-label={`Signed in as ${identityLabel(identity)}`}>
        <span aria-hidden="true">{(identity?.displayName || identity?.handle || "C").slice(0, 1).toUpperCase()}</span>
        <strong>{identityLabel(identity)}</strong>
        <small>Identity connected</small>
      </div>
      <div className="archaeology-intro-copy">
        <h2 id="archaeology-title">Bring your work into Commons</h2>
        <p id="archaeology-description">Find projects you may want Commons to understand. Discovery checks project names, Git and documentation signals, Codex-history availability, and recent activity only.</p>
      </div>
      <div className="archaeology-privacy-note">
        <strong>Metadata only at this step</strong>
        <span>Commons does not read file contents or conversations until you select projects and start an exploration.</span>
      </div>
      {!error && discovery?.available === false ? <p className="archaeology-message" role="status">{discovery.reason || "Project discovery is not configured on this installation."}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer">
        <button className="secondary-button" type="button" onClick={onSkip}>Skip for now</button>
        <button className="primary-button" type="button" disabled={busy || discovery?.available !== true} onClick={onDiscover}>{busy ? "Finding projects…" : "Find projects"}</button>
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

function CandidateList({ candidates, config, disabled, onChange }) {
  const allSelected = candidates.length > 0 && candidates.every((candidate) => config.selectedProjectIds.includes(candidate.id));
  const toggleAll = () => onChange({ ...config, selectedProjectIds: allSelected ? [] : candidates.map((candidate) => candidate.id) });
  const toggle = (id) => onChange({
    ...config,
    selectedProjectIds: config.selectedProjectIds.includes(id)
      ? config.selectedProjectIds.filter((candidateID) => candidateID !== id)
      : [...config.selectedProjectIds, id],
  });
  return (
    <section className="archaeology-projects" aria-labelledby="archaeology-projects-title">
      <div className="archaeology-section-heading">
        <div><h3 id="archaeology-projects-title">Projects</h3><p>{config.selectedProjectIds.length} of {candidates.length} selected</p></div>
        <button type="button" disabled={disabled || !candidates.length} onClick={toggleAll}>{allSelected ? "Clear all" : "Select all"}</button>
      </div>
      {candidates.length ? <div className="archaeology-candidate-list">
        {candidates.map((candidate) => {
          const checked = config.selectedProjectIds.includes(candidate.id);
          const sources = sourceLabels(candidate.signals);
          return (
            <label key={candidate.id} className={`archaeology-candidate${checked ? " is-selected" : ""}`}>
              <input type="checkbox" checked={checked} disabled={disabled} onChange={() => toggle(candidate.id)} />
              <span className="archaeology-candidate-mark" aria-hidden="true"><Folder /></span>
              <span className="archaeology-candidate-copy">
                <strong>{candidate.name}</strong>
                <small>{candidate.pathLabel || "Project folder"}</small>
                <span>{sources.length ? sources.join(" · ") : "No supported sources found"}{candidate.lastActivity ? ` · Active ${candidate.lastActivity.relative}` : " · No recent activity recorded"}</span>
                <em>{candidate.privacyNote}</em>
              </span>
              <span className="archaeology-candidate-estimate"><span><Clock aria-hidden="true" />{formatDurationRange(candidate.estimate)}</span><small>{costCopy[candidate.estimate?.relativeCost] || "Model use varies"}</small></span>
            </label>
          );
        })}
      </div> : <div className="archaeology-empty"><strong>No candidate projects found</strong><span>You can skip for now and return when another project folder is available.</span></div>}
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
  return (
    <div className="archaeology-configure">
      <header className="archaeology-content-heading">
        <div><span>Optional setup</span><h2 id="archaeology-title">Choose what Commons should explore</h2><p id="archaeology-description">Nothing is imported from this screen. Commons prepares one bounded Codex task per project and waits for a validated report.</p></div>
      </header>
      <CandidateList candidates={candidates} config={config} disabled={busy} onChange={onChange} />
      <Configuration config={config} disabled={busy} onChange={onChange} />
      <div className="archaeology-truth-note"><strong>{formatDurationRange({ durationSecondsMin: config.depth === "quick" ? 60 : config.depth === "deep" ? 480 : 240, durationSecondsMax: config.depth === "quick" ? 240 : config.depth === "deep" ? 1800 : 900 })} per project</strong><span>Time and model usage vary with project history. Commons will show real project states, not a simulated percentage.</span></div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {model?.capabilities?.historianHandoff?.available === false ? <p className="archaeology-message" role="status">{model.capabilities.historianHandoff.reason || "Codex task-pack handoff is not available on this installation."}</p> : null}
      <footer className="archaeology-footer">
        <button className="secondary-button" type="button" disabled={busy} onClick={onSkip}>Skip for now</button>
        <button className="primary-button" type="button" disabled={busy || !valid || model?.capabilities?.historianHandoff?.available !== true} onClick={() => onStart(config)}>{busy ? "Preparing…" : "Prepare Codex task pack"}</button>
      </footer>
    </div>
  );
}

function Handoff({ model, busy, error, onRefresh, onClose }) {
  const handoff = model?.handoff || {};
  const [copyState, setCopyState] = useState("");
  const ready = handoff.state === "ready_to_claim";
  const claimed = handoff.state === "claimed";

  async function copyPack() {
    if (typeof globalThis.navigator?.clipboard?.writeText !== "function") {
      setCopyState("Copy is unavailable in this browser.");
      return;
    }
    try {
      await globalThis.navigator.clipboard.writeText(taskPackText(handoff));
      setCopyState("Task pack copied.");
    } catch {
      setCopyState("Copy is unavailable in this browser.");
    }
  }

  return (
    <div className="archaeology-runs archaeology-handoff">
      <header className="archaeology-content-heading">
        <div><span>Codex-owned handoff</span><h2 id="archaeology-title">{ready ? "Continue in Codex" : claimed ? "Codex is reviewing the history" : "Task pack needs attention"}</h2><p id="archaeology-description">Commons prepared a durable, bounded task pack. It did not start an agent or claim that work is running.</p></div>
      </header>
      <div className="archaeology-handoff-id"><span>Handoff</span><code>{handoff.id || "Not available"}</code></div>
      <div className="archaeology-run-list" aria-live="polite">
        {(handoff.pack?.projects || []).map((project) => (
          <article key={project.candidateId} className="archaeology-run">
            <span className="archaeology-run-mark" aria-hidden="true"><BookOpen /></span>
            <div><strong>{project.label}</strong><span>Bounded Codex task</span><small>{model.config?.depth || "standard"} depth · source selection retained</small></div>
            <details className="archaeology-task-prompt"><summary>View instructions</summary><p>{project.taskPrompt}</p></details>
          </article>
        ))}
      </div>
      <div className="archaeology-truth-note">
        <strong>{ready ? "Ready for an exact Codex session to claim" : claimed ? `Claimed by ${handoff.claimedBy || "a Codex session"}` : "No report is available"}</strong>
        <span>{ready ? "Copy the pack into Codex. This installation keeps the handoff explicit until direct agent join is available." : claimed ? "Commons will accept only a validated report from the exact claiming session. Refresh to check for a review." : handoff.failure || "The server could not prepare this handoff."}</span>
      </div>
      {copyState ? <p className="archaeology-message" role="status">{copyState}</p> : null}
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer archaeology-footer--between">
        <button className="secondary-button" type="button" onClick={onClose}>Close</button>
        <div>
          {(ready || claimed) ? <button className="secondary-button" type="button" onClick={copyPack}>Copy task pack</button> : null}
          <button className="primary-button" type="button" disabled={busy} onClick={onRefresh}>{busy ? "Checking…" : "Refresh status"}</button>
        </div>
      </footer>
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
  const view = archaeologyView(archaeology);

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
      {view !== "intro" && view !== "discovering" && view !== "discovery_failed" ? <button className="archaeology-close" type="button" onClick={close}>Close</button> : null}
      {view === "intro" ? <Intro identity={identity} model={archaeology} busy={busy} error={error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "discovering" || view === "discovery_failed" ? <DiscoveryState failed={view === "discovery_failed"} error={error || archaeology?.discovery?.error} onDiscover={onDiscover} onSkip={skip} /> : null}
      {view === "handoff" ? <Handoff model={archaeology} busy={busy} error={error} onRefresh={onRefresh} onClose={close} /> : null}
      {view === "configure" ? <Configure model={archaeology} config={config} busy={busy} error={error} onChange={setConfig} onStart={onStart} onSkip={skip} /> : null}
      {view === "running" || view === "paused" ? <Runs model={archaeology} busy={busy} error={error} onPause={onPause} onResume={onResume} onCancel={onCancel} onClose={close} /> : null}
      {view === "review" ? <Review model={archaeology} busy={busy} error={error} onReview={onReview} onClose={close} /> : null}
      {view === "failed" ? <DiscoveryState failed error={error || "Project exploration stopped before a review was ready."} onDiscover={() => onStart(config)} onSkip={close} /> : null}
    </dialog>
  );
}
