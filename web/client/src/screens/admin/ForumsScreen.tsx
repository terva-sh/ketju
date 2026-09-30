import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import type { ManagedForum, ManagedForumList } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { ConfirmButton } from './common/ConfirmButton';
import { Field } from './common/Field';
import { send } from './common/send';

// ForumsScreen is /admin/forums (TKT-01M29CXK19): the forum tree, with a
// form to create a forum, and for each forum its name and description, Move
// up and Move down among its siblings, the root it is placed under, its
// visibility, and archiving. Every change answers with the whole list, which
// the screen redraws from, because a move can renumber the siblings.
//
// Moves are buttons and a select rather than drag and drop, so they work
// from the keyboard, and focus stays on the control that moved the forum.
//
// One change is in flight at a time, across every forum and the create
// form. Each answer is the whole list as its transaction committed it, so
// two changes in flight could answer out of order, and the older list would
// hide the newer change though it committed (Terva review 1574). While one
// is saving, every control that changes something is disabled, and a change
// asked for anyway is refused with a sentence rather than sent.
const forumsUrl = '/api/v1/admin/forums';

// archiveText is what archiving does, in words. It is the same for every
// visibility, because archiving changes no read access: listing who can
// read a restricted forum would mean restating its grants (review 1558 on
// PR #209).
export const archiveText =
  'Anyone who could read it still can. Only posting stops, except for admins. ' +
  'Moderators can still hide, lock, pin and move threads out, and only admins can move threads in.';

// visibilityHint is how soon a visibility change reaches readers: the
// permission cache's lifetime at most.
export const visibilityHint = 'Takes effect within a minute.';

const visibilities: { value: string; label: string }[] = [
  { value: 'public', label: 'Public: anyone, signed in or not' },
  { value: 'members', label: 'Members: every signed-in member' },
  { value: 'restricted', label: 'Restricted: only groups granted read' },
];

// Focus is the control to put focus back on once the list redraws.
interface Focus {
  id: number;
  control: string;
}

// Write sends one change and, when it is made, redraws from its answer.
// It answers null, or the sentence to show beside the control.
type Write = (method: 'POST' | 'PUT' | 'PATCH', url: string, body: unknown, then?: Focus) => Promise<string | null>;

// saving is the sentence for a change asked for while another is in flight.
export const saving = 'Another change is still being saved. Try again once it has finished.';

export function ForumsScreen() {
  const res = useResource(forumsUrl, (signal) => apiGet<ManagedForumList>(forumsUrl, { signal }));
  const [fresh, setFresh] = useState<ManagedForumList | null>(null);
  const [focus, setFocus] = useState<Focus | null>(null);
  // inflight guards against a second change within one render, before
  // busy has disabled the controls; busy is what the controls show.
  const inflight = useRef(false);
  const [busy, setBusy] = useState(false);
  const list = fresh ?? res.data;

  // After a move the row is somewhere else in the tree; put focus back on
  // the control that moved it, or on its other move button when that one
  // is now disabled at the end of the list.
  useEffect(() => {
    if (!focus) return;
    const row = document.querySelector(`[data-forum-id="${focus.id}"]`);
    const want = row?.querySelector<HTMLElement>(`[data-control="${focus.control}"]`);
    const other = focus.control === 'up' ? 'down' : focus.control === 'down' ? 'up' : '';
    const fallback = other ? row?.querySelector<HTMLElement>(`[data-control="${other}"]`) : null;
    const target = want && !(want as HTMLButtonElement).disabled ? want : fallback;
    target?.focus();
    setFocus(null);
  }, [focus, list]);

  if (!list) {
    return res.error ? <ErrorPanel error={res.error} what="The forums" onRetry={res.retry} /> : null;
  }
  const roots = list.forums.filter((f) => f.parent_id === null);
  const under = (id: number) => list.forums.filter((f) => f.parent_id === id);
  const write: Write = async (method, url, body, then) => {
    if (inflight.current) return saving;
    inflight.current = true;
    setBusy(true);
    try {
      const r = await send<ManagedForumList>(method, url, body);
      if (!r.ok) return r.message;
      setFresh(r.value);
      if (then) setFocus(then);
      return null;
    } finally {
      inflight.current = false;
      setBusy(false);
    }
  };

  return (
    <div class="admin-forums">
      {roots.length === 0 ? (
        <p class="admin-empty" data-empty>
          No forums yet.
        </p>
      ) : (
        <ol class="admin-forum-tree" aria-label="Forums">
          {roots.map((root, i) => (
            <li key={root.id}>
              <ForumRow forum={root} prev={roots[i - 1]} next={roots[i + 1]} roots={roots} nested={under(root.id).length} busy={busy} write={write} />
              {under(root.id).length > 0 && (
                <ol aria-label={`Forums under ${root.name}`}>
                  {under(root.id).map((child, j, sibs) => (
                    <li key={child.id}>
                      <ForumRow forum={child} prev={sibs[j - 1]} next={sibs[j + 1]} roots={roots} nested={0} busy={busy} write={write} />
                    </li>
                  ))}
                </ol>
              )}
            </li>
          ))}
        </ol>
      )}
      <CreateForum roots={roots} busy={busy} write={write} />
    </div>
  );
}

