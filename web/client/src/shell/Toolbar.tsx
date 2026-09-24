import type { ShellAction } from '../state/shell';

// Toolbar is the desktop form of a screen's actions: the same list the
// phone's more menu shows, as buttons.
export function Toolbar({ actions, label }: { actions: ShellAction[]; label: string }) {
  if (actions.length === 0) return null;
  return (
    <div class="toolbar" role="toolbar" aria-label={label} data-shell="toolbar">
      {actions.map((a) => (
        <button key={a.id} type="button" data-action={a.id} onClick={a.run}>
          {a.label}
        </button>
      ))}
    </div>
  );
}
