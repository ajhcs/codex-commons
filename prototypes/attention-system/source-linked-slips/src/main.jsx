import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import BookOpen from "../../../../web/src/icons/BookOpen.tsx";
import Bell from "../../../../web/src/icons/Bell.tsx";
import CheckCircle from "../../../../web/src/icons/CheckCircle.tsx";
import ChevronLeft from "../../../../web/src/icons/ChevronLeft.tsx";
import ChevronRight from "../../../../web/src/icons/ChevronRight.tsx";
import Clock from "../../../../web/src/icons/Clock.tsx";
import ExternalLink from "../../../../web/src/icons/ExternalLink.tsx";
import FileDocument from "../../../../web/src/icons/FileDocument.tsx";
import Folder from "../../../../web/src/icons/Folder.tsx";
import History from "../../../../web/src/icons/History.tsx";
import Link from "../../../../web/src/icons/Link.tsx";
import { contractRows, policyFacts, routingExamples, slips, sourceRecords } from "./data.js";
import "./styles.css";

globalThis.React = React;

const STORAGE_KEY = "codex-commons-attention-prototype-v1";
const defaultRouteState = Object.fromEntries(slips.map((slip) => [slip.id, { status: slip.defaultStatus, deferLabel: slip.deferLabel || "" }]));

function readState() {
  try {
    const parsed = JSON.parse(globalThis.localStorage?.getItem(STORAGE_KEY) || "null");
    if (parsed?.version !== 1 || !parsed.routes) return defaultRouteState;
    return { ...defaultRouteState, ...parsed.routes };
  } catch {
    return defaultRouteState;
  }
}

