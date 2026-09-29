package identity

import (
	"slices"
	"testing"

	"github.com/terva-sh/ketju/internal/auth/oidc"
	"github.com/terva-sh/ketju/internal/config"
)

var clubAndOther = PolicyFrom([]config.Provider{
	{Key: "club", GroupsClaim: "groups", GroupMap: map[string]string{"staff": "moderators", "leads": "moderators",
		"writers": "editors"}},
	{Key: "other"},
})

// TestPolicyLocalGroups states the rule that authenticating is not
// authorising. Defaulting an unmapped value into a local group would mean
// everyone the IdP admits becomes a member of something, which is the
// opposite of the decision an operator makes by writing a group map at all.
// A provider with no map, or one not configured, yields nothing, which is
// what makes a sign-in through it drop every idp membership.
func TestPolicyLocalGroups(t *testing.T) {
	for _, c := range []struct {
		name, provider string
		groups, want   []string
	}{
		{"mapped, in claim order", "club", []string{"writers", "unmapped", "staff"}, []string{"editors", "moderators"}},
		{"a duplicate grants once", "club", []string{"staff", "staff", "leads"}, []string{"moderators"}},
		{"nothing mapped", "club", []string{"unmapped"}, nil},
		{"no claim", "club", nil, nil},
		{"a provider with no map", "other", []string{"staff"}, nil},
		{"a provider not configured", "gone", []string{"staff"}, nil},
	} {
		if got := clubAndOther.LocalGroups(c.provider, c.groups); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := Policy(nil).LocalGroups("club", []string{"staff"}); got != nil {
		t.Errorf("a nil policy: %v", got)
	}
}

// A registration carries only the claim values the provider's map names,
// and they map as the whole claim would.
func TestClaimsOfKeepsOnlyMappedGroups(t *testing.T) {
	s := &Service{Policy: clubAndOther}
	id := &oidc.Identity{Provider: "club", Subject: "s", Groups: []string{"a", "staff", "b", "writers", "staff"}}
	c := s.ClaimsOf(id)
	if !slices.Equal(c.Groups, []string{"staff", "writers"}) {
		t.Errorf("Groups %v, want [staff writers]", c.Groups)
	}
	if got, want := s.Policy.LocalGroups("club", c.identity().Groups), s.Policy.LocalGroups("club", id.Groups); !slices.Equal(got, want) {
		t.Errorf("the carried claims map to %v, the token's to %v", got, want)
	}
	if c := s.ClaimsOf(&oidc.Identity{Provider: "other", Groups: []string{"staff"}}); c.Groups != nil {
		t.Errorf("a provider with no map carried %v", c.Groups)
	}
}

// UnknownTargets names each target no group has, once, and not the ones a
// group has.
func TestUnknownTargets(t *testing.T) {
	_, d := registry(t)
	p := PolicyFrom([]config.Provider{
		{Key: "club", GroupMap: map[string]string{"a": "moderators", "b": "nope", "c": "nope"}},
		{Key: "b-side", GroupMap: map[string]string{"x": "missing"}},
		{Key: "other"},
	})
	got, err := p.UnknownTargets(t.Context(), d)
	if err != nil {
		t.Fatal(err)
	}
	want := []UnknownTarget{{"b-side", "missing"}, {"club", "nope"}}
	if !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}
