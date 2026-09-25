import type { ComponentChildren } from 'preact';
import { ReplyBar } from '../composer/ReplyBar';
import { isPhone } from '../state/layout';
import { shellContext } from '../state/shell';
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
// On a thread the reader may reply to, the phone's reply bar takes the tab
// bar's place (docs/06 "Mobile"): the screen says so through its shell
// context.
export function Shell({ children }: { children: ComponentChildren }) {
  const phone = isPhone.value;
  const reply = phone ? shellContext.value?.reply : undefined;
  const cls = `shell ${phone ? 'phone' : 'desktop'}${reply ? ' has-replybar' : ''}`;
  return (
    <div class={cls} data-layout={phone ? 'phone' : 'desktop'}>
      {phone ? <PhoneTopBar /> : <TopBar />}
      <div class="shell-content">
        {children}
      </div>
      {phone && (reply ? <ReplyBar thread={reply} /> : <TabBar />)}
    </div>
  );
}
