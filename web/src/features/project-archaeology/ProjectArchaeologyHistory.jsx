import { useEffect, useState } from "react";
import BookOpen from "../../icons/BookOpen.tsx";
import CheckCircle from "../../icons/CheckCircle.tsx";
import Clock from "../../icons/Clock.tsx";
import History from "../../icons/History.tsx";
import { archaeologyTaskPresentation, reconcileOutcomeSelection, sourceLabels } from "./projectArchaeologyState.js";

function statusTone(value) {
  return ["verified", "healthy"].includes(value) ? "healthy" : ["failed", "attention"].includes(value) ? "attention" : "quiet";
}

function StatusFact({ label, value, detail, tone = "quiet" }) {
  return <div className={`archaeology-health-fact is-${tone}`}><dt>{label}</dt><dd>{value}</dd>{detail ? <small>{detail}</small> : null}</div>;
}

function VerificationFact({ label, check }) {
  const value = check.status === "unknown" ? "Not yet verified" : check.status === "verified" ? "Verified" : `${check.violations} violation${check.violations === 1 ? "" : "s"}`;
  const detail = check.checkedAt?.relative || (check.status === "unknown" ? "No verified check recorded" : `${check.violations} recorded`);
  return <StatusFact label={label} value={value} detail={detail} tone={statusTone(check.status)} />;
}

function InstallationStatus({ status, loading, error, onRefresh }) {
  const evidence = status?.evidence;
  const betaReady = evidence?.betaPrerequisitesMet === true;
  const betaBlockers = evidence ? [
    evidence.uncertainHistorians ? `${evidence.uncertainHistorians} uncertain historian${evidence.uncertainHistorians === 1 ? "" : "s"}` : "",
    evidence.lostReports ? `${evidence.lostReports} completed without a report` : "",
    ...[["Repeated report reads", evidence.reportRecovery], ["Duplicate launch check", evidence.duplicateLaunchCheck], ["Repository immutability", evidence.repositoryImmutability], ["Canonical immutability", evidence.canonicalImmutability]].filter(([, check]) => check.status !== "verified").map(([label, check]) => `${label}: ${check.status === "unknown" ? "not yet verified" : "attention"}`),
    evidence.restoreDrill.status !== "verified" ? `Restore drill: ${evidence.restoreDrill.status === "unknown" ? "not yet verified" : "failed"}` : "",
  ].filter(Boolean) : [];
  return (
    <details className="archaeology-installation" open={Boolean(error)}>
      <summary><span><strong>Installation status</strong><small>{loading ? "Checking…" : error ? "Needs attention" : status?.archaeology?.uncertainCount ? `${status.archaeology.uncertainCount} uncertain` : "Operational facts"}</small></span></summary>
      <button className="archaeology-installation-refresh" type="button" disabled={loading} onClick={onRefresh}>Refresh status</button>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      {status ? <dl className="archaeology-health-grid">
        <StatusFact label="Commons" value={status.service.version} detail={status.service.startedAt ? `Started ${status.service.startedAt.relative}` : "Process start time unavailable"} tone="healthy" />
        <StatusFact label="Database" value={`Schema ${status.database.schemaVersion}`} tone="healthy" />
        <StatusFact label="Codex runtime" value={status.codex.version} detail={status.codex.sessionRevocationPending ? "Browser session revocation pending · Codex browser sign-in is blocked" : `${status.codex.accountState.replaceAll("_", " ")} · ${status.codex.compatibilityStatus}${status.codex.compatibilityCheckedAt ? ` ${status.codex.compatibilityCheckedAt.relative}` : ""}`} tone={status.codex.sessionRevocationPending || status.codex.compatibilityStatus === "incompatible" ? "attention" : status.codex.compatibilityStatus === "compatible" ? "healthy" : "quiet"} />
        <StatusFact label="Historian work" value={`${status.archaeology.activeCount} active`} detail={`${status.archaeology.uncertainCount} uncertain`} tone={status.archaeology.uncertainCount ? "attention" : "healthy"} />
        <StatusFact label="Verified backup" value={status.backup.lastVerifiedAt?.relative || "Not yet verified"} tone={statusTone(status.backup.status)} />
        <StatusFact label="Reconciliation" value={status.reconciliation.status} detail={status.reconciliation.lastAt?.relative || "No recorded check"} tone={statusTone(status.reconciliation.status)} />
      </dl> : null}
      {evidence ? <section className="archaeology-evidence-dashboard" aria-labelledby="archaeology-evidence-title">
        <header><div><h3 id="archaeology-evidence-title">Daily-use evidence</h3><p>Facts accumulate from manual historian runs. {betaReady ? "The server reports every Beta prerequisite met; you still decide when to promote." : betaBlockers.length ? `Still needed: ${betaBlockers.join(" · ")}.` : "The server has not yet attested every Beta prerequisite."}</p></div><span className={betaReady ? "is-ready" : "is-building"}>{betaReady ? "Ready for your Beta review" : "Not yet ready for Beta review"}</span></header>
        <dl>
          <StatusFact label="Historian results" value={`${evidence.completedHistorians} completed`} detail={`${evidence.failedHistorians} failed · ${evidence.uncertainHistorians} uncertain`} tone={evidence.failedHistorians || evidence.uncertainHistorians ? "attention" : "healthy"} />
          <StatusFact label="Project coverage" value={`${evidence.distinctProjects} projects`} detail={`${evidence.reportsReceived} reports received`} />
          <StatusFact label="Completed without report" value={`${evidence.lostReports}`} detail={evidence.lostReports ? "Requires reconciliation before Beta review" : "Durable row count"} tone={evidence.lostReports ? "attention" : "quiet"} />
          <StatusFact label="Human review" value={`${evidence.reviewedImports} reviewed imports`} detail={`${evidence.cancellations} cancellations`} />
          <VerificationFact label="Repeated report reads" check={evidence.reportRecovery} />
          <VerificationFact label="Duplicate launch check" check={evidence.duplicateLaunchCheck} />
          <VerificationFact label="Repository immutability" check={evidence.repositoryImmutability} />
          <VerificationFact label="Canonical immutability" check={evidence.canonicalImmutability} />
          <StatusFact label="Restore drill" value={evidence.restoreDrill.status} detail={evidence.restoreDrill.lastVerifiedAt?.relative || "No verified drill"} tone={statusTone(evidence.restoreDrill.status)} />
        </dl>
      </section> : null}
    </details>
  );
}

