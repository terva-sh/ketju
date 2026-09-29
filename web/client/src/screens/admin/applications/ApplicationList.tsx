import { useLocation } from 'preact-iso';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../../api/client';
import type { AdminApplicationList, AdminApplicationRow } from '../../../api/dto';
import { useResource } from '../../../api/resource';
import { ErrorPanel } from '../../../components/ErrorPanel';
import { when } from '../../apply/when';
import { Field } from '../common/Field';
import { Table } from '../common/Table';

// ApplicationList is /admin/applications: one status's applications, newest
// first, 25 a page (docs/07 "Applications"). Pending is the default, and
// the filter is ?status= in the address, so a filtered list can be linked
// and Back returns to it.

// The list's filters, as the server takes them.
export const filters = ['pending', 'approved', 'denied', 'withdrawn', 'all'] as const;
export type Filter = (typeof filters)[number];

const labels: Record<Filter, string> = {
  pending: 'Pending',
  approved: 'Approved',
  denied: 'Denied',
  withdrawn: 'Withdrawn',
  all: 'All',
};

// applicationHref opens one application. It carries the list's filter, when
// it is not the default, so the detail's way back returns to the same list
// (Terva review 1472).
export const applicationHref = (id: number | string, status: Filter = 'pending') =>
  `/admin/applications?id=${id}${status === 'pending' ? '' : `&status=${status}`}`;

// listHref is the list under one filter.
export const listHref = (status: Filter) =>
  status === 'pending' ? '/admin/applications' : `/admin/applications?status=${status}`;

export const filterLabel = (status: Filter) => labels[status];

// hasStatus is whether an application is in status: pending, approved,
// denied or withdrawn. It is the application's own state, not an error's.
export const hasStatus = ({ status }: { status: string }, want: string) => status === want;

function listUrl(status: Filter, cursor = ''): string {
  const q = new URLSearchParams({ status });
  if (cursor) q.set('cursor', cursor);
  return `/api/v1/admin/applications?${q}`;
}

export function StatusMark({ status }: { status: string }) {
  return (
    <span class="admin-mark app-status" data-status={status}>
      {status}
    </span>
  );
}

// ApplicantCell is who applied, as the sign-in says: the address and
// whether the provider verified it, and the provider.
export function ApplicantCell({
  provider,
  email,
  verified,
}: {
  provider: string | null;
  email: string | null;
  verified: boolean;
}) {
  if (provider === null) return <span class="admin-muted">Sign-in removed</span>;
  return (
    <span class="app-applicant">
      {email ? <span class="app-email">{email}</span> : <span class="admin-muted">No email</span>}{' '}
      {email && (
        <span class="admin-mark" data-verified={verified}>
          {verified ? 'verified' : 'unverified'}
        </span>
      )}{' '}
      <span class="admin-muted">via {provider}</span>
    </span>
  );
}

export function ApplicationList({ status }: { status: Filter }) {
  const { route } = useLocation();
  const [more, setMore] = useState<AdminApplicationList[]>([]);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const url = listUrl(status);
  const res = useResource(url, (signal) => apiGet<AdminApplicationList>(url, { signal }));

  const pages = res.data ? [res.data, ...more] : [];
  const rows = pages.flatMap((p) => p.applications);
  const next = pages.length ? pages[pages.length - 1].next_cursor : '';
  // The screen keys this list by status, so a filter change starts a new
  // one and a More still pending belongs to a list that is gone.
  const loadMore = async () => {
    const ctl = new AbortController();
    pending.current?.abort();
    pending.current = ctl;
    setMoreError(null);
    setLoadingMore(true);
    try {
      const page = await apiGet<AdminApplicationList>(listUrl(status, next), { signal: ctl.signal });
      if (!ctl.signal.aborted) setMore((m) => [...m, page]);
    } catch (err) {
      if (!ctl.signal.aborted) setMoreError(err);
    } finally {
      if (!ctl.signal.aborted) setLoadingMore(false);
    }
  };

  const choose = (v: string) => route(listHref(v as Filter));

  return (
    <div class="app-list">
      <form class="admin-filters" onSubmit={(e) => e.preventDefault()} aria-label="Filter applications">
        <Field label="Status">
          {(c) => (
            <select {...c} value={status} onChange={(e) => choose((e.target as HTMLSelectElement).value)} data-filter>
              {filters.map((f) => (
                <option key={f} value={f}>
                  {labels[f]}
                </option>
              ))}
            </select>
          )}
        </Field>
      </form>
      {res.error && !res.data && <ErrorPanel error={res.error} what="The applications" onRetry={res.retry} />}
      {!res.data && !res.error && (
        <p class="admin-muted" aria-live="polite">
          Loading…
        </p>
      )}
      {res.data && (
        <Table<AdminApplicationRow>
          caption={`${labels[status]} applications, newest first`}
          rows={rows}
          rowKey={(r) => r.id}
          empty={status === 'pending' ? 'No application is waiting.' : 'No application matches.'}
          columns={[
            {
              key: 'id',
              label: 'Application',
              cell: (r) => (
                <a href={applicationHref(r.id, status)} data-application-link={r.id}>
                  #{r.id}
                </a>
              ),
            },
            { key: 'at', label: 'Submitted', cell: (r) => when(r.created_at) },
            {
              key: 'who',
              label: 'Applicant',
              cell: (r) => <ApplicantCell provider={r.provider} email={r.email} verified={r.email_verified} />,
            },
            // Only All mixes statuses; any other filter names the one shown.
            ...(status === 'all'
              ? [{ key: 'status', label: 'Status', cell: (r: AdminApplicationRow) => <StatusMark status={r.status} /> }]
              : []),
          ]}
        />
      )}
      {next && (
        <button type="button" class="admin-button" onClick={loadMore} disabled={loadingMore}>
          More applications
        </button>
      )}
      {moreError !== null && <ErrorPanel error={moreError} what="More applications" />}
    </div>
  );
}
