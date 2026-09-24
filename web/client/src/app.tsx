import { LocationProvider, Router, Route } from 'preact-iso';
import { clientScope, Fallback, routes } from './routes';

export function App() {
  return (
    <LocationProvider scope={clientScope}>
      <Router>
        {routes.map((r) => (
          <Route key={r.path} path={r.path} component={r.component} />
        ))}
        <Route default component={Fallback} />
      </Router>
    </LocationProvider>
  );
}