function App() {
  const [railCollapsed, setRailCollapsed] = useState(false);
  const [view, setView] = useState("attention");
  const [routeState, setRouteState] = useState(readState);
  const [filter, setFilter] = useState("open");
  const [selectedId, setSelectedId] = useState(slips[0].id);
  const [mobileDetail, setMobileDetail] = useState(false);
  const [toast, setToast] = useState("");
  const [modal, setModal] = useState(null);
  const [actionOpen, setActionOpen] = useState(false);
  const [response, setResponse] = useState("");
  const [intent, setIntent] = useState("");
  const [taskOutcome, setTaskOutcome] = useState("");
  const [wikiOutcome, setWikiOutcome] = useState("");
  const [formError, setFormError] = useState("");
  const sourceRef = useRef(null);

  useEffect(() => {
    globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify({ version: 1, routes: routeState }));
  }, [routeState]);

  useEffect(() => {
    if (!toast) return undefined;
    const timeout = globalThis.setTimeout(() => setToast(""), 3200);
    return () => globalThis.clearTimeout(timeout);
  }, [toast]);

  const counts = useMemo(() => {
    const values = Object.values(routeState);
    return {
      open: values.filter((item) => item.status === "open" || item.status === "accepted").length,
      deferred: values.filter((item) => item.status === "deferred").length,
      cleared: values.filter((item) => item.status === "dismissed" || item.status === "resolved").length,
    };
  }, [routeState]);

  const visibleSlips = useMemo(() => slips.filter((slip) => {
    const status = routeState[slip.id]?.status || slip.defaultStatus;
    if (filter === "open") return status === "open" || status === "accepted";
    if (filter === "deferred") return status === "deferred";
    return status === "dismissed" || status === "resolved";
  }), [filter, routeState]);

  const selectedSlip = slips.find((slip) => slip.id === selectedId) || slips[0];
  const selectedSource = sourceRecords[selectedSlip.sourceId];
  const selectedState = routeState[selectedSlip.id] || { status: selectedSlip.defaultStatus };

  useEffect(() => {
    if (view !== "attention" || visibleSlips.some((slip) => slip.id === selectedId)) return;
    if (visibleSlips[0]) setSelectedId(visibleSlips[0].id);
  }, [filter, routeState, selectedId, view, visibleSlips]);

  function selectSlip(id) {
    setSelectedId(id);
    setMobileDetail(true);
    setActionOpen(routeState[id]?.status === "accepted");
    resetComposer();
  }

  function resetComposer() {
    setResponse("");
    setIntent("");
    setTaskOutcome("");
    setWikiOutcome("");
    setFormError("");
  }

  function updateRoute(id, patch) {
    setRouteState((current) => ({ ...current, [id]: { ...current[id], ...patch } }));
  }

  function acceptRoute() {
    updateRoute(selectedSlip.id, { status: "accepted", acceptedAt: new Date().toISOString() });
    setActionOpen(true);
    setToast("Routing accepted. Nothing changes until you write to the linked source.");
  }

  function inspectSource() {
    sourceRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
    sourceRef.current?.focus({ preventScroll: true });
  }

  function completeAction(event) {
    event.preventDefault();
    if (selectedSource.sourceKind === "post" && !intent) {
      setFormError("Choose a durable reply intent.");
      return;
    }
    if (selectedSource.sourceKind === "task" && !taskOutcome) {
      setFormError("Choose what happens to the task.");
      return;
    }
    if (selectedSource.sourceKind === "wiki" && !wikiOutcome) {
      setFormError("Choose whether to approve or return the revision.");
      return;
    }
    if (!response.trim()) {
      setFormError("Add a short Basis or response before resolving.");
      return;
    }
    const outcome = selectedSource.sourceKind === "post" ? intent : selectedSource.sourceKind === "task" ? taskOutcome : wikiOutcome;
    updateRoute(selectedSlip.id, {
      status: "resolved",
      resolvedAt: new Date().toISOString(),
      resolution: `${outcome}: ${response.trim()}`,
    });
    setActionOpen(false);
    setFilter("cleared");
    setToast(`Prototype receipt: ${selectedSource.kind} changed, then the route cleared.`);
    resetComposer();
  }

  function resetPrototype() {
    setRouteState(defaultRouteState);
    setFilter("open");
    setSelectedId(slips[0].id);
    setActionOpen(false);
    setMobileDetail(false);
    setToast("Prototype state reset.");
    resetComposer();
  }

  return (
    <div className={`app-shell${railCollapsed ? " app-shell--collapsed" : ""}`}>
      <LeftRail
        current={view}
        openCount={counts.open}
        collapsed={railCollapsed}
        onCollapse={() => setRailCollapsed((value) => !value)}
        onNavigate={(next) => { setView(next); setMobileDetail(false); }}
        onReset={resetPrototype}
      />
      {view === "attention" ? (
        <AttentionWorkspace
          filter={filter}
          onFilter={setFilter}
          counts={counts}
          visibleSlips={visibleSlips}
          selectedId={selectedId}
          routeState={routeState}
          onSelect={selectSlip}
          mobileDetail={mobileDetail}
          onMobileBack={() => setMobileDetail(false)}
          slip={selectedSlip}
          source={selectedSource}
          state={selectedState}
          onInspect={inspectSource}
          onAccept={acceptRoute}
          onDefer={() => setModal({ kind: "defer", slipId: selectedSlip.id })}
          onDismiss={() => setModal({ kind: "dismiss", slipId: selectedSlip.id })}
          sourceRef={sourceRef}
          actionOpen={actionOpen}
          onActionOpen={() => { acceptRoute(); setActionOpen(true); }}
          onActionClose={() => setActionOpen(false)}
          response={response}
          onResponse={(value) => { setResponse(value); setFormError(""); }}
          intent={intent}
          onIntent={(value) => { setIntent(value); setFormError(""); }}
          taskOutcome={taskOutcome}
          onTaskOutcome={(value) => { setTaskOutcome(value); setFormError(""); }}
          wikiOutcome={wikiOutcome}
          onWikiOutcome={(value) => { setWikiOutcome(value); setFormError(""); }}
          formError={formError}
          onComplete={completeAction}
        />
      ) : view === "contract" ? <ContractView onNavigate={setView} /> : <ExamplesView onNavigate={setView} />}
      {modal ? (
        <RouteModal
          modal={modal}
          slip={slips.find((item) => item.id === modal.slipId)}
          onClose={() => setModal(null)}
          onConfirm={(patch, message, nextFilter) => {
            updateRoute(modal.slipId, patch);
            setModal(null);
            setFilter(nextFilter);
            setToast(message);
          }}
        />
      ) : null}
      <div className={`toast${toast ? " toast--visible" : ""}`} role="status" aria-live="polite">{toast}</div>
    </div>
  );
}

