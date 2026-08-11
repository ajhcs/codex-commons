import { useEffect, useRef, useState } from "react";
import { createIdempotencyKey } from "./AuthControls.jsx";
import { taskStateLabels } from "./ProjectParts.jsx";
import { commonsAdapter } from "../data/adapter.js";

const createStates = ["ready", "blocked"];
const allStates = ["ready", "in_progress", "blocked", "done", "cancelled"];

function dependencyIDs(value) {
  return [...new Set(value.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean))];
}

export function TaskEditorDialog({ open, projectID, task = null, milestones = [], session, onClose, onSaved, onAuthRequired }) {
  const dialogRef = useRef(null);
  const initializedRef = useRef(false);
  const idempotencyRef = useRef("");
  const [baseTask, setBaseTask] = useState(task);
  const [draft, setDraft] = useState({ title: "", description: "", acceptance: "", state: "ready", priority: 0, milestoneID: "", dependencies: "" });
  const [status, setStatus] = useState({ submitting: false, message: "", kind: "" });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (open && dialog && !dialog.open) dialog.showModal();
    if (!open && dialog?.open) dialog.close();
    if (!open) {
      initializedRef.current = false;
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "", kind: "" });
      return;
    }
    if (!initializedRef.current) {
      setBaseTask(task);
      setDraft(task ? {
        title: task.title,
        description: task.description || "",
        acceptance: task.acceptance || "",
        state: task.state,
        priority: task.priority,
        milestoneID: task.milestoneID || "",
        dependencies: task.dependencies.map((dependency) => dependency.id).join("\n"),
      } : { title: "", description: "", acceptance: "", state: "ready", priority: 0, milestoneID: "", dependencies: "" });
      initializedRef.current = true;
    }
  }, [open, task]);

  function update(key, value) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    const priority = Number(draft.priority);
    const dependencies = dependencyIDs(draft.dependencies);
    if (!draft.title.trim() || !Number.isInteger(priority) || priority < 0) {
      setStatus({ submitting: false, message: "Title and a non-negative integer priority are required.", kind: "error" });
      return;
    }
    if (dependencies.length > 20) {
      setStatus({ submitting: false, message: "A task can reference at most 20 dependency IDs.", kind: "error" });
      return;
    }
    if (baseTask && dependencies.includes(baseTask.id)) {
      setStatus({ submitting: false, message: "A task cannot depend on itself.", kind: "error" });
      return;
    }
    if (!idempotencyRef.current) idempotencyRef.current = createIdempotencyKey();
    setStatus({ submitting: true, message: "Saving task…", kind: "" });
    const input = baseTask ? {
      title: draft.title.trim(),
      description: draft.description.trim(),
      acceptance: draft.acceptance.trim(),
      priority,
      milestone_id: draft.milestoneID,
      dependency_ids: dependencies,
      base_revision: baseTask.revision,
    } : {
      title: draft.title.trim(),
      ...(draft.description.trim() ? { description: draft.description.trim() } : {}),
      ...(draft.acceptance.trim() ? { acceptance: draft.acceptance.trim() } : {}),
      state: draft.state,
      priority,
      ...(draft.milestoneID ? { milestone_id: draft.milestoneID } : {}),
      ...(dependencies.length ? { dependency_ids: dependencies } : {}),
    };
    try {
      const result = baseTask
        ? await commonsAdapter.updateTask(baseTask.id, input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current })
        : await commonsAdapter.createTask(projectID, input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current });
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "Task saved.", kind: "success" });
      onSaved(result);
    } catch (error) {
      if (error.status === 409 || error.code === "conflict") {
        idempotencyRef.current = "";
        if (baseTask) {
          try {
            const canonical = await commonsAdapter.readTask(baseTask.id, 1);
            setBaseTask(canonical.task);
            setStatus({ submitting: false, message: "This task changed while you were editing. Commons reloaded the canonical revision; your draft is preserved. Review it and save again.", kind: "error" });
          } catch {
            setStatus({ submitting: false, message: "This task changed while you were editing. Your draft is preserved, but Commons could not reload it. Close and reopen before saving again.", kind: "error" });
          }
        } else {
          setStatus({ submitting: false, message: "This task conflicts with current project state. Your draft is preserved.", kind: "error" });
        }
        return;
      }
      if (error.status === 401 || error.code === "csrf_failed") onAuthRequired();
      setStatus({ submitting: false, message: error.message, kind: "error" });
    }
  }

  return (
    <dialog ref={dialogRef} className="management-editor task-editor" aria-labelledby="task-editor-title" onClose={onClose}>
      <form onSubmit={submit}>
        <header><div><h2 id="task-editor-title">{baseTask ? "Edit task" : "New task"}</h2><p>Canonical work only. Agent ownership and chat stay outside this human surface.</p></div><button type="button" onClick={onClose}>Close</button></header>
        <div className="management-fields management-fields--split">
          <label className="field-span">Title<input value={draft.title} onChange={(event) => update("title", event.target.value)} autoComplete="off" /></label>
          {!baseTask ? <label>Initial state<select value={draft.state} onChange={(event) => update("state", event.target.value)}>{createStates.map((item) => <option key={item} value={item}>{taskStateLabels[item]}</option>)}</select></label> : null}
          <label>Priority<input type="number" min="0" step="1" value={draft.priority} onChange={(event) => update("priority", event.target.value)} /></label>
          <label className="field-span">Milestone <span>Optional</span><select value={draft.milestoneID} onChange={(event) => update("milestoneID", event.target.value)}><option value="">Unscheduled</option>{milestones.map((milestone) => <option key={milestone.id} value={milestone.id}>{milestone.title} · {milestone.status}</option>)}</select></label>
          <label className="field-span">Description <span>Optional</span><textarea rows="3" value={draft.description} onChange={(event) => update("description", event.target.value)} /></label>
          <label className="field-span">Acceptance <span>Optional</span><textarea rows="3" value={draft.acceptance} onChange={(event) => update("acceptance", event.target.value)} /></label>
          <label className="field-span">Dependency IDs <span>Optional · comma, space, or line separated · maximum 20</span><textarea rows="3" value={draft.dependencies} onChange={(event) => update("dependencies", event.target.value)} placeholder="TASK-123\nTASK-124" /></label>
        </div>
        {status.message ? <p className={`form-message${status.kind ? ` form-message--${status.kind}` : ""}`} role="status">{status.message}</p> : null}
        <footer><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary-button" type="submit" disabled={status.submitting}>{status.submitting ? "Saving…" : "Save task"}</button></footer>
      </form>
    </dialog>
  );
}

