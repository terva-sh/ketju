import { problemMessage } from '../api/problem';

// ErrorPanel is how every screen reports a failed request: the words come
// from problemMessage, the one table (src/api/problem.ts), and there is
// always a way out, so no screen is left waiting on a request that ended.
//
// A Retry-After is shown and not acted on. Retrying on the reader's behalf
// would aim the client at a limiter whose purpose is to shed load, and would
// need cancelling whenever the screen changed; the reader decides when.
export function ErrorPanel({ error, what, onRetry }: { error: unknown; what?: string; onRetry?: () => void }) {
  const m = problemMessage(error);
  return (
    <div class="error-panel" role="alert">
      {what && <p>{what} could not be loaded.</p>}
      <p>{m.text}</p>
      {m.retryAfter !== null && (
        <p>
          Try again in {m.retryAfter} second{m.retryAfter === 1 ? '' : 's'}.
        </p>
      )}
      {m.signIn && <p data-sign-in>Sign in, then try again.</p>}
      {onRetry && (
        <button type="button" onClick={onRetry}>
          Retry
        </button>
      )}
    </div>
  );
}
