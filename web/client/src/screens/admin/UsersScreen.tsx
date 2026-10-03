import { useLocation } from 'preact-iso';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type {
  AdminUser,
  AdminUserDetail,
  AdminUserList,
  AgentInfo,
  CreatedAgent,
  HandleChange,
  IssuedCredential,
} from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { renameViewer } from '../../state/rename';
import { refreshViewer, viewer } from '../../state/viewer';
import { AgentPanel } from './AgentPanel';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';
import { Table } from './common/Table';
import { GroupsTab } from './GroupsTab';
import { NewAgentForm } from './NewAgentForm';
import { PermissionsTab } from './PermissionsTab';
import { RenameControl } from './RenameControl';
import { SuspensionPanel } from './SuspensionPanel';

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

// userHref is the users tab with handle's detail open, or with none: the
// other parameters of the address, such as the prefix the queue's author
// link filtered by, are kept as they are (TKT-01M3P0CYR3).
export function userHref(query: Record<string, string>, handle: string | null): string {
  const q = new URLSearchParams(query);
  if (handle === null) q.delete('user');
  else q.set('user', handle);
  const s = q.toString();
  return '/admin/users' + (s ? '?' + s : '');
}

interface Filters {
  prefix: string;
  role: string;
  status: string;
  kind: string;
}

function UsersTab({ admin }: { admin: boolean }) {
  const { query, route } = useLocation();
  // The prefix starts from the address, so a link to /admin/users?prefix=
  // opens a filtered list; after that the filters live here, and typing does
  // not rewrite the address.
  const [filters, setFilters] = useState<Filters>(() => ({ prefix: query.prefix ?? '', role: '', status: '', kind: '' }));
  const [more, setMore] = useState<AdminUserList[]>([]);
  const [moreError, setMoreError] = useState<unknown>(null);
  // The account whose detail is open is the address's ?user=, so a detail
  // can be linked to and survives a reload, and a rename can replace it
  // (TKT-01M3P0CYR3).
  const selected = query.user || null;
  // created is the answer to creating an agent, until the new agent's
  // detail has taken it: its first detail, and the token it may carry,
  // which AgentPanel copies into its own state as it mounts and then clears
  // here (TKT-01M3QH3GS2). Choosing any account, or none, drops it too, so a
  // token never waits here for a detail that is not coming; and since the
  // detail takes it as it mounts, Back and Forward find nothing to bring
  // back.
  const [created, setCreated] = useState<CreatedAgent | null>(null);
  // renamed is the handle the open account had before an admin renamed it
  // here, for the detail under the new one to say so.
  const [renamed, setRenamed] = useState<{ from: string; to: string } | null>(null);
  const tab = useRef<HTMLDivElement>(null);
  const closed = useRef<string | null>(null);
  // now is the address as this tab last rendered it, and whether it is
  // still mounted, for an answer that arrives after the render that asked.
  const now = useRef({ live: true, query, selected });
  now.current = { live: true, query, selected };
  useEffect(
    () => () => {
      now.current.live = false;
    },
    [],
  );
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
  // A new ?prefix= in the address, by a link or by Back and Forward while
  // this tab stays mounted, becomes the filter too (Terva review 1596).
  // Only a change of the address's prefix does: opening or closing an
  // account keeps it, so what was typed in the field stays.
  const addressPrefix = query.prefix ?? '';
  useEffect(() => {
    if (filters.prefix !== addressPrefix) setFilter('prefix', addressPrefix);
  }, [addressPrefix]);
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
    if (handle === selected) return;
    setCreated(null);
    route(userHref(query, handle));
  };
  const onCreated = (c: CreatedAgent) => {
    reload();
    setCreated(c);
    route(userHref(query, c.user.user.user.handle));
  };
  // A rename asks for the list again, so the row shows the new handle, and
  // replaces the address, so Back does not return to a handle that now
  // redirects.
  //
  // An admin who renamed their own account is still that account: the
  // viewer takes the new handle at once, before the detail under it loads,
  // so nothing that tells the viewer's own account by its handle (as
  // SuspensionPanel's mayAct does) takes it for someone else's; then /me
  // is asked for again.
  //
  // A rename that answers after the admin has left the tab, or opened
  // another account, moves nobody (Terva review 1597): the list is fetched
  // afresh when the tab is next shown, and one still shown is asked for
  // again. The viewer's own handle is corrected either way.
  const onRenamed = (change: HandleChange) => {
    const me = viewer.value;
    if (me.user && me.user.handle.toLowerCase() === change.previous.toLowerCase()) {
      // The admin's own account: their drafts move with it (state/rename.ts).
      renameViewer(change.handle);
      refreshViewer().catch(() => {});
    }
    const { live, query: q, selected: open } = now.current;
    if (!live) return;
    reload();
    if (open?.toLowerCase() !== change.previous.toLowerCase()) return;
    setRenamed({ from: change.previous, to: change.handle });
    setCreated(null);
    route(userHref(q, change.handle), true);
  };
  // Closing the detail returns focus to its row, when the list still has it.
  const close = () => {
    closed.current = selected;
    choose(null);
  };
  useEffect(() => {
    if (renamed && renamed.to !== selected) setRenamed(null);
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
          renamedFrom={renamed && renamed.to === selected ? renamed.from : null}
          onRenamed={onRenamed}
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
  renamedFrom,
  onRenamed,
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
  // renamedFrom is the handle this account had until an admin renamed it
  // here: the detail says so and takes focus once it has loaded.
  renamedFrom: string | null;
  onRenamed: (change: HandleChange) => void;
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
  // After a rename the Rename button focus was on has gone with the old
  // detail, so the new one's heading takes focus once it is there.
  const focusTitle = useRef(renamedFrom !== null);
  useEffect(() => {
    if (d && focusTitle.current) {
      focusTitle.current = false;
      title.current?.focus();
    }
  }, [!!d]);
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
  // refetch fetches the detail again, shows it, and resolves to it, or to
  // null when the fetch fails: SuspensionPanel chooses its words from the
  // account as it is after a change (TKT-01M3QMRE47). No signal is passed,
  // so nothing aborts it.
  const refetch = async () => {
    try {
      const next = await apiGet<AdminUserDetail>(url);
      setFresh(next);
      return next;
    } catch {
      return null;
    }
  };
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
      {renamedFrom && (
        <p class="admin-muted" role="status" data-renamed>
          Renamed from @{renamedFrom}. The old handle is kept for 90 days and sends visitors here.
        </p>
      )}
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
      <SuspensionPanel d={d} url={url} refetch={refetch} onListChanged={onRoleChanged} />

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
      {admin && <RenameControl handle={u.user.handle} url={url} onRenamed={onRenamed} />}
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
