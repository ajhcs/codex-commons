const sourceRecords = {
  "codex-control-plane": {
    kind: "Decision post · dogfood source key",
    title: "Codex remains the conversation and control plane",
    summary: "People manage chats and direct requests in Codex. Commons holds shared durable context across Posts, Wiki, Roadmap, Tasks, and agent discovery.",
    facts: [
      ["Source", "dogfood/codex-commons/manifest.json · posts[codex-control-plane]"],
      ["Basis", "Separating conversation from durable coordination avoids recreating messaging and assignment infrastructure."],
      ["Existing state", "Recorded decision; the current dogfood experience has produced new counter-evidence worth reviewing."],
      ["Provenance", "codex-session:019fe855-b3d0-7eb1-8451-42750efd4fcd"],
      ["Reachability", "Historical provenance only — not live presence, assignment, or a chat control."],
    ],
  },
  "evaluate-human-workflow": {
    kind: "Task · dogfood source key",
    title: "Evaluate Codex Commons through real work",
    summary: "Use this project rather than demo records and capture friction only when it changes trust, comprehension, or the next action.",
    facts: [
      ["Source", "dogfood/codex-commons/manifest.json · tasks[evaluate-human-workflow]"],
      ["State", "Ready · active milestone: Dogfood one real project"],
      ["Acceptance", "Use a real decision or question; record one helpful moment and one concrete friction point or an explicit no-friction result."],
      ["Dependency effect", "Specify the minimum Codex bridge contract remains blocked until this evaluation is resolved."],
    ],
  },
  "history-v1": {
    kind: "Historical import preview · reviewed batch",
    title: "Codex Commons historical reconstruction v1",
    summary: "A deterministic review-only batch accounts for 20 outcomes and 41 sessions. Applying it would append durable historical task provenance.",
    facts: [
      ["Source", "historical-import/manifests/codex-commons.v1.json"],
      ["Batch", "codex-commons-history-v1 · current_wins"],
      ["Preview", "20 tasks planned; 37 task-session links; 4 project aliases; 13 events; zero blockers in the checked-in snapshot."],
      ["Source digest", "sha256:e51d4934aa11bde2f23e64096ab57e19642e38c1de6e1c04f9f7a93acc37e8f9"],
      ["Authority", "Human-only apply with fresh authenticated preview, CSRF, idempotency key, and exact digest confirmation."],
    ],
  },
  "dogfood-before-bridge": {
    kind: "Decision post · dogfood source key",
    title: "Dogfood one real project before widening the bridge",
    summary: "Codex Commons itself is the reference project; its initial corpus stays small and real so the bridge follows observed use.",
    facts: [
      ["Source", "dogfood/codex-commons/manifest.json · posts[dogfood-before-bridge]"],
      ["Outcome", "Dogfood corpus and Project Core surfaces established before bridge implementation."],
      ["Durable result", "The decision remains useful; later work can reference it without reopening the gate."],
    ],
  },
};

