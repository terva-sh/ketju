package web

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/terva-sh/ketju/internal/dto"
	"github.com/terva-sh/ketju/internal/problem"
)

// ShellData is what the SPA shell template is rendered with: the built
// index.html from internal/web/spa (docs/07 "Embedding").
//
// docs/07 puts three things in the shell besides the nonce: the current
// page's title and description, for link unfurls, and a bootstrap /me block
// so the first paint needs no round trip. Everything else the client fetches
// from the API, where a response can be revalidated; the shell is never
// cached, so what goes in it is paid for on every load.
type ShellData struct {
	// Nonce is this response's CSP nonce. A script or style tag in the
	// template must carry it or the browser will refuse to run it.
	Nonce string
	// Title and Description are the <title> and meta description.
	Title       string
	Description string
	// Bootstrap is the viewer, rendered by html/template into a
	// <script type="application/json"> block. That element is a JavaScript
	// context to html/template, so the value is marshalled to JSON with <, >
	// and & escaped, and cannot close the element or open another.
	Bootstrap dto.Me
}

// Page is what a request's shell says beyond the nonce.
type Page struct {
	Title       string
	Description string
	Bootstrap   dto.Me
}

// defaultPage is served until the server supplies its own: the product's
// name and an anonymous viewer. The site_name setting and a real /me arrive
// with ketju serve (TKT-01M29CCBYX) and the session (TKT-01M29CA99R).
func defaultPage(*http.Request) (Page, error) {
	return Page{Title: "ketju", Bootstrap: dto.Me{Settings: dto.Settings{PageSize: 25}}}, nil
}

// Shell serves the single-page application shell.
type Shell struct {
	tmpl *template.Template
	log  *slog.Logger
	page func(*http.Request) (Page, error)
}

// NewShell parses a shell template.
//
// html/template rather than text/template because the nonce is interpolated
// into an attribute, and contextual escaping is what keeps a template edit
// from turning into an injection point. It also means a template that tries to
// build a script body out of data fails loudly here rather than quietly in a
// browser.
func NewShell(tmpl string, log *slog.Logger) (*Shell, error) {
	t, err := template.New("shell").Parse(tmpl)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &Shell{tmpl: t, log: log, page: defaultPage}, nil
}

// SetPage sets what each request's shell carries: its title, description and
// bootstrap viewer. An error from fn is a 500, not a shell with a guessed
// viewer in it.
func (s *Shell) SetPage(fn func(*http.Request) (Page, error)) {
	s.page = fn
}

// ServeHTTP renders the shell with this response's nonce.
func (s *Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only GET and HEAD reach the shell. A POST to an application route is a
	// client bug, and answering it with 200 and a page would hide it.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		problem.Write(w, r, problem.CodeMethodNotAllowed)
		return
	}

	nonce, ok := NonceFrom(r.Context())
	if !ok {
		// The shell is unreachable without securityHeaders in front of it, so
		// this means the chain was assembled wrong. Serving a page whose
		// scripts the policy will block is worse than saying so: the reader
		// would get a blank screen and a console error, and the cause would be
		// two packages away.
		s.log.ErrorContext(r.Context(), "the shell was reached with no CSP nonce in context; "+
			"securityHeaders is not in front of it, so every script on this page would be blocked")
		problem.Write(w, r, problem.CodeInternal)
		return
	}

	// Rendered into a buffer first. A template that fails halfway through has
	// already written a partial page if it renders straight to the
	// ResponseWriter, and a partial page arrives with a 200 nobody can take
	// back.
	page, err := s.page(r)
	if err != nil {
		s.log.ErrorContext(r.Context(), "building the SPA shell's page failed", "error", err)
		problem.Write(w, r, problem.CodeInternal)
		return
	}
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, ShellData{Nonce: nonce, Title: page.Title,
		Description: page.Description, Bootstrap: page.Bootstrap}); err != nil {
		s.log.ErrorContext(r.Context(), "rendering the SPA shell failed", "error", err)
		problem.Write(w, r, problem.CodeInternal)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// The shell carries a per-response nonce, so a cached copy would hand a
	// stale nonce to a later reader and the policy would block every script on
	// the page. The assets it references are the cacheable part.
	h.Set("Cache-Control", "no-store")

	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(buf.Bytes())
}
