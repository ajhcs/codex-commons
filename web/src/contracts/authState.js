import { AUTH_STATES } from "./commons.js";

export const AUTH_ACTIONS = Object.freeze({
  LOAD: "load",
  SESSION: "session",
  START_PAIRING: "start_pairing",
  PAIRING_UPDATE: "pairing_update",
  NEEDS_PROFILE: "needs_profile",
  AUTHENTICATED: "authenticated",
  SIGN_OUT: "sign_out",
  ERROR: "error",
  CLEAR_ERROR: "clear_error",
  PROFILE_DRAFT: "profile_draft",
});

export const EMPTY_AUTH_SESSION = Object.freeze({
  authenticated: false,
  principal: null,
  csrfToken: "",
  authMethod: "",
  profileRevision: 0,
});

export function initialAuthState() {
  return {
    status: "loading",
    session: null,
    pairing: null,
    error: null,
    profileDraft: { displayName: "", handle: "" },
    redirect: "",
  };
}

function sessionState(session) {
  return session?.authenticated ? "authenticated" : "unauthenticated";
}

function normalizeSession(session) {
  return session?.authenticated ? session : EMPTY_AUTH_SESSION;
}

function stableState(state) {
  return state.status === "error" ? (state.error?.resumeState || "unauthenticated") : state.status;
}

export function authReducer(state, action) {
  switch (action.type) {
    case AUTH_ACTIONS.LOAD:
      return { ...state, status: "loading", error: null };
    case AUTH_ACTIONS.SESSION:
      return {
        ...state,
        status: sessionState(action.session),
        session: normalizeSession(action.session),
        pairing: null,
        error: null,
      };
    case AUTH_ACTIONS.START_PAIRING:
      return {
        ...state,
        status: "pairing",
        pairing: action.pairing || state.pairing,
        error: null,
        redirect: action.redirect ?? state.redirect,
      };
    case AUTH_ACTIONS.PAIRING_UPDATE:
      return {
        ...state,
        status: action.pairing?.state === "needs_profile" ? "needs_profile" : "pairing",
        pairing: action.pairing || state.pairing,
        error: null,
      };
    case AUTH_ACTIONS.NEEDS_PROFILE:
      return {
        ...state,
        status: "needs_profile",
        pairing: action.pairing || state.pairing,
        error: null,
        profileDraft: action.profileDraft || state.profileDraft,
      };
    case AUTH_ACTIONS.AUTHENTICATED:
      return {
        ...state,
        status: "authenticated",
        session: normalizeSession(action.session),
        pairing: null,
        error: null,
        redirect: action.redirect ?? "",
      };
    case AUTH_ACTIONS.SIGN_OUT:
      return {
        ...state,
        status: "unauthenticated",
        session: EMPTY_AUTH_SESSION,
        pairing: null,
        error: null,
      };
    case AUTH_ACTIONS.ERROR:
      return {
        ...state,
        status: "error",
        error: {
          code: action.error?.code || "auth_error",
          message: action.error?.message || "Commons sign-in could not be completed.",
          resumeState: action.error?.resumeState || stableState(state),
          retryAfterSeconds: Number(action.error?.retryAfterSeconds) || 0,
          retryAt: action.error?.retryAt || "",
        },
      };
    case AUTH_ACTIONS.CLEAR_ERROR:
      return {
        ...state,
        status: action.resumeState || stableState(state),
        error: null,
      };
    case AUTH_ACTIONS.PROFILE_DRAFT:
      return {
        ...state,
        profileDraft: action.profileDraft || state.profileDraft,
      };
    default:
      return state;
  }
}

export function isAuthState(value) {
  return AUTH_STATES.includes(value);
}
