import type { ComponentChildren } from 'preact';

// Table is the admin screens' list of rows: a real table, with a caption for
// screen readers and a header per column, so every section's list reads the
// same way. It scrolls sideways inside its own box on a phone rather than
// widening the page.
export interface Column<T> {
  key: string;
  label: string;
  cell: (row: T) => ComponentChildren;
  // numeric columns align right, in tabular figures.
  numeric?: boolean;
}

export function Table<T>({
  caption,
  columns,
  rows,
  rowKey,
  empty,
}: {
  caption: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string | number;
  // What to say when there are no rows.
  empty: string;
}) {
  if (rows.length === 0) {
    return (
      <p class="admin-empty" data-empty>
        {empty}
      </p>
    );
  }
  return (
    <div class="admin-table">
      <table>
        <caption>{caption}</caption>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} scope="col" class={c.numeric ? 'tnum num' : undefined}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={rowKey(r)}>
              {columns.map((c) => (
                <td key={c.key} class={c.numeric ? 'tnum num' : undefined}>
                  {c.cell(r)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
