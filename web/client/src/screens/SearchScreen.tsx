import { useLayoutEffect, useRef, useState } from 'preact/hooks';
import { useLocation } from 'preact-iso';
import { apiGet, peekGet } from '../api/client';
import type { Forum, ForumTree, SearchResult, SearchResults, SnippetPart } from '../api/dto';
import { isProblem, problemMessage } from '../api/problem';
import { useResource } from '../api/resource';
import { ErrorPanel } from '../components/ErrorPanel';
import { permalink } from '../components/thread/Post';
import { PostTime } from '../components/thread/PostTime';
import { RoleBadge } from '../components/thread/RoleBadge';
import { dateOf, startOf } from '../dates';
import { isPhone } from '../state/layout';
import { useScrollMemory } from '../state/scroll';
import { useShellContext } from '../state/shell';
import { forumsUrl } from './ForumIndex';
import './search.css';

// SearchScreen is /search (TKT-01M3V70X0F, docs/07 "Search"): posts and
// thread titles in the forums the reader may read, newest first, from
// GET /api/v1/search.
//
// The address is the state. It carries the endpoint's own parameters,
// cursor included, so a search is a link and Back returns to the one
// before. The form edits a draft that Search puts in the address, so typing
// fetches nothing (the audit screen's pattern, screens/admin/AuditScreen).
//
// Nothing a search answers is held: the endpoint is no-store and sends no
// ETag, because each answer is judged against what the viewer may read at
// that moment (docs/14 "Invariants"). Back asks again, and useScrollMemory
// puts the reader back once the answer is on screen.

// filterParams are the filters, in the order the address and the request
// spell them, after q.
const filterParams = ['forum', 'author', 'after', 'before'] as const;
const params = ['q', ...filterParams] as const;
type Param = (typeof params)[number];
type Query = Record<Param, string>;

function queryOf(query: Record<string, string>): Query {
  const f = {} as Query;
  for (const p of params) f[p] = query[p] ?? '';
  return f;
}

// searchQuery is the query string for s and cursor, the empty ones left out.
export function searchQuery(s: Partial<Query>, cursor = ''): string {
  const q = new URLSearchParams();
  for (const p of params) if (s[p]) q.set(p, s[p]!);
  if (cursor) q.set('cursor', cursor);
  const str = q.toString();
  return str ? '?' + str : '';
}

const filtersSet = (s: Query) => filterParams.filter((p) => s[p] !== '').length;

// resultHref is where a result goes: the post's permalink, or the thread
// itself for a match on its title (ordinal 0).
export function resultHref(r: Pick<SearchResult, 'thread' | 'ordinal'>): string {
  return r.ordinal > 0
    ? permalink(r.thread, r.ordinal)
    : `/t/${encodeURIComponent(r.thread.id)}/${encodeURIComponent(r.thread.slug)}`;
}

// queryWords is what a reader is told about a refused query. The field
// error codes are docs/07's; these three are the ones the search endpoint
// gives, in its own terms, and anything else reads as the refusal as a whole
// does. forms/messages.ts is not used: src/forms is bundled with the
// application screens (vite.config.ts), and importing it here would load
// that chunk with every page.
const queryWords: Record<string, string> = {
  required: 'Type a word or phrase to search for.',
  too_long: 'Keep a search to 200 characters or fewer.',
  too_many: 'Keep a search to 16 words or fewer.',
};

