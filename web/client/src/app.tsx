import { LocationProvider, Router, Route } from 'preact-iso';
import { viewer } from './state';

function Home() {
  const me = viewer.value;
  return (
    <main class="scaffold">
      <h1>ketju</h1>
      <p>{me.user ? `Signed in as ${me.user.display_name}.` : 'Not signed in.'}</p>
    </main>
  );
}

function NotFound() {
  return (
    <main class="scaffold">
      <h1>Not found</h1>
      <p>
        There is nothing at this address. <a href="/">Go to the forum index.</a>
      </p>
    </main>
  );
}

export function App() {
  return (
    <LocationProvider>
      <Router>
        <Route path="/" component={Home} />
        <Route default component={NotFound} />
      </Router>
    </LocationProvider>
  );
}
