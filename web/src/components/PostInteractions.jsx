import { useEffect, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import DotsHorizontal from "../icons/DotsHorizontal.tsx";
import { createIdempotencyKey, isExpiredSession } from "./AuthControls.jsx";

const intentOptions = [
  { value: "answer", label: "Answer" },
  { value: "add_evidence", label: "Add evidence" },
  { value: "challenge", label: "Challenge" },
  { value: "clarify", label: "Clarify" },
];

export const commentIntentLabels = Object.fromEntries(intentOptions.map(({ value, label }) => [value, label]));

export function CommentComposer({ postID, session, onAuthRequired, onSuccess }) {
  const textareaRef = useRef(null);
  const controllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const [intent, setIntent] = useState("");
  const [body, setBody] = useState("");
  const [status, setStatus] = useState({ state: "idle", message: "" });

  useEffect(() => () => controllerRef.current?.abort(), []);

  function changed(setter, value) {
    setter(value);
    idempotencyRef.current = "";
    setStatus({ state: "idle", message: "" });
  }

  function requireAuth() {
    onAuthRequired(() => queueMicrotask(() => textareaRef.current?.focus()));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      requireAuth();
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey();
      setStatus({ state: "loading", message: "Adding comment…" });
      await commonsAdapter.createComment({
        ref: postID,
        body: body.trim(),
        intent,
      }, {
        csrfToken: session.csrfToken,
        idempotencyKey: idempotencyRef.current,
      }, controller.signal);
      setBody("");
      idempotencyRef.current = "";
      setStatus({ state: "success", message: "Comment added." });
      onSuccess();
    } catch (error) {
      if (error.name === "AbortError") return;
      if (isExpiredSession(error)) {
        setStatus({ state: "error", message: "Sign in again, then submit this saved comment." });
        requireAuth();
        return;
      }
      setStatus({ state: "error", message: error.message });
    }
  }

  const busy = status.state === "loading";
  return (
    <form className="comment-composer" onSubmit={submit}>
      <fieldset disabled={busy}>
        <legend>Reply intent</legend>
        {intentOptions.map((option) => (
          <button key={option.value} type="button" className={intent === option.value ? "is-selected" : ""} aria-pressed={intent === option.value} onClick={() => changed(setIntent, option.value)}>
            {option.label}
          </button>
        ))}
      </fieldset>
      <label className="comment-body-field">
        <span className="sr-only">Comment</span>
        <textarea
          name="comment"
          ref={textareaRef}
          required
          maxLength={8000}
          rows={3}
          value={body}
          placeholder={session?.authenticated ? "Add to this thread…" : "Sign in to add to this thread…"}
          onChange={(event) => changed(setBody, event.target.value)}
          onKeyDown={(event) => {
            if ((event.metaKey || event.ctrlKey) && event.key === "Enter") event.currentTarget.form?.requestSubmit();
          }}
          disabled={busy}
        />
      </label>
      <div className="comment-composer-footer">
        <span>Ctrl/⌘ Enter to submit</span>
        <button type="submit" className="comment-submit" disabled={busy || !intent || !body.trim()}>{busy ? "Adding…" : session?.authenticated ? "Comment" : "Sign in to comment"}</button>
      </div>
      {status.message ? <p className={`form-message form-message--${status.state}`} role="status" aria-live="polite">{status.message}</p> : null}
    </form>
  );
}

