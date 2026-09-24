import { useLocation } from 'preact-iso';
import { Icon } from '../components/Icon';
import { viewer } from '../state/viewer';
import { Badge } from './Badge';
import { alerts, current, links, meHref } from './nav';

// TopBar is the desktop and tablet navigation (mockup nav()): the wordmark,
// the sections, then search, alerts, the moderation queue for those who
// have one, and the reader.
export function TopBar() {
  const { path } = useLocation();
  const me = viewer.value;
  const role = me.user?.role;
  return (
    <header class="topbar desktop" data-shell="topbar">
      <a class="wordmark" href="/">
        Ketju
      </a>
      <nav aria-label="Sections">
        {links.value.map((l) => (
          <a key={l.id} href={l.href} data-nav={l.id} aria-current={current(l.href, path) ? 'page' : undefined}>
            {l.label}
            <Badge n={l.badge} what="unread threads" />
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
        {(role === 'moderator' || role === 'admin') && (
          <a class="icon-link" href="/admin/queue" aria-label="Moderation queue">
            <Icon name="inbox" />
          </a>
        )}
        <a class="who" href={meHref.value} data-nav="me">
          {me.user ? me.user.handle : 'Sign in'}
        </a>
      </div>
    </header>
  );
}
