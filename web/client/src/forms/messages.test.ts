import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import type { FormField } from '../api/dto';
import { problemMessage, ProblemError } from '../api/problem';
import { fieldMessage, knownCodes } from './messages';

// The field error codes are API surface (docs/07 "Field errors on
// validation-failed"). The table in the document, between its form-codes
// markers, is the list; every code in it has words here, and this table
// names no other.
const documented = (() => {
  const doc = readFileSync('../../docs/07-api-and-clients.md', 'utf8');
  const block = doc.split('<!-- form-codes:begin -->')[1].split('<!-- form-codes:end -->')[0];
  return [...block.matchAll(/^\| `([a-z_]+)` \|/gm)].map((m) => m[1]);
})();

describe('field messages', () => {
  it('reads the documented codes', () => {
    expect(documented.length).toBeGreaterThan(10);
  });

  it.each(documented)('has words for %s', (code) => {
    expect(knownCodes).toContain(code);
    expect(fieldMessage(code).trim()).not.toBe('');
  });

  it('names no code the document lacks', () => {
    expect(knownCodes.filter((c) => !documented.includes(c))).toEqual([]);
  });

  it('says the limit when the field has one', () => {
    expect(fieldMessage('too_long', { max: 1000 } as FormField)).toContain('1000');
  });

  it.each(['made_up', 'constructor', '__proto__', ''])('reads an unknown code %j as the refusal as a whole', (code) => {
    expect(fieldMessage(code)).toBe(problemMessage(new ProblemError('validation-failed', 422)).text);
  });
});
