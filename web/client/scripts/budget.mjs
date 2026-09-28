// `npm run budget`: the thread view's bundle budget (TKT-01M29CGBYH).
//
// It measures the build in internal/web/spa/dist (bundle.mjs says what
// counts), prints one line per file and a total on every run, and exits 1
// when the total is over budget. The budget is read from ../budget.json and
// from nowhere else: no flag and no environment variable changes it, so
// raising it is a reviewed edit to a committed number. bundle.test.mjs holds
// both files to that.

import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { check, readBudget } from './bundle.mjs';

const here = dirname(fileURLToPath(import.meta.url));
const { ok, lines } = check(join(here, '..', '..', '..', 'internal', 'web', 'spa', 'dist'), readBudget());
for (const line of lines) console.log(line);
if (!ok) process.exitCode = 1;
