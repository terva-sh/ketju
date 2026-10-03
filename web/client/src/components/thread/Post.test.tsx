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
    // Neither a reply nor replied to, so it offers no Conversation unless a
    // test gives it one.
    reply_to: null,
    reply_count: 0,
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

// Conversation, a post's reply chain lens (TKT-01M29CTY71, docs/06 "the
// lens bar"): on a reply and on a replied-to post, after link in the
// footer, and in the more list on a phone.
describe('conversation', () => {
  const href = `/t/${thread.id}/${thread.slug}?lens=chain:7`;
  const footer = () => [...root.querySelectorAll('.post-footer > *')].map((e) => e.textContent);

  it('is on a reply and on a post with replies, and nowhere else', async () => {
    await draw(post({ reply_to: 3 }), vi.fn());
    expect(footer()).toEqual(['link', 'conversation', 'flag', 'history']);
    expect(root.querySelector('[data-conversation]')?.getAttribute('href')).toBe(href);
    await draw(post({ reply_count: 2 }), vi.fn());
    expect(root.querySelector('[data-conversation]')?.getAttribute('href')).toBe(href);
    await draw(post(), vi.fn());
    expect(root.querySelector('[data-conversation]')).toBeNull();
  });

  it('is in the more list on a phone', async () => {
    isPhone.value = true;
    await draw(post({ reply_to: 3 }), vi.fn());
    const list = root.querySelector('.post-more-list')!;
    expect([...list.querySelectorAll('a, button')].map((e) => e.textContent)).toEqual(['link', 'conversation', 'flag', 'history']);
    expect(list.querySelector('[data-conversation]')?.getAttribute('href')).toBe(href);
  });
});

// The order of a post's footer and header (TKT-01M3S3Z9E1; docs/06 "Mobile"
// and "Post anatomy"): on a phone the footer reads the expander, reply,
// quote, then More; the desktop keeps quote before reply. The header's DOM
// order is the same at both widths, since the phone stacks it in CSS. On a
// phone the ordinal is text, as the phone mockup draws it, so the header's
// links are the author over the reply-to (TKT-01M3V3CFMA).
describe('the order of the footer and header', () => {
  const actions = { quote: vi.fn(), reply: vi.fn() };
  async function drawAll(p: PostDTO) {
    await act(async () =>
      render(<Post post={p} thread={thread} unread={false} actions={actions} onFlag={vi.fn()} expander />, root),
    );
  }
  // footer names the footer's controls in DOM order, which is focus order.
  const footer = () =>
    [...root.querySelectorAll('.post-footer > *')].map((e) =>
      e.matches('[data-replies]')
        ? 'expander'
        : e.matches('.post-more')
          ? 'more'
          : (e.getAttribute('data-post-action') ?? e.textContent),
    );

  it('orders the phone footer expander, reply, quote, then More', async () => {
    isPhone.value = true;
    await drawAll(post({ reply_count: 2 }));
    expect(footer()).toEqual(['expander', 'reply', 'quote', 'more']);
  });

  it('keeps the desktop footer order', async () => {
    await drawAll(post({ reply_count: 2 }));
    expect(footer()).toEqual(['expander', 'quote', 'reply', 'link', 'conversation', 'flag', 'history']);
  });

  it("keeps the header's DOM order", async () => {
    for (const phone of [false, true]) {
      isPhone.value = phone;
      await drawAll(
        post({
          author: { ...post().author, role: 'moderator' },
          edited_at: Date.now(),
          edited_by_moderator: false,
          reply_to: 1,
          reply_to_suppressed: false,
        }),
      );
      const names = [...root.querySelectorAll('.post-header > *')].map((e) => e.className || e.localName);
      expect(names, `phone ${phone}`).toEqual(
        phone
          ? ['ordinal', 'author hit', 'role-badge', 'time', 'edited', 'reply-to hit']
          : ['ordinal hit', 'author hit', 'role-badge', 'time', 'edited', 'reply-to hit'],
      );
    }
  });
});

