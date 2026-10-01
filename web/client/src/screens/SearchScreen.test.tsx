import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, SearchResults } from '../api/dto';
import { isPhone } from '../state/layout';
import { viewer } from '../state/viewer';
import { problemAnswer, renderAt, route, screenOf, settle, stubServer, type StubServer, unmount } from '../testing';
import memberMe from '../../../../internal/dto/testdata/golden/me.member.json';
import forumsGolden from '../../../../internal/dto/testdata/golden/forums.member.json';
import searchGolden from '../../../../internal/dto/testdata/golden/search.member.json';
import emptyGolden from '../../../../internal/dto/testdata/golden/search-empty.anonymous.json';
import css from './search.css?raw';

// /search (TKT-01M3V70X0F, docs/07 "Search"), against a stubbed server that
// answers with the goldens.

const found = () => structuredClone(searchGolden) as SearchResults;
const nothing = () => ({ ...(structuredClone(emptyGolden) as SearchResults), indexing: false });

let server: StubServer;
let savedViewer: Me;
const savedPhone = isPhone.value;
const tz = process.env.TZ;
beforeEach(() => {
  savedViewer = viewer.value;
  viewer.value = structuredClone(memberMe) as Me;
  isPhone.value = false;
  server = stubServer({
    'GET /api/v1/forums': () => ({ status: 200, body: forumsGolden }),
    'GET /api/v1/search?q=bill': () => ({ status: 200, body: found() }),
  });
});
afterEach(async () => {
  await unmount();
  viewer.value = savedViewer;
  isPhone.value = savedPhone;
  if (tz === undefined) delete process.env.TZ;
  else process.env.TZ = tz;
  vi.unstubAllGlobals();
});

const searches = () => server.sent.filter((s) => s.url.startsWith('/api/v1/search')).map((s) => s.url);
const field = <T extends HTMLElement>(root: HTMLElement, name: string) => root.querySelector<T>(`[name="${name}"]`)!;
const results = (root: HTMLElement) => [...root.querySelectorAll<HTMLElement>('.search-result')];
const status = (root: HTMLElement) => root.querySelector('[role=status]')!.textContent;

async function type(el: HTMLInputElement | HTMLSelectElement, value: string) {
  await act(async () => {
    el.value = value;
    el.dispatchEvent(new Event(el instanceof HTMLSelectElement ? 'change' : 'input'));
  });
}

async function submit(root: HTMLElement) {
  await act(async () => {
    root.querySelector('form[role=search]')!.dispatchEvent(new Event('submit', { cancelable: true }));
  });
  await settle(root);
}

describe('with no query', () => {
  it('says what search covers, asks the server nothing, and puts focus in the field', async () => {
    const root = await renderAt('/search');
    expect(screenOf(root)).toBe('search');
    expect(root.querySelector('[data-search-intro]')!.textContent).toContain('forums you can read');
    expect(document.activeElement).toBe(field(root, 'q'));
    expect(field(root, 'q').hasAttribute('data-autofocus')).toBe(true);
    expect(searches()).toEqual([]);
  });

  // The route focuser runs after the screen's own effects, and would move
  // focus to the heading; data-autofocus keeps it in the field.
  it('keeps focus in the field when the reader arrives from another screen', async () => {
    const root = await renderAt('/');
    await route('/search');
    await settle(root);
    expect(screenOf(root)).toBe('search');
    expect(document.activeElement).toBe(field(root, 'q'));
  });

  it('leaves focus alone when there is a query', async () => {
    const root = await renderAt('/search?q=bill');
    expect(document.activeElement).not.toBe(field(root, 'q'));
    expect(field(root, 'q').hasAttribute('data-autofocus')).toBe(false);
  });
});

