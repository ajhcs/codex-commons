import React from "react";
import { createRoot } from "react-dom/client";
import { AppCore } from "./AppCore.jsx";
import { AuthSessionProvider } from "./hooks/AuthSessionContext.jsx";
import { PreferencesProvider } from "./hooks/usePreferences.jsx";
import "./styles.css";

createRoot(document.getElementById("root")).render(
  <React.StrictMode>
    <PreferencesProvider>
      <AuthSessionProvider>
        <AppCore />
      </AuthSessionProvider>
    </PreferencesProvider>
  </React.StrictMode>,
);