function BatchList({ history, selectedBatchID, onSelect, onLoadMore }) {
  return (
    <section className="archaeology-history-list" aria-labelledby="archaeology-history-list-title">
      <div className="archaeology-section-heading"><div><h3 id="archaeology-history-list-title">Runs</h3><p>{history.items.length} retained run{history.items.length === 1 ? "" : "s"} loaded</p></div></div>
      {history.items.length ? <div className="archaeology-history-rows">{history.items.map((batch) => (
        <button key={batch.batchId} type="button" aria-pressed={selectedBatchID === batch.batchId} onClick={() => onSelect(batch.batchId)}>
          <span className={`archaeology-history-mark is-${batch.state}`} aria-hidden="true">{batch.state === "completed" ? <CheckCircle /> : <History />}</span>
          <span><strong>{batch.createdAt.absolute}</strong><small>{batch.selectedTotal} project{batch.selectedTotal === 1 ? "" : "s"} · {batch.depth} · {sourceLabels(batch.sources).join(" + ")}</small></span>
          <span><strong>{batch.state.replaceAll("_", " ")}</strong><small>{batch.completedCount} reports · {batch.attentionCount} attention</small></span>
        </button>
      ))}</div> : <div className="archaeology-empty"><strong>No historian runs yet</strong><span>Your first manual run will appear here and remain available after future runs.</span></div>}
      {history.nextCursor ? <button className="secondary-button archaeology-history-more" type="button" disabled={history.loading} onClick={onLoadMore}>{history.loading ? "Loading runs…" : "Load older runs"}</button> : null}
    </section>
  );
}

