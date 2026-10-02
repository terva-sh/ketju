// The words for a refused handle, shared by approving an application
// (screens/admin/applications/Decision.tsx), renaming an account
// (screens/admin/RenameControl.tsx) and a member changing their own
// (screens/account/AccountScreen.tsx), so one rule reads the same wherever
// it is met. It lives outside screens/admin so the reader's chunk, which
// holds the settings screens, does not import from the admin chunk
// (TKT-01M3V6XTYG). The codes are internal/service/identity's (handle.go),
// each with one meaning. They are not form field codes, because a handle is
// no form's field.
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

// handleMessage is what a refused handle says. own is a member changing
// their own handle, who is told same in their own words.
export function handleMessage(code: string, own = false): string {
  if (own && code === 'same') return 'That is already your handle.';
  return Object.hasOwn(handleWords, code) ? handleWords[code] : 'That handle was not accepted. Choose another.';
}
