import { describe, expect, it } from 'vitest';
import contractSource from './dto-contract.ts?raw';

// dto-contract.ts is checked by tsc, which only checks what it imports. A
// golden it does not import is a response shape nothing holds the types to,
// so every file in the directory must be named there.
describe('the DTO contract', () => {
  const goldens = Object.keys(import.meta.glob('../../../../internal/dto/testdata/golden/*.json')).map((p) =>
    p.slice(p.lastIndexOf('/') + 1),
  );

  it('finds the goldens', () => {
    expect(goldens.length).toBeGreaterThan(10);
  });

  it.each(goldens)('checks %s', (name) => {
    const imported = new RegExp(`from '[./]+/internal/dto/testdata/golden/${name.replace('.', '\\.')}'`);
    expect(contractSource).toMatch(imported);
  });

  it('checks every golden it imports', () => {
    const names = [...contractSource.matchAll(/^import (\w+) from '[^']+\.json';$/gm)].map((m) => m[1]);
    for (const n of names) expect(contractSource).toMatch(new RegExp(`matches<\\w+>\\(\\)\\(${n}\\)`));
  });
});
