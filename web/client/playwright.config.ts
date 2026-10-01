import { defineConfig } from '@playwright/test';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';

// The Playwright smoke suite (TKT-01M29CGBY1): sign in, read a thread,
// reply, and see the unread divider, at a 390 px phone and a 1440 px desktop.
// A failure at either width fails the run, and CI runs it inside the
// required Build and Test job.
//
// The server is the real ketju serve, built with the e2e tag (cmd/ketju
// e2e.go): a fresh data directory, a seeded forum, and the fake OpenID
// provider on loopback, so the run needs no network beyond this machine.
//
// This file is read by the runner and again by each worker. The run
// directory is made once, by the runner, and handed to the workers through
// the environment, which they inherit.
const port = Number(process.env.KETJU_E2E_PORT ?? 8787);
const run = (process.env.KETJU_E2E_DIR ??= join(mkdtempSync(join(tmpdir(), 'ketju-e2e-')), 'run'));
const bin = join(dirname(run), 'ketju-e2e');
const baseURL = `http://localhost:${port}`;
// sh quotes a word for the shell webServer.command runs in: a path from the
// environment may hold spaces or anything else (review on PR #82).
const sh = (s: string) => `'${s.replaceAll("'", `'\\''`)}'`;

export default defineConfig({
  testDir: 'e2e',
  // One worker and no retries: both projects share one server, and a flake
  // must fail the run, not be retried out of sight.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: !!process.env.CI,
  // list, then the evidence a failure leaves (scripts/evidence-reporter.mjs,
  // TKT-01M3TNBKNQ): a failing test's error-context.md and the serve.log
  // lines it caused, printed into the output, because CI's runner keeps
  // nothing else once the job ends. A passing run prints nothing more.
  reporter: [['list'], ['./scripts/evidence-reporter.mjs']],
  outputDir: join(dirname(run), 'results'),
  use: {
    baseURL,
    // The fake provider's certificate is its own, made when it starts.
    ignoreHTTPSErrors: true,
    trace: 'retain-on-failure',
    // CI's Alpine image runs its own Chromium, since Playwright's builds
    // need glibc; elsewhere the one Playwright installed.
    launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE }
      : {},
  },
  // setup signs the seeded readers in once (e2e/session.setup.ts); both
  // widths depend on it.
  projects: [
    { name: 'setup', testMatch: /session\.setup\.ts$/ },
    {
      name: 'phone',
      use: { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true },
      dependencies: ['setup'],
    },
    { name: 'desktop', use: { viewport: { width: 1440, height: 900 } }, dependencies: ['setup'] },
  ],
  webServer: {
    command: `go build -tags e2e -o ${sh(bin)} ../../cmd/ketju && ${sh(bin)} e2e -dir ${sh(run)} -port ${port}`,
    url: `${baseURL}/api/v1/me`,
    timeout: 180_000,
    reuseExistingServer: false,
    stdout: 'pipe',
    stderr: 'pipe',
  },
});
