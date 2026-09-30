import { useEffect, useState } from 'preact/hooks';
import { delayUntil } from './deadline';

// useWindowClock renders its caller again when the soonest of closesAt that
// is still ahead passes, so an offer the rule withdraws at that moment goes
// with no other input. One timer serves the page, however many posts it
// holds; a per-post interval lost for that.
//
// The timer is set through delayUntil, never for longer than maxTimeout: a
// browser fires a longer one at once, and a 30-day window would render,
// set itself again and fire, without end. One that fires early renders
// nothing new and is set again from the new now (TKT-01M3PTT97X).
export function useWindowClock(closesAt: (number | null)[]) {
  const [tick, setTick] = useState(0);
  const now = Date.now();
  let soonest: number | null = null;
  for (const at of closesAt) if (at !== null && at > now && (soonest === null || at < soonest)) soonest = at;
  useEffect(() => {
    if (soonest === null) return;
    const t = setTimeout(() => setTick((n) => n + 1), delayUntil(soonest, Date.now()));
    return () => clearTimeout(t);
  }, [soonest, tick]);
}
