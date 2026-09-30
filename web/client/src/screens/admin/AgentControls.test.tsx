import { act } from 'preact/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { AdminUserDetail, AdminUserList, CreatedAgent, Me } from '../../api/dto';
import { viewer } from '../../state/viewer';
import { renderAt, settle, unmount } from '../../testing';
import moderatorMe from '../../../../../internal/dto/testdata/golden/me.moderator.json';
import userList from '../../../../../internal/dto/testdata/golden/admin-users.admin.json';
import userAdmin from '../../../../../internal/dto/testdata/golden/admin-user.admin.json';
import agentDetail from '../../../../../internal/dto/testdata/golden/admin-user-agent.admin.json';
import createdGolden from '../../../../../internal/dto/testdata/golden/created-agent.admin.json';
import createdTokenless from '../../../../../internal/dto/testdata/golden/created-agent-tokenless.admin.json';
import issuedGolden from '../../../../../internal/dto/testdata/golden/issued-credential.admin.json';

// The agent controls on /admin/users (TKT-01M3QH3GS2): creating an agent,
// its one-time token, issuing and revoking, and its write limits, against a
// stubbed server that answers with the goldens.
//
// The token is the one secret the client ever holds. These tests look for
// it everywhere the page could keep it: the markup, every input's value,
// both web storages and the address.

const asModerator = () => structuredClone(moderatorMe) as Me;
const asAdmin = () => {
  const me = asModerator();
  me.user = { ...me.user!, role: 'admin' };
  return me;
};

type Answer = { status: number; body?: unknown };
let routes: Record<string, (body: unknown) => Answer | Promise<Answer>>;
let sent: { method: string; url: string; body: unknown }[];

function serve() {
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const body = init.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, url, body });
    const handler = routes[`${method} ${url}`];
    if (!handler) return new Response('{}', { status: 404, headers: { 'Content-Type': 'application/problem+json' } });
    const a = await handler(body);
    const problem = a.status >= 400;
    return new Response(a.body === undefined ? null : JSON.stringify(a.body), {
      status: a.status,
      headers: { 'Content-Type': problem ? 'application/problem+json' : 'application/json' },
    });
  });
}

const refusal = (type: string, status: number) => ({ status, body: { type, status, title: type } });

const created = () => structuredClone(createdGolden) as CreatedAgent;
const token = createdGolden.credential.token;
const issuedToken = 'ketju_agent_second-token-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB';

// withIndexer is the list once the new agent is in it.
function withIndexer(): AdminUserList {
  const l = structuredClone(userList) as AdminUserList;
  l.users.push(created().user.user);
  return l;
}

let saved: Me;
beforeEach(() => {
  saved = viewer.value;
  viewer.value = asAdmin();
  sent = [];
  localStorage.clear();
  sessionStorage.clear();
  routes = {
    'GET /api/v1/admin/users': () => ({ status: 200, body: userList }),
    'GET /api/v1/admin/users/alice': () => ({ status: 200, body: userAdmin }),
    'GET /api/v1/admin/users/scribe': () => ({ status: 200, body: agentDetail }),
    'GET /api/v1/admin/users/indexer': () => ({ status: 200, body: created().user }),
  };
  serve();
});
afterEach(async () => {
  await unmount();
  viewer.value = saved;
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Reflect.deleteProperty(navigator, 'clipboard');
});

const buttons = (el: Element) => [...el.querySelectorAll('button')].map((b) => b.textContent);
const button = (el: Element, text: string) => {
  const b = [...el.querySelectorAll('button')].find((x) => x.textContent === text);
  if (!b) throw new Error(`no button "${text}" in: ${buttons(el).join(', ')}`);
  return b;
};
const press = async (root: HTMLElement, el: Element, text: string) => {
  await act(async () => button(el, text).click());
  await settle(root);
};

// anywhere is every place in the page a token could be read back from.
function anywhere(): string {
  const values = [...document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>('input, textarea')].map((i) => i.value);
  const stored = [localStorage, sessionStorage].flatMap((s) =>
    Array.from({ length: s.length }, (_, i) => `${s.key(i)}=${s.getItem(s.key(i)!)}`),
  );
  return [document.body.innerHTML, ...values, ...stored, location.href].join('\n');
}

