package render

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const testBlob = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// elements parses rendered HTML and returns every element named tag, in
// document order. Asserting on the parsed tree rather than on substrings is
// what assertSafe does too: an escaped "<img" in text is text.
func elements(t *testing.T, out, tag string) []*html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("the output does not parse as HTML: %v\n%s", err, out)
	}
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			found = append(found, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// TestRemoteImageIsNeverAnImg is the first criterion. The CSP would show any
// of these as a broken image; each must come out as a link to its URL, with
// the alt text, or the URL when there is none, as the link's text.
func TestRemoteImageIsNeverAnImg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, src, href, text string
	}{
		{"http", `![a photo](http://example.org/a.png)`, "http://example.org/a.png", "a photo"},
		{"https", `![a photo](https://example.org/a.png)`, "https://example.org/a.png", "a photo"},
		{"upper case scheme", `![a photo](HTTPS://example.org/a.png)`, "https://example.org/a.png", "a photo"},
		{"mixed case scheme", `![a photo](hTtP://example.org/a.png)`, "http://example.org/a.png", "a photo"},
		{"protocol relative", `![a photo](//example.org/a.png)`, "//example.org/a.png", "a photo"},
		{"backslash host", `![a photo](/\example.org/a.png)`, "/%5Cexample.org/a.png", "a photo"},
		{"relative path", `![a photo](a.png)`, "a.png", "a photo"},
		{"no alt", `![](https://example.org/a.png)`, "https://example.org/a.png", "https://example.org/a.png"},
		{"blank alt", `![  ](https://example.org/a.png)`, "https://example.org/a.png", "https://example.org/a.png"},
		{"reference", "![a photo][p]\n\n[p]: https://example.org/a.png", "https://example.org/a.png", "a photo"},
		{"angle brackets", `![a photo](<https://example.org/a b.png>)`, "https://example.org/a%20b.png", "a photo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out := mustRender(t, c.src)
			assertSafe(t, c.src, out)
			if imgs := elements(t, out, "img"); len(imgs) != 0 {
				t.Fatalf("a remote image rendered as an img: %s", out)
			}
			links := elements(t, out, "a")
			if len(links) != 1 {
				t.Fatalf("want one link, got %d: %s", len(links), out)
			}
			if href, _ := attr(links[0], "href"); href != c.href {
				t.Errorf("href = %q, want %q: %s", href, c.href, out)
			}
			if got := textOf(links[0]); got != c.text {
				t.Errorf("link text = %q, want %q: %s", got, c.text, out)
			}
		})
	}
}

// TestAttachmentImageStaysAnImg is the second criterion: an attachment is the
// one image a post may show, and anything that only resembles one is a link.
func TestAttachmentImageStaysAnImg(t *testing.T) {
	t.Parallel()

	out := mustRender(t, "![a cat](/blobs/"+testBlob+` "the title")`)
	imgs := elements(t, out, "img")
	if len(imgs) != 1 {
		t.Fatalf("want one img, got %d: %s", len(imgs), out)
	}
	for k, want := range map[string]string{
		"src": "/blobs/" + testBlob, "alt": "a cat", "title": "the title",
	} {
		if got, _ := attr(imgs[0], k); got != want {
			t.Errorf("%s = %q, want %q: %s", k, got, want, out)
		}
	}
	if links := elements(t, out, "a"); len(links) != 0 {
		t.Errorf("an attachment image became a link: %s", out)
	}

	nearMisses := map[string]string{
		"upper case hash":   "/blobs/" + strings.ToUpper(testBlob),
		"short hash":        "/blobs/" + testBlob[:63],
		"long hash":         "/blobs/" + testBlob + "0",
		"suffix":            "/blobs/" + testBlob + "/x",
		"query":             "/blobs/" + testBlob + "?x=1",
		"fragment":          "/blobs/" + testBlob + "#x",
		"absolute":          "https://example.org/blobs/" + testBlob,
		"protocol relative": "//example.org/blobs/" + testBlob,
		"no leading slash":  "blobs/" + testBlob,
		"dot segment":       "/blobs/../blobs/" + testBlob,
		"non hex":           "/blobs/" + strings.Repeat("g", 64),
	}
	for name, src := range nearMisses {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := mustRender(t, "![x]("+src+")")
			assertSafe(t, src, out)
			if imgs := elements(t, out, "img"); len(imgs) != 0 {
				t.Fatalf("%q rendered as an img: %s", src, out)
			}
			if links := elements(t, out, "a"); len(links) != 1 {
				t.Fatalf("%q did not render as one link: %s", src, out)
			}
		})
	}
}

