import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { Settings } from '../../api/dto';
import { asProblem, problemMessage } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { syncThemeFromAccount, themePrefs } from '../../state/theme';
import { forgetAllThreadPages } from '../../state/threads';
import { viewer } from '../../state/viewer';
import { SettingsHead } from './settings';

export const settingsUrl = '/api/v1/settings';

// The server's timezone for an account that never chose one
// (internal/service/users me.go defaultTimezone).
const defaultZone = 'UTC';

const themeWords: Record<string, string> = { system: 'System', light: 'Light', dark: 'Dark' };

type Key = 'timezone' | 'page_size' | 'theme';

// adopt makes s the viewer's settings, so PostTime and the theme change at
// once, and takes the account's theme (state/theme.ts). Thread pages cut at
// another page size are dropped with the old size.
function adopt(s: Settings) {
  const was = viewer.value.settings;
  if (was.timezone === s.timezone && was.locale === s.locale && was.page_size === s.page_size && was.theme === s.theme) return;
  viewer.value = { ...viewer.value, settings: s };
  if (was.page_size !== s.page_size) forgetAllThreadPages();
  syncThemeFromAccount(viewer.value);
}

// saves is the queue every settings save goes through, one at a time, each
// sent once the one before it has been answered, so the server applies them
// in the order they were made and each answer is the account as of every
// save before it. Every answer is taken in turn, and the last one is the
// account. Sent at once, two saves to different fields could be applied in
// either order, and an answer taken by its send order could drop a field
// the account had saved (Terva review 1838). The queue is the module's, not
// the screen's, so a save made after leaving /settings and coming back
// still waits for one the earlier screen left in flight (Terva review 1875).
let saves: Promise<void> = Promise.resolve();
// made counts the saves ever queued, and settled those answered or failed.
let made = 0;
let settled = 0;

// timezoneOptions is what the timezone select offers: the browser's zones,
// with UTC and the zones given, the stored one and one being saved, added
// when the browser's list lacks them. The list omits valid IANA names the
// server accepts, UTC among them, and a select with no option for the
// stored zone would show another and could send it (Terva review 1691).
// null where the browser cannot list its zones, and the field is then text.
export function timezoneOptions(...zones: string[]): string[] | null {
  let list: string[];
  try {
    list = Intl.supportedValuesOf('timeZone');
  } catch {
    return null;
  }
  const all = new Set(list);
  for (const z of [defaultZone, ...zones]) if (z) all.add(z);
  return [...all].sort();
}

// fieldWords is what a refused change says beside its field. The server's
// codes for these fields (docs/07 PATCH /settings: wrong_type, not_an_option
// and out_of_range) each have one meaning per field, so the words are the
// field's. forms/messages.ts is not used: its words are for the application
// form, and importing it would pull the lazy application chunk into the
// reader's (measured: 1040 B more).
function fieldWords(key: Key, err: unknown): string {
  const p = asProblem(err);
  if (!p.fieldErrors[key]) return problemMessage(p).text;
  return fieldText[key];
}
const pageSizeWords = 'Choose a whole number from 10 to 50.';
const fieldText: Record<Key, string> = {
  timezone: 'This is not a timezone the server knows. Choose one such as Europe/Helsinki.',
  page_size: pageSizeWords,
  theme: 'Choose System, Light or Dark.',
};

