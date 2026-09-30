// The words for a refused handle, shared by approving an application
// (applications/Decision.tsx) and renaming an account (RenameControl.tsx),
// so one rule reads the same wherever a staff member meets it. The codes are
// internal/service/identity's (handle.go), each with one meaning. They are
// not form field codes, because a handle is no form's field.
//
// There is no code for a handle given up by a rename: the server answers
// taken for it, so the words for taken say it may have been given up
// recently (docs/05 "Changing a handle").
const handleWords: Record<string, string> = {
  invalid_handle:
    'A handle is 3 to 30 characters: lowercase letters, digits, _ and -. It starts with a letter or digit and does not end with -.',
  reserved: 'That handle is kept for the site itself. Choose another.',
  taken: 'That handle is in use, or was given up too recently to reuse. Choose another.',
  same: "That is already this account's handle.",
};

export function handleMessage(code: string): string {
  return Object.hasOwn(handleWords, code) ? handleWords[code] : 'That handle was not accepted. Choose another.';
}

// isAnonymizedHandle is how the client knows an anonymized account until the
// admin detail carries a flag for it: registration refuses the prefix
// (domain.AnonymizedHandlePrefix), so only an anonymized account holds one.
// The server decides by its own column, whatever this answers.
export function isAnonymizedHandle(handle: string): boolean {
  return handle.toLowerCase().startsWith('deleted-user-');
}
