import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { HistoricalImportPreviewDialog } from "./HistoricalImportPreviewDialog.jsx";
import { ProjectArchaeologyDialog } from "./ProjectArchaeologyDialog.jsx";
import {
  archaeologyIdentityFixture,
  archaeologyReadyFixture,
  archaeologyHandoffFixture,
  archaeologyAttentionFixture,
  archaeologyCanceledFixture,
  archaeologyLegacyFixture,
  archaeologyReviewFixture,
} from "./projectArchaeologyFixtures.js";
import "../../styles.css";

const importBridge = {
  projectId: "codex-commons",
  request: { sourceDigest: `sha256:${"c".repeat(64)}` },
  preview: {
    batchId: "Codex Commons history",
    sourceDigest: `sha256:${"c".repeat(64)}`,
    manifestDigest: `sha256:${"d".repeat(64)}`,
    counts: { tasks: 18, projectThreadAliases: 6, attributions: 22, events: 41 },
  },
};

const initialStates = {
  intro: { ...archaeologyReadyFixture, id: "", discovery: { state: "idle", metadataOnly: true, candidates: [] } },
  discovering: { ...archaeologyReadyFixture, discovery: { state: "discovering", metadataOnly: true, candidates: [] } },
  ready: archaeologyReadyFixture,
  refreshing: {
    ...archaeologyReadyFixture,
    revision: 8,
    discovery: {
      ...archaeologyReadyFixture.discovery,
      stage: "reading_codex_metadata",
      startedAt: { iso: new Date().toISOString() },
    },
    config: { ...archaeologyReadyFixture.config, selectedProjectIds: [] },
  },
  handoff: archaeologyHandoffFixture,
  attention: archaeologyAttentionFixture,
  canceled: archaeologyCanceledFixture,
  legacy: archaeologyLegacyFixture,
  review: archaeologyReviewFixture,
};

function Storyboard() {
  const requested = new URLSearchParams(window.location.search).get("state") || "ready";
  const [model, setModel] = useState(initialStates[requested] || archaeologyReadyFixture);
  const [open, setOpen] = useState(requested !== "preview");
  const [previewOpen, setPreviewOpen] = useState(requested === "preview");
  const [busy, setBusy] = useState(requested === "refreshing");
  const [refreshingProjects, setRefreshingProjects] = useState(requested === "refreshing");

  function finishRefresh() {
    setModel((current) => ({
      ...current,
      revision: (current?.revision || 0) + 1,
      discovery: {
        ...current.discovery,
        state: "ready",
        stage: "ready",
        updatedAt: { iso: "2026-08-12T12:44:00.000Z", relative: "Just now", absolute: "Aug 12, 12:44 PM" },
      },
    }));
    setRefreshingProjects(false);
    setBusy(false);
  }

  function briefly(action) {
    setBusy(true);
    window.setTimeout(() => { action(); setBusy(false); }, 300);
  }

  return (
    <main className="storyboard-canvas">
      <h1>Project Archaeology storyboard</h1>
      <p>Use the state controls to inspect the optional post-auth sequence.</p>
      <nav aria-label="Storyboard states">
        {Object.entries(initialStates).map(([key, value]) => <button key={key} type="button" onClick={() => { setModel(value); setRefreshingProjects(key === "refreshing"); setBusy(key === "refreshing"); setOpen(true); }}>{key}</button>)}
        <button type="button" onClick={() => { setOpen(false); setPreviewOpen(true); }}>preview</button>
        {refreshingProjects ? <button type="button" onClick={finishRefresh}>finish refresh</button> : null}
      </nav>
      {!open ? <button className="primary-button" type="button" onClick={() => setOpen(true)}>Reopen storyboard</button> : null}
      <ProjectArchaeologyDialog
        open={open}
        identity={archaeologyIdentityFixture}
        archaeology={model}
        busy={busy}
        refreshingProjects={refreshingProjects}
        updateStatus={{ state: "restored", lastCheckedAt: new Date("2026-08-12T12:42:00Z") }}
        onDiscover={() => briefly(() => setModel((current) => ({ ...archaeologyReadyFixture, revision: (current?.revision || 0) + 1 })))}
        onStart={() => briefly(() => setModel(archaeologyHandoffFixture))}
        onRefresh={() => briefly(() => setModel(archaeologyReviewFixture))}
        onCancel={() => briefly(() => setModel(archaeologyCanceledFixture))}
        onReview={() => briefly(() => setOpen(false))}
        onSkip={() => setOpen(false)}
        onClose={() => setOpen(false)}
      />
      <HistoricalImportPreviewDialog
        open={previewOpen}
        bridge={importBridge}
        onConfirm={() => setPreviewOpen(false)}
        onClose={() => setPreviewOpen(false)}
      />
    </main>
  );
}

createRoot(document.getElementById("root")).render(<React.StrictMode><Storyboard /></React.StrictMode>);