async function open(root: HTMLElement, handle: string) {
  await act(async () => root.querySelector<HTMLButtonElement>(`[data-user="${handle}"]`)!.click());
  await settle(root);
}

async function type(input: HTMLInputElement, value: string) {
  await act(async () => {
    input.value = value;
    input.dispatchEvent(new Event('input'));
  });
}

async function submit(root: HTMLElement, form: Element) {
  await act(async () => {
    form.dispatchEvent(new Event('submit', { cancelable: true }));
  });
  await settle(root);
}

// create opens the form, fills it in and submits it.
async function create(root: HTMLElement, { issue = true, handle = 'indexer', name = 'Indexer' } = {}) {
  await press(root, root, 'New agent');
  const form = root.querySelector<HTMLFormElement>('form[data-new-agent]')!;
  const [h, n] = [...form.querySelectorAll<HTMLInputElement>('input[type="text"]')];
  await type(h, handle);
  await type(n, name);
  const box = form.querySelector<HTMLInputElement>('input[type="checkbox"]')!;
  if (box.checked !== issue) {
    await act(async () => box.click());
  }
  await submit(root, form);
}

const tokenInput = (root: HTMLElement) => root.querySelector<HTMLInputElement>('[data-token-panel] input');

describe('creating an agent', () => {
  it('creates an agent with a token, shows it once in the new agent’s detail, and selects it', async () => {
    routes['POST /api/v1/admin/users'] = () => ({ status: 201, body: created() });
    const root = await renderAt('/admin/users');
    routes['GET /api/v1/admin/users'] = () => ({ status: 200, body: withIndexer() });
    await create(root);

    expect(sent.find((s) => s.method === 'POST')?.body).toStrictEqual({
      kind: 'agent',
      handle: 'indexer',
      display_name: 'Indexer',
      issue_credential: true,
    });
    const detail = root.querySelector('[data-detail="indexer"]')!;
    expect(detail).not.toBeNull();
    const panel = detail.querySelector('[data-token-panel]')!;
    expect(panel.getAttribute('role')).toBe('status');
    expect(panel.textContent).toContain('This token is shown once. Store it now.');
    expect(tokenInput(root)!.value).toBe(token);
    expect(tokenInput(root)!.readOnly).toBe(true);
    expect(document.activeElement).toBe(tokenInput(root));
    expect(buttons(panel)).toEqual(['Copy', 'Done']);
    // The list was asked for again, and now has the new agent.
    await settle(root);
    expect(sent.filter((s) => s.method === 'GET' && s.url === '/api/v1/admin/users')).toHaveLength(2);
    expect(root.querySelector('[data-user="indexer"]')).not.toBeNull();
  });

  it('holds no token in the form', async () => {
    routes['POST /api/v1/admin/users'] = () => ({ status: 201, body: created() });
    const root = await renderAt('/admin/users');
    await create(root);
    const form = root.querySelector('[data-new-agent]')!;
    const inForm = [form.innerHTML, ...[...form.querySelectorAll('input')].map((i) => i.value)].join();
    expect(inForm).not.toContain(token);
    expect(root.querySelector('[data-detail] [data-token-panel]')).not.toBeNull();
    expect(root.querySelector('.admin-split > div:first-child [data-token-panel]')).toBeNull();
  });

  it('creates an agent without a token', async () => {
    routes['POST /api/v1/admin/users'] = () => ({ status: 201, body: structuredClone(createdTokenless) });
    const root = await renderAt('/admin/users');
    await create(root, { issue: false });
    expect(sent.find((s) => s.method === 'POST')?.body).toStrictEqual({
      kind: 'agent',
      handle: 'indexer',
      display_name: 'Indexer',
      issue_credential: false,
    });
    expect(root.querySelector('[data-token-panel]')).toBeNull();
    const detail = root.querySelector('[data-detail="indexer"]')!;
    expect(detail.querySelector('[data-no-credential]')).not.toBeNull();
    expect(buttons(detail)).toContain('Issue token');
  });

  it('says a taken handle is taken, and a refused one is refused', async () => {
    routes['POST /api/v1/admin/users'] = () => refusal('conflict', 409);
    const root = await renderAt('/admin/users');
    await create(root, { handle: 'scribe' });
    const alert = () => root.querySelector('[data-new-agent] [role="alert"]')!.textContent;
    expect(alert()).toBe('That handle is taken.');
    expect(root.textContent).not.toContain('Someone else changed this');
    expect(root.querySelector('[data-detail]')).toBeNull();

    routes['POST /api/v1/admin/users'] = () => refusal('validation-failed', 422);
    await submit(root, root.querySelector('form[data-new-agent]')!);
    expect(alert()).toBe('That is not a valid handle, or it is reserved.');
  });

  it('is not offered to a moderator', async () => {
    viewer.value = asModerator();
    const root = await renderAt('/admin/users');
    expect(buttons(root)).not.toContain('New agent');
    expect(root.querySelector('[data-new-agent]')).toBeNull();
  });
});

