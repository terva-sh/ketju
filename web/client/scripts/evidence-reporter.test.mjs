// @vitest-environment node
//
// TKT-01M3TNBKNQ: the e2e evidence reporter prints a failing test's
// error-context.md and the serve.log lines written while it ran, prints
// nothing for a test that passed, was skipped or failed as expected, keeps a
// broken run's log bounded, and never throws.

import { appendFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import EvidenceReporter, { maxBlocks, maxContextBytes, maxLogLines, maxLogTailBytes } from './evidence-reporter.mjs';

let dir;
let saved;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'ketju-evidence-'));
  mkdirSync(join(dir, 'run'));
  saved = process.env.KETJU_E2E_DIR;
  process.env.KETJU_E2E_DIR = join(dir, 'run');
});
afterEach(() => {
  if (saved === undefined) delete process.env.KETJU_E2E_DIR;
  else process.env.KETJU_E2E_DIR = saved;
  rmSync(dir, { recursive: true, force: true });
});

const serveLog = () => join(dir, 'run', 'serve.log');
const logLine = (path) => `{"msg":"request","method":"GET","path":"${path}","status":200}\n`;

// started makes a reporter that has begun a run configured as
// playwright.config.ts is, a config file in dir and tests in dir/e2e,
// writing into the array it returns.
function started() {
  const out = [];
  const r = new EvidenceReporter({ write: (s) => out.push(s) });
  r.onBegin({ configFile: join(dir, 'playwright.config.ts'), rootDir: join(dir, 'e2e') });
  return { r, out, text: () => out.join('') };
}

let next = 0;
function testCase({ title = 'a test', project = 'desktop', expectedStatus = 'passed' } = {}) {
  const file = join(dir, 'e2e', 'admin-applications.spec.ts');
  return {
    id: `t${++next}`,
    expectedStatus,
    location: { file, line: 40, column: 1 },
    parent: { project: () => ({ name: project }) },
    titlePath: () => ['', project, 'admin-applications.spec.ts', 'the review screen', title],
  };
}

function context(text = '# Error details\n\nLocator: locator(\'[data-application-link="2"]\')\n') {
  const path = join(dir, `error-context-${++next}.md`);
  writeFileSync(path, text);
  return path;
}

const result = (status, attachments = []) => ({ status, attachments });
const withContext = (path) => [{ name: 'error-context', contentType: 'text/markdown', path }];

