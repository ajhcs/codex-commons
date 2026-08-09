import { useEffect, useRef, useState } from "react";
import Clipboard from "../icons/Clipboard.tsx";

export function TaskPreviewDialog({ task, onClose }) {
  const ref = useRef(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    const dialog = ref.current;
    if (task && dialog && !dialog.open) dialog.showModal();
    if (!task && dialog?.open) dialog.close();
  }, [task]);

  useEffect(() => {
    if (!task) setCopied(false);
  }, [task]);

  if (!task) return <dialog ref={ref} />;

  async function copyReference() {
    await navigator.clipboard.writeText(task.ref);
    setCopied(true);
  }

  return (
    <dialog ref={ref} className="task-dialog" aria-labelledby="task-preview-title" onClose={onClose}>
      <div className="dialog-heading">
        <div><span className="dialog-kicker">Task preview</span><h2 id="task-preview-title">{task.title}</h2></div>
        <button type="button" onClick={onClose}>Close</button>
      </div>
      <dl className="task-fact-grid">
        <div><dt>Reference</dt><dd>{task.ref}</dd></div>
        <div><dt>Project</dt><dd>{task.project || "—"}</dd></div>
        <div><dt>State</dt><dd className="text-capitalize">{task.state?.replaceAll("_", " ") || "Needs attention"}</dd></div>
        <div><dt>Owner</dt><dd>{task.owner || "Unassigned"}</dd></div>
        {task.priority != null ? <div><dt>Priority</dt><dd>{task.priority}</dd></div> : null}
        {task.updated ? <div><dt>Updated</dt><dd>{task.updated.absolute} UTC</dd></div> : null}
      </dl>
      {task.nextAction ? <div className="task-next-action"><span>Next action</span><strong>{task.nextAction}</strong></div> : null}
      <div className="dialog-note">This preview uses the task reference already returned by Commons. The full task workspace will replace it when the Tasks slice is connected.</div>
      <div className="dialog-actions">
        <button className="secondary-button" type="button" onClick={onClose}>Done</button>
        <button className="primary-button" type="button" onClick={copyReference}><Clipboard aria-hidden="true" />{copied ? "Copied" : "Copy task reference"}</button>
      </div>
    </dialog>
  );
}
