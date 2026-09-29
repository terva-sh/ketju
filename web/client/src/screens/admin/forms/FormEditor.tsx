import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet } from '../../../api/client';
import type { Form, FormAdmin } from '../../../api/dto';
import { isAbort, isProblem } from '../../../api/problem';
import { useResource } from '../../../api/resource';
import { ErrorPanel } from '../../../components/ErrorPanel';
import { FormView, initialValues, type Values } from '../../../forms/FormView';
import { fieldMessage } from '../../../forms/messages';
import { when } from '../../apply/when';
import { ConfirmButton } from '../common/ConfirmButton';
import { Field } from '../common/Field';
import { send } from '../common/send';
import { Table } from '../common/Table';
import {
  addField,
  attach,
  fieldTypes,
  fromSchema,
  moveField,
  removeField,
  toSchema,
  typeLabel,
  updateField,
  type Attached,
  type Draft,
  type Unplaced,
} from './draft';
import { FieldEditor, words, type Change } from './FieldEditor';
import './editor.css';

// FormEditor is /admin/forms (TKT-01M29CP2P1): the registration form's
// draft beside the server's preview of it, and the versions saved so far.
// Admins only; the section list offers it to no one else (sections.ts), and
// the routes answer 403 to anyone else.
//
// Each save writes a new version and makes it active. The old one stays,
// for the applications answered on it (docs/05 "Forms"). The server is the
// authority on the schema's rules: the editor sends the draft to be
// previewed 500 ms after each change, and Save is offered only once the
// preview of the draft as it now stands has passed. What the server refuses
// is placed on the controls it names (draft.ts attach); what names no
// control is said in the editor's own lead line above the draft.

const formUrl = '/api/v1/admin/forms/registration';
export const previewDelay = 500;

const none: Attached = { placed: {}, unplaced: [] };

// Preview is the server's answer about one revision of the draft: the form
// as an applicant would see it, or the words for its refusal.
interface Preview {
  rev: number;
  form?: Form;
  refused?: string;
}

export function FormEditor() {
  const res = useResource(formUrl, (signal) => apiGet<FormAdmin>(formUrl, { signal }));
  if (!res.data) {
    return res.error ? (
      <ErrorPanel error={res.error} what="The form" onRetry={res.retry} />
    ) : (
      <p class="admin-muted" aria-live="polite">
        Loading…
      </p>
    );
  }
  return <Editor loaded={res.data} />;
}