describe('the one-time token', () => {
  async function createdWithToken() {
    routes['POST /api/v1/admin/users'] = () => ({ status: 201, body: created() });
    routes['GET /api/v1/admin/users'] = () => ({ status: 200, body: withIndexer() });
    const root = await renderAt('/admin/users');
    await create(root);
    expect(anywhere()).toContain(token);
    return root;
  }

  it('is gone after Done: from the page, web storage and the address', async () => {
    const root = await createdWithToken();
    await press(root, root.querySelector('[data-token-panel]')!, 'Done');
    expect(root.querySelector('[data-token-panel]')).toBeNull();
    expect(anywhere()).not.toContain(token);
    expect(localStorage.length + sessionStorage.length).toBe(0);
    expect(location.href).not.toContain(token);
    // Focus is left on the block the panel was in, not on nothing.
    expect(document.activeElement?.textContent).toBe('Token');
  });

  // Terva review 1488, finding 1: the creation token goes with the detail.
  it('is gone when the detail is closed, and does not come back when the agent is chosen again', async () => {
    const root = await createdWithToken();
    await press(root, root.querySelector('[data-detail="indexer"]')!, 'Close');
    expect(root.querySelector('[data-detail]')).toBeNull();
    expect(anywhere()).not.toContain(token);
    expect(document.activeElement).toBe(root.querySelector('[data-user="indexer"]'));

    await open(root, 'indexer');
    expect(root.querySelector('[data-detail="indexer"]')).not.toBeNull();
    expect(root.querySelector('[data-token-panel]')).toBeNull();
    expect(anywhere()).not.toContain(token);
  });

  it('is gone when another account is chosen, and does not come back when the agent is chosen again', async () => {
    const root = await createdWithToken();
    await open(root, 'alice');
    expect(root.querySelector('[data-detail="alice"]')).not.toBeNull();
    expect(anywhere()).not.toContain(token);

    await open(root, 'indexer');
    expect(root.querySelector('[data-token-panel]')).toBeNull();
    expect(anywhere()).not.toContain(token);
  });

  it('is gone when the admin leaves the screen', async () => {
    await createdWithToken();
    await unmount();
    expect(anywhere()).not.toContain(token);
  });

  it('copies the token, and says what to do when the clipboard is refused', async () => {
    const writeText = vi.fn(async (_: string) => {});
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
    const root = await createdWithToken();
    const panel = root.querySelector('[data-token-panel]')!;
    await press(root, panel, 'Copy');
    expect(writeText).toHaveBeenCalledWith(token);
    expect(panel.querySelector('[data-copied]')!.textContent).toBe('Copied');

    writeText.mockRejectedValueOnce(new DOMException('denied', 'NotAllowedError'));
    await press(root, panel, 'Copy');
    expect(panel.querySelector('[data-copied]')!.textContent).toBe('Select the token and copy it');
    expect(document.activeElement).toBe(tokenInput(root));
  });
});

