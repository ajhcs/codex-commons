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

export function HistoricalImportPreviewDialog({ open, bridge, busy = false, error = "", applied = null, onConfirm, onClose, onOpenProject }) {
  const dialogRef = useRef(null);
  const inputRef = useRef(null);
  const [confirmation, setConfirmation] = useState("");
  const preview = bridge?.preview;
  const exact = preview?.sourceDigest || "";

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    setConfirmation("");
    queueMicrotask(() => inputRef.current?.focus());
  }, [bridge?.preview?.manifestDigest, open]);

  function close() { onClose?.(); }

  return (
    <dialog ref={dialogRef} className="archaeology-dialog historical-import-dialog" aria-labelledby="historical-import-title" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      {applied ? (
        <div className="historical-import-success">
          <span className="archaeology-state-mark" aria-hidden="true"><CheckCircle /></span>
          <h2 id="historical-import-title">Project history is now current</h2>
          <p>Commons applied the reviewed history. Existing newer records won every collision.</p>
          <dl className="historical-import-counts"><div><dt>Created</dt><dd>{applied.counts.created}</dd></div><div><dt>Kept current</dt><dd>{applied.counts.skippedCurrent}</dd></div><div><dt>Replayed safely</dt><dd>{applied.counts.replayed}</dd></div></dl>
          <footer className="archaeology-footer"><button className="secondary-button" type="button" onClick={close}>Close</button><button className="primary-button" type="button" onClick={() => onOpenProject?.(bridge.projectId)}>Open project</button></footer>
        </div>
      ) : (
        <div className="historical-import-preview">
          <header className="archaeology-content-heading"><div><span>Human approval</span><h2 id="historical-import-title">Review the canonical import</h2><p>Nothing has been applied. Confirm this exact source digest only after reviewing the bounded counts and current-wins behavior.</p></div></header>
          <section className="historical-import-summary" aria-labelledby="historical-import-summary-title">
            <div><h3 id="historical-import-summary-title">{preview?.batchId || "Historical import"}</h3><span>{bridge?.projectId || "Project"}</span></div>
            <dl className="historical-import-counts">{countRows(preview?.counts).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
          </section>
          <div className="archaeology-truth-note"><strong>Current Commons records win</strong><span>Reviewed source keys are deduplicated. A historical record cannot overwrite newer canonical work; safe replays do not duplicate it.</span></div>
          <label className="historical-import-confirmation"><span>Exact source digest</span><code>{exact}</code><input ref={inputRef} value={confirmation} onChange={(event) => setConfirmation(event.target.value.trim())} autoComplete="off" spellCheck="false" aria-describedby="historical-import-confirmation-help" /><small id="historical-import-confirmation-help">Enter the complete digest shown above. Partial values are never accepted.</small></label>
          {error ? <p className="archaeology-message archaeology-message--error" role="alert">{error}</p> : null}
          <footer className="archaeology-footer"><button className="secondary-button" type="button" disabled={busy} onClick={close}>Review later</button><button className="primary-button" type="button" disabled={busy || !exact || confirmation !== exact} onClick={() => onConfirm(confirmation)}>{busy ? "Applying reviewed history…" : "Apply reviewed history"}</button></footer>
        </div>
      )}
    </dialog>
  );
}
