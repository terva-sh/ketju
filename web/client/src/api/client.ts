import { asProblem, isAbort, ProblemError, revokesAccess, toProblem } from './problem';

// The API client (docs/07 "State"). Every request the client makes goes
// through here, so an error is always a ProblemError and a cached GET is
// always revalidated the same way.

// held is the last body and ETag per URL, most recently used last. The server
// answers If-None-Match with 304 when the body it would send is the one held
// (the tag hashes the body, docs/07), so a held body is served only after the
// server has said it is still right. It is bounded, because a reader who
// pages through a long thread would otherwise keep every page they passed.
const held = new Map<string, { etag: string; body: unknown }>();
export const heldLimit = 100;

function hold(url: string, etag: string, body: unknown) {
  held.delete(url);
  held.set(url, { etag, body });
  if (held.size > heldLimit) held.delete(held.keys().next().value!);
}

// apiGet fetches url as JSON, conditionally when a body is held for it.
export async function apiGet<T>(url: string, init: { signal?: AbortSignal } = {}): Promise<T> {
  const prior = held.get(url);
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (prior) headers['If-None-Match'] = prior.etag;
  const r = await request(url, { headers, signal: init.signal });
  if (r.status === 304) {
    // A 304 is an answer about the tag sent. With none sent there is no body
    // it can mean, and treating it as success would render nothing as data.
    if (!prior) throw new ProblemError('internal', 304);
    hold(url, prior.etag, prior.body);
    return prior.body as T;
  }
  if (!r.ok) {
    const p = await toProblem(r);
    // An answer that withdraws access withdraws the held copy with it, so it
    // is not shown again while the next request is in flight.
    if (revokesAccess(p)) held.delete(url);
    throw p;
  }
  const body = await json<T>(r);
  const etag = r.headers.get('ETag');
  if (etag) hold(url, etag, body);
  else held.delete(url);
  return body;
}

// peekGet is the body held for url, if any, without asking the server: what a
// screen can show at once while apiGet revalidates it.
export function peekGet<T>(url: string): T | undefined {
  return held.get(url)?.body as T | undefined;
}

// forgetGets drops every held body whose URL matches: the held copies of
// something the caller knows to be out of date.
export function forgetGets(matches: (url: string) => boolean) {
  for (const url of [...held.keys()]) if (matches(url)) held.delete(url);
}

// apiSend sends body as JSON, the only type the API accepts on a mutating
// request (docs/07 CSRF rule 1), and returns the JSON answer, or undefined
// for 204.
export async function apiSend<T>(method: 'POST' | 'PUT' | 'PATCH' | 'DELETE', url: string, body?: unknown): Promise<T> {
  const r = await request(url, {
    method,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!r.ok) throw await toProblem(r);
  if (r.status === 204) return undefined as T;
  return json<T>(r);
}

async function request(url: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(url, { credentials: 'same-origin', ...init });
  } catch (err) {
    if (isAbort(err)) throw err;
    throw asProblem(err);
  }
}

async function json<T>(r: Response): Promise<T> {
  try {
    return (await r.json()) as T;
  } catch (err) {
    if (isAbort(err)) throw err;
    // A 200 whose body is not JSON is the server's fault, not the network's.
    throw new ProblemError('internal', r.status);
  }
}

// resetApiCache empties the held bodies, for tests.
export function resetApiCache() {
  held.clear();
}
