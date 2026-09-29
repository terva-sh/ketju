import { useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { Group, GroupDetail, GroupList, GroupMember } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';
import { Table } from './common/Table';

// GroupsTab is /admin/users?tab=groups (TKT-01M29CXK1X): the groups, one
// group's members, and creating, renaming and deleting groups. A membership
// the identity provider made is marked and has no remove button, since the
// next sign-in would undo the edit (docs/05 "Roles and groups"). A group is
// deleted only once it has no members and no permission rows, and the tab
// says what is left to clear.
const groupsUrl = '/api/v1/admin/groups';

export function GroupsTab() {
  const res = useResource(groupsUrl, (signal) => apiGet<GroupList>(groupsUrl, { signal }));
  const [selected, setSelected] = useState<number | null>(null);
  const reload = res.retry;

  return (
    <div class="admin-split">
      <div>
        {res.error && !res.data && <ErrorPanel error={res.error} what="The groups" onRetry={res.retry} />}
        {res.data && (
          <Table
            caption="Groups"
            rows={res.data.groups}
            rowKey={(g) => g.id}
            empty="No groups."
            columns={[
              {
                key: 'name',
                label: 'Name',
                cell: (g) => (
                  <button type="button" class="admin-link" onClick={() => setSelected(g.id)} data-group={g.slug}>
                    {g.name}
                  </button>
                ),
              },
              { key: 'slug', label: 'Slug', cell: (g) => g.slug },
              { key: 'members', label: 'Members', cell: (g) => g.members, numeric: true },
              { key: 'grants', label: 'Permission rows', cell: (g) => g.grants, numeric: true },
              { key: 'system', label: 'Kind', cell: (g) => (g.system ? 'system' : '') },
            ]}
          />
        )}
        <CreateGroup
          onCreated={(g) => {
            reload();
            setSelected(g.id);
          }}
        />
      </div>
      {selected !== null && (
        <GroupPanel
          key={selected}
          id={selected}
          onChanged={reload}
          onDeleted={() => {
            setSelected(null);
            reload();
          }}
        />
      )}
    </div>
  );
}

function CreateGroup({ onCreated }: { onCreated: (g: Group) => void }) {
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [failed, setFailed] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: Event) => {
    e.preventDefault();
    setBusy(true);
    const r = await send<Group>('POST', groupsUrl, { slug, name, description });
    setBusy(false);
    if (!r.ok) {
      setFailed(r.message);
      return;
    }
    setFailed(null);
    setSlug('');
    setName('');
    setDescription('');
    onCreated(r.value);
  };
  return (
    <form class="admin-form" onSubmit={submit} aria-label="New group" data-create-group>
      <h2>New group</h2>
      <Field label="Slug" hint="1 to 40 of a-z, 0-9 and -, starting with a letter or a number. It cannot be changed.">
        {(c) => <input {...c} value={slug} onInput={(e) => setSlug((e.target as HTMLInputElement).value)} />}
      </Field>
      <Field label="Name">
        {(c) => <input {...c} value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />}
      </Field>
      <Field label="Description">
        {(c) => (
          <input {...c} value={description} onInput={(e) => setDescription((e.target as HTMLInputElement).value)} />
        )}
      </Field>
      <button type="submit" class="admin-button" disabled={busy}>
        Create group
      </button>
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </form>
  );
}

