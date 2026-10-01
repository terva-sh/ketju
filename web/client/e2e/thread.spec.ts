import { expect, test, type CDPSession, type Locator, type Page } from '@playwright/test';
import { mkdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setReadPosition, stateFile } from './session';

// The thread page against its mockups, and the safe-area padding of the
// phone's bars (TKT-01M39WBN4Q), run once per project in playwright.config.ts.
//
// With KETJU_SCREENSHOTS=1 the first test also writes the screenshots
// committed beside it, e2e/screenshots/thread-{phone,desktop}-{light,dark}.png,
// which the pull request compares by eye against docs/mockups/
// Thread{Light,Dark}.dc.html and MobileThread{Light,Dark}.dc.html. CI runs the
// assertions and writes nothing. They are evidence for review, not a pixel
// gate: the seeded times are relative to when the seed ran, and fonts render
// differently on the dev host and in CI's Chromium.

test.use({ storageState: stateFile('alice') });

const seed: { thread: string } = JSON.parse(readFileSync(join(process.env.KETJU_E2E_DIR!, 'seed.json'), 'utf8'));

// Page 3 of the seeded thread, as the mockups draw page 3 of theirs: the
// last page, so the composer is at its foot, and short enough that a capture
// stays small. Alice has read to lastRead on it, so the page draws the amber
// divider before the next post and the unread edge on every post after it.
// Other specs (flag, replies, shortcuts, a11y) open the thread as alice and
// move her position, so it is set rather than trusted.
const onPage = 3;
const lastRead = 53;

type Box = { x: number; y: number; width: number; height: number };

