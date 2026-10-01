import { afterEach, describe, expect, it } from 'vitest';
import { isListUrl, listFor, rememberList, resetLists } from './lists';

afterEach(() => resetLists());

describe('listFor', () => {
  it('is the forum list last shown when it was this forum’s', () => {
    rememberList('general', '/f/general?cursor=c');
    expect(listFor('general')).toBe('/f/general?cursor=c');
    expect(listFor('events')).toBe('/f/events');
  });

  // TKT-01M3V6Y4JP: a cross-forum list holds every forum's threads.
  it('is a cross-forum list last shown, for a thread in any forum', () => {
    rememberList(null, '/watching?cursor=c');
    expect(listFor('general')).toBe('/watching?cursor=c');
    expect(listFor('events')).toBe('/watching?cursor=c');
    rememberList('events', '/f/events');
    expect(listFor('general')).toBe('/f/general');
  });

  it('is the forum’s first page with nothing remembered', () => {
    expect(listFor('a b')).toBe('/f/a%20b');
  });
});

describe('isListUrl', () => {
  it('is every answer that carries the viewer’s unread counts', () => {
    for (const u of [
      '/api/v1/forums',
      '/api/v1/forums/general/threads?cursor=x',
      '/api/v1/unread',
      '/api/v1/new?cursor=a',
      '/api/v1/participated',
      '/api/v1/watching?cursor=b',
    ]) {
      expect(isListUrl(u), u).toBe(true);
    }
  });

  it('is nothing else', () => {
    for (const u of ['/api/v1/me', '/api/v1/threads/abc?page=1', '/api/v1/newsletter', '/api/v1/unread/x', '/api/v1/search?q=new']) {
      expect(isListUrl(u), u).toBe(false);
    }
  });
});
