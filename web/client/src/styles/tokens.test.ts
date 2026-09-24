import { describe, expect, it } from 'vitest';
import generator from '../../../../docs/mockups/generate.py?raw';
import tokens from './tokens.css?raw';

// The mockups are the only place the intended look exists (TKT-01M29CCC0F),
// so the colour tokens are generate.py's LIGHT and DARK dicts exactly. This
// holds the two files together: a colour changed in one and not the other
// fails here.

function dict(name: string): Record<string, string> {
  const body = new RegExp(`${name}\\s*=\\s*dict\\(([\\s\\S]*?)\\n\\)`).exec(generator)?.[1];
  if (!body) throw new Error(`no ${name} in generate.py`);
  return Object.fromEntries([...body.matchAll(/(\w+)="([^"]*)"/g)].map((m) => [`--${m[1].replace(/_/g, '-')}`, m[2]]));
}

// block returns the declarations of the first rule whose selector is sel.
function block(sel: string): Record<string, string> {
  const at = tokens.indexOf(`${sel} {`);
  if (at < 0) throw new Error(`no ${sel} rule in tokens.css`);
  const body = tokens.slice(at + sel.length + 2, tokens.indexOf('}', at));
  return Object.fromEntries([...body.matchAll(/(--[\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
}

const light = dict('LIGHT');
const dark = dict('DARK');

describe('the colour tokens', () => {
  it('reads both themes from generate.py', () => {
    expect(Object.keys(light).length).toBeGreaterThan(10);
    expect(Object.keys(dark)).toEqual(Object.keys(light));
  });

  it.each(Object.keys(light))('%s is the mockups’ light value by default', (k) => {
    expect(block(':root')[k]).toBe(light[k]);
  });

  it.each(Object.keys(dark))('%s is the mockups’ dark value when the system asks for dark', (k) => {
    expect(block(':root:not([data-theme])')[k]).toBe(dark[k]);
  });

  it.each(Object.keys(dark))('%s is the mockups’ dark value when the reader chooses dark', (k) => {
    expect(block(":root[data-theme='dark']")[k]).toBe(dark[k]);
  });

  it('gives the two dark rules the same declarations', () => {
    expect(block(':root:not([data-theme])')).toEqual(block(":root[data-theme='dark']"));
  });

  it('applies the system dark only where the reader has not chosen', () => {
    expect(tokens).toMatch(/@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-theme\]\) \{/);
  });

  it('themes every avatar colour', () => {
    const avatars = Object.keys(block(':root')).filter((k) => k.startsWith('--avatar-'));
    expect(avatars.length).toBe(8);
    for (const k of avatars) expect(block(":root[data-theme='dark']")[k]).toBeDefined();
  });
});
