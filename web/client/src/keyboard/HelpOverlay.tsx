import { Fragment } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { isControl, trapTab } from '../components/focus';
import { helpOpen, shortcuts } from './shortcuts';

// HelpOverlay lists the keyboard shortcuts, each with the control that does
// the same. ? or the "Keyboard shortcuts" button below the posts opens it.
// It is a modal dialog: focus starts on Close, Tab stays inside, and Escape,
// Close or a click outside ends it, handing focus back to what opened it.
export function HelpOverlay() {
  const root = useRef<HTMLDivElement>(null);
  const close = () => (helpOpen.value = false);
  useEffect(() => {
    const opener = document.activeElement;
    root.current?.querySelector<HTMLElement>('button')?.focus();
    return () => {
      if (isControl(opener)) opener.focus({ preventScroll: true });
      else if (opener instanceof HTMLElement && opener.isConnected && opener.matches('[tabindex="-1"]')) opener.focus({ preventScroll: true });
    };
  }, []);
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && !e.defaultPrevented) {
      e.preventDefault();
      close();
    } else if (e.key === 'Tab') trapTab(e, root.current);
  };
  return (
    <div class="help-backdrop" onClick={(e) => e.target === e.currentTarget && close()}>
      <div ref={root} class="help-overlay" role="dialog" aria-modal="true" aria-labelledby="help-title" onKeyDown={onKeyDown}>
        <div class="help-head">
          <h2 id="help-title">Keyboard shortcuts</h2>
          <button type="button" class="help-close" onClick={close}>
            Close
          </button>
        </div>
        <table class="help-list">
          <thead>
            <tr>
              <th scope="col">Keys</th>
              <th scope="col">Does</th>
              <th scope="col">Or use</th>
            </tr>
          </thead>
          <tbody>
            {shortcuts.map((s) => (
              <tr key={s.keys.join(' ')} data-shortcut={s.keys[0]}>
                <td>
                  {s.keys.map((k, i) => (
                    <Fragment key={i}>
                      {i > 0 && ' then '}
                      <kbd>{k}</kbd>
                    </Fragment>
                  ))}
                </td>
                <td>{s.does}</td>
                <td>{s.control}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p class="help-note">Shortcuts do nothing while you type in the composer.</p>
      </div>
    </div>
  );
}
