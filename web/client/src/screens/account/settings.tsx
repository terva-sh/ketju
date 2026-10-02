import type { ComponentChildren } from 'preact';
import { useRef, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { problemMessage } from '../../api/problem';
import { isControl, useModal } from '../../components/focus';
import { nav } from '../../leave';
import { profileHref } from '../../shell/nav';
import { useShellContext } from '../../state/shell';
import { viewer } from '../../state/viewer';

// What the settings screens share (TKT-01M29CXK4E): their heading and the
// links between them, the confirmation dialog, and the full load of / that
// follows signing this browser out. Account (TKT-01M3V6XTYG) is last.

const pages = [
  ['/settings', 'Preferences'],
  ['/settings/sessions', 'Sessions'],
  ['/settings/identities', 'Sign-in identities'],
  ['/settings/account', 'Account'],
] as const;

// SettingsHead is a settings screen's heading, and the links to all of them.
// Back, on a phone, and the desktop's link above the heading go to the
// member's profile, where the Account block led here from.
export function SettingsHead({ title }: { title: string }) {
  const { path } = useLocation();
  const me = viewer.value.user;
  const back = me ? { href: profileHref(me.handle), label: 'Profile' } : undefined;
  useShellContext({ title, back, actions: [] }, [title, back?.href]);
  return (
    <header class="account-head">
      {back && (
        <a class="account-up" href={back.href}>
          Profile
        </a>
      )}
      <h1>{title}</h1>
      <nav class="settings-nav" aria-label="Settings">
        {pages.map(([href, label]) => (
          <a key={href} href={href} aria-current={path === href ? 'page' : undefined}>
            {label}
          </a>
        ))}
      </nav>
    </header>
  );
}

// leaveHome loads / in full once this browser's session has ended, as the
// Sign out form's 303 does (shell/SignOut.tsx): the load drops every signal
// and held answer of the member who left, and other tabs reload through
// state/viewerGuard.ts.
export const leaveHome = () => nav.leave('/');

// Confirm is a modal dialog that asks before an action that cannot be taken
// back (useModal): focus starts on Cancel, Tab stays inside, and Escape,
// Cancel or a press on the backdrop close it, focus going back to the
// control that opened it. run is the action; what it throws is shown in the
// dialog, which stays open so the member can try again or cancel. explain
// gives a refusal the screen has its own words for, and answers undefined
// for the rest, which read as problemMessage says.
export function Confirm({
  title,
  action,
  run,
  onClose,
  explain,
  children,
}: {
  title: string;
  action: string;
  run: () => Promise<void>;
  onClose: () => void;
  explain?: (err: unknown) => string | undefined;
  children: ComponentChildren;
}) {
  const root = useRef<HTMLDivElement>(null);
  const [busy, setBusy] = useState(false);
  const [refused, setRefused] = useState<string | null>(null);
  useModal(root, onClose, (el) => {
    const from = document.activeElement;
    el?.querySelector<HTMLElement>('button')?.focus();
    return () => {
      if (isControl(from)) from.focus({ preventScroll: true });
    };
  });
  const go = async () => {
    if (busy) return;
    setBusy(true);
    setRefused(null);
    try {
      await run();
    } catch (err) {
      setRefused(explain?.(err) ?? problemMessage(err).text);
      setBusy(false);
    }
  };
  return (
    <div class="confirm-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={root}
        class="confirm"
        role="dialog"
        aria-modal="true"
        aria-labelledby="confirm-title"
        aria-describedby="confirm-body"
        data-confirm
      >
        <h2 id="confirm-title">{title}</h2>
        <p id="confirm-body">{children}</p>
        {refused && (
          <p class="settings-error" role="alert">
            {refused}
          </p>
        )}
        <div class="confirm-buttons">
          <button type="button" class="sign-out" onClick={onClose}>
            Cancel
          </button>
          <button type="button" class="sign-out confirm-go" disabled={busy} onClick={go} data-confirm-go>
            {action}
          </button>
        </div>
      </div>
    </div>
  );
}
