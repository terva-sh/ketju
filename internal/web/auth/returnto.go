package auth

import (
	"net/url"
	"strings"
)

// SafeReturnPath is where a sign-in may send the browser when it is done:
// raw if it is a path on this site, and "/" for anything else.
//
// A sign-in that redirects wherever it is told is an open redirect with a
// trustworthy domain in front, which is the classic way a login endpoint
// becomes a phishing tool. So return_to is read as the browser would read
// it, and refused unless the browser would stay here:
//
//   - It starts with a slash, so it names no scheme. url.Parse refuses any
//     ASCII control character, which browsers strip before parsing and which
//     could otherwise join what this check saw as separate ("/\t/evil").
//   - It has no host: "//evil.example" is protocol-relative.
//   - Its path, unescaped, neither starts with "//" nor holds a backslash,
//     which browsers read as a slash ("/\evil.example"), and which a proxy
//     that normalises paths might ("/%2F%2Fevil.example", "/%5Cevil").
func SafeReturnPath(raw string) string {
	if raw == "" || raw[0] != '/' {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" {
		return "/"
	}
	if p, err := url.PathUnescape(u.EscapedPath()); err != nil || strings.HasPrefix(p, "//") || strings.Contains(p, "\\") {
		return "/"
	}
	return raw
}
