import { describe, expect, it } from 'vitest';
import { foldTag, hasTagErrors, splitTags, tagMessages } from './tagErrors';

// TKT-01M3YSQPZX: the Tags field's text as sent, and the refusals named.
describe('splitTags', () => {
  it('splits on spaces and commas and drops empties', () => {
    expect(splitTags('Garden, long-read  x')).toEqual(['Garden', 'long-read', 'x']);
    expect(splitTags(' ,a,,b\tc\n')).toEqual(['a', 'b', 'c']);
    expect(splitTags('   ')).toEqual([]);
    expect(splitTags('')).toEqual([]);
  });
});

describe('foldTag', () => {
  it('folds ASCII A-Z only, as the server does', () => {
    expect(foldTag('Long-Read')).toBe('long-read');
    // The Kelvin sign lowercases to k under Unicode rules; the server
    // refuses it rather than folding it.
    expect(foldTag('\u212A')).toBe('\u212A');
    expect(foldTag('Ä')).toBe('Ä');
  });
});

describe('tagMessages', () => {
  const sent = ['agent', 'ok', 'a_b', 'abcdefghijklmnopqrstuvwxyz'];

  it('names each refused tag by its index, in the order sent, then the count', () => {
    expect(
      tagMessages(
        {
          'tags.3': 'too_long',
          'tags.0': 'reserved_tag',
          'tags.2': 'invalid_tag',
          tags: 'too_many',
          title: 'too_long',
        },
        sent,
      ),
    ).toEqual([
      '“agent” is reserved for moderators and agent accounts.',
      '“a_b” is not a tag: use letters, digits and single hyphens between them.',
      '“abcdefghijklmnopqrstuvwxyz” is longer than 24 characters.',
      'A thread can have at most 8 tags.',
    ]);
  });

  it('says an empty tag is one, and reads a code it does not know as a refusal', () => {
    expect(tagMessages({ 'tags.0': 'invalid_tag', 'tags.1': 'made_up' }, ['', 'x'])).toEqual([
      'A tag cannot be empty.',
      '“x” was refused.',
    ]);
    expect(tagMessages({ tags: 'wrong_type' }, [])).toEqual(['The tags could not be read.']);
  });

  it('says nothing when the tags were not named', () => {
    expect(tagMessages({ title: 'too_long' }, sent)).toEqual([]);
    expect(hasTagErrors({ title: 'too_long' })).toBe(false);
    expect(hasTagErrors({ 'tags.4': 'invalid_tag' })).toBe(true);
    expect(hasTagErrors({ tags: 'too_many' })).toBe(true);
    // A field that only starts with the word is not the tags.
    expect(hasTagErrors({ tagsx: 'required' })).toBe(false);
  });
});
