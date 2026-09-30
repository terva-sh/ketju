import { useLocation } from 'preact-iso';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { AdminUser, AdminUserDetail, AdminUserList, AgentInfo, CreatedAgent, IssuedCredential } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { viewer } from '../../state/viewer';
import { AgentPanel } from './AgentPanel';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';
import { Table } from './common/Table';
import { GroupsTab } from './GroupsTab';
import { NewAgentForm } from './NewAgentForm';
import { PermissionsTab } from './PermissionsTab';

// UsersScreen is /admin/users (TKT-01M29CXK1X): the accounts, and for an
// admin the groups and the permission grid as its ?tab=groups and
// ?tab=permissions tabs, which keeps docs/07's route table as it is.
//
// A moderator sees the list and an account's detail, read-only and without
// its identities, because suspension is theirs and is reached from here.
// Roles, groups and permissions are an admin's; the server refuses them to
// anyone else whatever this screen offers.
export const roles = ['member', 'moderator', 'admin'];
const statuses = ['active', 'pending', 'suspended', 'deactivated'];
const kinds = ['human', 'agent'];

type Tab = 'users' | 'groups' | 'permissions';
const tabs: { tab: Tab; title: string; href: string }[] = [
  { tab: 'users', title: 'Users', href: '/admin/users' },
  { tab: 'groups', title: 'Groups', href: '/admin/users?tab=groups' },
  { tab: 'permissions', title: 'Permissions', href: '/admin/users?tab=permissions' },
];

