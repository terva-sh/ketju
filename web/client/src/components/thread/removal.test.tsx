import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Post as PostDTO, ThreadPage } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { removedMark, removedText } from './removal';
import { Tombstone } from './Tombstone';

// How a removed post reads, and a hard-redacted one in particular
// (TKT-01M3R1WPNS; docs/03 "Hard redaction", docs/07 "Removed posts"). A
// redacted post is gone for good whoever removed it, so its words say that
// and nothing about who.

const removals = ['author', 'moderator', 'review', null, 'something newer'];

describe('removedText and removedMark', () => {
  it('say removed permanently for a redacted post, whatever its removal', () => {
    for (const removal of removals) {
      expect(removedText({ removal, redacted: true })).toBe('Post removed permanently');
      expect(removedMark({ removal, redacted: true })).toBe('removed permanently');
    }
  });

  it('say who removed a post that is not redacted, as before', () => {
    const text = removals.map((removal) => removedText({ removal, redacted: false }));
    const mark = removals.map((removal) => removedMark({ removal, redacted: false }));
    expect(text).toEqual([
      'Post removed by author',
      'Post removed by a moderator',
      'Post hidden pending review',
      'Post removed',
      'Post removed',
    ]);
    expect(mark).toEqual(['removed by author', 'removed by a moderator', 'hidden pending review', 'deleted', 'deleted']);
    for (const w of [...text, ...mark]) expect(w).not.toContain('permanently');
  });
});

describe('a redacted post on the thread page', () => {
  const base = golden as ThreadPage;
  let root: HTMLElement;
  beforeEach(() => {
    root = document.createElement('div');
    document.body.replaceChildren(root);
  });
  afterEach(async () => {
    await act(async () => render(null, root));
  });

  it('is a tombstone that says it was removed permanently', async () => {
    const p: PostDTO = {
      ...base.posts[0],
      ordinal: 65,
      deleted: true,
      removal: 'moderator',
      redacted: true,
      html: '',
      attachments: [],
      moved_to: null,
    };
    await act(async () => render(<Tombstone post={p} thread={{ id: '3yymw47g00001', slug: 's' }} unread={false} />, root));
    const t = root.querySelector('.tombstone')!;
    expect(t.getAttribute('data-ordinal')).toBe('65');
    expect(t.querySelector('.tombstone-text')!.textContent).toBe('Post removed permanently');
    expect(t.textContent).not.toContain('by a moderator');
  });
});
