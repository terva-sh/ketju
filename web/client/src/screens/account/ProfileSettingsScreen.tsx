import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../../api/client';
import type { ProfileSettings } from '../../api/dto';
import { asProblem, problemMessage } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { profileHref } from '../../shell/nav';
import { refreshViewer, viewer } from '../../state/viewer';
import { SettingsHead } from './settings';

export const profileUrl = '/api/v1/settings/profile';

// The limits docs/05 "Display name and bio" sets, in characters: code
// points, as the server counts them, not UTF-16 units.
export const nameMax = 100;
export const bioMax = 2000;
// The count is spoken once this few characters are left.
const nearLimit = 200;

type Key = 'display_name' | 'bio';

const chars = (s: string) => [...s].length;

// What a refused field says. The server's codes (docs/07 PATCH
// /settings/profile) each have one meaning per field.
const words: Record<Key, Record<string, string>> = {
  display_name: {
    too_long: `A display name is at most ${nameMax} characters.`,
    invalid_text: 'A display name is one line of text, with no control characters.',
  },
  bio: {
    too_long: `A bio is at most ${bioMax} characters.`,
    invalid_text: 'A bio cannot hold control characters.',
  },
};
const fieldWords = (key: Key, code: string) => words[key][code] ?? 'That was not accepted. Check it and try again.';

// ProfileSettingsScreen is /settings/profile (TKT-01M3V6XVVG; docs/05
// "Display name and bio", decided by the run 2026-10-02): the member's
// display name and bio, as GET /settings/profile answers. Save sends PATCH
// /settings/profile with only the keys that changed, takes the answer as
// stored, and asks /me again, so the top bar and author blocks follow. A
// refused field shows its words beside it and takes focus.
export function ProfileSettingsScreen() {
  const res = useResource(profileUrl, (signal) => apiGet<ProfileSettings>(profileUrl, { signal }));
  // stored is the profile as the server last answered: the GET, then each
  // save's answer.
  const [stored, setStored] = useState<ProfileSettings | null>(null);
  const [name, setName] = useState<string | null>(null);
  const [bio, setBio] = useState<string | null>(null);
  const [errors, setErrors] = useState<Partial<Record<Key, string>>>({});
  const [refused, setRefused] = useState('');
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);
  const [focusTo, setFocusTo] = useState<{ key: Key } | null>(null);
  const nameField = useRef<HTMLInputElement>(null);
  const bioField = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (res.data && !stored) setStored(res.data);
  }, [res.data]);
  useLayoutEffect(() => {
    if (focusTo) (focusTo.key === 'display_name' ? nameField : bioField).current?.focus();
  }, [focusTo]);

  const base = stored ?? res.data;
  const shownName = name ?? base?.display_name ?? '';
  const shownBio = bio ?? base?.bio ?? '';
  const left = bioMax - chars(shownBio);

  const refuse = (found: Partial<Record<Key, string>>) => {
    setErrors(found);
    const first = (['display_name', 'bio'] as const).find((k) => found[k]);
    if (first) setFocusTo({ key: first });
  };

  const save = async (e: Event) => {
    e.preventDefault();
    if (!base || busy) return;
    setSaved(false);
    setRefused('');
    const patch: Partial<ProfileSettings> = {};
    if (shownName.trim() !== base.display_name) patch.display_name = shownName;
    if (shownBio !== base.bio) patch.bio = shownBio;
    const here: Partial<Record<Key, string>> = {};
    if (chars(shownName.trim()) > nameMax) here.display_name = fieldWords('display_name', 'too_long');
    if (left < 0) here.bio = fieldWords('bio', 'too_long');
    if (here.display_name || here.bio) return refuse(here);
    setErrors({});
    if (patch.display_name === undefined && patch.bio === undefined) {
      setName(null);
      setSaved(true);
      return;
    }
    setBusy(true);
    try {
      const r = await apiSend<ProfileSettings>('PATCH', profileUrl, patch, { status: 200 });
      setStored(r);
      setName(null);
      setBio(null);
      setSaved(true);
      if (patch.display_name !== undefined) refreshViewer().catch(() => {});
    } catch (err) {
      const p = asProblem(err);
      const found: Partial<Record<Key, string>> = {};
      for (const k of ['display_name', 'bio'] as const) if (p.fieldErrors[k]) found[k] = fieldWords(k, p.fieldErrors[k]);
      if (found.display_name || found.bio) refuse(found);
      else setRefused(problemMessage(p).text);
    } finally {
      setBusy(false);
    }
  };

  const me = viewer.value.user;
  return (
    <main class="account" data-screen="settings-profile">
      <SettingsHead title="Profile" />
      {res.error && !base && <ErrorPanel error={res.error} what="Your profile" onRetry={res.retry} />}
      {base && (
        <form class="account-card settings-form" onSubmit={save} data-profile-form>
          <div class="settings-field">
            <label for="profile-display_name">Display name</label>
            <input
              id="profile-display_name"
              ref={nameField}
              type="text"
              value={shownName}
              maxLength={nameMax}
              autocomplete="name"
              aria-describedby="profile-display_name-hint profile-display_name-error"
              aria-invalid={!!errors.display_name}
              onInput={(e) => setName(e.currentTarget.value)}
            />
            <p id="profile-display_name-hint" class="settings-hint">
              Shown on your posts in place of your handle. Leave it empty to show your handle.
            </p>
            <p id="profile-display_name-error" class="settings-error" data-field-error="display_name">
              {errors.display_name}
            </p>
          </div>
          <div class="settings-field">
            <label for="profile-bio">Bio</label>
            <textarea
              id="profile-bio"
              ref={bioField}
              class="settings-bio"
              rows={6}
              value={shownBio}
              aria-describedby="profile-bio-hint profile-bio-count profile-bio-error"
              aria-invalid={!!errors.bio}
              onInput={(e) => setBio(e.currentTarget.value)}
            />
            <p id="profile-bio-hint" class="settings-hint">
              Markdown, as in a post. At most {bioMax} characters.
            </p>
            <p id="profile-bio-count" class="settings-hint" aria-live="polite" data-bio-count>
              {left <= nearLimit ? (left >= 0 ? `${left} characters left.` : `${-left} characters over.`) : ''}
            </p>
            <p id="profile-bio-error" class="settings-error" data-field-error="bio">
              {errors.bio}
            </p>
          </div>
          <button type="submit" class="sign-out settings-all" disabled={busy}>
            Save
          </button>
          {refused && (
            <p class="settings-error" role="alert">
              {refused}
            </p>
          )}
          <p class="settings-saved" role="status">
            {saved && (
              <>
                Saved.{' '}
                {me && (
                  <a href={profileHref(me.handle)} data-view-profile>
                    View your profile
                  </a>
                )}
              </>
            )}
          </p>
        </form>
      )}
    </main>
  );
}
