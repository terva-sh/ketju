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

// latest numbers the requests to each URL, so an answer can tell whether a
// newer request for the same URL started after it. An entry lives only while
// a request for its URL is in flight.
const latest = new Map<string, number>();
let issued = 0;

// apiGet fetches url as JSON, conditionally when a body is held for it.
//
// Only the newest request for a URL writes what is held for it. Requests for
// one URL can overlap, and the network does not keep their order: an older
// one finishing last, with a 200 or a 304 about the tag it sent, would put
// back a body the newer one had replaced. An aborted request is old by
// definition. Either way its answer is returned to its caller and changes
// nothing here.
export async function apiGet<T>(url: string, init: { signal?: AbortSignal } = {}): Promise<T> {
  const n = ++issued;
  latest.set(url, n);
  try {
    return await conditionalGet<T>(url, init.signal, () => latest.get(url) === n && !init.signal?.aborted);
  } finally {
    if (latest.get(url) === n) latest.delete(url);
  }
}

async function conditionalGet<T>(url: string, signal: AbortSignal | undefined, current: () => boolean): Promise<T> {
  const prior = held.get(url);
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (prior) headers['If-None-Match'] = prior.etag;
  const r = await request(url, { headers, signal });
  if (r.status === 304) {
    // A 304 is an answer about the tag sent. With none sent there is no body
    // it can mean, and treating it as success would render nothing as data.
    if (!prior) throw new ProblemError('internal', 304);
    if (current()) hold(url, prior.etag, prior.body);
    return prior.body as T;
  }
  if (!r.ok) {
    const p = await toProblem(r);
    // An answer that withdraws access withdraws the held copy with it, so it
    // is not shown again while the next request is in flight.
    if (revokesAccess(p) && current()) held.delete(url);
    throw p;
  }
  const body = await json<T>(r);
  if (!current()) return body;
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
// something the caller knows to be out of date. With inFlight, a request to
// a matching URL still out is superseded too: it started before whatever
// made the copies stale, so its answer is returned to its caller and not
// held (Terva review 1624 on PR #248, after a mark-unread).
export function forgetGets(matches: (url: string) => boolean, opts: { inFlight?: boolean } = {}) {
  for (const url of [...held.keys()]) if (matches(url)) held.delete(url);
  if (opts.inFlight) for (const url of [...latest.keys()]) if (matches(url)) latest.delete(url);
}

// forgetHeld drops the body held for url, but only if it is body itself: a
// caller that has decided one answer is stale must not drop a newer one that
// replaced it there in the meantime.
export function forgetHeld(url: string, body: unknown) {
  if (held.get(url)?.body === body) held.delete(url);
}

// apiGetOptional fetches url as JSON where the server may answer 204 for
// "there is none", as the drafts routes do (docs/07 "Drafts"). It answers
// the body, null for 204, with the headers, since a draft's 204 carries its
// revision in one (Draft-Revision). Nothing is held: those answers are
// no-store.
export async function apiGetOptional<T>(url: string, init: { signal?: AbortSignal } = {}): Promise<{ body: T | null; headers: Headers }> {
  const r = await request(url, { headers: { Accept: 'application/json' }, signal: init.signal });
  if (!r.ok) throw await toProblem(r);
  return { body: r.status === 204 ? null : await json<T>(r), headers: r.headers };
}

// apiSend sends body as JSON, the only type the API accepts on a mutating
// request (docs/07 CSRF rule 1), and returns the JSON answer, or undefined
// for 204. An aborted request rejects with the abort, as apiGet's does.
// keepalive lets the request outlive the page that sent it, as the read
// beacon's must when the page is going away (state/readTracker.ts).
// status, when given, is the one success the caller accepts: any other 2xx
// is the server's fault, internal, as the flag dialog needs to say its
// thanks only for the 200 receipt (TKT-01M3QQWA5Z, Terva review 1544).
// onHeaders is handed the headers of a successful answer, which is how a
// draft's DELETE hands over the Draft-Revision its 204 carries.
//
// fetch is called before the first await, so a request made from a pagehide
// handler starts inside it (state/drafts.ts).
export async function apiSend<T>(
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  url: string,
  body?: unknown,
  init: { signal?: AbortSignal; keepalive?: boolean; status?: number; onHeaders?: (h: Headers) => void } = {},
): Promise<T> {
  let payload: string | undefined;
  try {
    payload = body === undefined ? undefined : JSON.stringify(body);
  } catch {
    // A cycle or a BigInt: no request can be made, and it is the client's
    // own fault, not the network's.
    throw new ProblemError('internal', 0);
  }
  const r = await request(url, {
    method,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: payload,
    signal: init.signal,
    ...(init.keepalive ? { keepalive: true } : {}),
  });
  if (!r.ok) throw await toProblem(r);
  if (init.status !== undefined && r.status !== init.status) throw new ProblemError('internal', r.status);
  init.onHeaders?.(r.headers);
  if (r.status === 204) return undefined as T;
  return json<T>(r);
}

// apiUpload sends file as the one multipart part named file, which is what
// POST /blobs and PUT /settings/avatar take (docs/07 "Attachments"), and
// returns the JSON answer. It sets no Content-Type, so the browser writes
// the multipart boundary.
export async function apiUpload<T>(url: string, file: File, method: 'POST' | 'PUT' = 'POST'): Promise<T> {
  const body = new FormData();
  body.append('file', file);
  const r = await request(url, { method, headers: { Accept: 'application/json' }, body });
  if (!r.ok) throw await toProblem(r);
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
  latest.clear();
}
