import { useRef, useState } from 'preact/hooks';
import { apiGet } from '../../api/client';
import { isProblem } from '../../api/problem';
import type { ConfigSection, SettingValue, SiteSetting, SiteSettings } from '../../api/dto';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { stamp } from './AuditScreen';
import { Field } from './common/Field';
import { send } from './common/send';

// SettingsScreen is /admin/settings (TKT-01M29CXK2H, docs/07 "Site
// settings"): every runtime setting, drawn from the registry the server
// lists, and the parts of config.toml it does not hold.
//
// The fields are generated, not written per key: a key's kind decides the
// control, a closed list of choices makes it a select, and the registry's
// own words say what it takes. A key added to the registry later appears
// here with no client change.
//
// Each field saves itself, so one refusal names one key. "Saved." follows
// the rule settled for the agent limits form (TKT-01M3QV39H4): any edit
// clears it, and an answer shows it only when the field still holds what
// was sent.
const settingsUrl = '/api/v1/admin/settings';

export const configLine = 'Change these in config.toml and restart ketju.';

export function SettingsScreen() {
  const res = useResource(settingsUrl, (signal) => apiGet<SiteSettings>(settingsUrl, { signal }));
  const l = res.data;
  if (!l) {
    return res.error ? <ErrorPanel error={res.error} what="The site settings" onRetry={res.retry} /> : null;
  }
  return (
    <div class="admin-settings">
      {groups(l.settings).map(([name, keys]) => (
        <section key={name} class="admin-settings-group" data-group={name}>
          <h2>{name}</h2>
          {keys.map((s) => (
            <SettingField key={s.key} setting={s} />
          ))}
        </section>
      ))}
      <ConfigSections sections={l.config} />
    </div>
  );
}

// groups is the settings by the part of their key before the first dot,
// General first for a key with none, then the rest in key order.
export function groups(settings: SiteSetting[]): [string, SiteSetting[]][] {
  const by = new Map<string, SiteSetting[]>();
  for (const s of settings) {
    const dot = s.key.indexOf('.');
    const name = dot < 0 ? 'General' : s.key[0].toUpperCase() + s.key.slice(1, dot);
    by.set(name, [...(by.get(name) ?? []), s]);
  }
  return [...by].sort(([a], [b]) => (a === 'General' ? -1 : b === 'General' ? 1 : a < b ? -1 : a > b ? 1 : 0));
}

// draftOf is a value as its control holds it: text for every kind.
function draftOf(v: SettingValue): string {
  return String(v);
}

// shown is a value as a sentence names it.
function shown(v: SettingValue): string {
  return typeof v === 'string' ? `"${v}"` : String(v);
}

// valueOf is a control's text as the key's JSON, or undefined when an
// integer field holds something that is not a whole number a JSON number
// carries exactly. Past 2^53 Number rounds, and far past it gives Infinity,
// which JSON writes as null, so either would save a value nobody typed
// (Terva review 1595). The range is otherwise the server's to judge, and
// its allowed words to say.
function valueOf(s: SiteSetting, draft: string): SettingValue | undefined {
  if (s.kind === 'boolean') return draft === 'true';
  if (s.kind === 'integer') {
    const t = draft.trim();
    const n = Number(t);
    return /^-?\d+$/.test(t) && Number.isSafeInteger(n) ? n : undefined;
  }
  return draft;
}

// lastChanged says who last set the key, following the row: an admin from
// this screen, the command line, which writes no account, or nobody.
export function lastChanged(s: SiteSetting): string {
  if (s.updated_at === null) return '';
  if (s.updated_by === null) return `Last changed from the command line, ${stamp(s.updated_at)}.`;
  const who = s.updated_by.display_name || s.updated_by.handle;
  return `Last changed by ${who} (@${s.updated_by.handle}), ${stamp(s.updated_at)}.`;
}