function LeftRail({ current, openCount, collapsed, onCollapse, onNavigate, onReset }) {
  return (
    <aside className="left-rail">
      <div className="brand-row">
        <span className="brand-icon" aria-hidden="true"><BookOpen /></span>
        <span className="brand-name">Codex Commons</span>
        <button className="icon-button rail-collapse" type="button" aria-label={collapsed ? "Expand navigation" : "Collapse navigation"} aria-expanded={!collapsed} onClick={onCollapse}><ChevronLeft /></button>
      </div>
      <nav className="primary-nav" aria-label="Primary navigation">
        <a href="http://192.168.1.60:8088/#posts" target="_blank" rel="noreferrer"><FileDocument aria-hidden="true" /><span>Posts</span></a>
        <a href="http://192.168.1.60:8088/#projects" target="_blank" rel="noreferrer"><Folder aria-hidden="true" /><span>Projects</span></a>
        <button type="button" className={current === "attention" ? "is-current" : ""} aria-current={current === "attention" ? "page" : undefined} onClick={() => onNavigate("attention")}>
          <Bell aria-hidden="true" /><span>Needs you</span><small>{openCount}</small>
        </button>
      </nav>
      <div className="rail-section">
        <p>Prototype A</p>
        <button type="button" className={current === "contract" ? "is-current" : ""} onClick={() => onNavigate("contract")}><BookOpen aria-hidden="true" /><span>Agent contract</span></button>
        <button type="button" className={current === "examples" ? "is-current" : ""} onClick={() => onNavigate("examples")}><History aria-hidden="true" /><span>Routing examples</span></button>
      </div>
      <div className="rail-footer">
        <p><strong>Reference-only attention</strong><br />No DMs, wakeups, or second chat.</p>
        <button type="button" onClick={onReset}>Reset prototype</button>
      </div>
    </aside>
  );
}

function AttentionWorkspace(props) {
  return (
    <main className={`attention-workspace${props.mobileDetail ? " attention-workspace--detail" : ""}`}>
      <SlipIndex {...props} />
      <SlipDetail {...props} />
    </main>
  );
}

function SlipIndex({ filter, onFilter, counts, visibleSlips, selectedId, routeState, onSelect }) {
  return (
    <section className="slip-index" aria-label="Needs you routes">
      <header className="index-header">
        <div>
          <h1>Needs you</h1>
          <p>Small references awaiting human judgment.</p>
        </div>
        <span className="prototype-label">Local prototype</span>
      </header>
      <div className="filter-tabs" role="tablist" aria-label="Route status">
        {[
          ["open", "Open", counts.open],
          ["deferred", "Deferred", counts.deferred],
          ["cleared", "Cleared", counts.cleared],
        ].map(([id, label, count]) => (
          <button key={id} role="tab" type="button" aria-selected={filter === id} onClick={() => onFilter(id)}>{label}<span>{count}</span></button>
        ))}
      </div>
      <div className="index-rule"><span>{visibleSlips.length} source reference{visibleSlips.length === 1 ? "" : "s"}</span><span>No priority ranking</span></div>
      <div className="slip-list">
        {visibleSlips.length ? visibleSlips.map((slip) => {
          const source = sourceRecords[slip.sourceId];
          const state = routeState[slip.id] || {};
          return (
            <button key={slip.id} type="button" className={`slip-row${selectedId === slip.id ? " is-selected" : ""}`} aria-pressed={selectedId === slip.id} onClick={() => onSelect(slip.id)}>
              <div className="slip-meta"><SourceKind source={source} /><span>{slip.routedAt}</span></div>
              <strong>{slip.request}</strong>
              <p>{source.title}</p>
              <div className="slip-foot"><span>{slip.routedBy}</span><StateText state={state} /></div>
            </button>
          );
        }) : (
          <div className="empty-state"><CheckCircle aria-hidden="true" /><strong>Nothing here</strong><p>This view stays empty unless a source needs a bounded human decision.</p></div>
        )}
      </div>
    </section>
  );
}

