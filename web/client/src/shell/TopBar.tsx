import { useLocation } from 'preact-iso';
import { Icon } from '../components/Icon';
import { badges, siteName, viewer } from '../state/viewer';
import { Badge } from './Badge';
import { alerts, current, links, meHref } from './nav';

// TopBar is the desktop and tablet navigation (mockup nav()): the wordmark,
// the sections, then search, alerts, the moderation queue for those who
// have one (/me's queue is a number for them), and the reader.
export function TopBar() {
  const { path, url } = useLocation();
  const me = viewer.value;
  const queue = badges.value.queue;
  return (
    <header class="topbar desktop" data-shell="topbar">
      <a class="wordmark" href="/">
        {siteName}
      </a>
      <nav aria-label="Sections">
        {links.value.map((l) => (
          <a key={l.id} href={l.href} data-nav={l.id} aria-current={current(l.href, path) ? 'page' : undefined}>
            {l.label}
            <Badge n={l.badge} what={l.what ?? ''} />
          </a>
        ))}
      </nav>
      <div class="tools">
        <a class="search" href="/search" aria-current={current('/search', path) ? 'page' : undefined}>
          <Icon name="search" />
          <span>Search</span>
        </a>
        <a
          class="icon-link"
          href={alerts.value.href}
          data-nav="alerts"
          aria-label="Alerts"
          aria-current={current(alerts.value.href, path) ? 'page' : undefined}
        >
          <Icon name="bell" />
          <Badge n={alerts.value.badge} what="notifications" />
        </a>
        {queue !== null && (
          <a
            class="icon-link"
            href="/admin/queue"
            data-nav="queue"
            aria-label="Moderation queue"
            aria-current={current('/admin/queue', path) ? 'page' : undefined}
          >
            <Icon name="inbox" />
            <Badge n={queue} what="items to review" />
          </a>
        )}
        <a class="who" href={meHref(url)} data-nav="me">
          {me.user ? me.user.handle : 'Sign in'}
        </a>
      </div>
    </header>
  );
}
