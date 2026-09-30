import { Icon } from '../components/Icon';
import { shellContext } from '../state/shell';
import { badges } from '../state/viewer';
import { Badge } from './Badge';
import { MoreMenu } from './MoreMenu';

// PhoneTopBar is the phone's 52 px bar (mockup mtopbar()): back, the
// screen's title on one line, cut short rather than wrapped or pushing the
// controls off screen, then search, the moderation queue for those who have
// one, with the top bar's badge (mockup mtopbar(); it is no sixth tab, docs/10
// item 25), the screen's primary action when it has one (the new-thread
// screen's Post), and the screen's more menu.
export function PhoneTopBar() {
  const ctx = shellContext.value;
  const queue = badges.value.queue;
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
      {queue !== null && (
        <a class="tb-button" href="/admin/queue" data-nav="queue" aria-label="Moderation queue">
          <Icon name="inbox" size={20} />
          <Badge n={queue} what="items to review" />
        </a>
      )}
      {ctx?.primary && (
        <button
          type="button"
          class="tb-primary"
          data-shell-primary={ctx.primary.id}
          disabled={ctx.primary.disabled}
          onClick={ctx.primary.run}
        >
          {ctx.primary.label}
        </button>
      )}
      <MoreMenu actions={ctx?.actions ?? []} />
    </header>
  );
}
