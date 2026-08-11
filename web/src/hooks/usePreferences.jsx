import { createContext, useContext, useEffect, useMemo, useState } from "react";

export const PREFERENCES_KEY = "codex-commons:preferences:v1";
export const DEFAULT_PREFERENCES = Object.freeze({ theme: "system", text: "default", density: "comfortable" });
const PreferenceContext = createContext(null);

const allowed = {
  theme: new Set(["system", "light", "dark"]),
  text: new Set(["default", "large"]),
  density: new Set(["comfortable", "compact"]),
};

function normalize(value) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return DEFAULT_PREFERENCES;
  return {
    theme: allowed.theme.has(value.theme) ? value.theme : DEFAULT_PREFERENCES.theme,
    text: allowed.text.has(value.text) ? value.text : DEFAULT_PREFERENCES.text,
    density: allowed.density.has(value.density) ? value.density : DEFAULT_PREFERENCES.density,
  };
}

function readPreferences() {
  try {
    return normalize(JSON.parse(globalThis.localStorage?.getItem(PREFERENCES_KEY) || "null"));
  } catch {
    return DEFAULT_PREFERENCES;
  }
}

function applyPreferences(preferences) {
  const root = document.documentElement;
  const dark = preferences.theme === "dark" || (preferences.theme === "system" && globalThis.matchMedia?.("(prefers-color-scheme: dark)").matches);
  root.dataset.themePreference = preferences.theme;
  root.dataset.theme = dark ? "dark" : "light";
  root.dataset.text = preferences.text;
  root.dataset.density = preferences.density;
  root.style.colorScheme = dark ? "dark" : "light";
}

export function PreferencesProvider({ children }) {
  const [preferences, setPreferences] = useState(readPreferences);

  useEffect(() => {
    applyPreferences(preferences);
    try {
      globalThis.localStorage?.setItem(PREFERENCES_KEY, JSON.stringify(preferences));
    } catch {
      // Preferences are optional; disabled or full storage must not break Commons.
    }
    if (preferences.theme !== "system" || !globalThis.matchMedia) return undefined;
    const query = globalThis.matchMedia("(prefers-color-scheme: dark)");
    const sync = () => applyPreferences(preferences);
    query.addEventListener("change", sync);
    return () => query.removeEventListener("change", sync);
  }, [preferences]);

  const value = useMemo(() => ({
    preferences,
    update(key, next) {
      if (!allowed[key]?.has(next)) return;
      setPreferences((current) => ({ ...current, [key]: next }));
    },
    reset() { setPreferences(DEFAULT_PREFERENCES); },
  }), [preferences]);

  return <PreferenceContext.Provider value={value}>{children}</PreferenceContext.Provider>;
}

export function usePreferences() {
  const value = useContext(PreferenceContext);
  if (!value) throw new Error("usePreferences must be used inside PreferencesProvider");
  return value;
}
