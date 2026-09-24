import type { ComponentChildren } from 'preact';
import { isPhone } from '../state/layout';
import { PhoneTopBar } from './PhoneTopBar';
import { TabBar } from './TabBar';
import { TopBar } from './TopBar';
import './shell.css';

// Shell is the navigation around every screen. It is the one place layout
// is a component choice rather than CSS (docs/07 "Responsive"): a top bar
// above 720 px, a phone top bar and a bottom tab bar below it. The screen
// between them is the same element at both widths, so crossing the
// breakpoint re-renders the bars and leaves the screen and the address as
// they were.
//
// The tab bar stays on the thread page until the phone reply bar
// (TKT-01M29CGBVN) exists to take its place.
export function Shell({ children }: { children: ComponentChildren }) {
  const phone = isPhone.value;
  return (
    <div class={phone ? 'shell phone' : 'shell desktop'} data-layout={phone ? 'phone' : 'desktop'}>
      {phone ? <PhoneTopBar /> : <TopBar />}
      <div class="shell-content">
        {children}
      </div>
      {phone && <TabBar />}
    </div>
  );
}
