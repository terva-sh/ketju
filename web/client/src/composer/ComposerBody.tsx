import type { Signal } from '@preact/signals';
import type { ComponentChildren } from 'preact';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { apiGet, apiSend } from '../api/client';
import type { Author, RenderResult, UserSuggestions } from '../api/dto';
import { isAbort, problemMessage } from '../api/problem';
import { Icon, type IconName } from '../components/Icon';
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

// ComposerFields is what the composer body edits: a reply's composer
// (state/composer.ts) and a new thread's (state/newThread.ts) both have them.
export interface ComposerFields {
  source: Signal<string>;
  // caret is where the textarea's caret last was, where a quote goes.
  caret: Signal<number>;
  // summoned counts the requests to bring the composer into view and put the
  // caret at caret. The composer acts on each change.
  summoned: Signal<number>;
  // posting freezes the source while a post of it is in flight.
  posting: Signal<boolean>;
}

// ComposerBody is the composer every writing screen shares (docs/06
// "Composer": the same everywhere): Write and Preview, the formatting
// toolbar, the textarea with @handle autocomplete, and the server's preview.
// The reply composer (Composer) and the new-thread screen
// (screens/NewThreadScreen) each put their own head, notice and footer
// around it, and their own Post button in the footer.
//
// The preview is the server's rendering of exactly this source, in thread
// when one is given, from POST /render, inserted as it came, the way a post's
// body is: no markdown parser ships to the client, so there is nothing here
// that could render the source differently from the post it becomes.
//
// sheet lays it out as the column that fills the phone's composer sheet,
// toolbar last. focus puts the caret in the textarea when it mounts, which
// the reply sheet asks for, since it opens because someone asked to write.
//
// editing names the post an author's editor is open on (PostEditor,
// TKT-01M3PTT97X). The editor can be on the page beside the reply composer,
// so its tabs and panels take ids of their own, and it is marked
// data-editing rather than data-composer, which names the reply composer.
export function ComposerBody({
  state: c,
  thread,
  replyTo = null,
  name,
  label,
  placeholder,
  sheet = false,
  focus = false,
  invalid = false,
  head,
  notice,
  foot,
  editing,
}: {
  state: ComposerFields;
  thread?: string;
  replyTo?: number | null;
  name: string;
  label: string;
  placeholder: string;
  sheet?: boolean;
  focus?: boolean;
  // invalid marks the textarea as the field a refusal named.
  invalid?: boolean;
  head?: ComponentChildren;
  notice?: ComponentChildren;
  foot?: ComponentChildren;
  editing?: number;
}) {
  const source = c.source.value;
  const posting = c.posting.value;
  const [tab, setTab] = useState<'write' | 'preview'>('write');
  const root = useRef<HTMLElement>(null);
  const area = useRef<HTMLTextAreaElement>(null);
  // The selection to put back once the textarea shows the text an edit
  // made, and whether to focus it: set by an edit, a summons or a chosen
  // mention, applied after the render that carries the new value.
  const pending = useRef<Pending | null>(focus ? { start: c.caret.value, end: c.caret.value, focus: true } : null);
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

  // Nothing changes the source while a post is in flight: a successful post
  // clears what it sent, so an edit made meanwhile would be lost (reviews on
  // PR #81). The textarea is read-only and ignores input then, the toolbar
  // is off, and the mention list, which closes when the textarea loses
  // focus to Post, cannot be open.
  const edit = (kind: EditKind) => {
    const a = area.current;
    if (!a) return;
    const r = applyEdit({ text: a.value, start: a.selectionStart, end: a.selectionEnd }, kind);
    c.source.value = r.text;
    pending.current = { start: r.start, end: r.end, focus: true };
  };

  const listId = `mentions${useId()}`;
  const open = mention.open;
  const ids = editing === undefined ? 'composer' : `edit-${editing}`;
  return (
    <section
      class={sheet ? 'composer in-sheet' : 'composer'}
      ref={root}
      aria-label={name}
      data-composer={editing === undefined ? thread : undefined}
      data-editing={editing}
    >
      <header class="composer-head">
        {head}
        <div class="composer-tabs" role="tablist" aria-label="Composer view">
          {(['write', 'preview'] as const).map((t) => (
            <button
              key={t}
              type="button"
              role="tab"
              id={`${ids}-tab-${t}`}
              aria-selected={tab === t}
              aria-controls={`${ids}-panel-${t}`}
              onClick={() => setTab(t)}
            >
              {t === 'write' ? 'Write' : 'Preview'}
            </button>
          ))}
        </div>
      </header>
      {notice}
      {tab === 'write' && (
        <div class="composer-tools" role="toolbar" aria-label="Formatting">
          {tools.map(([kind, icon, label]) => (
            <button key={kind} type="button" aria-label={label} title={label} data-edit={kind} disabled={posting} onClick={() => edit(kind)}>
              <Icon name={icon} />
            </button>
          ))}
        </div>
      )}
      <div class="composer-body" id={`${ids}-panel-write`} role="tabpanel" aria-labelledby={`${ids}-tab-write`} hidden={tab !== 'write'}>
        <textarea
          ref={area}
          aria-label={label}
          aria-invalid={invalid || undefined}
          placeholder={placeholder}
          value={source}
          readOnly={posting}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={open ? listId : undefined}
          aria-activedescendant={open ? `${listId}-${mention.active}` : undefined}
          onInput={(e) => {
            if (c.posting.value) return;
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
        id={`${ids}-panel-preview`}
        role="tabpanel"
        aria-labelledby={`${ids}-tab-preview`}
        aria-busy={preview.busy}
        hidden={tab !== 'preview'}
      >
        {preview.busy ? (
          <p class="preview-busy">Rendering…</p>
        ) : preview.error !== null ? (
          <p class="preview-error" role="alert">
            {problemMessage(preview.error).text}
          </p>
        ) : preview.empty ? (
          <p class="preview-empty">Nothing to preview yet.</p>
        ) : (
          <div class="post-body" data-preview dangerouslySetInnerHTML={{ __html: preview.html }} />
        )}
      </div>
      {foot}
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
// previewDelay: in thread and as a reply to replyTo when a thread is given,
// so quotes and the reply are checked as the post's will be, and otherwise
// as the first post of a thread not yet made, whose quotes render
// optimistically (docs/07). A change before the answer comes cancels the
// request.
//
// Each answer is held with the input it answers, and is shown only while
// that is still the input: the check is made as the pane renders, not by an
// effect after it, so there is no render, however brief, in which an answer
// or an error about other input is on screen (reviews 537, 541, 543).
// Empty source asks nothing and shows nothing.
export function usePreview(thread: string | undefined, source: string, replyTo: number | null) {
  const key = JSON.stringify([thread ?? null, source, replyTo]);
  const [held, setHeld] = useState<{ key: string; html: string; error: unknown } | null>(null);
  const empty = source.trim() === '';
  useEffect(() => {
    if (empty) return;
    const ctrl = new AbortController();
    const body = thread === undefined ? { source } : { source, thread_id: thread, reply_to: replyTo };
    const timer = setTimeout(() => {
      apiSend<RenderResult>('POST', '/api/v1/render', body, { signal: ctrl.signal }).then(
        // An answer for input that has since changed is dropped rather than
        // held: holding it would replace the newer answer it lost the race
        // to, and it could never be shown anyway.
        (r) => {
          if (!ctrl.signal.aborted) setHeld({ key, html: r.html, error: null });
        },
        (err) => {
          if (!ctrl.signal.aborted && !isAbort(err)) setHeld({ key, html: '', error: err });
        },
      );
    }, previewDelay);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [key]);
  const answer = !empty && held?.key === key ? held : null;
  return { empty, busy: !empty && answer === null, html: answer?.html ?? '', error: answer?.error ?? null };
}

// useMentions is the @handle autocomplete: which mention the caret is at the
// end of, the people GET /users offers for it, and the keys that pick one.
// A lookup that fails offers nobody; autocomplete is a convenience, and the
// author can still type the handle.
function useMentions(
  area: { current: HTMLTextAreaElement | null },
  c: Pick<ComposerFields, 'source'>,
  pending: { current: Pending | null },
) {
  const [at, setAt] = useState<MentionAt | null>(null);
  // The people offered, held with the mention they were offered for, as the
  // preview holds its answer: the list is shown only while that is still
  // the mention being typed, checked as it renders, so no render offers
  // people for a prefix already changed (review 547).
  const [held, setHeld] = useState<{ key: string; users: Author[] } | null>(null);
  const [active, setActive] = useState(0);
  // The mention the author pressed Escape on stays closed until they type.
  const [dismissed, setDismissed] = useState<string | null>(null);
  const key = at ? `${at.start}:${at.prefix}` : null;

  useEffect(() => {
    if (!at || key === null) return;
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      const url = `/api/v1/users?prefix=${encodeURIComponent(at.prefix)}&limit=${mentionLimit}`;
      apiGet<UserSuggestions>(url, { signal: ctrl.signal }).then(
        (r) => {
          if (ctrl.signal.aborted) return;
          setHeld({ key, users: r.users });
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

  const users = key !== null && held?.key === key ? held.users : [];
  const open = users.length > 0 && dismissed !== key;

  const check = () => {
    const a = area.current;
    if (!a) return;
    const next = a.selectionStart === a.selectionEnd ? mentionAt(a.value, a.selectionStart) : null;
    // Escape closes the list for the mention it was pressed on; typing on
    // to another mention, even one that comes back to the same letters,
    // lets it open again (review 557).
    if ((next ? `${next.start}:${next.prefix}` : null) !== key) setDismissed(null);
    // A new object only for a new mention, so an unchanged one asks nothing
    // again; typed is compared too, since insertion replaces all of it.
    setAt((prev) => (prev && next && prev.start === next.start && prev.typed === next.typed ? prev : next));
  };

  const choose = (u: Author) => {
    const a = area.current;
    if (!a || !at) return;
    const r = insertMention(a.value, at, a.selectionStart, u.handle);
    c.source.value = r.text;
    pending.current = { start: r.caret, end: r.caret, focus: true };
    setAt(null);
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
