import { describe, expect, it } from 'vitest';

// AC3's other half: problems become words in one place. A screen or
// component that compared a problem's type or status itself would be a
// second table, and one that called fetch would not get problems at all.
// Only src/api talks to the network.
const sources = import.meta.glob(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}', '!../testing.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const inside = (dir: string) => Object.entries(sources).filter(([p]) => p.startsWith(`../${dir}/`));

describe('one path for errors', () => {
  it('reads the sources', () => {
    expect(inside('screens').length).toBeGreaterThan(0);
    expect(inside('components').length).toBeGreaterThan(0);
  });

  it.each([...inside('screens'), ...inside('components')].map(([p]) => p))(
    '%s does not classify a problem itself',
    (p) => {
      expect(sources[p]).not.toMatch(/\.(type|status)\s*[!=]==?/);
      expect(sources[p]).not.toMatch(/problem\+json/);
    },
  );

  it.each(Object.keys(sources).filter((p) => !p.startsWith('./')))('%s does not call fetch', (p) => {
    expect(sources[p]).not.toMatch(/\bfetch\s*\(/);
  });
});
