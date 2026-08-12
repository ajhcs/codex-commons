import { useEffect, useRef, useState } from "react";
import { createIdempotencyKey, isExpiredSession } from "../../components/AuthControls.jsx";
import { commonsAdapter } from "../../data/adapter.js";
import { useAuthSession } from "../../hooks/useAuthSession.js";
import { HistoricalImportPreviewDialog } from "./HistoricalImportPreviewDialog.jsx";
import { ProjectArchaeologyDialog } from "./ProjectArchaeologyDialog.jsx";

function message(error, fallback) {
  return error?.message || fallback;
}

export function ProjectArchaeologyFlow({ open, onClose, onNavigate }) {
  const auth = useAuthSession();
  const controllerRef = useRef(null);
  const [archaeology, setArchaeology] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [previewBridge, setPreviewBridge] = useState(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [applied, setApplied] = useState(null);

  function writeOptions() {
    const csrfToken = auth.session?.csrfToken;
    if (!csrfToken) throw new Error("Sign in to continue Project Archaeology.");
    return { csrfToken, idempotencyKey: createIdempotencyKey() };
  }

  function handleError(next, fallback) {
    if (isExpiredSession(next)) auth.expire();
    setError(message(next, fallback));
  }

  async function refresh() {
    setBusy(true);
    setError("");
    try {
      const next = await commonsAdapter.readProjectArchaeology();
      setArchaeology(next);
      return next;
    } catch (next) {
      handleError(next, "Project Archaeology is unavailable.");
      return null;
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    if (!open || !auth.session?.authenticated) return undefined;
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    setBusy(true);
    setError("");
    commonsAdapter.readProjectArchaeology(controller.signal)
      .then(setArchaeology)
      .catch((next) => { if (next?.name !== "AbortError") handleError(next, "Project Archaeology is unavailable."); })
      .finally(() => { if (controllerRef.current === controller) { controllerRef.current = null; setBusy(false); } });
    return () => controller.abort();
  }, [auth.session?.authenticated, open]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  async function discover() {
    setBusy(true);
    setError("");
    try {
      setArchaeology(await commonsAdapter.discoverProjectArchaeology(writeOptions()));
    } catch (next) {
      handleError(next, "Commons could not check project metadata.");
    } finally {
      setBusy(false);
    }
  }

  async function prepare(config) {
    if (!archaeology) return;
    setBusy(true);
    setError("");
    try {
      const configured = await commonsAdapter.updateProjectArchaeologyConfig(config, archaeology.revision, writeOptions());
      const next = await commonsAdapter.startProjectArchaeology(configured.revision, writeOptions());
      setArchaeology(next);
    } catch (next) {
      handleError(next, "Commons could not prepare the Codex task pack.");
      if (next?.status === 409) await refresh();
    } finally {
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
        busy={busy}
        error={error}
        onDiscover={discover}
        onStart={prepare}
        onPause={() => transition(commonsAdapter.pauseProjectArchaeology, "Commons could not pause this exploration.")}
        onResume={() => transition(commonsAdapter.resumeProjectArchaeology, "Commons could not resume this exploration.")}
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
