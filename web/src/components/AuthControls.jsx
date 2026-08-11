import { useEffect, useRef, useState } from "react";
import { isValidHumanDisplayName, isValidHumanHandle } from "../contracts/commons.js";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
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

function useModal(open, dialogRef, onClose) {
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open, dialogRef]);

  function close() {
    if (dialogRef.current?.open) dialogRef.current.close();
    else onClose?.();
  }

  return close;
}

function errorMessage(error) {
  return error?.message || "Commons sign-in could not be completed.";
}

function ProfileFields({ value, onChange, disabled = false }) {
  return (
    <div className="auth-profile-fields">
      <label>
        <span>Display name</span>
        <input
          name="display-name"
          autoComplete="name"
          value={value.displayName}
          onChange={(event) => onChange({ ...value, displayName: event.target.value })}
          disabled={disabled}
          maxLength={200}
          required
        />
      </label>
      <label>
        <span>Handle</span>
        <div className="auth-handle-input"><span aria-hidden="true">@</span><input
          name="handle"
          autoComplete="username"
          value={value.handle}
          onChange={(event) => onChange({ ...value, handle: event.target.value.toLowerCase() })}
          disabled={disabled}
          minLength={3}
          maxLength={64}
          pattern="[a-z0-9](?:[a-z0-9-]{1,62}[a-z0-9])?"
          required
        /></div>
        <small>Lowercase letters, numbers, and internal hyphens.</small>
      </label>
    </div>
  );
}