function SettingField({ setting }: { setting: SiteSetting }) {
  const [s, setS] = useState(setting);
  const [draft, setDraft] = useState(draftOf(setting.value));
  // current is the field's text as the latest render has it, which an
  // answer compares with what it sent: the save closure holds the render
  // that sent, and cannot see an edit made while the PUT was in flight.
  const current = useRef(draft);
  current.current = draft;
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // An edit clears both answers: neither says anything about the new text.
  const edit = (v: string) => {
    setDraft(v);
    setSaved(false);
    setFailed(null);
  };

  const save = async (sent: string) => {
    const value = valueOf(s, sent);
    if (value === undefined) {
      setSaved(false);
      setFailed(`Enter a whole number. It takes ${s.allowed}.`);
      return;
    }
    setBusy(true);
    setSaved(false);
    try {
      const res = await send<SiteSetting>('PUT', `${settingsUrl}/${encodeURIComponent(s.key)}`, { value });
      if (!res.ok) {
        // A refusal is of what was sent; the field may hold something else
        // by now, which it would wrongly mark (Terva review 1595).
        if (current.current === sent) {
          setFailed(isProblem(res.error, 'validation-failed') ? `That was not accepted. It takes ${s.allowed}.` : res.message);
        }
        return;
      }
      setFailed(null);
      setS(res.value);
      setSaved(current.current === sent);
    } finally {
      setBusy(false);
    }
  };

  const reset = () => {
    const d = draftOf(s.default);
    setDraft(d);
    current.current = d;
    void save(d);
  };

  const hint = `${s.doc} It takes ${s.allowed}. Default: ${shown(s.default)}.`;
  const changed = lastChanged(s);
  return (
    <form
      class="admin-setting"
      data-setting={s.key}
      onSubmit={(e) => {
        e.preventDefault();
        void save(draft);
      }}
    >
      <Field label={s.key} hint={hint} error={failed ?? undefined}>
        {(c) =>
          s.kind === 'boolean' ? (
            <input
              {...c}
              type="checkbox"
              checked={draft === 'true'}
              onChange={(e) => edit(String((e.target as HTMLInputElement).checked))}
            />
          ) : s.choices.length > 0 ? (
            <select {...c} value={draft} onChange={(e) => edit((e.target as HTMLSelectElement).value)}>
              {s.choices.map((v) => (
                <option key={v} value={v}>
                  {v}
                </option>
              ))}
            </select>
          ) : (
            <input
              {...c}
              type="text"
              inputMode={s.kind === 'integer' ? 'numeric' : undefined}
              autocomplete="off"
              value={draft}
              onInput={(e) => edit((e.target as HTMLInputElement).value)}
            />
          )
        }
      </Field>
      <div class="admin-inline">
        <button type="submit" class="admin-button" disabled={busy} data-control="save">
          Save
        </button>
        {!s.is_default && (
          <button type="button" class="admin-link" disabled={busy} onClick={reset} data-control="reset">
            Reset to default
          </button>
        )}
        {saved && (
          <span class="admin-muted" role="status" data-saved>
            Saved.
          </span>
        )}
      </div>
      {changed && (
        <p class="admin-muted" data-changed>
          {changed}
        </p>
      )}
    </form>
  );
}

// ConfigSections names each part of config.toml, why it is set there, and
// the keys under it. It shows no value: `ketju config check` shows those to
// an operator, with secrets redacted.
function ConfigSections({ sections }: { sections: ConfigSection[] }) {
  return (
    <section class="admin-settings-config" aria-labelledby="admin-config-heading" data-config>
      <h2 id="admin-config-heading">Set in config.toml</h2>
      <p>
        These are read before the database opens, or hold secrets, so they are not changed here. {configLine}
      </p>
      <dl>
        {sections.map((c) => (
          <div key={c.name} data-section={c.name}>
            <dt>
              <code>{c.name}</code>
            </dt>
            <dd>
              {c.reason}
              <br />
              <span class="admin-muted">
                {c.keys.map((k, i) => (
                  <span key={k}>
                    {i > 0 && ', '}
                    <code>{k}</code>
                  </span>
                ))}
              </span>
            </dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
