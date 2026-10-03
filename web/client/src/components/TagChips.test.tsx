import { render } from 'preact';
import { afterEach, describe, expect, it } from 'vitest';
import type { ThreadRow as Row } from '../api/dto';
import { TagChips, tagHref } from './TagChips';
import { ThreadRow } from './ThreadRow';
import list from '../../../../internal/dto/testdata/golden/threads.member.json';

// TKT-01M3YSQPZX: a thread's tags are chips, each a link to a forum's list
// filtered to it (docs/06 "Filtering by tag").

const host = document.createElement('div');
document.body.append(host);
afterEach(() => render(null, host));

const chips = () => [...host.querySelectorAll<HTMLAnchorElement>('.thread-tags a')].map((a) => [a.textContent, a.getAttribute('href')]);

describe('tag chips', () => {
  it('link each tag to the forum list filtered to it', () => {
    render(<TagChips tags={['garden', 'long-read']} forum="general" />, host);
    expect(host.querySelector('.thread-tags')?.getAttribute('aria-label')).toBe('Tags');
    expect(chips()).toEqual([
      ['garden', '/f/general?tag=garden'],
      ['long-read', '/f/general?tag=long-read'],
    ]);
    // Each takes the phone's enlarged target.
    expect([...host.querySelectorAll('.thread-tags a')].every((a) => a.classList.contains('hit'))).toBe(true);
  });

  it('draw nothing for a thread with no tags', () => {
    render(<TagChips tags={[]} forum="general" />, host);
    expect(host.innerHTML).toBe('');
  });

  it('escape the forum and the tag in the address', () => {
    expect(tagHref('a b', 'x&y')).toBe('/f/a%20b?tag=x%26y');
  });

  it("use the list's forum on a forum's own list", () => {
    const row = structuredClone(list.threads[0]) as Row;
    expect(row.forum).toBeNull();
    render(
      <ul>
        <ThreadRow thread={row} signedIn listForum="general" />
      </ul>,
      host,
    );
    expect(chips()).toEqual(row.tags.map((t) => [t, `/f/general?tag=${t}`]));
  });

  it("use the row's own forum on a cross-forum list", () => {
    const row = { ...structuredClone(list.threads[0]), forum: { slug: 'gardening', name: 'Gardening' } } as Row;
    render(
      <ul>
        <ThreadRow thread={row} signedIn />
      </ul>,
      host,
    );
    expect(chips()).toEqual(row.tags.map((t) => [t, `/f/gardening?tag=${t}`]));
  });
});
