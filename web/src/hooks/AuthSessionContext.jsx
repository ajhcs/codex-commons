import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";

const AuthSessionContext = createContext(null);

export function AuthSessionProvider({ children }) {
  const controllerRef = useRef(null);
  const [state, setState] = useState({ status: "loading", session: null, error: "" });

  const refresh = useCallback(async () => {
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setState((current) => ({ ...current, status: "loading", error: "" }));
    try {
      const session = await commonsAdapter.readSession(controller.signal);
      setState({ status: "ready", session, error: "" });
      return session;
    } catch (error) {
      if (error.name === "AbortError") throw error;
      setState({ status: "error", session: null, error: error.message });
      throw error;
    }
  }, []);

  useEffect(() => {
    refresh().catch(() => {});
    return () => controllerRef.current?.abort();
  }, [refresh]);

  const value = useMemo(() => ({
    ...state,
    refresh,
    accept(session) { setState({ status: "ready", session, error: "" }); },
    expire() { setState({ status: "ready", session: { authenticated: false, principal: null, csrfToken: "" }, error: "" }); },
  }), [state, refresh]);

  return <AuthSessionContext.Provider value={value}>{children}</AuthSessionContext.Provider>;
}

export function useSharedAuthSession() {
  const value = useContext(AuthSessionContext);
  if (!value) throw new Error("useAuthSession must be used inside AuthSessionProvider");
  return value;
}