// SettingsScreen is /settings (TKT-01M29CXK4E): the member's timezone,
// thread page size and theme, as GET /settings answers. Each field sends
// PATCH /settings with its own key when it changes, and nothing else does;
// the answer, the settings as stored, becomes the viewer's at once. The
// form shows from the viewer while GET /settings revalidates it, and a
// newer answer from another device is taken in the same way. A signed-out
// reader is told to sign in, by the server's unauthenticated.
//
// A field shows the value it is saving until the answer comes, and a value
// refused keeps showing beside the reason, as any form field does; a field
// drawn from the stored value alone would jump back while it saved.
export function SettingsScreen() {
  const res = useResource(
    settingsUrl,
    (signal) => apiGet<Settings>(settingsUrl, { signal }),
    () => (viewer.value.user ? viewer.value.settings : undefined),
  );
  const [errors, setErrors] = useState<Partial<Record<Key, string>>>({});
  // pending is what a field shows in place of the stored value: a page size
  // may be the text typed.
  const [pending, setPending] = useState<{ timezone?: string; page_size?: number | string; theme?: string }>({});
  const [saved, setSaved] = useState('');
  // Edits are numbered, and last is each field's newest, whether it was
  // sent or refused here: only the answer to that edit settles what the
  // field shows, so an earlier save's answer does not clear a value typed
  // since (Terva review 1875). Saves go through the module's queue (saves).
  const sent = useRef(0);
  const last = useRef<Partial<Record<Key, number>>>({});

  // The GET's answer is taken only if no save was out when the screen
  // asked and none has been made since: otherwise it may be from before a
  // save, and the save's own answer is the newer.
  const asked = useRef({ made, idle: made === settled });
  useEffect(() => {
    if (res.data && !res.loading && asked.current.idle && made === asked.current.made) adopt(res.data);
  }, [res.data, res.loading]);

  // edit shows value in key's field, with why beside it when it is refused
  // here, and answers the edit's number.
  const edit = (key: Key, value: string | number, why?: string) => {
    const n = ++sent.current;
    last.current[key] = n;
    setPending((p) => ({ ...p, [key]: value }));
    setErrors((e) => ({ ...e, [key]: why }));
    return n;
  };

  const save = (key: Key, value: string | number) => {
    const n = edit(key, value);
    setSaved('');
    made++;
    saves = saves.then(async () => {
      try {
        adopt(await apiSend<Settings>('PATCH', settingsUrl, { [key]: value }, { status: 200 }));
        setSaved('Saved.');
        if (last.current[key] === n) setPending(({ [key]: _, ...rest }) => rest);
      } catch (err) {
        if (last.current[key] === n) setErrors((e) => ({ ...e, [key]: fieldWords(key, err) }));
      } finally {
        settled++;
      }
    });
  };

  const stored = res.data && viewer.value.settings;
  const s = stored && { ...stored, ...pending };
  return (
    <main class="account" data-screen="settings">
      <SettingsHead title="Settings" />
      {res.error && !res.data && <ErrorPanel error={res.error} what="Your settings" onRetry={res.retry} />}
      {s && (
        <form class="account-card settings-form" onSubmit={(e) => e.preventDefault()}>
          <Timezone stored={stored.timezone} shown={s.timezone} error={errors.timezone} save={(z) => save('timezone', z)} />
          <Field id="page_size" label="Posts on a thread page" hint="From 10 to 50." error={errors.page_size}>
            <input
              id="settings-page_size"
              type="number"
              inputMode="numeric"
              min={10}
              max={50}
              step={1}
              value={s.page_size}
              aria-describedby="settings-page_size-hint settings-page_size-error"
              aria-invalid={!!errors.page_size}
              onChange={(e) => {
                const v = e.currentTarget.valueAsNumber;
                if (!Number.isInteger(v) || v < 10 || v > 50) edit('page_size', e.currentTarget.value, pageSizeWords);
                else if (v !== stored.page_size || pending.page_size !== undefined) save('page_size', v);
              }}
            />
          </Field>
          <fieldset class="settings-field" aria-describedby="settings-theme-error">
            <legend>Theme</legend>
            <div class="settings-choices">
              {themePrefs.map((t) => (
                <label key={t}>
                  <input
                    type="radio"
                    name="theme"
                    value={t}
                    checked={s.theme === t}
                    onChange={() => save('theme', t)}
                  />
                  {themeWords[t]}
                </label>
              ))}
            </div>
            <FieldError id="theme" error={errors.theme} />
          </fieldset>
          <p class="settings-saved" role="status">
            {saved}
          </p>
        </form>
      )}
    </main>
  );
}

// Timezone is the timezone field: a select of every zone the browser knows
// with the shown one chosen, or a text field where it knows none. It sends
// only on its own change. While the account has the server's default and
// this device is elsewhere, it offers this device's zone.
function Timezone({
  stored,
  shown,
  error,
  save,
}: {
  stored: string;
  shown: string;
  error?: string;
  save: (zone: string) => void;
}) {
  const zones = timezoneOptions(stored, shown);
  const here = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const offer = stored === defaultZone && here && here !== defaultZone;
  return (
    <Field id="timezone" label="Timezone" hint="Times on posts are shown in it." error={error}>
      {zones ? (
        <select
          id="settings-timezone"
          value={shown}
          aria-describedby="settings-timezone-hint settings-timezone-error"
          aria-invalid={!!error}
          onChange={(e) => save(e.currentTarget.value)}
        >
          {zones.map((z) => (
            <option key={z} value={z}>
              {z}
            </option>
          ))}
        </select>
      ) : (
        <input
          id="settings-timezone"
          type="text"
          value={shown}
          autocomplete="off"
          spellcheck={false}
          aria-describedby="settings-timezone-hint settings-timezone-error"
          aria-invalid={!!error}
          onChange={(e) => {
            const z = e.currentTarget.value.trim();
            if (z && z !== shown) save(z);
          }}
        />
      )}
      {offer && (
        <button type="button" class="sign-out settings-here" onClick={() => save(here)} data-device-zone>
          Use this device's timezone ({here})
        </button>
      )}
    </Field>
  );
}

function Field({
  id,
  label,
  hint,
  error,
  children,
}: {
  id: string;
  label: string;
  hint: string;
  error?: string;
  children: ComponentChildren;
}) {
  return (
    <div class="settings-field">
      <label for={`settings-${id}`}>{label}</label>
      {children}
      <p id={`settings-${id}-hint`} class="settings-hint">
        {hint}
      </p>
      <FieldError id={id} error={error} />
    </div>
  );
}

// FieldError is always in the page, so aria-describedby names something,
// and is empty until a change is refused.
function FieldError({ id, error }: { id: string; error?: string }) {
  return (
    <p id={`settings-${id}-error`} class="settings-error" data-field-error={id}>
      {error}
    </p>
  );
}
