// maxTimeout is the longest delay setTimeout keeps, 2^31 - 1 ms (about 24.8
// days). A browser fires a longer one at once.
export const maxTimeout = 2_147_483_647;

// delayUntil is the setTimeout delay for a deadline at, from now: the time
// left, at most maxTimeout, and 0 once it has passed. A timer set for a
// deadline further away than maxTimeout fires early, and whoever set it sets
// it again from the new now. A server window setting has no upper bound, so
// its deadline may be years ahead (TKT-01M3QXV399).
export function delayUntil(at: number, now: number): number {
  return Math.min(Math.max(at - now, 0), maxTimeout);
}
