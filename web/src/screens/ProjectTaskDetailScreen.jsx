import { useEffect, useRef, useState } from "react";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { TaskEditorDialog, TaskStateDialog } from "../components/TaskManagementDialogs.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useResource } from "../hooks/useResource.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { InlineState, TaskState } from "../components/ProjectParts.jsx";
import { Timestamp } from "../components/Controls.jsx";
import { ContributorProvenance, ProvenanceDisclosure } from "../components/Provenance.jsx";

export function ProjectTaskDetailScreen({ projectInfo, taskID, onBack, onOpenTask }) {
  const [refreshKey, setRefreshKey] = useState(0);
  const [editorOpen, setEditorOpen] = useState(false);
  const [stateEditorOpen, setStateEditorOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const resumeRef = useRef(null);
  const auth = useAuthSession();
  const resource = useResource(async (signal) => {
    const [opened, milestones] = await Promise.all([
      commonsAdapter.readTask(taskID, 20, signal),
      commonsAdapter.readProjectMilestones(projectInfo.project.id, 100, signal),
    ]);
    return { ...opened, milestones };
  }, [taskID, projectInfo.project.id, refreshKey]);
  const [events, setEvents] = useState([]);
  const [nextCursor, setNextCursor] = useState("");
  const [eventsStatus, setEventsStatus] = useState({ loading: false, error: "" });
  const controllerRef = useRef(null);

  useEffect(() => {
    if (!resource.data) return;
    setEvents(resource.data.events);
    setNextCursor(resource.data.eventsNextCursor || "");
    setEventsStatus({ loading: false, error: "" });
  }, [resource.data]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  async function loadOlderEvents() {
    if (!nextCursor || eventsStatus.loading) return;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setEventsStatus({ loading: true, error: "" });
    try {
      const page = await commonsAdapter.readTaskEvents(taskID, { cursor: nextCursor, limit: 20 }, controller.signal);
      setEvents((current) => {
        const known = new Set(current.map((event) => event.id));
        return [...current, ...page.items.filter((event) => !known.has(event.id))];
      });
      setNextCursor(page.nextCursor);
      setEventsStatus({ loading: false, error: "" });
    } catch (error) {
      if (error.name !== "AbortError") setEventsStatus({ loading: false, error: error.message });
    }
  }

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function startAuthenticated(open) {
    if (auth.session?.authenticated) open();
    else requestAuth(open);
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    const resume = resumeRef.current;
    resumeRef.current = null;
    resume?.();
  }

  if (!resource.data) return <InlineState status={resource.status} error={resource.error} />;
  const { task } = resource.data;
  const milestone = task.milestone || resource.data.milestones.items.find((item) => item.id === task.milestoneID);
  if (task.projectID !== projectInfo.project.id) return <InlineState status="error" error="This task does not belong to the selected project." />;

  return (
    <article className="task-detail">
      <div className="detail-command-row">
        <button className="detail-back" type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to tasks</button>
        <div><button className="secondary-button" type="button" onClick={() => startAuthenticated(() => setEditorOpen(true))}>Edit task</button><button className="secondary-button" type="button" onClick={() => startAuthenticated(() => setStateEditorOpen(true))}>Change state</button></div>
      </div>
      <header>
        <div className="detail-heading-meta"><TaskState state={task.state} /><span>P{task.priority}</span><span>Revision {task.revision}</span></div>
        <h2>{task.title}</h2>
        <p className="canonical-ref">{task.id}</p>
      </header>
      <div className="task-detail-grid">
        <div className="task-detail-main">
          <section><h3>Description</h3><p>{task.description || "No description has been recorded."}</p></section>
          <section className="acceptance-block"><h3>Acceptance</h3><p>{task.acceptance || "No acceptance criteria have been recorded."}</p></section>
          <section aria-labelledby="dependencies-title">
            <h3 id="dependencies-title">Dependencies</h3>
            {task.dependencies.length ? (
              <div className="dependency-list">
                {task.dependencies.map((dependency) => (
                  <button key={dependency.id} type="button" onClick={() => onOpenTask(dependency.id)}>
                    <TaskState state={dependency.state} /><span><strong>{dependency.title}</strong><small>{dependency.id}</small></span>
                  </button>
                ))}
              </div>
            ) : <p className="muted">No dependencies.</p>}
            {task.dependenciesTruncated ? <p className="bounded-note">Only the first 20 dependency records are shown.</p> : null}
          </section>
        </div>
        <aside className="task-facts" aria-label="Task facts">
          <dl>
            <div><dt>Milestone</dt><dd>{milestone ? <><strong>{milestone.title}</strong><small>{milestone.status} · {milestone.id}</small></> : task.milestoneID ? <><strong>Milestone unavailable</strong><small>{task.milestoneID}</small></> : "Unscheduled"}</dd></div>
            <div><dt>Created</dt><dd><Timestamp value={task.created} /></dd></div>
            <div><dt>Updated</dt><dd><Timestamp value={task.updated} /></dd></div>
          </dl>
          <div className="task-provenance-stack">
            <ProvenanceDisclosure provenance={task.ownerProvenance} recorded={task.updated} label="Current claim provenance" />
            <ContributorProvenance contributors={task.contributors} truncated={task.contributorsTruncated} />
          </div>
        </aside>
      </div>
      <section className="task-history" aria-labelledby="task-history-title">
        <div><h3 id="task-history-title">Task history</h3><span>Append-only events</span></div>
        {events.length ? (
          <ol>
            {events.map((event) => (
              <li key={event.id}>
                <span className="history-dot" aria-hidden="true" />
                <div>
                  <strong>{event.summary}</strong>
                  <span>{event.kind.replaceAll("_", " ")} · revision {event.revision} · <Timestamp value={event.created} compact /></span>
                  <ProvenanceDisclosure provenance={event.provenance} recorded={event.created} label="Event provenance" compact />
                </div>
              </li>
            ))}
          </ol>
        ) : <p className="muted">No task events have been recorded.</p>}
        {nextCursor ? <button className="secondary-button" type="button" disabled={eventsStatus.loading} onClick={loadOlderEvents}>{eventsStatus.loading ? "Loading history…" : "Load older history"}</button> : null}
        {eventsStatus.error ? <p className="form-message form-message--error" role="status">{eventsStatus.error}</p> : null}
      </section>
      <TaskEditorDialog
        open={editorOpen}
        projectID={projectInfo.project.id}
        task={task}
        milestones={resource.data.milestones.items}
        session={auth.session}
        onClose={() => setEditorOpen(false)}
        onSaved={() => { setEditorOpen(false); setRefreshKey((value) => value + 1); }}
        onAuthRequired={() => requestAuth(() => setEditorOpen(true))}
      />
      <TaskStateDialog
        open={stateEditorOpen}
        task={task}
        session={auth.session}
        onClose={() => setStateEditorOpen(false)}
        onSaved={() => { setStateEditorOpen(false); setRefreshKey((value) => value + 1); }}
        onAuthRequired={() => requestAuth(() => setStateEditorOpen(true))}
      />
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
    </article>
  );
}
