import { createContext, useCallback, useContext, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import { CommonsAPIError } from "../data/transport.js";
import { AUTH_ACTIONS, authReducer, initialAuthState } from "../contracts/authState.js";

const AuthSessionContext = createContext(null);

function abortError(error) {
  return error?.name === "AbortError";
}

function authError(error, fallback = "Commons sign-in could not be completed.") {
  return {
    code: error?.code || "auth_error",
    message: error?.message || fallback,
    retryAfterSeconds: Number(error?.retryAfterSeconds) || 0,
    retryAt: error?.retryAt || "",
  };
}

function currentHash() {
  if (typeof window === "undefined") return "";
  const hash = typeof window.location?.hash === "string" ? window.location.hash : "";
  return hash.length <= 2048 ? hash : "";
}

function restoreHash(hash) {
  if (!hash || typeof window === "undefined" || !window.location) return;
  if (window.location.hash !== hash) window.location.hash = hash;
}

export function AuthSessionProvider({ children }) {
  const [state, dispatch] = useReducer(authReducer, undefined, initialAuthState);
  const stateRef = useRef(state);
  const controllerRef = useRef(null);
  const pollTimerRef = useRef(null);
  const flowRef = useRef(0);
  const projectHistoryRequestRef = useRef(0);
  const [projectHistoryRequest, setProjectHistoryRequest] = useState(null);

  stateRef.current = state;

  const stopFlow = useCallback(() => {
    flowRef.current += 1;
    if (pollTimerRef.current !== null) {
      globalThis.clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    controllerRef.current?.abort();
    controllerRef.current = null;
  }, []);

  const finishAuthenticated = useCallback((session) => {
    const redirect = stateRef.current.redirect;
    stopFlow();
    dispatch({ type: AUTH_ACTIONS.AUTHENTICATED, session, redirect: "" });
    restoreHash(redirect);
    return session;
  }, [stopFlow]);

  const refresh = useCallback(async () => {
    stopFlow();
    const controller = new AbortController();
    controllerRef.current = controller;
    dispatch({ type: AUTH_ACTIONS.LOAD });
    try {
      const session = await commonsAdapter.readSession(controller.signal);
      if (controller.signal.aborted) throw controller.signal.reason || new DOMException("Aborted", "AbortError");
      dispatch({ type: AUTH_ACTIONS.SESSION, session });
      return session;
    } catch (error) {
      if (abortError(error)) throw error;
      dispatch({ type: AUTH_ACTIONS.ERROR, error: { ...authError(error, "Commons could not check the writing session."), resumeState: "unauthenticated" } });
      throw error;
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
    }
  }, [stopFlow]);

  const schedulePoll = useCallback((flowID, attemptID, delay) => {
    if (flowID !== flowRef.current) return;
    if (pollTimerRef.current !== null) globalThis.clearTimeout(pollTimerRef.current);
    pollTimerRef.current = globalThis.setTimeout(async () => {
      pollTimerRef.current = null;
      if (flowID !== flowRef.current) return;
      const controller = controllerRef.current;
      if (!controller || controller.signal.aborted) return;
      try {
        const result = await commonsAdapter.pollCodexPairing(attemptID, controller.signal);
        if (flowID !== flowRef.current) return;
        if (result.state === "waiting_for_user") {
          dispatch({ type: AUTH_ACTIONS.PAIRING_UPDATE, pairing: { ...stateRef.current.pairing, ...result, state: result.state } });
          schedulePoll(flowID, attemptID, Math.max(1000, result.pollAfterMS || 1500));
          return;
        }
        if (result.state === "needs_profile") {
          dispatch({ type: AUTH_ACTIONS.NEEDS_PROFILE, pairing: { ...stateRef.current.pairing, ...result, state: result.state } });
          return;
        }
        if (result.state === "authenticated") {
          finishAuthenticated(result.session);
          return;
        }
        if (result.state === "cancelled") {
          dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
          return;
        }
        dispatch({
          type: AUTH_ACTIONS.ERROR,
          error: {
            code: result.code || result.state,
            message: result.message || (result.state === "expired" ? "The Codex sign-in window expired." : "Codex sign-in was not completed."),
            resumeState: "unauthenticated",
          },
        });
      } catch (error) {
        if (abortError(error) || flowID !== flowRef.current) return;
        if (error?.code === "auth_poll_wait" || error?.code === "poll_too_soon") {
          schedulePoll(flowID, attemptID, Math.max(1000, (Number(error.retryAfterSeconds) || 1.5) * 1000));
          return;
        }
        dispatch({ type: AUTH_ACTIONS.ERROR, error: { ...authError(error), resumeState: "unauthenticated" } });
      }
    }, Math.max(250, delay));
  }, []);

  const beginPairing = useCallback(async (redirect = currentHash()) => {
    stopFlow();
    const flowID = flowRef.current;
    const controller = new AbortController();
    controllerRef.current = controller;
    dispatch({ type: AUTH_ACTIONS.START_PAIRING, pairing: null, redirect });
    try {
      const status = await commonsAdapter.readCodexStatus(controller.signal);
      if (!status.available) {
        throw new CommonsAPIError("Codex sign-in is unavailable on this Commons installation.", { code: "codex_unavailable", status: 503 });
      }
      const pairing = await commonsAdapter.startCodexPairing(controller.signal);
      if (flowID !== flowRef.current) return null;
      dispatch({ type: AUTH_ACTIONS.START_PAIRING, pairing: { ...pairing, state: "waiting_for_user" }, redirect });
      schedulePoll(flowID, pairing.attemptID, pairing.pollAfterMS || 1500);
      return pairing;
    } catch (error) {
      if (abortError(error) || flowID !== flowRef.current) return null;
      dispatch({ type: AUTH_ACTIONS.ERROR, error: { ...authError(error), resumeState: "unauthenticated" } });
      throw error;
    }
  }, [schedulePoll, stopFlow]);

  const pollNow = useCallback(async () => {
    const pairing = stateRef.current.pairing;
    if (!pairing?.attemptID || stateRef.current.status !== "pairing") return null;
    if (pollTimerRef.current !== null) {
      globalThis.clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    const controller = controllerRef.current;
    if (!controller || controller.signal.aborted) return null;
    try {
      const result = await commonsAdapter.pollCodexPairing(pairing.attemptID, controller.signal);
      if (result.state === "waiting_for_user") {
        dispatch({ type: AUTH_ACTIONS.PAIRING_UPDATE, pairing: { ...pairing, ...result } });
        schedulePoll(flowRef.current, pairing.attemptID, Math.max(1000, result.pollAfterMS || 1500));
      } else if (result.state === "needs_profile") {
        dispatch({ type: AUTH_ACTIONS.NEEDS_PROFILE, pairing: { ...pairing, ...result } });
      } else if (result.state === "authenticated") {
        finishAuthenticated(result.session);
      } else if (result.state === "cancelled") {
        dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
      } else {
        dispatch({ type: AUTH_ACTIONS.ERROR, error: { code: result.code || result.state, message: result.message || "Codex sign-in was not completed.", resumeState: "unauthenticated" } });
      }
      return result;
    } catch (error) {
      if (abortError(error)) return null;
      if (error?.code === "auth_poll_wait" || error?.code === "poll_too_soon") {
        schedulePoll(flowRef.current, pairing.attemptID, Math.max(1000, (Number(error.retryAfterSeconds) || 1.5) * 1000));
        return null;
      }
      dispatch({ type: AUTH_ACTIONS.ERROR, error: { ...authError(error), resumeState: "unauthenticated" } });
      throw error;
    }
  }, [finishAuthenticated, schedulePoll]);

  const submitProfile = useCallback(async (profile) => {
    const pairing = stateRef.current.pairing;
    const profileRetry = stateRef.current.status === "error" && stateRef.current.error?.resumeState === "needs_profile";
    if (!pairing?.attemptID || (stateRef.current.status !== "needs_profile" && !profileRetry)) {
      throw new CommonsAPIError("This Codex setup step is no longer available.", { code: "profile_unavailable", status: 409 });
    }
    if (profileRetry) dispatch({ type: AUTH_ACTIONS.CLEAR_ERROR, resumeState: "needs_profile" });
    dispatch({ type: AUTH_ACTIONS.PROFILE_DRAFT, profileDraft: profile });
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    try {
      const session = await commonsAdapter.completeCodexProfile(pairing.attemptID, profile, controller.signal);
      return finishAuthenticated(session);
    } catch (error) {
      if (abortError(error)) throw error;
      dispatch({ type: AUTH_ACTIONS.ERROR, error: { ...authError(error), resumeState: "needs_profile" } });
      throw error;
    } finally {
      if (controllerRef.current === controller) controllerRef.current = null;
    }
  }, [finishAuthenticated]);

  const cancelPairing = useCallback(async () => {
    const pairing = stateRef.current.pairing;
    stopFlow();
    if (pairing?.attemptID) {
      try {
        await commonsAdapter.cancelCodexPairing(pairing.attemptID, new AbortController().signal);
      } catch (error) {
        if (!abortError(error) && error?.code !== "pairing_not_found") throw error;
      }
    }
    dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
  }, [stopFlow]);

  const accept = useCallback((session) => {
    if (session?.authenticated) return finishAuthenticated(session);
    dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
    return session;
  }, [finishAuthenticated]);

  const expire = useCallback(() => {
    stopFlow();
    dispatch({ type: AUTH_ACTIONS.SIGN_OUT });
  }, [stopFlow]);

  const logout = useCallback(async (signal) => {
    const session = stateRef.current.session;
    if (!session?.authenticated) return session;
    try {
      const next = await commonsAdapter.logout(session.csrfToken, globalThis.crypto?.randomUUID?.() || `logout-${Date.now()}`, signal);
      accept(next);
      return next;
    } catch (error) {
      if (error?.status === 401 || error?.code === "csrf_failed") expire();
      throw error;
    }
  }, [accept, expire]);

  const updateProfile = useCallback(async (profile, signal) => {
    const session = stateRef.current.session;
    if (!session?.authenticated) throw new CommonsAPIError("An authenticated Commons session is required.", { code: "unauthorized", status: 401 });
    try {
      const next = await commonsAdapter.updateProfile(profile, session.profileRevision, session.csrfToken, globalThis.crypto?.randomUUID?.() || `profile-${Date.now()}`, signal);
      return finishAuthenticated(next);
    } catch (error) {
      if (error?.status === 401 || error?.code === "csrf_failed") expire();
      throw error;
    }
  }, [expire, finishAuthenticated]);

  useEffect(() => {
    refresh().catch(() => {});
    return () => stopFlow();
  }, [refresh, stopFlow]);
  const requestProjectHistory = useCallback((archaeologySeed) => {
    if (!archaeologySeed || typeof archaeologySeed !== "object") return;
    projectHistoryRequestRef.current += 1;
    setProjectHistoryRequest({ id: projectHistoryRequestRef.current, archaeologySeed });
  }, []);

  const consumeProjectHistoryRequest = useCallback((requestID) => {
    setProjectHistoryRequest((current) => current?.id === requestID ? null : current);
  }, []);


  const value = useMemo(() => ({
    ...state,
    authState: state.status,
    ready: state.status === "authenticated" || state.status === "unauthenticated",
    refresh,
    beginPairing,
    retryPairing: () => beginPairing(state.redirect || currentHash()),
    pollNow,
    submitProfile,
    cancelPairing,
    accept,
    expire,
    logout,
    updateProfile,
    projectHistoryRequest,
    requestProjectHistory,
    consumeProjectHistoryRequest,
    setProfileDraft(profileDraft) { dispatch({ type: AUTH_ACTIONS.PROFILE_DRAFT, profileDraft }); },
    clearError() { dispatch({ type: AUTH_ACTIONS.CLEAR_ERROR }); },
  }), [state, refresh, beginPairing, pollNow, submitProfile, cancelPairing, accept, expire, logout, updateProfile, projectHistoryRequest, requestProjectHistory, consumeProjectHistoryRequest]);

  return <AuthSessionContext.Provider value={value}>{children}</AuthSessionContext.Provider>;
}

export function useSharedAuthSession() {
  const value = useContext(AuthSessionContext);
  if (!value) throw new Error("useAuthSession must be used inside AuthSessionProvider");
  return value;
}

export { useSharedAuthSession as useAuthSession };
