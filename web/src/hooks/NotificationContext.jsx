import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";
import { createIdempotencyKey, isExpiredSession } from "../components/AuthControls.jsx";
import { useAuthSession } from "./useAuthSession.js";

const NotificationContext = createContext(null);
const emptyData = { items: [], nextCursor: "", unreadCount: 0 };

export function NotificationProvider({ children }) {
  const auth = useAuthSession();
  const controllerRef = useRef(null);
  const readControllerRef = useRef(null);
  const markingRef = useRef(new Set());
  const [state, setState] = useState({ status: "idle", data: emptyData, error: "" });
  const [active, setActive] = useState(null);
  const principal = auth.session?.authenticated ? auth.session.principal.principal : "";

  const refresh = useCallback(async () => {
    if (!principal) return emptyData;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setState((current) => ({ ...current, status: "loading", error: "" }));
    try {
      const data = await commonsAdapter.readNotifications({ unread: true, cursor: "", limit: 20 }, controller.signal);
      setState({ status: "ready", data, error: "" });
      return data;
    } catch (error) {
      if (error.name === "AbortError") throw error;
      if (isExpiredSession(error)) auth.expire();
      setState((current) => ({ ...current, status: "error", error: error.message }));
      throw error;
    }
  }, [principal]);

  useEffect(() => {
    if (auth.status !== "ready") return undefined;
    if (!principal) {
      controllerRef.current?.abort();
      readControllerRef.current?.abort();
      setState({ status: "idle", data: emptyData, error: "" });
      setActive(null);
      return undefined;
    }
    refresh().catch(() => {});
    return () => controllerRef.current?.abort();
  }, [auth.status, principal, refresh]);

  useEffect(() => () => {
    controllerRef.current?.abort();
    readControllerRef.current?.abort();
  }, []);

  const open = useCallback((notification) => {
    if (!notification) return;
    setActive({ notification, status: "opening", message: "Opening canonical thread…" });
  }, []);

  const close = useCallback(() => setActive(null), []);

  const sourceFailed = useCallback((notificationID, message) => {
    setActive((current) => current?.notification.id === notificationID
      ? { ...current, status: "error", message: message || "The canonical source could not be opened. This mention remains unread." }
      : current);
  }, []);

  const sourceOpened = useCallback(async ({ notificationID, postRef, commentRef = "" }) => {
    const current = active;
    if (!current || current.notification.id !== notificationID
      || current.notification.source.postRef !== postRef
      || (current.notification.source.commentRef || "") !== commentRef
      || markingRef.current.has(notificationID)) return;
    if (current.notification.readAt) {
      setActive((value) => value?.notification.id === notificationID ? { ...value, status: "ready", message: "Opened from notification" } : value);
      return;
    }
    markingRef.current.add(notificationID);
    readControllerRef.current?.abort();
    const controller = new AbortController();
    readControllerRef.current = controller;
    setActive((value) => value?.notification.id === notificationID ? { ...value, status: "marking", message: "Canonical source opened" } : value);
    try {
      await commonsAdapter.markNotificationRead({ id: notificationID }, {
        csrfToken: auth.session?.csrfToken || "",
        idempotencyKey: createIdempotencyKey(),
      }, controller.signal);
      setState((value) => ({
        ...value,
        data: {
          ...value.data,
          unreadCount: Math.max(0, value.data.unreadCount - 1),
          items: value.data.items.map((item) => item.id === notificationID ? { ...item, readAt: { relative: "Just now" } } : item),
        },
      }));
      setActive((value) => value?.notification.id === notificationID
        ? { ...value, notification: { ...value.notification, readAt: { relative: "Just now" } }, status: "ready", message: "Opened from notification" }
        : value);
    } catch (error) {
      if (error.name !== "AbortError") {
        if (isExpiredSession(error)) auth.expire();
        setActive((value) => value?.notification.id === notificationID
          ? { ...value, status: "error", message: "Thread opened, but the mention could not be marked read." }
          : value);
      }
    } finally {
      markingRef.current.delete(notificationID);
    }
  }, [active, auth.session?.csrfToken]);

  const value = useMemo(() => ({
    ...state,
    active,
    nextUnread: state.data.items.find((item) => !item.readAt) || null,
    refresh,
    open,
    close,
    sourceOpened,
    sourceFailed,
  }), [state, active, refresh, open, close, sourceOpened, sourceFailed]);

  return <NotificationContext.Provider value={value}>{children}</NotificationContext.Provider>;
}

export function useNotifications() {
  const value = useContext(NotificationContext);
  if (!value) throw new Error("useNotifications must be used inside NotificationProvider");
  return value;
}
