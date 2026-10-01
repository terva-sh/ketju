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
import EvidenceReporter, { maxBlocks, maxContextBytes, maxLogLines } from './evidence-reporter.mjs';

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
    expect(got).toContain(`${maxLogLines + 30} lines:`);
    expect(got).toContain('… 30 earlier lines left out');
    expect(got).not.toContain('"/n/29"');
    expect(got).toContain('"/n/30"');
    expect(got).toContain(`"/n/${maxLogLines + 29}"`);
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