describe('the form', () => {
  it('fetches nothing while typing, and puts the query and filters in the address on Search, without the cursor', async () => {
    server.routes['GET /api/v1/search?q=bill&cursor=MjU'] = () => ({ status: 200, body: nothing() });
    server.routes['GET /api/v1/search?q=hosting+bill&forum=general&author=alice'] = () => ({ status: 200, body: found() });
    const root = await renderAt('/search?q=bill&cursor=MjU');
    expect(field<HTMLInputElement>(root, 'q').value).toBe('bill');
    await type(field(root, 'q'), '  hosting bill ');
    await type(field(root, 'author'), 'alice');
    await type(field(root, 'forum'), 'general');
    expect(searches()).toEqual(['/api/v1/search?q=bill&cursor=MjU']);

    await submit(root);
    expect(location.pathname + location.search).toBe('/search?q=hosting+bill&forum=general&author=alice');
    expect(searches()).toEqual(['/api/v1/search?q=bill&cursor=MjU', '/api/v1/search?q=hosting+bill&forum=general&author=alice']);
    expect(results(root)).toHaveLength(2);
    // The form stayed the same form: its fields hold what was searched.
    expect(field<HTMLInputElement>(root, 'q').value).toBe('hosting bill');
  });

  // Terva review 1799: Search on the address already shown routes nowhere,
  // so it asks the server again itself, and shows nothing from before while
  // that request is out.
  it('asks again when Search submits the address already shown, hiding the last answer meanwhile', async () => {
    let release!: () => void;
    let calls = 0;
    server.routes['GET /api/v1/search?q=bill'] = async () => {
      if (++calls === 1) return { status: 200, body: found() };
      await new Promise<void>((r) => (release = r));
      return { status: 200, body: nothing() };
    };
    const root = await renderAt('/search?q=bill');
    expect(results(root)).toHaveLength(2);
    await submit(root);
    expect(searches()).toEqual(['/api/v1/search?q=bill', '/api/v1/search?q=bill']);
    expect(results(root)).toHaveLength(0);
    expect(root.querySelector('[data-searching]')).not.toBeNull();
    await act(async () => release());
    await settle(root);
    expect(root.querySelector('[data-no-results]')).not.toBeNull();
    expect(location.search).toBe('?q=bill');
  });

  it('retries a refused query on Search, though the field is unchanged', async () => {
    let calls = 0;
    server.routes['GET /api/v1/search?q=bill'] = () =>
      ++calls === 1
        ? problemAnswer('validation-failed', 422, { errors: [{ field: 'q', code: 'too_long' }] })
        : { status: 200, body: found() };
    const root = await renderAt('/search?q=bill');
    expect(root.querySelector('[data-q-error]')).not.toBeNull();
    await submit(root);
    expect(root.querySelector('[data-q-error]')).toBeNull();
    expect(results(root)).toHaveLength(2);
  });

  // Terva review 1799: an answer is never shown again before the server is
  // asked again, even when the query is cleared and then typed again.
  it('shows no earlier answer when a cleared query is searched again', async () => {
    let calls = 0;
    server.routes['GET /api/v1/search?q=bill'] = () =>
      ++calls === 1 ? { status: 200, body: found() } : new Promise(() => {});
    const root = await renderAt('/search?q=bill');
    expect(results(root)).toHaveLength(2);
    await type(field(root, 'q'), '');
    await submit(root);
    expect(location.search).toBe('');
    expect(root.querySelector('[data-search-intro]')).not.toBeNull();
    await type(field(root, 'q'), 'bill');
    await submit(root);
    expect(location.search).toBe('?q=bill');
    expect(results(root)).toHaveLength(0);
    expect(root.querySelector('[data-searching]')).not.toBeNull();
    expect(calls).toBe(2);
  });

  it('offers every forum, children indented under their parent, after All forums', async () => {
    const root = await renderAt('/search');
    const options = [...field<HTMLSelectElement>(root, 'forum').options].map((o) => [o.value, o.textContent]);
    expect(options[0]).toEqual(['', 'All forums']);
    expect(options[1]).toEqual(['general', 'General']);
    expect(options[2][0]).toBe('general-events');
    expect(options[2][1]).toMatch(/^ +\S/);
  });

  // The dates are local days, and To includes its day. Los Angeles moves its
  // clocks on 2026-03-08, so that day is 23 hours long: a day is not 24 h
  // of ms, and the bounds are each local midnight.
  it('sends From as local midnight and To as the next local midnight, in the reader\'s zone', async () => {
    process.env.TZ = 'America/Los_Angeles';
    const after = Date.UTC(2026, 2, 8, 8);
    const before = Date.UTC(2026, 2, 9, 7);
    const want = `/api/v1/search?q=bill&after=${after}&before=${before}`;
    server.routes[`GET ${want}`] = () => ({ status: 200, body: found() });
    const root = await renderAt('/search?q=bill');
    await type(field(root, 'after'), '2026-03-08');
    await type(field(root, 'before'), '2026-03-08');
    await submit(root);
    expect(location.search).toBe(`?q=bill&after=${after}&before=${before}`);
    expect(searches().at(-1)).toBe(want);
    // And back again: the fields show the days the address names.
    expect(field<HTMLInputElement>(root, 'after').value).toBe('2026-03-08');
    expect(field<HTMLInputElement>(root, 'before').value).toBe('2026-03-08');
  });

  it('shows a refused query beside the field', async () => {
    server.routes['GET /api/v1/search?q=a+b'] = () =>
      problemAnswer('validation-failed', 422, { errors: [{ field: 'q', code: 'too_many' }] });
    const root = await renderAt('/search?q=a+b');
    const q = field(root, 'q');
    const error = root.querySelector('[data-q-error]')!;
    expect(error.textContent).toBe('Keep a search to 16 words or fewer.');
    expect(q.getAttribute('aria-invalid')).toBe('true');
    expect(q.getAttribute('aria-describedby')).toContain(error.id);
    expect(root.querySelector('.error-panel')).toBeNull();
  });
});

