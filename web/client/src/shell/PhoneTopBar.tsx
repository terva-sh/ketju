import { Icon } from '../components/Icon';
import { shellContext } from '../state/shell';
import { MoreMenu } from './MoreMenu';

// PhoneTopBar is the phone's 52 px bar (mockup mtopbar()): back, the
// screen's title on one line, cut short rather than wrapped or pushing the
// controls off screen, then search and the screen's more menu.
export function PhoneTopBar() {
  const ctx = shellContext.value;
  return (
    <header class="topbar phone" data-shell="phone-topbar">
      {ctx?.back ? (
        <a class="tb-button" href={ctx.back.href} aria-label={`Back to ${ctx.back.label}`}>
          <Icon name="back" size={20} />
        </a>
      ) : (
        <span class="tb-spacer" />
      )}
      {ctx ? (
        <span class="title" data-shell="title" title={ctx.title}>
          {ctx.title}
        </span>
      ) : (
        <a class="title wordmark" data-shell="title" href="/">
          Ketju
        </a>
      )}
      <a class="tb-button" href="/search" aria-label="Search">
        <Icon name="search" size={20} />
      </a>
      <MoreMenu actions={ctx?.actions ?? []} />
    </header>
  );
}
