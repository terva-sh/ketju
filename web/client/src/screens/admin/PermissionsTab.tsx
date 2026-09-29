import { useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { Decisions, PermissionGrid, PermissionPreview } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';

// PermissionsTab is /admin/users?tab=permissions (TKT-01M29CXK1X): the
// grid of groups against forums for one permission at a time, with the
// site-wide column first.
//
// A permission grid is where an admin quietly locks everyone out of a
// forum, themselves included, so changes are staged, not sent: each staged
// cell is marked, a member's effective permissions can be previewed with the
// staged changes laid over the grid before anything is written, and Apply
// sends the whole batch, which the server applies in one transaction or not
// at all.
export const permissions = ['read', 'create_thread', 'reply', 'upload', 'moderate'] as const;
const gridUrl = '/api/v1/admin/permissions';

// A cell: forum id, or 0 for site-wide, group id, and permission.
const cellKey = (forum: number | null, group: number, p: string) => `${forum ?? 0}:${group}:${p}`;

export interface Change {
  forum_id: number | null;
  group_id: number;
  permission: string;
  grant: boolean;
}

export function PermissionsTab() {
  const res = useResource(gridUrl, (signal) => apiGet<PermissionGrid>(gridUrl, { signal }));
  const [fresh, setFresh] = useState<PermissionGrid | null>(null);
  const [permission, setPermission] = useState<string>('read');
  // staged maps a cell to the state it will have: true granted.
  const [staged, setStaged] = useState<Map<string, Change>>(new Map());
  const [handle, setHandle] = useState('');
  const [preview, setPreview] = useState<PermissionPreview | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  // applying is true while a batch is with the server. The grid is not
  // editable then, so nothing is staged that the answer could discard
  // (Terva review 1414, finding 1).
  const [applying, setApplying] = useState(false);
  // asked counts what a preview answer could be about: the staged batch,
  // the handle and the grid. Anything that changes one of them moves it on,
  // and a preview answer arriving after that is about something no longer
  // on screen, so it is dropped (review 1414, finding 2).
  const asked = useRef(0);
  const grid = fresh ?? res.data;

  if (!grid) {
    return res.error ? <ErrorPanel error={res.error} what="The permission grid" onRetry={res.retry} /> : null;
  }
  const granted = new Set(grid.grants.map((g) => cellKey(g.forum_id, g.group_id, g.permission)));
  const changes = [...staged.values()];
  const stale = () => {
    asked.current++;
    setPreview(null);
  };

  const toggle = (forum: number | null, group: number) => {
    if (applying) return;
    const k = cellKey(forum, group, permission);
    const next = new Map(staged);
    if (next.has(k)) next.delete(k);
    else next.set(k, { forum_id: forum, group_id: group, permission, grant: !granted.has(k) });
    setStaged(next);
    stale();
  };
  const discard = () => {
    setStaged(new Map());
    stale();
    setFailed(null);
  };
  const runPreview = async (e: Event) => {
    e.preventDefault();
    if (applying) return;
    const mine = ++asked.current;
    const r = await send<PermissionPreview>('POST', `${gridUrl}/preview`, { handle: handle.trim(), changes });
    if (mine !== asked.current) return;
    if (!r.ok) {
      setFailed(r.message);
      setPreview(null);
      return;
    }
    setFailed(null);
    setPreview(r.value);
  };
  const apply = async () => {
    const batch = changes;
    setApplying(true);
    stale();
    let r;
    try {
      r = await send<PermissionGrid>('POST', gridUrl, { changes: batch });
    } finally {
      setApplying(false);
    }
    if (!r.ok) {
      setFailed(r.message);
      return false;
    }
    setFailed(null);
    setFresh(r.value);
    // The grid changed under any preview asked for meanwhile (review 1416).
    stale();
    // Only what was sent is unstaged: the grid was locked meanwhile, but
    // clearing the whole map would lose anything staged all the same.
    const sent = new Set(batch.map((c) => cellKey(c.forum_id, c.group_id, c.permission)));
    setStaged((s) => new Map([...s].filter(([k]) => !sent.has(k))));
  };

  const columns: { id: number | null; name: string }[] = [
    { id: null, name: 'Everywhere' },
    ...grid.forums.map((f) => ({ id: f.id, name: f.name })),
  ];
  const label = (p: string) => p.replace('_', ' ');

  return (
    <div class="admin-permissions">
      <p class="admin-hint">
        A group with any row on a forum is governed there by those rows alone; otherwise its Everywhere rows
        apply. Admins pass every check. Applied changes take effect from each member's next request.
      </p>
      <Field label="Permission">
        {(c) => (
          <select {...c} value={permission} onChange={(e) => setPermission((e.target as HTMLSelectElement).value)}>
            {permissions.map((p) => (
              <option key={p} value={p}>
                {label(p)}
              </option>
            ))}
          </select>
        )}
      </Field>

      <div class="admin-table admin-grid">
        <table>
          <caption>
            Who may {label(permission)}, by group and forum
          </caption>
          <thead>
            <tr>
              <th scope="col">Group</th>
              {columns.map((c) => (
                <th key={c.id ?? 0} scope="col">
                  {c.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {grid.groups.map((g) => (
              <tr key={g.id}>
                <th scope="row">{g.name}</th>
                {columns.map((c) => {
                  const k = cellKey(c.id, g.id, permission);
                  const change = staged.get(k);
                  const on = change ? change.grant : granted.has(k);
                  return (
                    <td key={c.id ?? 0} class={change ? 'staged' : undefined} data-cell={k} data-staged={change ? '' : undefined}>
                      <input
                        type="checkbox"
                        checked={on}
                        disabled={applying}
                        aria-label={`${g.name}: ${label(permission)} in ${c.name}`}
                        onChange={() => toggle(c.id, g.id)}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p data-staged-count>
        {changes.length === 0
          ? 'No changes staged.'
          : `${changes.length} change${changes.length === 1 ? '' : 's'} staged, not yet applied.`}
      </p>

      <form class="admin-form admin-inline" onSubmit={runPreview} aria-label="Preview for a member">
        <Field label="Preview for member" hint="What this account could do in each forum, now and with the staged changes.">
          {(c) => (
            <input
              {...c}
              value={handle}
              onInput={(e) => {
                setHandle((e.target as HTMLInputElement).value);
                stale();
              }}
            />
          )}
        </Field>
        <button type="submit" class="admin-button" disabled={!handle.trim() || applying}>
          Preview
        </button>
      </form>

      {preview && <Preview preview={preview} />}

      {changes.length > 0 && (
        <div class="admin-actions">
          <ConfirmButton
            label={`Apply ${changes.length} change${changes.length === 1 ? '' : 's'}`}
            prompt="Apply them all at once? They take effect from each member's next request."
            onConfirm={apply}
          />
          <button type="button" class="admin-button" onClick={discard} disabled={applying}>
            Discard
          </button>
        </div>
      )}
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </div>
  );
}

function Preview({ preview }: { preview: PermissionPreview }) {
  const u = preview.user.user;
  return (
    <div class="admin-table" data-preview={u.handle}>
      <table>
        <caption>
          What {u.handle} ({u.role}, {preview.user.status}) could do: now, then with the staged changes
        </caption>
        <thead>
          <tr>
            <th scope="col">Forum</th>
            {permissions.map((p) => (
              <th key={p} scope="col">
                {p.replace('_', ' ')}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {preview.forums.map((f) => (
            <tr key={f.forum.id}>
              <th scope="row">{f.forum.name}</th>
              {permissions.map((p) => {
                const before = f.before[p as keyof Decisions];
                const after = f.after[p as keyof Decisions];
                const changed = before !== after;
                return (
                  <td key={p} class={changed ? 'changed' : undefined} data-changed={changed ? '' : undefined}>
                    {changed ? `${before} → ${after}` : after}
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
