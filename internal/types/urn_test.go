package types

import "testing"

func TestParseURN_CleanNumeric(t *testing.T) {
	u, err := ParseURN("urn:li:fsd_jobPosting:4418763611")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Type != "fsd_jobPosting" {
		t.Errorf("expected type fsd_jobPosting, got %q", u.Type)
	}
	if u.ID != "4418763611" {
		t.Errorf("expected ID 4418763611, got %q", u.ID)
	}
}

func TestParseURN_CompositeRejected(t *testing.T) {
	// Composite jobPostingCard URNs carry a tuple, not a bare numeric id, and
	// must be rejected so callers gating on err do not accept garbage ids.
	if _, err := ParseURN("urn:li:fsd_jobPostingCard:(4414051567,JOBS_SEARCH)"); err == nil {
		t.Error("expected error for composite URN, got nil")
	}
}

func TestParseURN_TooFewParts(t *testing.T) {
	if _, err := ParseURN("4418763611"); err == nil {
		t.Error("expected error for bare id, got nil")
	}
	if _, err := ParseURN("urn:li:fsd_jobPosting"); err == nil {
		t.Error("expected error for missing id segment, got nil")
	}
}

func TestParseURN_NotURN(t *testing.T) {
	if _, err := ParseURN("xyz:li:fsd_jobPosting:123"); err == nil {
		t.Error("expected error when prefix is not 'urn', got nil")
	}
}

func TestParseURN_LeadingNumericRun(t *testing.T) {
	// A trailing non-numeric suffix is trimmed to the leading numeric run.
	u, err := ParseURN("urn:li:fsd_jobPosting:123456?trk=abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID != "123456" {
		t.Errorf("expected ID 123456, got %q", u.ID)
	}
}
