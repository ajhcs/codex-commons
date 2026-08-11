import { useRef, useState } from "react";
import { AppShell } from "../components/AppShell.jsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { MilestoneEditorDialog, ProjectEditorDialog } from "../components/ProjectManagementDialogs.jsx";
import { ProjectHeader, InlineState } from "../components/ProjectParts.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useResource } from "../hooks/useResource.js";
import { PostsScreen } from "./PostsScreen.jsx";
import { ProjectOverviewCoreScreen } from "./ProjectOverviewCoreScreen.jsx";
import { ProjectTaskDetailScreen } from "./ProjectTaskDetailScreen.jsx";
import { ProjectTasksScreen } from "./ProjectTasksScreen.jsx";
import { ProjectWikiPageScreen } from "./ProjectWikiPageScreen.jsx";
import { ProjectWikiScreen } from "./ProjectWikiScreen.jsx";

export function ProjectArea({ location, onNavigate }) {
  const projectID = location.projectID;
  const [refreshKey, setRefreshKey] = useState(0);
  const [projectEditorOpen, setProjectEditorOpen] = useState(false);
  const [milestoneEditor, setMilestoneEditor] = useState({ open: false, milestone: null });
  const [loginOpen, setLoginOpen] = useState(false);
  const resumeRef = useRef(null);
  const auth = useAuthSession();
  const resource = useResource((signal) => commonsAdapter.readProject(projectID, signal), [projectID, refreshKey]);

  function navigateProject(section, itemID = "", revision = 0) {
    onNavigate("project", { projectID, section, itemID, revision });
  }

  function requestAuth(resume) {
    auth.expire();
    resumeRef.current = resume || null;
    setLoginOpen(true);
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    const resume = resumeRef.current;
    resumeRef.current = null;
    resume?.();
  }

  function startProjectEdit() {
    if (auth.session?.authenticated) setProjectEditorOpen(true);
    else requestAuth(() => setProjectEditorOpen(true));
  }

  function startMilestoneEdit(milestone = null) {
    const open = () => setMilestoneEditor({ open: true, milestone });
    if (auth.session?.authenticated) open();
    else requestAuth(open);
  }

  function refreshCanonical() {
    setRefreshKey((value) => value + 1);
  }

  async function refreshMilestoneCanonical() {
    const editingID = milestoneEditor.milestone?.id;
    refreshCanonical();
    if (!editingID) return;
    const page = await commonsAdapter.readProjectMilestones(projectID, 100);
    const canonical = page.items.find((milestone) => milestone.id === editingID);
    if (!canonical) throw new Error("Canonical milestone is outside the bounded collection.");
    setMilestoneEditor((current) => ({ ...current, milestone: canonical }));
  }

  if (location.section === "posts") {
    return (
      <PostsScreen
        projectID={projectID}
        projectInfo={resource.data}
        selectedPostID={location.itemID}
        onNavigate={onNavigate}
        onProjectNavigate={navigateProject}
      />
    );
  }

  return (
    <AppShell route="project" onNavigate={onNavigate}>
      <div className="standard-workspace">
        {!resource.data ? (
          <div className="project-route-state"><InlineState status={resource.status} error={resource.error} /></div>
        ) : (
          <>
            <ProjectHeader
              project={resource.data.project}
              activeSection={location.section}
              onBack={() => onNavigate("projects")}
              onNavigate={navigateProject}
              actions={(
                <>
                  <button className="secondary-button" type="button" onClick={startProjectEdit}>Edit project</button>
                  <button className="primary-button" type="button" onClick={() => startMilestoneEdit()}>New milestone</button>
                </>
              )}
            />
            {location.section === "overview" ? <ProjectOverviewCoreScreen projectInfo={resource.data} onOpenTask={(taskID) => navigateProject("tasks", taskID)} onNavigate={navigateProject} onEditMilestone={() => startMilestoneEdit(resource.data.activeMilestone)} /> : null}
            {location.section === "tasks" && !location.itemID ? <ProjectTasksScreen projectInfo={resource.data} onOpenTask={(taskID) => navigateProject("tasks", taskID)} /> : null}
            {location.section === "tasks" && location.itemID ? <ProjectTaskDetailScreen projectInfo={resource.data} taskID={location.itemID} onBack={() => navigateProject("tasks")} onOpenTask={(taskID) => navigateProject("tasks", taskID)} /> : null}
            {location.section === "wiki" && !location.itemID ? <ProjectWikiScreen projectInfo={resource.data} onOpenPage={(slug) => navigateProject("wiki", slug)} /> : null}
            {location.section === "wiki" && location.itemID ? (
              <ProjectWikiPageScreen
                projectInfo={resource.data}
                slug={location.itemID}
                revision={location.revision}
                onBack={() => navigateProject("wiki")}
                onOpenRevision={(nextRevision) => navigateProject("wiki", location.itemID, nextRevision)}
                onOpenCurrent={() => navigateProject("wiki", location.itemID)}
              />
            ) : null}
            <ProjectEditorDialog
              open={projectEditorOpen}
              project={resource.data.project}
              session={auth.session}
              onClose={() => setProjectEditorOpen(false)}
              onSaved={() => { setProjectEditorOpen(false); refreshCanonical(); }}
              onConflict={refreshCanonical}
              onAuthRequired={() => requestAuth(() => setProjectEditorOpen(true))}
            />
            <MilestoneEditorDialog
              open={milestoneEditor.open}
              projectID={projectID}
              milestone={milestoneEditor.milestone}
              nextPosition={resource.data.counts.milestones}
              session={auth.session}
              onClose={() => setMilestoneEditor({ open: false, milestone: null })}
              onSaved={() => { setMilestoneEditor({ open: false, milestone: null }); refreshCanonical(); }}
              onConflict={refreshMilestoneCanonical}
              onAuthRequired={() => requestAuth(() => setMilestoneEditor((current) => ({ ...current, open: true })))}
            />
            <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeRef.current = null; }} onAuthenticated={authenticated} />
          </>
        )}
      </div>
    </AppShell>
  );
}
