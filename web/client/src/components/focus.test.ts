import { h, render, type ComponentChild } from 'preact';
import { useRef } from 'preact/hooks';
import { act } from 'preact/test-utils';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { focusMainHeading, guardFocus, isControl, routeFocuser, useModal } from './focus';

// The page-wide focus helpers (TKT-01M29CGBXK, AC2). The Router's call and
// the screens are covered end to end by e2e/focus.spec.ts.

afterEach(() => document.body.replaceChildren());

function page(html: string) {
  document.body.innerHTML = html;
}

// frame waits out the guard's wait, a frame then a task, twice over: the
// guard's own wait is scheduled from a mutation observer's microtask, which
// runs after this one is.
const once = () => new Promise<void>((r) => requestAnimationFrame(() => setTimeout(r, 0)));
const frame = async () => {
  await once();
  await once();
};

describe('focusMainHeading', () => {
  it('focuses the main heading without scrolling, and makes it focusable', () => {
    page('<h1 id="other">Not this</h1><main><p>x</p><h1 id="here">Here</h1><h1>Nor this</h1></main>');
    const h = document.getElementById('here')!;
    const focus = vi.spyOn(h, 'focus');
    focusMainHeading();
    expect(document.activeElement).toBe(h);
    expect(h.getAttribute('tabindex')).toBe('-1');
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
  });

  it('leaves a heading that already takes focus as it is', () => {
    page('<main><h1 tabindex="0">Here</h1></main>');
    focusMainHeading();
    expect(document.querySelector('h1')!.getAttribute('tabindex')).toBe('0');
    expect(document.activeElement).toBe(document.querySelector('h1'));
  });

  it('falls back to the main, and does nothing without one', () => {
    page('<main><p>No heading</p></main>');
    focusMainHeading();
    expect(document.activeElement).toBe(document.querySelector('main'));
    page('<div><h1>Outside</h1></div>');
    expect(() => focusMainHeading()).not.toThrow();
    expect(document.activeElement).toBe(document.body);
  });
});

describe('routeFocuser', () => {
  it('moves focus on a new screen, and not within one thread', () => {
    page('<main><h1>Screen</h1></main><button id="b">b</button>');
    const b = document.getElementById('b')!;
    const h = document.querySelector('h1')!;
    const on = routeFocuser('/');
    const from = (url: string) => {
      b.focus();
      on(url);
      return document.activeElement === h;
    };
    expect(from('/f/general')).toBe(true);
    expect(from('/t/abc/slug?page=1')).toBe(true);
    // The same thread: another page, a permalink, a history.
    expect(from('/t/abc/slug?page=2')).toBe(false);
    expect(from('/t/abc/slug/31')).toBe(false);
    expect(from('/t/abc')).toBe(false);
    // Another thread is another screen.
    expect(from('/t/abd/other')).toBe(true);
    expect(from('/f/general?cursor=x#top')).toBe(true);
    // The same path again, as the Router never sends but a hash might.
    expect(from('/f/general#x')).toBe(false);
  });

  it('starts from the path the app loaded at', () => {
    page('<main><h1>Screen</h1></main><button id="b">b</button>');
    const b = document.getElementById('b')!;
    b.focus();
    routeFocuser('/t/abc/slug')('/t/abc/slug/9');
    expect(document.activeElement).toBe(b);
  });
});

describe('guardFocus', () => {
  let stop: () => void = () => {};
  afterEach(() => stop());

  it('moves focus from a removed control to the nearest control around it', async () => {
    page('<main><h1>Screen</h1><div id="box" tabindex="0"><p><button id="gone">x</button></p></div></main>');
    stop = guardFocus();
    document.getElementById('gone')!.focus();
    document.getElementById('gone')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.getElementById('box'));
  });

  it('skips a focusable ancestor that went with it', async () => {
    page('<main><h1>Screen</h1><div id="outer" tabindex="0"><div id="inner" tabindex="0"><button id="gone">x</button></div></div></main>');
    stop = guardFocus();
    document.getElementById('gone')!.focus();
    document.getElementById('inner')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.getElementById('outer'));
  });

  it('moves it to the heading when nothing around it takes focus', async () => {
    page('<main><h1>Screen</h1><nav><a id="gone" href="/x">x</a></nav></main>');
    stop = guardFocus();
    document.getElementById('gone')!.focus();
    document.querySelector('nav')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.querySelector('h1'));
  });

  it('waits for a component that returns focus itself', async () => {
    page('<main><h1>Screen</h1><button id="opener">o</button><div id="dialog"><button id="in">i</button></div></main>');
    stop = guardFocus();
    const heading = vi.fn();
    document.querySelector('h1')!.addEventListener('focus', heading);
    document.getElementById('in')!.focus();
    document.getElementById('dialog')!.remove();
    // The dialog's own effect, after the next paint, as Preact runs one.
    requestAnimationFrame(() => document.getElementById('opener')!.focus());
    await frame();
    expect(document.activeElement).toBe(document.getElementById('opener'));
    // Focus went straight back, not by way of the heading.
    expect(heading).not.toHaveBeenCalled();
  });

  it('leaves focus alone when it was not lost', async () => {
    page('<main><h1>Screen</h1><button id="a">a</button><button id="b">b</button><p id="p">p</p></main>');
    stop = guardFocus();
    document.getElementById('a')!.focus();
    // Something else changes; focus is where it was.
    document.getElementById('p')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.getElementById('a'));
    // Focus left on purpose, to the body, with the element still here.
    (document.activeElement as HTMLElement).blur();
    document.getElementById('b')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.body);
  });

  it('stops when torn down', async () => {
    page('<main><h1>Screen</h1><button id="gone">x</button></main>');
    stop();
    stop = guardFocus();
    stop();
    document.getElementById('gone')!.focus();
    document.getElementById('gone')!.remove();
    await frame();
    expect(document.activeElement).toBe(document.body);
  });
});