describe('the evidence reporter', () => {
  it('a passing test prints nothing', () => {
    const { r, text } = started();
    appendFileSync(serveLog(), logLine('/before'));
    for (const [expectedStatus, status] of [
      ['passed', 'passed'],
      ['skipped', 'skipped'],
      ['failed', 'failed'], // test.fail(): failed as expected
      ['passed', 'skipped'], // did not run, after a serial group's failure
    ]) {
      const t = testCase({ expectedStatus });
      r.onTestBegin(t);
      appendFileSync(serveLog(), logLine('/during'));
      r.onTestEnd(t, result(status, withContext(context())));
    }
    expect(text()).toBe('');
  });

  it('a failing test prints its error context and the server lines it caused', () => {
    const { r, text } = started();
    appendFileSync(serveLog(), logLine('/before-the-test'));
    const t = testCase({ title: 'a moderator approves a pending application' });
    r.onTestBegin(t);
    appendFileSync(serveLog(), logLine('/admin/applications'));
    appendFileSync(serveLog(), logLine('/api/v1/admin/applications'));
    r.onTestEnd(t, result('failed', withContext(context())));

    const got = text();
    expect(got).toContain(
      '── failure evidence: [desktop] › e2e/admin-applications.spec.ts:40 › the review screen › a moderator approves a pending application (failed) ──',
    );
    expect(got).toContain(`Locator: locator('[data-application-link="2"]')`);
    expect(got).toContain('serve.log during this test');
    expect(got).toContain('"path":"/api/v1/admin/applications"');
    expect(got).toContain('"path":"/admin/applications"');
    expect(got).not.toContain('/before-the-test');
    expect(got.trimEnd().endsWith('── end of failure evidence ──')).toBe(true);
  });

  it('a timed-out test prints too', () => {
    const { r, text } = started();
    const t = testCase();
    r.onTestBegin(t);
    r.onTestEnd(t, result('timedOut', withContext(context())));
    expect(text()).toContain('(timedOut) ──');
    expect(text()).toContain('Locator:');
  });

  it(`the server lines are cut to the last ${maxLogLines}`, () => {
    const { r, text } = started();
    const t = testCase();
    r.onTestBegin(t);
    for (let i = 0; i < maxLogLines + 30; i++) appendFileSync(serveLog(), logLine(`/n/${i}`));
    r.onTestEnd(t, result('failed', withContext(context())));
    const got = text();
    const leftOut = Array.from({ length: 30 }, (_, i) => logLine(`/n/${i}`).length).reduce((a, b) => a + b);
    expect(got).toContain(`the last ${maxLogLines} lines:`);
    expect(got).toContain(`… ${leftOut} earlier bytes left out`);
    expect(got).not.toContain('"/n/29"');
    expect(got).toContain('"/n/30"');
    expect(got).toContain(`"/n/${maxLogLines + 29}"`);
  });

  // Review 1636 on PR #261: the slice is read from its last
  // maxLogTailBytes, not whole, so a test that logged a great deal costs no
  // more memory. Lines of 4 KiB put fewer than maxLogLines in that tail.
  it(`a large server log is read only from its last ${maxLogTailBytes} bytes`, () => {
    const { r, text } = started();
    const t = testCase();
    const pad = 'p'.repeat(4096);
    const line = (i) => `{"path":"/big/${i}","pad":"${pad}"}\n`;
    r.onTestBegin(t);
    for (let i = 0; i < 150; i++) appendFileSync(serveLog(), line(i));
    r.onTestEnd(t, result('failed', withContext(context())));
    const got = text();
    const shown = got.split('\n').filter((l) => l.includes('"/big/'));
    expect(shown.length).toBeLessThan(maxLogLines);
    expect(shown.length).toBeGreaterThan(0);
    // Every line shown is whole, though the read began inside one.
    for (const l of shown) expect(l).toMatch(/^\{"path":"\/big\/\d+","pad":"p+"\}$/);
    expect(shown.at(-1)).toContain('"/big/149"');
    const total = Array.from({ length: 150 }, (_, i) => line(i).length).reduce((a, b) => a + b);
    const shownBytes = shown.reduce((n, l) => n + l.length + 1, 0);
    expect(got).toContain(`the last ${shown.length} lines:`);
    expect(got).toContain(`… ${total - shownBytes} earlier bytes left out`);
  });

  // Review 1637 on PR #261: when the tail falls exactly on a line's start,
  // that line is whole and is shown. Lines of exactly 4096 bytes put the
  // tail on line 36's first byte, with 64 whole lines after it.
  it('a line that starts exactly at the tail is kept', () => {
    const { r, text } = started();
    const t = testCase();
    const line = (i) => `${`{"path":"/edge/${String(i).padStart(3, '0')}","pad":"`.padEnd(4093, 'p')}"}\n`;
    expect(line(0)).toHaveLength(4096);
    r.onTestBegin(t);
    for (let i = 0; i < 100; i++) appendFileSync(serveLog(), line(i));
    r.onTestEnd(t, result('failed', withContext(context())));
    const got = text();
    const shown = got.split('\n').filter((l) => l.includes('"/edge/'));
    expect(shown).toHaveLength(maxLogTailBytes / 4096);
    expect(shown[0]).toContain('"/edge/036"');
    expect(got).toContain(`… ${36 * 4096} earlier bytes left out`);
  });

  it(`a large error context is cut at ${maxContextBytes} bytes`, () => {
    const { r, text } = started();
    const t = testCase();
    const path = context('x'.repeat(maxContextBytes + 500));
    r.onTestBegin(t);
    r.onTestEnd(t, result('failed', withContext(path)));
    const got = text();
    expect(got).toContain(`… cut at ${maxContextBytes} bytes, 500 more in ${path}`);
    expect(got.match(/x{1000,}/)?.[0]).toHaveLength(maxContextBytes);
  });

  it(`after ${maxBlocks} failures only a line per test`, () => {
    const { r, out } = started();
    const paths = [];
    for (let i = 0; i < maxBlocks + 2; i++) {
      const t = testCase({ title: `failure ${i}` });
      paths.push(context());
      r.onTestBegin(t);
      appendFileSync(serveLog(), logLine(`/f/${i}`));
      r.onTestEnd(t, result('failed', withContext(paths[i])));
    }
    expect(out).toHaveLength(maxBlocks + 2);
    expect(out.filter((s) => s.includes('── end of failure evidence ──'))).toHaveLength(maxBlocks);
    expect(out[maxBlocks]).toBe(
      `failure evidence: [desktop] › e2e/admin-applications.spec.ts:40 › the review screen › failure ${maxBlocks} (failed): error context ${paths[maxBlocks]}\n`,
    );
    expect(out[maxBlocks + 1]).not.toContain('serve.log');
  });

  // Review 1636 on PR #261: a block that could not be built is not one of
  // the ten, so the evidence of a later failure still prints in full.
  it('a failure that could not be printed does not count toward the cap', () => {
    const { r, out } = started();
    for (let i = 0; i < maxBlocks; i++) {
      const t = testCase({ title: `unreadable ${i}` });
      r.onTestBegin(t);
      r.onTestEnd(t, result('failed', withContext(dir)));
    }
    const t = testCase({ title: 'readable' });
    r.onTestBegin(t);
    r.onTestEnd(t, result('failed', withContext(context())));
    expect(out.slice(0, maxBlocks).every((s) => s.startsWith('failure evidence: could not print'))).toBe(true);
    expect(out[maxBlocks]).toContain('── failure evidence: [desktop] › e2e/admin-applications.spec.ts:40 › the review screen › readable (failed) ──');
    expect(out[maxBlocks]).toContain('── end of failure evidence ──');
  });

  it('a missing error context or serve.log is said, not thrown', () => {
    const { r, text } = started();
    const t = testCase();
    r.onTestBegin(t);
    r.onTestEnd(t, result('failed'));
    expect(text()).toContain('error-context.md: Playwright wrote none for this test');
    expect(text()).toContain('serve.log: not found');
  });

  it('an error inside the reporter writes one line and does not throw', () => {
    const { r, out } = started();
    const t = testCase();
    r.onTestBegin(t);
    expect(() => r.onTestEnd(t, result('failed', withContext(dir)))).not.toThrow();
    expect(out).toHaveLength(1);
    expect(out[0]).toMatch(/^failure evidence: could not print \(.+\)\n$/);
  });
});
