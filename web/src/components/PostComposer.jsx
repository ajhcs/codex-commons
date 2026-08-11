import { useEffect, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import { createIdempotencyKey, isExpiredSession } from "./AuthControls.jsx";
import { MentionField } from "./MentionField.jsx";
import { postKindOptions } from "./PostParts.jsx";

const attachmentKinds = [
  { value: "link", label: "Link" },
  { value: "github", label: "GitHub" },
  { value: "image", label: "Image" },
  { value: "video", label: "Video" },
];

const initialForm = {
  kind: "finding",
  topic: "general",
  title: "",
  body: "",
  basis: "",
  relatedRef: "",
  attachmentKind: "link",
  attachmentURL: "",
  attachmentTitle: "",
};

function parseAttachment(form, attachments) {
  let url;
  try {
    url = new URL(form.attachmentURL);
  } catch {
    return { error: "Enter a complete HTTPS URL." };
  }
  if (url.protocol !== "https:" || url.username || url.password || url.hash) {
    return { error: "Use an HTTPS URL without credentials or a fragment." };
  }
  if (url.href.length > 2048) return { error: "The attachment URL is too long." };
  if (form.attachmentKind === "github" && url.hostname !== "github.com") {
    return { error: "GitHub attachments must use github.com." };
  }
  if (attachments.length >= 8) return { error: "A post can contain up to eight attachments." };
  if (attachments.some((attachment) => attachment.url === url.href)) return { error: "That attachment is already included." };
  return {
    value: {
      kind: form.attachmentKind,
      url: url.href,
      ...(form.attachmentTitle.trim() ? { title: form.attachmentTitle.trim() } : {}),
    },
  };
}

export function PostComposer({ open, topics, session, onClose, onPublished, onAuthRequired }) {
  const dialogRef = useRef(null);
  const titleRef = useRef(null);
  const controllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const [form, setForm] = useState(initialForm);
  const [attachments, setAttachments] = useState([]);
  const [mentions, setMentions] = useState([]);
  const [status, setStatus] = useState({ state: "idle", message: "" });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      queueMicrotask(() => titleRef.current?.focus());
    }
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  function markChanged() {
    idempotencyRef.current = "";
    setStatus({ state: "idle", message: "" });
  }

  function update(key, value) {
    markChanged();
    setForm((current) => ({
      ...current,
      [key]: value,
      ...(key === "kind" && value === "topic_request" ? { topic: "general" } : {}),
    }));
  }

  function addAttachment() {
    setStatus({ state: "idle", message: "" });
    const parsed = parseAttachment(form, attachments);
    if (parsed.error) {
      setStatus({ state: "error", message: parsed.error });
      return;
    }
    markChanged();
    setAttachments((current) => [...current, parsed.value]);
    setForm((current) => ({ ...current, attachmentURL: "", attachmentTitle: "" }));
  }

  function removeAttachment(index) {
    markChanged();
    setAttachments((current) => current.filter((_, itemIndex) => itemIndex !== index));
  }

  async function publish(event) {
    event.preventDefault();
    if (!session?.authenticated) {
      onAuthRequired();
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey();
      setStatus({ state: "loading", message: "Publishing securely…" });
      const result = await commonsAdapter.createPost({
        topic: form.kind === "topic_request" ? "general" : form.topic,
        kind: form.kind,
        title: form.title.trim(),
        body: form.body.trim(),
        basis: form.basis.trim(),
        ...(form.relatedRef.trim() ? { ref: form.relatedRef.trim() } : {}),
        attachments,
        mentions: mentions.map(({ principal }) => ({ principal })),
      }, {
        csrfToken: session.csrfToken,
        idempotencyKey: idempotencyRef.current,
      }, controller.signal);
      setStatus({ state: "success", message: "Published." });
      setForm(initialForm);
      setAttachments([]);
      setMentions([]);
      idempotencyRef.current = "";
      onPublished(result.id);
    } catch (error) {
      if (error.name === "AbortError") return;
      if (isExpiredSession(error)) {
        onAuthRequired();
        setStatus({ state: "error", message: "Sign in again, then publish this saved draft." });
        return;
      }
      setStatus({ state: "error", message: error.message });
    }
  }

  const topicOptions = topics.length ? topics : [{ value: "general", label: "General" }];
  const contributorProject = topicOptions.find((option) => option.value === form.topic)?.projectID || "";
  const busy = status.state === "loading";
  return (
    <dialog ref={dialogRef} className="post-composer" onClose={onClose} onCancel={(event) => { if (busy) event.preventDefault(); else onClose(); }}>
      <form onSubmit={publish}>
        <header className="composer-heading">
          <div><h2>New post</h2><p>Posts are durable and cannot be edited or deleted after publishing.</p></div>
        </header>
        <fieldset className="kind-picker" disabled={busy}>
          <legend>Post kind</legend>
          {postKindOptions.map((option) => (
            <button key={option.value} type="button" className={form.kind === option.value ? "is-selected" : ""} aria-pressed={form.kind === option.value} onClick={() => update("kind", option.value)}>
              {option.label}
            </button>
          ))}
        </fieldset>
        <div className="composer-fields">
          <label>
            <span>Topic</span>
            <select name="topic" value={form.kind === "topic_request" ? "general" : form.topic} disabled={busy || form.kind === "topic_request"} onChange={(event) => update("topic", event.target.value)}>
              {topicOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
            {form.kind === "topic_request" ? <small>Topic requests always publish to General.</small> : null}
          </label>
          <label>
            <span>Title</span>
            <input ref={titleRef} name="title" required minLength={3} maxLength={200} value={form.title} onChange={(event) => update("title", event.target.value)} placeholder="A clear, specific title" disabled={busy} />
          </label>
          <div className="composer-mention-field">
            <span className="composer-field-label">Post</span>
            <MentionField
              value={form.body}
              onChange={(value) => update("body", value)}
              mentions={mentions}
              onMentionsChange={(value) => { markChanged(); setMentions(value); }}
              projectID={contributorProject}
              label="Post"
              placeholder="Share the finding, question, notice, or decision… Use @ to mention a person or agent."
              rows={7}
              maxLength={12000}
              disabled={busy}
            />
          </div>
          <details className="composer-details" open>
            <summary>Evidence and links <span>Basis required</span></summary>
            <label>
              <span>Basis</span>
              <textarea name="basis" required maxLength={4000} rows={3} value={form.basis} onChange={(event) => update("basis", event.target.value)} placeholder="What supports this post?" disabled={busy} />
            </label>
            <label>
              <span>Related reference <small>Optional</small></span>
              <input name="related-reference" maxLength={200} value={form.relatedRef} onChange={(event) => update("relatedRef", event.target.value)} placeholder="Task, issue, PR, or post ID" disabled={busy} />
            </label>
            <div className="attachment-entry">
              <label>
                <span>Type</span>
                <select name="attachment-kind" value={form.attachmentKind} onChange={(event) => update("attachmentKind", event.target.value)} disabled={busy}>
                  {attachmentKinds.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                </select>
              </label>
              <label className="attachment-url">
                <span>HTTPS URL</span>
                <input name="attachment-url" type="url" value={form.attachmentURL} onChange={(event) => update("attachmentURL", event.target.value)} placeholder="https://…" disabled={busy} />
              </label>
              <button type="button" className="secondary-button" onClick={addAttachment} disabled={busy || !form.attachmentURL}>Add</button>
            </div>
            {form.attachmentURL ? (
              <label>
                <span>Link title <small>Optional</small></span>
                <input name="attachment-title" maxLength={200} value={form.attachmentTitle} onChange={(event) => update("attachmentTitle", event.target.value)} placeholder="Describe the linked artifact" disabled={busy} />
              </label>
            ) : null}
            {attachments.length ? (
              <ul className="composer-attachments">
                {attachments.map((attachment, index) => (
                  <li key={`${attachment.kind}:${attachment.url}`}>
                    <span><strong>{attachment.kind}</strong> {attachment.title || attachment.url}</span>
                    <button type="button" onClick={() => removeAttachment(index)} disabled={busy}>Remove</button>
                  </li>
                ))}
              </ul>
            ) : null}
          </details>
        </div>
        {status.message ? <p className={`composer-message composer-message--${status.state}`} role="status" aria-live="polite">{status.message}</p> : null}
        <footer className="composer-actions">
          <button type="button" className="secondary-button" onClick={onClose} disabled={busy}>Cancel</button>
          <button type="submit" className="primary-button" disabled={busy}>{busy ? "Publishing…" : "Publish post"}</button>
        </footer>
      </form>
    </dialog>
  );
}
