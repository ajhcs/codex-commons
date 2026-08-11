import { useEffect, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import BookOpen from "../icons/BookOpen.tsx";

export function createIdempotencyKey() {
  const browserCrypto = globalThis.crypto;
  if (typeof browserCrypto?.randomUUID === "function") return browserCrypto.randomUUID();
  if (typeof browserCrypto?.getRandomValues === "function") {
    const bytes = browserCrypto.getRandomValues(new Uint8Array(16));
    return `web-${[...bytes].map((value) => value.toString(16).padStart(2, "0")).join("")}`;
  }
  throw new Error("Secure browser randomness is unavailable. Writing is disabled.");
}

export function isExpiredSession(error) {
  return error?.status === 401 || error?.code === "csrf_failed";
}

export function LoginDialog({ open, onClose, onAuthenticated }) {
  const dialogRef = useRef(null);
  const inputRef = useRef(null);
  const controllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const [secret, setSecret] = useState("");
  const [status, setStatus] = useState({ state: "idle", message: "" });

  function clearSensitiveState() {
    controllerRef.current?.abort();
    controllerRef.current = null;
    idempotencyRef.current = "";
    setSecret("");
    setStatus({ state: "idle", message: "" });
  }

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      queueMicrotask(() => inputRef.current?.focus());
    }
    if (!open) {
      clearSensitiveState();
      if (dialog.open) dialog.close();
    }
  }, [open]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  function updateSecret(value) {
    setSecret(value);
    setStatus({ state: "idle", message: "" });
    idempotencyRef.current = "";
  }

  async function submit(event) {
    event.preventDefault();
    if (!secret) {
      setStatus({ state: "error", message: "Enter the writing key." });
      inputRef.current?.focus();
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey();
      setStatus({ state: "loading", message: "" });
      const session = await commonsAdapter.login(secret, idempotencyRef.current, controller.signal);
      setSecret("");
      idempotencyRef.current = "";
      setStatus({ state: "idle", message: "" });
      onAuthenticated(session);
    } catch (error) {
      if (error.name === "AbortError") return;
      setStatus({
        state: "error",
        message: error.status === 401 ? "That writing key was not accepted." : error.message,
      });
    }
  }

  function requestClose() {
    clearSensitiveState();
    if (dialogRef.current?.open) dialogRef.current.close();
    else onClose();
  }

  function cancel(event) {
    if (status.state === "loading") {
      event.preventDefault();
      return;
    }
    event.preventDefault();
    requestClose();
  }

  function closed() {
    clearSensitiveState();
    onClose();
  }

  return (
    <dialog ref={dialogRef} className="auth-dialog" onClose={closed} onCancel={cancel}>
      <form onSubmit={submit}>
        <header>
          <span className="auth-mark" aria-hidden="true"><BookOpen /></span>
          <div>
            <h2>Unlock writing</h2>
            <p>Reading stays available. Sign in only when you want to contribute.</p>
          </div>
        </header>
        <label>
          <span>Writing key</span>
          <input
            ref={inputRef}
            type="password"
            name="writing-key"
            autoComplete="current-password"
            value={secret}
            onChange={(event) => updateSecret(event.target.value)}
            disabled={status.state === "loading"}
            required
          />
        </label>
        <p className="auth-privacy">The key is sent directly to this Commons server and is never stored in the browser.</p>
        {status.message ? <p className="form-message form-message--error" role="alert">{status.message}</p> : null}
        <footer>
          <button type="button" className="secondary-button" onClick={requestClose} disabled={status.state === "loading"}>Cancel</button>
          <button type="submit" className="primary-button" disabled={status.state === "loading"}>{status.state === "loading" ? "Signing in…" : "Continue"}</button>
        </footer>
      </form>
    </dialog>
  );
}

export function SessionControl({ status, session, onSignIn, onSignOut }) {
  if (status === "loading" && !session) return <span className="session-status">Checking session…</span>;
  if (!session?.authenticated) {
    return <button type="button" className="session-sign-in" onClick={onSignIn}>Sign in</button>;
  }
  const name = session.principal.displayName;
  return (
    <details className="session-menu">
      <summary aria-label={`Signed in as ${name}`}>
        <span aria-hidden="true">{name.slice(0, 1).toUpperCase()}</span>
        <strong>{name}</strong>
      </summary>
      <div>
        <p>Signed in for writing</p>
        <button type="button" onClick={onSignOut}>Sign out</button>
      </div>
    </details>
  );
}
