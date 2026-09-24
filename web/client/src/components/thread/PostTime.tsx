import { viewer } from '../../state/viewer';

// PostTime is when a post was written, short, in the viewer's timezone and
// locale: the time for today, else the weekday and time within a week, else
// the date. The full moment is in the title and the datetime attribute.
export function PostTime({ at, now = Date.now() }: { at: number; now?: number }) {
  const { timezone, locale } = viewer.value.settings;
  const opts = { timeZone: timezone || undefined };
  const lang = locale || undefined;
  return (
    <time dateTime={new Date(at).toISOString()} title={new Date(at).toLocaleString(lang, opts)}>
      {shortTime(at, now, lang, opts.timeZone)}
    </time>
  );
}

export function shortTime(at: number, now: number, locale?: string, timeZone?: string): string {
  const day = (ms: number) => new Date(ms).toLocaleDateString('en-CA', { timeZone });
  const hm: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', timeZone };
  if (day(at) === day(now)) return new Date(at).toLocaleTimeString(locale, hm);
  if (now - at < 6 * 24 * 3600 * 1000 && at <= now) {
    return new Date(at).toLocaleString(locale, { weekday: 'short', ...hm });
  }
  return new Date(at).toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric', timeZone });
}