function GroupPanel({ id, onChanged, onDeleted }: { id: number; onChanged: () => void; onDeleted: () => void }) {
  const url = `${groupsUrl}/${id}`;
  const res = useResource(url, (signal) => apiGet<GroupDetail>(url, { signal }));
  const [fresh, setFresh] = useState<GroupDetail | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [handle, setHandle] = useState('');
  // The edits in the form, null while a field is as the server has it.
  const [name, setName] = useState<string | null>(null);
  const [description, setDescription] = useState<string | null>(null);
  const d = fresh ?? res.data;
  if (!d) {
    return res.error ? <ErrorPanel error={res.error} what="The group" onRetry={res.retry} /> : null;
  }
  const g = d.group;
  const implicit = g.slug === 'everyone' || g.slug === 'members';

  const changed = (r: { ok: true; value: GroupDetail } | { ok: false; message: string }) => {
    if (!r.ok) {
      setFailed(r.message);
      return false;
    }
    setFailed(null);
    setFresh(r.value);
    onChanged();
    return true;
  };
  const add = async (e: Event) => {
    e.preventDefault();
    if (changed(await send<GroupDetail>('POST', `${url}/members`, { handle: handle.trim() }))) setHandle('');
  };
  const remove = (m: GroupMember) => async () =>
    changed(await send<GroupDetail>('DELETE', `${url}/members/${encodeURIComponent(m.user.user.handle)}`));
  // edit sends the fields that differ from the group as it stands, so an
  // untouched field is not rewritten (review 1416: the description is
  // editable here too).
  const edits: { name?: string; description?: string } = {};
  if (name !== null && name !== g.name) edits.name = name;
  if (description !== null && description !== g.description) edits.description = description;
  const edited = Object.keys(edits).length > 0;
  const edit = async (e: Event) => {
    e.preventDefault();
    if (!edited) return;
    const r = await send<Group>('PATCH', url, edits);
    if (!r.ok) {
      setFailed(r.message);
      return;
    }
    setFailed(null);
    setName(null);
    setDescription(null);
    setFresh({ ...d, group: r.value });
    onChanged();
  };
  const remove_ = async () => {
    const r = await send('DELETE', url);
    if (!r.ok) {
      setFailed(r.message);
      return false;
    }
    onDeleted();
  };

  // What stands between the group and its deletion, in words.
  const blockers: string[] = [];
  if (g.members > 0) blockers.push(`${g.members} member${g.members === 1 ? '' : 's'}`);
  if (g.grants > 0) blockers.push(`${g.grants} permission row${g.grants === 1 ? '' : 's'}`);

  return (
    <section class="admin-detail" aria-label={`Group ${g.name}`} data-group-detail={g.slug}>
      <h2>
        {g.name} <span class="admin-muted">{g.slug}</span>
      </h2>
      {g.description && <p>{g.description}</p>}

      <form class="admin-form" onSubmit={edit} aria-label="Edit the group">
        <Field label="Name">
          {(c) => (
            <input {...c} value={name ?? g.name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />
          )}
        </Field>
        <Field label="Description">
          {(c) => (
            <input
              {...c}
              value={description ?? g.description}
              onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
            />
          )}
        </Field>
        {edited && (
          <button type="submit" class="admin-button">
            Save
          </button>
        )}
      </form>

      <h3>Members</h3>
      {implicit ? (
        <p class="admin-muted">Membership of {g.name} is implicit: nobody is added or removed by hand.</p>
      ) : (
        <>
          <Table
            caption={`Members of ${g.name}`}
            rows={d.members}
            rowKey={(m) => m.user.id}
            empty="No members."
            columns={[
              { key: 'handle', label: 'Handle', cell: (m) => m.user.user.handle },
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
              {
                key: 'remove',
                label: 'Change',
                cell: (m) =>
                  m.source === 'idp' ? null : (
                    <ConfirmButton
                      label="Remove"
                      prompt={`Remove ${m.user.user.handle} from ${g.name}?`}
                      onConfirm={remove(m)}
                    />
                  ),
              },
            ]}
          />
          <form class="admin-form admin-inline" onSubmit={add} aria-label="Add a member">
            <Field label="Add a member by handle">
              {(c) => <input {...c} value={handle} onInput={(e) => setHandle((e.target as HTMLInputElement).value)} />}
            </Field>
            <button type="submit" class="admin-button" disabled={!handle.trim()}>
              Add
            </button>
          </form>
        </>
      )}

      <h3>Delete</h3>
      {g.system ? (
        <p class="admin-muted" data-delete-blocked>
          System groups cannot be deleted.
        </p>
      ) : blockers.length > 0 ? (
        <p class="admin-muted" data-delete-blocked>
          Remove its {blockers.join(' and ')} before deleting it.
        </p>
      ) : (
        <ConfirmButton label="Delete group" danger prompt={`Delete ${g.name}? This cannot be undone.`} onConfirm={remove_} />
      )}
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </section>
  );
}
