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
// Additional colon-separated segments are concatenated into ID with colons.
func ParseURN(raw string) (URN, error) {
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) < 4 {
		return URN{}, fmt.Errorf("types: invalid URN %q (expected urn:li:<type>:<id>)", raw)
	}
	if parts[0] != "urn" {
		return URN{}, fmt.Errorf("types: URN must start with 'urn', got %q", parts[0])
	}
	return URN{
		Raw:       raw,
		Namespace: parts[1],
		Type:      parts[2],
		ID:        parts[3],
	}, nil
}

// MustParseURN parses a URN or panics. Useful in tests.
func MustParseURN(raw string) URN {
	u, err := ParseURN(raw)
	if err != nil {
		panic(err)
	}
	return u
}
