export function DataTable({ label, columns, items, rowKey, loading = false, error = null, emptyMessage = "Nothing matches these filters." }) {
  return (
    <div className="table-frame">
      <table aria-label={label} aria-busy={loading}>
        <thead>
          <tr>{columns.map((column) => <th key={column.key} scope="col" className={column.className}>{column.label}</th>)}</tr>
        </thead>
        <tbody>
          {loading ? <TableState colSpan={columns.length} kind="loading" title="Loading current data" detail="Fetching a bounded page…" /> : null}
          {!loading && error ? <TableState colSpan={columns.length} kind="error" title="Couldn’t load this view" detail={error} /> : null}
          {!loading && !error && items.length === 0 ? <TableState colSpan={columns.length} kind="empty" title={emptyMessage} detail="Adjust a filter or clear the search." /> : null}
          {!loading && !error ? items.map((item) => (
            <tr key={rowKey(item)}>{columns.map((column) => <td key={column.key} className={column.className}>{column.render(item)}</td>)}</tr>
          )) : null}
        </tbody>
      </table>
    </div>
  );
}

function TableState({ colSpan, kind, title, detail }) {
  return (
    <tr className="table-state-row">
      <td colSpan={colSpan}>
        <div className={`table-state table-state--${kind}`} role={kind === "error" ? "alert" : "status"}>
          <span className="table-state-title">{title}</span>
          <span>{detail}</span>
        </div>
      </td>
    </tr>
  );
}
