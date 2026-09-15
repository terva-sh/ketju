package render

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// mustRender renders or fails.
func mustRender(t *testing.T, src string) string {
	t.Helper()
	r, err := Render(src)
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return r.HTML
}

// TestScriptTagsAreStripped is the first of the three the ticket names.
//
// Every case is a real way people have shipped an XSS through a markdown
// renderer, which is why they are enumerated rather than covered by one
// representative example: the interesting property of this class of bug is
// that each variant defeats a different partial defence.
func TestScriptTagsAreStripped(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"plain":            `<script>alert(1)</script>`,
		"with attributes":  `<script type="text/javascript" src="https://evil.example/x.js"></script>`,
		"uppercase":        `<SCRIPT>alert(1)</SCRIPT>`,
		"mixed case":       `<ScRiPt>alert(1)</ScRiPt>`,
		"split by newline": "<script\n>alert(1)</script>",
		"inside a list":    "- item\n- <script>alert(1)</script>",
		"inside a quote":   "> <script>alert(1)</script>",
		"inside a table":   "| a |\n|---|\n| <script>alert(1)</script> |",
		"after text":       "Hello <script>alert(1)</script> world",
		"style block":      `<style>body{display:none}</style>`,
		"noscript":         `<noscript><p>x</p></noscript>`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertSafe(t, src, mustRender(t, src))
		})
	}
}

// TestEventHandlerAttributesAreStripped is the second.
func TestEventHandlerAttributesAreStripped(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"img onerror":       `<img src=x onerror="alert(1)">`,
		"body onload":       `<body onload="alert(1)">`,
		"svg onload":        `<svg onload="alert(1)"></svg>`,
		"div onmouseover":   `<div onmouseover="alert(1)">hover</div>`,
		"a onclick":         `<a href="#" onclick="alert(1)">click</a>`,
		"input onfocus":     `<input autofocus onfocus="alert(1)">`,
		"details ontoggle":  `<details ontoggle="alert(1)"><summary>s</summary></details>`,
		"iframe onload":     `<iframe src="about:blank" onload="alert(1)"></iframe>`,
		"unquoted handler":  `<img src=x onerror=alert(1)>`,
		"handler in a link": `[text](https://example.org" onmouseover="alert(1))`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertSafe(t, src, mustRender(t, src))
		})
	}
}

// TestJavascriptURLsAreStripped is the third.
//
// The encoded forms matter as much as the plain one. A filter that compares
// against the literal string "javascript:" is defeated by every entry below
// it, and the reason to allowlist schemes rather than blocklist them is that
// this list has no end.
func TestJavascriptURLsAreStripped(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"plain":            `[x](javascript:alert(1))`,
		"uppercase":        `[x](JAVASCRIPT:alert(1))`,
		"mixed case":       `[x](JaVaScRiPt:alert(1))`,
		"leading space":    `[x](  javascript:alert(1))`,
		"embedded tab":     "[x](java\tscript:alert(1))",
		"entity encoded":   `[x](java&#115;cript:alert(1))`,
		"entity colon":     `[x](javascript&#58;alert(1))`,
		"newline inside":   "[x](java\nscript:alert(1))",
		"image source":     `![x](javascript:alert(1))`,
		"autolink":         `<javascript:alert(1)>`,
		"reference link":   "[x][r]\n\n[r]: javascript:alert(1)",
		"vbscript":         `[x](vbscript:msgbox(1))`,
		"data html":        `[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
		"data svg":         `![x](data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9ImFsZXJ0KDEpIj48L3N2Zz4=)`,
		"file scheme":      `[x](file:///etc/passwd)`,
		"raw anchor":       `<a href="javascript:alert(1)">x</a>`,
		"raw image source": `<img src="javascript:alert(1)">`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := mustRender(t, src)
			assertSafe(t, src, got)
		})
	}
}

