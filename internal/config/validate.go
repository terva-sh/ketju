package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// validate checks everything that survived decoding, defaults and overrides.
//
// Every message names the key and says what was wrong with the value it saw.
// Where there is an obvious correction it says that too, because the reader is
// an operator with a server that will not start, not a developer with the
// source open.
func validate(c *Config, p *problemList) {
	validateListen(c.Listen, p)
	validateDataDir(c.DataDir, p)
	validateStorage(c.Storage, p)
	validateLog(c.Log, p)
	validateSiteURL(c, p)
	validateProviders(c, p)
	validateBootstrap(c.Bootstrap, p)
	validateLimits(c.Limits, p)
}

// maxAttachmentMB is a sanity ceiling, not a policy. An upload is buffered
// against this bound before anything has authenticated the uploader, so a
// four-figure value turns one request into a memory exhaustion tool. An
// operator who genuinely wants to host larger files wants object storage,
// which is a different ticket and a different conversation.
const maxAttachmentMB = 512

func validateLimits(l Limits, p *problemList) {
	if l.AttachmentMB < 1 {
		p.addf("limits.attachment_mb", "%d is not a usable cap; an attachment limit below 1 MB refuses every real upload", l.AttachmentMB)
	}
	if l.AttachmentMB > maxAttachmentMB {
		p.addf("limits.attachment_mb", "%d MB is above the ceiling of %d MB; an upload is bounded before the uploader is authenticated", l.AttachmentMB, maxAttachmentMB)
	}
}

func validateListen(addr string, p *problemList) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		p.addf("listen", "%q is not host:port (a bare port needs a colon, as in \":8080\")", addr)
		return
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		// A named service is legal for net.Listen but resolves differently on
		// every machine, which is not a thing to discover in production.
		p.addf("listen", "port %q is not a number", port)
		return
	}
	if n < 1 || n > 65535 {
		p.addf("listen", "port %d is outside 1-65535", n)
	}
	if host != "" && net.ParseIP(host) == nil && !isHostname(host) {
		p.addf("listen", "%q is not an IP address or a hostname", host)
	}
}

func validateDataDir(dir string, p *problemList) {
	if dir == "" {
		p.add("data_dir", "required: the directory holding threads/, blobs/ and db/. There is no default that is right on two machines")
		return
	}
	fi, err := os.Stat(dir)
	if err != nil {
		// Not existing yet is fine; `ketju serve` creates it. Anything else —
		// a permission error on the parent, a broken symlink — is worth
		// saying now rather than at the first append.
		if !os.IsNotExist(err) {
			p.addf("data_dir", "cannot inspect %s: %v", dir, redactPathError(err))
		}
		return
	}
	if !fi.IsDir() {
		p.addf("data_dir", "%s exists and is not a directory", dir)
	}
}

func validateStorage(s Storage, p *problemList) {
	switch s.Fsync {
	case "group", "always", "never":
	default:
		p.addf("storage.fsync", "unknown policy %q; use \"group\" (the default), \"always\", or \"never\"", s.Fsync)
	}
	// "never" is deliberately not an error. It is the right policy for an
	// import or a test run, and refusing it would send people to a fork of the
	// config loader. It does lose acknowledged posts on a crash, which is what
	// `ketju config check` and the startup line are for.
	if s.OpenThreads < minimumOpenThreadsAllow {
		p.addf("storage.open_threads", "%d is below the minimum of %d; a cache that holds no threads cannot serve a read",
			s.OpenThreads, minimumOpenThreadsAllow)
	} else if s.OpenThreads > defaultMaxOpenThreads {
		p.addf("storage.open_threads", "%d would hold roughly %d file descriptors open; this is almost certainly a typo",
			s.OpenThreads, 2*s.OpenThreads)
	}
	checkCacheMB("storage.rendered_cache_mb", s.RenderedCacheMB, p)
	checkCacheMB("storage.response_cache_mb", s.ResponseCacheMB, p)
}

func checkCacheMB(key string, mb int, p *problemList) {
	switch {
	case mb < 0:
		p.addf(key, "%d is negative; 0 disables the cache", mb)
	case mb > defaultMaxCacheMB:
		p.addf(key, "%d MB is larger than any machine this runs on; this is almost certainly a typo", mb)
	}
}

// validateLog refuses an unknown level or format by name.
//
// Both are small closed sets, and a typo in either is the kind of mistake that
// otherwise surfaces as "why is there no debug output" long after the restart
// that introduced it.
func validateLog(l Log, p *problemList) {
	switch l.Level {
	case "debug", "info", "warn", "error":
	default:
		p.add("log.level", fmt.Sprintf("%q is not a level; use debug, info, warn or error", l.Level))
	}
	switch l.Format {
	case "json", "text", "auto":
	default:
		p.add("log.format", fmt.Sprintf("%q is not a format; use json, text or auto", l.Format))
	}
}

func validateSiteURL(c *Config, p *problemList) {
	if c.SiteURL == "" {
		if len(c.OIDC.Providers) > 0 {
			p.add("site_url", "required once an OIDC provider is configured: the redirect URI is built from it, and it cannot be derived from listen when the server is behind a proxy")
		}
		return
	}
	u, err := url.Parse(c.SiteURL)
	if err != nil {
		p.addf("site_url", "%q is not a URL: %v", c.SiteURL, err)
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		p.addf("site_url", "%q must be an absolute http or https URL", c.SiteURL)
		return
	}
	if u.Host == "" {
		p.addf("site_url", "%q has no host", c.SiteURL)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		p.addf("site_url", "%q must be a base URL, with no query or fragment", c.SiteURL)
	}
}