export function SearchScreen() {
  const { query } = useLocation();
  const asked = queryOf(query);
  const cursor = query.cursor ?? '';
  const key = searchQuery(asked, cursor);
  const url = '/api/v1/search' + key;
  const res = useResource(asked.q ? url : null, (signal) => apiGet<SearchResults>(url, { signal }));
  // Only the answer to the request just made is shown: none while a
  // request is out, and none beside a failure, so nothing on screen was
  // served before the server last checked what the viewer may read
  // (Terva review 1799). useResource's held copy and its data kept beside an
  // error are for other screens.
  const data = asked.q && !res.loading && !res.error ? res.data : undefined;
  useShellContext({ title: 'Search', actions: [] }, []);
  useScrollMemory(key, !!data);

  const qCode = isProblem(res.error, 'validation-failed') ? res.error!.fieldErrors.q : undefined;
  const qError = qCode === undefined ? undefined : (queryWords[qCode] ?? problemMessage(res.error).text);
  const count = data?.results.length ?? 0;
  const badForum = isProblem(res.error, 'not-found') && asked.forum !== '';
  const clear = '/search' + searchQuery({ q: asked.q });
  const filtered = filtersSet(asked) > 0;

  return (
    <main class="search-page" data-screen="search">
      <h1>Search</h1>
      <SearchForm asked={asked} current={key} again={res.retry} qError={qError} />
      <p class="visually-hidden" role="status">
        {data ? (count ? `${count} result${count === 1 ? '' : 's'} on this page` : 'No results') : ''}
      </p>
      {!asked.q && (
        <p class="search-intro" data-search-intro>
          Search finds posts and thread titles in the forums you can read.
        </p>
      )}
      {asked.q && !data && !res.error && (
        <p class="search-muted" data-searching>
          Searching…
        </p>
      )}
      {badForum && (
        <p class="search-empty" data-bad-forum>
          No forum you can see is called “{asked.forum}”. <a href={clear}>Clear filters</a>
        </p>
      )}
      {res.error && !data && !badForum && qError === undefined && (
        <ErrorPanel error={res.error} what="The search" onRetry={res.retry} />
      )}
      {data?.indexing && (
        <p class="search-indexing" data-indexing>
          The search index is still being built, so some posts may be missing.
        </p>
      )}
      {data && data.results.length === 0 && (
        <p class="search-empty" data-no-results>
          {cursor ? 'No more results.' : <>Nothing matches “{asked.q}”.</>}
          {filtered && (
            <>
              {' '}
              <a href={clear} data-clear-filters>
                Clear filters
              </a>
            </>
          )}
        </p>
      )}
      {data && data.results.length > 0 && (
        <ol class="search-results">
          {data.results.map((r) => (
            <Result key={`${r.thread.id}:${r.ordinal}`} result={r} />
          ))}
        </ol>
      )}
      {data && (cursor || data.next_cursor) && (
        <nav class="search-pages" aria-label="Pages">
          {cursor && <a href={'/search' + searchQuery(asked)}>First results</a>}
          {data.next_cursor && (
            <a href={'/search' + searchQuery(asked, data.next_cursor)} rel="next" data-more>
              More results
            </a>
          )}
        </nav>
      )}
    </main>
  );
}

// SearchForm is the query and the filters. It edits a draft, seeded from the
// address and put back there by Search; a new address replaces the draft.
// Search on the address already shown asks the server again (again), since
// routing to it changes nothing: the index may have caught up, or the last
// request failed (Terva review 1799). current is that address's query.
function SearchForm({
  asked,
  current,
  again,
  qError,
}: {
  asked: Query;
  current: string;
  again: () => void;
  qError?: string;
}) {
  const { route } = useLocation();
  const [draft, setDraft] = useState<Query>(asked);
  // The draft follows the address, as Search, Back and a link change it,
  // without remounting the form, so focus stays where it was.
  const address = searchQuery(asked);
  useLayoutEffect(() => setDraft(asked), [address]);
  const input = useRef<HTMLInputElement>(null);
  // With no query the field takes focus: on arrival through the route
  // focuser (data-autofocus, components/focus.ts), and here for a first
  // load or a search cleared in place.
  useLayoutEffect(() => {
    if (!asked.q) input.current?.focus({ preventScroll: true });
  }, [asked.q]);

  const set = (k: Param, v: string) => setDraft((d) => ({ ...d, [k]: v }));
  const text = (e: Event) => (e.target as HTMLInputElement).value;
  const submit = (e: Event) => {
    e.preventDefault();
    const next = searchQuery({ ...draft, q: draft.q.trim() });
    if (next === current) again();
    else route('/search' + next);
  };
  const n = filtersSet(asked);
  const phone = isPhone.value;
  const fields = <Filters draft={draft} set={set} text={text} clear={n > 0 ? searchQuery({ q: asked.q }) : ''} />;

  return (
    <form role="search" class="search-form" onSubmit={submit} aria-label="Search posts and threads">
      <div class="search-query">
        <label for="search-q" class="visually-hidden">
          Search for
        </label>
        <input
          ref={input}
          id="search-q"
          type="search"
          name="q"
          value={draft.q}
          maxLength={200}
          autoComplete="off"
          enterKeyHint="search"
          data-autofocus={asked.q ? undefined : ''}
          aria-invalid={qError ? true : undefined}
          aria-describedby={qError ? 'search-q-error search-q-hint' : 'search-q-hint'}
          onInput={(e) => set('q', text(e))}
        />
        <button type="submit" class="search-submit">
          Search
        </button>
      </div>
      {qError && (
        <p id="search-q-error" class="search-error" role="alert" data-q-error>
          {qError}
        </p>
      )}
      <p id="search-q-hint" class="search-hint">
        Every word must match. Put a phrase in double quotes.
      </p>
      {phone ? (
        <details class="search-filters" open={n > 0}>
          <summary>Filters{n > 0 ? ` (${n} set)` : ''}</summary>
          {fields}
        </details>
      ) : (
        <fieldset class="search-filters">
          <legend>Filters</legend>
          {fields}
        </fieldset>
      )}
    </form>
  );
}