// TestRawHTMLNeverReachesTheOutput covers the elements that are not scripts
// but are still somebody else's code running in this page.
func TestRawHTMLNeverReachesTheOutput(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"<iframe", "<object", "<embed", "<form", "<base", "<link", "<meta",
		"<style", "<template", "<math", "<svg", "<frame", "<applet", "<marquee",
	}
	sources := []string{
		`<iframe src="https://evil.example"></iframe>`,
		`<object data="https://evil.example/x.swf"></object>`,
		`<embed src="https://evil.example/x.swf">`,
		`<form action="https://evil.example"><input name="p" type="password"></form>`,
		`<base href="https://evil.example/">`,
		`<link rel="stylesheet" href="https://evil.example/x.css">`,
		`<meta http-equiv="refresh" content="0;url=https://evil.example">`,
		`<template><p>x</p></template>`,
		`<math><mtext><script>alert(1)</script></mtext></math>`,
		`<svg><use href="#x"/></svg>`,
		"<!-- a comment -->",
		"<!--[if IE]><script>alert(1)</script><![endif]-->",
		"<div><p>nested raw</p></div>",
	}
	for _, src := range sources {
		out := mustRender(t, src)
		lower := strings.ToLower(out)
		for _, tag := range forbidden {
			if strings.Contains(lower, tag) {
				t.Errorf("%s survived\n source: %s\n output: %s", tag, src, out)
			}
		}
		assertSafe(t, src, out)
	}
}

// TestTheAllowlistIsAnAllowlist: the point of building from NewPolicy rather
// than UGCPolicy is that an element nobody added is dropped. If this starts
// failing, somebody widened the policy, and the question is whether they meant
// to.
func TestTheAllowlistIsAnAllowlist(t *testing.T) {
	t.Parallel()

	// None of these are produced by the configured pipeline, so none of them
	// should survive the sanitizer if it were somehow handed one.
	for _, h := range []string{
		`<article>x</article>`,
		`<aside>x</aside>`,
		`<details open><summary>s</summary>x</details>`,
		`<abbr title="t">x</abbr>`,
		`<span class="x">x</span>`,
		`<font color="red">x</font>`,
		`<audio controls src="https://x.example/a.mp3"></audio>`,
		`<video controls src="https://x.example/v.mp4"></video>`,
	} {
		got := sanitize(h)
		if strings.Contains(got, "<") {
			t.Errorf("sanitize(%q) kept markup: %q", h, got)
		}
	}
}

// TestCheckboxesCarryNothingSubmittable: <input> is allowed so task lists
// render, and this is the assertion that keeps that narrow.
func TestCheckboxesCarryNothingSubmittable(t *testing.T) {
	t.Parallel()

	got := mustRender(t, "- [x] done")
	if !strings.Contains(got, `type="checkbox"`) {
		t.Fatalf("the task list checkbox did not survive: %s", got)
	}
	for _, bad := range []string{
		`<input type="text"`, `<input type="hidden"`, "name=", "value=", "form=", "formaction=",
	} {
		if strings.Contains(got, bad) {
			t.Errorf("%s appears in a task list: %s", bad, got)
		}
	}
	// And an input the pipeline did not produce is dropped outright.
	if out := sanitize(`<input type="text" name="p" value="v">`); strings.Contains(out, "<input") {
		t.Errorf("a text input survived the sanitizer: %q", out)
	}
}

// TestFootnoteIDsCannotBeChosenFreely: ids are allowed so the two ends of a
// footnote can find each other, and the pattern is what stops a footnote label
// from minting an id that collides with the page's own anchors.
func TestFootnoteIDsCannotBeChosenFreely(t *testing.T) {
	t.Parallel()

	got := mustRender(t, "Text[^a].\n\n[^a]: note")
	if !strings.Contains(got, `id="fn:1"`) && !strings.Contains(got, `id="fn:a"`) {
		t.Fatalf("the footnote lost its id: %s", got)
	}
	for _, h := range []string{
		`<li id="main-content">x</li>`,
		`<sup id="composer">x</sup>`,
		`<li id="fn:has spaces">x</li>`,
		`<div id="anything">x</div>`,
	} {
		if out := sanitize(h); strings.Contains(out, "id=") {
			t.Errorf("sanitize(%q) kept an id: %q", h, out)
		}
	}
}

