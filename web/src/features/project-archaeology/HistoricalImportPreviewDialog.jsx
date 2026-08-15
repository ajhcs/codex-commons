import { useEffect, useRef, useState } from "react";
import CheckCircle from "../../icons/CheckCircle.tsx";

function countRows(counts = {}) {
  return [
    ["Historical tasks", counts.tasks || 0],
    ["Session aliases", counts.projectThreadAliases || 0],
    ["Attributions", counts.attributions || 0],
    ["Task events", counts.events || 0],
  ];
}

export function HistoricalImportPreviewDialog({ open, bridge, reviewedPages = 0, busy = false, error = "", applied = null, onLoadMore, onConfirm, onClose, onOpenProject }) {
  const dialogRef = useRef(null);
  const headingRef = useRef(null);
  const [confirmation, setConfirmation] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const preview = bridge?.preview;
  const proposal = bridge?.proposal;
  const exact = preview?.manifestDigest || "";
  const reviewComplete = Boolean(bridge?.reviewCompletionToken) && !bridge?.nextCursor;
  const receiptByKey = new Map((preview?.tasks || []).map((task) => [task.key, task]));
  const projectGroups = Object.values((bridge?.projects || []).reduce((groups, project) => {
    const current = groups[project.projectId] || { projectId: project.projectId, projectName: project.projectName, tasks: [] };
    return { ...groups, [project.projectId]: { ...current, projectName: current.projectName || project.projectName, tasks: (proposal?.tasks || []).filter((task) => task.projectId === project.projectId) } };
  }, {}));
  const groupedTasks = projectGroups.length ? projectGroups : [{ projectId: bridge?.projectId || "Project", tasks: proposal?.tasks || [] }];

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    setConfirmation("");
    setReviewed(false);
    queueMicrotask(() => headingRef.current?.focus());
  }, [bridge?.preview?.manifestDigest, open]);

  function close() { onClose?.(); }

  return (
    <dialog ref={dialogRef} className="archaeology-dialog historical-import-dialog" aria-labelledby="historical-import-title" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      {applied ? (
        <div className="historical-import-success">
          <span className="archaeology-state-mark" aria-hidden="true"><CheckCircle /></span>
          <h2 id="historical-import-title">Reviewed history is now current</h2>
          <p>Commons applied the complete selected manifest in one transaction. The durable receipt below binds the exact reviewed selection; nothing was partially applied.</p>
          <dl className="historical-import-counts"><div><dt>Selected proposals</dt><dd>{applied.outcomeIds?.length || 0}</dd></div><div><dt>Manifest</dt><dd><code>{applied.manifestDigest}</code></dd></div><div><dt>Audit receipt</dt><dd><code>{applied.auditId}</code></dd></div></dl>
          <footer className="archaeology-footer"><button className="primary-button" type="button" onClick={close}>Done</button></footer>
        </div>
      ) : (
        <div className="historical-import-preview">
          <header className="archaeology-content-heading"><div><span>Human approval</span><h2 ref={headingRef} id="historical-import-title" tabIndex="-1">Review the exact history proposal</h2><p>Nothing has been applied. Inspect every proposed task and its cited evidence, then confirm the server-derived manifest that binds this review.</p></div></header>
          <section className="historical-import-summary" aria-labelledby="historical-import-summary-title">
            <div><h3 id="historical-import-summary-title">{bridge?.batchId || preview?.batchId || "Historical import"}</h3><span>{bridge?.outcomeIds?.length || 1} selected proposal{bridge?.outcomeIds?.length === 1 ? "" : "s"} · all or nothing</span></div>
            <dl className="historical-import-counts">{countRows(preview?.counts).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
            <dl className="historical-import-digests">
              <div><dt>Server manifest digest</dt><dd><code>{exact}</code></dd></div>
              <div><dt>Selection digest</dt><dd><code>{preview?.selectionDigest || bridge?.selectionDigest || ""}</code></dd></div>
            </dl>
          </section>
          <section className="historical-proposal" aria-labelledby="historical-proposal-title">
            <header><div><h3 id="historical-proposal-title">Proposed tasks</h3><p>These exact records and evidence will be submitted to canonical history.</p></div><span>{proposal?.tasks?.length || 0}</span></header>
            {proposal?.projectThreadAliases?.length ? (
              <details className="historical-aliases">
                <summary>Session aliases · {proposal.projectThreadAliases.length}</summary>
                <ul>{proposal.projectThreadAliases.map((alias) => <li key={`${alias.projectId || "project"}:${alias.outcomeId || "proposal"}:${alias.alias}`}><div><strong>{alias.alias}</strong><span>{alias.outcomeId ? `Proposal ${alias.outcomeId} · ` : ""}{alias.session}</span></div><SourceFacts source={alias.source} /></li>)}</ul>
              </details>
            ) : null}
            <div className="historical-project-groups">{groupedTasks.map((group) => <section key={group.projectId} className="historical-project-group" aria-labelledby={`historical-project-${group.projectId.replaceAll(/[^a-zA-Z0-9_-]/g, "-")}`}><header><div><span>Project</span><h3 id={`historical-project-${group.projectId.replaceAll(/[^a-zA-Z0-9_-]/g, "-")}`}>{group.projectName || "Codex project"}</h3><small>{group.projectId}</small></div><span>{group.tasks.length} Task{group.tasks.length === 1 ? "" : "s"}</span></header><div className="historical-task-list">{group.tasks.map((task) => {
              const receipt = receiptByKey.get(task.key);
              return (
                <article className="historical-task" key={task.key}>
                  <header><div><span>{task.outcomeId ? `Proposal ${task.outcomeId} · ` : ""}{task.originalKey || task.key}</span><h3>{task.title}</h3></div><div className="historical-task-status"><span>Priority {task.priority}</span><strong>{dispositionLabel(receipt?.disposition)}</strong></div></header>
                  {task.description ? <section><h4>Description</h4><p>{task.description}</p></section> : null}
                  {task.acceptance ? <section><h4>Acceptance</h4><p>{task.acceptance}</p></section> : null}
                  <SourceFacts source={task.source} />
                  <EvidenceList title={`Attributions · ${task.attributions.length}`} items={task.attributions} />
                  <EvidenceList title={`Task events · ${task.events.length}`} items={task.events} event />
                </article>
              );
            })}</div></section>)}</div>
          </section>
          <div className="archaeology-truth-note"><strong>Current Commons records win</strong><span>Reviewed source keys are deduplicated. A historical record cannot overwrite newer canonical work; safe replays do not duplicate it.</span></div>
          {bridge?.nextCursor ? <div className="historical-import-pages"><span>{reviewedPages} exact-diff page{reviewedPages === 1 ? "" : "s"} reviewed · server review in progress</span><button className="secondary-button" type="button" disabled={busy} onClick={onLoadMore}>{busy ? "Loading exact diff…" : "Review next exact-diff page"}</button></div> : reviewComplete ? <div className="historical-import-pages is-complete" role="status"><span>All {reviewedPages} exact-diff page{reviewedPages === 1 ? "" : "s"} reviewed · completion verified by Commons</span></div> : null}
          <label className="historical-import-reviewed"><input type="checkbox" checked={reviewed} disabled={!reviewComplete} onChange={(event) => setReviewed(event.target.checked)} /><span>{reviewComplete ? "I reviewed every selected proposal, Task, source, attribution, event, collision and disposition shown above." : "Continue in order until Commons verifies every exact-diff page before acknowledging this manifest."}</span></label>
          <label className="historical-import-confirmation"><span>Confirm the server manifest digest</span><input name="manifest-digest-confirmation" value={confirmation} disabled={!reviewComplete} onChange={(event) => setConfirmation(event.target.value.trim())} autoComplete="off" spellCheck="false" aria-describedby="historical-import-confirmation-help" /><small id="historical-import-confirmation-help">{reviewComplete ? <>Enter the complete server-derived manifest digest shown above. The selection digest <code>{preview?.selectionDigest || bridge?.selectionDigest}</code> binds the selected batch proposals; the manifest independently binds every simulated canonical disposition and revision. This server-attested review expires {bridge.reviewExpiresAt?.absolute || "soon"}.</> : "Manifest confirmation unlocks only after Commons attests the complete bounded review."}</small></label>
          {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
          <footer className="archaeology-footer"><button className="secondary-button" type="button" disabled={busy} onClick={close}>Review later</button><button className="primary-button" type="button" disabled={busy || !reviewComplete || !reviewed || !exact || !proposal?.tasks?.length || confirmation !== exact} onClick={() => onConfirm(confirmation, true)}>{busy ? "Applying complete manifest…" : "Apply selected history"}</button></footer>
        </div>
      )}
    </dialog>
  );
}

function sourceWhen(source) {
  return source?.occurredAt?.absolute || source?.occurredAt?.iso || source?.occurredAt || "Time unavailable";
}

function SourceFacts({ source }) {
  if (!source) return null;
  return (
    <dl className="historical-source-facts">
      <div><dt>Source</dt><dd><code>{source.kind}:{source.stableId}</code></dd></div>
      <div><dt>Digest</dt><dd><code>{source.digest}</code></dd></div>
      <div><dt>Occurred</dt><dd>{sourceWhen(source)}</dd></div>
    </dl>
  );
}

function dispositionLabel(value) {
  if (value === "created") return "Will create";
  if (value === "skipped_current") return "Keeps current";
  if (value === "replayed") return "Already applied";
  return "Previewed";
}

function EvidenceList({ title, items, event = false }) {
  if (!items?.length) return null;
  return (
    <section className="historical-evidence-group">
      <h4>{title}</h4>
      <ul>{items.map((item) => (
        <li key={event ? item.key : `${item.session}-${item.role}`}>
          <div><strong>{event ? item.summary : item.session}</strong><span>{event ? `${item.kind} · ${item.confidence}${item.session ? ` · ${item.session}` : ""}` : `${item.role} · ${item.confidence}`}</span></div>
          <SourceFacts source={item.source} />
        </li>
      ))}</ul>
    </section>
  );
}
