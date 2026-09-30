import { useLocation } from 'preact-iso';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { AuditEntry, AuditPage, AuditTarget } from '../../api/dto';
import { useResource, type Resource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Field } from './common/Field';
import { Table } from './common/Table';

// AuditScreen is /admin/audit (TKT-01M29CP2R4, docs/07 "The audit log"):
// who did what, newest first, filtered by actor, action, target and time.
// It is an admin's alone; the section list offers it to no one else, and
// the server refuses everyone else whatever this screen asks.
//
// The filters live in the address, as the API's own parameters, so an
// answer to "who moved this thread" is a link that can be pasted, and Back
// returns to the list before. The screen passes them to the API unchanged.

// params are the API's filters, in the order the address and the request
// spell them.
const params = ['actor', 'system', 'action', 'target_type', 'target_id', 'from', 'to'] as const;
type Param = (typeof params)[number];
type Filters = Record<Param, string>;

// targetTypes are docs/04's audit_log target types.
export const targetTypes = [
  'user',
  'forum',
  'group',
  'flag',
  'application',
  'thread',
  'post',
  'setting',
  'permission',
  'form',
  'identity',
  'blob',
];

// filtersOf reads the filters from the address's query.
function filtersOf(query: Record<string, string>): Filters {
  const f = {} as Filters;
  for (const p of params) f[p] = query[p] ?? '';
  return f;
}

// search is the query string for f, and cursor, with the empty ones left
// out.
function search(f: Filters, cursor = ''): string {
  const q = new URLSearchParams();
  for (const p of params) if (f[p]) q.set(p, f[p]);
  if (cursor) q.set('cursor', cursor);
  const s = q.toString();
  return s ? '?' + s : '';
}

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

// safeHref is href when it is a path on this site, and nothing otherwise,
// so a link is only ever drawn to one of our own pages. A leading slash is
// not enough: a browser reads a backslash as a slash, so "/\host" is
// another site (Terva review 1578). The href must hold no backslash or
// control character, and must resolve to this origin.
export function safeHref(href: string): string {
  if (!href.startsWith('/') || /[\\\u0000-\u001f\u007f]/.test(href)) return '';
  try {
    return new URL(href, location.origin).origin === location.origin ? href : '';
  } catch {
    return '';
  }
}

function TargetLink({ target }: { target: AuditTarget }) {
  const href = safeHref(target.href);
  return href ? (
    <a href={href} data-target={target.type}>
      {target.label}
    </a>
  ) : (
    <span data-target={target.type} title={target.id}>
      {target.label}
    </span>
  );
}

// Details are the row's details as text: a key, and its value as written,
// a string as it is and anything else as JSON. Nothing here is markup.
function Details({ details }: { details: Record<string, unknown> }) {
  const keys = Object.keys(details);
  if (keys.length === 0) return <span class="admin-muted">—</span>;
  return (
    <dl class="admin-facts audit-details">
      {keys.map((k) => {
        const v = details[k];
        return [<dt key={k + ':k'}>{k}</dt>, <dd key={k + ':v'}>{typeof v === 'string' ? v : JSON.stringify(v)}</dd>];
      })}
    </dl>
  );
}

function Actor({ entry }: { entry: AuditEntry }) {
  if (entry.actor) return <span data-actor={entry.actor.handle}>{entry.actor.handle}</span>;
  const via = entry.details.via;
  return (
    <span class="admin-muted" data-actor="">
      System{typeof via === 'string' ? ` (${via})` : ''}
    </span>
  );
}

export function AuditScreen() {
  const { query } = useLocation();
  const filters = filtersOf(query);
  const key = search(filters);
  const url = '/api/v1/admin/audit' + key;
  const res = useResource(url, (signal) => apiGet<AuditPage>(url, { signal }));
  return (
    <div class="admin-audit">
      <AuditFilters key={key} filters={filters} actions={res.data?.actions ?? []} />
      <AuditList key={key} filters={filters} res={res} />
    </div>
  );
}