// TestNestedAndUnsafeImages is the third criterion.
func TestNestedAndUnsafeImages(t *testing.T) {
	t.Parallel()

	t.Run("inside a link", func(t *testing.T) {
		t.Parallel()
		for src, want := range map[string]string{
			"[see ![a photo](https://example.org/a.png)](https://example.org/page)": "see a photo",
			"[see ![](https://example.org/a.png)](https://example.org/page)":        "see https://example.org/a.png",
			"[see ![bad](javascript:alert(1))](https://example.org/page)":           "see bad",
			"[![a](https://example.org/1.png)![b](https://example.org/2.png)](/p)":  "ab",
		} {
			out := mustRender(t, src)
			assertSafe(t, src, out)
			links := elements(t, out, "a")
			if len(links) != 1 || len(elements(t, out, "img")) != 0 {
				t.Errorf("want exactly the outer link and no img: %s", out)
				continue
			}
			if got := textOf(links[0]); got != want {
				t.Errorf("link text = %q, want %q: %s", got, want, out)
			}
		}
	})

	t.Run("unsafe sources", func(t *testing.T) {
		t.Parallel()
		for src, want := range map[string]string{
			`![a png](data:image/png;base64,iVBORw0KGgo=)`:          "a png",
			`![an svg](data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=)`: "an svg",
			`![script](javascript:alert(1))`:                        "script",
			`![script](JaVaScRiPt:alert(1))`:                        "script",
			`![](javascript:alert(1))`:                              "",
			`![vb](vbscript:msgbox(1))`:                             "vb",
			`![file](file:///etc/passwd)`:                           "file",
			`![ftp](ftp://example.org/a.png)`:                       "ftp",
		} {
			out := mustRender(t, src)
			assertSafe(t, src, out)
			if n := len(elements(t, out, "img")) + len(elements(t, out, "a")); n != 0 {
				t.Errorf("%s: want text only, got %s", src, out)
			}
			ps := elements(t, out, "p")
			if len(ps) != 1 || textOf(ps[0]) != want {
				t.Errorf("%s: want the text %q, got %s", src, want, out)
			}
		}
	})

	t.Run("alt text is escaped", func(t *testing.T) {
		t.Parallel()
		src := "![a <b>bold</b> & `<i>`](https://example.org/a.png)"
		out := mustRender(t, src)
		assertSafe(t, src, out)
		for _, bad := range []string{"<b>", "<i>"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s reached the output unescaped: %s", bad, out)
			}
		}
		if !strings.Contains(out, "&lt;i&gt;") {
			t.Errorf("the code span's text was lost: %s", out)
		}
	})
}

// TestLinkableMatchesThePolicy: linkable asks the sanitizer, and this holds
// it to what a markdown link to the same URL keeps through the whole
// pipeline, so an image can never become a link the sanitizer strips.
func TestLinkableMatchesThePolicy(t *testing.T) {
	t.Parallel()

	for _, u := range []string{
		"https://example.org/a.png",
		"HTTP://example.org/a.png",
		"//example.org/a.png",
		`/\example.org/a.png`,
		"/relative/a.png",
		"relative.png",
		"#fragment",
		"?query",
		"mailto:someone@example.org",
		"ftp://example.org/a.png",
		"javascript:alert(1)",
		"data:image/png;base64,iVBORw0KGgo=",
		"data:text/html;base64,PHNjcmlwdD4=",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"tel:+15551234567",
		"https://example.org/a%zz.png",
		"http://[::1",
		"https://example.org/a.png?x=1&y=2",
		`https://example.org/"quoted".png`,
	} {
		out := mustRender(t, "[x](<"+u+">)")
		kept := false
		for _, a := range elements(t, out, "a") {
			if href, ok := attr(a, "href"); ok && href != "" {
				kept = true
			}
		}
		if got := linkable(hrefBytes(u)); got != kept {
			t.Errorf("%q: linkable says %v, the pipeline's link %s", u, got, map[bool]string{true: "kept its href", false: "lost its href"}[kept])
		}
	}
}
