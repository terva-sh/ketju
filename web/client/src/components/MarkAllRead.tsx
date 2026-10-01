import { useState } from 'preact/hooks';
import { apiSend } from '../api/client';
import type { MarkedRead } from '../api/dto';
import { problemMessage } from '../api/problem';
import { forgetReadState } from '../state/lists';
import { refreshViewer } from '../state/viewer';
import './markread.css';

// MarkAllRead is the "Mark all read" button (docs/06 "Read tracking",
// TKT-01M3V6Y4JP): url is PUT /api/v1/me/read for the whole site, or PUT
// /api/v1/forums/{slug}/read for one forum. Once the server has answered,
// everything held that reports the viewer's place is dropped (the lists and
// the thread pages), onMarked asks the screen's own list again, and /me is
// asked again for the badges, so the change shows without a reload. /me is
// not waited for, as after a mark-unread (TKT-01M3KFFFJR): the badges are a
// signal and follow it whenever it answers.
//
// The screen offers it only to a member; the server refuses anyone else.
export function MarkAllRead({ url, onMarked }: { url: string; onMarked: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  const mark = async () => {
    setBusy(true);
    setError(null);
    try {
      await apiSend<MarkedRead>('PUT', url);
      forgetReadState();
      onMarked();
      void refreshViewer().catch(() => undefined);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <button type="button" class="mark-read" onClick={mark} disabled={busy} data-mark-read>
        Mark all read
      </button>
      {error !== null && (
        <p class="mark-read-error" role="alert">
          Could not mark all read. {problemMessage(error).text}
        </p>
      )}
    </>
  );
}