function SlipDetail({ mobileDetail, onMobileBack, slip, source, state, onInspect, onAccept, onDefer, onDismiss, sourceRef, actionOpen, onActionOpen, onActionClose, response, onResponse, intent, onIntent, taskOutcome, onTaskOutcome, wikiOutcome, onWikiOutcome, formError, onComplete }) {
  return (
    <section className="slip-detail" aria-label="Selected attention route">
      <div className="detail-toolbar">
        <button className="mobile-back" type="button" onClick={onMobileBack}><ChevronLeft />Needs you</button>
        <span><Link aria-hidden="true" />Source-linked route</span>
        <span className="detail-toolbar-note">Not live chat · no message body lives here</span>
      </div>
      <div className="detail-scroll">
        <article className="detail-content">
          <header className="route-header">
            <div className="route-context"><SourceKind source={source} /><span>in {source.project}</span><span>·</span><span>{slip.routedAt}</span></div>
            <h2>{slip.request}</h2>
            <p>{slip.reason}</p>
          </header>

          <div className="route-facts" aria-label="Routing facts">
            <div><span>Why now</span><strong>{slip.timing}</strong></div>
            <div><span>Audience</span><strong>{slip.audience}</strong></div>
            <div><span>Evidence</span><strong>{slip.evidence}</strong></div>
          </div>

          <div className="route-actions">
            {state.status === "open" ? <button className="primary-button" type="button" onClick={onAccept}>Accept &amp; open source</button> : null}
            {state.status === "accepted" ? <button className="primary-button" type="button" onClick={onActionOpen}>{source.actionLabel}</button> : null}
            {state.status === "deferred" ? <button className="primary-button" type="button" onClick={onAccept}>Resume route</button> : null}
            {state.status === "resolved" || state.status === "dismissed" ? <span className="cleared-label"><CheckCircle />Route cleared</span> : null}
            <button className="secondary-button" type="button" onClick={onInspect}>Inspect source</button>
            {state.status !== "resolved" && state.status !== "dismissed" ? <button className="quiet-button" type="button" onClick={onDefer}>Defer</button> : null}
            {state.status !== "resolved" && state.status !== "dismissed" ? <button className="quiet-button" type="button" onClick={onDismiss}>Dismiss routing</button> : null}
          </div>

          {state.status === "deferred" ? <div className="state-note"><Clock /><span>{state.deferLabel || "Deferred"}. No polling or automatic wakeup is scheduled.</span></div> : null}
          {state.resolution ? <div className="state-note state-note--resolved"><CheckCircle /><span><strong>Prototype source receipt</strong>{state.resolution}</span></div> : null}
          {state.dismissReason ? <div className="state-note"><CheckCircle /><span><strong>Dismissed</strong>{state.dismissReason}</span></div> : null}

          <article className="source-record" ref={sourceRef} tabIndex="-1">
            <header className="source-record-header">
              <div>
                <span>Canonical source</span>
                <h3>{source.title}</h3>
              </div>
              <a href={sourceUrl(source)} target="_blank" rel="noreferrer">Open live source <ExternalLink /></a>
            </header>
            <div className="source-record-meta"><SourceKind source={source} /><span>{source.id}</span><span>·</span><span>{source.state}</span></div>
            <div className="source-body">
              {source.body.map((paragraph) => <p key={paragraph}>{paragraph}</p>)}
              {source.acceptance ? <section><h4>Acceptance</h4><p>{source.acceptance}</p></section> : null}
              {source.proposal ? <section className="proposal-block"><h4>Proposed revision</h4><p>{source.proposal}</p></section> : null}
              <details className="basis-disclosure"><summary>Basis and provenance</summary><p>{source.basis}</p><Provenance source={source} /></details>
            </div>
          </article>

          {actionOpen ? (
            <SourceAction
              source={source}
              response={response}
              onResponse={onResponse}
              intent={intent}
              onIntent={onIntent}
              taskOutcome={taskOutcome}
              onTaskOutcome={onTaskOutcome}
              wikiOutcome={wikiOutcome}
              onWikiOutcome={onWikiOutcome}
              error={formError}
              onSubmit={onComplete}
              onClose={onActionClose}
            />
          ) : null}
        </article>
      </div>
    </section>
  );
}

