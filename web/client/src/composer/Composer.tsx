import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../api/client';
import type { Author, RenderResult, UserSuggestions } from '../api/dto';
import { isAbort, problemMessage } from '../api/problem';
import { Icon, type IconName } from '../components/Icon';
import { composerFor, type Composer as ComposerState } from '../state/composer';
import { applyEdit, type EditKind } from './edit';
import { insertMention, mentionAt, type MentionAt } from './mention';
import './composer.css';

// previewDelay is how long the source must sit still before it is rendered
// (AC5): long enough that typing sends nothing, short enough that a pause to
// look reads as live.
export const previewDelay = 400;

// mentionDelay is the same for the handle lookup, shorter because a list
// that lags behind the letters typed is no use.
export const mentionDelay = 150;

const mentionLimit = 8;

// Composer is the reply composer at the foot of a thread (docs/06
// "Composer"; ThreadLight's composer): Write and Preview, the toolbar, the
// reply chip and @handle autocomplete.
//
// The preview is the server's rendering of exactly this source in this
// thread, from POST /render, inserted as it came, the way a post's body is:
// no markdown parser ships to the client, so there is nothing here that
// could render the source differently from the post it becomes.
//
// Posting is not here yet: POST /threads/{id}/posts and its answer are
// TKT-01M29CA9B9's, and a Post button that could only fail is worse than
// none. Nor is saving the draft, which is TKT-01M29CA9CE's; the source
// lasts the session.
export function Composer({ thread, authorOf }: { thread: string; authorOf: (ordinal: number) => string | undefined }) {
  const c = composerFor(thread);
  const source = c.source.value;
  const replyTo = c.replyTo.value;
  const [tab, setTab] = useState<'write' | 'preview'>('write');
  const root = useRef<HTMLElement>(null);
  const area = useRef<HTMLTextAreaElement>(null);
  // The selection to put back once the textarea shows the text an edit
  // made, and whether to focus it: set by an edit, a summons or a chosen
  // mention, applied after the render that carries the new value.
  const pending = useRef<Pending | null>(null);
  const preview = usePreview(thread, source, replyTo);
  const mention = useMentions(area, c, pending);

  // A quote or a reply from a post, or the thread's Reply, summons the
  // composer: into view, on the Write tab, with the caret where it was left
  // or after the quote just inserted. Only a summons made while this
  // composer is on screen moves the reader; mounting does not.
  //
  // Layout effects, here and below, so the textarea has focus before the
  // browser handles another event: with an effect run after paint, keys
  // pressed straight after quote or reply go to the button that was
  // clicked, and a space clicks it again.
  const seen = useRef(c.summoned.value);
  const summoned = c.summoned.value;
  useLayoutEffect(() => {
    if (summoned === seen.current) return;
    seen.current = summoned;
    setTab('write');
    pending.current = { start: c.caret.value, end: c.caret.value, focus: true };
    root.current?.scrollIntoView?.({ block: 'center' });
  }, [summoned]);

  // Declared after the summons, so a summons on the Write tab is applied in
  // the same commit: effects run in order, and setTab('write') on the Write
  // tab renders nothing new to run it later.
  useLayoutEffect(() => {
    const p = pending.current;
    const a = area.current;
    if (!p || !a || tab !== 'write') return;
    pending.current = null;
    if (p.focus) a.focus();
    a.setSelectionRange(p.start, p.end);
    c.caret.value = p.end;
  });

  const edit = (kind: EditKind) => {
    const a = area.current;
    if (!a) return;
    const r = applyEdit({ text: a.value, start: a.selectionStart, end: a.selectionEnd }, kind);
    c.source.value = r.text;
    pending.current = { start: r.start, end: r.end, focus: true };
  };

  const listId = `mentions-${thread}`;
  const open = mention.open;
  return (
    <section class="composer" ref={root} aria-label="Reply" data-composer={thread}>
      <header class="composer-head">
        <h2>Reply</h2>
        {replyTo !== null && (
          <span class="reply-chip" data-reply-chip={replyTo}>
            <Icon name="reply" size={12} />#{replyTo}
            {authorOf(replyTo) && <span class="who">{authorOf(replyTo)}</span>}
            <button type="button" aria-label={`Stop replying to #${replyTo}`} onClick={() => (c.replyTo.value = null)}>
              ×
            </button>
          </span>
        )}
        <div class="composer-tabs" role="tablist" aria-label="Composer view">
          {(['write', 'preview'] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              id={`composer-tab-${t}`}
              aria-selected={tab === t}
              aria-controls={`composer-panel-${t}`}
              onClick={() => setTab(t)}
            >
              {t === 'write' ? 'Write' : 'Preview'}
            </button>
          ))}
        </div>
      </header>
      {tab === 'write' && (
        <div class="composer-tools" role="toolbar" aria-label="Formatting">
          {tools.map(([kind, icon, label]) => (
            <button key={kind} type="button" aria-label={label} title={label} data-edit={kind} onClick={() => edit(kind)}>
              <Icon name={icon} />
            </button>
          ))}
        </div>
      )}
      <div class="composer-body" id="composer-panel-write" role="tabpanel" aria-labelledby="composer-tab-write" hidden={tab !== 'write'}>
        <textarea
          ref={area}
          aria-label="Reply text"
          placeholder="Write a reply. Markdown works."
          value={source}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={open ? listId : undefined}
          aria-activedescendant={open ? `${listId}-${mention.active}` : undefined}
          onInput={(e) => {
            c.source.value = e.currentTarget.value;
            c.caret.value = e.currentTarget.selectionStart;
            mention.check();
          }}
          onKeyDown={(e) => mention.key(e)}
          onKeyUp={(e) => {
            c.caret.value = e.currentTarget.selectionStart;
            mention.check();
          }}
          onClick={(e) => {
            c.caret.value = e.currentTarget.selectionStart;
            mention.check();
          }}
          onBlur={() => mention.close()}
        />
        {open && (
          <ul class="mention-list" id={listId} role="listbox" aria-label="People">
            {mention.users.map((u, i) => (
              <li
                key={u.handle}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === mention.active}
                data-handle={u.handle}
                // mousedown, not click: a click would blur the textarea
                // first, which closes the list before the click lands.
                onMouseDown={(e) => {
                  e.preventDefault();
                  mention.choose(u);
                }}
              >
                <span class="handle">@{u.handle}</span>
                {u.display_name && <span class="name">{u.display_name}</span>}
              </li>
            ))}
          </ul>
        )}
      </div>
      <div
        class="composer-preview"
        id="composer-panel-preview"
        role="tabpanel"
        aria-labelledby="composer-tab-preview"
        aria-busy={preview.busy}
        hidden={tab !== 'preview'}
      >
        {preview.error !== null ? (
          <p class="preview-error" role="alert">
            {problemMessage(preview.error).text}
          </p>
        ) : preview.html === '' && !preview.busy ? (
          <p class="preview-empty">Nothing to preview yet.</p>
        ) : (
          <div class="post-body" data-preview dangerouslySetInnerHTML={{ __html: preview.html }} />
        )}
      </div>
      <footer class="composer-foot">
        <span>Markdown supported</span>
      </footer>
    </section>
  );
}

