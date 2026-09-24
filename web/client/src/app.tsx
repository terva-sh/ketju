import { LocationProvider, Router, Route } from 'preact-iso';
import { clientScope, routes } from './routes';
import { NotFound } from './screens/Placeholder';

export function App() {
  return (
    <LocationProvider scope={clientScope}>
      <Router>
        {routes.map((r) => (
          <Route key={r.path} path={r.path} component={r.component} />
        ))}
        <Route default component={NotFound} />
      </Router>
    </LocationProvider>
  );
}
