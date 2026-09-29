package identity

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"

	"github.com/terva-sh/ketju/internal/config"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Policy is what each provider's configuration decides about a sign-in
// through it, by provider key. It is built from the configuration once, at
// startup, rather than read from the provider registry at request time: a
// provider a failed refresh has disabled drops out of the registry, and its
// policy must not vanish with it between a callback and the registration
// that follows (TKT-01M29CKRE9 shares this type).
type Policy map[string]ProviderPolicy

// ProviderPolicy is one provider's part of Policy.
type ProviderPolicy struct {
	// GroupMap maps a value of the provider's groups claim onto a local
	// group's slug (docs/05 "Roles and groups").
	GroupMap map[string]string
}

// PolicyFrom builds the policy the providers configure.
func PolicyFrom(providers []config.Provider) Policy {
	p := make(Policy, len(providers))
	for _, pr := range providers {
		p[pr.Key] = ProviderPolicy{GroupMap: pr.GroupMap}
	}
	return p
}

// LocalGroups maps the groups a sign-in through provider asserts onto local
// group slugs through the provider's group_map, each once, in the order the
// claim gave them. A provider with no group_map yields none, so a sign-in
// through it removes every IdP-mapped membership (settled in grooming).
//
// An unmapped group yields nothing rather than a default. Authenticating
// establishes who someone is and says nothing about what this forum meant
// to grant them, and an operator who writes a group map has already
// expressed that distinction by writing one.
func (p Policy) LocalGroups(provider string, groups []string) []string {
	m := p[provider].GroupMap
	if len(m) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		local, ok := m[g]
		if !ok || local == "" || seen[local] {
			continue
		}
		seen[local] = true
		out = append(out, local)
	}
	return out
}

// mapped is the values of groups that provider's group_map names: what a
// registration has to carry for LocalGroups to give the same answer later.
func (p Policy) mapped(provider string, groups []string) []string {
	m := p[provider].GroupMap
	var out []string
	for _, g := range groups {
		if _, ok := m[g]; ok && !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// UnknownTarget is a group_map target that names no group.
type UnknownTarget struct {
	Provider, Slug string
}

// UnknownTargets lists the group_map targets that name no group, in
// provider and slug order. A mapping onto one grants nothing. It is a
// warning rather than a configuration error, because deciding it needs the
// database, and a group may be created after the configuration is written.
func (p Policy) UnknownTargets(ctx context.Context, d *db.DB) ([]UnknownTarget, error) {
	var out []UnknownTarget
	err := d.Read(ctx, func(q *dbq.Queries, _ *sql.Tx) error {
		for provider, pp := range p {
			seen := map[string]bool{}
			for _, slug := range pp.GroupMap {
				if seen[slug] {
					continue
				}
				seen[slug] = true
				_, err := q.GetGroupBySlug(ctx, slug)
				if errors.Is(err, sql.ErrNoRows) {
					out = append(out, UnknownTarget{Provider: provider, Slug: slug})
					continue
				}
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Slug < out[j].Slug
	})
	return out, err
}
