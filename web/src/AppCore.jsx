import { useEffect, useState } from "react";
import { AppShell } from "./components/AppShell.jsx";
import { PeopleScreen } from "./screens/PeopleScreen.jsx";
import { PostsScreen } from "./screens/PostsScreen.jsx";
import { ProjectArea } from "./screens/ProjectArea.jsx";
import { ProjectsCoreScreen } from "./screens/ProjectsCoreScreen.jsx";

const ROUTES = new Set(["posts", "projects", "people"]);
const PROJECT_SECTIONS = new Set(["overview", "tasks", "posts", "wiki"]);

function decodeSegment(value) {
  try {
    return decodeURIComponent(value || "");
  } catch {
    return "";
  }
}

function routeFromHash() {
  const requested = window.location.hash.slice(1);
  const segments = requested.split("/");
  if (segments[0] === "project") {
    const projectID = decodeSegment(segments[1]);
    if (!projectID) return { route: "projects", projectID: "", section: "", itemID: "", revision: 0 };
    const section = PROJECT_SECTIONS.has(segments[2]) ? segments[2] : "overview";
    const itemID = decodeSegment(segments[3]);
    const revision = section === "wiki" && segments[4] === "revisions" ? Number(segments[5]) || 0 : 0;
    return { route: "project", projectID, section, itemID, revision };
  }
  if (segments[0] === "post") {
    const postID = decodeSegment(segments.slice(1).join("/"));
    return postID
      ? { route: "post", postID, projectID: "", section: "", itemID: "", revision: 0 }
      : { route: "posts", postID: "", projectID: "", section: "", itemID: "", revision: 0 };
  }
  return {
    route: ROUTES.has(requested) ? requested : "posts",
    postID: "",
    projectID: "",
    section: "",
    itemID: "",
    revision: 0,
  };
}

function projectHash({ projectID, section = "overview", itemID = "", revision = 0 }) {
  const base = `project/${encodeURIComponent(projectID)}/${section}`;
  if (!itemID) return base;
  const item = `${base}/${encodeURIComponent(itemID)}`;
  return section === "wiki" && revision ? `${item}/revisions/${revision}` : item;
}

export function AppCore() {
  const [location, setLocation] = useState(routeFromHash);

  useEffect(() => {
    const syncRoute = () => setLocation(routeFromHash());
    window.addEventListener("hashchange", syncRoute);
    return () => window.removeEventListener("hashchange", syncRoute);
  }, []);

  function navigate(nextRoute, reference = "") {
    let nextHash = nextRoute;
    if (nextRoute === "project") {
      const target = typeof reference === "string" ? { projectID: reference, section: "overview" } : reference;
      nextHash = target?.projectID ? projectHash(target) : "projects";
    }
    if (nextRoute === "post") nextHash = reference ? `post/${encodeURIComponent(reference)}` : "posts";
    window.location.hash = nextHash;
    setLocation(routeFromHash());
  }

  if (location.route === "posts" || location.route === "post") {
    return <PostsScreen selectedPostID={location.route === "post" ? location.postID : ""} onNavigate={navigate} />;
  }
  if (location.route === "project") return <ProjectArea location={location} onNavigate={navigate} />;
  return (
    <AppShell route={location.route} onNavigate={navigate}>
      <div className="standard-workspace">
        {location.route === "projects" ? <ProjectsCoreScreen onNavigate={navigate} /> : null}
        {location.route === "people" ? <PeopleScreen /> : null}
      </div>
    </AppShell>
  );
}