func validateProviders(c *Config, p *problemList) {
	seen := make(map[string]int, len(c.OIDC.Providers))
	for i := range c.OIDC.Providers {
		pr := c.OIDC.Providers[i]
		key := fmt.Sprintf("oidc.providers[%d]", i)
		if pr.Key == "" {
			p.add(key+".key", "required: it is stored on every identity row, so it identifies the provider forever")
		} else {
			named := fmt.Sprintf("oidc.providers.%s", pr.Key)
			if !isProviderKey(pr.Key) {
				p.addf(named+".key", "%q must be lowercase letters, digits, dash or underscore; it appears in URLs and in the database", pr.Key)
			}
			if first, dup := seen[pr.Key]; dup {
				p.addf(named+".key", "used by both provider %d and provider %d; the key is what an identity row points at, so two providers cannot share one", first, i)
			} else {
				seen[pr.Key] = i
			}
			key = named
		}

		if pr.Name == "" {
			p.add(key+".name", "required: it is the label on the login button")
		}
		validateIssuer(key, pr.Issuer, p)
		if pr.ClientID == "" {
			p.add(key+".client_id", "required")
		}
		if pr.ClientSecret.IsZero() && pr.ClientSecretFile == "" {
			p.add(key+".client_secret", "required: give it here, or as a path in client_secret_file, or in the environment as "+providerEnv(pr.Key, "CLIENT_SECRET"))
		}
		for _, d := range pr.AllowedEmailDomains {
			if d == "" || strings.ContainsAny(d, "@ ") {
				p.addf(key+".allowed_email_domains", "%q is not a domain; give the part after the @", d)
			}
		}
		for from, to := range pr.GroupMap {
			if from == "" || to == "" {
				p.addf(key+".group_map", "maps %q to %q; neither side may be empty", from, to)
			}
		}
		if pr.GroupsClaim == "" && len(pr.GroupMap) > 0 {
			p.add(key+".group_map", "set without groups_claim, so there is nothing to map from")
		}
	}
}

func validateIssuer(key, issuer string, p *problemList) {
	if issuer == "" {
		p.add(key+".issuer", "required: only configured issuers are ever contacted, which is what makes providers an allowlist")
		return
	}
	u, err := url.Parse(issuer)
	if err != nil {
		p.addf(key+".issuer", "%q is not a URL: %v", issuer, err)
		return
	}
	if u.Scheme != "https" {
		// Not a style preference. The ID token's signing keys are fetched
		// from this origin, so plaintext here means anyone on the path can
		// choose who is allowed to log in.
		p.addf(key+".issuer", "%q must be https; the signing keys are fetched from it, so plaintext means anyone on the network decides who may log in", issuer)
		return
	}
	if u.Host == "" {
		p.addf(key+".issuer", "%q has no host", issuer)
	}
	// A trailing slash is not judged here. Some providers send one (Authentik's
	// per-application issuer is https://host/application/o/<slug>/) and most do
	// not, so either form can be the right one. The issuer is used exactly as
	// written: discovery compares it with the document's issuer byte for byte,
	// and the verifier compares the iss claim, so a wrong guess about the slash
	// disables the provider at startup with both values in the error.
	if u.RawQuery != "" || u.Fragment != "" {
		p.addf(key+".issuer", "%q must be a bare issuer URL, with no query or fragment", issuer)
	}
}

func validateBootstrap(b Bootstrap, p *problemList) {
	for _, e := range b.AdminEmails {
		if !strings.Contains(e, "@") || strings.HasPrefix(e, "@") || strings.HasSuffix(e, "@") {
			p.addf("bootstrap.admin_emails", "%q is not an email address", e)
		}
	}
	for _, s := range b.AdminSubjects {
		provider, sub, ok := strings.Cut(s, ":")
		if !ok || provider == "" || sub == "" {
			p.addf("bootstrap.admin_subjects", "%q must be \"provider_key:subject\"; a bare subject is ambiguous once there are two providers", s)
		}
	}
}

// warn reports what is legal but costs something an operator should choose
// knowingly. A warning does not stop the server and does not fail
// `ketju config check`; serve logs each one at startup, and the check prints
// them. docs/02 "Configuration" lists which configurations are warnings and
// why each is not an error.
//
// Each warning opens with the key, as a problem does, so that the line an
// operator pastes says which setting it is about.
func warn(c *Config) []string {
	var out []string
	if c.Storage.Fsync == "never" {
		out = append(out, `storage.fsync: "never" acknowledges a post before it reaches the disk, so a crash or power loss loses posts members were told were saved; use it for imports and tests, not for a forum people write in`)
	}
	for i, pr := range c.OIDC.Providers {
		key := fmt.Sprintf("oidc.providers[%d]", i)
		if pr.Key != "" {
			key = "oidc.providers." + pr.Key
		}
		if pr.InsecureSkipIssuerCheck {
			out = append(out, key+".insecure_skip_issuer_check: on, so this provider's ID tokens are accepted without checking who issued them; it exists for the one provider family whose issuer differs from its discovery URL (docs/09), and is otherwise a hole")
		}
		if pr.AutoApprove {
			// Approval-mode registration is TKT-01M29CKRE9's, and until it
			// lands auto_approve changes nothing (internal/service/identity
			// RegistrationOpen). That ticket removes this warning.
			out = append(out, key+`.auto_approve: on, but this version has no approval queue for it to bypass, so it has no effect; someone new from this provider can register only while registration.mode is "open"`)
		}
	}
	return out
}

func isHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			case c == '-' && i != 0 && i != len(label)-1:
			default:
				return false
			}
		}
	}
	return true
}

func isProviderKey(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
