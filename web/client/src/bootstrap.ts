// The viewer the server rendered into the page (docs/07 "Embedding"): the
// shell carries /me as a JSON data block so the first paint needs no round
// trip. It is never executed, which is why the CSP allows it.

export interface Author {
  handle: string;
  display_name: string;
  avatar: string;
  role: string;
  kind: string;
}

export interface Me {
  user: Author | null;
  unread: { threads: number; participated: number; watching: number };
  notifications: number;
  settings: { timezone: string; locale: string; page_size: number };
}

export const anonymous: Me = {
  user: null,
  unread: { threads: 0, participated: 0, watching: 0 },
  notifications: 0,
  settings: { timezone: '', locale: '', page_size: 25 },
};

// readBootstrap parses the data block, and falls back to an anonymous viewer
// when it does not parse. Under the Vite dev server the block is the raw
// template placeholder, because Vite, not Go, served the page.
export function readBootstrap(doc: Document = document): Me {
  const text = doc.getElementById('bootstrap')?.textContent ?? '';
  try {
    const me = JSON.parse(text) as Me;
    return me && typeof me === 'object' ? me : anonymous;
  } catch {
    return anonymous;
  }
}