function ForumRow({
  forum: f,
  prev,
  next,
  roots,
  nested,
  busy,
  write,
}: {
  forum: ManagedForum;
  prev?: ManagedForum;
  next?: ManagedForum;
  roots: ManagedForum[];
  // nested is how many forums are under this one: a forum with any stays
  // at the top level, since forums nest one level.
  nested: number;
  // busy is true while any change on the screen is saving.
  busy: boolean;
  write: Write;
}) {
  const url = `${forumsUrl}/${f.id}`;
  const [failed, setFailed] = useState<string | null>(null);
  // The edits in the form, null while a field is as the server has it.
  const [name, setName] = useState<string | null>(null);
  const [description, setDescription] = useState<string | null>(null);

  const change = async (method: 'POST' | 'PUT' | 'PATCH', to: string, body: unknown, then?: Focus) => {
    const message = await write(method, to, body, then);
    setFailed(message);
    return message === null;
  };
  const edits: { name?: string; description?: string } = {};
  if (name !== null && name !== f.name) edits.name = name;
  if (description !== null && description !== f.description) edits.description = description;
  const edited = Object.keys(edits).length > 0;
  const save = async (e: Event) => {
    e.preventDefault();
    if (!edited) return;
    if (await change('PATCH', url, edits)) {
      setName(null);
      setDescription(null);
    }
  };
  const move = (body: { parent_id: number | null; before?: number; after?: number }, control: string) =>
    change('POST', `${url}/move`, body, { id: f.id, control });
  const places = roots.filter((r) => r.id !== f.id);

  return (
    <section class="admin-detail admin-forum" aria-label={`Forum ${f.name}`} data-forum={f.slug} data-forum-id={f.id}>
      <h2>
        {f.name} <span class="admin-muted">/f/{f.slug}</span>
        {f.archived && (
          <span class="admin-mark" data-archived>
            archived
          </span>
        )}
      </h2>
      <p class="admin-muted tnum">
        {f.threads} thread{f.threads === 1 ? '' : 's'}, {f.posts} post{f.posts === 1 ? '' : 's'}
      </p>

      <div class="admin-actions">
        <button
          type="button"
          class="admin-button"
          data-control="up"
          disabled={busy || !prev}
          onClick={() => prev && move({ parent_id: f.parent_id, before: prev.id }, 'up')}
        >
          Move up
        </button>
        <button
          type="button"
          class="admin-button"
          data-control="down"
          disabled={busy || !next}
          onClick={() => next && move({ parent_id: f.parent_id, after: next.id }, 'down')}
        >
          Move down
        </button>
      </div>

      <form class="admin-form" onSubmit={save} aria-label={`Edit ${f.name}`}>
        <Field label="Name">
          {(c) => <input {...c} value={name ?? f.name} onInput={(e) => setName((e.target as HTMLInputElement).value)} />}
        </Field>
        <Field label="Description">
          {(c) => (
            <input
              {...c}
              value={description ?? f.description}
              onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
            />
          )}
        </Field>
        {edited && (
          <button type="submit" class="admin-button" disabled={busy}>
            Save
          </button>
        )}
      </form>

      <Field
        label="Place under"
        hint={nested > 0 ? 'It has forums under it, so it stays at the top level.' : undefined}
      >
        {(c) => (
          <select
            {...c}
            data-control="parent"
            value={f.parent_id === null ? '' : String(f.parent_id)}
            disabled={busy || nested > 0}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              move({ parent_id: v === '' ? null : Number(v) }, 'parent');
            }}
          >
            <option value="">Top level</option>
            {places.map((r) => (
              <option key={r.id} value={String(r.id)}>
                {r.name}
              </option>
            ))}
          </select>
        )}
      </Field>

      <Field label="Visibility" hint={visibilityHint}>
        {(c) => (
          <select
            {...c}
            data-control="visibility"
            value={f.visibility}
            disabled={busy}
            onChange={(e) =>
              change('PUT', `${url}/visibility`, { visibility: (e.target as HTMLSelectElement).value }, {
                id: f.id,
                control: 'visibility',
              })
            }
          >
            {visibilities.map((v) => (
              <option key={v.value} value={v.value}>
                {v.label}
              </option>
            ))}
          </select>
        )}
      </Field>

      <div class="admin-field" data-archive>
        <p class="admin-hint" data-archive-text>
          {archiveText}
        </p>
        {f.archived ? (
          <ConfirmButton
            label="Unarchive"
            disabled={busy}
            prompt={`Unarchive ${f.name}? Posting opens again to whoever may post there.`}
            onConfirm={() => change('PUT', `${url}/archived`, { archived: false })}
          />
        ) : (
          <ConfirmButton
            label="Archive"
            disabled={busy}
            prompt={`Archive ${f.name}?`}
            onConfirm={() => change('PUT', `${url}/archived`, { archived: true })}
          />
        )}
      </div>
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </section>
  );
}

