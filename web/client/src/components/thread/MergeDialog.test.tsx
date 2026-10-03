import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MergedPosts, ThreadHeader, ThreadPage } from '../../api/dto';
import { resetApiCache } from '../../api/client';
import { cachedThread, resetThreadCache } from '../../state/threads';
import { problemAnswer, stubServer, type Answer, type StubServer } from '../../testing';
import golden from '../../../../../internal/dto/testdata/golden/thread.member.json';
import { MergeDialog, mergeRefusal, threadIdFrom } from './MovePosts';

// Merging selected posts into another thread (TKT-01M29CYSBA; docs/06
// "Moderator flow for drift", docs/07 "Moving posts"): the merge dialog,
// against a stubbed server.

const page = golden as ThreadPage;
const source: ThreadHeader = page.thread;
const targetId = '3yymw47g00042';
const target: ThreadHeader = {
  ...source,
  id: targetId,
  slug: 'slack-exports',
  title: 'Slack exports',
  forum: { slug: 'meta', name: 'Meta' },
  post_count: 9,
};
const lookup = `GET /api/v1/threads/${targetId}?page=1`;
const mergeUrl = `POST /api/v1/admin/threads/${source.id}/merge`;

let root: HTMLElement;
let opener: HTMLButtonElement;
let server: StubServer;
let found: () => Answer;
let answer: (b: unknown) => Answer;
const onClose = vi.fn();
const onMerge = vi.fn();
const merged: MergedPosts = {
  source: { ...source, log_length: source.log_length + 100 },
  target: { ...target, post_count: 11, log_length: target.log_length + 200 },
  first_copy: 10,
};

beforeEach(() => {
  root = document.createElement('div');
  opener = document.createElement('button');
  opener.textContent = 'Merge…';
  document.body.replaceChildren(opener, root);
  resetApiCache();
  resetThreadCache();
  onClose.mockReset();
  onMerge.mockReset();
  found = () => ({ status: 200, body: { ...page, thread: target } });
  answer = () => ({ status: 200, body: merged });
  server = stubServer({ [lookup]: () => found(), [mergeUrl]: (b) => answer(b) });
});
afterEach(async () => {
  await act(async () => render(null, root));
  vi.unstubAllGlobals();
});

const wait = () => act(() => new Promise((r) => setTimeout(r, 5)));
const press = (key: string) =>
  act(async () => void (document.activeElement ?? document).dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })));

async function open() {
  await act(async () =>
    render(<MergeDialog source={source} ordinals={[61, 62]} onClose={onClose} onMerge={onMerge} returnTo={() => opener} />, root),
  );
  await wait();
  return root.querySelector<HTMLElement>('[data-merge-dialog]')!;
}
async function type(d: HTMLElement, text: string) {
  const t = d.querySelector<HTMLInputElement>('input[type=text]')!;
  t.value = text;
  await act(async () => void t.dispatchEvent(new Event('input', { bubbles: true })));
  await wait();
}
const status = (d: HTMLElement) => d.querySelector('#merge-status')!.textContent;
const mergeButton = (d: HTMLElement) => d.querySelector<HTMLButtonElement>('.flag-send')!;
const merges = () => server.sent.filter((r) => r.method === 'POST');

describe('threadIdFrom', () => {
  it.each([
    [targetId, targetId],
    ['  3YYMW47G00042 ', targetId],
    [`/t/${targetId}/slack-exports`, targetId],
    [`/t/${targetId}/slack-exports/7`, targetId],
    [`https://forum.example/t/${targetId}/slack-exports?page=2#post-7`, targetId],
    [`${location.origin}/t/${targetId}`, targetId],
    ['', null],
    ['slack exports', null],
    ['3yymw47g0004', null], // twelve characters
    ['3yymw47g00u42', null], // u is not in the alphabet
    ['zyymw47g00042', null], // more than 64 bits
    [`/f/meta/${targetId}`, null],
    ['/t/%E0%A4%A/x', null],
  ])('reads %j as %j', (text, want) => {
    expect(threadIdFrom(text)).toBe(want);
  });
});

