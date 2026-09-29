import { viewer } from '../../state/viewer';

// when is a moment as the application screens say it: the date and time in
// the viewer's timezone and locale, which /me carries for an applicant too.
export function when(at: number): string {
  const { timezone, locale } = viewer.value.settings;
  return new Date(at).toLocaleString(locale || undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: timezone || undefined,
  });
}

// again says when a refused request may be tried again, from a Retry-After
// in seconds: a count for a short wait, the moment for a long one, such as
// the day between two applications (docs/05 step 4).
export function again(seconds: number | null, now = Date.now()): string {
  if (seconds === null) return 'Try again later.';
  if (seconds < 90) return `Try again in ${seconds} second${seconds === 1 ? '' : 's'}.`;
  return `You can try again after ${when(now + seconds * 1000)}.`;
}
