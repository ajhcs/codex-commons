import { useEffect, useRef } from "react";
import CommonsCompanion from "../../components/CommonsCompanion.jsx";

export function ProjectHistoryOffer({ open, identity, onContinue, onSkip }) {
  const dialogRef = useRef(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  function skip() {
    if (dialogRef.current?.open) dialogRef.current.close();
    else onSkip?.();
  }

  return (
    <dialog
      ref={dialogRef}
      className="history-offer-dialog"
      aria-labelledby="history-offer-title"
      aria-describedby="history-offer-description"
      onClose={onSkip}
      onCancel={(event) => { event.preventDefault(); skip(); }}
    >
      <div className="history-offer-visual" aria-hidden="true">
        <CommonsCompanion state="history-offered" size="hero" />
      </div>
      <div className="history-offer-copy">
        <span className="history-offer-status">Identity connected</span>
        <h2 id="history-offer-title">Bring in project history?</h2>
        <p id="history-offer-description">
          Welcome, {identity?.displayName || "Commons member"}. Commons can look for project metadata and let you choose what to explore. Nothing is imported automatically.
        </p>
        <div className="history-offer-actions">
          <button type="button" className="secondary-button" onClick={skip}>Not now</button>
          <button type="button" className="primary-button" onClick={onContinue}>Choose projects</button>
        </div>
        <small>You can reopen this from your account menu.</small>
      </div>
    </dialog>
  );
}

export default ProjectHistoryOffer;