function BatchDetail({ batch, candidates, loading, error, onReview, onLoadMoreOutcomes }) {
  const [selected, setSelected] = useState([]);
  const outcomeIdentity = (batch?.review?.proposedOutcomes || []).map((outcome) => outcome.id).join("\n");
  useEffect(() => {
    setSelected((current) => reconcileOutcomeSelection(current, batch?.review?.proposedOutcomes || []));
  }, [batch?.batchId, outcomeIdentity]);
  if (loading) return <div className="archaeology-history-detail archaeology-empty" role="status"><strong>Opening run details…</strong><span>Loading bounded task and report facts.</span></div>;
  if (error) return <p className="archaeology-message archaeology-message--error" role="alert">{error}</p>;
  if (!batch) return <div className="archaeology-history-detail archaeology-empty"><strong>Choose a run</strong><span>Its exact tasks and retained report will open here.</span></div>;
  const outcomes = batch.review?.proposedOutcomes || [];
  const allSelected = outcomes.length > 0 && selected.length === outcomes.length;
  function toggle(id) { setSelected((current) => current.includes(id) ? current.filter((value) => value !== id) : [...current, id]); }
  return (
    <section className="archaeology-history-detail" aria-labelledby="archaeology-history-detail-title">
      <header><div><span>{batch.state.replaceAll("_", " ")}</span><h3 id="archaeology-history-detail-title">{batch.createdAt.absolute}</h3><p>{batch.selectedTotal} projects · {batch.depth} · Codex-managed execution</p></div><span>{batch.hasReport ? "Report retained" : "No report"}</span></header>
      <div className="archaeology-history-task-list">{batch.tasks.map((task) => {
        const presentation = archaeologyTaskPresentation(task);
        const projectName = task.projectName || candidates.find((candidate) => candidate.id === task.projectId)?.name || "Codex project";
        return <article key={task.jobId}><span className={`archaeology-run-mark archaeology-run--${presentation.tone}`} aria-hidden="true"><BookOpen /></span><div><strong>Project history · {projectName}</strong><span>{presentation.primary}</span><small>{presentation.secondary}</small></div><details><summary>Exact Codex IDs</summary><dl>{[["Project", task.projectId], ["Batch", task.batchId], ["Job", task.jobId], ["Thread", task.threadId], ["Turn", task.turnId]].filter(([, value]) => value).map(([label, value]) => <div key={label}><dt>{label}</dt><dd><code tabIndex="0">{value}</code></dd></div>)}</dl></details></article>;
      })}</div>
      {outcomes.length ? <section className="archaeology-history-report"><div><h3>Report proposals</h3><button type="button" onClick={() => setSelected(allSelected ? [] : outcomes.map((item) => item.id))}>{allSelected ? "Clear all" : "Select all"}</button></div><p>Choose the proposals to combine into one exact, all-or-nothing canonical diff.</p>{outcomes.map((outcome) => <label key={outcome.id}><input type="checkbox" checked={selected.includes(outcome.id)} onChange={() => toggle(outcome.id)} /><span><strong>{outcome.title}</strong><small>{outcome.summary} · {outcome.provenance?.length || 0} exact citation{outcome.provenance?.length === 1 ? "" : "s"} retained</small></span></label>)}{batch.outcomesNextCursor ? <button className="secondary-button archaeology-history-more" type="button" disabled={loading} onClick={onLoadMoreOutcomes}>{loading ? "Loading proposals…" : "Load 5 more proposals"}</button> : null}<button className="primary-button" type="button" disabled={!selected.length || batch.review?.canApply !== true} onClick={() => onReview(batch.batchId, selected)}>{batch.review?.canApply === true ? `Review exact diff · ${selected.length} selected` : "Canonical Apply is not enabled"}</button></section> : <div className="archaeology-empty"><strong>No review-ready report</strong><span>The exact task states above remain durable history.</span></div>}
    </section>
  );
}

export function ProjectArchaeologyHistory({ history, batch, status, candidates = [], error, onSelectBatch, onLoadMore, onLoadMoreOutcomes, onRefreshStatus, onReview, onBack }) {
  return (
    <div className="archaeology-history">
      <header className="archaeology-content-heading"><div><span>Project Archaeology</span><h2 id="archaeology-title" tabIndex="-1">Run history</h2><p id="archaeology-description">Every manual historian run stays inspectable. Exact Codex IDs remain secondary provenance; nothing is imported automatically.</p></div></header>
      <InstallationStatus status={status.value} loading={status.loading} error={status.error} onRefresh={onRefreshStatus} />
      <div className="archaeology-history-layout"><BatchList history={history} selectedBatchID={batch.value?.batchId || batch.selectedID} onSelect={onSelectBatch} onLoadMore={onLoadMore} /><BatchDetail batch={batch.value} candidates={candidates} loading={batch.loading} error={batch.error} onReview={onReview} onLoadMoreOutcomes={onLoadMoreOutcomes} /></div>
      {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
      <footer className="archaeology-footer"><button className="secondary-button" type="button" onClick={onBack}>Back to current run</button></footer>
    </div>
  );
}
