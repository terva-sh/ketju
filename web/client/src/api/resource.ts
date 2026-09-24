import { useEffect, useState } from 'preact/hooks';
import { asProblem, type ProblemError } from './problem';

export interface Resource<T> {
  data: T | undefined;
  error: ProblemError | undefined;
  // loading is true only while a request for this key is outstanding. Every
  // request ends in data or an error, so no screen spins forever.
  loading: boolean;
  retry(): void;
}

interface Answer<T> {
  key: string;
  data: T | undefined;
  error: ProblemError | undefined;
  loading: boolean;
}

// useResource runs fetcher for key and answers with its result, SWR-style:
// peek's value, a held copy, shows at once while the request revalidates it.
//
// The answer is keyed. It is read only when it is for the key being asked
// about, so moving to another key never shows the last key's data or error,
// not even for the one render before the new request starts. A superseded
// request is aborted, and whatever it settles with is ignored. A failure
// keeps any data this key already had, beside the error. A null key asks for
// nothing.
export function useResource<T>(
  key: string | null,
  fetcher: (signal: AbortSignal) => Promise<T>,
  peek?: () => T | undefined,
): Resource<T> {
  const [answer, setAnswer] = useState<Answer<T> | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (key === null) return;
    const ctl = new AbortController();
    setAnswer((a) => ({
      key,
      data: a?.key === key ? a.data : peek?.(),
      error: undefined,
      loading: true,
    }));
    fetcher(ctl.signal).then(
      (data) => {
        if (!ctl.signal.aborted) setAnswer({ key, data, error: undefined, loading: false });
      },
      (err) => {
        // Only this request's own abort is silent. Any other failure, an
        // abort from somewhere else included, ends the request in an error,
        // or the screen would wait on it forever.
        if (ctl.signal.aborted) return;
        setAnswer((a) => ({ key, data: a?.key === key ? a.data : undefined, error: asProblem(err), loading: false }));
      },
    );
    return () => ctl.abort();
  }, [key, attempt]);

  const retry = () => setAttempt((n) => n + 1);
  if (key === null) return { data: undefined, error: undefined, loading: false, retry };
  if (answer?.key !== key) return { data: peek?.(), error: undefined, loading: true, retry };
  return { data: answer.data, error: answer.error, loading: answer.loading, retry };
}
