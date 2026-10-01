// A Playwright reporter that prints a failing test's evidence into the run's
// output (TKT-01M3TNBKNQ). CI keeps nothing else: the runner's container, and
// with it every trace.zip and error-context.md under the run's results/
// directory, is gone when the job ends, and the forge has no artifact route.
// So for each test that failed, timed out or was interrupted, this prints:
//
//   - Playwright's error-context.md: the error with the failing locator and
//     its call log, the page's ARIA tree when it failed, and the source frame;
//   - the lines ketju e2e wrote to serve.log while that test ran, which name
//     every request the page made, its status and how long it took.
//
// A test that passed, was skipped, or failed as expected prints nothing, so a
// green run's log is the list reporter's alone. playwright.config.ts runs this
// after list, so a failure's ✘ line comes first and its evidence under it.
//
// It runs inside the Playwright runner, so nothing here may throw: an error
// is written as one line and the run goes on with its outcome unchanged.

import { closeSync, openSync, readSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';

// The bounds keep a broken run's log readable. An error context is a few KB
// (the whole page's ARIA tree); a test's own server lines are tens.
export const maxContextBytes = 64 * 1024;
export const maxLogLines = 120;
export const maxBlocks = 10;

export default class EvidenceReporter {
  constructor(options = {}) {
    this.write = options.write ?? ((s) => process.stdout.write(s));
    this.log = '';
    this.root = process.cwd();
    this.offsets = new Map();
    this.printed = 0;
  }

  onBegin(config) {
    // playwright.config.ts sets KETJU_E2E_DIR in this process before any
    // reporter is made, and ketju e2e writes serve.log into it (cmd/ketju
    // e2e.go).
    if (process.env.KETJU_E2E_DIR) this.log = join(process.env.KETJU_E2E_DIR, 'serve.log');
    // Files are named from the config's directory, as list names them;
    // rootDir is the test directory, one level further in.
    if (config?.configFile) this.root = dirname(config.configFile);
    else if (config?.rootDir) this.root = config.rootDir;
  }

  onTestBegin(test) {
    this.offsets.set(test.id, size(this.log));
  }

  onTestEnd(test, result) {
    const from = this.offsets.get(test.id) ?? 0;
    this.offsets.delete(test.id);
    // A skipped result whose expected status is not skipped is a test that
    // did not run, after a serial group's failure for example. The failure
    // that stopped it prints its own evidence.
    if (result.status === test.expectedStatus || result.status === 'skipped') return;
    try {
      const context = result.attachments.find((a) => a.name === 'error-context' && a.path)?.path;
      if (++this.printed > maxBlocks) {
        this.write(`failure evidence: ${this.name(test, result)}: error context ${context ?? 'not written'}\n`);
        return;
      }
      this.write(this.block(test, result, context, from));
    } catch (e) {
      this.write(`failure evidence: could not print (${e instanceof Error ? e.message : String(e)})\n`);
    }
  }

  name(test, result) {
    const project = test.parent?.project()?.name ?? '';
    const where = `${relative(this.root, test.location.file)}:${test.location.line}`;
    // titlePath is the root suite, the project, the file, then the describe
    // blocks and the test's own title.
    return `[${project}] › ${where} › ${test.titlePath().slice(3).join(' › ')} (${result.status})`;
  }

  block(test, result, context, from) {
    const out = [`\n── failure evidence: ${this.name(test, result)} ──\n`];
    if (context) {
      out.push(`error-context.md (${context}):\n`);
      const { text, left } = readRange(context, 0, maxContextBytes);
      out.push(text.endsWith('\n') ? text : `${text}\n`);
      if (left > 0) out.push(`… cut at ${maxContextBytes} bytes, ${left} more in ${context}\n`);
    } else {
      out.push('error-context.md: Playwright wrote none for this test\n');
    }
    if (!this.log || !exists(this.log)) {
      out.push('serve.log: not found\n');
    } else {
      const end = size(this.log);
      const start = from <= end ? from : 0;
      const lines = readRange(this.log, start, end - start).text.split('\n').filter((l) => l !== '');
      const kept = lines.slice(-maxLogLines);
      out.push(`serve.log during this test (${this.log}), ${lines.length} lines:\n`);
      if (lines.length > kept.length) out.push(`… ${lines.length - kept.length} earlier lines left out\n`);
      for (const l of kept) out.push(`${l}\n`);
    }
    out.push('── end of failure evidence ──\n');
    return out.join('');
  }
}

function exists(path) {
  try {
    statSync(path);
    return true;
  } catch {
    return false;
  }
}

function size(path) {
  if (!path) return 0;
  try {
    return statSync(path).size;
  } catch {
    return 0;
  }
}

// readRange reads up to n bytes of path from start, and says how many bytes
// past them the file still holds.
function readRange(path, start, n) {
  const fd = openSync(path, 'r');
  try {
    const total = statSync(path).size;
    const want = Math.max(0, Math.min(n, total - start));
    const buf = Buffer.alloc(want);
    const got = want > 0 ? readSync(fd, buf, 0, want, start) : 0;
    return { text: buf.subarray(0, got).toString('utf8'), left: Math.max(0, total - start - got) };
  } finally {
    closeSync(fd);
  }
}
