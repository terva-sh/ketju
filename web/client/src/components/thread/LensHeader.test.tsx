import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { Lens, Me } from '../../api/dto';
import member from '../../../../../internal/dto/testdata/golden/me.member.json';
import { viewer } from '../../state/viewer';
import { GapDivider, LensHeader } from './LensHeader';

// The lens header (TKT-01M29CTY71, docs/06 "Lenses"): what is filtered, the
// counts, Full thread, and the empty states.

const thread = { id: '0369k573g0001', slug: 'why-we-left-slack' };
const home = '/t/0369k573g0001/why-we-left-slack';
const lens = (kind: string, value = '', over: Partial<Lens> = {}): Lens => ({
  kind,
  value,
  shown: 7,
  hidden: 205,
  gaps: [],
  ...over,
});

let root: HTMLElement;
const saved = viewer.value;
const rect = Element.prototype.getBoundingClientRect;
beforeEach(() => {
  viewer.value = structuredClone(member) as Me;
  root = document.createElement('div');
  document.body.replaceChildren(root);
});
afterEach(async () => {
  await act(async () => render(null, root));
  viewer.value = saved;
  Element.prototype.getBoundingClientRect = rect;
});

// draw renders the header over a list of posts with these ordinals, as the
// thread body draws it.
async function draw(l: Lens, ordinals: number[]) {
  await act(async () =>
    render(
      <>
        <LensHeader lens={l} thread={thread} first={ordinals[0]} />
        <section class="posts">
          {ordinals.map((o) => (
            <article key={o} class="post" id={`post-${o}`} data-ordinal={o} />
          ))}
        </section>
      </>,
      root,
    ),
  );
}

const full = () => root.querySelector<HTMLAnchorElement>('[data-full-thread]')!.getAttribute('href');
const empty = () => root.querySelector('[data-lens-empty]');

describe('the lens header', () => {
  it('names the lens and counts what it shows and hides', async () => {
    await draw(lens('author', 'mara'), [3, 5]);
    expect(root.querySelector('.lens-what strong')?.textContent).toBe('Posts by mara');
    expect(root.querySelector('[data-lens-count]')?.textContent).toBe('Showing 7 of 212 posts · 205 hidden');
    expect(empty()).toBeNull();
  });

  it('writes a since lens’s moment as every other time is', async () => {
    const at = Date.UTC(2026, 8, 1);
    await draw(lens('since', String(at)), [3]);
    expect(root.querySelector('.lens-what time')?.getAttribute('datetime')).toBe(new Date(at).toISOString());
  });

  // AC11: the post at the top of the viewport, kept current as the reader
  // scrolls.
  it('sends Full thread to the post at the top of the viewport', async () => {
    let scrolled = 0;
    Element.prototype.getBoundingClientRect = function (this: Element) {
      const o = Number(this.getAttribute('data-ordinal') ?? 0);
      const top = o ? (o - 3) * 300 - scrolled : 0;
      return { top, bottom: top + 280, height: 280 } as DOMRect;
    };
    await draw(lens('author', 'mara'), [3, 4, 5]);
    expect(full()).toBe(`${home}/3`);
    scrolled = 400;
    await act(async () => {
      dispatchEvent(new Event('scroll'));
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    });
    expect(full()).toBe(`${home}/4`);
    scrolled = 700;
    await act(async () => {
      dispatchEvent(new Event('scroll'));
      await new Promise((r) => requestAnimationFrame(() => r(null)));
    });
    expect(full()).toBe(`${home}/5`);
  });

  it('measures below the phone’s sticky bar', async () => {
    const bar = document.createElement('header');
    bar.className = 'topbar phone';
    document.body.prepend(bar);
    Element.prototype.getBoundingClientRect = function (this: Element) {
      if (this === bar) return { top: 0, bottom: 53, height: 53 } as DOMRect;
      const o = Number(this.getAttribute('data-ordinal') ?? 0);
      // #3 ends under the bar.
      const top = o === 3 ? -200 : -200 + (o - 3) * 250;
      return { top, bottom: top + 250, height: 250 } as DOMRect;
    };
    await draw(lens('author', 'mara'), [3, 4]);
    expect(full()).toBe(`${home}/4`);
    bar.remove();
  });
});

describe('an empty lens', () => {
  const none = (kind: string, value = '') => lens(kind, value, { shown: 0, hidden: 212 });

  it('asks a visitor to sign in for unread and mentions', async () => {
    viewer.value = { ...structuredClone(member), user: null } as Me;
    history.replaceState(null, '', `${home}?lens=unread`);
    for (const kind of ['unread', 'mentions']) {
      await draw(none(kind), []);
      const a = empty()!.querySelector('a')!;
      expect(a.textContent).toBe('Sign in to see what is new here');
      expect(a.getAttribute('href')).toBe(`/auth/signin?return_to=${encodeURIComponent(`${home}?lens=unread`)}`);
    }
  });

  // On an empty page Full thread and the empty state's link go to the
  // thread's own address, where the landing rule places the reader.
  it('offers the full thread when nothing is unread, or an author or a date has no posts', async () => {
    for (const [kind, value, text] of [
      ['unread', '212', 'Nothing unread. Read the full thread'],
      ['author', 'mara', 'No posts here. Read the full thread'],
      ['since', String(Date.UTC(2030, 0, 1)), 'No posts here. Read the full thread'],
    ]) {
      await draw(none(kind, value), []);
      expect(empty()?.textContent, kind).toBe(text);
      expect(empty()!.querySelector('a')!.getAttribute('href')).toBe(home);
      expect(full()).toBe(home);
    }
  });

  it('says a one-post chain is neither a reply nor replied to', async () => {
    await draw(lens('chain', '3', { shown: 1, hidden: 211 }), [3]);
    expect(empty()?.textContent).toBe('#3 is not a reply and has no replies.');
  });
});

describe('the gap divider', () => {
  it('counts the skipped posts and links to the first of them', async () => {
    await act(async () => render(<GapDivider gap={{ after: 52, count: 8 }} thread={thread} />, root));
    const a = root.querySelector('.lens-gap a')!;
    expect(a.textContent).toBe('… 8 posts …');
    expect(a.getAttribute('href')).toBe(`${home}/53`);
    await act(async () => render(<GapDivider gap={{ after: 0, count: 1 }} thread={thread} />, root));
    expect(root.querySelector('.lens-gap a')!.textContent).toBe('… 1 post …');
  });
});