describe('the merge dialog', () => {
  it('is modal, takes focus, traps Tab, says what a merge does, and gives focus back on Escape', async () => {
    const d = await open();
    expect(d.getAttribute('role')).toBe('dialog');
    expect(d.getAttribute('aria-modal')).toBe('true');
    expect(d.querySelector('h2')!.textContent).toBe('Merge 2 posts into another thread');
    expect(document.activeElement).toBe(d.querySelector('input[type=text]'));
    expect(d.textContent).toContain(
      'The posts move to the end of that thread, keeping their authors and times. To undo, select them there and merge them back.',
    );
    const cancel = [...d.querySelectorAll('button')].find((b) => b.textContent === 'Cancel')!;
    cancel.focus();
    await act(async () => void document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true })));
    expect(d.contains(document.activeElement)).toBe(true);
    await press('Escape');
    expect(onClose).toHaveBeenCalled();
  });

  it('shows the thread a link names before it offers Merge', async () => {
    const d = await open();
    expect(mergeButton(d).disabled).toBe(true);
    await type(d, `https://forum.example/t/${targetId}/slack-exports/4`);
    expect(server.sent.map((r) => `${r.method} ${r.url}`)).toEqual([lookup]);
    expect(d.querySelector('[data-merge-target]')!.textContent).toBe('Slack exports in Meta');
    expect(d.querySelector('#merge-status')!.getAttribute('aria-live')).toBe('polite');
    expect(mergeButton(d).disabled).toBe(false);
    // Changing the text takes the preview, and Merge, away until it names a
    // thread again.
    await type(d, `https://forum.example/t/${targetId.slice(0, 12)}`);
    expect(d.querySelector('[data-merge-target]')).toBeNull();
    expect(status(d)).toBe('Paste a link to a thread, or its 13-character id.');
    expect(mergeButton(d).disabled).toBe(true);
  });

  it('says a hidden thread and a missing one alike', async () => {
    for (const [type_, code] of [
      ['not-found', 404],
      ['forbidden', 403],
    ] as const) {
      found = () => problemAnswer(type_, code);
      const d = await open();
      await type(d, targetId);
      expect(status(d)).toBe('No thread you can read has that address.');
      expect(d.querySelector('input')!.getAttribute('aria-invalid')).toBe('true');
      expect(mergeButton(d).disabled).toBe(true);
      await act(async () => render(null, root));
    }
  });

  it('refuses the thread the posts are in', async () => {
    const d = await open();
    await type(d, `/t/${source.id}/${source.slug}`);
    expect(status(d)).toBe('That is this thread. Choose another.');
    expect(server.sent).toEqual([]);
    expect(mergeButton(d).disabled).toBe(true);
  });

  it('offers a retry when the lookup fails for another reason', async () => {
    found = () => problemAnswer('unavailable', 503);
    const d = await open();
    await type(d, targetId);
    found = () => ({ status: 200, body: { ...page, thread: target } });
    const retry = [...d.querySelectorAll('button')].find((b) => b.textContent === 'Retry')!;
    await act(async () => retry.click());
    await wait();
    expect(d.querySelector('[data-merge-target]')!.textContent).toBe('Slack exports in Meta');
  });

  it('merges, stores both headers, and goes to the target at the first copy', async () => {
    const d = await open();
    await type(d, targetId);
    await act(async () => mergeButton(d).click());
    await wait();
    expect(merges()).toEqual([
      { method: 'POST', url: `/api/v1/admin/threads/${source.id}/merge`, body: { ordinals: [61, 62], into: targetId } },
    ]);
    // The first copy is the one the answer names.
    expect(onMerge).toHaveBeenCalledWith(merged.target, 10);
    expect(cachedThread(source.id)?.header.log_length).toBe(merged.source.log_length);
    expect(cachedThread(targetId)?.header.post_count).toBe(11);
  });

  // Terva review 2058: the landing is the answer's first_copy, not counted
  // back from the header, which a reply landing in the target after the
  // merge has already moved on.
  it('lands on the first copy the answer names, whatever the target now counts', async () => {
    answer = () => ({ status: 200, body: { ...merged, target: { ...merged.target, post_count: 12 } } });
    const d = await open();
    await type(d, targetId);
    await act(async () => mergeButton(d).click());
    await wait();
    expect(onMerge).toHaveBeenCalledWith({ ...merged.target, post_count: 12 }, 10);
  });

  // Terva review 2057, on the split dialog: a dialog closed while its merge
  // was on the way takes nobody to the target, but the caches still learn of
  // the merge.
  it('goes nowhere when closed before the merge answers', async () => {
    let release = () => {};
    server.routes[mergeUrl] = () => new Promise<Answer>((r) => (release = () => r({ status: 200, body: merged })));
    const d = await open();
    await type(d, targetId);
    await act(async () => mergeButton(d).click());
    await act(async () => render(null, root));
    release();
    await wait();
    expect(merges()).toHaveLength(1);
    expect(onMerge).not.toHaveBeenCalled();
    expect(cachedThread(targetId)?.header.post_count).toBe(11);
  });

  it.each([
    ['conflict', 409, 'A post may have been removed or moved, or a thread archived.'],
    ['forbidden', 403, 'You cannot move posts into that thread’s forum.'],
    ['not-found', 404, 'No thread you can read has that address. Or one of these posts is no longer there.'],
    ['validation-failed', 422, 'Choose another thread, and 1 to 10000 posts.'],
    ['unavailable', 503, 'If it was recorded, the posts arrive in that thread within a minute.'],
  ])('says why a %s refusal was given, and keeps what was typed', async (type_, code, words) => {
    answer = () => problemAnswer(type_, code);
    const d = await open();
    await type(d, targetId);
    await act(async () => mergeButton(d).click());
    await wait();
    expect(d.querySelector('[role=alert]')!.textContent).toContain(words);
    expect(d.querySelector<HTMLInputElement>('input[type=text]')!.value).toBe(targetId);
    expect(onMerge).not.toHaveBeenCalled();
    expect(mergeRefusal(new Error('x'))).not.toBe('');
  });
});
