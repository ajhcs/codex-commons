import { useEffect, useState } from "react";
import { AppShell } from "./components/AppShell.jsx";
import { PeopleScreen } from "./screens/PeopleScreen.jsx";
import { PostsScreen } from "./screens/PostsScreen.jsx";
import { ProjectOverviewScreen } from "./screens/ProjectOverviewScreen.jsx";
import { ProjectsScreen } from "./screens/ProjectsScreen.jsx";

const ROUTES = new Set(["posts", "projects", "people"]);

function decodeReference(requested, prefix, route, fallback) {
  if (!requested.startsWith(prefix)) return null;
  try {
    const id = decodeURIComponent(requested.slice(prefix.length));
    return id ? { route, id } : { route: fallback, id: "" };
  } catch {
    return { route: fallback, id: "" };
  }
}

function routeFromHash() {
  const requested = window.location.hash.slice(1);
  const project = decodeReference(requested, "project/", "project", "projects");
  if (project) return { route: project.route, projectID: project.id, postID: "" };
  const post = decodeReference(requested, "post/", "post", "posts");
  if (post) return { route: post.route, projectID: "", postID: post.id };
  return {
    route: ROUTES.has(requested) ? requested : "posts",
    projectID: "",
    postID: "",
  };
}

export function App() {
  const [location, setLocation] = useState(routeFromHash);

  useEffect(() => {
    const syncRoute = () => setLocation(routeFromHash());
    window.addEventListener("hashchange", syncRoute);
    return () => window.removeEventListener("hashchange", syncRoute);
  }, []);

  function navigate(nextRoute, reference = "") {
    const nextHash = (nextRoute === "project" || nextRoute === "post") && reference
      ? `${nextRoute}/${encodeURIComponent(reference)}`
      : nextRoute === "project" ? "projects"
        : nextRoute === "post" ? "posts"
          : nextRoute;
    window.location.hash = nextHash;
    setLocation(routeFromHash());
  }

  const route = location.route;
  if (route === "posts" || route === "post") {
    return (
      <PostsScreen
        selectedPostID={route === "post" ? location.postID : ""}
        onNavigate={navigate}
      />
    );
  }
  return (
    <AppShell route={route} onNavigate={navigate}>
      <div className="standard-workspace">
        {route === "projects" ? <ProjectsScreen onNavigate={navigate} /> : null}
        {route === "people" ? <PeopleScreen /> : null}
        {route === "project" ? <ProjectOverviewScreen projectID={location.projectID} onBack={() => navigate("projects")} /> : null}
      </div>
    </AppShell>
  );
}