export function TaskStateDialog({ open, task, session, onClose, onSaved, onAuthRequired }) {
  const dialogRef = useRef(null);
  const initializedRef = useRef(false);
  const idempotencyRef = useRef("");
  const [baseTask, setBaseTask] = useState(task);
  const [draft, setDraft] = useState({ state: task?.state || "ready", basis: "" });
  const [status, setStatus] = useState({ submitting: false, message: "", kind: "" });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (open && dialog && !dialog.open) dialog.showModal();
    if (!open && dialog?.open) dialog.close();
    if (!open) {
      initializedRef.current = false;
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "", kind: "" });
      return;
    }
    if (!initializedRef.current) {
      setBaseTask(task);
      setDraft({ state: task.state, basis: "" });
      initializedRef.current = true;
    }
  }, [open, task]);

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    if (!draft.basis.trim()) {
      setStatus({ submitting: false, message: "Record a brief basis for the durable state change.", kind: "error" });
      return;
    }
    if (!idempotencyRef.current) idempotencyRef.current = createIdempotencyKey();
    setStatus({ submitting: true, message: "Changing task state…", kind: "" });
    try {
      const result = await commonsAdapter.changeTaskState(baseTask.id, {
        state: draft.state,
        basis: draft.basis.trim(),
        base_revision: baseTask.revision,
      }, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current });
      idempotencyRef.current = "";
      onSaved(result);
    } catch (error) {
      if (error.status === 409 || error.code === "conflict") {
        idempotencyRef.current = "";
        try {
          const canonical = await commonsAdapter.readTask(baseTask.id, 1);
          setBaseTask(canonical.task);
          setStatus({ submitting: false, message: "This task changed before your state update. Commons reloaded the canonical revision; your basis is preserved. Review and submit again.", kind: "error" });
        } catch {
          setStatus({ submitting: false, message: "This task changed before your state update. Your basis is preserved, but Commons could not reload it. Close and reopen before trying again.", kind: "error" });
        }
        return;
      }
      if (error.status === 401 || error.code === "csrf_failed") onAuthRequired();
      setStatus({ submitting: false, message: error.message, kind: "error" });
    }
  }

  return (
    <dialog ref={dialogRef} className="management-editor" aria-labelledby="task-state-title" onClose={onClose}>
      <form onSubmit={submit}>
        <header><div><h2 id="task-state-title">Change task state</h2><p>{baseTask?.title}</p></div><button type="button" onClick={onClose}>Close</button></header>
        <div className="management-fields">
          <label>State<select value={draft.state} onChange={(event) => setDraft((current) => ({ ...current, state: event.target.value }))}>{allStates.map((item) => <option key={item} value={item}>{taskStateLabels[item]}</option>)}</select></label>
          <label>Basis<textarea rows="4" value={draft.basis} onChange={(event) => setDraft((current) => ({ ...current, basis: event.target.value }))} placeholder="What evidence or decision supports this state?" /></label>
        </div>
        {status.message ? <p className={`form-message${status.kind ? ` form-message--${status.kind}` : ""}`} role="status">{status.message}</p> : null}
        <footer><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary-button" type="submit" disabled={status.submitting}>{status.submitting ? "Saving…" : "Save state"}</button></footer>
      </form>
    </dialog>
  );
}