function CreateForum({ roots, busy, write }: { roots: ManagedForum[]; busy: boolean; write: Write }) {
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [parent, setParent] = useState('');
  const [visibility, setVisibility] = useState('members');
  const [failed, setFailed] = useState<string | null>(null);
  const submit = async (e: Event) => {
    e.preventDefault();
    const message = await write('POST', forumsUrl, {
      slug,
      name,
      description,
      parent_id: parent === '' ? null : Number(parent),
      visibility,
    });
    setFailed(message);
    if (message === null) {
      setSlug('');
      setName('');
      setDescription('');
    }
  };
  return (
    <form class="admin-form" onSubmit={submit} aria-label="New forum" data-create-forum>
      <h2>New forum</h2>
      <Field
        label="Slug"
        hint="1 to 60 of a-z, 0-9 and -, not starting or ending with -. It is the forum's address, /f/slug, and cannot be changed."
      >
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
      <Field label="Place under">
        {(c) => (
          <select {...c} value={parent} onChange={(e) => setParent((e.target as HTMLSelectElement).value)}>
            <option value="">Top level</option>
            {roots.map((r) => (
              <option key={r.id} value={String(r.id)}>
                {r.name}
              </option>
            ))}
          </select>
        )}
      </Field>
      <Field label="Visibility" hint={visibilityHint}>
        {(c) => (
          <select {...c} value={visibility} onChange={(e) => setVisibility((e.target as HTMLSelectElement).value)}>
            {visibilities.map((v) => (
              <option key={v.value} value={v.value}>
                {v.label}
              </option>
            ))}
          </select>
        )}
      </Field>
      <button type="submit" class="admin-button" disabled={busy}>
        Create forum
      </button>
      {failed && (
        <p class="admin-error" role="alert">
          {failed}
        </p>
      )}
    </form>
  );
}
