import { signal } from '@preact/signals';
import { render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { resetApiCache } from '../api/client';
import { ComposerBody, previewDelay, type ComposerFields } from './ComposerBody';

// The composer body the reply composer and the new-thread screen share
// (TKT-01M3NS16VH). Composer.test.tsx and ComposerSheet.test.tsx hold its
// behaviour inside the reply composer; these hold what differs with no
// thread.

const fields = (): ComposerFields => ({ source: signal(''), caret: signal(0), summoned: signal(0), posting: signal(false) });

function stubRender() {
  const bodies: unknown[] = [];
  vi.stubGlobal('fetch', async (url: string, init: RequestInit = {}) => {
    if (url !== '/api/v1/render') return new Response('{}', { status: 404 });
    bodies.push(JSON.parse(String(init.body)));
    return new Response(JSON.stringify({ html: '<p>x</p>', mentions: [], quotes: [], reply_to: null }), { status: 200 });
  });
  return bodies;
}

async function mount(el: preact.JSX.Element) {
  const root = document.createElement('div');
  document.body.replaceChildren(root);
  await act(() => render(el, root));
  return root;
}

async function type(root: HTMLElement, text: string) {
  const a = root.querySelector('textarea')!;
  a.value = text;
  await act(() => void a.dispatchEvent(new Event('input', { bubbles: true })));
}

const wait = (ms: number) => act(() => new Promise((r) => setTimeout(r, ms)));

afterEach(() => {
  resetApiCache();
  vi.unstubAllGlobals();
});

describe('the composer body', () => {
  it('previews in a thread, with the reply, only when given one', async () => {
    const bodies = stubRender();
    const inThread = await mount(<ComposerBody state={fields()} thread="t1" replyTo={3} name="Reply" label="Reply text" placeholder="" />);
    await type(inThread, 'one');
    await wait(previewDelay + 20);
    const alone = await mount(<ComposerBody state={fields()} name="New thread" label="Thread text" placeholder="" />);
    await type(alone, 'two');
    await wait(previewDelay + 20);
    expect(bodies).toEqual([{ source: 'one', thread_id: 't1', reply_to: 3 }, { source: 'two' }]);
    expect(alone.querySelector('section')!.hasAttribute('data-composer')).toBe(false);
    expect(alone.querySelector('section')!.getAttribute('aria-label')).toBe('New thread');
  });

  it('writes to the state it is given, and marks the textarea a refusal named', async () => {
    stubRender();
    const f = fields();
    const root = await mount(<ComposerBody state={f} name="New thread" label="Thread text" placeholder="Write" invalid />);
    await type(root, 'hello');
    expect(f.source.value).toBe('hello');
    const a = root.querySelector('textarea')!;
    expect(a.getAttribute('aria-label')).toBe('Thread text');
    expect(a.getAttribute('aria-invalid')).toBe('true');
    await act(() => void (f.posting.value = true));
    expect(a.readOnly).toBe(true);
    await type(root, 'changed');
    expect(f.source.value).toBe('hello');
  });

  it('puts head, notice and foot around the shared parts', async () => {
    const root = await mount(
      <ComposerBody
        state={fields()}
        name="X"
        label="L"
        placeholder=""
        head={<span data-head />}
        notice={<p data-notice />}
        foot={<footer data-foot />}
      />,
    );
    const order = [...root.querySelectorAll('[data-head], [role=tablist], [data-notice], [role=toolbar], textarea, [data-foot]')].map(
      (e) => e.getAttribute('role') ?? e.tagName.toLowerCase() + Object.keys((e as HTMLElement).dataset).join(''),
    );
    expect(order).toEqual(['spanhead', 'tablist', 'pnotice', 'toolbar', 'combobox', 'footerfoot']);
  });
});
