// The words for a refused handle moved to src/handles.ts (TKT-01M3V6XTYG),
// where the member's own settings screen reads them too.

// isAnonymizedHandle is how the client knows an anonymized account until the
// admin detail carries a flag for it: registration refuses the prefix
// (domain.AnonymizedHandlePrefix), so only an anonymized account holds one.
// The server decides by its own column, whatever this answers.
export function isAnonymizedHandle(handle: string): boolean {
  return handle.toLowerCase().startsWith('deleted-user-');
}
