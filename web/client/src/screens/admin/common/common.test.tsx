import { render, type ComponentChild } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ProblemError } from '../../../api/problem';
import { ConfirmButton } from './ConfirmButton';
import { Field } from './Field';
import { send } from './send';
import { Table } from './Table';

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.replaceChildren();
});

async function mount(node: ComponentChild): Promise<HTMLElement> {
  const root = document.createElement('div');
  document.body.replaceChildren(root);
  await act(async () => render(node, root));
  return root;
}

const problem = (type: string, status: number) =>
  new Response(JSON.stringify({ type, status, title: type }), {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  });

describe('send', () => {
  it.each([
    ['conflict', 409, 'Someone else changed this while you were working.'],
    ['validation-failed', 422, 'That was not accepted. Check what you entered'],
    ['forbidden', 403, 'Your account may not make this change.'],
    ['internal', 500, 'Something went wrong on the server.'],
  ])('turns %s into a sentence', async (type, status, words) => {
    vi.stubGlobal('fetch', async () => problem(type, status));
    const r = await send('POST', '/api/v1/admin/x', {});
    expect(r.ok).toBe(false);
    if (!r.ok) {
      expect(r.message).toContain(words);
      expect(r.error.type).toBe(type);
    }
  });

  it('answers with the result when the change is made', async () => {
    let sent: RequestInit | undefined;
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      sent = init;
      return new Response(JSON.stringify({ id: 4 }), { status: 200, headers: { 'Content-Type': 'application/json' } });
    });
    expect(await send('PATCH', '/api/v1/admin/x', { a: 1 })).toEqual({ ok: true, value: { id: 4 } });
    expect(sent?.body).toBe('{"a":1}');
    expect((sent?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
  });

  it('still rejects an abort', async () => {
    vi.stubGlobal('fetch', async () => {
      throw new DOMException('aborted', 'AbortError');
    });
    await expect(send('DELETE', '/api/v1/admin/x')).rejects.toThrow('aborted');
  });
});

describe('ConfirmButton', () => {
  it('asks first, and acts only on Confirm', async () => {
    const acted = vi.fn();
    const root = await mount(<ConfirmButton label="Suspend" prompt="Suspend alice?" onConfirm={acted} />);
    await act(async () => root.querySelector('button')!.click());
    expect(acted).not.toHaveBeenCalled();
    expect(root.textContent).toContain('Suspend alice?');

    const buttons = () => [...root.querySelectorAll('button')].map((b) => b.textContent);
    expect(buttons()).toEqual(['Confirm', 'Cancel']);
    await act(async () => root.querySelectorAll('button')[1].click());
    expect(acted).not.toHaveBeenCalled();
    expect(root.textContent).not.toContain('Suspend alice?');

    await act(async () => root.querySelector('button')!.click());
    await act(async () => root.querySelectorAll('button')[0].click());
    expect(acted).toHaveBeenCalledTimes(1);
    expect(buttons()).toEqual(['Suspend']);
  });

  // Terva review 1374 (PR #145, head 819e596): a failed action keeps the
  // confirmation open, says why, and can be tried again.
  it('stays open and says why when the action is refused', async () => {
    let attempts = 0;
    const acted = vi.fn(async () => {
      attempts++;
      if (attempts === 1) throw new ProblemError('conflict', 409);
    });
    const root = await mount(<ConfirmButton label="Suspend" prompt="Suspend alice?" onConfirm={acted} />);
    await act(async () => root.querySelector('button')!.click());
    await act(async () => root.querySelectorAll('button')[0].click());
    expect(root.querySelector('[role="alert"]')?.textContent).toContain('Someone else changed this');
    expect([...root.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Confirm', 'Cancel']);
    expect(root.querySelectorAll('button')[0].disabled).toBe(false);

    await act(async () => root.querySelectorAll('button')[0].click());
    expect(acted).toHaveBeenCalledTimes(2);
    expect([...root.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Suspend']);
  });

  it('stays open when the action reports its own failure with false', async () => {
    const root = await mount(<ConfirmButton label="Delete" prompt="Delete it?" onConfirm={async () => false} />);
    await act(async () => root.querySelector('button')!.click());
    await act(async () => root.querySelectorAll('button')[0].click());
    expect([...root.querySelectorAll('button')].map((b) => b.textContent)).toEqual(['Confirm', 'Cancel']);
    expect(root.querySelector('[role="alert"]')).toBeNull();
  });
});

describe('Field', () => {
  it('ties the label, hint and error to the control', async () => {
    const root = await mount(
      <Field label="Name" hint="Shown in the header." error="Too long.">
        {(c) => <input {...c} />}
      </Field>,
    );
    const input = root.querySelector('input')!;
    expect(root.querySelector('label')!.htmlFor).toBe(input.id);
    const described = input.getAttribute('aria-describedby')!.split(' ');
    expect(described.map((id) => document.getElementById(id)?.textContent)).toEqual(['Shown in the header.', 'Too long.']);
    expect(input.getAttribute('aria-invalid')).toBe('true');
  });
});

describe('Table', () => {
  const columns = [
    { key: 'name', label: 'Name', cell: (r: { name: string; n: number }) => r.name },
    { key: 'n', label: 'Posts', cell: (r: { name: string; n: number }) => r.n, numeric: true },
  ];

  it('draws a header per column and a row per item', async () => {
    const root = await mount(
      <Table caption="Members" columns={columns} rows={[{ name: 'alice', n: 3 }]} rowKey={(r) => r.name} empty="None." />,
    );
    expect([...root.querySelectorAll('th')].map((th) => th.textContent)).toEqual(['Name', 'Posts']);
    expect([...root.querySelectorAll('td')].map((td) => td.textContent)).toEqual(['alice', '3']);
    expect(root.querySelector('caption')!.textContent).toBe('Members');
  });

  it('says what is missing when there are no rows', async () => {
    const root = await mount(<Table caption="Members" columns={columns} rows={[]} rowKey={(r) => r.name} empty="None." />);
    expect(root.querySelector('table')).toBeNull();
    expect(root.textContent).toBe('None.');
  });
});