// Author names link to the author's profile (TKT-01M3V3CFMA; docs/06
// "Targets and gestures"), with the phone's hit area, whatever the post.
describe('the author link', () => {
  const link = () => root.querySelector<HTMLAnchorElement>('.post-header a.author')!;

  it.each([false, true])('post author names link to their profile, phone %s', async (phone) => {
    isPhone.value = phone;
    await draw(post({ author: { ...post().author, handle: 'mo-ritz', display_name: 'Moritz' } }));
    expect(link().getAttribute('href')).toBe('/u/mo-ritz');
    expect(link().textContent).toBe('Moritz');
    expect(link().classList.contains('hit')).toBe(true);
  });

  it('falls back to the handle, as an anonymized account has no display name', async () => {
    await draw(post({ author: { ...post().author, handle: 'deleted-user-7', display_name: '' } }));
    expect(link().getAttribute('href')).toBe('/u/deleted-user-7');
    expect(link().textContent).toBe('deleted-user-7');
  });

  it('is on a removed post shown with its body', async () => {
    await draw(post({ deleted: true, removal: 'moderator' }));
    expect(link().getAttribute('href')).toBe('/u/carol');
  });

  // The phone's stacked header has room for one 44 px target over another
  // only if one of them is text: the ordinal is, as the phone mockup draws
  // it, and the permalink is under More.
  it('leaves the ordinal as text on a phone, with the permalink under More', async () => {
    isPhone.value = true;
    await draw(post());
    const ordinal = root.querySelector('.post-header .ordinal')!;
    expect(ordinal.localName).toBe('span');
    expect(ordinal.textContent).toBe('#7');
    await act(async () => root.querySelector<HTMLButtonElement>('[data-post-more]')!.click());
    expect(root.querySelector('.post-more-list a[href="/t/3yymw47g00001/s/7"]')?.textContent).toBe('link');
    isPhone.value = false;
    await draw(post());
    expect(root.querySelector('.post-header a.ordinal')?.getAttribute('href')).toBe('/t/3yymw47g00001/s/7');
  });
});

// A quoted post's backlinks (docs/06 "Post anatomy", TKT-01M3YTHAZB).
describe('quoted by', () => {
  const line = () => root.querySelector<HTMLElement>('.post [data-quoted-by]');
  const links = () => [...root.querySelectorAll<HTMLAnchorElement>('[data-quoted-by] a')];

  it('draws nothing when nothing quotes the post', async () => {
    await draw(post({ quoted_by: [] }));
    expect(line()).toBeNull();
  });

  it('links each quoting post, in order, between the body and the footer', async () => {
    await draw(post({ quoted_by: [51, 60] }));
    expect(line()!.textContent).toBe('Quoted by #51 #60');
    expect(links().map((a) => [a.getAttribute('href'), a.dataset.replyTo, a.classList.contains('hit')])).toEqual([
      ['#post-51', '51', true],
      ['#post-60', '60', true],
    ]);
    // After the body and its attachments, and before the footer.
    const order = [...root.querySelector('.post-main')!.children].map((e) => e.classList[0] ?? e.localName);
    expect(order.indexOf('quoted-by')).toBeGreaterThan(order.indexOf('post-body'));
    expect(order.indexOf('post-footer')).toBe(order.indexOf('quoted-by') + 1);
  });

  it('links ten and says how many more past that', async () => {
    const twelve = Array.from({ length: 12 }, (_, i) => 20 + i);
    await draw(post({ quoted_by: twelve }));
    expect(links().map((a) => Number(a.dataset.replyTo))).toEqual(twelve.slice(0, 10));
    expect(line()!.textContent).toMatch(/#29 and 2 more$/);
    await draw(post({ quoted_by: twelve.slice(0, 10) }));
    expect(links()).toHaveLength(10);
    expect(line()!.textContent).not.toMatch(/more/);
  });

  it('draws nothing on a tombstone', async () => {
    await draw(post({ quoted_by: [51], deleted: true, removal: 'author', html: '' }));
    expect(line()).toBeNull();
  });

  it('is drawn on a removed post a moderator is shown with its body', async () => {
    await draw(post({ quoted_by: [51], deleted: true, removal: 'moderator' }));
    expect(line()!.textContent).toBe('Quoted by #51');
  });
});