// AuditFilters is the filter form. It edits a draft, and Apply puts it in
// the address, which fetches the list again; typing alone fetches nothing.
// actions is the answer's list of every action written today, so the
// client holds no copy of it.
function AuditFilters({ filters, actions }: { filters: Filters; actions: string[] }) {
  const { route } = useLocation();
  const [draft, setDraft] = useState<Filters>(filters);
  // The action filter offers each action, and each noun's prefix before
  // them, such as "user.", which selects every action on accounts.
  const prefixes = [...new Set(actions.map((a) => a.slice(0, a.indexOf('.') + 1)))];
  const set = (k: Param, v: string) => setDraft((d) => ({ ...d, [k]: v }));
  const text = (e: Event) => (e.target as HTMLInputElement).value;
  const apply = (e: Event) => {
    e.preventDefault();
    route('/admin/audit' + search(draft));
  };
  const system = draft.system === '1';

  return (
    <form class="admin-filters audit-filters" onSubmit={apply} aria-label="Filter the audit log">
      <Field label="Actor's handle">
        {(c) => (
          <input
            {...c}
            type="search"
            name="actor"
            value={draft.actor}
            disabled={system}
            onInput={(e) => set('actor', text(e))}
          />
        )}
      </Field>
      <label class="admin-check">
        <input
          type="checkbox"
          name="system"
          checked={system}
          onChange={(e) => setDraft((d) => ({ ...d, system: (e.target as HTMLInputElement).checked ? '1' : '', actor: '' }))}
        />
        Done by no account
      </label>
      <Field label="Action" hint="Choose one, or type it. Ending in a dot matches every action that starts with it.">
        {(c) => (
          <>
            <input
              {...c}
              type="search"
              name="action"
              list="audit-actions"
              value={draft.action}
              onInput={(e) => set('action', text(e))}
            />
            <datalist id="audit-actions" data-actions>
              {[...prefixes, ...actions].map((a) => (
                <option key={a} value={a} />
              ))}
            </datalist>
          </>
        )}
      </Field>
      <Field label="Target type">
        {(c) => (
          <select
            {...c}
            name="target_type"
            value={draft.target_type}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              setDraft((d) => ({ ...d, target_type: v, target_id: v ? d.target_id : '' }));
            }}
          >
            <option value="">Any</option>
            {targetTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        )}
      </Field>
      <Field label="Target id">
        {(c) => (
          <input
            {...c}
            type="search"
            name="target_id"
            value={draft.target_id}
            disabled={!draft.target_type}
            onInput={(e) => set('target_id', text(e))}
          />
        )}
      </Field>
      <Field label="From">
        {(c) => (
          <input
            {...c}
            type="date"
            name="from"
            value={draft.from ? dateOf(Number(draft.from)) : ''}
            onInput={(e) => set('from', text(e) ? String(startOf(text(e))) : '')}
          />
        )}
      </Field>
      <Field label="To, inclusive">
        {(c) => (
          <input
            {...c}
            type="date"
            name="to"
            value={draft.to ? dateOf(Number(draft.to) - 1) : ''}
            onInput={(e) => set('to', text(e) ? String(startOf(text(e), 1)) : '')}
          />
        )}
      </Field>
      <div class="admin-actions">
        <button type="submit" class="admin-button">
          Apply
        </button>
        <a class="admin-link" href="/admin/audit">
          Clear
        </a>
      </div>
    </form>
  );
}

// AuditList is the first page, res, and the older pages Older appends.
function AuditList({ filters, res }: { filters: Filters; res: Resource<AuditPage> }) {
  const [more, setMore] = useState<AuditPage[]>([]);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);

  const pages = res.data ? [res.data, ...more] : [];
  const rows = pages.flatMap((p) => p.entries);
  const next = pages.length ? pages[pages.length - 1].next_cursor : '';
  // The screen keys this list by its filters, so a filter change starts a
  // new one and an Older still pending belongs to a list that is gone.
  const loadMore = async () => {
    const ctl = new AbortController();
    pending.current?.abort();
    pending.current = ctl;
    setMoreError(null);
    setLoadingMore(true);
    try {
      const page = await apiGet<AuditPage>('/api/v1/admin/audit' + search(filters, next), { signal: ctl.signal });
      if (!ctl.signal.aborted) setMore((m) => [...m, page]);
    } catch (err) {
      if (!ctl.signal.aborted) setMoreError(err);
    } finally {
      if (!ctl.signal.aborted) setLoadingMore(false);
    }
  };

  return (
    <div class="audit-list">
      {res.error && !res.data && <ErrorPanel error={res.error} what="The audit log" onRetry={res.retry} />}
      {!res.data && !res.error && (
        <p class="admin-muted" aria-live="polite">
          Loading…
        </p>
      )}
      {res.data && (
        <Table<AuditEntry>
          caption="The audit log, newest first"
          rows={rows}
          rowKey={(e) => e.id}
          empty="Nothing in the log matches."
          columns={[
            { key: 'at', label: 'When', cell: (e) => <time dateTime={new Date(e.at).toISOString()}>{stamp(e.at)}</time> },
            { key: 'actor', label: 'Who', cell: (e) => <Actor entry={e} /> },
            { key: 'action', label: 'Action', cell: (e) => <code>{e.action}</code> },
            {
              key: 'target',
              label: 'Target',
              cell: (e) => (
                <>
                  <TargetLink target={e.target} />
                  {e.related.length > 0 && (
                    <span class="audit-related" data-related>
                      {' '}
                      ({e.related.map((r, i) => [i > 0 ? ' → ' : '', <TargetLink key={i} target={r} />])})
                    </span>
                  )}
                </>
              ),
            },
            { key: 'details', label: 'Details', cell: (e) => <Details details={e.details} /> },
            { key: 'ip', label: 'Address', cell: (e) => e.ip || <span class="admin-muted">—</span> },
          ]}
        />
      )}
      {next && (
        <button type="button" class="admin-button" onClick={loadMore} disabled={loadingMore} data-older>
          Older
        </button>
      )}
      {moreError !== null && <ErrorPanel error={moreError} what="Older entries" />}
    </div>
  );
}
