import { useEffect, useRef, useState } from "react";
import { createIdempotencyKey } from "./AuthControls.jsx";
import { commonsAdapter } from "../data/adapter.js";

const emptyDraft = { slug: "", title: "", summary: "", body: "" };

export function WikiRevisionDialog({ open, projectID, page = null, session, onClose, onSaved, onAuthRequired, onConflict, onOpenConflict }) {
  const dialogRef = useRef(null);
  const initializedRef = useRef(false);
  const idempotencyRef = useRef("");
  const [draft, setDraft] = useState(emptyDraft);
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
      setDraft(page ? { slug: page.slug, title: page.title, summary: page.summary, body: page.body } : emptyDraft);
      initializedRef.current = true;
    }
  }, [open, page]);

  function update(key, value) {
    setDraft((current) => ({ ...current, [key]: value }));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    const slug = (page?.slug || draft.slug).trim();
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(slug)) {
      setStatus({ submitting: false, message: "Use a lowercase wiki slug with words separated by hyphens.", kind: "error" });
      return;
    }
    if (!draft.title.trim() || !draft.summary.trim() || !draft.body.trim()) {
      setStatus({ submitting: false, message: "Title, revision summary, and body are required.", kind: "error" });
      return;
    }
    if (!idempotencyRef.current) idempotencyRef.current = createIdempotencyKey();
    setStatus({ submitting: true, message: "Saving durable revision…", kind: "" });
    try {
      const result = await commonsAdapter.createWikiRevision(projectID, slug, {
        title: draft.title.trim(),
        summary: draft.summary.trim(),
        body: draft.body.trim(),
        ...(page ? { base_revision: page.revision } : {}),
      }, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current });
      idempotencyRef.current = "";
      setStatus({ submitting: false, message: "Revision saved.", kind: "success" });
      onSaved({ ...result, slug });
    } catch (error) {
      if (error.status === 409 || error.code === "conflict") {
        idempotencyRef.current = "";
        if (page) {
          setStatus({ submitting: false, message: "This page changed while you were writing. Commons reloaded the canonical revision; your draft is preserved. Review it and save again.", kind: "error", conflictSlug: "" });
          onConflict?.();
        } else {
          setStatus({ submitting: false, message: "A page with this slug already exists. Your draft is preserved.", kind: "error", conflictSlug: slug });
        }
        return;
      }
      if (error.status === 401 || error.code === "csrf_failed") onAuthRequired();
      setStatus({ submitting: false, message: error.message, kind: "error" });
    }
  }

  return (
    <dialog ref={dialogRef} className="wiki-editor" aria-labelledby="wiki-editor-title" onClose={onClose}>
      <form onSubmit={submit}>
        <header>
          <div><h2 id="wiki-editor-title">{page ? "New wiki revision" : "New wiki page"}</h2><p>Every save appends durable history. Existing revisions are never overwritten.</p></div>
          <button type="button" onClick={onClose}>Close</button>
        </header>
        <div className="wiki-editor-fields">
          {!page ? <label>Slug<input value={draft.slug} onChange={(event) => update("slug", event.target.value)} autoComplete="off" placeholder="retry-policy" /></label> : null}
          <label>Title<input value={draft.title} onChange={(event) => update("title", event.target.value)} autoComplete="off" /></label>
          <label>Revision summary<input value={draft.summary} onChange={(event) => update("summary", event.target.value)} autoComplete="off" placeholder="What changed and why" /></label>
          <label>Body<textarea rows="16" value={draft.body} onChange={(event) => update("body", event.target.value)} /></label>
        </div>
        {status.message ? <p className={`form-message${status.kind ? ` form-message--${status.kind}` : ""}`} role="status">{status.message}</p> : null}
        {status.conflictSlug ? <button className="conflict-open-button" type="button" onClick={() => onOpenConflict?.(status.conflictSlug)}>Open existing page</button> : null}
        <footer>
          <button className="secondary-button" type="button" onClick={onClose}>Cancel</button>
          <button className="primary-button" type="submit" disabled={status.submitting}>{status.submitting ? "Saving…" : "Save revision"}</button>
        </footer>
      </form>
    </dialog>
  );
}
