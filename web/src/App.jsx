import { useEffect, useState } from "react";
import { AppShell } from "./components/AppShell.jsx";
import { fixtureAdapter } from "./data/adapter.js";
import { useResource } from "./hooks/useResource.js";
import { AttentionScreen } from "./screens/AttentionScreen.jsx";
import { PeopleScreen } from "./screens/PeopleScreen.jsx";
import { ProjectOverviewScreen } from "./screens/ProjectOverviewScreen.jsx";
import { ProjectsScreen } from "./screens/ProjectsScreen.jsx";

const ROUTES = new Set(["attention", "projects", "people", "project"]);

function routeFromHash() {
  const requested = window.location.hash.slice(1);
  return ROUTES.has(requested) ? requested : "attention";
}

export function App() {
  const [route, setRoute] = useState(routeFromHash);
  const presence = useResource((signal) => fixtureAdapter.readPeople({
    q: "", project: "", execution: "", host: "", host_connected: undefined,
    cursor: "", limit: 4,
  }, signal), []);

  useEffect(() => {
    const syncRoute = () => setRoute(routeFromHash());
    window.addEventListener("hashchange", syncRoute);
    return () => window.removeEventListener("hashchange", syncRoute);
  }, []);

  function navigate(nextRoute) {
    window.location.hash = nextRoute;
    setRoute(nextRoute);
  }

  return (
    <AppShell
      route={route}
      onNavigate={navigate}
      presence={presence.data?.items || []}
      presenceTotal={presence.data?.total}
      presenceStatus={presence.status}
    >
      {route === "attention" ? <AttentionScreen /> : null}
      {route === "projects" ? <ProjectsScreen onNavigate={navigate} /> : null}
      {route === "people" ? <PeopleScreen /> : null}
      {route === "project" ? <ProjectOverviewScreen onBack={() => navigate("projects")} /> : null}
    </AppShell>
  );
}
