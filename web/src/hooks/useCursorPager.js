import { useState } from "react";

export function useCursorPager(defaultLimit = 10) {
  const [cursors, setCursors] = useState([""]);
  const [limit, setLimitState] = useState(defaultLimit);
  const cursor = cursors.at(-1) || "";

  function reset() {
    setCursors([""]);
  }

  function next(nextCursor) {
    if (nextCursor) setCursors((current) => [...current, nextCursor]);
  }

  function previous() {
    setCursors((current) => current.length > 1 ? current.slice(0, -1) : current);
  }

  function setLimit(value) {
    setLimitState(value);
    reset();
  }

  return { cursor, page: cursors.length, limit, canPrevious: cursors.length > 1, reset, next, previous, setLimit };
}
