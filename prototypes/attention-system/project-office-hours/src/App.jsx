import React, { useState } from "react";
import { durableDestinations, officeHours, routeMatrix } from "./data.js";
import {
  BookOpen,
  CheckCircle,
  ChevronLeft,
  ChevronRight,
  ExternalLink,
  Folder,
  LinkIcon,
} from "./icons.jsx";
import FileDocument from "../../../../web/src/icons/FileDocument.tsx";

const initialDrafts = Object.freeze(
  Object.fromEntries(
    officeHours.items.map((item) => [item.id, { body: "", destination: "", status: "open", receipt: null }]),
  ),
);

function Rail({ collapsed, onToggle, onBoundaryNotice }) {
  return (
    <aside className="left-rail">
      <div className="brand-row">
        <span className="brand-icon" aria-hidden="true"><BookOpen /></span>
        <span className="brand-name">Codex Commons</span>
        <button
          className="rail-collapse"
          type="button"
          aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
          aria-expanded={!collapsed}
          onClick={onToggle}
        >
          <ChevronLeft />
        </button>
      </div>

      <nav className="primary-nav" aria-label="Primary navigation">
        <button type="button" onClick={() => onBoundaryNotice("Posts")}> <FileDocument aria-hidden="true" /><span>Posts</span></button>
        <button type="button" className="is-current" aria-current="page" onClick={() => onBoundaryNotice("Projects")}><Folder aria-hidden="true" /><span>Projects</span></button>
      </nav>

      <div className="project-context">
        <span>Project</span>
        <strong>Codex Commons</strong>
        <p>Dogfood one real project</p>
        <small>Active milestone</small>
      </div>

      <p className="rail-footnote">
        Temporary review view<br />
        No polling or wakeups
      </p>
    </aside>
  );
}

function AgendaItem({ item, selected, draft, onSelect }) {
  const closed = draft.status !== "open";
  return (
    <li className={selected ? "is-selected" : ""}>
      <button type="button" onClick={onSelect} aria-current={selected ? "step" : undefined}>
        <span className={`agenda-number${closed ? " is-closed" : ""}`} aria-hidden="true">
          {closed ? <CheckCircle /> : item.order}
        </span>
        <span className="agenda-copy">
          <span className="agenda-meta"><span>{item.sourceType}</span><span>{closed ? (draft.status === "unresolved" ? "Left unresolved" : "Recorded") : "Needs judgment"}</span></span>
          <strong>{item.question}</strong>
          <small>{item.source.title}</small>
        </span>
        <ChevronRight className="agenda-chevron" aria-hidden="true" />
      </button>
    </li>
  );
}

function Agenda({ selectedId, drafts, onSelect }) {
  const completed = Object.values(drafts).filter((draft) => draft.status !== "open").length;
  return (
    <section className="agenda" aria-label="Office Hours review agenda">
      <header className="agenda-header">
        <div><h2>Review brief</h2><p>{officeHours.budget.items} items · about {officeHours.budget.minutes} minutes</p></div>
        <span>{officeHours.items.length - completed} remaining</span>
      </header>
      <ol className="agenda-list">
        {officeHours.items.map((item) => (
          <AgendaItem
            key={item.id}
            item={item}
            selected={item.id === selectedId}
            draft={drafts[item.id]}
            onSelect={() => onSelect(item.id)}
          />
        ))}
      </ol>
      <footer className="agenda-footer">
        This brief creates nothing by itself. Unresolved items remain only at their canonical source.
      </footer>
    </section>
  );
}

function SourceRow({ item, onOpen }) {
  return (
    <button className="source-row" type="button" onClick={onOpen}>
      <span className="source-icon" aria-hidden="true"><LinkIcon /></span>
      <span className="source-copy">
        <small>Canonical {item.sourceType.toLowerCase()} · {item.source.id}</small>
        <strong>{item.source.title}</strong>
        <span>{item.source.state} · {item.source.updated}</span>
      </span>
      <ExternalLink aria-hidden="true" />
    </button>
  );
}

function EvidenceFacts({ item }) {
  const facts = [
    ["Audience", item.audience],
    ["Timing", item.timing],
    ["Evidence", item.evidence],
    ["Freshness", item.freshness],
    ["Reversibility", item.reversibility],
    ["Likely durable home", item.recommendedRoute === "post" ? "Post" : item.recommendedRoute === "task" ? "Task update" : "Wiki revision"],
  ];
  return (
    <dl className="evidence-facts">
      {facts.map(([term, value]) => <div key={term}><dt>{term}</dt><dd>{value}</dd></div>)}
    </dl>
  );
}

