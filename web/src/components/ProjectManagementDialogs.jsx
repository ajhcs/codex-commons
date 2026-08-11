import { useEffect, useRef, useState } from "react";
import { createIdempotencyKey } from "./AuthControls.jsx";
import { commonsAdapter } from "../data/adapter.js";

const projectStatuses = ["active", "paused", "completed", "archived"];
const milestoneStatuses = ["planned", "active", "completed", "cancelled"];

export function ProjectEditorDialog({ open, project = null, session, onClose, onSaved, onAuthRequired, onConflict }) {
  const dialogRef = useRef(null);
  const initializedRef = useRef(false);
  const idempotencyRef = useRef("");
  const [draft, setDraft] = useState({ id: "", name: "", status: "active", purpose: "", now: "" });
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
      setDraft(project ? {
        id: project.id,
        name: project.name,
        status: project.status,
        purpose: project.purpose,
        now: project.now || "",
      } : { id: "", name: "", status: "active", purpose: "", now: "" });
      initializedRef.current = true;
    }
  }, [open, project]);

  function update(key, value) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    const id = (project?.id || draft.id).trim();
    if (!project && !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(id)) {
      setStatus({ submitting: false, message: "Use a lowercase project ID with words separated by hyphens.", kind: "error" });
      return;
    }
    if (!draft.name.trim() || !draft.purpose.trim() || !projectStatuses.includes(draft.status)) {
      setStatus({ submitting: false, message: "Name, purpose, and a valid status are required.", kind: "error" });
      return;
    }
    if (!idempotencyRef.current) idempotencyRef.current = createIdempotencyKey();
    setStatus({ submitting: true, message: "Saving project…", kind: "" });
    const input = {
      ...(!project ? { id } : {}),
      name: draft.name.trim(),
      status: draft.status,
      purpose: draft.purpose.trim(),
      ...(draft.now.trim() ? { now: draft.now.trim() } : {}),
      ...(project ? { base_revision: project.revision } : {}),
    };
    try {
      const result = project
        ? await commonsAdapter.updateProject(project.id, input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current })
        : await commonsAdapter.createProject(input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current });
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "Project saved.", kind: "success" });
      onSaved(result);
    } catch (error) {
      if (error.status === 409 || error.code === "conflict") {
        idempotencyRef.current = "";
        setStatus({
          submitting: false,
          message: project
            ? "This project changed while you were editing. Commons reloaded the canonical record; your draft is preserved. Review it and save again."
            : "A project with this ID already exists. Your draft is preserved.",
          kind: "error",
        });
        if (project) onConflict?.();
        return;
      }
      if (error.status === 401 || error.code === "csrf_failed") onAuthRequired();
      setStatus({ submitting: false, message: error.message, kind: "error" });
    }
  }

  return (
    <dialog ref={dialogRef} className="management-editor" aria-labelledby="project-editor-title" onClose={onClose}>
      <form onSubmit={submit}>
        <header><div><h2 id="project-editor-title">{project ? "Edit project" : "New project"}</h2><p>Keep the durable purpose and current focus concise.</p></div><button type="button" onClick={onClose}>Close</button></header>
        <div className="management-fields">
          {!project ? <label>Project ID<input value={draft.id} onChange={(event) => update("id", event.target.value)} autoComplete="off" placeholder="billing-orchestrator" /></label> : null}
          <label>Name<input value={draft.name} onChange={(event) => update("name", event.target.value)} autoComplete="off" /></label>
          <label>Status<select value={draft.status} onChange={(event) => update("status", event.target.value)}>{projectStatuses.map((item) => <option key={item} value={item}>{item[0].toUpperCase() + item.slice(1)}</option>)}</select></label>
          <label>Purpose<textarea rows="3" value={draft.purpose} onChange={(event) => update("purpose", event.target.value)} /></label>
          <label>Current focus <span>Optional</span><textarea rows="2" value={draft.now} onChange={(event) => update("now", event.target.value)} /></label>
        </div>
        {status.message ? <p className={`form-message${status.kind ? ` form-message--${status.kind}` : ""}`} role="status">{status.message}</p> : null}
        <footer><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary-button" type="submit" disabled={status.submitting}>{status.submitting ? "Saving…" : "Save project"}</button></footer>
      </form>
    </dialog>
  );
}

