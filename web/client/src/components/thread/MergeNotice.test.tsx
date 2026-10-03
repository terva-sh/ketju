import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { MoveNotice as MoveNoticeDTO, ThreadPage } from '../../api/dto';
import member from '../../../../../internal/dto/testdata/golden/thread-merged.member.json';
import anon from '../../../../../internal/dto/testdata/golden/thread-merged.anon.json';
import { MergeNotice } from './removal';

// The notice above a merge's first copy (TKT-01M29CYSBA; docs/07 "Moving
// posts"), as the server draws it for a reader of the source and for a
// viewer it does not tell where the posts came from.

let root: HTMLElement;
beforeEach(() => {
  root = document.createElement('div');
  document.body.replaceChildren(root);
});
afterEach(async () => {
  await act(async () => render(null, root));
});

const noticeOf = (p: ThreadPage): MoveNoticeDTO => p.posts.find((x) => x.merge_notice)!.merge_notice!;
async function draw(notice: MoveNoticeDTO) {
  await act(async () => render(<MergeNotice notice={notice} />, root));
  return root.querySelector('[data-merge-notice]')!;
}

describe('MergeNotice', () => {
  it('links a reader of the source at the first moved post there', async () => {
    const n = await draw(noticeOf(member as ThreadPage));
    expect(n.textContent).toBe('1 post was merged from Why we left Slack.');
    expect(n.querySelector('a')!.getAttribute('href')).toBe('/t/3yymw47g00001/why-we-left-slack/64');
  });

  it('tells a viewer who may not read the source only that posts were merged in', async () => {
    const n = await draw(noticeOf(anon as ThreadPage));
    expect(n.textContent).toBe('1 post was merged from another thread.');
    expect(n.querySelector('a')).toBeNull();
  });

  it('counts more than one', async () => {
    const n = await draw({ count: 3, thread: null, ordinal: null });
    expect(n.textContent).toBe('3 posts were merged from another thread.');
  });
});