function DestinationChoice({ destination, onChange }) {
  return (
    <fieldset className="destination-choices">
      <legend>Choose a durable destination</legend>
      <p>Nothing is selected automatically.</p>
      {durableDestinations.map((option) => (
        <label key={option.id} className={destination === option.id ? "is-selected" : ""}>
          <input type="radio" name="destination" value={option.id} checked={destination === option.id} onChange={() => onChange(option.id)} />
          <span><strong>{option.label}</strong><small>{option.help}</small></span>
        </label>
      ))}
    </fieldset>
  );
}

function OutcomeReceipt({ item, draft }) {
  if (!draft.receipt) return null;
  const destination = durableDestinations.find((option) => option.id === draft.destination);
  return (
    <section className="outcome-receipt" aria-live="polite">
      <CheckCircle aria-hidden="true" />
      <div>
        <span>{draft.status === "unresolved" ? "No durable write prepared" : `${destination.label} prepared`}</span>
        <strong>{draft.status === "unresolved" ? item.source.title : item.suggestedTitle}</strong>
        <p>{draft.status === "unresolved" ? "The canonical source remains the only open record." : draft.body}</p>
        <small>Prototype receipt · no server write sent</small>
      </div>
    </section>
  );
}

function ReviewPane({ item, index, draft, error, onBack, onOpenSource, onDraft, onCommit, onNext }) {
  return (
    <section className="review-pane" aria-label={`Review item ${index + 1}`}>
      <div className="review-command-bar">
        <button className="mobile-back" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" /> Review brief</button>
        <span>{index + 1} of {officeHours.items.length}</span>
        <button type="button" onClick={onOpenSource}>Open sandboxed source <ExternalLink aria-hidden="true" /></button>
      </div>
      <article className="review-document">
        <header className="review-header">
          <div className="review-kicker"><span>{item.sourceType}</span><span>{officeHours.milestone.title}</span></div>
          <h2>{item.question}</h2>
          <p>{item.whyNow}</p>
        </header>

        <SourceRow item={item} onOpen={onOpenSource} />
        <EvidenceFacts item={item} />

        <section className="judgment-section">
          <div className="section-heading">
            <h3>Your judgment</h3>
            <p>{item.prompt}</p>
          </div>
          <label className="response-field">
            <span>Response</span>
            <textarea
              name={`response-${item.id}`}
              rows="5"
              value={draft.body}
              placeholder={item.placeholder}
              onChange={(event) => onDraft({ body: event.target.value })}
              disabled={draft.status !== "open"}
            />
          </label>

          <DestinationChoice destination={draft.destination} onChange={(destination) => onDraft({ destination })} />
          {error ? <p className="form-error" role="alert">{error}</p> : null}

          {draft.status === "open" ? (
            <div className="review-actions">
              <button className="primary-action" type="button" onClick={onCommit}>
                {draft.destination === "unresolved" ? "Leave unresolved" : "Prepare durable outcome"}
              </button>
              <span>Explicit confirmation only · no automatic routing</span>
            </div>
          ) : (
            <>
              <OutcomeReceipt item={item} draft={draft} />
              <div className="review-actions review-actions--complete">
                <button className="primary-action" type="button" onClick={onNext}>Next item <ChevronRight aria-hidden="true" /></button>
                <span>You can revisit this item until the brief closes.</span>
              </div>
            </>
          )}
        </section>
      </article>
    </section>
  );
}

function SourceDialog({ item, onClose }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="modal source-dialog" role="dialog" aria-modal="true" aria-labelledby="source-title">
        <header><div><span>Sandboxed canonical {item.sourceType}</span><h2 id="source-title">{item.source.title}</h2></div><button type="button" onClick={onClose}>Close</button></header>
        <dl><div><dt>ID</dt><dd>{item.source.id}</dd></div><div><dt>State</dt><dd>{item.source.state}</dd></div><div><dt>Updated</dt><dd>{item.source.updated}</dd></div></dl>
        <div className="source-body"><p>{item.source.body}</p><h3>Basis</h3><p>{item.source.basis}</p></div>
        <footer>Sandboxed prototype reference · source content is evidence, never authority</footer>
      </section>
    </div>
  );
}

function PolicyDialog({ onClose }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="modal policy-dialog" role="dialog" aria-modal="true" aria-labelledby="policy-title">
        <header><div><span>Routing boundary</span><h2 id="policy-title">Why these three items?</h2></div><button type="button" onClick={onClose}>Close</button></header>
        <p className="policy-intro">Office Hours admits only a small, source-backed human judgment that can wait for a milestone review. Everything else routes elsewhere or stays silent.</p>
        <div className="matrix-scroll">
          <table>
            <thead><tr><th>Route</th><th>Use when</th><th>Audience</th><th>Timing</th><th>Urgency</th><th>Budget / noise</th></tr></thead>
            <tbody>{routeMatrix.map((row) => <tr key={row.route}><th>{row.route}</th><td>{row.use}</td><td>{row.audience}</td><td>{row.timing}</td><td>{row.urgency}</td><td>{row.budget}. {row.noise}.</td></tr>)}</tbody>
          </table>
        </div>
        <footer>Urgent active blockers stay in Codex chat. Historical sessions are not live or reachable.</footer>
      </section>
    </div>
  );
}