test('the thread page renders the mockup’s parts', async ({ page, isMobile }, info) => {
  await setReadPosition(page.request, seed.thread, lastRead);
  await page.goto(`${seed.thread}?page=${onPage}`);
  await expect(page.locator('#post-51')).toBeVisible();

  // The thread head (docs/06 "The thread head"): the breadcrumb above the
  // title, and under it who started the thread, when, and how many posts it
  // has. carol writes the seed's ThreadCreated. The count is not named,
  // since other specs add posts to the thread.
  const crumbs = page.locator('.thread-head nav.breadcrumb a');
  await expect(crumbs).toHaveText(['Forums', 'General']);
  expect(await crumbs.evaluateAll((as) => as.map((a) => a.getAttribute('href')))).toEqual(['/', '/f/general']);
  const meta = page.locator('.thread-head h1 + .thread-meta');
  await expect(meta).toHaveText(/^started by carol · .+ · \d+ posts$/);
  await expect(meta.locator('time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);

  // The header: ordinal, name and time. A member's post carries no role
  // badge (RoleBadge).
  const first = page.locator('#post-51 .post-header');
  await expect(first.locator('.ordinal')).toHaveText('#51');
  await expect(first.locator('.author')).not.toBeEmpty();
  await expect(first.locator('time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);
  await expect(page.locator('#post-51 .role-badge')).toHaveCount(0);
  // #55 answers #1 (seedReplies), two pages back, so it carries the reply-to
  // indicator.
  await expect(page.locator('#post-55 .post-header .reply-to')).toHaveText('↩ #1');

  // The parts the seed puts on this page (cmd/ketju e2e.go seedAuthors,
  // seedEdited and seedRemoved): #56 is the moderator's, so it carries her
  // role badge; #52 was edited by its author past the grace window, so it
  // carries the edited mark; and #57 was deleted by its author, so alice
  // sees a tombstone row in its place, keeping its ordinal.
  await expect(page.locator('#post-56 .role-badge')).toHaveText('moderator');
  await expect(page.locator('#post-52 .edited')).toHaveText('(edited)');
  const removed = page.locator('#post-57');
  await expect(removed).toHaveClass(/\btombstone\b/);
  await expect(removed.locator('.tombstone-line .ordinal')).toHaveText('#57');
  await expect(removed.locator('.tombstone-text')).toHaveText('Post removed by author');

  // The header and footer at each width (TKT-01M3S3Z9E1; docs/06 "Mobile"
  // and "Post anatomy"). On the phone the header stacks: author and badge on
  // the first line, then ordinal and time, with the reply-to at the right
  // end of that second line. The author's name is the first line's link and
  // the ordinal is text, as the phone mockup draws them (TKT-01M3V3CFMA), so
  // Tab goes from the author down to the reply-to, and never back up. The
  // body takes the full width below the avatar,
  // and the footer reads reply before quote, with More last. On the desktop
  // the header is one line and the footer reads quote before reply.
  const mod = page.locator('#post-56');
  const box = async (l: Locator) => (await l.boundingBox())!;
  const sameLine = (a: Box, b: Box) => a.y < b.y + b.height && b.y < a.y + a.height;
  const author = await box(mod.locator('.author'));
  const ordinal = await box(mod.locator('.ordinal'));
  const actions = mod.locator('.post-footer [data-post-action=reply], .post-footer [data-post-action=quote]');
  const [firstAction, secondAction] = [await box(actions.nth(0)), await box(actions.nth(1))];
  expect(firstAction.x).toBeLessThan(secondAction.x);
  if (isMobile) {
    expect(author.y + author.height / 2).toBeLessThan(ordinal.y);
    expect(sameLine(await box(mod.locator('.role-badge')), author)).toBe(true);
    expect(sameLine(await box(mod.locator('.post-header time')), ordinal)).toBe(true);
    // The body starts at the post's left padding, not beside the avatar.
    const inset = await mod.evaluate((el) => {
      const s = getComputedStyle(el);
      const body = el.querySelector('.post-body')!.getBoundingClientRect();
      return body.left - el.getBoundingClientRect().left - parseFloat(s.borderLeftWidth) - parseFloat(s.paddingLeft);
    });
    expect(Math.abs(inset)).toBeLessThan(1);
    await expect(actions).toHaveText(['reply', 'quote']);
    const footer = mod.locator('.post-footer > *');
    await expect(footer.last()).toHaveClass(/\bpost-more\b/);
    expect((await box(footer.last())).x).toBeGreaterThan(secondAction.x);

    const answer = page.locator('#post-55');
    const o = answer.locator('.ordinal');
    const rt = answer.locator('.reply-to');
    const [ob, rb] = [await box(o), await box(rt)];
    expect(sameLine(rb, ob)).toBe(true);
    expect(rb.x).toBeGreaterThan(ob.x + ob.width);
    expect(await o.evaluate((el) => el.localName)).toBe('span');
    await answer.locator('.author').focus();
    await page.keyboard.press('Tab');
    await expect(rt).toBeFocused();
    // So the screenshots below do not draw its focus ring.
    await rt.blur();
  } else {
    expect(sameLine(author, ordinal)).toBe(true);
    await expect(actions).toHaveText(['quote', 'reply']);
  }

  // The divider sits between the last read post and the first unread, and
  // only the posts after it carry the unread edge.
  const divider = page.locator('.unread-divider');
  await expect(divider).toHaveCount(1);
  // It ends with when alice set her place (docs/06 "Landing on return").
  await expect(divider.locator('.last-read')).toHaveText(new RegExp(`^last read #${lastRead} · \\S`));
  await expect(divider.locator('.last-read time')).toHaveAttribute('datetime', /^\d{4}-\d\d-\d\dT/);
  await expect(page.locator(`#post-${lastRead} + .unread-divider + #post-${lastRead + 1}`)).toHaveCount(1);
  await expect(page.locator(`#post-${lastRead}`)).not.toHaveClass(/\bunread\b/);
  await expect(page.locator(`#post-${lastRead + 1}`)).toHaveClass(/\bunread\b/);

  // The pager: numbered on the desktop, "Page N of M" on the phone. M is not
  // named, since other specs add posts to the thread. The phone replies from
  // its reply bar, the desktop from the composer at the foot.
  if (isMobile) {
    await expect(page.locator('.pager-narrow .where').first()).toHaveText(new RegExp(`^Page ${onPage} of \\d+$`));
    await expect(page.locator('.pager-wide').first()).toBeHidden();
    await expect(page.locator('[data-shell="replybar"]')).toBeVisible();
  } else {
    await expect(page.locator('.pager-wide .page.current').first()).toHaveText(String(onPage));
    await expect(page.locator('.pager-narrow').first()).toBeHidden();
    await expect(page.locator('.composer')).toBeVisible();
  }

  if (process.env.KETJU_SCREENSHOTS === '1') await capture(page, `thread-${info.project.name}`);
});

// capture writes name-{light,dark}.png of the page at its full height: a
// taller viewport rather than a full-page capture, so the bars that stick to
// the foot sit where a reader sees them, as forum-index.spec.ts does. It
// waits for the fonts, stops animations and the caret, and masks every time
// on the page: each post's, the thread's, the draft note's and the unread
// divider's, the only things on the page that change between runs.
async function capture(page: Page, name: string) {
  const dir = join(dirname(fileURLToPath(import.meta.url)), 'screenshots');
  mkdirSync(dir, { recursive: true });
  await page.evaluate(() => document.fonts.ready.then(() => undefined));
  const vp = page.viewportSize()!;
  const height = await page.evaluate(() => document.documentElement.scrollHeight);
  await page.setViewportSize({ width: vp.width, height: Math.max(vp.height, height) });
  await page.evaluate(() => window.scrollTo(0, 0));
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    await page.screenshot({
      path: join(dir, `${name}-${colorScheme}.png`),
      animations: 'disabled',
      caret: 'hide',
      mask: [page.locator('.post-header time, .thread-meta time, .draft-note time, .unread-divider time')],
      maskColor: '#8B95A8',
    });
  }
  await page.emulateMedia({ colorScheme: null });
  await page.setViewportSize(vp);
}

// The safe-area check (TKT-01M29CCC0F AC3). index.html sets
// viewport-fit=cover, and the tab bar, the reply bar and the phone's top bar
// pad themselves clear of a phone's notch and home indicator with
// env(safe-area-inset-*) (styles/base.css). The Vitest test
// (src/styles/breakpoints.test.ts) reads that file as text; this one proves
// the cascade, since shell.css and composer.css restyle the same bars.
//
// Playwright has no device that sets an inset, so it is emulated through
// the DevTools protocol: Emulation.setSafeAreaInsetsOverride, then the
// computed padding. A Chromium without that method gets
// CSS.getMatchedStylesForNode instead, asserting that the declaration that
// wins for each property is the env() one. The test says which ran, on its
// own line of the run's output and as an annotation, and
// KETJU_SAFE_AREA_METHOD=matched forces the second so it can be run on a
// Chromium that has both.
const inset = { top: 47, bottom: 34, left: 0, right: 0 };

// Each bar, the page it is checked on, and the side it pads. The top bar's
// padding-top is base.css's .topbar rule: .topbar.phone (shell/shell.css)
// sets only its left and right padding.
const bars: { name: string; path: string; selector: string; side: 'top' | 'bottom' }[] = [
  { name: 'tab bar', path: '/f/general', selector: '[data-shell="tabbar"]', side: 'bottom' },
  { name: 'top bar', path: '/f/general', selector: '[data-shell="phone-topbar"]', side: 'top' },
  { name: 'reply bar', path: `${seed.thread}?page=1`, selector: '[data-shell="replybar"]', side: 'bottom' },
];

test('the bars pad by the safe-area insets', async ({ page, isMobile }) => {
  test.skip(!isMobile, 'the bars are the phone’s');
  const cdp = await page.context().newCDPSession(page);
  const method = process.env.KETJU_SAFE_AREA_METHOD === 'matched' ? 'matched' : await overrideInsets(cdp, inset);
  const ran = method === 'override' ? 'Emulation.setSafeAreaInsetsOverride' : 'CSS.getMatchedStylesForNode';
  test.info().annotations.push({ type: 'safe-area method', description: ran });
  console.log(`safe-area check: ${ran}`);

  for (const path of [...new Set(bars.map((b) => b.path))]) {
    await page.goto(path);
    for (const bar of bars.filter((b) => b.path === path)) {
      const el = page.locator(bar.selector);
      await expect(el, bar.name).toBeVisible();
      if (method === 'override') {
        await expect(el, `${bar.name} with the inset`).toHaveCSS(`padding-${bar.side}`, `${inset[bar.side]}px`);
      } else {
        // The env() with its no-inset fallback, 0px; the built CSS is
        // minified, so the space after the comma may be gone.
        expect(await winning(cdp, el, `padding-${bar.side}`), bar.name).toMatch(
          new RegExp(`^env\\(safe-area-inset-${bar.side}, ?0px\\)$`),
        );
      }
    }
    if (method === 'override') {
      // With the override emptied there is no inset, and each bar falls back
      // to 0px: the top bar's max(8px, …) is its left and right padding only.
      await overrideInsets(cdp, {});
      for (const bar of bars.filter((b) => b.path === path)) {
        await expect(page.locator(bar.selector), `${bar.name} with no inset`).toHaveCSS(`padding-${bar.side}`, '0px');
      }
      await overrideInsets(cdp, inset);
    }
  }
});

// overrideInsets sets the emulated insets and says whether it could: a
// Chromium that lacks the method answers that it was not found.
async function overrideInsets(cdp: CDPSession, insets: Partial<typeof inset>): Promise<'override' | 'matched'> {
  try {
    await cdp.send('Emulation.setSafeAreaInsetsOverride', { insets });
    return 'override';
  } catch (err) {
    if (/wasn'?t found|not found/i.test(String(err))) return 'matched';
    throw err;
  }
}

// winning is the value of the declaration that wins property for the element:
// the inline style, else the last matched author rule that sets it, since
// CSS.getMatchedStylesForNode lists matched rules in cascade order. The
// padding shorthand sets it too (review 1581). Chromium lists a shorthand's
// longhands beside it, so a later `padding: 5px` already reads as 5px, but
// the shorthand is counted as well rather than trusted to that. None of
// these bars sets the property !important, which the check refuses rather
// than rank.
async function winning(cdp: CDPSession, el: Locator, property: string): Promise<string | undefined> {
  const marker = `safe-area-${Math.random().toString(36).slice(2)}`;
  await el.evaluate((node, m) => node.setAttribute('data-safe-area-probe', m), marker);
  try {
    await cdp.send('DOM.enable');
    await cdp.send('CSS.enable');
    const { root } = await cdp.send('DOM.getDocument', { depth: 0 });
    const selector = `[data-safe-area-probe="${marker}"]`;
    const { nodeId } = await cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector });
    expect(nodeId, 'the bar is in the document').toBeGreaterThan(0);
    const styles = await cdp.send('CSS.getMatchedStylesForNode', { nodeId });
    const sets = (props: { name: string; value: string; disabled?: boolean; parsedOk?: boolean; important?: boolean }[]) =>
      props.filter((p) => (p.name === property || p.name === 'padding') && !p.disabled && p.parsedOk !== false);
    const inline = sets(styles.inlineStyle?.cssProperties ?? []);
    if (inline.length > 0) return inline[inline.length - 1].value;
    const rules = (styles.matchedCSSRules ?? []).filter((m) => m.rule.origin !== 'user-agent');
    let value: string | undefined;
    for (const m of rules) {
      const decl = sets(m.rule.style.cssProperties);
      if (decl.length === 0) continue;
      expect(decl.some((d) => d.important), `${property} is not set !important`).toBe(false);
      value = decl[decl.length - 1].value;
    }
    return value;
  } finally {
    await el.evaluate((node) => node.removeAttribute('data-safe-area-probe'));
  }
}
