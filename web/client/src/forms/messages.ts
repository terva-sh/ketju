import type { FormField } from '../api/dto';
import { problemMessage, ProblemError } from '../api/problem';

// The words for each field error code (docs/07 "Field errors on
// validation-failed"). The codes are API surface, like a problem's type, and
// messages.test.ts holds this table to the document's list in both
// directions. The schema codes are here too, for the form editor
// (TKT-01M29CP2P1), which reads the same errors.
const words: Record<string, (f?: FormField) => string> = {
  required: () => 'This is required.',
  too_long: (f) => (f && f.max > 0 ? `Keep this to ${f.max} characters or fewer.` : 'This is longer than the limit.'),
  invalid_email: () => 'Enter an email address, such as name@example.com.',
  invalid_text: () => 'This holds a character that is not allowed here, such as a line break.',
  not_an_option: () => 'Choose one of the options.',
  duplicate_option: () => 'Each option can be chosen only once.',
  wrong_type: () => 'This answer is not the kind the field takes.',
  not_allowed: () => 'This does not take an answer.',
  invalid_key: () => 'A key starts with a lowercase letter and holds only lowercase letters, digits and underscores.',
  duplicate_key: () => 'Another field already uses this key.',
  unknown_type: () => 'This is not a field type the site knows.',
  unknown_claim: () => 'Prefill can come only from name, email or preferred_username.',
  out_of_range: () => 'The limit is outside what this field type allows.',
  too_many: () => 'There are more of these than the form allows.',
  unknown_property: () => 'This holds a property no form field has.',
  malformed: () => 'This could not be read.',
};

// knownCodes is exported for the test that holds the table to docs/07.
export const knownCodes = Object.keys(words);

// fieldMessage is what a reader is told about one field's code. A code this
// client does not know reads as the refusal as a whole does, rather than as
// nothing.
export function fieldMessage(code: string, field?: FormField): string {
  const w = Object.hasOwn(words, code) ? words[code] : undefined;
  return w ? w(field) : problemMessage(new ProblemError('validation-failed', 422)).text;
}
