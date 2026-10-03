import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Post as PostDTO, ThreadPage } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { MoveNotice, movedHref, removedMark, removedText, TombstoneText } from './removal';
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

// TKT-01M29CYSAM: a moved post's tombstone, and its move's notice. The link
// is the copy's permalink, with the target's slug and the copy's ordinal,
// never the source's; while the move is unfinished it is the thread alone.
describe('a moved post', () => {
  const split = { id: '3yymw47g00099', slug: 'split-out', title: 'Split <out>' };
  let root: HTMLElement;
  beforeEach(() => {
    root = document.createElement('div');
    document.body.replaceChildren(root);
  });
  afterEach(async () => {
    await act(async () => render(null, root));
  });
  const draw = async (node: preact.ComponentChild) => act(async () => render(node, root));
  const moved = { removal: null, redacted: false, moved: true };

  it('names the thread it went to, linked at the copy', async () => {
    await draw(<TombstoneText post={{ ...moved, moved_to: { thread: split, ordinal: 2 } }} />);
    expect(root.textContent).toBe('Post moved to Split <out>');
    expect(root.querySelector('a')!.getAttribute('href')).toBe('/t/3yymw47g00099/split-out/2');
  });

  it('says it is still moving while the copy has no ordinal', async () => {
    await draw(<TombstoneText post={{ ...moved, moved_to: { thread: split, ordinal: null } }} />);
    expect(root.textContent).toBe('Post moved to Split <out> (still moving)');
    expect(root.querySelector('a')!.getAttribute('href')).toBe('/t/3yymw47g00099/split-out');
  });

  it('names nothing to a viewer the server does not tell', async () => {
    await draw(<TombstoneText post={{ ...moved, moved_to: null }} />);
    expect(root.textContent).toBe('Post moved to another thread');
    expect(root.querySelector('a')).toBeNull();
  });

  it('is not a removal', async () => {
    await draw(<TombstoneText post={{ removal: null, redacted: false, moved: false, moved_to: null }} />);
    expect(root.textContent).toBe('Post removed');
  });

  it.each([
    [{ count: 1, thread: split, ordinal: 4 }, '1 post was moved to Split <out>.', '/t/3yymw47g00099/split-out/4'],
    [{ count: 5, thread: split, ordinal: null }, '5 posts were moved to Split <out>.', '/t/3yymw47g00099/split-out'],
    [{ count: 5, thread: null, ordinal: null }, '5 posts were moved to another thread.', undefined],
  ])('the notice for %j', async (notice, text, href) => {
    await draw(<MoveNotice notice={notice} />);
    expect(root.textContent).toBe(text);
    expect(root.querySelector('a')?.getAttribute('href')).toBe(href);
    expect(movedHref(split, 3)).toBe('/t/3yymw47g00099/split-out/3');
  });
});