const gates = [
  {
    id: "GATE-BOUNDARY",
    state: "open",
    source: "codex-control-plane",
    sourceLabel: "Decision post",
    since: "New evidence today",
    title: "Decide how far project collaboration moves into Commons",
    preview: "Dogfooding suggests Commons may own more thoughtful project collaboration without becoming direct messaging.",
    question: "Which boundary should guide the minimum Codex bridge?",
    description: "The current decision says Codex is both conversation and control plane. Real use now suggests Commons could become the durable collaboration surface for larger projects while Codex remains the live execution plane. This changes bridge scope and deserves a recorded human decision—not an incidental chat answer.",
    route: {
      trigger: "Direct human counter-evidence conflicts with a durable product decision.",
      threshold: "High confidence: verbatim human feedback plus an existing resolved decision.",
      timing: "At the bridge design boundary; no active execution is blocked this minute.",
      audience: "Human judgment, then public project decision. No individual agent needs a message.",
      budget: "Current task context + one exact source open; no feed scan or recurring check.",
      cooldown: "One open gate for this source; do not re-surface without new evidence.",
    },
    choices: [
      { id: "preserve", title: "Preserve the current boundary", copy: "Codex remains conversation and control; Commons remains durable memory and coordination." },
      { id: "expand", title: "Expand Commons for larger-project collaboration", copy: "Codex remains live execution; Commons gains bounded, directed human attention without DMs or agent assignment." },
      { id: "pilot", title: "Defer the boundary until a bridge pilot", copy: "Keep the existing decision and collect one measured pilot before superseding it." },
    ],
    operations: {
      preserve: ["Add an Answer comment with the human rationale", "Keep the existing decision resolved", "Update the bridge task acceptance to preserve the boundary"],
      expand: ["Publish a new Decision post with the human rationale", "Supersede the existing decision with the new Post ID", "Update Product model Wiki and bridge task acceptance"],
      pilot: ["Add a Clarify comment describing the missing evidence", "Keep the current decision authoritative", "Add the bounded pilot condition to the bridge task"],
    },
  },
  {
    id: "GATE-METHOD",
    state: "open",
    source: "evaluate-human-workflow",
    sourceLabel: "Task",
    since: "Milestone boundary",
    title: "Choose one human-attention method for the bridge pilot",
    preview: "Three isolated concepts now need one bounded pilot choice before the bridge contract can become concrete.",
    question: "Which attention method should the pilot measure first?",
    description: "The dogfood task exists specifically to learn which surface earns its place. Once three concepts are visually reviewed, choosing one changes the bridge task's next action. Until then, implementation should stay blocked rather than silently combine all three.",
    route: {
      trigger: "A planned milestone requires one mutually exclusive pilot direction.",
      threshold: "Evidence is sufficient only after all three prototypes pass visual and contract review.",
      timing: "Surface once when the prototypes are ready; never poll or pre-empt the review.",
      audience: "Human choice with a project-wide consequence.",
      budget: "Three short concept receipts; full prototype opens are human-directed.",
      cooldown: "No repeat until a prototype materially changes.",
    },
    choices: [
      { id: "slips", title: "Source-linked attention slips", copy: "Continuous but quiet reference-only routing for isolated moments needing judgment." },
      { id: "hours", title: "Project Office Hours", copy: "Batch a few unresolved sources into a deliberate human review session." },
      { id: "gates", title: "Proposal and decision gates", copy: "Surface only consequential, milestone-blocking choices with an explicit durable receipt." },
    ],
    operations: {
      slips: ["Add an Answer comment to the evaluation post", "Update the evaluation task with the chosen pilot", "Keep the bridge task blocked until pilot acceptance is met"],
      hours: ["Add an Answer comment to the evaluation post", "Update the evaluation task with the chosen pilot", "Keep the bridge task blocked until pilot acceptance is met"],
      gates: ["Add an Answer comment to the evaluation post", "Update the evaluation task with the chosen pilot", "Keep the bridge task blocked until pilot acceptance is met"],
    },
  },
  {
    id: "GATE-HISTORY",
    state: "open",
    source: "history-v1",
    sourceLabel: "Import preview",
    since: "Explicit approval required",
    title: "Approve or defer the append-only historical continuity import",
    preview: "The reviewed 20-outcome batch is eligible, but a fresh server preview and exact digest confirmation remain mandatory.",
    question: "Should the reviewed historical batch proceed to a fresh authenticated server preview?",
    description: "The offline reconstruction is complete and verified. Moving toward apply is append-only and affects durable task history, so it requires explicit human authority. This gate cannot apply anything; it can only record the decision to request a fresh preview or defer.",
    route: {
      trigger: "A verified append-only batch reached its explicit human authority boundary.",
      threshold: "Deterministic checks pass; source accounting and privacy scan are complete.",
      timing: "After verification, before any network or database mutation.",
      audience: "Human authority only; agents cannot approve or apply.",
      budget: "Read checked-in receipt metadata; open full manifest only on request.",
      cooldown: "Do not re-surface unless digest, preview result, or human instruction changes.",
    },
    choices: [
      { id: "preview", title: "Request a fresh server preview", copy: "Permit only a read-only authenticated preview against current canonical tasks." },
      { id: "defer", title: "Defer the import", copy: "Keep the checked-in batch dormant and make no external call." },
      { id: "revise", title: "Return the batch for revision", copy: "Record a challenge against the reviewed source or outcome scope before another preview." },
    ],
    operations: {
      preview: ["Add an Answer receipt to the existing import review source", "Authorize one fresh read-only authenticated server preview", "Require a second explicit approval before apply"],
      defer: ["Add a Clarify receipt stating that the batch remains dormant", "Make no network or database call", "Do not re-surface without new human instruction"],
      revise: ["Add a Challenge receipt with the human rationale", "Keep apply unavailable", "Return to offline manifest review"],
    },
  },
  {
    id: "GATE-DOGFOOD",
    state: "completed",
    source: "dogfood-before-bridge",
    sourceLabel: "Decision post",
    since: "Resolved",
    title: "Dogfood one real project before widening the bridge",
    preview: "Completed boundary: the small real corpus now grounds bridge and attention-system choices.",
    question: "Completed decision",
    description: "The project established one real Codex Commons workspace before bridge work expanded. The gate is retained only as a receipt pointing back to the durable source; it is not a new discussion thread.",
    route: {
      trigger: "Human explicitly chose dogfooding before bridge implementation.",
      threshold: "Direct instruction and durable decision source.",
      timing: "Recorded at the product boundary.",
      audience: "Public project memory.",
      budget: "No further routing required.",
      cooldown: "Completed; never re-surface unless superseded by new evidence.",
    },
    outcome: "The decision changed the next action: build and evaluate a real Codex Commons project before widening the bridge.",
  },
];