export function MilestoneEditorDialog({ open, projectID, milestone = null, nextPosition = 0, session, onClose, onSaved, onAuthRequired, onConflict }) {
  const dialogRef = useRef(null);
  const initializedRef = useRef(false);
  const idempotencyRef = useRef("");
  const [draft, setDraft] = useState({ title: "", status: "planned", position: 0, targetDate: "" });
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
      setDraft(milestone ? {
        title: milestone.title,
        status: milestone.status,
        position: milestone.position,
        targetDate: milestone.targetDate || "",
      } : { title: "", status: "planned", position: nextPosition, targetDate: "" });
      initializedRef.current = true;
    }
  }, [open, milestone, nextPosition]);

  function update(key, value) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    const position = Number(draft.position);
    if (!draft.title.trim() || !milestoneStatuses.includes(draft.status) || !Number.isInteger(position) || position < 0) {
      setStatus({ submitting: false, message: "Title, a valid status, and a non-negative integer position are required.", kind: "error" });
      return;
    }
    if (!idempotencyRef.current) idempotencyRef.current = createIdempotencyKey();
    setStatus({ submitting: true, message: "Saving milestone…", kind: "" });
    const input = {
      title: draft.title.trim(),
      status: draft.status,
      position,
      ...(draft.targetDate ? { target_date: draft.targetDate } : {}),
      ...(milestone ? { base_revision: milestone.revision } : {}),
    };
    try {
      const result = milestone
        ? await commonsAdapter.updateMilestone(milestone.id, input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current })
        : await commonsAdapter.createMilestone(projectID, input, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current });
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "Milestone saved.", kind: "success" });
      onSaved(result);
    } catch (error) {
      if (error.status === 409 || error.code === "conflict") {
        idempotencyRef.current = "";
        if (milestone) {
          try {
            await onConflict?.();
            setStatus({ submitting: false, message: "This milestone changed while you were editing. Commons reloaded the canonical record; your draft is preserved. Review it and save again.", kind: "error" });
          } catch {
            setStatus({ submitting: false, message: "This milestone changed while you were editing. Your draft is preserved, but Commons could not reload the canonical record. Close and reopen the editor before saving again.", kind: "error" });
          }
        } else {
          setStatus({ submitting: false, message: "This milestone conflicts with current project state. Your draft is preserved; check whether another milestone is already active.", kind: "error" });
        }
        return;
      }
      if (error.status === 401 || error.code === "csrf_failed") onAuthRequired();
      setStatus({ submitting: false, message: error.message, kind: "error" });
    }
  }

  return (
    <dialog ref={dialogRef} className="management-editor" aria-labelledby="milestone-editor-title" onClose={onClose}>
      <form onSubmit={submit}>
        <header><div><h2 id="milestone-editor-title">{milestone ? "Edit milestone" : "New milestone"}</h2><p>Milestones order the simple roadmap. Only one can be active.</p></div><button type="button" onClick={onClose}>Close</button></header>
        <div className="management-fields management-fields--split">
          <label className="field-span">Title<input value={draft.title} onChange={(event) => update("title", event.target.value)} autoComplete="off" /></label>
          <label>Status<select value={draft.status} onChange={(event) => update("status", event.target.value)}>{milestoneStatuses.map((item) => <option key={item} value={item}>{item[0].toUpperCase() + item.slice(1)}</option>)}</select></label>
          <label>Position<input type="number" min="0" step="1" value={draft.position} onChange={(event) => update("position", event.target.value)} /></label>
          <label className="field-span">Target date <span>Optional</span><input type="date" value={draft.targetDate} onChange={(event) => update("targetDate", event.target.value)} /></label>
        </div>
        {status.message ? <p className={`form-message${status.kind ? ` form-message--${status.kind}` : ""}`} role="status">{status.message}</p> : null}
        <footer><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary-button" type="submit" disabled={status.submitting}>{status.submitting ? "Saving…" : "Save milestone"}</button></footer>
      </form>
    </dialog>
  );
}
