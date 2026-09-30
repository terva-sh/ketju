import { describe, expect, it } from 'vitest';
import { delayUntil, maxTimeout } from './deadline';

describe('delayUntil', () => {
  const now = 1_757_500_000_000;
  const day = 86_400_000;

  it('is the time left below the bound', () => {
    expect(delayUntil(now + 60_000, now)).toBe(60_000);
    expect(delayUntil(now + maxTimeout, now)).toBe(maxTimeout);
  });

  it('is the bound for a deadline further away', () => {
    expect(maxTimeout).toBe(2 ** 31 - 1);
    expect(delayUntil(now + 30 * day, now)).toBe(maxTimeout);
    // The longest window the server serves, about 292 years.
    expect(delayUntil(now + 9_223_372_036_854, now)).toBe(maxTimeout);
  });

  it('is 0 once the deadline has passed', () => {
    expect(delayUntil(now, now)).toBe(0);
    expect(delayUntil(now - 1, now)).toBe(0);
  });
});
