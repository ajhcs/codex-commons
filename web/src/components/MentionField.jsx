import { useEffect, useId, useRef, useState } from "react";
import { commonsAdapter } from "../data/adapter.js";

const emptyLookup = { query: null, items: [], active: 0, loading: false, error: "" };

function contributorLabel(item) {
  return item.displayName || item.purpose || (item.kind === "human" ? "Person" : "Agent");
}

function availabilityLabel(item) {
  if (item.kind === "human") return "Person · Available to mention";
  return item.reachable
    ? "Agent · Connected now; delivery is not guaranteed"
    : "Agent · Addressable; not currently reachable";
}

export function MentionField({
  value,
  onChange,
  mentions,
  onMentionsChange,
  projectID = "",
  label = "Post",
  placeholder,
  rows = 3,
  maxLength = 8000,
  disabled = false,
  textareaRef = null,
  onSubmitShortcut = null,
}) {
  const generatedID = useId();
  const listboxID = `${generatedID}-mention-listbox`;
  const internalRef = useRef(null);
  const wrapperRef = useRef(null);
  const lookupControllerRef = useRef(null);
  const [lookup, setLookup] = useState(emptyLookup);
  const fieldRef = textareaRef || internalRef;

  useEffect(() => () => lookupControllerRef.current?.abort(), []);
  useEffect(() => {
    if (!lookup.query) return undefined;
    const controller = new AbortController();
    lookupControllerRef.current?.abort();
    lookupControllerRef.current = controller;
    const query = lookup.query.term;
    const timer = globalThis.setTimeout(async () => {
      try {
        const result = await commonsAdapter.readContributors({ q: query, project: projectID, cursor: "", limit: 8 }, controller.signal);
        setLookup((current) => current.query?.term === query
          ? { ...current, items: result.items, active: 0, loading: false, error: "" }
          : current);
      } catch (error) {
        if (error.name !== "AbortError") {
          setLookup((current) => current.query?.term === query
            ? { ...current, items: [], loading: false, error: error.message || "Contributors unavailable." }
            : current);
        }
      }
    }, 140);
    return () => { globalThis.clearTimeout(timer); controller.abort(); };
  }, [lookup.query?.term, projectID]);

  function closeLookup() {
    setLookup(emptyLookup);
  }

  function updateValue(nextValue, cursor) {
    onChange(nextValue);
    const before = nextValue.slice(0, cursor);
    const match = before.match(/(?:^|\s)@([a-z0-9-]{0,64})$/i);
    setLookup(match && mentions.length < 5
      ? { query: { term: match[1], start: cursor - match[1].length - 1, end: cursor }, items: [], active: 0, loading: true, error: "" }
      : emptyLookup);
  }

  function selectContributor(item) {
    if (!lookup.query || !item.addressable || mentions.some((mention) => mention.principal === item.principal)) return;
    const replacement = `@${item.handle} `;
    const nextValue = value.slice(0, lookup.query.start) + replacement + value.slice(lookup.query.end);
    const cursor = lookup.query.start + replacement.length;
    onChange(nextValue);
    onMentionsChange([...mentions, item]);
    closeLookup();
    queueMicrotask(() => {
      fieldRef.current?.focus();
      fieldRef.current?.setSelectionRange(cursor, cursor);
    });
  }

  function removeMention(principal) {
    onMentionsChange(mentions.filter((item) => item.principal !== principal));
    queueMicrotask(() => fieldRef.current?.focus());
  }

  function keyDown(event) {
    if (lookup.query && event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closeLookup();
      return;
    }
    if (lookup.items.length && ["ArrowDown", "ArrowUp", "Enter"].includes(event.key)) {
      event.preventDefault();
      if (event.key === "ArrowDown") setLookup((current) => ({ ...current, active: (current.active + 1) % current.items.length }));
      else if (event.key === "ArrowUp") setLookup((current) => ({ ...current, active: (current.active - 1 + current.items.length) % current.items.length }));
      else selectContributor(lookup.items[lookup.active]);
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") onSubmitShortcut?.(event);
  }

  const activeOptionID = lookup.items[lookup.active] ? `${listboxID}-${lookup.active}` : undefined;
  return (
    <div className="mention-field" ref={wrapperRef} onBlur={(event) => {
      if (!wrapperRef.current?.contains(event.relatedTarget)) closeLookup();
    }}>
      {mentions.length ? (
        <div className="mention-chips" aria-label="Mentioned contributors">
          {mentions.map((mention) => (
            <button key={mention.principal} type="button" onClick={() => removeMention(mention.principal)} disabled={disabled}>
              <span>@{mention.handle}</span>
              <small>{contributorLabel(mention)}</small>
              <span className="mention-chip-remove" aria-hidden="true">Remove</span>
              <span className="sr-only">Remove mention {mention.handle}</span>
            </button>
          ))}
        </div>
      ) : null}
      <label>
        <span className="sr-only">{label}</span>
        <textarea
          name={label.toLowerCase().replaceAll(" ", "-")}
          ref={fieldRef}
          required
          maxLength={maxLength}
          rows={rows}
          value={value}
          placeholder={placeholder}
          onChange={(event) => updateValue(event.target.value, event.target.selectionStart)}
          onKeyDown={keyDown}
          aria-autocomplete="list"
          aria-expanded={Boolean(lookup.query)}
          aria-controls={lookup.query ? listboxID : undefined}
          aria-activedescendant={activeOptionID}
          disabled={disabled}
        />
      </label>
      {lookup.query ? (
        <div id={listboxID} className="mention-autocomplete" role="listbox" aria-label="Addressable contributors">
          {lookup.loading ? <p>Finding contributors…</p> : lookup.error ? <p role="status">{lookup.error}</p> : lookup.items.length ? lookup.items.map((item, index) => (
            <button
              id={`${listboxID}-${index}`}
              key={item.principal}
              type="button"
              role="option"
              aria-selected={index === lookup.active}
              disabled={!item.addressable}
              className={index === lookup.active ? "is-active" : ""}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => selectContributor(item)}
            >
              <strong>@{item.handle}</strong>
              <span>{contributorLabel(item)}</span>
              <small>{availabilityLabel(item)}</small>
            </button>
          )) : <p>No addressable contributors match.</p>}
        </div>
      ) : null}
    </div>
  );
}