const state = { filter: "open", selectedID: "GATE-BOUNDARY", staged: null, receipts: new Map() };
const gateList = document.querySelector("#gateList");
const gateDetail = document.querySelector("#gateDetail");
const sourceDialog = document.querySelector("#sourceDialog");
const policyDialog = document.querySelector("#policyDialog");

function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]);
}

function filteredGates() {
  return gates.filter((gate) => gate.state === state.filter);
}

function renderCounts() {
  const open = gates.filter((gate) => gate.state === "open").length;
  const completed = gates.filter((gate) => gate.state === "completed").length;
  document.querySelector("#openCount").textContent = open;
  document.querySelector("#completedCount").textContent = completed;
  document.querySelector("#railOpenCount").textContent = open;
}

function renderList() {
  const visible = filteredGates();
  if (!visible.some((gate) => gate.id === state.selectedID)) state.selectedID = visible[0]?.id || "";
  gateList.innerHTML = visible.length ? visible.map((gate) => `
    <article class="gate-row ${gate.id === state.selectedID ? "is-selected" : ""} ${gate.state === "completed" ? "is-completed" : ""}">
      <button type="button" data-gate="${gate.id}" aria-current="${gate.id === state.selectedID ? "true" : "false"}">
        <div class="gate-row-meta"><strong>${gate.state === "completed" ? "Completed" : "Needs a decision"}</strong><span>${escapeHTML(gate.since)}</span></div>
        <h2>${escapeHTML(gate.title)}</h2>
        <p>${escapeHTML(gate.preview)}</p>
        <footer><span>${escapeHTML(gate.sourceLabel)}</span><span>${escapeHTML(gate.id)}</span></footer>
      </button>
    </article>
  `).join("") : `<p class="empty-index">No ${state.filter} gates.</p>`;
  gateList.querySelectorAll("[data-gate]").forEach((button) => button.addEventListener("click", () => selectGate(button.dataset.gate)));
}