export function UsersScreen() {
  const { query } = useLocation();
  const admin = viewer.value.user?.role === 'admin';
  const asked = query.tab as Tab | undefined;
  const tab: Tab = admin && (asked === 'groups' || asked === 'permissions') ? asked : 'users';
  return (
    <div class="admin-users">
      {admin && (
        <nav class="admin-tabs" aria-label="Users, groups and permissions">
          <ul>
            {tabs.map((t) => (
              <li key={t.tab}>
                <a href={t.href} aria-current={t.tab === tab ? 'page' : undefined}>
                  {t.title}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      )}
      {tab === 'users' && <UsersTab admin={admin} />}
      {tab === 'groups' && <GroupsTab />}
      {tab === 'permissions' && <PermissionsTab />}
    </div>
  );
}

// when is a timestamp as a date, or a dash.
export function when(ms: number | null): string {
  return ms === null ? '—' : new Date(ms).toISOString().slice(0, 10);
}

function listUrl(f: Filters, cursor = ''): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) if (v) q.set(k, v);
  if (cursor) q.set('cursor', cursor);
  const s = q.toString();
  return '/api/v1/admin/users' + (s ? '?' + s : '');
}

interface Filters {
  prefix: string;
  role: string;
  status: string;
  kind: string;
}

function UsersTab({ admin }: { admin: boolean }) {
  const [filters, setFilters] = useState<Filters>({ prefix: '', role: '', status: '', kind: '' });
  const [more, setMore] = useState<AdminUserList[]>([]);
  const [moreError, setMoreError] = useState<unknown>(null);
  const [selected, setSelected] = useState<string | null>(null);
  // created is the answer to creating an agent, until the new agent's
  // detail has taken it: its first detail, and the token it may carry,
  // which AgentPanel copies into its own state as it mounts and then clears
  // here (TKT-01M3QH3GS2). Choosing any account, or none, drops it too, so a
  // token never waits here for a detail that is not coming.
  const [created, setCreated] = useState<CreatedAgent | null>(null);
  const tab = useRef<HTMLDivElement>(null);
  const closed = useRef<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  // list counts the lists this tab has shown: a filter change starts a new
  // one, and a More answer for an older list is dropped rather than
  // appended to a list it does not belong to (Terva review 1414, finding
  // 3). The pending request is aborted as well.
  const list = useRef(0);
  const pending = useRef<AbortController | null>(null);
  const url = listUrl(filters);
  const res = useResource(url, (signal) => apiGet<AdminUserList>(url, { signal }));

  // restart begins a new list: later pages are dropped and a More still
  // pending is aborted.
  const restart = () => {
    list.current++;
    pending.current?.abort();
    setMore([]);
    setMoreError(null);
    setLoadingMore(false);
  };
  const setFilter = (k: keyof Filters, v: string) => {
    restart();
    setFilters((f) => ({ ...f, [k]: v }));
  };
  // reload asks for the list again after a change it shows, a role, so the
  // row is not left with the old one or under a filter it no longer
  // matches (Terva review 1419).
  const reload = () => {
    restart();
    res.retry();
  };
  const pages = res.data ? [res.data, ...more] : [];
  const users = pages.flatMap((p) => p.users);
  const next = pages.length ? pages[pages.length - 1].next_cursor : '';
  const loadMore = async () => {
    const mine = list.current;
    const ctl = new AbortController();
    pending.current = ctl;
    setMoreError(null);
    setLoadingMore(true);
    try {
      const page = await apiGet<AdminUserList>(listUrl(filters, next), { signal: ctl.signal });
      if (mine === list.current) setMore((m) => [...m, page]);
    } catch (err) {
      if (mine === list.current && !ctl.signal.aborted) setMoreError(err);
    } finally {
      if (mine === list.current) setLoadingMore(false);
    }
  };

  const choose = (handle: string | null) => {
    setCreated(null);
    setSelected(handle);
  };
  const onCreated = (c: CreatedAgent) => {
    reload();
    setCreated(c);
    setSelected(c.user.user.user.handle);
  };
  // Closing the detail returns focus to its row, when the list still has it.
  const close = () => {
    closed.current = selected;
    choose(null);
  };
  useEffect(() => {
    const h = closed.current;
    closed.current = null;
    if (selected === null && h !== null) {
      tab.current?.querySelector<HTMLElement>(`[data-user="${CSS.escape(h)}"]`)?.focus();
    }
  }, [selected]);
  const handover = created && created.user.user.user.handle === selected ? created : null;

  const pick = (u: AdminUser) => (
    <button type="button" class="admin-link" onClick={() => choose(u.user.handle)} data-user={u.user.handle}>
      {u.user.handle}
    </button>
  );
  const select = (label: string, k: keyof Filters, options: string[]) => (
    <Field label={label}>
      {(c) => (
        <select {...c} value={filters[k]} onChange={(e) => setFilter(k, (e.target as HTMLSelectElement).value)}>
          <option value="">Any</option>
          {options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      )}
    </Field>
  );

  return (
    <div class="admin-split" ref={tab}>
      <div>
        {admin && <NewAgentForm onCreated={onCreated} />}
        <form class="admin-filters" onSubmit={(e) => e.preventDefault()} aria-label="Filter accounts">
          <Field label="Handle starts with">
            {(c) => (
              <input
                {...c}
                type="search"
                value={filters.prefix}
                onInput={(e) => setFilter('prefix', (e.target as HTMLInputElement).value)}
              />
            )}
          </Field>
          {select('Role', 'role', roles)}
          {select('Status', 'status', statuses)}
          {select('Kind', 'kind', kinds)}
        </form>
        {res.error && !res.data && <ErrorPanel error={res.error} what="The accounts" onRetry={res.retry} />}
        {res.data && (
          <Table
            caption="Accounts"
            rows={users}
            rowKey={(u) => u.id}
            empty="No account matches."
            columns={[
              { key: 'handle', label: 'Handle', cell: pick },
              { key: 'name', label: 'Name', cell: (u) => u.user.display_name },
              { key: 'role', label: 'Role', cell: (u) => u.user.role },
              { key: 'kind', label: 'Kind', cell: (u) => u.user.kind },
              { key: 'status', label: 'Status', cell: (u) => <span data-status={u.status}>{u.status}</span> },
              { key: 'seen', label: 'Last seen', cell: (u) => when(u.last_seen_at) },
            ]}
          />
        )}
        {next && (
          <button type="button" class="admin-button" onClick={loadMore} disabled={loadingMore}>
            More accounts
          </button>
        )}
        {moreError !== null && <ErrorPanel error={moreError} what="More accounts" />}
      </div>
      {selected && (
        <UserDetail
          key={selected}
          handle={selected}
          admin={admin}
          onRoleChanged={reload}
          onClose={close}
          initial={handover?.user ?? null}
          issued={handover?.credential ?? null}
          onIssuedTaken={() => setCreated(null)}
        />
      )}
    </div>
  );
}

function UserDetail({
  handle,
  admin,
  onRoleChanged,
  onClose,
  initial,
  issued,
  onIssuedTaken,
}: {
  handle: string;
  admin: boolean;
  onRoleChanged: () => void;
  onClose: () => void;
  // initial is a detail already in hand, the creation answer's; issued is
  // the token that came with it, for AgentPanel. Both are read on mount.
  initial: AdminUserDetail | null;
  issued: IssuedCredential | null;
  onIssuedTaken: () => void;
}) {
  const url = `/api/v1/admin/users/${encodeURIComponent(handle)}`;
  const res = useResource(url, (signal) => apiGet<AdminUserDetail>(url, { signal }));
  const [fresh, setFresh] = useState<AdminUserDetail | null>(initial);
  const title = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    // A new agent's detail takes focus from the form that made it; with a
    // token, the token panel takes it instead.
    if (initial && !issued) title.current?.focus();
  }, []);
  const [role, setRole] = useState<string | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const d = fresh ?? res.data;
  // shown is the detail on screen now, for a change that answers after the
  // render that started it. Every change applies its answer to that, by the
  // fields it made, so changes in flight together do not undo each other on
  // screen: a role answer keeps a token issued meanwhile, and a token
  // answer keeps the role (Terva reviews 1507 and 1508).
  const shown = useRef(d);
  shown.current = d;
  const update = (change: (d: AdminUserDetail) => AdminUserDetail) =>
    setFresh((prev) => {
      const now = prev ?? shown.current;
      return now ? change(now) : prev;
    });
  const close = (
    <button type="button" class="admin-button" onClick={onClose}>
      Close
    </button>
  );

  // An account that has not loaded, or could not be, can still be closed
  // (Terva review 1507, finding 2).
  if (!d) {
    return (
      <section class="admin-detail" aria-label={`Account ${handle}`} aria-busy={!res.error} data-detail-pending={handle}>
        <div class="admin-detail-head">
          <h2>@{handle}</h2>
          {close}
        </div>
        {res.error && <ErrorPanel error={res.error} what="The account" onRetry={res.retry} />}
      </section>
    );
  }
  const u = d.user;
  const chosen = role ?? u.user.role;
  const changeRole = async () => {
    const r = await send<AdminUserDetail>('PUT', `${url}/role`, { role: chosen });
    if (!r.ok) {
      setFailed(r.message);
      return false;
    }
    setFailed(null);
    update((now) => ({ ...r.value, agent: now.agent }));
    setRole(null);
    onRoleChanged();
  };
  const removeFrom = (groupID: number) => async () => {
    const r = await send('DELETE', `/api/v1/admin/groups/${groupID}/members/${encodeURIComponent(handle)}`);
    if (!r.ok) {
      setFailed(r.message);
      return false;
    }
    setFailed(null);
    update((now) => ({ ...now, groups: now.groups.filter((g) => g.group.id !== groupID) }));
  };

  return (
    <section class="admin-detail" aria-label={`Account ${u.user.handle}`} data-detail={u.user.handle}>
      <div class="admin-detail-head">
        <h2 ref={title} tabIndex={-1}>
          {u.user.display_name || u.user.handle} <span class="admin-muted">@{u.user.handle}</span>
        </h2>
        {close}
      </div>
      <dl class="admin-facts">
        <dt>Status</dt>
        <dd data-status={u.status}>{u.status}</dd>
        <dt>Kind</dt>
        <dd>{u.user.kind}</dd>
        <dt>Role</dt>
        <dd data-role>{u.user.role}</dd>
        <dt>Joined</dt>
        <dd>{when(u.created_at)}</dd>
        {d.sessions !== null && (
          <>
            <dt>Live sessions</dt>
            <dd data-sessions>{d.sessions}</dd>
          </>
        )}
      </dl>

      {admin && (
        <div class="admin-role">
          <Field label="Role" hint="A change applies from the account's next request. The last active admin cannot be demoted.">
            {(c) => (
              <select {...c} value={chosen} onChange={(e) => setRole((e.target as HTMLSelectElement).value)}>
                {roles.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {chosen !== u.user.role && (
            <ConfirmButton
              label="Change role"
              prompt={`Make ${u.user.handle} ${chosen === 'admin' ? 'an' : 'a'} ${chosen}?`}
              onConfirm={changeRole}
            />
          )}
        </div>
      )}
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}

      {admin && d.agent !== null && (
        <AgentPanel
          d={d as AdminUserDetail & { agent: AgentInfo }}
          url={url}
          onDetail={update}
          issued={issued}
          onIssuedTaken={onIssuedTaken}
        />
      )}

      <h3>Groups</h3>
      <Table
        caption={`Groups ${u.user.handle} is in`}
        rows={d.groups}
        rowKey={(m) => m.group.id}
        empty="No groups beyond members."
        columns={[
          { key: 'group', label: 'Group', cell: (m) => m.group.name },
          {
            key: 'source',
            label: 'From',
            cell: (m) =>
              m.source === 'idp' ? (
                <span class="admin-mark" data-idp title="Re-evaluated at every sign-in; not editable here">
                  identity provider
                </span>
              ) : (
                <span>{m.added_by ? `added by ${m.added_by.handle}` : 'added by hand'}</span>
              ),
          },
          ...(admin
            ? [
                {
                  key: 'remove',
                  label: 'Change',
                  cell: (m: AdminUserDetail['groups'][number]) =>
                    m.source === 'idp' ? null : (
                      <ConfirmButton
                        label="Remove"
                        prompt={`Remove ${u.user.handle} from ${m.group.name}?`}
                        onConfirm={removeFrom(m.group.id)}
                      />
                    ),
                },
              ]
            : []),
        ]}
      />

      {d.identities !== null && (
        <>
          <h3>Identities</h3>
          <Table
            caption={`How ${u.user.handle} signs in`}
            rows={d.identities}
            rowKey={(i) => i.provider + (i.email ?? '')}
            empty="No identity signs this account in."
            columns={[
              { key: 'provider', label: 'Provider', cell: (i) => i.provider },
              {
                key: 'email',
                label: 'Email',
                cell: (i) => (i.email ? `${i.email}${i.email_verified ? '' : ' (unverified)'}` : '—'),
              },
              { key: 'login', label: 'Last sign-in', cell: (i) => when(i.last_login_at) },
            ]}
          />
        </>
      )}
    </section>
  );
}
