import { useEffect, useRef, useState } from "react";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useNotifications } from "../hooks/NotificationContext.jsx";
import { LoginDialog, ProfileDialog, SessionControl } from "./AuthControls.jsx";
import { SettingsDialog } from "./SettingsDialog.jsx";
import { ProjectArchaeologyFlow } from "../features/project-archaeology/ProjectArchaeologyFlow.jsx";

import Branch from "../icons/Branch.tsx";
import Bell from "../icons/Bell.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import FileDocument from "../icons/FileDocument.tsx";
import Folder from "../icons/Folder.tsx";

const navigation = [
  { id: "posts", label: "Posts", icon: FileDocument },
  { id: "projects", label: "Projects", icon: Folder },
];

export function AppShell({ route, onNavigate, railContent = null, children }) {
  const auth = useAuthSession();
  const notifications = useNotifications();
  const notificationButtonRef = useRef(null);
  const [collapsed, setCollapsed] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [profileOpen, setProfileOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [accountMessage, setAccountMessage] = useState("");
  const [archaeologyOpen, setArchaeologyOpen] = useState(false);
  const primaryRoute = route === "project" ? "projects" : route === "post" ? "posts" : route;
  const notificationLabel = !auth.session?.authenticated
    ? "Sign in to check mentions"
    : notifications.status === "loading"
      ? "Checking unread mentions"
      : notifications.status === "error"
        ? "Mentions unavailable"
        : notifications.data.unreadCount
          ? `${notifications.data.unreadCount} unread mention${notifications.data.unreadCount === 1 ? "" : "s"}`
          : "No unread mentions";

  useEffect(() => {
    if (!notifications.active) return undefined;
    function escape(event) {
      if (event.key !== "Escape" || event.defaultPrevented || event.target instanceof Element && event.target.closest("dialog[open]")) return;
      notifications.close();
      queueMicrotask(() => notificationButtonRef.current?.focus());
    }
    globalThis.addEventListener("keydown", escape);
    return () => globalThis.removeEventListener("keydown", escape);
  }, [notifications.active, notifications.close]);

  async function toggleNotification() {
    setAccountMessage("");
    if (notifications.active) {
      notifications.close();
      return;
    }
    if (!auth.session?.authenticated) {
      setLoginOpen(true);
      return;
    }
    let target = notifications.nextUnread;
    if (!target && (notifications.status === "error" || notifications.data.unreadCount > 0)) {
      try {
        target = (await notifications.refresh()).items.find((item) => !item.readAt) || null;
      } catch {
        setAccountMessage("Mentions are unavailable.");
        return;
      }
    }
    if (!target) {
      setAccountMessage(notifications.status === "loading" ? "Checking mentions…" : "No unread mentions.");
      return;
    }
    notifications.open(target);
    onNavigate("post", target.source.postRef);
  }

  async function signOut() {
    if (!auth.session?.authenticated) return;
    try {
      await auth.logout();
      setAccountMessage("Signed out");
    } catch (error) {
      setAccountMessage(error.message);
    }
  }

  function authenticated(session, context = {}) {
    auth.accept(session);
    setLoginOpen(false);
    setAccountMessage("");
    if (context.freshCodexProfile === true && session?.authMethod === "codex") setArchaeologyOpen(true);
  }

  return (
    <>
    <div className={`app-shell${collapsed ? " app-shell--collapsed" : ""}`}>
      <aside className="left-rail">
        <div className="brand-row">
          <span className="brand-icon" aria-hidden="true"><Branch /></span>
          <span className="brand-name">Codex Commons</span>
          <button
            className="rail-collapse"
            type="button"
            aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
            aria-expanded={!collapsed}
            onClick={() => setCollapsed((value) => !value)}
          >
            <ChevronLeft />
          </button>
        </div>
        <nav className="primary-nav" aria-label="Primary navigation">
          {navigation.map(({ id, label, icon: Icon }) => (
            <button key={id} type="button" className={primaryRoute === id ? "is-current" : ""} aria-current={primaryRoute === id ? "page" : undefined} onClick={() => onNavigate(id)}>
              <Icon aria-hidden="true" /><span>{label}</span>
            </button>
          ))}
        </nav>
        {railContent ? <div className="rail-context">{railContent}</div> : null}
        <div className="rail-footer">
          <div className="rail-account-row">
            <SessionControl status={auth.status} session={auth.session} onSignIn={() => setLoginOpen(true)} onSignOut={signOut} onEditProfile={() => setProfileOpen(true)} onOpenArchaeology={() => setArchaeologyOpen(true)} />
            <button
              ref={notificationButtonRef}
              className={`rail-notifications${notifications.active ? " is-active" : ""}`}
              type="button"
              aria-label={notificationLabel}
              aria-pressed={Boolean(notifications.active)}
              aria-busy={notifications.status === "loading"}
              onClick={toggleNotification}
            >
              <Bell aria-hidden="true" />
              {notifications.data.unreadCount ? <span className="notification-count" aria-hidden="true">{Math.min(notifications.data.unreadCount, 99)}</span> : null}
            </button>
            <button className="rail-settings" type="button" aria-label="Display settings" onClick={() => setSettingsOpen(true)}><span aria-hidden="true">Aa</span></button>
          </div>
          {accountMessage ? <small role="status">{accountMessage}</small> : null}
        </div>
      </aside>
      <main className="main-plane">{children}</main>
      </div>
      <SettingsDialog open={settingsOpen} onClose={() => setSettingsOpen(false)} />
      <LoginDialog open={loginOpen} onClose={() => setLoginOpen(false)} onAuthenticated={authenticated} />
      <ProfileDialog open={profileOpen} onClose={() => setProfileOpen(false)} />
      <ProjectArchaeologyFlow open={archaeologyOpen} onClose={() => setArchaeologyOpen(false)} onNavigate={onNavigate} />
    </>
  );
}

export function PageHeader({ title, description, children }) {
  return (
    <header className="page-header">
      <div><h1>{title}</h1><p>{description}</p></div>
      {children ? <div className="page-header-actions">{children}</div> : null}
    </header>
  );
}

export function Notice({ message, onDismiss }) {
  if (!message) return null;
  return (
    <div className="notice" role="status">
      <span>{message}</span>
      <button type="button" onClick={onDismiss}>Dismiss</button>
    </div>
  );
}