export function LoginDialog({ open, onClose, onAuthenticated }) {
  const dialogRef = useRef(null);
  const codeRef = useRef(null);
  const controllerRef = useRef(null);
  const idempotencyRef = useRef("");
  const flowStartedRef = useRef(false);
  const recoveryInputRef = useRef(null);
  const auth = useAuthSession();
  const [recoveryOpen, setRecoveryOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [recoveryState, setRecoveryState] = useState({ state: "idle", message: "" });
  const [profile, setProfile] = useState({ displayName: "", handle: "" });
  const [copyState, setCopyState] = useState("");

  const closeDialog = useModal(open, dialogRef, onClose);
  const pairing = auth.pairing;
  const profileMode = auth.status === "needs_profile" || auth.error?.resumeState === "needs_profile";
  const pairingMode = auth.status === "pairing";
  const flowError = auth.status === "error" && !profileMode ? auth.error : null;

  useEffect(() => {
    if (!open) return;
    if (auth.profileDraft?.displayName || auth.profileDraft?.handle) setProfile(auth.profileDraft);
    queueMicrotask(() => {
      if (profileMode) document.querySelector("input[name='display-name']")?.focus();
      else if (recoveryOpen) recoveryInputRef.current?.focus();
    });
  }, [open, profileMode, recoveryOpen, auth.profileDraft]);

  useEffect(() => {
    if (!open || !flowStartedRef.current || auth.status !== "authenticated" || !auth.session) return;
    flowStartedRef.current = false;
    onAuthenticated?.(auth.session);
  }, [auth.session, auth.status, onAuthenticated, open]);

  useEffect(() => () => {
    controllerRef.current?.abort();
  }, []);

  function resetLocal() {
    controllerRef.current?.abort();
    controllerRef.current = null;
    idempotencyRef.current = "";
    setSecret("");
    setRecoveryState({ state: "idle", message: "" });
    setCopyState("");
    setRecoveryOpen(false);
  }

  function requestClose() {
    if (pairingMode || profileMode) auth.cancelPairing().catch(() => {});
    flowStartedRef.current = false;
    resetLocal();
    closeDialog();
  }

  function cancel(event) {
    event.preventDefault();
    requestClose();
  }

  function closed() {
    resetLocal();
    onClose?.();
  }

  async function beginCodex() {
    flowStartedRef.current = true;
    setRecoveryState({ state: "idle", message: "" });
    try {
      await auth.beginPairing();
    } catch {
      // The shared auth state contains the user-facing error and retry target.
    }
  }

  async function retry() {
    flowStartedRef.current = true;
    try {
      await auth.retryPairing();
    } catch {
      // The shared auth state contains the user-facing error.
    }
  }

  async function pollNow() {
    try { await auth.pollNow(); } catch { /* shared auth state renders the error */ }
  }

  async function submitProfile(event) {
    event.preventDefault();
    const next = { displayName: profile.displayName.trim(), handle: profile.handle.trim().toLowerCase() };
    if (!isValidHumanDisplayName(next.displayName) || !isValidHumanHandle(next.handle)) return;
    try {
      await auth.submitProfile(next);
    } catch {
      // The shared auth state preserves the profile draft for retry.
    }
  }

  async function submitRecovery(event) {
    event.preventDefault();
    if (!secret) {
      setRecoveryState({ state: "error", message: "Enter the recovery key." });
      recoveryInputRef.current?.focus();
      return;
    }
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      idempotencyRef.current ||= createIdempotencyKey();
      setRecoveryState({ state: "loading", message: "" });
      const session = await commonsAdapter.login(secret, idempotencyRef.current, controller.signal);
      setSecret("");
      idempotencyRef.current = "";
      auth.accept(session);
      onAuthenticated?.(session);
    } catch (error) {
      if (error?.name === "AbortError") return;
      setRecoveryState({ state: "error", message: error.status === 401 ? "That recovery key was not accepted." : errorMessage(error) });
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
    }
  }

  async function copyCode() {
    if (!pairing?.userCode || typeof globalThis.navigator?.clipboard?.writeText !== "function") return;
    try {
      await globalThis.navigator.clipboard.writeText(pairing.userCode);
      setCopyState("Code copied");
    } catch {
      setCopyState("Copy unavailable");
    }
  }

  const title = profileMode ? "Finish your Commons profile" : pairingMode ? "Continue with Codex" : flowError ? "Codex sign-in needs attention" : recoveryOpen ? "Use a recovery key" : "Sign in to Commons";
  const description = profileMode
    ? "Choose the identity that will appear on your Commons writing."
    : pairingMode
      ? "Open the secure Codex page, enter the one-time code, then return here."
      : flowError
        ? "Reading remains available while you retry sign-in."
        : recoveryOpen
          ? "Recovery is an explicit local fallback for installations that enable it."
          : "Link this installation to your Codex account to write in Commons.";

  return (
    <dialog ref={dialogRef} className="auth-dialog auth-dialog--codex" onClose={closed} onCancel={cancel}>
      <form onSubmit={profileMode ? submitProfile : recoveryOpen ? submitRecovery : (event) => { event.preventDefault(); beginCodex(); }}>
        <header>
          <span className="auth-mark" aria-hidden="true"><BookOpen /></span>
          <div>
            <h2>{title}</h2>
            <p>{description}</p>
          </div>
        </header>

        {profileMode ? (
          <>
            <ProfileFields value={profile} onChange={setProfile} disabled={false} />
            {auth.error?.message ? <p className="form-message form-message--error" role="alert">{auth.error.message}</p> : null}
            <p className="auth-privacy">Commons stores your profile name and handle with this installation. Codex credentials stay with Codex.</p>
            <footer>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
              <button type="submit" className="primary-button" disabled={!isValidHumanDisplayName(profile.displayName.trim()) || !isValidHumanHandle(profile.handle.trim().toLowerCase())}>Save profile</button>
            </footer>
          </>
        ) : pairingMode ? (
          <>
            <div className="pairing-code-card">
              <span>One-time code</span>
              <strong ref={codeRef}>{pairing?.userCode || "Waiting…"}</strong>
              <button type="button" className="secondary-button" onClick={copyCode} disabled={!pairing?.userCode}>Copy code</button>
              {copyState ? <small role="status">{copyState}</small> : null}
            </div>
            {pairing?.verificationURL ? <a className="pairing-link" href={pairing.verificationURL} target="_blank" rel="noreferrer">Open Codex sign-in <span aria-hidden="true">↗</span></a> : null}
            <p className="pairing-status" aria-live="polite">Waiting for Codex confirmation. This window checks automatically.</p>
            <footer>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
              <button type="button" className="primary-button" onClick={pollNow}>I’ve finished</button>
            </footer>
          </>
        ) : flowError ? (
          <>
            <div className="auth-error-card" role="alert"><strong>{errorMessage(flowError)}</strong><span>Nothing was saved in the browser.</span></div>
            <footer>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
              <button type="button" className="primary-button" onClick={retry}>Try again</button>
            </footer>
            <button type="button" className="auth-recovery-link" onClick={() => { auth.clearError(); setRecoveryOpen(true); }}>Use recovery key</button>
          </>
        ) : recoveryOpen ? (
          <>
            <label>
              <span>Recovery key</span>
              <input
                ref={recoveryInputRef}
                type="password"
                name="recovery-key"
                autoComplete="current-password"
                value={secret}
                onChange={(event) => { setSecret(event.target.value); setRecoveryState({ state: "idle", message: "" }); idempotencyRef.current = ""; }}
                disabled={recoveryState.state === "loading"}
                required
              />
            </label>
            <p className="auth-privacy">The key is sent directly to this Commons server and is never stored in the browser.</p>
            {recoveryState.message ? <p className="form-message form-message--error" role="alert">{recoveryState.message}</p> : null}
            <footer>
              <button type="button" className="secondary-button" onClick={() => setRecoveryOpen(false)} disabled={recoveryState.state === "loading"}>Back</button>
              <button type="submit" className="primary-button" disabled={recoveryState.state === "loading"}>{recoveryState.state === "loading" ? "Signing in…" : "Continue"}</button>
            </footer>
          </>
        ) : (
          <>
            <div className="auth-primary-card">
              <strong>Use your Codex account</strong>
              <span>Commons receives only the account identity needed to bind this installation.</span>
              <button type="submit" className="primary-button">Continue with Codex</button>
            </div>
            <p className="auth-privacy">No Codex password, token, or browser credential is stored by Commons.</p>
            <footer>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
            </footer>
            <button type="button" className="auth-recovery-link" onClick={() => setRecoveryOpen(true)}>Use recovery key</button>
          </>
        )}
      </form>
    </dialog>
  );
}

