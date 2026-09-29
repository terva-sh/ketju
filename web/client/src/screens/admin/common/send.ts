import { apiSend } from '../../../api/client';
import { asProblem, isAbort, problemMessage, type ProblemError, type ProblemType } from '../../../api/problem';

// send is how an admin screen makes a change. It never throws for a refusal:
// it answers with the result, or with a sentence the screen shows beside the
// control that sent it. An abort still rejects, as apiSend's does, because a
// superseded request has nothing to say.
//
// Three refusals are the admin screens' everyday answers rather than
// failures, and get words that say what to do next. Anything else reads as
// it does everywhere else (src/api/problem.ts).
export type Sent<T> = { ok: true; value: T } | { ok: false; message: string; error: ProblemError };

const sentences: Partial<Record<ProblemType, string>> = {
  conflict: 'Someone else changed this while you were working. Reload to see the change, then try again.',
  'validation-failed': 'That was not accepted. Check what you entered and try again.',
  forbidden: 'Your account may not make this change.',
};

// sentence is the words for a failed change.
export function sentence(err: unknown): string {
  const p = asProblem(err);
  return sentences[p.type] ?? problemMessage(p).text;
}

export async function send<T>(
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  url: string,
  body?: unknown,
  init: { signal?: AbortSignal } = {},
): Promise<Sent<T>> {
  try {
    return { ok: true, value: await apiSend<T>(method, url, body, init) };
  } catch (err) {
    if (isAbort(err)) throw err;
    return { ok: false, message: sentence(err), error: asProblem(err) };
  }
}