function PostStateDialog({ state, postID, candidates, session, onClose, onAuthRequired, onSuccess }) {
  const dialogRef = useRef(null);
  const replacementRef = useRef(null);
  const controllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const [replacement, setReplacement] = useState("");
  const [status, setStatus] = useState({ state: "idle", message: "" });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (state && !dialog.open) {
      dialog.showModal();
      if (state === "superseded") queueMicrotask(() => replacementRef.current?.focus());
    }
    if (!state && dialog.open) dialog.close();
  }, [state]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  function closeForAuth() {
    const resumeState = state;
    onClose();
    onAuthRequired(() => onSuccess("resume", resumeState));
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      closeForAuth();
      return;
    }
    if (state === "superseded" && replacement.trim() === postID) {
      setStatus({ state: "error", message: "Choose a different existing post." });
      replacementRef.current?.focus();
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey();
      setStatus({ state: "loading", message: "Saving state…" });
      await commonsAdapter.changePostState({
        ref: postID,
        state,
        ...(state === "superseded" ? { superseded_by: replacement.trim() } : {}),
      }, {
        csrfToken: session.csrfToken,
        idempotencyKey: idempotencyRef.current,
      }, controller.signal);
      setReplacement("");
      idempotencyRef.current = "";
      setStatus({ state: "idle", message: "" });
      onSuccess(state);
    } catch (error) {
      if (error.name === "AbortError") return;
      if (isExpiredSession(error)) {
        setStatus({ state: "error", message: "Sign in again to finish this change." });
        closeForAuth();
        return;
      }
      setStatus({ state: "error", message: error.message });
    }
  }

  const superseding = state === "superseded";
  const busy = status.state === "loading";
  return (
    <dialog ref={dialogRef} className="state-dialog" onClose={onClose} onCancel={(event) => { if (busy) event.preventDefault(); else onClose(); }}>
      <form onSubmit={submit}>
        <header>
          <h2>{superseding ? "Supersede this post" : "Resolve this post"}</h2>
          <p>{superseding ? "Point readers to a newer canonical post." : "Mark the thread complete without changing its contents."}</p>
        </header>
        {superseding ? (
          <label>
            <span>Replacement post ID</span>
            <input ref={replacementRef} name="replacement-post" list="replacement-posts" required maxLength={200} value={replacement} onChange={(event) => { setReplacement(event.target.value); idempotencyRef.current = ""; setStatus({ state: "idle", message: "" }); }} placeholder="Choose a post or enter its ID" disabled={busy} />
            <datalist id="replacement-posts">
              {candidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.title}</option>)}
            </datalist>
            <small>Showing posts from the current feed. You can enter another existing post ID.</small>
          </label>
        ) : null}
        <p className="immutability-note">This appends a state change. The original post and its history remain intact.</p>
        {status.message ? <p className="form-message form-message--error" role="alert">{status.message}</p> : null}
        <footer>
          <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Cancel</button>
          <button type="submit" className="primary-button" disabled={busy}>{busy ? "Saving…" : superseding ? "Supersede" : "Resolve"}</button>
        </footer>
      </form>
    </dialog>
  );
}

export function PostStateMenu({ post, candidates = [], session, onAuthRequired, onSuccess }) {
  const menuRef = useRef(null);
  const [dialogState, setDialogState] = useState("");

  if (post.state !== "open") return null;

  function begin(state) {
    menuRef.current?.removeAttribute("open");
    if (!session?.authenticated) {
      onAuthRequired(() => setDialogState(state));
      return;
    }
    setDialogState(state);
  }

  function completed(state, resumeState) {
    if (state === "resume") {
      setDialogState(resumeState);
      return;
    }
    setDialogState("");
    onSuccess(state === "resolved" ? "Post resolved." : "Post superseded.");
  }

  return (
    <>
      <details ref={menuRef} className="post-state-menu">
        <summary aria-label="More post actions"><DotsHorizontal aria-hidden="true" /></summary>
        <div>
          <button type="button" onClick={() => begin("resolved")}>Resolve</button>
          <button type="button" onClick={() => begin("superseded")}>Supersede…</button>
        </div>
      </details>
      <PostStateDialog
        state={dialogState}
        postID={post.id}
        session={session}
        candidates={candidates}
        onClose={() => setDialogState("")}
        onAuthRequired={onAuthRequired}
        onSuccess={completed}
      />
    </>
  );
}
