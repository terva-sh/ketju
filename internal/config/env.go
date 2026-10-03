package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvPrefix is the prefix on every variable this package reads.
const EnvPrefix = "KETJU_"

// FileSuffix names the companion variable that supplies a value from a file.
//
// One rule, applied to every override: KETJU_X_FILE supplies the value of
// KETJU_X. It exists because the environment is the wrong place for a secret —
// it is inherited by every child process, it shows up in a crash dump, and on
// Linux it is readable from /proc for anyone who can see the process — while a
// file can be mounted with its own permissions and its own lifetime. Docker
// and Kubernetes both grew the same convention, which is why it is spelled the
// same way here.
const FileSuffix = "_FILE"

// Override is one environment variable and the key it replaces.
type Override struct {
	// Env is the variable name, without the _FILE companion.
	Env string

	// Key is the dotted config key it overrides.
	Key string

	// Secret is true when the value must never be printed.
	Secret bool

	// apply writes the value, reporting a problem if it cannot be parsed.
	apply func(c *Config, v string, p *problemList)
}

// overrides is the complete set of whole-config variables, written out rather
// than derived from the struct tags.
//
// Deriving them would be less code and a worse contract. The mapping from
// nested key to variable name has to be guessed by whoever writes the compose
// file, and a derived scheme changes silently whenever a field is renamed or
// nested one level deeper. This list is the interface; a field rename that
// should change a variable name has to change it here, in the diff, where a
// reviewer sees it.
var overrides = []Override{
	{Env: "KETJU_LISTEN", Key: "listen", apply: func(c *Config, v string, _ *problemList) { c.Listen = v }},
	{Env: "KETJU_METRICS_LISTEN", Key: "metrics_listen", apply: func(c *Config, v string, _ *problemList) { c.MetricsListen = v }},
	{Env: "KETJU_DATA_DIR", Key: "data_dir", apply: func(c *Config, v string, _ *problemList) { c.DataDir = v }},
	{Env: "KETJU_COOKIE_KEY_FILE", Key: "cookie_key_file", apply: func(c *Config, v string, _ *problemList) { c.CookieKeyFile = v }},
	{Env: "KETJU_SITE_URL", Key: "site_url", apply: func(c *Config, v string, _ *problemList) { c.SiteURL = v }},

	{Env: "KETJU_LOG_LEVEL", Key: "log.level", apply: func(c *Config, v string, _ *problemList) { c.Log.Level = v }},
	{Env: "KETJU_LOG_FORMAT", Key: "log.format", apply: func(c *Config, v string, _ *problemList) { c.Log.Format = v }},

	{Env: "KETJU_STORAGE_FSYNC", Key: "storage.fsync", apply: func(c *Config, v string, _ *problemList) { c.Storage.Fsync = v }},
	{Env: "KETJU_STORAGE_OPEN_THREADS", Key: "storage.open_threads", apply: func(c *Config, v string, p *problemList) {
		c.Storage.OpenThreads = parseInt(v, "storage.open_threads", "KETJU_STORAGE_OPEN_THREADS", p)
	}},
	{Env: "KETJU_STORAGE_RENDERED_CACHE_MB", Key: "storage.rendered_cache_mb", apply: func(c *Config, v string, p *problemList) {
		c.Storage.RenderedCacheMB = parseInt(v, "storage.rendered_cache_mb", "KETJU_STORAGE_RENDERED_CACHE_MB", p)
	}},
	{Env: "KETJU_STORAGE_RESPONSE_CACHE_MB", Key: "storage.response_cache_mb", apply: func(c *Config, v string, p *problemList) {
		c.Storage.ResponseCacheMB = parseInt(v, "storage.response_cache_mb", "KETJU_STORAGE_RESPONSE_CACHE_MB", p)
	}},

	{Env: "KETJU_LIMITS_ATTACHMENT_MB", Key: "limits.attachment_mb", apply: func(c *Config, v string, p *problemList) {
		c.Limits.AttachmentMB = parseInt(v, "limits.attachment_mb", "KETJU_LIMITS_ATTACHMENT_MB", p)
	}},
	{Env: "KETJU_LIMITS_UNATTACHED_UPLOAD_MB", Key: "limits.unattached_upload_mb", apply: func(c *Config, v string, p *problemList) {
		c.Limits.UnattachedUploadMB = parseInt(v, "limits.unattached_upload_mb", "KETJU_LIMITS_UNATTACHED_UPLOAD_MB", p)
	}},
	{Env: "KETJU_LIMITS_SSE_CONNECTIONS", Key: "limits.sse_connections", apply: func(c *Config, v string, p *problemList) {
		c.Limits.SSEConnections = parseInt(v, "limits.sse_connections", "KETJU_LIMITS_SSE_CONNECTIONS", p)
	}},
	{Env: "KETJU_LIMITS_SSE_PER_USER", Key: "limits.sse_per_user", apply: func(c *Config, v string, p *problemList) {
		c.Limits.SSEPerUser = parseInt(v, "limits.sse_per_user", "KETJU_LIMITS_SSE_PER_USER", p)
	}},
	{Env: "KETJU_LIMITS_SSE_ANONYMOUS", Key: "limits.sse_anonymous", apply: func(c *Config, v string, p *problemList) {
		c.Limits.SSEAnonymous = parseInt(v, "limits.sse_anonymous", "KETJU_LIMITS_SSE_ANONYMOUS", p)
	}},

	{Env: "KETJU_BOOTSTRAP_ADMIN_EMAILS", Key: "bootstrap.admin_emails", apply: func(c *Config, v string, _ *problemList) {
		c.Bootstrap.AdminEmails = splitList(v)
	}},
	{Env: "KETJU_BOOTSTRAP_ADMIN_SUBJECTS", Key: "bootstrap.admin_subjects", apply: func(c *Config, v string, _ *problemList) {
		c.Bootstrap.AdminSubjects = splitList(v)
	}},
}

