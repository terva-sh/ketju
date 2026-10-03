import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend, apiUpload } from '../../api/client';
import type { Author, ProfileSettings } from '../../api/dto';
import { asProblem, problemMessage, ProblemError } from '../../api/problem';
import { useResource } from '../../api/resource';
import { ErrorPanel } from '../../components/ErrorPanel';
import { Avatar } from '../../components/thread/Avatar';
import { profileHref } from '../../shell/nav';
import { budgetWords } from '../../state/files';
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
    // The fields stay editable while the save is on its way, so each edit
    // is dropped for the stored value only if it is still the one sent: a
    // change typed meanwhile is kept, unsaved (Terva review 2062).
    const sentName = name;
    const sentBio = bio;
    try {
      const r = await apiSend<ProfileSettings>('PATCH', profileUrl, patch, { status: 200 });
      // The avatar card works alongside, so a save takes only the fields it
      // sets and keeps the avatar as last stored, whichever answer arrives
      // last (Terva review 2063).
      setStored((now) => ({ ...(now ?? r), display_name: r.display_name, bio: r.bio }));
      setName((now) => (now === sentName ? null : now));
      setBio((now) => (now === sentBio ? null : now));
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
      {base && me && (
        <AvatarCard
          me={me}
          avatar={base.avatar}
          onChange={(r) => setStored((now) => ({ ...(now ?? r), avatar: r.avatar }))}
        />
      )}
      {base && (
        <form class="account-card settings-form" onSubmit={save} data-profile-form>
          <div class="settings-field">
            <label for="profile-display_name">Display name</label>
            <input
              id="profile-display_name"
              ref={nameField}
              type="text"
              value={shownName}
              autocomplete="name"
              aria-describedby="profile-display_name-hint profile-display_name-error"
              aria-invalid={!!errors.display_name}
              onInput={(e) => setName(e.currentTarget.value)}
            />
            <p id="profile-display_name-hint" class="settings-hint">
              Shown on your posts in place of your handle, at most {nameMax} characters. Leave it empty to show your
              handle.
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

export const avatarUrlApi = '/api/v1/settings/avatar';
// The avatar's cap (docs/05 "Display name and bio"), checked here before a
// file is sent, and by the server, which is the rule.
export const avatarMax = 1 << 20;

// avatarWords says why an avatar was refused, by the server's code (docs/07
// PUT /settings/avatar).
function avatarWords(err: unknown): string {
  const p = asProblem(err);
  switch (p.fieldErrors.file ?? p.type) {
    case 'type_not_allowed':
      return 'An avatar is a PNG or JPEG image.';
    case 'image_too_large':
      return 'This image is too large to use. Try a smaller one.';
    case 'payload-too-large':
      return 'An avatar is at most 1 MB.';
    case 'blocked':
      return "This image can't be used.";
    case 'upload_budget':
      return budgetWords;
  }
  return problemMessage(p).text;
}

// AvatarCard is the profile's avatar (TKT-01M3YSQ4CR): the one the member
// has, a chooser that sends a PNG or JPEG of at most 1 MB as soon as it is
// picked, and Remove when one is set. The server crops the picture to its
// centred square and re-encodes it. Each change asks /me again, so the top
// bar and author blocks follow.
function AvatarCard({ me, avatar, onChange }: { me: Author; avatar: string; onChange: (r: ProfileSettings) => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState('');
  const input = useRef<HTMLInputElement>(null);

  const clearChooser = () => {
    if (input.current) input.current.value = '';
  };
  const send = async (run: () => Promise<ProfileSettings>, said: string) => {
    setBusy(true);
    setError('');
    setDone('');
    try {
      onChange(await run());
      setDone(said);
      refreshViewer().catch(() => {});
    } catch (err) {
      setError(avatarWords(err));
    } finally {
      setBusy(false);
      clearChooser();
    }
  };
  const chosen = (e: Event) => {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!file || busy) return;
    if (file.size > avatarMax) {
      setDone('');
      setError(avatarWords(new ProblemError('payload-too-large', 413)));
      clearChooser();
      return;
    }
    send(() => apiUpload<ProfileSettings>(avatarUrlApi, file, 'PUT'), 'Avatar saved.');
  };
  const remove = () =>
    send(() => apiSend<ProfileSettings>('DELETE', avatarUrlApi, undefined, { status: 200 }), 'Avatar removed.');

  return (
    <section class="account-card settings-form" aria-labelledby="avatar-heading" data-avatar-card>
      <h2 id="avatar-heading" class="settings-title">
        Avatar
      </h2>
      <div class="settings-avatar">
        <Avatar author={{ ...me, avatar }} />
        <div class="settings-field">
          <label for="profile-avatar">{avatar ? 'Choose a new image' : 'Choose an image'}</label>
          <input
            id="profile-avatar"
            ref={input}
            type="file"
            accept="image/png,image/jpeg"
            disabled={busy}
            aria-describedby="profile-avatar-hint profile-avatar-error"
            aria-invalid={!!error}
            onChange={chosen}
          />
        </div>
      </div>
      <p id="profile-avatar-hint" class="settings-hint">
        A PNG or JPEG of at most 1 MB, cropped to a square from its middle. Signed-in members see it; everyone else sees
        your initial.
      </p>
      <p id="profile-avatar-error" class="settings-error" data-field-error="avatar" role="alert">
        {error}
      </p>
      {avatar && (
        <button type="button" class="sign-out settings-all" disabled={busy} data-remove-avatar onClick={remove}>
          Remove avatar
        </button>
      )}
      <p class="settings-saved" role="status">
        {done}
      </p>
    </section>
  );
}