describe('an agent’s credential', () => {
  it('shows when the live token was issued and last used', async () => {
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-detail="scribe"] [data-agent-panel]')!;
    expect(panel.querySelector('[data-issued]')!.textContent).not.toBe('');
    expect(panel.querySelector('[data-last-used]')!.textContent).not.toBe('never');
    expect(buttons(panel)).toEqual(['Issue new token', 'Revoke', 'Save limits']);

    const never = structuredClone(agentDetail) as AdminUserDetail;
    never.agent!.credential!.last_used_at = null;
    routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: never });
    await open(root, 'alice');
    await open(root, 'scribe');
    expect(root.querySelector('[data-last-used]')!.textContent).toBe('never');
  });

  it('issues a new token only after asking, and shows it once', async () => {
    const replaced = structuredClone(agentDetail) as AdminUserDetail;
    replaced.agent!.credential = { created_at: issuedGolden.created_at, last_used_at: null };
    routes['POST /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: replaced });
      return { status: 201, body: { ...issuedGolden, token: issuedToken } };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    expect(panel.textContent).toContain('The current token stops working at once.');
    expect(sent.some((s) => s.method === 'POST')).toBe(false);
    await press(root, panel, 'Confirm');
    expect(sent.filter((s) => s.method === 'POST').map((s) => s.url)).toEqual(['/api/v1/admin/users/scribe/credential']);
    expect(tokenInput(root)!.value).toBe(issuedToken);
    expect(document.activeElement).toBe(tokenInput(root));
    expect(panel.querySelector('[data-last-used]')!.textContent).toBe('never');

    await press(root, root.querySelector('[data-token-panel]')!, 'Done');
    expect(anywhere()).not.toContain(issuedToken);
  });

  it('says why when an agent cannot be given a token', async () => {
    routes['POST /api/v1/admin/users/scribe/credential'] = () => refusal('conflict', 409);
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');
    expect(panel.querySelector('.admin-error')!.textContent).toBe('Only an active agent can be given a token.');
    expect(root.querySelector('[data-token-panel]')).toBeNull();
  });

  it('issues a first token without asking, because nothing is replaced', async () => {
    const none = structuredClone(agentDetail) as AdminUserDetail;
    none.agent!.credential = null;
    routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: none });
    routes['POST /api/v1/admin/users/scribe/credential'] = () => ({ status: 201, body: issuedGolden });
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    expect(panel.querySelector('[data-no-credential]')).not.toBeNull();
    await press(root, panel, 'Issue token');
    expect(sent.filter((s) => s.method === 'POST')).toHaveLength(1);
    expect(tokenInput(root)!.value).toBe(issuedGolden.token);
  });

  it('revokes only after asking, and asks for the detail again', async () => {
    const revoked = structuredClone(agentDetail) as AdminUserDetail;
    revoked.agent!.credential = null;
    routes['DELETE /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: revoked });
      return { status: 204 };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    const gets = () => sent.filter((s) => s.method === 'GET' && s.url === '/api/v1/admin/users/scribe').length;
    const before = gets();
    await press(root, panel, 'Revoke');
    expect(sent.some((s) => s.method === 'DELETE')).toBe(false);
    await press(root, panel, 'Confirm');
    expect(sent.filter((s) => s.method === 'DELETE').map((s) => s.url)).toEqual(['/api/v1/admin/users/scribe/credential']);
    expect(gets()).toBe(before + 1);
    expect(panel.querySelector('[data-no-credential]')).not.toBeNull();
    expect(buttons(panel)).toContain('Issue token');
  });

  it('says there is no live token when another admin revoked it first', async () => {
    const revoked = structuredClone(agentDetail) as AdminUserDetail;
    revoked.agent!.credential = null;
    routes['DELETE /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: revoked });
      return refusal('not-found', 404);
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Revoke');
    await press(root, panel, 'Confirm');
    expect(panel.querySelector('[role="status"]')!.textContent).toBe('This agent has no live token.');
    expect(panel.querySelector('[data-no-credential]')).not.toBeNull();
    expect(panel.querySelector('.admin-error')).toBeNull();
  });

  it('revoking clears a token still on show', async () => {
    routes['POST /api/v1/admin/users'] = () => ({ status: 201, body: created() });
    routes['DELETE /api/v1/admin/users/indexer/credential'] = () => ({ status: 204 });
    const root = await renderAt('/admin/users');
    await create(root);
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Revoke');
    await press(root, panel, 'Confirm');
    expect(anywhere()).not.toContain(token);
  });
});

