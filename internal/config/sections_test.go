package config

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestEverySectionHasAReason is TKT-01M29CXK2H's second criterion, the
// server half: every key Entries lists for a config that sets every
// section, providers included, belongs to exactly one section, and every
// section says why it lives in config.toml. A key added to Config without a
// reason fails here rather than reaching the admin screen unexplained.
func TestEverySectionHasAReason(t *testing.T) {
	t.Parallel()
	sections := Sections()
	owner := map[string]string{}
	for _, s := range sections {
		if strings.TrimSpace(s.Reason) == "" {
			t.Errorf("the section %s has no reason", s.Name)
		}
		if len(s.Keys) == 0 {
			t.Errorf("the section %s has no keys", s.Name)
		}
		for _, k := range s.Keys {
			if k != s.Name && !strings.HasPrefix(k, s.Name+".") && !strings.HasPrefix(k, s.Name+"[]") {
				t.Errorf("the section %s lists %s, which is not under it", s.Name, k)
			}
			if prev, dup := owner[k]; dup {
				t.Errorf("%s is in both %s and %s", k, prev, s.Name)
			}
			owner[k] = s.Name
		}
	}
	for name := range sectionReasons {
		if !slices.ContainsFunc(sections, func(s Section) bool { return s.Name == name }) {
			t.Errorf("sectionReasons explains %s, which Config does not have", name)
		}
	}

	// Entries keys a provider's fields by the provider's key, where a
	// section names the element [].
	provider := regexp.MustCompile(`^oidc\.providers\.[^.]+\.`)
	entries := loadFull(t).Entries()
	if len(entries) == 0 {
		t.Fatal("the full config has no entries")
	}
	sawProvider := false
	for _, e := range entries {
		key := e.Key
		if provider.MatchString(key) {
			sawProvider = true
			key = provider.ReplaceAllString(key, "oidc.providers[].")
		}
		if _, ok := owner[key]; !ok {
			t.Errorf("Entries lists %s (as %s), which no section names", e.Key, key)
		}
	}
	if !sawProvider {
		t.Error("the full config has no provider, so the walk into a list of tables went untested")
	}
}