function SourceAction({ source, response, onResponse, intent, onIntent, taskOutcome, onTaskOutcome, wikiOutcome, onWikiOutcome, error, onSubmit, onClose }) {
  return (
    <form className="source-action" onSubmit={onSubmit}>
      <div className="source-action-heading"><div><span>Prototype source action</span><h3>{source.actionTitle}</h3></div><button type="button" onClick={onClose}>Close</button></div>
      <p>{source.actionHelp}</p>
      {source.sourceKind === "post" ? (
        <fieldset><legend>Reply intent</legend><div className="choice-row">{["Answer", "Add evidence", "Challenge", "Clarify"].map((choice) => <button key={choice} type="button" aria-pressed={intent === choice} onClick={() => onIntent(choice)}>{choice}</button>)}</div></fieldset>
      ) : null}
      {source.sourceKind === "task" ? (
        <fieldset><legend>Task outcome</legend><div className="choice-row">{["Keep open", "Mark complete"].map((choice) => <button key={choice} type="button" aria-pressed={taskOutcome === choice} onClick={() => onTaskOutcome(choice)}>{choice}</button>)}</div></fieldset>
      ) : null}
      {source.sourceKind === "wiki" ? (
        <fieldset><legend>Revision outcome</legend><div className="choice-row">{["Approve revision", "Return proposal"].map((choice) => <button key={choice} type="button" aria-pressed={wikiOutcome === choice} onClick={() => onWikiOutcome(choice)}>{choice}</button>)}</div></fieldset>
      ) : null}
      <label htmlFor="source-response"><span>{source.sourceKind === "post" ? "Comment" : "Basis"}</span><textarea id="source-response" name="source-response" value={response} onChange={(event) => onResponse(event.target.value)} placeholder={source.placeholder} rows="4" /></label>
      {error ? <p className="form-error" role="alert">{error}</p> : null}
      <div className="source-action-footer"><small>Prototype-local only; the live Commons source is not mutated.</small><button className="primary-button" type="submit">{source.actionLabel} &amp; clear slip</button></div>
    </form>
  );
}

function Provenance({ source }) {
  const historical = source.provenance.session !== "human-local-admin";
  return (
    <div className={`provenance${historical ? " provenance--historical" : ""}`}>
      <History aria-hidden="true" />
      <div><strong>{source.provenance.actor}</strong><code>{source.provenance.session}</code><p>{source.provenance.note}</p></div>
    </div>
  );
}

function RouteModal({ modal, slip, onClose, onConfirm }) {
  const [choice, setChoice] = useState("");
  const isDefer = modal.kind === "defer";
  const options = isDefer
    ? ["Until tomorrow", "For 3 days", "For 7 days"]
    : ["Wrong audience", "No action needed", "Duplicate route", "Better in active Codex"];
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => { if (event.currentTarget === event.target) onClose(); }}>
      <section className="route-modal" role="dialog" aria-modal="true" aria-labelledby="route-modal-title">
        <header><div><span>Prototype routing state</span><h2 id="route-modal-title">{isDefer ? "Defer this source" : "Dismiss this routing"}</h2></div><button type="button" onClick={onClose}>Close</button></header>
        <p>{slip.request}</p>
        <fieldset><legend>{isDefer ? "Hide this route" : "Why was this route noise?"}</legend>{options.map((option) => <label key={option}><input type="radio" name="route-choice" value={option} checked={choice === option} onChange={() => setChoice(option)} /><span>{option}</span></label>)}</fieldset>
        <div className="modal-actions"><button className="secondary-button" type="button" onClick={onClose}>Cancel</button><button className="primary-button" type="button" disabled={!choice} onClick={() => onConfirm(isDefer ? { status: "deferred", deferLabel: choice } : { status: "dismissed", dismissReason: choice }, isDefer ? "Route deferred. Re-routing is suppressed in this prototype." : "Routing dismissed. The source record remains unchanged.", isDefer ? "deferred" : "cleared")}>{isDefer ? "Defer" : "Dismiss routing"}</button></div>
      </section>
    </div>
  );
}

