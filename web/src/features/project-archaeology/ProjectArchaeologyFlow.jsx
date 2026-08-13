import { useEffect, useRef, useState } from "react";
import { createIdempotencyKey, isExpiredSession } from "../../components/AuthControls.jsx";
import { commonsAdapter } from "../../data/adapter.js";
import { useAuthSession } from "../../hooks/useAuthSession.js";
import { HistoricalImportPreviewDialog } from "./HistoricalImportPreviewDialog.jsx";
import { ProjectArchaeologyDialog } from "./ProjectArchaeologyDialog.jsx";
import { archaeologyConfigCommitReady, IDLE_PROJECT_ARCHAEOLOGY_OPERATIONS, shouldRefreshProjectCatalog } from "./projectArchaeologyState.js";

function message(error, fallback) {
  if (error?.code === "invalid_payload") return "Commons returned project-history data this version cannot safely use.";
  if (error?.code === "unavailable" || error?.status === 503) return "Project history is not available on this Commons installation yet.";
  if (error?.status === 403) return "This Commons account cannot open project history.";
  return error?.message || fallback;
}

export function ProjectArchaeologyFlow({ open, initialArchaeology = null, onClose, onNavigate }) {
  const auth = useAuthSession();
  const controllerRef = useRef(null);
  const pollControllerRef = useRef(null);
  const [archaeology, setArchaeology] = useState(null);
  const [busy, setBusy] = useState(false);
  const [operations, setOperations] = useState(IDLE_PROJECT_ARCHAEOLOGY_OPERATIONS);
  const [refreshingProjects, setRefreshingProjects] = useState(false);
  const [error, setError] = useState("");
  const [previewBridge, setPreviewBridge] = useState(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [applied, setApplied] = useState(null);
  const [launchingCount, setLaunchingCount] = useState(0);
  const [pageVisible, setPageVisible] = useState(() => globalThis.document?.visibilityState !== "hidden");
  const [updateStatus, setUpdateStatus] = useState({ state: "idle", lastCheckedAt: null });

  function writeOptions() {
    const csrfToken = auth.session?.csrfToken;
    if (!csrfToken) throw new Error("Sign in to continue Project Archaeology.");
    return { csrfToken, idempotencyKey: createIdempotencyKey() };
  }

  function setOperation(name, active) {
    setOperations((current) => current[name] === active ? current : { ...current, [name]: active });
  }

  function handleError(next, fallback) {
    if (isExpiredSession(next)) auth.expire();
    setError(message(next, fallback));
  }

  async function refresh({ preserveError = false } = {}) {
    const controller = new AbortController();
    pollControllerRef.current?.abort();
    pollControllerRef.current = controller;
    setOperation("backgroundRead", true);
    setBusy(true);
    if (!preserveError) setError("");
    setUpdateStatus((current) => ({ ...current, state: "checking" }));
    try {
      const next = await commonsAdapter.readProjectArchaeology(controller.signal);
      setArchaeology(next);
      setUpdateStatus({ state: "restored", lastCheckedAt: new Date() });
      return next;
    } catch (next) {
      if (next?.name === "AbortError") return null;
      handleError(next, "Project Archaeology is unavailable.");
      setUpdateStatus((current) => ({ ...current, state: "paused" }));
      return null;
    } finally {
      if (pollControllerRef.current === controller) pollControllerRef.current = null;
      setOperation("backgroundRead", false);
      setBusy(false);
    }
  }

  useEffect(() => {
    const documentObject = globalThis.document;
    if (!documentObject?.addEventListener) return undefined;
    const syncVisibility = () => {
      const visible = documentObject.visibilityState !== "hidden";
      setPageVisible(visible);
      if (!visible) {
        pollControllerRef.current?.abort();
        setOperation("lifecyclePolling", false);
        setUpdateStatus((current) => ({ ...current, state: "hidden" }));
      } else {
        setUpdateStatus((current) => current.state === "hidden" ? { ...current, state: "checking" } : current);
      }
    };
    documentObject.addEventListener("visibilitychange", syncVisibility);
    return () => documentObject.removeEventListener("visibilitychange", syncVisibility);
  }, []);

  useEffect(() => {
    if (!open || !auth.session?.authenticated) return undefined;
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    setOperation("backgroundRead", true);
    setArchaeology(null);
    setBusy(true);
    setError("");
    async function loadCurrentCatalog() {
      const current = initialArchaeology || await commonsAdapter.readProjectArchaeology(controller.signal);
      if (controller.signal.aborted) return;
      setArchaeology(current);
      setOperation("backgroundRead", false);
      setUpdateStatus({ state: "restored", lastCheckedAt: new Date() });
      if (!shouldRefreshProjectCatalog(current)) return;
      const csrfToken = auth.session?.csrfToken;
      if (!csrfToken) return;
      setOperation("catalogRefresh", true);
      setRefreshingProjects(true);
      setArchaeology({ ...current, discovery: { ...current.discovery, stage: "reading_codex_metadata", startedAt: { iso: new Date().toISOString() } } });
      try {
        const refreshed = await commonsAdapter.discoverProjectArchaeology({
          csrfToken,
          idempotencyKey: createIdempotencyKey(),
        }, controller.signal);
        if (!controller.signal.aborted) setArchaeology(refreshed);
      } catch (next) {
        if (next?.name !== "AbortError") handleError(next, "Commons could not refresh projects from Codex.");
      } finally {
        if (!controller.signal.aborted) {
          setOperation("catalogRefresh", false);
          setRefreshingProjects(false);
        }
      }
    }
    loadCurrentCatalog()
      .catch((next) => { if (next?.name !== "AbortError") handleError(next, "Project Archaeology is unavailable."); })
      .finally(() => {
        if (controllerRef.current === controller) {
          controllerRef.current = null;
          setOperation("backgroundRead", false);
          setOperation("catalogRefresh", false);
          setBusy(false);
        }
      });
    return () => controller.abort();
  }, [auth.session?.authenticated, auth.session?.csrfToken, initialArchaeology, open]);

  useEffect(() => () => { controllerRef.current?.abort(); pollControllerRef.current?.abort(); }, []);

  useEffect(() => {
    if (!open || !auth.session?.authenticated || !archaeology) return undefined;
    const discoveryActive = archaeology.discovery?.state === "discovering";
    const launchActive = archaeology.handoff?.tasks?.some((task) => task.jobId && ["queued", "starting", "active", "report_ready", "cancel_requested"].includes(task.state));
    if (!discoveryActive && !launchActive) return undefined;
    if (!pageVisible) {
      setUpdateStatus((current) => current.state === "hidden" ? current : { ...current, state: "hidden" });
      return undefined;
    }
    const controller = new AbortController();
    pollControllerRef.current?.abort();
    pollControllerRef.current = controller;
    const onlyWaitingForReports = !discoveryActive && archaeology.handoff?.tasks?.every((task) => ["active", "report_ready", "claimed", "running"].includes(task.state));
    const delay = updateStatus.state === "checking" ? 0 : discoveryActive && archaeology.discovery?.stage === "queued" ? 750 : onlyWaitingForReports ? 5000 : 1500;
    const timer = globalThis.setTimeout(async () => {
      setUpdateStatus((current) => ({ ...current, state: "checking" }));
      setOperation("lifecyclePolling", true);
      try {
        const next = await commonsAdapter.readProjectArchaeology(controller.signal);
        if (!controller.signal.aborted) {
          setArchaeology(next);
          setUpdateStatus({ state: "restored", lastCheckedAt: new Date() });
        }
      } catch (next) {
        if (next?.name !== "AbortError") setUpdateStatus((current) => ({ ...current, state: "paused" }));
      } finally {
        if (pollControllerRef.current === controller) setOperation("lifecyclePolling", false);
      }
    }, delay);
    return () => {
      globalThis.clearTimeout(timer);
      controller.abort();
      if (pollControllerRef.current === controller) {
        pollControllerRef.current = null;
        setOperation("lifecyclePolling", false);
      }
    };
  }, [archaeology, auth.session?.authenticated, open, pageVisible]);

  async function discover() {
    setOperation("catalogRefresh", true);
    setRefreshingProjects(true);
    setArchaeology((current) => current ? { ...current, discovery: { ...current.discovery, stage: "reading_codex_metadata", startedAt: { iso: new Date().toISOString() } } } : current);
    setBusy(true);
    setError("");
    try {
      setArchaeology(await commonsAdapter.discoverProjectArchaeology(writeOptions()));
    } catch (next) {
      handleError(next, "Commons could not check project metadata.");
    } finally {
      setOperation("catalogRefresh", false);
      setRefreshingProjects(false);
      setBusy(false);
    }
  }

  async function prepare(config) {
    if (!archaeology) return;
    setOperation("configCommit", true);
    setLaunchingCount(config.selectedProjectIds.length);
    setBusy(true);
    setError("");
    try {
      const configured = await commonsAdapter.updateProjectArchaeologyConfig(config, archaeology.revision, writeOptions());
      if (!archaeologyConfigCommitReady(config, configured, archaeology.revision)) {
        setArchaeology(configured);
        throw new Error(configured.capabilities?.taskLaunch?.reason || "Commons could not verify the saved project selection for launch.");
      }
      const next = await commonsAdapter.startProjectArchaeology(configured.revision, writeOptions());
      setArchaeology(next);
    } catch (next) {
      handleError(next, "Commons could not start the selected Codex tasks.");
      if (next?.status === 409) await refresh({ preserveError: true });
    } finally {
      setOperation("configCommit", false);
      setLaunchingCount(0);
      setBusy(false);
    }
  }

  async function transition(method, fallback) {
    if (!archaeology) return;
    setBusy(true);
    setError("");
    try {
      setArchaeology(await method(archaeology.revision, writeOptions()));
    } catch (next) {
      handleError(next, fallback);
      if (next?.status === 409) await refresh();
    } finally {
      setBusy(false);
    }
  }

  async function review(outcomeID) {
    setBusy(true);
    setError("");
    try {
      const bridge = await commonsAdapter.previewProjectArchaeologyImport(outcomeID, writeOptions());
      setPreviewBridge(bridge);
      setApplied(null);
      setPreviewOpen(true);
      onClose?.();
    } catch (next) {
      handleError(next, "Commons could not open the canonical import preview.");
    } finally {
      setBusy(false);
    }
  }

  async function apply(confirmation) {
    if (!previewBridge) return;
    setBusy(true);
    setError("");
    try {
      setApplied(await commonsAdapter.applyHistoricalImport(previewBridge, confirmation, writeOptions()));
    } catch (next) {
      handleError(next, "Commons could not apply the reviewed history.");
    } finally {
      setBusy(false);
    }
  }

  function closePreview() {
    setPreviewOpen(false);
    setPreviewBridge(null);
    setApplied(null);
    setError("");
  }

  function openProject(projectID) {
    closePreview();
    onNavigate?.("project", projectID);
  }

  return (
    <>
      <ProjectArchaeologyDialog
        open={open}
        identity={auth.session?.principal}
        archaeology={archaeology}
        operations={operations}
        busy={busy}
        launchingCount={launchingCount}
        refreshingProjects={refreshingProjects || archaeology?.discovery?.state === "discovering"}
        updateStatus={updateStatus}
        error={error}
        onDiscover={discover}
        onStart={prepare}
        onCancel={() => transition(commonsAdapter.cancelProjectArchaeology, "Commons could not cancel this exploration.")}
        onRefresh={refresh}
        onReview={review}
        onSkip={onClose}
        onClose={onClose}
      />
      <HistoricalImportPreviewDialog open={previewOpen} bridge={previewBridge} busy={busy} error={error} applied={applied} onConfirm={apply} onClose={closePreview} onOpenProject={openProject} />
    </>
  );
}
