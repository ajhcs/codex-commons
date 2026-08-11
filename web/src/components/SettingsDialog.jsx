import { useEffect, useRef } from "react";
import { usePreferences } from "../hooks/usePreferences.jsx";

const groups = [
  { key: "theme", label: "Theme", options: [["system", "System"], ["light", "Light"], ["dark", "Dark"]] },
  { key: "text", label: "Text", options: [["default", "Default"], ["large", "Large"]] },
  { key: "density", label: "Density", options: [["comfortable", "Comfortable"], ["compact", "Compact"]] },
];

export function SettingsDialog({ open, onClose }) {
  const dialogRef = useRef(null);
  const { preferences, update, reset } = usePreferences();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog ref={dialogRef} className="settings-dialog" aria-labelledby="settings-title" onClose={onClose} onCancel={(event) => { event.preventDefault(); onClose(); }}>
      <header><div><h2 id="settings-title">Settings</h2><p>Display preferences stay on this browser only.</p></div><button type="button" onClick={onClose}>Close</button></header>
      <div className="settings-groups">
        {groups.map((group) => (
          <fieldset key={group.key}>
            <legend>{group.label}</legend>
            <div className="settings-options">
              {group.options.map(([value, label]) => (
                <button key={value} type="button" aria-pressed={preferences[group.key] === value} onClick={() => update(group.key, value)}>{label}</button>
              ))}
            </div>
          </fieldset>
        ))}
      </div>
      <footer><button className="secondary-button" type="button" onClick={reset}>Restore defaults</button><button className="primary-button" type="button" onClick={onClose}>Done</button></footer>
    </dialog>
  );
}
