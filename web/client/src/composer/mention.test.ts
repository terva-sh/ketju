import { describe, expect, it } from 'vitest';
import { insertMention, mentionAt } from './mention';

// mentionAt is read against internal/render/mention.go's rules: where the
// parser would see a mention, and nowhere else. | marks the caret.
const at = (s: string) => mentionAt(s.replace('|', ''), s.indexOf('|'));

describe('mentionAt', () => {
  it('finds the handle being typed', () => {
    expect(at('hi @al|')).toEqual({ start: 3, typed: 'al' });
    expect(at('@a|')).toEqual({ start: 0, typed: 'a' });
    expect(at('(@bo| said')).toEqual({ start: 1, typed: 'bo' });
    expect(at('line\n@x_y-z|')).toEqual({ start: 5, typed: 'x_y-z' });
  });

  it('is nothing before a character is typed', () => {
    expect(at('hi @|')).toBeNull();
  });

  it('is nothing after a handle byte, as an e-mail address is not a mention', () => {
    expect(at('mail me@ex|')).toBeNull();
    expect(at('a.@b|')).toBeNull();
    expect(at('_@b|')).toBeNull();
  });

  it('is nothing in the middle of a word', () => {
    expect(at('@al|ice')).toBeNull();
    expect(at('@al|.')).toBeNull();
    expect(at('@al| ')).toEqual({ start: 0, typed: 'al' });
  });

  it('is nothing once a dot is typed, which ends a mention', () => {
    expect(at('@alice.|')).toBeNull();
    expect(at('@a.b|')).toBeNull();
  });

  it('is nothing past the longest handle', () => {
    expect(at('@' + 'a'.repeat(30) + '|')).toEqual({ start: 0, typed: 'a'.repeat(30) });
    expect(at('@' + 'a'.repeat(31) + '|')).toBeNull();
  });

  it('is nothing without an @', () => {
    expect(at('alice|')).toBeNull();
    expect(at('|')).toBeNull();
  });
});

// AC4: the form the renderer expects, @ and the handle as the server spells
// it, with a space after.
describe('insertMention', () => {
  it('replaces what was typed with the handle and a space', () => {
    const text = 'thanks @AL';
    expect(insertMention(text, { start: 7, typed: 'AL' }, text.length, 'alice')).toEqual({
      text: 'thanks @alice ',
      caret: 14,
    });
  });

  it('keeps what follows the caret, and does not double a space', () => {
    expect(insertMention('@al and', { start: 0, typed: 'al' }, 3, 'alice')).toEqual({ text: '@alice and', caret: 7 });
    expect(insertMention('@al,', { start: 0, typed: 'al' }, 3, 'alice')).toEqual({ text: '@alice ,', caret: 7 });
  });
});
