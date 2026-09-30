import { ProblemError } from './problem';

// hasStatus is whether err is a problem answered with one of statuses. A
// code can cover several statuses: unavailable is the server's own 503 and a
// proxy's 502 or 504. The rare screen whose words depend on which one
// answered asks here, rather than reading the status itself (src/api is the
// one place problems are classified). It is a module of its own, apart from
// problem.ts, so it ships only in the chunk that imports it, and the reader
// bundle does not carry it (TKT-01M3QMRE47).
export function hasStatus(err: unknown, ...statuses: number[]): boolean {
  return err instanceof ProblemError && statuses.includes(err.status);
}
