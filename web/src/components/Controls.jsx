import { useEffect, useId, useRef, useState } from "react";
import ChevronDown from "../icons/ChevronDown.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import ChevronRight from "../icons/ChevronRight.tsx";
import Clock from "../icons/Clock.tsx";
import Search from "../icons/Search.tsx";

export function SearchField({ label, value, onChange, placeholder = "Search…" }) {
  const id = useId();
  return (
    <label className="search-field" htmlFor={id}>
      <span className="sr-only">{label}</span>
      <Search aria-hidden="true" />
      <input
        id={id}
        type="search"
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      {value ? <button className="clear-search" type="button" onClick={() => onChange("")} aria-label={`Clear ${label}`}>Clear</button> : null}
    </label>
  );
}

export function Select({ label, value, options, onChange, allLabel = "All", compact = false, optionsTruncated = false }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const rootRef = useRef(null);
  const buttonRef = useRef(null);
  const activeValueIsMissing = value && !options.some((option) => option.value === value);
  const items = [
    { value: "", label: allLabel },
    ...(activeValueIsMissing ? [{ value, label: value }] : []),
    ...options,
  ];
  const selected = items.find((item) => item.value === value) || items[0];
  const selectedIndex = Math.max(0, items.findIndex((option) => option.value === selected.value));

  useEffect(() => {
    if (!open) return undefined;
    const close = (event) => {
      if (!rootRef.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [open]);

  function choose(nextValue) {
    onChange(nextValue);
    setOpen(false);
    buttonRef.current?.focus();
  }

  function handleKeyDown(event) {
    if (event.key === "Escape") {
      setOpen(false);
      buttonRef.current?.focus();
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      const next = (selectedIndex + direction + items.length) % items.length;
      choose(items[next].value);
    }
  }

  return (
    <div className={`select-control${compact ? " select-control--compact" : ""}`} ref={rootRef}>
      <span className="select-label" id={`${id}-label`}>{label}</span>
      <button
        ref={buttonRef}
        className="select-trigger"
        type="button"
        aria-labelledby={`${id}-label ${id}-value`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
        onKeyDown={handleKeyDown}
      >
        <span id={`${id}-value`}>{selected.label}</span>
        <ChevronDown aria-hidden="true" />
      </button>
      {open ? (
        <div className="select-menu" role="listbox" aria-labelledby={`${id}-label`} onKeyDown={handleKeyDown}>
          {items.map((option) => (
            <button
              key={option.value || "all"}
              className="select-option"
              type="button"
              role="option"
              aria-selected={option.value === value}
              onClick={() => choose(option.value)}
            >
              <span>{option.label}</span>
              {option.count != null ? <span className="option-count">{option.count}</span> : null}
            </button>
          ))}
          {optionsTruncated ? (
            <p className="select-menu-note" role="status">Showing the first 50 available values.</p>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function DateFilter({ value, onChange }) {
  return (
    <div className="date-filter">
      <Clock aria-hidden="true" />
      <Select
        compact
        label="Updated"
        value={value}
        onChange={onChange}
        allLabel="Any time"
        options={[
          { value: "7d", label: "Last 7 days" },
          { value: "30d", label: "Last 30 days" },
        ]}
      />
    </div>
  );
}

export function SeverityIndicator({ severity }) {
  return (
    <span className={`severity severity--${severity}`}>
      <span className="severity-mark" aria-hidden="true" />
      <span>{severity[0].toUpperCase() + severity.slice(1)}</span>
    </span>
  );
}

export function Timestamp({ value, compact = false }) {
  if (!value) return <span className="muted">No activity yet</span>;
  return (
    <time className={`timestamp${compact ? " timestamp--compact" : ""}`} dateTime={value.iso} title={`${value.absolute} UTC`}>
      <span>{value.relative}</span>
      {compact ? null : <span>{value.absolute}</span>}
    </time>
  );
}

export function ActionButton({ children, destination, onOpen }) {
  return (
    <button className="action-button" type="button" onClick={() => onOpen(destination)} disabled={!destination}>
      <span>{children}</span>
      <ChevronRight aria-hidden="true" />
    </button>
  );
}

export function CursorPager({ page, canPrevious, canNext, limit, total, onPrevious, onNext, onLimit }) {
  return (
    <div className="pager" aria-label="Pagination">
      <Select
        compact
        label="Rows"
        value={String(limit)}
        allLabel="10"
        onChange={(value) => onLimit(Number(value || 10))}
        options={[{ value: "5", label: "5" }, { value: "10", label: "10" }, { value: "20", label: "20" }]}
      />
      <span className="pager-summary">Page {page} · {total} total</span>
      <div className="pager-actions">
        <button type="button" onClick={onPrevious} disabled={!canPrevious} aria-label="Previous page"><ChevronLeft aria-hidden="true" /><span>Previous</span></button>
        <button type="button" onClick={onNext} disabled={!canNext}><span>Next</span><ChevronRight aria-hidden="true" /></button>
      </div>
    </div>
  );
}
