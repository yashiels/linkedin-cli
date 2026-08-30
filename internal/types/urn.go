// Package types defines shared data structures and helpers for the lnk CLI.
package types

import (
	"fmt"
	"strings"
)

// URN represents a LinkedIn URN such as "urn:li:fsd_jobPosting:4418763611".
type URN struct {
	// Raw is the original URN string.
	Raw string
	// Namespace is the "li" part.
	Namespace string
	// Type is the entity type, e.g. "fsd_jobPosting".
	Type string
	// ID is the entity identifier, e.g. "4418763611".
	ID string
}

// String returns the canonical URN representation.
func (u URN) String() string {
	return u.Raw
}

// ParseURN parses a LinkedIn URN of the form "urn:li:<type>:<id>".
//
// The ID segment must contain a clean numeric identifier. Composite URNs such
// as "urn:li:fsd_jobPostingCard:(4414051567,JOBS_SEARCH)" carry a tuple rather
// than a bare id; ParseURN extracts the leading numeric run when one is present
// at the start of the id segment, and otherwise returns an error so callers
// gating on err do not accept a garbage id.
func ParseURN(raw string) (URN, error) {
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) < 4 {
		return URN{}, fmt.Errorf("types: invalid URN %q (expected urn:li:<type>:<id>)", raw)
	}
	if parts[0] != "urn" {
		return URN{}, fmt.Errorf("types: URN must start with 'urn', got %q", parts[0])
	}
	id := leadingDigits(parts[3])
	if id == "" {
		return URN{}, fmt.Errorf("types: URN %q has no numeric id (got %q)", raw, parts[3])
	}
	return URN{
		Raw:       raw,
		Namespace: parts[1],
		Type:      parts[2],
		ID:        id,
	}, nil
}

// leadingDigits returns the run of ASCII digits at the start of s, or "" if s
// does not begin with a digit.
func leadingDigits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

// MustParseURN parses a URN or panics. Useful in tests.
func MustParseURN(raw string) URN {
	u, err := ParseURN(raw)
	if err != nil {
		panic(err)
	}
	return u
}