// TestLinksGetNofollow: every link in user content is untrusted, and the forum
// should not be lending its reputation to whatever somebody pasted.
func TestLinksGetNofollow(t *testing.T) {
	t.Parallel()

	got := mustRender(t, "[x](https://example.org)")
	if !strings.Contains(got, `rel="nofollow"`) {
		t.Errorf("a link has no rel=nofollow: %s", got)
	}
}

// TestCodeKeepsOnlyALanguageClass: highlighting needs the class, and the class
// comes from the info string, which is whatever the author typed.
func TestCodeKeepsOnlyALanguageClass(t *testing.T) {
	t.Parallel()

	got := mustRender(t, "```go\nx := 1\n```")
	if !strings.Contains(got, `class="language-go"`) {
		t.Errorf("the language class was dropped: %s", got)
	}
	for _, h := range []string{
		`<code class="hidden">x</code>`,
		`<code class="language-with a space">x</code>`,
		`<p class="language-go">x</p>`,
	} {
		if out := sanitize(h); strings.Contains(out, "class=") {
			t.Errorf("sanitize(%q) kept a class: %q", h, out)
		}
	}
}

// assertSafe parses the output and asserts on its structure rather than on
// its text.
//
// Substring matching was the first attempt and it was wrong twice in the same
// way: `<javascript:alert(1)>` and a link with a quote in it both end up as
// escaped text in a paragraph, which is the correct and safe result, and a
// grep for "javascript:" calls both of them failures. What matters is not
// whether a dangerous string appears anywhere in the output but whether it
// appears anywhere a browser would act on it -- an element, or an attribute.
// So: parse, walk, and check the three things a browser acts on.
func assertSafe(t *testing.T, src, out string) {
	t.Helper()

	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("the output does not parse as HTML: %v\n%s", err, out)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if !allowedElements[n.Data] {
				t.Errorf("element <%s> survived\n source: %s\n output: %s", n.Data, src, out)
			}
			for _, a := range n.Attr {
				name := strings.ToLower(a.Key)
				if strings.HasPrefix(name, "on") {
					t.Errorf("event handler %s=%q survived\n source: %s\n output: %s", name, a.Val, src, out)
				}
				if name == "style" {
					t.Errorf("a style attribute survived\n source: %s\n output: %s", src, out)
				}
				if urlAttrs[name] {
					assertSafeURL(t, src, out, name, a.Val)
				}
			}
		}
		if n.Type == html.CommentNode {
			t.Errorf("a comment survived\n source: %s\n output: %s", src, out)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
}

func assertSafeURL(t *testing.T, src, out, attr, raw string) {
	t.Helper()
	// Strip the characters a browser ignores before parsing a URL, which is
	// the trick behind "java\tscript:" and " javascript:".
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f', 0:
			return -1
		}
		return r
	}, raw)

	u, err := url.Parse(cleaned)
	if err != nil {
		// An unparseable URL is not a scheme a browser will dispatch on.
		return
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto":
	default:
		t.Errorf("%s=%q carries the %q scheme\n source: %s\n output: %s",
			attr, raw, u.Scheme, src, out)
	}
}

// allowedElements is the sanitizer's element list, plus the three the HTML
// parser inserts around a fragment.
var allowedElements = map[string]bool{
	"html": true, "head": true, "body": true,

	"p": true, "br": true, "hr": true, "blockquote": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"em": true, "strong": true, "del": true,
	"pre": true, "code": true,
	"a": true, "img": true,
	"ul": true, "ol": true, "li": true, "input": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true,
	"tr": true, "th": true, "td": true, "caption": true,
	"sup": true, "div": true, "section": true,
}

var urlAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true,
	"data": true, "srcset": true, "poster": true, "background": true,
	"xlink:href": true,
}
