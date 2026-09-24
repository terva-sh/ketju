import { LocationProvider, Router, Route } from 'preact-iso';
import { routes } from './routes';
import { NotFound } from './screens/Placeholder';

export function App() {
  return (
    <LocationProvider>
      <Router>
        {routes.map((r) => (
          <Route key={r.path} path={r.path} component={r.component} />
        ))}
        <Route default component={NotFound} />
      </Router>
    </LocationProvider>
  );
}
