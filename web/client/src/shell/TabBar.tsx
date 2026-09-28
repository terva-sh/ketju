import { useLocation } from 'preact-iso';
import { Icon } from '../components/Icon';
import { Badge } from './Badge';
import { current, tabs } from './nav';

// TabBar is the phone's bottom navigation (mockup mtabbar()): five tabs,
// each a full-height target, clear of the home indicator (base.css).
export function TabBar() {
  const { path, url } = useLocation();
  return (
    <nav class="tabbar" aria-label="Sections" data-shell="tabbar">
      {tabs(url).map((t) => (
        <a key={t.id} href={t.href} data-nav={t.id} aria-current={current(t.href, path) ? 'page' : undefined}>
          <span class="tab-icon">
            <Icon name={t.icon} size={20} />
            <Badge n={t.badge} what={t.id === 'alerts' ? 'notifications' : 'unread threads'} />
          </span>
          <span class="tab-label">{t.label}</span>
        </a>
      ))}
    </nav>
  );
}