interface Pending {
  start: number;
  end: number;
  focus: boolean;
}

const tools: [EditKind, IconName, string][] = [
  ['bold', 'bold', 'Bold'],
  ['italic', 'italic', 'Italic'],
  ['quote', 'quote', 'Quote'],
  ['code', 'code', 'Code'],
  ['link', 'link', 'Link'],
  ['mention', 'at', 'Mention someone'],
];

// usePreview renders source through POST /render once it has sat still for
// previewDelay, in this thread and as a reply to replyTo, so quotes and the
// reply are checked as the post's will be. A change before the answer comes
// cancels the request, so only the newest source's rendering is ever shown.
// Empty source asks nothing.
function usePreview(thread: string, source: string, replyTo: number | null) {
  const [state, setState] = useState<{ html: string; error: unknown; busy: boolean }>({ html: '', error: null, busy: false });
  useEffect(() => {
    if (source.trim() === '') {
      setState({ html: '', error: null, busy: false });
      return;
    }
    setState((s) => ({ ...s, busy: true }));
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      apiSend<RenderResult>('POST', '/api/v1/render', { source, thread_id: thread, reply_to: replyTo }, { signal: ctrl.signal }).then(
        (r) => {
          if (!ctrl.signal.aborted) setState({ html: r.html, error: null, busy: false });
        },
        (err) => {
          if (!ctrl.signal.aborted && !isAbort(err)) setState({ html: '', error: err, busy: false });
        },
      );
    }, previewDelay);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [thread, source, replyTo]);
  return state;
}

// useMentions is the @handle autocomplete: which mention the caret is at the
// end of, the people GET /users offers for it, and the keys that pick one.
// A lookup that fails offers nobody; autocomplete is a convenience, and the
// author can still type the handle.
function useMentions(
  area: { current: HTMLTextAreaElement | null },
  c: ComposerState,
  pending: { current: Pending | null },
) {
  const [at, setAt] = useState<MentionAt | null>(null);
  const [users, setUsers] = useState<Author[]>([]);
  const [active, setActive] = useState(0);
  // The mention the author pressed Escape on stays closed until they type.
  const [dismissed, setDismissed] = useState<string | null>(null);
  const key = at ? `${at.start}:${at.typed}` : null;

  useEffect(() => {
    setUsers([]);
    if (!at) return;
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      const url = `/api/v1/users?prefix=${encodeURIComponent(at.typed)}&limit=${mentionLimit}`;
      apiGet<UserSuggestions>(url, { signal: ctrl.signal }).then(
        (r) => {
          if (ctrl.signal.aborted) return;
          setUsers(r.users);
          setActive(0);
        },
        () => {},
      );
    }, mentionDelay);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [key]);

  const open = at !== null && users.length > 0 && dismissed !== key;

  const check = () => {
    const a = area.current;
    if (!a) return;
    const next = a.selectionStart === a.selectionEnd ? mentionAt(a.value, a.selectionStart) : null;
    setAt((prev) => (prev && next && prev.start === next.start && prev.typed === next.typed ? prev : next));
  };

  const choose = (u: Author) => {
    const a = area.current;
    if (!a || !at) return;
    const r = insertMention(a.value, at, a.selectionStart, u.handle);
    c.source.value = r.text;
    pending.current = { start: r.caret, end: r.caret, focus: true };
    setAt(null);
    setUsers([]);
  };

  const close = () => setDismissed(key);

  return {
    open,
    users,
    active,
    check,
    close,
    choose,
    key: (e: KeyboardEvent) => {
      if (!open) return;
      switch (e.key) {
        case 'ArrowDown':
          setActive((active + 1) % users.length);
          break;
        case 'ArrowUp':
          setActive((active - 1 + users.length) % users.length);
          break;
        case 'Enter':
        case 'Tab':
          choose(users[active]);
          break;
        case 'Escape':
          close();
          break;
        default:
          return;
      }
      e.preventDefault();
    },
  };
}