// providerOverrides are the per-provider suffixes. The variable name carries
// the provider key: KETJU_OIDC_GOOGLE_CLIENT_SECRET.
//
// A provider cannot be created from the environment, only amended. A provider
// block is six related keys that only make sense together, and half a provider
// assembled from three variables would be a configuration nobody wrote down.
var providerOverrides = []struct {
	Suffix string
	Key    string
	Secret bool
	apply  func(p *Provider, v string)
}{
	{Suffix: "ISSUER", Key: "issuer", apply: func(p *Provider, v string) { p.Issuer = v }},
	{Suffix: "CLIENT_ID", Key: "client_id", apply: func(p *Provider, v string) { p.ClientID = v }},
	{Suffix: "CLIENT_SECRET", Key: "client_secret", Secret: true, apply: func(p *Provider, v string) {
		p.ClientSecret = NewSecret(v)
		// An override replaces the indirection rather than competing with it,
		// so that a deployment which sets the variable is not also required to
		// remove a client_secret_file line from a file it may not own.
		p.ClientSecretFile = ""
	}},
}

// Overrides reports every variable this build understands, including the
// per-provider ones for the providers actually configured. It is what
// `ketju config check` prints, and what keeps the documentation honest.
func (c *Config) Overrides() []Override {
	out := make([]Override, 0, len(overrides)+len(c.OIDC.Providers)*len(providerOverrides))
	out = append(out, overrides...)
	for _, p := range c.OIDC.Providers {
		for _, po := range providerOverrides {
			out = append(out, Override{
				Env:    providerEnv(p.Key, po.Suffix),
				Key:    fmt.Sprintf("oidc.providers.%s.%s", p.Key, po.Key),
				Secret: po.Secret,
			})
		}
	}
	return out
}

// applyEnv overlays the environment on what the file said. It runs before
// defaults, so that an override is applied to the file's value and a default
// still fills a key neither source set.
func applyEnv(c *Config, getenv func(string) string, present map[string]bool, p *problemList) {
	for _, o := range overrides {
		v, src, ok := lookup(getenv, o.Env, p)
		if !ok {
			continue
		}
		o.apply(c, v, p)
		present[o.Key] = true
		c.setSource(o.Key, src)
	}

	for i := range c.OIDC.Providers {
		pr := &c.OIDC.Providers[i]
		if pr.Key == "" {
			// Validation reports the missing key; without one there is no
			// variable name to look for.
			continue
		}
		for _, po := range providerOverrides {
			name := providerEnv(pr.Key, po.Suffix)
			v, src, ok := lookup(getenv, name, p)
			if !ok {
				continue
			}
			po.apply(pr, v)
			prefix := "oidc.providers." + pr.Key + "."
			c.setSource(prefix+po.Key, src)
			if po.Key == "client_secret" {
				// The override emptied client_secret_file too, so the
				// variable is why it is empty.
				c.setSource(prefix+"client_secret_file", src)
			}
		}
	}
}

// lookup resolves one variable, honouring the _FILE companion.
//
// Setting both is an error rather than a precedence rule. Precedence here
// would mean a deployment that sets both gets one of them silently, and the
// one it gets is the one nobody checked.
func lookup(getenv func(string) string, name string, p *problemList) (string, Source, bool) {
	fileVar := name + FileSuffix
	direct := getenv(name)
	path := getenv(fileVar)
	// An empty variable counts as unset. Setting one to the empty string is
	// almost always an unexpanded shell variable rather than a deliberate
	// blanking, and the alternative -- treating it as an override to "" --
	// turns a typo in a compose file into a config with no data directory.
	hasDirect, hasFile := direct != "", path != ""

	switch {
	case hasDirect && hasFile:
		p.addf(name, "%s and %s are both set; give the value in one place so that there is no question which one is live", name, fileVar)
		return "", Source{}, false
	case hasFile:
		b, err := os.ReadFile(path)
		if err != nil {
			p.addf(fileVar, "cannot read %s: %v", path, redactPathError(err))
			return "", Source{}, false
		}
		v := strings.TrimRight(string(b), "\r\n")
		if v == "" {
			p.addf(fileVar, "%s is empty", path)
			return "", Source{}, false
		}
		return v, Source{Layer: LayerEnvFile, From: fileVar}, true
	case hasDirect:
		return direct, Source{Layer: LayerEnv, From: name}, true
	default:
		return "", Source{}, false
	}
}

// providerEnv builds the variable name for one provider key. A key may contain
// a dash, which is not legal in a shell variable name, so it becomes an
// underscore.
func providerEnv(key, suffix string) string {
	k := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(key))
	return EnvPrefix + "OIDC_" + k + "_" + suffix
}

// parseInt reports the variable as well as the key, because the operator who
// sees this error set the variable and may not have the file in front of them.
func parseInt(v, key, env string, p *problemList) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		p.addf(key, "%s is %q, which is not a number", env, v)
		return 0
	}
	return n
}

// splitList parses a comma-separated override of a list-valued key. Empty
// entries are dropped so that a trailing comma is not a blank admin email.
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