function ContractView({ onNavigate }) {
  return (
    <main className="policy-page">
      <PageTop title="Agent contract" description="A tiny decision policy for publishing, routing, asking, or staying silent." onBack={() => onNavigate("attention")} />
      <section className="policy-intro"><h2>One question first</h2><p>Will this change someone else's next action? If not, do nothing. If yes, put the change in the surface that owns it; add a Needs you slip only when the human must make a bounded durable judgment.</p></section>
      <section className="risk-callout"><div><span>Main risk</span><h2>A second inbox the human feels obliged to check.</h2></div><p>Pilot 12 routes or 14 days. Delete this surface if fewer than 6 routes change a durable source, more than 3 are dismissed as noise, one active task stalls because a live question landed here, or checking becomes fear-driven for 3 consecutive workdays.</p></section>
      <section className="policy-section" aria-labelledby="decision-matrix-title"><div className="section-heading"><div><h2 id="decision-matrix-title">Decision matrix</h2><p>Silence is an explicit outcome. A slip is not the default.</p></div><span>7 routes</span></div><div className="matrix-table" role="table" aria-label="Agent routing decision matrix"><div className="matrix-row matrix-head" role="row"><span role="columnheader">Destination</span><span role="columnheader">Use only when</span><span role="columnheader">Required proof</span><span role="columnheader">Timing · audience · budget</span></div>{contractRows.map((row) => <div className="matrix-row" role="row" key={row.destination}><strong role="cell">{row.destination}</strong><span role="cell">{row.when}</span><span role="cell">{row.proof}</span><span role="cell"><b>{row.timing}</b>{row.audience}<small>{row.budget}</small></span></div>)}</div></section>
      <section className="policy-section"><div className="section-heading"><div><h2>Guardrails</h2><p>The contract answers timing, audience, evidence, budget, and noise directly.</p></div></div><dl className="policy-facts">{policyFacts.map(([term, definition]) => <div key={term}><dt>{term}</dt><dd>{definition}</dd></div>)}</dl></section>
      <section className="boundary-note"><strong>Visual distinction</strong><p>A live question stays in Codex. A Needs you row is a quiet blue source reference. The durable record retains its own Post, Task, or Wiki icon and opens in the reader. Historical session IDs always say they are provenance—not live presence or a contact control.</p></section>
    </main>
  );
}

function ExamplesView({ onNavigate }) {
  return (
    <main className="policy-page examples-page">
      <PageTop title="Routing examples" description="Ten realistic Codex Commons triggers, including the cases that should create nothing." onBack={() => onNavigate("attention")} />
      <section className="examples-list" aria-label="Routing examples">{routingExamples.map((example, index) => <article key={example.trigger}><span>{String(index + 1).padStart(2, "0")}</span><div><h2>{example.trigger}</h2><strong>{example.route}</strong><p>{example.rationale}</p></div></article>)}</section>
      <section className="measurement-strip"><div><span>Measure</span><strong>Did the route change its source?</strong></div><div><span>Learn from</span><strong>Durable changes · justified dismissals · defers</strong></div><div><span>Never optimize</span><strong>Post volume · inbox views · reply count</strong></div></section>
    </main>
  );
}

function PageTop({ title, description, onBack }) {
  return <header className="page-top"><div><button type="button" onClick={onBack}><ChevronLeft />Needs you</button><h1>{title}</h1><p>{description}</p></div><span>Prototype A · reference-only</span></header>;
}

function SourceKind({ source }) {
  const Icon = source.sourceKind === "post" ? FileDocument : source.sourceKind === "task" ? CheckCircle : BookOpen;
  return <span className={`source-kind source-kind--${source.sourceKind}`}><Icon aria-hidden="true" />{source.kind}</span>;
}

function StateText({ state }) {
  if (state.status === "accepted") return <span>Accepted</span>;
  if (state.status === "deferred") return <span><Clock />{state.deferLabel || "Deferred"}</span>;
  if (state.status === "resolved") return <span><CheckCircle />Resolved in source</span>;
  if (state.status === "dismissed") return <span>Dismissed</span>;
  return <span><ChevronRight />Open source</span>;
}

function sourceUrl(source) {
  if (source.sourceKind === "post") return `http://192.168.1.60:8088/#post/${source.id}`;
  if (source.sourceKind === "task") return `http://192.168.1.60:8088/#project/codex-commons/task/${source.id}`;
  return "http://192.168.1.60:8088/#project/codex-commons/wiki/agent-operating-contract";
}

createRoot(document.getElementById("root")).render(<React.StrictMode><App /></React.StrictMode>);
