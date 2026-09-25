import { Component, type ComponentChildren } from 'preact';
import { useEffect, useMemo } from 'preact/hooks';
import { LocationProvider, Router, Route, useLocation } from 'preact-iso';
import { guardFocus, routeFocuser } from './components/focus';
import { clientScope, Fallback, routes } from './routes';
import { NotFound } from './screens/Placeholder';
import { Shell } from './shell/Shell';

export function App() {
  return (
    <LocationProvider scope={clientScope}>
      <Routes />
    </LocationProvider>
  );
}

function Routes() {
  const { url } = useLocation();
  // Focus follows the reader to each new screen, and is never lost with a
  // removed element (TKT-01M29CGBXK).
  const onRouteChange = useMemo(() => routeFocuser(location.pathname), []);
  useEffect(() => guardFocus(), []);
  return (
    <Shell>
      <UndecodableAddress url={url}>
        <Router onRouteChange={onRouteChange}>
          {routes.map((r) => (
            <Route key={r.path} path={r.path} component={r.component} />
          ))}
          <Route default component={Fallback} />
        </Router>
      </UndecodableAddress>
    </Shell>
  );
}

// UndecodableAddress catches an address the router cannot decode.
// preact-iso's matcher runs decodeURIComponent over each path parameter
// while rendering, and a same-origin link such as /f/% or /t/id/%FF makes it
// throw. No pattern over the raw href can tell a decodable escape from one
// that is not, so rather than guess, the address is NotFound — it names
// nothing — and the next navigation renders normally. Any other error is not
// this one's to hide.
class UndecodableAddress extends Component<{ url: string; children: ComponentChildren }, { failed: string | null }> {
  state = { failed: null as string | null };

  componentDidCatch(err: unknown) {
    if (!(err instanceof URIError)) throw err;
    this.setState({ failed: this.props.url });
  }

  render() {
    return this.state.failed === this.props.url ? <NotFound /> : this.props.children;
  }
}