function Editor({ loaded }: { loaded: FormAdmin }) {
  const [admin, setAdmin] = useState(loaded);
  const [draft, setDraft] = useState(() => fromSchema(loaded.schema));
  // rev counts the edits since the draft was seeded; 0 is a draft nobody has
  // changed, which the active version's own form previews.
  const [rev, setRev] = useState(0);
  const [preview, setPreview] = useState<Preview>({ rev: 0, form: loaded.form });
  const [attached, setAttached] = useState<Attached>(none);
  const [values, setValues] = useState<Values>({});
  const [phone, setPhone] = useState(false);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [notice, setNotice] = useState('');
  const [newType, setNewType] = useState(fieldTypes[0]);
  const root = useRef<HTMLDivElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const conflictRef = useRef<HTMLDivElement>(null);
  const leadRef = useRef<HTMLDivElement>(null);
  // focusNext is where focus goes after the next render: the first of the
  // named controls of field id that is there and enabled, or the add
  // control when the field is gone.
  const focusNext = useRef<{ id: number; what: string[] } | null>(null);
  const focusLead = useRef(false);

  const reseed = (a: FormAdmin) => {
    setAdmin(a);
    setDraft(fromSchema(a.schema));
    setRev(0);
    setPreview({ rev: 0, form: a.form });
    setAttached(none);
    setConflict(false);
    setSaveError('');
  };

  // edit changes the draft, and clears what was said about the control
  // changed: the reader is correcting it, and the next preview says again
  // whatever still stands.
  const edit = (next: Draft, cleared?: string) => {
    setDraft(next);
    setRev((r) => r + 1);
    setNotice('');
    setSaveError('');
    if (cleared && attached.placed[cleared]) {
      const placed = { ...attached.placed };
      delete placed[cleared];
      setAttached({ ...attached, placed });
    }
  };

  // The preview of each revision, 500 ms after the edit that made it. An
  // edit meanwhile starts the wait again and aborts the request in flight,
  // so only the newest draft's answer is ever shown.
  useEffect(() => {
    if (rev === 0) return;
    const ctl = new AbortController();
    const sent = draft;
    const timer = setTimeout(async () => {
      try {
        const r = await send<Form>('POST', `${formUrl}/preview`, { schema: toSchema(sent) }, { signal: ctl.signal });
        if (ctl.signal.aborted) return;
        if (r.ok) {
          setPreview({ rev, form: r.value });
          setAttached(none);
        } else {
          setPreview({ rev, refused: r.message });
          setAttached(attach(r.error.fieldErrors, sent));
        }
      } catch {
        // Aborted by a newer edit: send rejects for nothing else.
      }
    }, previewDelay);
    return () => {
      clearTimeout(timer);
      ctl.abort();
    };
  }, [rev]);

  useEffect(() => {
    const want = focusNext.current;
    if (want) {
      focusNext.current = null;
      const box = root.current?.querySelector(`[data-fe-id="${want.id}"]`);
      const el = want.what
        .map((w) => box?.querySelector<HTMLButtonElement | HTMLInputElement>(`[data-fe-focus="${w}"]`))
        .find((e) => e && !e.disabled);
      (el ?? root.current?.querySelector<HTMLElement>('[data-fe-add]'))?.focus();
    }
    if (focusLead.current) {
      focusLead.current = false;
      leadRef.current?.focus();
    }
  });
  useEffect(() => {
    if (notice) noticeRef.current?.focus();
  }, [notice]);
  useEffect(() => {
    if (conflict) conflictRef.current?.focus();
  }, [conflict]);

  const checking = rev > 0 && preview.rev !== rev;
  const passed = rev > 0 && preview.rev === rev && !!preview.form;
  const canSave = passed && !conflict;

  const save = async () => {
    if (saving || !canSave) return;
    setSaving(true);
    setSaveError('');
    const sent = draft;
    const r = await send<FormAdmin>('POST', `${formUrl}/versions`, {
      base_version: admin.active_version,
      schema: toSchema(sent),
    });
    setSaving(false);
    if (r.ok) {
      reseed(r.value);
      setNotice(`Saved. Version ${r.value.active_version} is now the form applicants see.`);
      return;
    }
    if (isProblem(r.error, 'conflict')) {
      setConflict(true);
      return;
    }
    if (isProblem(r.error, 'validation-failed')) {
      setPreview({ rev, refused: r.message });
      setAttached(attach(r.error.fieldErrors, sent));
      focusLead.current = true;
      return;
    }
    setSaveError(r.message);
  };

  // reload discards the draft for the version someone else saved. A failure
  // rejects, and the confirmation says why and stays open.
  const reload = async () => {
    const a = await apiGet<FormAdmin>(formUrl);
    reseed(a);
    setNotice(`Loaded version ${a.active_version}. Your draft was discarded.`);
  };

  const onField = (id: number) => (control: string, change: Change) =>
    edit(updateField(draft, id, change), `${id}.${control}`);
  const onMove = (id: number) => (by: -1 | 1) => {
    focusNext.current = { id, what: by < 0 ? ['up', 'down'] : ['down', 'up'] };
    edit(moveField(draft, id, by));
  };
  const onRemove = (id: number) => () => {
    const i = draft.fields.findIndex((x) => x.id === id);
    const after = draft.fields[i + 1] ?? draft.fields[i - 1];
    if (after) focusNext.current = { id: after.id, what: ['label'] };
    else focusNext.current = { id: -1, what: [] };
    edit(removeField(draft, id));
  };
  const onAdd = () => {
    const [next, id] = addField(draft, newType);
    focusNext.current = { id, what: ['label'] };
    edit(next);
  };

  const errorsOf = (id: number) => {
    const out: Record<string, (typeof attached.placed)[string] | undefined> = {};
    for (const [k, v] of Object.entries(attached.placed)) {
      if (k.startsWith(`${id}.`)) out[k.slice(String(id).length + 1)] = v;
    }
    return out;
  };
  const refused = preview.rev === rev && preview.refused !== undefined;
  const placedCount = Object.keys(attached.placed).length;

  let saveHint = '';
  if (conflict) saveHint = '';
  else if (rev === 0) saveHint = 'Nothing has changed since this version was saved.';
  else if (checking) saveHint = 'Checking the draft…';
  else if (refused) saveHint = 'The draft cannot be saved until the server accepts it.';

  return (
    <div class="form-editor" ref={root}>
      <p class="admin-muted" data-editing>
        Editing version {admin.active_version}, the form applicants see now. Saving writes version{' '}
        {admin.active_version + 1}; applications already sent keep the version they answered.
      </p>
      {notice && (
        <p class="fe-notice" role="status" tabIndex={-1} ref={noticeRef} data-notice>
          {notice}
        </p>
      )}
      <div class="fe-split">
        <section class="fe-draft" aria-labelledby="fe-draft-title">
          <h2 id="fe-draft-title">Draft</h2>
          {refused && (
            <div class="fe-lead" role="alert" tabIndex={-1} ref={leadRef} data-lead>
              <p>
                {preview.refused}
                {placedCount > 0 && ' The problems are marked on the fields below.'}
              </p>
              {attached.unplaced.length > 0 && (
                <ul>
                  {attached.unplaced.map((u) => (
                    <li key={u.path}>
                      {where(u)}: {fieldMessage(u.code)}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
          <fieldset class="fe-body" disabled={saving}>
            <Field label="Title" hint="The heading of the form." error={words(attached.placed.title)}>
              {(c) => (
                <input
                  {...c}
                  type="text"
                  value={draft.title}
                  onInput={(e) => edit({ ...draft, title: (e.currentTarget as HTMLInputElement).value }, 'title')}
                />
              )}
            </Field>
            <Field
              label="Introduction (optional)"
              hint="Markdown, shown above the fields."
              error={words(attached.placed.intro)}
            >
              {(c) => (
                <textarea
                  {...c}
                  rows={3}
                  value={draft.intro}
                  onInput={(e) => edit({ ...draft, intro: (e.currentTarget as HTMLTextAreaElement).value }, 'intro')}
                />
              )}
            </Field>
            {draft.fields.map((x, i) => (
              <FieldEditor
                key={x.id}
                field={x}
                at={i}
                count={draft.fields.length}
                errors={errorsOf(x.id)}
                onChange={onField(x.id)}
                onMove={onMove(x.id)}
                onRemove={onRemove(x.id)}
              />
            ))}
            <div class="admin-inline fe-add">
              <Field label="Type of the new field">
                {(c) => (
                  <select
                    {...c}
                    data-fe-add
                    value={newType}
                    onChange={(e) => setNewType((e.currentTarget as HTMLSelectElement).value)}
                  >
                    {fieldTypes.map((t) => (
                      <option key={t} value={t}>
                        {typeLabel(t)}
                      </option>
                    ))}
                  </select>
                )}
              </Field>
              <button type="button" class="admin-button" onClick={onAdd}>
                Add field
              </button>
            </div>
          </fieldset>
          {conflict && (
            <div class="fe-conflict" role="alert" tabIndex={-1} ref={conflictRef} data-conflict>
              <p>
                Someone saved a newer version of this form while you were editing. Your draft is still here, but it
                cannot be saved over theirs.
              </p>
              <ConfirmButton
                label="Load the newest version"
                prompt="Discard your draft and load the newest version?"
                confirmLabel="Discard and load"
                danger
                onConfirm={reload}
              />
            </div>
          )}
          <div class="admin-actions fe-save">
            <button
              type="button"
              class="admin-button fe-save-button"
              disabled={!canSave && !saving}
              aria-disabled={saving || undefined}
              aria-describedby="fe-save-hint"
              onClick={save}
            >
              {saving ? 'Saving…' : `Save as version ${admin.active_version + 1}`}
            </button>
            <span id="fe-save-hint" class="admin-muted" aria-live="polite">
              {saveHint}
            </span>
            {saveError && (
              <p class="admin-error" role="alert">
                {saveError}
              </p>
            )}
          </div>
        </section>

        <section class="fe-preview" aria-labelledby="fe-preview-title">
          <div class="fe-preview-head">
            <h2 id="fe-preview-title">Preview</h2>
            <button type="button" class="admin-button" aria-pressed={phone} onClick={() => setPhone(!phone)}>
              Phone width
            </button>
          </div>
          <p class="admin-muted" aria-live="polite" data-preview-state>
            {checking ? 'Updating the preview…' : 'What an applicant sees. Nothing entered here is sent.'}
          </p>
          {refused || !preview.form ? (
            <p class="fe-refused" data-preview-refused>
              The server did not accept this draft, so there is nothing to preview. The problems are marked in the
              draft.
            </p>
          ) : (
            <div class={phone ? 'fe-frame fe-phone' : 'fe-frame'} data-preview>
              <p class="fe-form-title">{preview.form.title}</p>
              <FormView
                form={preview.form}
                mode="preview"
                values={{ ...initialValues(preview.form), ...values }}
                onChange={(k, v) => setValues((s) => ({ ...s, [k]: v }))}
              />
            </div>
          )}
        </section>
      </div>

      <Versions admin={admin} />
    </div>
  );
}

// where is the place a refusal names that has no control: the form as a
// whole, its list of fields, or one field and the property.
function where(u: Unplaced): string {
  if (u.field) {
    const name = `Field ${u.field.at + 1}${u.field.key ? ` (${u.field.key})` : ''}`;
    const prop = u.path.split('.').slice(2).join(' ');
    return prop ? `${name}, ${prop.replaceAll('_', ' ')}` : name;
  }
  if (u.path === '') return 'The form as a whole';
  if (u.path === 'fields') return 'The list of fields';
  return u.path;
}

// Versions is every version saved, newest first, and any one of them open
// read-only in the renderer the apply screen uses. Opening one leaves the
// draft as it is.
function Versions({ admin }: { admin: FormAdmin }) {
  const [open, setOpen] = useState<{ n: number; form?: Form; error?: unknown } | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const box = useRef<HTMLElement>(null);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  useEffect(() => {
    if (open?.form) heading.current?.focus();
  }, [open?.n, !!open?.form]);

  const view = async (n: number) => {
    pending.current?.abort();
    const ctl = new AbortController();
    pending.current = ctl;
    setOpen({ n });
    try {
      const a = await apiGet<FormAdmin>(`${formUrl}/versions/${n}`, { signal: ctl.signal });
      if (!ctl.signal.aborted) setOpen({ n, form: a.form });
    } catch (err) {
      if (!ctl.signal.aborted && !isAbort(err)) setOpen({ n, error: err });
    }
  };

  // close puts focus back on the version's View, so a keyboard user is not
  // left on a button that has gone.
  const close = (n: number) => {
    pending.current?.abort();
    setOpen(null);
    box.current?.querySelector<HTMLElement>(`[data-view="${n}"]`)?.focus();
  };

  return (
    <section class="fe-versions" aria-labelledby="fe-versions-title" ref={box}>
      <h2 id="fe-versions-title">Versions</h2>
      <Table
        caption="Every version saved, newest first"
        rowKey={(v) => v.version}
        rows={admin.versions}
        empty="No versions."
        columns={[
          {
            key: 'version',
            label: 'Version',
            cell: (v) => (
              <span data-version={v.version}>
                {v.version}
                {v.version === admin.active_version && (
                  <>
                    {' '}
                    <span class="admin-mark" data-active>
                      Active
                    </span>
                  </>
                )}
              </span>
            ),
          },
          { key: 'saved', label: 'Saved', cell: (v) => (v.created_by ? when(v.created_at) : 'At install') },
          { key: 'by', label: 'By', cell: (v) => (v.created_by ? `@${v.created_by.handle}` : 'Seeded at install') },
          {
            key: 'view',
            label: 'Open',
            cell: (v) => (
              <button type="button" class="admin-link" data-view={v.version} onClick={() => view(v.version)}>
                View version {v.version}
              </button>
            ),
          },
        ]}
      />
      {open && (
        <section class="fe-view" aria-labelledby="fe-view-title" data-viewing={open.n}>
          <h3 id="fe-view-title" tabIndex={-1} ref={heading}>
            Version {open.n}, read-only
          </h3>
          {open.error ? (
            <ErrorPanel error={open.error} what={`Version ${open.n}`} onRetry={() => view(open.n)} />
          ) : !open.form ? (
            <p class="admin-muted" aria-live="polite">
              Loading…
            </p>
          ) : (
            <>
              <p class="admin-muted">
                As applicants saw it. A saved version cannot be changed; the draft above saves a new one.
              </p>
              <fieldset class="fe-frame fe-readonly" disabled>
                <p class="fe-form-title">{open.form.title}</p>
                <FormView form={open.form} mode="preview" values={initialValues(open.form)} />
              </fieldset>
            </>
          )}
          <p>
            <button type="button" class="admin-button" onClick={() => close(open.n)}>
              Close version {open.n}
            </button>
          </p>
        </section>
      )}
    </section>
  );
}
