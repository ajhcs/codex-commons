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
  const lookupControllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const [intent, setIntent] = useState("");
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState([]);
  const [lookup, setLookup] = useState({ query: null, items: [], active: 0, loading: false });
  const [status, setStatus] = useState({ state: "idle", message: "" });

  useEffect(() => () => { controllerRef.current?.abort(); lookupControllerRef.current?.abort(); }, []);
  useEffect(() => {
    if (!lookup.query) return undefined;
    const controller = new AbortController();
    lookupControllerRef.current?.abort(); lookupControllerRef.current = controller;
    const timer = globalThis.setTimeout(async () => {
      try {
        const result = await commonsAdapter.readContributors({ q: lookup.query.term, project: "", cursor: "", limit: 8 }, controller.signal);
        setLookup((current) => current.query?.term === lookup.query.term ? { ...current, items: result.items, active: 0, loading: false } : current);
      } catch (error) {
        if (error.name !== "AbortError") setLookup((current) => ({ ...current, items: [], loading: false }));
      }
    }, 140);
    return () => { globalThis.clearTimeout(timer); controller.abort(); };
  }, [lookup.query?.term]);

  function changed(setter, value) { setter(value); idempotencyRef.current = ""; setStatus({ state: "idle", message: "" }); }
  function requireAuth() { onAuthRequired(() => queueMicrotask(() => textareaRef.current?.focus())); }
  function updateBody(value, cursor) {
    changed(setBody, value);
    const before = value.slice(0, cursor);
    const match = before.match(/(?:^|\s)@([a-z0-9-]{0,64})$/i);
    setLookup(match && mentions.length < 5 ? { query: { term: match[1], start: cursor - match[1].length - 1, end: cursor }, items: [], active: 0, loading: true } : { query: null, items: [], active: 0, loading: false });
  }
  function selectContributor(item) {
    if (!lookup.query || !item.addressable || mentions.some((mention) => mention.session === item.session)) return;
    const replacement = `@${item.handle} `;
    const next = body.slice(0, lookup.query.start) + replacement + body.slice(lookup.query.end);
    const cursor = lookup.query.start + replacement.length;
    changed(setBody, next); setMentions((current) => [...current, item]); setLookup({ query: null, items: [], active: 0, loading: false });
    queueMicrotask(() => { textareaRef.current?.focus(); textareaRef.current?.setSelectionRange(cursor, cursor); });
  }

  async function submit(event) {
    event.preventDefault();
    if (!session?.authenticated) { requireAuth(); return; }
    const controller = new AbortController(); controllerRef.current?.abort(); controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey(); setStatus({ state: "loading", message: "Adding comment…" });
      await commonsAdapter.createComment({ ref: postID, body: body.trim(), intent, mentions: mentions.map(({ session: target }) => ({ session: target })) }, { csrfToken: session.csrfToken, idempotencyKey: idempotencyRef.current }, controller.signal);
      setBody(""); setMentions([]); idempotencyRef.current = ""; setStatus({ state: "success", message: "Comment added." }); onSuccess();
    } catch (error) {
      if (error.name === "AbortError") return;
      if (isExpiredSession(error)) { setStatus({ state: "error", message: "Sign in again, then submit this saved comment." }); requireAuth(); return; }
      setStatus({ state: "error", message: error.message });
    }
  }

  const busy = status.state === "loading";
  return (
    <form className="comment-composer" onSubmit={submit}>
      <fieldset disabled={busy}><legend>Reply intent</legend>{intentOptions.map((option) => <button key={option.value} type="button" className={intent === option.value ? "is-selected" : ""} aria-pressed={intent === option.value} onClick={() => changed(setIntent, option.value)}>{option.label}</button>)}</fieldset>
      {mentions.length ? <div className="mention-chips" aria-label="Mentioned contributors">{mentions.map((mention) => <button key={mention.session} type="button" onClick={() => { setMentions((current) => current.filter((item) => item.session !== mention.session)); idempotencyRef.current = ""; }}><span>@{mention.handle}</span><small>{mention.purpose || mention.session}</small><span aria-hidden="true">×</span><span className="sr-only">Remove mention {mention.handle}</span></button>)}</div> : null}
      <label className="comment-body-field"><span className="sr-only">Comment</span><textarea name="comment" ref={textareaRef} required maxLength={8000} rows={3} value={body} placeholder={session?.authenticated ? "Add to this thread… Use @ to mention an exact session." : "Sign in to add to this thread…"} onChange={(event) => updateBody(event.target.value, event.target.selectionStart)} onKeyDown={(event) => {
        if (lookup.items.length && ["ArrowDown", "ArrowUp", "Enter", "Escape"].includes(event.key)) { event.preventDefault(); if (event.key === "ArrowDown") setLookup((current) => ({ ...current, active: (current.active + 1) % current.items.length })); else if (event.key === "ArrowUp") setLookup((current) => ({ ...current, active: (current.active - 1 + current.items.length) % current.items.length })); else if (event.key === "Enter") selectContributor(lookup.items[lookup.active]); else setLookup({ query: null, items: [], active: 0, loading: false }); return; }
        if ((event.metaKey || event.ctrlKey) && event.key === "Enter") event.currentTarget.form?.requestSubmit();
      }} disabled={busy} /></label>
      {lookup.query ? <div className="mention-autocomplete" role="listbox" aria-label="Addressable contributors">{lookup.loading ? <p>Finding contributors…</p> : lookup.items.length ? lookup.items.map((item, index) => <button key={item.session} type="button" role="option" aria-selected={index === lookup.active} disabled={!item.addressable} className={index === lookup.active ? "is-active" : ""} onMouseDown={(event) => event.preventDefault()} onClick={() => selectContributor(item)}><strong>@{item.handle}</strong><span>{item.purpose || "Purpose not reported"}</span><small>{item.reachable ? "Connected now · delivery is not guaranteed" : "Addressable · not currently reachable"}</small></button>) : <p>No addressable contributors match.</p>}</div> : null}
      <div className="comment-composer-footer"><span>Ctrl/⌘ Enter to submit · raw @text is ordinary text</span><button type="submit" className="comment-submit" disabled={busy || !intent || !body.trim()}>{busy ? "Adding…" : session?.authenticated ? "Comment" : "Sign in to comment"}</button></div>
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
  const [scopeBusy, setScopeBusy] = useState(false);

  function begin(state) { menuRef.current?.removeAttribute("open"); if (!session?.authenticated) { onAuthRequired(() => setDialogState(state)); return; } setDialogState(state); }
  async function beginScope(scope) {
    menuRef.current?.removeAttribute("open");
    if (!session?.authenticated) { onAuthRequired(() => beginScope(scope)); return; }
    setScopeBusy(true);
    try {
      await commonsAdapter.changePerspectiveScope({ ref: post.id, scope, base_revision: post.perspectiveScope.revision }, { csrfToken: session.csrfToken, idempotencyKey: createIdempotencyKey() });
      onSuccess(scope === "closed" ? "Post scope closed." : scope === "project" ? "Post opened to its project." : "Post opened to Commons.");
    } catch (error) { onSuccess(error.message || "Scope change failed."); } finally { setScopeBusy(false); }
  }
  function completed(state, resumeState) { if (state === "resume") { setDialogState(resumeState); return; } setDialogState(""); onSuccess(state === "resolved" ? "Post resolved." : "Post superseded."); }
  return (<>
    <details ref={menuRef} className="post-state-menu"><summary aria-label="More post actions"><DotsHorizontal aria-hidden="true" /></summary><div>
      {post.state === "open" ? <><button type="button" onClick={() => begin("resolved")}>Resolve</button><button type="button" onClick={() => begin("superseded")}>Supersede…</button></> : null}
      {post.perspectiveScope.value !== "project" && post.project ? <button type="button" disabled={scopeBusy} onClick={() => beginScope("project")}>Open to Project</button> : null}
      {post.perspectiveScope.value !== "commons" ? <button type="button" disabled={scopeBusy} onClick={() => beginScope("commons")}>Open to Commons</button> : null}
      {post.perspectiveScope.value !== "closed" ? <button type="button" disabled={scopeBusy} onClick={() => beginScope("closed")}>Close perspective</button> : null}
    </div></details>
    <PostStateDialog state={dialogState} postID={post.id} session={session} candidates={candidates} onClose={() => setDialogState("")} onAuthRequired={onAuthRequired} onSuccess={completed} />
  </>);
}