export function ProfileDialog({ open, onClose }) {
  const dialogRef = useRef(null);
  const auth = useAuthSession();
  const [profile, setProfile] = useState({ displayName: "", handle: "" });
  const [state, setState] = useState({ state: "idle", message: "" });
  const closeDialog = useModal(open, dialogRef, onClose);

  useEffect(() => {
    if (!open || !auth.session?.authenticated) return;
    setProfile({ displayName: auth.session.principal.displayName, handle: auth.session.principal.handle });
    setState({ state: "idle", message: "" });
  }, [open, auth.session]);

  function close() {
    setState({ state: "idle", message: "" });
    closeDialog();
  }

  async function submit(event) {
    event.preventDefault();
    const next = { displayName: profile.displayName.trim(), handle: profile.handle.trim().toLowerCase() };
    if (!isValidHumanDisplayName(next.displayName) || !isValidHumanHandle(next.handle)) {
      setState({ state: "error", message: "Choose a display name and a valid lowercase handle." });
      return;
    }
    setState({ state: "loading", message: "" });
    try {
      await auth.updateProfile(next);
      setState({ state: "success", message: "Profile updated." });
    } catch (error) {
      setState({ state: "error", message: errorMessage(error) });
    }
  }

  return (
    <dialog ref={dialogRef} className="auth-dialog profile-dialog" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      <form onSubmit={submit}>
        <header>
          <span className="auth-mark" aria-hidden="true"><BookOpen /></span>
          <div><h2>Edit profile</h2><p>Your dynamic human identity is used for new Commons writing.</p></div>
        </header>
        <ProfileFields value={profile} onChange={setProfile} disabled={state.state === "loading"} />
        {state.message ? <p className={`form-message form-message--${state.state === "success" ? "success" : "error"}`} role="status">{state.message}</p> : null}
        <footer>
          <button type="button" className="secondary-button" onClick={close} disabled={state.state === "loading"}>Close</button>
          <button type="submit" className="primary-button" disabled={state.state === "loading"}>{state.state === "loading" ? "Saving…" : "Save changes"}</button>
        </footer>
      </form>
    </dialog>
  );
}

export function SessionControl({ status, session, onSignIn, onSignOut, onEditProfile }) {
  if (status === "loading" && !session) return <span className="session-status">Checking session…</span>;
  if (!session?.authenticated) return <button type="button" className="session-sign-in" onClick={onSignIn}>Sign in</button>;
  const name = session.principal.displayName;
  const method = session.authMethod === "codex" ? "Signed in with Codex" : "Signed in with recovery";
  return (
    <details className="session-menu">
      <summary aria-label={`Signed in as ${name}`}>
        <span aria-hidden="true">{name.slice(0, 1).toUpperCase()}</span>
        <strong>{name}</strong>
      </summary>
      <div>
        <p>{method}</p>
        {session.profileRevision > 0 ? <button type="button" onClick={onEditProfile}>Edit profile</button> : null}
        <button type="button" onClick={onSignOut}>Sign out</button>
      </div>
    </details>
  );
}