describe('the filters', () => {
  it('are a fieldset, always shown, from 720 px up', async () => {
    const root = await renderAt('/search');
    const set = root.querySelector('fieldset.search-filters')!;
    expect(set.querySelector('legend')!.textContent).toBe('Filters');
    expect(set.querySelector('[name=forum]')).not.toBeNull();
    expect(root.querySelector('details')).toBeNull();
  });

  it('fold under a closed Filters disclosure on the phone when none is set', async () => {
    isPhone.value = true;
    const root = await renderAt('/search?q=bill');
    const d = root.querySelector<HTMLDetailsElement>('details.search-filters')!;
    expect(d.open).toBe(false);
    expect(d.querySelector('summary')!.textContent).toBe('Filters');
    expect(d.querySelector('[name=author]')).not.toBeNull();
    expect(root.querySelector('fieldset')).toBeNull();
  });

  it('start open on the phone when one is set, saying how many', async () => {
    isPhone.value = true;
    server.routes['GET /api/v1/search?q=bill&forum=general&author=alice'] = () => ({ status: 200, body: found() });
    const root = await renderAt('/search?q=bill&forum=general&author=alice');
    const d = root.querySelector<HTMLDetailsElement>('details.search-filters')!;
    expect(d.open).toBe(true);
    expect(d.querySelector('summary')!.textContent).toBe('Filters (2 set)');
  });

  it('offer Clear filters, which keeps the query', async () => {
    server.routes['GET /api/v1/search?q=bill&author=alice'] = () => ({ status: 200, body: found() });
    const root = await renderAt('/search?q=bill&author=alice');
    expect(root.querySelector('.search-filters [data-clear-filters]')!.getAttribute('href')).toBe('/search?q=bill');
  });
});

