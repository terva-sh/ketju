import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Post as PostDTO, ThreadPage } from '../../api/dto';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import { isPhone } from '../../state/layout';
import { viewer } from '../../state/viewer';
import { Post, type FlagHandler } from './Post';

// Where flag is offered in a post's footer, and the phone's "more" control
// (TKT-01M3QQWA5Z; docs/06 "Post anatomy" and "Mobile"). The viewer is the
// member golden's, alice.

const base = golden as ThreadPage;
const thread = { id: '3yymw47g00001', slug: 's' };

function post(over: Partial<PostDTO> = {}): PostDTO {
  return {
    ...base.posts[0],
    ordinal: 7,
    author: { ...base.posts[0].author, handle: 'carol', display_name: 'Carol' },
    deleted: false,
    removal: null,
    moved_to: null,
    html: '<p>body</p>',
    ...over,
  };
}

let root: HTMLElement;
const saved = viewer.value;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
  root = document.createElement('div');
  document.body.replaceChildren(root);
});
afterEach(async () => {
  await act(async () => render(null, root));
  viewer.value = saved;
  isPhone.value = false;
});

async function draw(p: PostDTO, onFlag?: FlagHandler) {
  await act(async () => render(<Post post={p} thread={thread} unread={false} onFlag={onFlag} />, root));
}

const flagIn = (el: ParentNode) => el.querySelector<HTMLButtonElement>('[data-post-action=flag]');

describe('flag in the post footer', () => {
  it('a member sees flag on a shown post', async () => {
    const onFlag = vi.fn<FlagHandler>();
    await draw(post(), onFlag);
    const flag = flagIn(root.querySelector('.post-footer')!)!;
    expect(flag).not.toBeNull();
    // Between link and history, as docs/06 draws the footer.
    expect([...root.querySelectorAll('.post-footer > *')].map((e) => e.textContent)).toEqual(['link', 'flag', 'history']);
    await act(async () => flag.click());
    expect(onFlag).toHaveBeenCalledWith(7, flag);
  });

  it('no flag on a removed or moved post', async () => {
    await draw(post({ deleted: true, removal: 'moderator' }), vi.fn());
    expect(flagIn(root)).toBeNull();
    await draw(post({ deleted: true, removal: 'author' }), vi.fn());
    expect(flagIn(root)).toBeNull();
    await draw(post({ moved_to: '3yymw47g00099' }), vi.fn());
    expect(flagIn(root)).toBeNull();
  });

  it('no flag without onFlag', async () => {
    await draw(post());
    expect(flagIn(root)).toBeNull();
  });

  it("no flag on the viewer's own post", async () => {
    for (const handle of ['alice', 'Alice', 'ALICE']) {
      await draw(post({ author: { ...post().author, handle } }), vi.fn());
      expect(flagIn(root), handle).toBeNull();
    }
  });

  it('no flag for a signed-out reader, even if a handler is passed', async () => {
    viewer.value = { ...structuredClone(member), user: null } as Me;
    await draw(post(), vi.fn());
    expect(flagIn(root)).toBeNull();
  });
});

describe("the phone's more control", () => {
  beforeEach(() => {
    isPhone.value = true;
  });

  it('the phone footer collapses', async () => {
    const onFlag = vi.fn<FlagHandler>();
    await draw(post(), onFlag);
    const footer = root.querySelector('.post-footer')!;
    const more = footer.querySelector<HTMLButtonElement>('[data-post-more]')!;
    const list = root.querySelector<HTMLElement>(`#${more.getAttribute('aria-controls')}`)!;
    // Link, flag and history are in the list, not in the footer's row.
    expect([...footer.children].filter((e) => e.matches('a, button')).length).toBe(0);
    expect([...list.querySelectorAll('a, button')].map((e) => e.textContent)).toEqual(['link', 'flag', 'history']);
    expect(list.hidden).toBe(true);
    expect(more.getAttribute('aria-label')).toBe('More actions for post #7');

    await act(async () => more.click());
    expect(more.getAttribute('aria-expanded')).toBe('true');
    expect(list.hidden).toBe(false);
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })));
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(list.hidden).toBe(true);
    expect(document.activeElement).toBe(more);

    await act(async () => more.click());
    await act(async () => flagIn(list)!.click());
    expect(more.getAttribute('aria-expanded')).toBe('false');
    expect(onFlag).toHaveBeenCalledWith(7, more);
  });

  it('a press elsewhere closes it', async () => {
    await draw(post(), vi.fn());
    const more = root.querySelector<HTMLButtonElement>('[data-post-more]')!;
    await act(async () => more.click());
    await act(async () => void document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })));
    expect(more.getAttribute('aria-expanded')).toBe('false');
  });

  it("holds no flag where the desktop footer would hold none", async () => {
    await draw(post({ author: { ...post().author, handle: 'alice' } }), vi.fn());
    const list = root.querySelector('.post-more-list')!;
    expect([...list.querySelectorAll('a, button')].map((e) => e.textContent)).toEqual(['link', 'history']);
  });
});