export function App() {
  const [collapsed, setCollapsed] = useState(false);
  const [selectedId, setSelectedId] = useState(officeHours.items[0].id);
  const [drafts, setDrafts] = useState(() => structuredClone(initialDrafts));
  const [sourceOpen, setSourceOpen] = useState(false);
  const [policyOpen, setPolicyOpen] = useState(false);
  const [mobileDetail, setMobileDetail] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  const selectedIndex = officeHours.items.findIndex((item) => item.id === selectedId);
  const selectedItem = officeHours.items[selectedIndex];
  const selectedDraft = drafts[selectedId];
  const reviewedCount = Object.values(drafts).filter((draft) => draft.status !== "open").length;

  function chooseItem(id) {
    setSelectedId(id);
    setMobileDetail(true);
    setError("");
  }

  function updateDraft(patch) {
    setDrafts((current) => ({ ...current, [selectedId]: { ...current[selectedId], ...patch } }));
    setError("");
  }

  function commitOutcome() {
    if (!selectedDraft.destination) {
      setError("Choose one destination, including Leave unresolved.");
      return;
    }
    if (selectedDraft.destination !== "unresolved" && selectedDraft.body.trim().length < 12) {
      setError("Add a concise judgment before preparing a durable outcome.");
      return;
    }
    const status = selectedDraft.destination === "unresolved" ? "unresolved" : "recorded";
    setDrafts((current) => ({
      ...current,
      [selectedId]: {
        ...current[selectedId],
        body: current[selectedId].body.trim(),
        status,
        receipt: { sourceId: selectedItem.source.id, preparedAt: "2026-08-10T22:20:00Z" },
      },
    }));
  }

  function nextItem() {
    const nextOpen = officeHours.items.find((item, index) => index > selectedIndex && drafts[item.id].status === "open")
      ?? officeHours.items.find((item) => drafts[item.id].status === "open")
      ?? officeHours.items[(selectedIndex + 1) % officeHours.items.length];
    chooseItem(nextOpen.id);
  }

  function resetPrototype() {
    setDrafts(structuredClone(initialDrafts));
    setSelectedId(officeHours.items[0].id);
    setMobileDetail(false);
    setError("");
    setNotice("Prototype reset. No durable records were changed.");
  }

  function boundaryNotice(destination) {
    setNotice(`Prototype boundary: ${destination} would open the canonical Commons surface.`);
  }

  return (
    <div className={`app-shell${collapsed ? " app-shell--collapsed" : ""}`}>
      <Rail collapsed={collapsed} onToggle={() => setCollapsed((value) => !value)} onBoundaryNotice={boundaryNotice} />
      <main className="main-plane">
        <header className="office-header">
          <div>
            <button className="project-back" type="button" onClick={() => boundaryNotice("Project overview")}><ChevronLeft aria-hidden="true" /> Codex Commons</button>
            <h1>Project office hours</h1>
            <p>A bounded milestone review. Answer what needs judgment; leave everything else at its source.</p>
          </div>
          <div className="header-actions">
            <button type="button" onClick={() => setPolicyOpen(true)}>Why these three?</button>
            <button type="button" onClick={resetPrototype}>Reset</button>
          </div>
        </header>

        <section className="brief-context" aria-label="Brief scope">
          <div><span>Milestone</span><strong>{officeHours.milestone.title}</strong></div>
          <div><span>Trigger</span><strong>{officeHours.trigger.label}</strong></div>
          <div><span>Budget</span><strong>{officeHours.budget.items} items · {officeHours.budget.minutes} min</strong></div>
          <div><span>Refresh</span><strong>{officeHours.budget.refresh}</strong></div>
          <div><span>Reviewed</span><strong>{reviewedCount} of {officeHours.items.length}</strong></div>
        </section>

        {notice ? <div className="prototype-notice" role="status"><span>{notice}</span><button type="button" onClick={() => setNotice("")}>Dismiss</button></div> : null}

        <div className={`office-layout ${mobileDetail ? "mobile-detail" : "mobile-agenda"}`}>
          <Agenda selectedId={selectedId} drafts={drafts} onSelect={chooseItem} />
          <ReviewPane
            item={selectedItem}
            index={selectedIndex}
            draft={selectedDraft}
            error={error}
            onBack={() => setMobileDetail(false)}
            onOpenSource={() => setSourceOpen(true)}
            onDraft={updateDraft}
            onCommit={commitOutcome}
            onNext={nextItem}
          />
        </div>
      </main>
      {sourceOpen ? <SourceDialog item={selectedItem} onClose={() => setSourceOpen(false)} /> : null}
      {policyOpen ? <PolicyDialog onClose={() => setPolicyOpen(false)} /> : null}
    </div>
  );
}