interface FiltersProps {
  draft: Query;
  set: (k: Param, v: string) => void;
  text: (e: Event) => string;
  // clear is the address with the filters dropped, when any is set.
  clear: string;
}

function Filters({ draft, set, text, clear }: FiltersProps) {
  return (
    <div class="search-filter-fields">
      <label class="search-field">
        <span>Forum</span>
        <ForumSelect value={draft.forum} onChange={(v) => set('forum', v)} />
      </label>
      <label class="search-field">
        <span>Author's handle</span>
        <input type="text" name="author" value={draft.author} autoComplete="off" onInput={(e) => set('author', text(e))} />
      </label>
      <label class="search-field">
        <span>From</span>
        <input
          type="date"
          name="after"
          value={draft.after ? dateOf(Number(draft.after)) : ''}
          onInput={(e) => set('after', text(e) ? String(startOf(text(e))) : '')}
        />
      </label>
      <label class="search-field">
        <span>To, inclusive</span>
        <input
          type="date"
          name="before"
          value={draft.before ? dateOf(Number(draft.before) - 1) : ''}
          onInput={(e) => set('before', text(e) ? String(startOf(text(e), 1)) : '')}
        />
      </label>
      {clear && (
        <a class="search-clear" href={'/search' + clear} data-clear-filters>
          Clear filters
        </a>
      )}
    </div>
  );
}

// ForumSelect offers every forum /forums lists for the reader, each root's
// children indented under it, after "All forums". A slug in the address that
// the list lacks is kept as an option of its own, so the field shows the
// filter that is applied rather than "All forums".
function ForumSelect({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const res = useResource(forumsUrl, (signal) => apiGet<ForumTree>(forumsUrl, { signal }), () =>
    peekGet<ForumTree>(forumsUrl),
  );
  const options: [string, string][] = [];
  const add = (f: Forum, depth: number) => {
    options.push([f.slug, '   '.repeat(depth) + f.name]);
    for (const c of f.children) add(c, depth + 1);
  };
  for (const f of res.data?.forums ?? []) add(f, 0);
  const known = value === '' || options.some(([slug]) => slug === value);
  return (
    <select name="forum" value={value} onChange={(e) => onChange((e.target as HTMLSelectElement).value)}>
      <option value="">All forums</option>
      {!known && <option value={value}>{value}</option>}
      {options.map(([slug, label]) => (
        <option key={slug} value={slug}>
          {label}
        </option>
      ))}
    </select>
  );
}

function Result({ result: r }: { result: SearchResult }) {
  return (
    <li class="search-result" data-result={`${r.thread.id}:${r.ordinal}`}>
      <a class="search-title hit" href={resultHref(r)}>
        {r.thread.title}
        {r.ordinal > 0 && <span class="search-ordinal"> #{r.ordinal}</span>}
      </a>
      <div class="search-meta">
        <a href={`/f/${encodeURIComponent(r.forum.slug)}`} data-result-forum>
          {r.forum.name}
        </a>
        <span data-result-author>
          {r.author.handle}
          <RoleBadge author={r.author} />
        </span>
        <PostTime at={r.created_at} />
      </div>
      <Snippet parts={r.snippet} />
    </li>
  );
}

// Snippet draws a snippet as text, each match in a mark element. The parts
// are plain text from the server and are never read as markup: each is a
// text node, so "<script>" is shown as those characters.
export function Snippet({ parts }: { parts: SnippetPart[] }) {
  return (
    <p class="search-snippet">
      {parts.map((p, i) => (p.match ? <mark key={i}>{p.text}</mark> : p.text))}
    </p>
  );
}