// Terva review 1507: a limits save and a token issue in flight together each
// change only their own fields when they answer, in whichever order.
describe('changes in flight together', () => {
  it('a limits answer that arrives after a new token does not bring the old token back', async () => {
    const replaced = structuredClone(agentDetail) as AdminUserDetail;
    replaced.agent!.credential = { created_at: issuedGolden.created_at, last_used_at: null };
    const limitsAnswer = structuredClone(agentDetail) as AdminUserDetail; // the old credential
    limitsAnswer.agent!.burst = 7;
    let answerLimits!: (a: Answer) => void;
    routes['PUT /api/v1/admin/users/scribe/agent-limits'] = () => new Promise<Answer>((r) => (answerLimits = r));
    routes['POST /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: replaced });
      return { status: 201, body: { ...issuedGolden, token: issuedToken } };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    await type(root.querySelector<HTMLInputElement>('[data-burst]')!, '7');
    await submit(root, root.querySelector('form.admin-limits')!);

    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');
    expect(panel.querySelector('[data-last-used]')!.textContent).toBe('never');

    await act(async () => answerLimits({ status: 200, body: limitsAnswer }));
    await settle(root);
    expect(panel.textContent).toContain('Saved.');
    expect(panel.querySelector('[data-last-used]')!.textContent).toBe('never');
    expect(root.querySelector<HTMLInputElement>('[data-burst]')!.value).toBe('7');
  });
});

// Terva review 1508, finding 1: an issue and a revoke never run together, so
// neither can answer after the other and undo it on screen.
describe('one token change at a time', () => {
  it('holds a revoke until an issue has answered', async () => {
    let answerIssue!: (a: Answer) => void;
    routes['POST /api/v1/admin/users/scribe/credential'] = () => new Promise<Answer>((r) => (answerIssue = r));
    const revoked = structuredClone(agentDetail) as AdminUserDetail;
    revoked.agent!.credential = null;
    routes['DELETE /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: revoked });
      return { status: 204 };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    const asking = (label: string) => panel.querySelector(`.admin-confirm[aria-label="${label}"]`)!;

    // Both confirmations open, then the issue confirmed and still in flight.
    await press(root, panel, 'Revoke');
    await press(root, panel, 'Issue new token');
    const revokeAsk = asking('Revoke');
    const issueAsk = asking('Issue new token');
    await press(root, issueAsk, 'Confirm');
    await press(root, revokeAsk, 'Confirm');
    expect(sent.some((s) => s.method === 'DELETE')).toBe(false);
    expect(panel.querySelector('.admin-error')!.textContent).toContain('Another token change is still being made');

    await act(async () => answerIssue({ status: 201, body: { ...issuedGolden, token: issuedToken } }));
    await settle(root);
    expect(tokenInput(root)!.value).toBe(issuedToken);
    await press(root, revokeAsk, 'Confirm');
    expect(sent.filter((s) => s.method === 'DELETE')).toHaveLength(1);
    expect(root.querySelector('[data-token-panel]')).toBeNull();
    expect(panel.querySelector('[data-no-credential]')).not.toBeNull();
  });

  it('disables the other token action while one is in flight', async () => {
    let answerIssue!: (a: Answer) => void;
    routes['POST /api/v1/admin/users/scribe/credential'] = () => new Promise<Answer>((r) => (answerIssue = r));
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');
    expect(button(panel, 'Revoke').disabled).toBe(true);
    await act(async () => answerIssue({ status: 201, body: issuedGolden }));
    await settle(root);
    expect(button(panel, 'Revoke').disabled).toBe(false);
  });
});

// Terva review 1508, finding 2: the refetch after a token change sets the
// agent's fields only.
describe('the refetch after a token change', () => {
  it('does not undo a role change that answered while it was in flight', async () => {
    const moderator = structuredClone(agentDetail) as AdminUserDetail;
    moderator.user.user.role = 'moderator';
    const later = structuredClone(agentDetail) as AdminUserDetail; // role member, as the GET began
    later.agent!.credential = { created_at: issuedGolden.created_at, last_used_at: 1757800000000 };
    let answerGet!: (a: Answer) => void;
    routes['POST /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => new Promise<Answer>((r) => (answerGet = r));
      return { status: 201, body: issuedGolden };
    };
    routes['PUT /api/v1/admin/users/scribe/role'] = () => ({ status: 200, body: moderator });
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const detail = root.querySelector('[data-detail="scribe"]')!;
    const panel = detail.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');

    const select = detail.querySelector<HTMLSelectElement>('.admin-role select')!;
    await act(async () => {
      select.value = 'moderator';
      select.dispatchEvent(new Event('change'));
    });
    await press(root, detail, 'Change role');
    await press(root, detail, 'Confirm');
    expect(detail.querySelector('[data-role]')!.textContent).toBe('moderator');

    await act(async () => answerGet({ status: 200, body: later }));
    await settle(root);
    expect(detail.querySelector('[data-role]')!.textContent).toBe('moderator');
    expect(panel.querySelector('[data-last-used]')!.textContent).not.toBe('never');
  });

  it('a role answer that arrives after a new token does not bring the old token back', async () => {
    const moderator = structuredClone(agentDetail) as AdminUserDetail; // the old credential
    moderator.user.user.role = 'moderator';
    let answerRole!: (a: Answer) => void;
    routes['PUT /api/v1/admin/users/scribe/role'] = () => new Promise<Answer>((r) => (answerRole = r));
    const replaced = structuredClone(agentDetail) as AdminUserDetail;
    replaced.agent!.credential = { created_at: issuedGolden.created_at, last_used_at: null };
    routes['POST /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: replaced });
      return { status: 201, body: issuedGolden };
    };
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const detail = root.querySelector('[data-detail="scribe"]')!;
    const select = detail.querySelector<HTMLSelectElement>('.admin-role select')!;
    await act(async () => {
      select.value = 'moderator';
      select.dispatchEvent(new Event('change'));
    });
    await press(root, detail, 'Change role');
    await press(root, detail.querySelector('.admin-role')!, 'Confirm');

    const panel = detail.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');
    expect(panel.querySelector('[data-last-used]')!.textContent).toBe('never');

    await act(async () => answerRole({ status: 200, body: moderator }));
    await settle(root);
    expect(detail.querySelector('[data-role]')!.textContent).toBe('moderator');
    expect(panel.querySelector('[data-last-used]')!.textContent).toBe('never');
  });
});

describe('an agent’s write limits', () => {
  const limitInputs = (root: HTMLElement) => ({
    burst: root.querySelector<HTMLInputElement>('[data-burst]')!,
    refill: root.querySelector<HTMLInputElement>('[data-refill]')!,
  });

  it('shows the site defaults as placeholders and the agent’s own limits as values', async () => {
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const { burst, refill } = limitInputs(root);
    expect(burst.placeholder).toBe('20');
    expect(refill.placeholder).toBe('30');
    expect(burst.value).toBe('40');
    expect(refill.value).toBe('');
    expect(root.querySelector('[data-agent-panel]')!.textContent).toContain('Empty uses the site default, 20.');
  });

  it('sends null for an empty field and a number for a filled one, and shows the answer', async () => {
    const answered = structuredClone(agentDetail) as AdminUserDetail;
    answered.agent!.burst = null;
    answered.agent!.refill_seconds = null;
    routes['PUT /api/v1/admin/users/scribe/agent-limits'] = () => ({ status: 200, body: answered });
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const { burst, refill } = limitInputs(root);
    const form = root.querySelector('form.admin-limits')!;

    await type(burst, '');
    await submit(root, form);
    expect(sent.at(-1)).toMatchObject({ method: 'PUT', url: '/api/v1/admin/users/scribe/agent-limits' });
    expect(sent.at(-1)!.body).toStrictEqual({ burst: null, refill_seconds: null });
    expect(form.textContent).toContain('Saved.');

    await type(burst, '5');
    await type(refill, '120');
    await submit(root, form);
    expect(sent.at(-1)!.body).toStrictEqual({ burst: 5, refill_seconds: 120 });
  });

  // Terva review 1509: a refetch that brings another admin's limits updates
  // the fields the admin has not touched, and keeps an edit in progress.
  it('follows a refetched limit into an untouched field, and keeps an edited one', async () => {
    const elsewhere = structuredClone(agentDetail) as AdminUserDetail;
    elsewhere.agent!.credential = { created_at: issuedGolden.created_at, last_used_at: null };
    elsewhere.agent!.burst = 10;
    elsewhere.agent!.refill_seconds = 90;
    routes['POST /api/v1/admin/users/scribe/credential'] = () => {
      routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: elsewhere });
      return { status: 201, body: issuedGolden };
    };
    routes['PUT /api/v1/admin/users/scribe/agent-limits'] = () => ({ status: 200, body: elsewhere });
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const { burst, refill } = limitInputs(root);
    await type(refill, '45');
    const panel = root.querySelector('[data-agent-panel]')!;
    await press(root, panel, 'Issue new token');
    await press(root, panel, 'Confirm');
    await settle(root);
    expect(burst.value).toBe('10');
    expect(refill.value).toBe('45');

    await submit(root, root.querySelector('form.admin-limits')!);
    expect(sent.at(-1)!.body).toStrictEqual({ burst: 10, refill_seconds: 45 });
  });

  it('sends nothing the server would refuse', async () => {
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const { burst, refill } = limitInputs(root);
    const form = root.querySelector('form.admin-limits')!;
    for (const [b, r] of [
      ['0', ''],
      ['1.5', ''],
      ['', '604801'],
      ['-3', ''],
      ['1e3', ''],
    ]) {
      await type(burst, b);
      await type(refill, r);
      await submit(root, form);
      expect(form.querySelector('[role="alert"]')!.textContent).toContain('Enter whole numbers');
    }
    expect(sent.some((s) => s.method === 'PUT')).toBe(false);
  });

  it('says why the server refused', async () => {
    routes['PUT /api/v1/admin/users/scribe/agent-limits'] = () => refusal('validation-failed', 422);
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const form = root.querySelector('form.admin-limits')!;
    await submit(root, form);
    expect(form.querySelector('[role="alert"]')!.textContent).toContain('The server refused those limits');
  });
});

