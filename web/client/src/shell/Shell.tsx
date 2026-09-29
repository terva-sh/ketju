import type { ComponentChildren } from 'preact';
import { ReplyBar } from '../composer/ReplyBar';
import { focusMainHeading } from '../components/focus';
import { isPhone } from '../state/layout';
import { shellContext } from '../state/shell';
import { viewer } from '../state/viewer';
import { PhoneTopBar } from './PhoneTopBar';
import { SignInHint } from './SignInHint';
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
      {/* First in the page, and on screen only when focused. It moves focus
          rather than following the hash, so the address stays as it was. */}
      <a
        class="skip-link"
        href="#main"
        onClick={(e) => {
          e.preventDefault();
          focusMainHeading();
        }}
      >
        Skip to content
      </a>
      {phone ? <PhoneTopBar /> : <TopBar />}
      {!viewer.value.user && <SignInHint />}
      <div class="shell-content" id="main">
        {children}
      </div>
      {phone && (reply ? <ReplyBar thread={reply} /> : <TabBar />)}
    </div>
  );
}
