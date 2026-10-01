import type { ThreadHint } from '../api/dto';

// The live-update subscription for one thread (docs/06 "Live updates",
// docs/07 GET /events). The stream carries only {thread_id, log_length}
// hints; refresh asks for the page on screen again, which is where access is
// judged, so a hint shows nothing the reader could not fetch.
//
// Polling every 30 s takes over when the stream errors, or when its ready
// event is 10 s late, which is what a proxy that buffers the stream looks
// like. Every fourth poll tries the stream again, and a ready from it stops
// the polling. The stream is closed on an error rather than left to the
// browser's own reconnect, so a refused or ended stream is retried on this
// schedule and nothing else. A tab hidden for 60 s closes its stream and
// stops polling; shown again, it opens a new stream, whose snapshot of the
// thread's log length catches it up.
export const readyMs = 10_000;
export const pollMs = 30_000;
export const hiddenMs = 60_000;

// subscribe follows thread id until the function it answers is called.
// held is the log length of the page on screen: a hint no longer than it
// changes nothing. refresh asks for the page on screen again.
export function subscribe(id: string, held: () => number, refresh: () => void): () => void {
  let es: EventSource | undefined;
  let wait: ReturnType<typeof setTimeout> | undefined;
  let poll: ReturnType<typeof setTimeout> | undefined;
  let hide: ReturnType<typeof setTimeout> | undefined;
  let polls = 0;

  const drop = () => {
    clearTimeout(wait);
    es?.close();
    es = undefined;
  };
  const stop = () => {
    drop();
    clearTimeout(poll);
    poll = undefined;
  };
  const tick = () => {
    refresh();
    if (++polls % 4 === 0) open();
    poll = setTimeout(tick, pollMs);
  };
  const fail = () => {
    drop();
    poll ??= setTimeout(tick, pollMs);
  };
  const open = () => {
    drop();
    const ES = globalThis.EventSource;
    if (!ES) return fail();
    const s = (es = new ES('/events?threads=' + encodeURIComponent(id)));
    s.addEventListener('ready', () => {
      clearTimeout(wait);
      clearTimeout(poll);
      poll = undefined;
      polls = 0;
    });
    s.addEventListener('thread', (e) => {
      const h = JSON.parse((e as MessageEvent<string>).data) as ThreadHint;
      if (h.thread_id === id && h.log_length > held()) refresh();
    });
    s.onerror = () => es === s && fail();
    wait = setTimeout(fail, readyMs);
  };
  const shown = () => {
    clearTimeout(hide);
    if (document.visibilityState === 'hidden') hide = setTimeout(stop, hiddenMs);
    else if (!es && poll === undefined) open();
  };

  open();
  shown();
  document.addEventListener('visibilitychange', shown);
  return () => {
    stop();
    clearTimeout(hide);
    document.removeEventListener('visibilitychange', shown);
  };
}
