// Errors from the API, and the one place they become words (docs/07 "Error
// catalogue"). A screen never reads a problem's type or status to decide what
// to say: it passes the error to ErrorPanel, or asks isProblem, so the same
// failure reads the same everywhere.

// The catalogue's codes, as internal/problem/testdata/catalogue.json fixes
// them, and network, which is the client's own: the request never reached
// the server, so there is no response to carry a code.
export type ProblemType =
  | 'bad-request'
  | 'unauthenticated'
  | 'forbidden'
  | 'not-found'
  | 'method-not-allowed'
  | 'conflict'
  | 'gone'
  | 'payload-too-large'
  | 'unsupported-media-type'
  | 'validation-failed'
  | 'rate-limited'
  | 'internal'
  | 'not-implemented'
  | 'unavailable'
  | 'network';

// What a reader is told for each code. signIn is set only where signing in
// would change the answer: unauthenticated, and never forbidden, whose viewer
// is already signed in (docs/07).
const messages: Record<ProblemType, { text: string; signIn: boolean }> = {
  'bad-request': { text: 'That address or request is not one the server understands.', signIn: false },
  unauthenticated: { text: 'You need to sign in to see this.', signIn: true },
  forbidden: { text: 'Your account does not have access to this.', signIn: false },
  'not-found': { text: 'There is nothing here. It may never have existed, or you may not be able to see it.', signIn: false },
  'method-not-allowed': { text: 'That action is not available here.', signIn: false },
  conflict: { text: 'Something changed while you were working. Reload and try again.', signIn: false },
  gone: { text: 'This has been deleted.', signIn: false },
  'payload-too-large': { text: 'That is larger than the limit allows.', signIn: false },
  'unsupported-media-type': { text: 'The server could not accept that request.', signIn: false },
  'validation-failed': { text: 'That was not accepted. Check it and try again.', signIn: false },
  'rate-limited': { text: 'Too many requests. Wait a moment and try again.', signIn: false },
  internal: { text: 'Something went wrong on the server.', signIn: false },
  'not-implemented': { text: 'This server does not do that yet.', signIn: false },
  unavailable: { text: 'The server is briefly unavailable. Try again shortly.', signIn: false },
  network: { text: 'Could not reach the server. Check your connection and try again.', signIn: false },
};

// knownTypes is exported for the test that holds the table to the catalogue.
export const knownTypes = Object.keys(messages) as ProblemType[];

// Status to code, for an error response that is not problem+json: a proxy's
// page, or a server too broken to write one. Anything unlisted is internal.
const byStatus: Record<number, ProblemType> = {
  400: 'bad-request',
  401: 'unauthenticated',
  403: 'forbidden',
  404: 'not-found',
  405: 'method-not-allowed',
  409: 'conflict',
  410: 'gone',
  413: 'payload-too-large',
  415: 'unsupported-media-type',
  422: 'validation-failed',
  429: 'rate-limited',
  501: 'not-implemented',
  502: 'unavailable',
  503: 'unavailable',
  504: 'unavailable',
};

export class ProblemError extends Error {
  constructor(
    readonly type: ProblemType,
    readonly status: number,
    // Seconds to wait, from Retry-After, when the response carried one.
    readonly retryAfter: number | null = null,
  ) {
    super(type);
    this.name = 'ProblemError';
  }
}

// toProblem reads a failed response. The type is believed only when it is a
// code this client knows; the title is not kept, because the table below,
// not the server's text, is what a reader sees.
export async function toProblem(r: Response): Promise<ProblemError> {
  let type: ProblemType = byStatus[r.status] ?? 'internal';
  if ((r.headers.get('Content-Type') ?? '').startsWith('application/problem+json')) {
    try {
      const body = (await r.json()) as { type?: unknown };
      if (typeof body.type === 'string' && body.type in messages) type = body.type as ProblemType;
    } catch {
      // An unreadable body keeps the status's code.
    }
  }
  return new ProblemError(type, r.status, retryAfter(r.headers.get('Retry-After')));
}

// asProblem turns anything a request threw into a ProblemError: fetch
// rejects with a TypeError when the network fails.
export function asProblem(err: unknown): ProblemError {
  return err instanceof ProblemError ? err : new ProblemError('network', 0);
}

// Retry-After in whole seconds, as the server writes it (internal/problem).
// The HTTP-date form is not produced there and reads as none.
function retryAfter(raw: string | null): number | null {
  if (raw === null || !/^[0-9]+$/.test(raw.trim())) return null;
  return Number(raw.trim());
}

export function problemMessage(err: unknown): { text: string; signIn: boolean; retryAfter: number | null } {
  const p = asProblem(err);
  return { ...messages[p.type], retryAfter: p.retryAfter };
}

// revokesAccess is an answer that says the viewer may no longer see what they
// asked for: it is not there, was deleted, or is not theirs to read (signed
// out, or access withdrawn). Whatever was held for it is no longer theirs to
// be shown, not even while retrying.
export function revokesAccess(err: unknown): boolean {
  return (
    err instanceof ProblemError &&
    (err.type === 'not-found' || err.type === 'gone' || err.type === 'forbidden' || err.type === 'unauthenticated')
  );
}

export function isProblem(err: unknown, type: ProblemType): boolean {
  return err instanceof ProblemError && err.type === type;
}

// isAbort is an aborted request: superseded, not failed.
export function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError';
}
