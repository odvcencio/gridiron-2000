// Package identity owns the explicit, operator-controlled identity boundary
// between an authentication provider's email and the application's canonical
// person key.
//
// Aliases are intentionally exact and one-way. This package does not infer
// Gmail dots, plus tags, domains, display names, or provider-specific account
// relationships. A mapping is a security configuration decision made by the
// operator.
package identity

import (
	"fmt"
	"net/mail"
	"os"
	"sort"
	"strings"
)

// Resolver maps explicitly configured authentication aliases to canonical
// application identities. The zero value is a useful resolver: it still
// normalizes an email for use as a stable key, but performs no alias merge.
type Resolver struct {
	aliases map[string]string
}

// Pair is one normalized alias mapping, suitable for diagnostics and tests.
type Pair struct {
	Alias     string
	Canonical string
}

// New validates and copies alias mappings. The input is alias -> canonical.
// Canonical targets may have multiple aliases, but a target may not itself be
// an alias: rejecting chains makes resolution deterministic and prevents
// configuration cycles or order-dependent privilege decisions.
func New(mappings map[string]string) (Resolver, error) {
	if len(mappings) == 0 {
		return Resolver{}, nil
	}
	aliases := make(map[string]string, len(mappings))
	for rawAlias, rawCanonical := range mappings {
		alias := normalize(rawAlias)
		canonical := normalize(rawCanonical)
		if err := validateEmail(alias, "alias"); err != nil {
			return Resolver{}, err
		}
		if err := validateEmail(canonical, "canonical identity"); err != nil {
			return Resolver{}, err
		}
		if alias == canonical {
			return Resolver{}, fmt.Errorf("identity alias %q maps to itself", alias)
		}
		if previous, exists := aliases[alias]; exists && previous != canonical {
			return Resolver{}, fmt.Errorf("identity alias %q has conflicting canonical targets %q and %q", alias, previous, canonical)
		}
		aliases[alias] = canonical
	}
	for alias, canonical := range aliases {
		if _, chained := aliases[canonical]; chained {
			return Resolver{}, fmt.Errorf("identity alias %q points to alias %q; mappings must be one-way", alias, canonical)
		}
	}
	return Resolver{aliases: aliases}, nil
}

// FromEnv loads IDENTITY_ALIASES. The wire format is a comma-separated list
// of alias=canonical pairs, for example:
//
//	IDENTITY_ALIASES=oscar@m31labs.dev=oscar.villavicencio@stablekernel.com
//
// Blank input disables aliasing. A malformed value fails closed.
func FromEnv() (Resolver, error) {
	raw := strings.TrimSpace(os.Getenv("IDENTITY_ALIASES"))
	if raw == "" {
		return Resolver{}, nil
	}
	mappings := make(map[string]string)
	for _, rawPair := range strings.Split(raw, ",") {
		pair := strings.TrimSpace(rawPair)
		if pair == "" {
			return Resolver{}, fmt.Errorf("IDENTITY_ALIASES contains an empty mapping")
		}
		alias, canonical, ok := strings.Cut(pair, "=")
		if !ok || strings.Contains(canonical, "=") {
			return Resolver{}, fmt.Errorf("IDENTITY_ALIASES mapping %q must use alias=canonical", pair)
		}
		alias = normalize(alias)
		canonical = normalize(canonical)
		if previous, exists := mappings[alias]; exists && previous != canonical {
			return Resolver{}, fmt.Errorf("IDENTITY_ALIASES alias %q has conflicting targets", alias)
		}
		mappings[alias] = canonical
	}
	return New(mappings)
}

// Resolve returns the canonical key for email. It always lower-cases and
// trims surrounding whitespace, including when aliasing is disabled.
func (r Resolver) Resolve(email string) string {
	normalized := normalize(email)
	if canonical, ok := r.aliases[normalized]; ok {
		return canonical
	}
	return normalized
}

// Enabled reports whether at least one explicit alias is configured.
func (r Resolver) Enabled() bool {
	return len(r.aliases) > 0
}

// Pairs returns a stable, defensive copy of the configured mappings.
func (r Resolver) Pairs() []Pair {
	if len(r.aliases) == 0 {
		return nil
	}
	aliases := make([]string, 0, len(r.aliases))
	for alias := range r.aliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	pairs := make([]Pair, 0, len(aliases))
	for _, alias := range aliases {
		pairs = append(pairs, Pair{Alias: alias, Canonical: r.aliases[alias]})
	}
	return pairs
}

func normalize(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateEmail(value, label string) error {
	if value == "" {
		return fmt.Errorf("identity %s must not be empty", label)
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("identity %s %q is not a bare email address", label, value)
	}
	if !strings.Contains(value, "@") {
		return fmt.Errorf("identity %s %q is not a bare email address", label, value)
	}
	return nil
}