describe('results', () => {
  it('link to the post, or to the thread for a title match, with forum, author and time', async () => {
    const root = await renderAt('/search?q=bill');
    const [post, title] = results(root);
    expect(post.querySelector('.search-title')!.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/61');
    expect(post.querySelector('.search-title')!.textContent).toBe('Why we left Slack #61');
    expect(title.querySelector('.search-title')!.getAttribute('href')).toBe('/t/3yymw47g00004/the-bill-for-hosting');
    // The title is text: its angle brackets are characters, not a tag.
    expect(title.querySelector('.search-title')!.textContent).toBe('The bill for <hosting>');
    expect(title.querySelector('hosting')).toBeNull();

    expect(post.querySelector('[data-result-forum]')!.getAttribute('href')).toBe('/f/general');
    expect(post.querySelector('[data-result-forum]')!.textContent).toBe('General');
    expect(post.querySelector('[data-result-author]')!.textContent).toBe('alice');
    expect(post.querySelector('time')!.getAttribute('datetime')).toBe(new Date(1757500000000).toISOString());
    expect(status(root)).toBe('2 results on this page');
  });

  it('mark the matches and nothing else', async () => {
    const root = await renderAt('/search?q=bill');
    const [post] = results(root);
    expect([...post.querySelectorAll('mark')].map((m) => m.textContent)).toEqual(['bill']);
    expect(post.querySelector('.search-snippet')!.textContent).toBe('…the bill was the smaller half of it.');
  });

  // The snippet is plain text from the server: the client escapes it, and
  // only a match becomes an element, a mark.
  it('draw a snippet holding <script> and & as text, creating no element from it', async () => {
    const page = found();
    page.results[0].snippet = [
      { text: '<script>alert(1)</script> & <img src=x onerror=alert(2)> ', match: false },
      { text: '<b>bill</b>', match: true },
      { text: ' &amp; more', match: false },
    ];
    server.routes['GET /api/v1/search?q=bill'] = () => ({ status: 200, body: page });
    const root = await renderAt('/search?q=bill');
    const snippet = results(root)[0].querySelector('.search-snippet')!;
    expect(snippet.querySelector('script, img, b')).toBeNull();
    expect([...snippet.children].map((c) => c.tagName)).toEqual(['MARK']);
    expect(snippet.querySelector('mark')!.textContent).toBe('<b>bill</b>');
    expect(snippet.textContent).toBe('<script>alert(1)</script> & <img src=x onerror=alert(2)> <b>bill</b> &amp; more');
    expect(document.querySelector('script')).toBeNull();
  });

  it('offer More results through the cursor, and First results from a later page', async () => {
    const second = nothing();
    second.results = [found().results[1]];
    server.routes['GET /api/v1/search?q=bill&cursor=MjU'] = () => ({ status: 200, body: second });
    const root = await renderAt('/search?q=bill');
    const more = root.querySelector('[data-more]')!;
    expect(more.getAttribute('href')).toBe('/search?q=bill&cursor=MjU');
    expect(more.getAttribute('rel')).toBe('next');
    expect(root.querySelector('.search-pages')!.textContent).not.toContain('First results');

    await route('/search?q=bill&cursor=MjU');
    await settle(root);
    expect(results(root)).toHaveLength(1);
    expect(root.querySelector('[data-more]')).toBeNull();
    const first = [...root.querySelectorAll('.search-pages a')].find((a) => a.textContent === 'First results')!;
    expect(first.getAttribute('href')).toBe('/search?q=bill');
  });

  // Nothing a search answers is held (no-store, no ETag), since each answer
  // is judged against what the viewer may read when it is served.
  it('asks again on Back, and shows the earlier search', async () => {
    server.routes['GET /api/v1/search?q=zzz'] = () => ({ status: 200, body: nothing() });
    const root = await renderAt('/search?q=bill');
    await route('/search?q=zzz');
    await settle(root);
    expect(root.querySelector('[data-no-results]')).not.toBeNull();
    await act(async () => {
      history.replaceState(null, '', '/search?q=bill');
      dispatchEvent(new PopStateEvent('popstate'));
    });
    await settle(root);
    expect(results(root)).toHaveLength(2);
    expect(field<HTMLInputElement>(root, 'q').value).toBe('bill');
    expect(searches()).toEqual(['/api/v1/search?q=bill', '/api/v1/search?q=zzz', '/api/v1/search?q=bill']);
  });
});

// The stylesheet is in every page's bundle, beside the top bar's search link,
// which is class "search" (shell/TopBar.tsx): a rule for .search here would
// restyle that link on every screen.
describe('search.css', () => {
  it('styles nothing by the top bar link\'s class', () => {
    expect(css).not.toMatch(/\.search(?![\w-])/);
  });
});

describe('the other states', () => {
  it('names the query when nothing matches, with no Clear filters when none is set', async () => {
    server.routes['GET /api/v1/search?q=zzz'] = () => ({ status: 200, body: nothing() });
    const root = await renderAt('/search?q=zzz');
    const empty = root.querySelector('[data-no-results]')!;
    expect(empty.textContent).toBe('Nothing matches “zzz”.');
    expect(root.querySelector('[data-clear-filters]')).toBeNull();
    expect(root.querySelector('[data-indexing]')).toBeNull();
    expect(status(root)).toBe('No results');
  });

  it('offers Clear filters when nothing matches with a filter set', async () => {
    server.routes['GET /api/v1/search?q=zzz&forum=general'] = () => ({ status: 200, body: nothing() });
    const root = await renderAt('/search?q=zzz&forum=general');
    const clear = root.querySelector('[data-no-results] [data-clear-filters]')!;
    expect(clear.getAttribute('href')).toBe('/search?q=zzz');
  });

  it('says some posts may be missing while the index is built', async () => {
    server.routes['GET /api/v1/search?q=zzz'] = () => ({ status: 200, body: emptyGolden });
    const root = await renderAt('/search?q=zzz');
    expect(root.querySelector('[data-indexing]')!.textContent).toContain('some posts may be missing');
    expect(root.querySelector('[data-no-results]')).not.toBeNull();
  });

  it('says a forum filter names no forum the reader can see, and keeps it in the field', async () => {
    server.routes['GET /api/v1/search?q=bill&forum=hidden'] = () => problemAnswer('not-found', 404);
    const root = await renderAt('/search?q=bill&forum=hidden');
    const bad = root.querySelector('[data-bad-forum]')!;
    expect(bad.textContent).toContain('No forum you can see is called “hidden”.');
    expect(bad.querySelector('a')!.getAttribute('href')).toBe('/search?q=bill');
    expect(root.querySelector('.error-panel')).toBeNull();
    expect(field<HTMLSelectElement>(root, 'forum').value).toBe('hidden');
  });

  it('reports any other failure with Retry', async () => {
    let calls = 0;
    server.routes['GET /api/v1/search?q=bill'] = () => (++calls === 1 ? problemAnswer('internal', 500) : { status: 200, body: found() });
    const root = await renderAt('/search?q=bill');
    const panel = root.querySelector('.error-panel')!;
    expect(panel.textContent).toContain('The search could not be loaded.');
    await act(async () => panel.querySelector('button')!.click());
    await settle(root);
    expect(root.querySelector('.error-panel')).toBeNull();
    expect(results(root)).toHaveLength(2);
  });
});
