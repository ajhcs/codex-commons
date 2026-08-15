import { useEffect, useId, useRef, useState } from "react";
import { isValidHumanDisplayName, isValidHumanHandle } from "../contracts/commons.js";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { copyText, manualCopyShortcut } from "../browser/copyText.js";
import { navigateAuthDestination, preopenAuthDestination } from "../browser/authDestination.js";
import Members from "../icons/Members.tsx";
import AuthJourney from "./AuthJourney.jsx";
import CommonsMark from "./CommonsMark.jsx";

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
          onChange={(event) => onChange((current) => ({ ...current, displayName: event.target.value }))}
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
          onChange={(event) => onChange((current) => ({ ...current, handle: event.target.value.toLowerCase() }))}
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
  const freshProfileRef = useRef(false);
  const verificationWindowRef = useRef(null);
  const recoveryInputRef = useRef(null);
  const pairingCodeID = useId();
  const pairingHelpID = useId();
  const onAuthenticatedRef = useRef(onAuthenticated);
  const auth = useAuthSession();
  const [recoveryOpen, setRecoveryOpen] = useState(false);
  const [secret, setSecret] = useState("");
  const [recoveryState, setRecoveryState] = useState({ state: "idle", message: "" });
  const [profile, setProfile] = useState({ displayName: "", handle: "" });
  const [copyState, setCopyState] = useState("");
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const [cooldownNow, setCooldownNow] = useState(() => Date.now());
  const [historyOffer, setHistoryOffer] = useState(null);
  const [historyCheckSlow, setHistoryCheckSlow] = useState(false);
  const [historyCheckFailed, setHistoryCheckFailed] = useState(false);
  const [autoOpenFailed, setAutoOpenFailed] = useState(false);

  const closeDialog = useModal(open, dialogRef, onClose);
  const pairing = auth.pairing;
  const profileMode = auth.status === "needs_profile" || auth.error?.resumeState === "needs_profile";
  const pairingMode = auth.status === "pairing";
  const flowError = auth.status === "error" && !profileMode ? auth.error : null;
  const cooldownSeconds = Math.max(0, Math.ceil((cooldownUntil - cooldownNow) / 1000));
  const retryBlocked = cooldownSeconds > 0;
  const completionSession = auth.status === "authenticated" && flowStartedRef.current ? auth.session : null;
  const completionMode = Boolean(completionSession?.authenticated);
  const journeyStage = completionMode
    ? "complete"
    : profileMode
      ? "profile"
      : pairingMode
        ? pairing?.userCode ? "authorize" : "connecting"
        : "ready";

  useEffect(() => {
    if (!flowError?.retryAfterSeconds && !flowError?.retryAt) {
      setCooldownUntil(0);
      return undefined;
    }
    const serverDeadline = Date.parse(flowError.retryAt || "");
    const deadline = Number.isFinite(serverDeadline) ? serverDeadline : Date.now() + (Number(flowError.retryAfterSeconds) || 0) * 1000;
    setCooldownUntil(deadline);
    setCooldownNow(Date.now());
    const timer = globalThis.setInterval(() => setCooldownNow(Date.now()), 1000);
    return () => globalThis.clearInterval(timer);
  }, [flowError?.code, flowError?.retryAfterSeconds, flowError?.retryAt]);

  useEffect(() => {
    onAuthenticatedRef.current = onAuthenticated;
  }, [onAuthenticated]);

  useEffect(() => {
    if (!open) return;
    if (auth.profileDraft?.displayName || auth.profileDraft?.handle) setProfile(auth.profileDraft);
    queueMicrotask(() => {
      if (profileMode) dialogRef.current?.querySelector("input[name='display-name']")?.focus();
      else if (recoveryOpen) recoveryInputRef.current?.focus();
    });
  }, [open, profileMode, recoveryOpen, auth.profileDraft]);

  useEffect(() => {
    if (!open || !flowStartedRef.current || auth.status !== "authenticated" || !auth.session) return undefined;
    const reducedMotion = globalThis.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches;
    let cancelled = false;
    let slowTimer;
    const timer = globalThis.setTimeout(async () => {
      if (!freshProfileRef.current || auth.session?.authMethod !== "codex") {
        flowStartedRef.current = false;
        onAuthenticatedRef.current?.(auth.session, { freshCodexProfile: false });
        return;
      }
      slowTimer = globalThis.setTimeout(() => setHistoryCheckSlow(true), 300);
      try {
        const model = await commonsAdapter.readProjectArchaeology();
        if (cancelled) return;
        if (model.capabilities?.discovery?.available === true) {
          setHistoryOffer(model);
          queueMicrotask(() => dialogRef.current?.querySelector("#auth-history-offer-title")?.focus());
          return;
        }
      } catch {
        if (!cancelled) setHistoryCheckFailed(true);
        return;
      } finally {
        globalThis.clearTimeout(slowTimer);
        if (!cancelled) setHistoryCheckSlow(false);
      }
      flowStartedRef.current = false;
      onAuthenticatedRef.current?.(auth.session, { freshCodexProfile: true });
    }, reducedMotion ? 0 : 420);
    return () => { cancelled = true; globalThis.clearTimeout(timer); globalThis.clearTimeout(slowTimer); };
  }, [auth.session, auth.status, open]);

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
    freshProfileRef.current = false;
    setRecoveryOpen(false);
    setHistoryOffer(null);
    setHistoryCheckSlow(false);
    setHistoryCheckFailed(false);
    setAutoOpenFailed(false);
    if (verificationWindowRef.current && !verificationWindowRef.current.closed) verificationWindowRef.current.close();
    verificationWindowRef.current = null;
  }

  function keepFocusInside(event) {
    if (event.key !== "Tab") return;
    const dialog = dialogRef.current;
    const focusable = [...(dialog?.querySelectorAll("a[href], button:not([disabled]), input:not([disabled])") || [])]
      .filter((node) => node.getClientRects().length > 0);
    if (!focusable.length) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && dialog?.ownerDocument.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && dialog?.ownerDocument.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
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
    freshProfileRef.current = false;
    flowStartedRef.current = true;
    setAutoOpenFailed(false);
    setRecoveryState({ state: "idle", message: "" });
    const destination = preopenAuthDestination();
    verificationWindowRef.current = destination;
    try {
      const pairingResult = await auth.beginPairing();
      if (pairingResult?.verificationURL && !navigateAuthDestination(destination, pairingResult.verificationURL)) setAutoOpenFailed(true);
    } catch {
      if (destination && !destination.closed) destination.close();
      verificationWindowRef.current = null;
      // The shared auth state contains the user-facing error and retry target.
    }
  }

  async function retry() {
    flowStartedRef.current = true;
    setAutoOpenFailed(false);
    const destination = preopenAuthDestination();
    verificationWindowRef.current = destination;
    try {
      const pairingResult = await auth.retryPairing();
      if (pairingResult?.verificationURL && !navigateAuthDestination(destination, pairingResult.verificationURL)) setAutoOpenFailed(true);
    } catch {
      if (destination && !destination.closed) destination.close();
      verificationWindowRef.current = null;
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
    freshProfileRef.current = true;
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
    const copied = await copyText(pairing?.userCode || "");
    if (copied) {
      setCopyState("Code copied.");
      return;
    }
    setCopyState(`Copy isn’t available here. Press ${manualCopyShortcut()} to copy the selected code, or type it in.`);
    queueMicrotask(() => { codeRef.current?.focus({ preventScroll: true }); codeRef.current?.select(); });
  }

  function continueToHistory() {
    const model = historyOffer;
    setHistoryOffer(null);
    flowStartedRef.current = false;
    auth.requestProjectHistory(model);
    resetLocal();
    closeDialog();
  }

  function skipHistory() {
    setHistoryOffer(null);
    setHistoryCheckFailed(false);
    flowStartedRef.current = false;
    onAuthenticatedRef.current?.(auth.session, { freshCodexProfile: true });
  }

  const title = completionMode ? "Welcome to Commons" : profileMode ? "Finish your Commons profile" : pairingMode ? pairing?.userCode ? "Authorize with Codex" : "Connecting to Codex" : flowError ? "Codex sign-in needs attention" : recoveryOpen ? "Use a recovery key" : "Sign in to Commons";
  const description = completionMode
    ? "Your Codex account is connected to your Commons identity."
    : profileMode
      ? "Codex is connected. Choose the identity that will appear on your Commons writing."
    : pairingMode
      ? pairing?.userCode ? "Open the secure Codex page, enter the one-time code, then return here." : "Commons is establishing a secure local connection to the Codex App Server."
      : flowError
        ? "Reading remains available while you retry sign-in."
        : recoveryOpen
          ? "Recovery is an explicit local fallback for installations that enable it."
          : "Link this installation to your Codex account to write in Commons.";

  return (
    <dialog ref={dialogRef} className="auth-dialog auth-dialog--codex" aria-labelledby="auth-dialog-title" aria-describedby="auth-dialog-description" onClose={closed} onCancel={cancel} onKeyDown={keepFocusInside}>
      <form onSubmit={profileMode ? submitProfile : recoveryOpen ? submitRecovery : (event) => { event.preventDefault(); beginCodex(); }}>
        <header>
          <span className="auth-mark"><CommonsMark state={completionMode ? "resolved" : pairingMode ? journeyStage === "authorize" ? "authorized" : "connecting" : "idle"} size="small" /></span>
          <div>
            <h2 id="auth-dialog-title">{title}</h2>
            <p id="auth-dialog-description">{description}</p>
          </div>
        </header>

        {completionMode && historyOffer ? (
          <section className="auth-history-offer auth-content-transition" aria-labelledby="auth-history-offer-title">
            <AuthJourney stage="complete" identity={completionSession.principal} />
            <span className="history-offer-status">Identity connected</span>
            <h2 id="auth-history-offer-title" tabIndex="-1">Bring your Codex work into Commons?</h2>
            <p>Choose which projects Commons should understand. Nothing is imported until you review it.</p>
            <footer>
              <button type="button" className="secondary-button" onClick={skipHistory}>Not now</button>
              <button type="button" className="primary-button" onClick={continueToHistory}>Choose projects</button>
            </footer>
            <small>You can return from your account menu.</small>
          </section>
        ) : completionMode && historyCheckFailed ? (
          <section className="auth-history-offer auth-content-transition" aria-labelledby="auth-history-offer-title">
            <AuthJourney stage="complete" identity={completionSession.principal} />
            <span className="history-offer-status">Identity connected</span>
            <h2 id="auth-history-offer-title" tabIndex="-1">Project history is unavailable</h2>
            <p role="status">Project history setup could not be checked. You can try again from your account menu.</p>
            <footer><button type="button" className="primary-button" onClick={skipHistory}>Enter Commons</button></footer>
          </section>
        ) : completionMode ? (
          <>
            <AuthJourney stage="complete" identity={completionSession.principal} />
            <p className="auth-complete-status" role="status">Connected. Entering Commons…</p>
            {historyCheckSlow ? <p className="auth-history-check" role="status">Checking project history availability…</p> : null}
          </>
        ) : profileMode ? (
          <>
            <AuthJourney stage="profile" identity={profile} />
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
            <AuthJourney stage={journeyStage} />
            {pairing?.userCode ? (
              <>
                <div className="pairing-code-card">
                  <span>One-time code</span>
                  <input
                    ref={codeRef}
                    id={pairingCodeID}
                    className="pairing-code-value"
                    value={pairing.userCode}
                    readOnly
                    spellCheck="false"
                    aria-label="One-time code"
                    aria-describedby={pairingHelpID}
                    onFocus={(event) => event.currentTarget.select()}
                  />
                  <button type="button" className="secondary-button" onClick={copyCode}>Copy code</button>
                  <small role="status">{copyState || (globalThis.isSecureContext === true ? "\u00a0" : `Select the code and press ${manualCopyShortcut()} to copy it.`)}</small>
                </div>
                {autoOpenFailed ? <p className="pairing-auto-open-note" role="status">Your browser could not open it automatically.</p> : null}
                {pairing.verificationURL ? <a className={`pairing-link${autoOpenFailed ? " primary-button" : ""}`} href={pairing.verificationURL} target="_blank" rel="noreferrer">{autoOpenFailed ? "Open Codex sign-in" : "Open sign-in again"} <span aria-hidden="true">↗</span></a> : null}
                <p id={pairingHelpID} className="pairing-status" aria-live="polite"><strong>Waiting for your confirmation in Codex.</strong> Commons checks automatically; this code stays visible while you authorize.</p>
                <footer>
                  <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
                  <button type="button" className="primary-button" onClick={pollNow}>I’ve finished</button>
                </footer>
              </>
            ) : (
              <>
                <div className="auth-handshake-state" role="status">
                  <strong>Establishing the secure connection</strong>
                  <span>The one-time code will appear here when Codex is ready.</span>
                </div>
                <footer><button type="button" className="secondary-button" onClick={requestClose}>Cancel</button></footer>
              </>
            )}
          </>
        ) : flowError ? (
          <>
            <div className="auth-error-card" role="alert"><strong>{errorMessage(flowError)}</strong><span>{retryBlocked ? `Try again in ${cooldownSeconds}s. Reading remains available.` : "Nothing was saved in the browser."}</span></div>
            <footer>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
              <button type="button" className="primary-button" onClick={retry} disabled={retryBlocked}>{retryBlocked ? `Try again in ${cooldownSeconds}s` : "Try again"}</button>
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
            <AuthJourney stage="ready" />
            <div className="auth-ready-content">
              <div className="auth-primary-card">
                <strong>Continue with your Codex account</strong>
                <span>Commons receives only the account identity needed to bind this installation.</span>
                <button type="submit" className="primary-button">Continue with Codex</button>
              </div>
              <p className="auth-privacy">Your Codex password, token, and browser credentials stay with Codex.</p>
            </div>
            <footer className="auth-ready-footer">
              <button type="button" className="auth-recovery-link" onClick={() => setRecoveryOpen(true)}>Use recovery key</button>
              <button type="button" className="secondary-button" onClick={requestClose}>Cancel</button>
            </footer>
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
    <dialog ref={dialogRef} className="auth-dialog profile-dialog" aria-labelledby="profile-dialog-title" aria-describedby="profile-dialog-description" onClose={close} onCancel={(event) => { event.preventDefault(); close(); }}>
      <form onSubmit={submit}>
        <header>
          <span className="auth-mark"><CommonsMark state="authorized" size="small" /></span>
          <div><h2 id="profile-dialog-title">Edit profile</h2><p id="profile-dialog-description">Your dynamic human identity is used for new Commons writing.</p></div>
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

export function SessionControl({ status, session, onSignIn, onSignOut, onEditProfile, onOpenArchaeology }) {
  if (status === "loading" && !session) return <span className="session-status">Checking session…</span>;
  if (!session?.authenticated) return <button type="button" className="session-sign-in" onClick={onSignIn}><Members aria-hidden="true" /><strong>Sign in</strong></button>;
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
        <button type="button" onClick={onOpenArchaeology}>Bring in project history</button>
        {session.profileRevision > 0 ? <button type="button" onClick={onEditProfile}>Edit profile</button> : null}
        <button type="button" onClick={onSignOut}>Sign out</button>
      </div>
    </details>
  );
}
