import { useLocation } from 'preact-iso';
import { ApplicationDetail } from './ApplicationDetail';
import { ApplicationList, filters, type Filter } from './ApplicationList';
import './applications.css';

// ApplicationsScreen is /admin/applications (TKT-01M3NRX2NN), where
// moderators and admins decide approval-mode applications (docs/05 "mode =
// approval" step 5). It is one route: ?id=N opens an application and
// ?status= filters the list, so docs/07's URL scheme stays as it is.
//
// The server's staff gate decides who may use it (internal/web/api
// applications.go); AdminRoot offers it to moderators and admins only.
export function ApplicationsScreen() {
  const { query } = useLocation();
  const raw = query.id ?? '';
  const id = /^[1-9][0-9]{0,15}$/.test(raw) ? Number(raw) : null;
  const status: Filter = (filters as readonly string[]).includes(query.status ?? '') ? (query.status as Filter) : 'pending';
  return (
    <div class="admin-applications">
      {/* v1 sends no email (docs/10 item 12, TKT-01M29CP2N0). */}
      <p class="app-email-line" data-no-email>
        Applicants are not emailed. They learn the outcome on their application status page, and notifications are
        in-app only.
      </p>
      {raw !== '' && id === null ? (
        <p>
          That is not an application number. <a href="/admin/applications">Back to applications</a>
        </p>
      ) : id !== null ? (
        <ApplicationDetail key={id} id={id} />
      ) : (
        <ApplicationList key={status} status={status} />
      )}
    </div>
  );
}
