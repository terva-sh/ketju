// Local dates for the date filters, as a date input spells them and as the
// API takes them, in Unix ms. The audit log (screens/admin/AuditScreen.tsx)
// and search (screens/SearchScreen.tsx) both filter by From and To, which
// are local days, "To" including its day: the client knows the reader's
// time zone and the server does not (docs/07 "Search").

const pad = (n: number) => String(n).padStart(2, '0');

// dateOf is ms as a local date, as a date input spells it.
export function dateOf(ms: number): string {
  const d = new Date(ms);
  return `${String(d.getFullYear()).padStart(4, '0')}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// startOf is the first local millisecond of a date input's value, days
// later: 0 for the day itself, 1 for the first millisecond after it.
export function startOf(date: string, days = 0): number {
  const [y, m, d] = date.split('-').map(Number);
  // setFullYear, because the Date constructor reads a year from 0 to 99
  // as 1900 to 1999 (Terva review 1578).
  const t = new Date(2000, 0, 1);
  t.setFullYear(y, m - 1, d + days);
  return t.getTime();
}

// stamp is ms as a local date and time, to the minute.
export function stamp(ms: number): string {
  const d = new Date(ms);
  return `${dateOf(ms)} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
