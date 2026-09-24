import type { ShellAction } from '../state/shell';

export interface ThreadActionTarget {
  id: string;
  slug: string;
}

// threadActions is every action on a thread, the one list the desktop
// toolbar and the phone's more menu both render (docs/06 "Mobile"), so an
// action added here is on both. Watch (TKT-01M29CA9CE) and the moderator
// actions add themselves here with their tickets; /me carries no permission
// to gate the moderator ones on yet.
export function threadActions(t: ThreadActionTarget): ShellAction[] {
  return [
    {
      id: 'copy-link',
      label: 'Copy link',
      run: () => {
        const url = new URL(`/t/${encodeURIComponent(t.id)}/${encodeURIComponent(t.slug)}`, location.href).href;
        void navigator.clipboard?.writeText(url).catch(() => {});
      },
    },
  ];
}
