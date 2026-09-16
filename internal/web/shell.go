package web

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/terva-sh/ketju/internal/problem"
)

// ShellData is what the SPA shell template is rendered with.
//
// It is deliberately small. The shell exists to load the client and to carry
// the nonce; anything else the client needs it fetches from the API, where the
// response can be cached and revalidated. Putting state in the shell means
// putting it in a document that must not be cached, which is how a shell grows
// into a second, worse API.
type ShellData struct {
	// Nonce is this response's CSP nonce. A script or style tag in the
	// template must carry it or the browser will refuse to run it.
	Nonce string
}

// DefaultShell is the shell served until the real client lands.
//
// TKT-01M29CCC02 scaffolds the Preact client and replaces this with the built
// index.html. What has to survive that replacement is the mechanism rather
// than the markup: a Go template rendered per response, with .Nonce on every
// script and style tag. That agreement is the reason this ticket ships a shell
// at all instead of leaving SPA nil.
//
// Note there is no inline script and no inline style, which is what lets the
// policy omit 'unsafe-inline'.
const DefaultShell = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ketju</title>
<link rel="stylesheet" href="/assets/app.css">
</head>
<body>
<div id="app"></div>
<script type="module" src="/assets/app.js" nonce="{{.Nonce}}"></script>
</body>
</html>
`

// Shell serves the single-page application shell.
type Shell struct {
	tmpl *template.Template
	log  *slog.Logger
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
	return &Shell{tmpl: t, log: log}, nil
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
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, ShellData{Nonce: nonce}); err != nil {
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