describe('isControl', () => {
  it('is a focusable control in the page, and not a heading focus was moved to', () => {
    page('<main><h1 tabindex="-1">h</h1><button id="b">b</button><button id="off" disabled>d</button><a id="a" href="/">a</a><a id="na">n</a></main>');
    const $ = (id: string) => document.getElementById(id);
    expect(isControl($('b'))).toBe(true);
    expect(isControl($('a'))).toBe(true);
    expect(isControl($('off'))).toBe(false);
    expect(isControl($('na'))).toBe(false);
    expect(isControl(document.querySelector('h1'))).toBe(false);
    expect(isControl(document.body)).toBe(false);
    expect(isControl(null)).toBe(false);
    const loose = document.createElement('button');
    expect(isControl(loose)).toBe(false);
  });
});

// useModal, for the modal dialogs (TKT-01M3TS0ZV9). Each dialog's own tests
// press Escape straight after opening it; these pin what no one dialog
// shows on its own.
describe('useModal', () => {
  const hosts: HTMLElement[] = [];
  afterEach(async () => {
    for (const host of hosts.splice(0)) await act(async () => render(null, host));
  });
  const press = (k: string, on: EventTarget) => {
    const e = new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true });
    on.dispatchEvent(e);
    return e;
  };
  async function mount(dialog: ComponentChild) {
    const host = document.createElement('div');
    document.body.append(host);
    hosts.push(host);
    await act(async () => render(dialog, host));
    return host;
  }
  function Dialog({ onClose, back }: { onClose: () => void; back?: HTMLElement }) {
    const root = useRef<HTMLDivElement>(null);
    useModal(root, onClose, (el) => {
      el?.querySelector<HTMLElement>('button')?.focus();
      return back && (() => back.focus());
    });
    return h('div', { ref: root, role: 'dialog' }, h('button', { id: 'first' }, 'first'), h('button', { id: 'last' }, 'last'));
  }

  it('takes Escape and Tab wherever focus is', async () => {
    const onClose = vi.fn();
    await mount(h(Dialog, { onClose }));
    expect(document.activeElement?.id).toBe('first');
    // A click on the dialog's text leaves focus on the body.
    (document.activeElement as HTMLElement).blur();
    expect(document.activeElement).toBe(document.body);
    expect(press('Tab', document.body).defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe('first');
    (document.activeElement as HTMLElement).blur();
    expect(press('Escape', document.body).defaultPrevented).toBe(true);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  // A menu that opened the dialog keeps its Escape listener on the document
  // until the frame after it closed. It was added first, so in the bubble
  // phase it would hear the key first and take it.
  it('hears Escape before a listener added earlier', async () => {
    let seen: boolean | null = null;
    const earlier = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      seen = e.defaultPrevented;
      if (!e.defaultPrevented) e.preventDefault();
    };
    document.addEventListener('keydown', earlier);
    try {
      const onClose = vi.fn();
      await mount(h(Dialog, { onClose }));
      press('Escape', document.activeElement!);
      expect(onClose).toHaveBeenCalledTimes(1);
      expect(seen).toBe(true);
    } finally {
      document.removeEventListener('keydown', earlier);
    }
  });

  it('lets go of the keys when it goes, and puts focus back', async () => {
    const opener = document.createElement('button');
    document.body.append(opener);
    opener.focus();
    const onClose = vi.fn();
    const host = await mount(h(Dialog, { onClose, back: opener }));
    expect(document.activeElement?.id).toBe('first');
    await act(async () => render(null, host));
    expect(document.activeElement).toBe(opener);
    expect(press('Escape', opener).defaultPrevented).toBe(false);
    expect(press('Tab', document.body).defaultPrevented).toBe(false);
    expect(onClose).not.toHaveBeenCalled();
  });
});