// Terva review 1507, finding 2.
describe('closing an account that did not load', () => {
  it('offers Close beside the error', async () => {
    routes['GET /api/v1/admin/users/scribe'] = () => refusal('internal', 500);
    const root = await renderAt('/admin/users');
    await open(root, 'scribe');
    const pending = root.querySelector('[data-detail-pending="scribe"]')!;
    expect(pending.textContent).toContain('Something went wrong on the server.');
    await press(root, pending, 'Close');
    expect(root.querySelector('[data-detail-pending]')).toBeNull();
    expect(document.activeElement).toBe(root.querySelector('[data-user="scribe"]'));
  });
});

describe('who sees the agent controls', () => {
  it('a moderator sees none of them', async () => {
    viewer.value = asModerator();
    const forModerator = structuredClone(agentDetail) as AdminUserDetail;
    forModerator.agent = null;
    forModerator.identities = null;
    forModerator.sessions = null;
    routes['GET /api/v1/admin/users/scribe'] = () => ({ status: 200, body: forModerator });
    const root = await renderAt('/admin/users');
    expect(buttons(root)).not.toContain('New agent');
    await open(root, 'scribe');
    const detail = root.querySelector('[data-detail="scribe"]')!;
    expect(detail.querySelector('[data-agent-panel]')).toBeNull();
    expect(buttons(detail)).toEqual(['Close']);
  });

  it('an admin viewing a human sees no agent panel', async () => {
    const root = await renderAt('/admin/users');
    await open(root, 'alice');
    expect(root.querySelector('[data-detail="alice"]')).not.toBeNull();
    expect(root.querySelector('[data-agent-panel]')).toBeNull();
  });
});
