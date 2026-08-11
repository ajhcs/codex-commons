import { useEffect, useMemo, useRef, useState } from "react";
import Plus from "../icons/Plus.tsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { TaskEditorDialog } from "../components/TaskManagementDialogs.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useResource } from "../hooks/useResource.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { InlineState, ProgressMeter, TaskState, taskStateLabels } from "../components/ProjectParts.jsx";
import { Select, Timestamp } from "../components/Controls.jsx";

const views = [
  { id: "list", label: "List" },
  { id: "board", label: "Kanban" },
  { id: "roadmap", label: "Roadmap" },
];

const boardStates = ["ready", "in_progress", "blocked", "done"];
const pageLimit = 25;

export function ProjectTasksScreen({ projectInfo, onOpenTask }) {
  const [view, setView] = useState("list");
  const [state, setState] = useState("");
  const [collection, setCollection] = useState(null);
  const [pageStatus, setPageStatus] = useState({ loading: false, error: "" });
  const [editorOpen, setEditorOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const controllerRef = useRef(null);
  const resumeRef = useRef(null);
  const auth = useAuthSession();
  const projectID = projectInfo.project.id;
  const queryKey = `${projectID}:${state}`;
  const resource = useResource(async (signal) => {
    const [tasks, milestones] = await Promise.all([
      commonsAdapter.readProjectTasks(projectID, { state, milestone: "", cursor: "", limit: pageLimit }, signal),
      commonsAdapter.readProjectMilestones(projectID, pageLimit, signal),
    ]);
    return { queryKey, tasks, milestones };
  }, [queryKey, refreshKey]);
  const activeData = resource.data?.queryKey === queryKey ? resource.data : null;

  useEffect(() => {
    controllerRef.current?.abort();
    setCollection(null);
    setPageStatus({ loading: false, error: "" });
  }, [queryKey]);

  useEffect(() => {
    if (!activeData) return;
    setCollection({
      items: activeData.tasks.items,
      total: activeData.tasks.total,
      nextCursor: activeData.tasks.nextCursor,
      stateCounts: activeData.tasks.stateCounts,
    });
  }, [activeData]);

  useEffect(() => () => controllerRef.current?.abort(), []);

  async function loadMore() {
    if (!collection?.nextCursor || pageStatus.loading) return;
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    setPageStatus({ loading: true, error: "" });
    try {
      const page = await commonsAdapter.readProjectTasks(projectID, {
        state,
        milestone: "",
        cursor: collection.nextCursor,
        limit: pageLimit,
      }, controller.signal);
      setCollection((current) => {
        if (!current) return current;
        const known = new Set(current.items.map((task) => task.id));
        return {
          ...current,
          items: [...current.items, ...page.items.filter((task) => !known.has(task.id))],
          total: page.total,
          nextCursor: page.nextCursor,
          stateCounts: page.stateCounts,
        };
      });
      setPageStatus({ loading: false, error: "" });
    } catch (error) {
      if (error.name !== "AbortError") setPageStatus({ loading: false, error: error.message });
    }
  }

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function startNewTask() {
    if (auth.session?.authenticated) setEditorOpen(true);
    else requestAuth(() => setEditorOpen(true));
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    const resume = resumeRef.current;
    resumeRef.current = null;
    resume?.();
  }

  const milestones = activeData?.milestones.items || [];
  const milestonesByID = useMemo(() => new Map(milestones.map((milestone) => [milestone.id, milestone])), [milestones]);
  const partialTasks = Boolean(collection?.nextCursor);
  const partialView = partialTasks || (view === "roadmap" && Boolean(activeData?.milestones.truncated));
  const viewLabel = views.find((item) => item.id === view)?.label || "Task view";

  return (
    <section className="project-core-section task-workspace" aria-labelledby="tasks-title">
      <div className="project-core-toolbar">
        <div><h2 id="tasks-title">Tasks</h2><p>One canonical dataset, viewed as a list, lightweight board, or milestone roadmap.</p></div>
        <div className="view-controls">
          <button className="primary-button" type="button" onClick={startNewTask}><Plus aria-hidden="true" />New task</button>
          <div className="view-switcher" aria-label="Task view">
            {views.map((item) => <button key={item.id} type="button" aria-pressed={view === item.id} onClick={() => setView(item.id)}>{item.label}</button>)}
          </div>
          <Select
            compact
            label="State"
            value={state}
            onChange={setState}
            allLabel="All states"
            options={Object.entries(taskStateLabels).map(([value, label]) => ({ value, label }))}
          />
        </div>
      </div>
      {!activeData || !collection ? <InlineState status={resource.status} error={resource.error} /> : null}
      {activeData && collection ? (
        <>
          <div className="task-count-strip" aria-label="Task state counts">
            {boardStates.map((taskState) => (
              <button key={taskState} type="button" aria-pressed={state === taskState} onClick={() => setState((current) => current === taskState ? "" : taskState)}>
                <span>{taskStateLabels[taskState]}</span><strong>{collection.stateCounts[taskState]}</strong>
              </button>
            ))}
          </div>
          <div className="collection-status" role="status">
            <span>{collection.items.length} of {collection.total} {state ? `${taskStateLabels[state].toLowerCase()} ` : ""}tasks loaded</span>
            {partialView ? <strong>{viewLabel} is partial until all available pages are loaded.</strong> : <strong>{viewLabel} uses the complete loaded collection.</strong>}
          </div>
          {view === "list" ? <TaskList tasks={collection.items} milestonesByID={milestonesByID} onOpenTask={onOpenTask} /> : null}
          {view === "board" ? <TaskBoard tasks={collection.items} onOpenTask={onOpenTask} filtered={Boolean(state)} /> : null}
          {view === "roadmap" ? <TaskRoadmap tasks={collection.items} milestones={milestones} onOpenTask={onOpenTask} /> : null}
          {partialTasks ? <button className="secondary-button load-more-button" type="button" disabled={pageStatus.loading} onClick={loadMore}>{pageStatus.loading ? "Loading tasks…" : "Load more tasks"}</button> : null}
          {activeData.milestones.truncated ? <p className="bounded-note">Milestone metadata is bounded to the first {activeData.milestones.limit} records; the roadmap remains explicitly partial.</p> : null}
          {pageStatus.error ? <p className="form-message form-message--error" role="status">{pageStatus.error}</p> : null}
        </>
      ) : null}
      <TaskEditorDialog
        open={editorOpen}
        projectID={projectID}
        milestones={milestones}
        session={auth.session}
        onClose={() => setEditorOpen(false)}
        onSaved={() => { setEditorOpen(false); setRefreshKey((value) => value + 1); }}
        onAuthRequired={() => requestAuth(() => setEditorOpen(true))}
      />
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
    </section>
  );
}

function TaskList({ tasks, milestonesByID, onOpenTask }) {
  if (!tasks.length) return <InlineState empty emptyTitle="No tasks in this view" emptyDetail="Clear the state filter or create work through an authenticated Commons client." />;
  return (
    <div className="task-list" role="list">
      <div className="task-list-head" aria-hidden="true"><span>Task</span><span>Milestone</span><span>State</span><span>Priority</span><span>Updated</span></div>
      {tasks.map((task) => (
        <button key={task.id} type="button" role="listitem" onClick={() => onOpenTask(task.id)}>
          <span className="task-list-title"><strong>{task.title}</strong><small>{task.id}{task.dependencies.length ? ` · ${task.dependencies.length} ${task.dependencies.length === 1 ? "dependency" : "dependencies"}` : ""}</small></span>
          <span>{milestonesByID.get(task.milestoneID)?.title || "Unscheduled"}</span>
          <span><TaskState state={task.state} /></span>
          <span className="task-priority">P{task.priority}</span>
          <span><Timestamp value={task.updated} compact /></span>
        </button>
      ))}
    </div>
  );
}

function TaskBoard({ tasks, onOpenTask, filtered }) {
  if (!tasks.length) return <InlineState empty emptyTitle="No tasks in this view" emptyDetail="Clear the state filter to restore the board." />;
  const states = filtered ? [...new Set(tasks.map((task) => task.state))] : boardStates;
  return (
    <div className="task-board">
      {states.map((state) => {
        const items = tasks.filter((task) => task.state === state);
        return (
          <section key={state} aria-labelledby={`board-${state}`}>
            <header><h3 id={`board-${state}`}>{taskStateLabels[state]}</h3><span>{items.length}</span></header>
            <div>
              {items.map((task) => (
                <button key={task.id} type="button" onClick={() => onOpenTask(task.id)}>
                  <strong>{task.title}</strong><span>{task.id}</span><small>P{task.priority}{task.dependencies.length ? ` · ${task.dependencies.length} dependencies` : ""}</small>
                </button>
              ))}
              {!items.length ? <p>No tasks</p> : null}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function TaskRoadmap({ tasks, milestones, onOpenTask }) {
  const scheduled = new Set(milestones.map((milestone) => milestone.id));
  const rows = [...milestones.map((milestone) => ({ milestone, tasks: tasks.filter((task) => task.milestoneID === milestone.id) }))];
  const unscheduled = tasks.filter((task) => !task.milestoneID || !scheduled.has(task.milestoneID));
  if (unscheduled.length) rows.push({ milestone: null, tasks: unscheduled });
  if (!rows.length) return <InlineState empty emptyTitle="No roadmap yet" emptyDetail="Milestones will appear here once they are recorded." />;
  return (
    <div className="roadmap-list">
      {rows.map(({ milestone, tasks: milestoneTasks }) => {
        const counts = {
          ready: milestoneTasks.filter((task) => task.state === "ready").length,
          in_progress: milestoneTasks.filter((task) => task.state === "in_progress").length,
          blocked: milestoneTasks.filter((task) => task.state === "blocked").length,
          done: milestoneTasks.filter((task) => task.state === "done").length,
          cancelled: milestoneTasks.filter((task) => task.state === "cancelled").length,
          total: milestoneTasks.length,
        };
        return (
          <section key={milestone?.id || "unscheduled"}>
            <header>
              <div><span>{milestone?.status || "Unscheduled"}</span><h3>{milestone?.title || "Unscheduled work"}</h3>{milestone?.targetDate ? <small>Target {milestone.targetDate}</small> : null}</div>
              <ProgressMeter counts={counts} />
            </header>
            <div className="roadmap-tasks">
              {milestoneTasks.map((task) => <button key={task.id} type="button" onClick={() => onOpenTask(task.id)}><TaskState state={task.state} /><strong>{task.title}</strong><span>{task.id}</span></button>)}
              {!milestoneTasks.length ? <p>No tasks assigned to this milestone.</p> : null}
            </div>
          </section>
        );
      })}
    </div>
  );
}
