package oidctest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
)

// authURL is an authorization request as the relying party makes one, with
// verifier's challenge.
func authURL(idp *IdP, verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	v := url.Values{"client_id": {ClientID}, "response_type": {"code"}, "redirect_uri": {"http://rp.example/cb"},
		"state": {"st"}, "nonce": {"n1"}, "code_challenge_method": {"S256"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
	return idp.URL + "/authorize?" + v.Encode()
}

// exchange trades the code in callback for an ID token and returns its sub.
func exchange(t *testing.T, idp *IdP, callback, verifier string) string {
	t.Helper()
	cb, _ := url.Parse(callback)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {cb.Query().Get("code")},
		"redirect_uri": {"http://rp.example/cb"}, "code_verifier": {verifier}}
	req, _ := http.NewRequest("POST", idp.URL+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(ClientID, ClientSecret)
	resp, err := idp.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != 200 {
		t.Fatalf("token: %d %v", resp.StatusCode, err)
	}
	tok, err := jose.ParseSigned(body.IDToken, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tok.Verify(&idp.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Sub
}

// With Users set, the authorization endpoint is a sign-in page, and the
// user followed is who the token names; without, it approves at once as
// Subject.
func TestSignInPage(t *testing.T) {
	idp := New(t)
	idp.Users = []string{"alice", "carol"}
	c := idp.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	resp, err := c.Get(authURL(idp, "v1"))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("the sign-in page answered %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	links := regexp.MustCompile(`href="([^"]+)">([^<]+)<`).FindAllStringSubmatch(string(page), -1)
	if len(links) != 2 || links[0][2] != "alice" || links[1][2] != "carol" {
		t.Fatalf("links %q in\n%s", links, page)
	}

	carol := strings.ReplaceAll(links[1][1], "&amp;", "&")
	resp, err = c.Get(idp.URL + carol)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("choosing carol answered %d", resp.StatusCode)
	}
	if sub := exchange(t, idp, resp.Header.Get("Location"), "v1"); sub != "carol" {
		t.Errorf("the token names %q, want carol", sub)
	}

	// Someone not on the list is shown the page again, not signed in.
	resp, err = c.Get(authURL(idp, "v2") + "&login_as=mallory")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("an unlisted login_as answered %d", resp.StatusCode)
	}

	idp.Users = nil
	idp.Subject = "sam"
	if sub := exchange(t, idp, idp.Authorize(t, authURL(idp, "v3")), "v3"); sub != "sam" {
		t.Errorf("without Users the token names %q, want sam", sub)
	}
}