function renderRouting(gate) {
  const entries = [
    ["Trigger", gate.route.trigger],
    ["Evidence threshold", gate.route.threshold],
    ["Timing", gate.route.timing],
    ["Audience", gate.route.audience],
    ["Context budget", gate.route.budget],
    ["Cooldown", gate.route.cooldown],
  ];
  return `<details class="routing-receipt"><summary>Why Codex routed this here instead of asking in chat</summary><dl class="routing-grid">${entries.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`).join("")}</dl></details>`;
}

function renderDecision(gate) {
  if (gate.state === "completed") return `<section class="completed-summary"><h2>Durable outcome</h2><p>${escapeHTML(gate.outcome)}</p><p>No reply surface remains here. Any future change must begin from the canonical decision source.</p></section>`;
  const staged = state.staged?.gateID === gate.id ? state.staged : null;
  const receipt = state.receipts.get(gate.id);
  if (receipt) return `<section class="completed-summary"><h2>Simulated receipt recorded</h2><p>${escapeHTML(receipt.summary)}</p><div class="success-note">Prototype only. No Commons content changed. In production this reference would disappear from Open after the canonical operations succeed.</div></section>`;
  const choices = gate.choices.map((choice) => `<label class="choice"><input type="radio" name="gate-choice" value="${choice.id}" ${staged?.choice === choice.id ? "checked" : ""}><span><strong>${escapeHTML(choice.title)}</strong><span>${escapeHTML(choice.copy)}</span></span></label>`).join("");
  const operationRows = staged ? gate.operations[staged.choice].map((operation, index) => `<li><strong>${index + 1 === gate.operations[staged.choice].length ? "Final receipt" : "Durable action"}</strong><span>${escapeHTML(operation)}</span></li>`).join("") : "";
  return `
    <section class="decision-section">
      <h2>${escapeHTML(gate.question)}</h2>
      <p>A gate does not accept conversational replies. Choose a direction and give the basis that should be written back to the canonical source.</p>
      <div class="decision-steps" aria-label="Completion steps"><span><strong>1</strong> Choose</span><span><strong>2</strong> State the basis</span><span><strong>3</strong> Review the durable receipt</span></div>
      <div class="choice-list">${choices}</div>
      <label class="rationale"><span>Decision basis</span><textarea id="rationale" placeholder="What evidence or constraint should future agents understand?">${escapeHTML(staged?.rationale || "")}</textarea><small>Required. This becomes an Answer, Clarify, or Challenge on the existing source—not a private message.</small></label>
      <div class="action-row"><button id="previewButton" class="primary-button" type="button" disabled>Preview durable changes</button><span>No write happens in this sandbox.</span></div>
    </section>
    ${staged ? `<section class="receipt-section"><h2>Proposed durable receipt</h2><p>The temporary gate can resolve only after these existing Commons operations succeed.</p><ul class="receipt-list">${operationRows}</ul><div class="action-row"><button id="recordButton" class="primary-button" type="button">Record simulated receipt</button><button id="editButton" class="secondary-button" type="button">Edit choice</button></div></section>` : ""}
  `;
}

function renderDetail() {
  const gate = gates.find((item) => item.id === state.selectedID);
  if (!gate) { gateDetail.innerHTML = `<div class="empty-index">Choose a gate.</div>`; return; }
  gateDetail.innerHTML = `
    <article>
      <header class="detail-header">
        <div class="detail-meta"><strong class="${gate.state === "completed" ? "done" : ""}">${gate.state === "completed" ? "Completed gate" : "Human decision gate"}</strong><span class="mode-boundary">${gate.state === "completed" ? "Durable receipt · not live chat" : "Durable decision · not live chat"}</span><span>${escapeHTML(gate.sourceLabel)}</span><span>${escapeHTML(gate.id)}</span></div>
        <h1>${escapeHTML(gate.title)}</h1>
        <p>${escapeHTML(gate.description)}</p>
        <div class="source-reference"><div><span>Canonical source</span><strong>${escapeHTML(sourceRecords[gate.source].title)}</strong></div><button type="button" data-source="${gate.source}">Open source preview</button></div>
        ${renderRouting(gate)}
      </header>
      ${renderDecision(gate)}
    </article>
  `;
  gateDetail.querySelector("[data-source]")?.addEventListener("click", (event) => openSource(event.currentTarget.dataset.source));
  wireDecision(gate);
}

function wireDecision(gate) {
  if (gate.state !== "open" || state.receipts.has(gate.id)) return;
  const radios = [...gateDetail.querySelectorAll('input[name="gate-choice"]')];
  const rationale = gateDetail.querySelector("#rationale");
  const preview = gateDetail.querySelector("#previewButton");
  const updatePreview = () => {
    const choice = radios.find((radio) => radio.checked)?.value || "";
    preview.disabled = !choice || !rationale.value.trim();
  };
  radios.forEach((radio) => radio.addEventListener("change", updatePreview));
  rationale.addEventListener("input", updatePreview);
  preview.addEventListener("click", () => {
    const choice = radios.find((radio) => radio.checked)?.value;
    state.staged = { gateID: gate.id, choice, rationale: rationale.value.trim() };
    renderDetail();
    gateDetail.querySelector(".receipt-section")?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
  gateDetail.querySelector("#editButton")?.addEventListener("click", () => {
    state.staged = null;
    renderDetail();
  });
  gateDetail.querySelector("#recordButton")?.addEventListener("click", () => {
    const choice = gate.choices.find((item) => item.id === state.staged.choice);
    state.receipts.set(gate.id, { summary: `${choice.title}. Basis: ${state.staged.rationale}` });
    state.staged = null;
    renderDetail();
  });
  updatePreview();
}

function selectGate(id) {
  state.selectedID = id;
  state.staged = null;
  renderList();
  renderDetail();
  if (window.matchMedia("(max-width: 760px)").matches) document.body.classList.add("show-reader");
}

function openSource(id) {
  const source = sourceRecords[id];
  document.querySelector("#sourceKind").textContent = source.kind;
  document.querySelector("#sourceTitle").textContent = source.title;
  document.querySelector("#sourceSummary").textContent = source.summary;
  document.querySelector("#sourceFacts").innerHTML = source.facts.map(([label, value]) => `<div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`).join("");
  sourceDialog.showModal();
}

document.querySelectorAll("[data-filter]").forEach((button) => button.addEventListener("click", () => {
  state.filter = button.dataset.filter;
  state.staged = null;
  document.querySelectorAll("[data-filter]").forEach((item) => item.setAttribute("aria-pressed", String(item === button)));
  renderList();
  renderDetail();
}));
document.querySelector("#policyButton").addEventListener("click", () => policyDialog.showModal());
document.querySelector("#mobileBack").addEventListener("click", () => document.body.classList.remove("show-reader"));

renderCounts();
renderList();
renderDetail();
