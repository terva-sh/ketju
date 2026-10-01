import { Fragment } from 'preact';
import { useRef } from 'preact/hooks';
import { isControl, useModal } from '../components/focus';
import { helpOpen, shortcuts } from './shortcuts';

// HelpOverlay lists the keyboard shortcuts, each with the control that does
// the same. ? or the "Keyboard shortcuts" button below the posts opens it.
// It is a modal dialog: focus starts on Close, Tab stays inside, and Escape,
// Close or a click outside ends it, handing focus back to what opened it.
//
// Its keys are taken on the document rather than on the dialog (useModal),
// so Escape closes it wherever focus is: on the post that opened it, or on
// the body after a click on the list's text. Focus moves and the listener
// goes on in the same task as the ? that drew the list. An Escape pressed
// before the next frame went to the post and was lost (TKT-01M3TMRCMJ).
export function HelpOverlay() {
  const root = useRef<HTMLDivElement>(null);
  const close = () => (helpOpen.value = false);
  useModal(root, close, (el) => {
    const opener = document.activeElement;
    el?.querySelector<HTMLElement>('button')?.focus();
    return () => {
      if (isControl(opener)) opener.focus({ preventScroll: true });
      else if (opener instanceof HTMLElement && opener.isConnected && opener.matches('[tabindex="-1"]')) opener.focus({ preventScroll: true });
    };
  });
  return (
    <div class="help-backdrop" onClick={(e) => e.target === e.currentTarget && close()}>
      <div ref={root} class="help-overlay" role="dialog" aria-modal="true" aria-labelledby="help-title">
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
