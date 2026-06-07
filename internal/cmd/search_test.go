package cmd

import (
	"testing"
)

func TestResolveFormat_JSON(t *testing.T) {
	flagJSON := true
	flagPlain := false
	if got := resolveFormat(&flagJSON, &flagPlain); got != "json" {
		t.Errorf("expected json, got %q", got)
	}
}

func TestResolveFormat_Plain(t *testing.T) {
	flagJSON := false
	flagPlain := true
	if got := resolveFormat(&flagJSON, &flagPlain); got != "plain" {
		t.Errorf("expected plain, got %q", got)
	}
}

func TestResolveFormat_Default(t *testing.T) {
	flagJSON := false
	flagPlain := false
	if got := resolveFormat(&flagJSON, &flagPlain); got != "table" {
		t.Errorf("expected table, got %q", got)
	}
}

func TestResolveFormat_JSONWins(t *testing.T) {
	// --json takes precedence over --plain.
	flagJSON := true
	flagPlain := true
	if got := resolveFormat(&flagJSON, &flagPlain); got != "json" {
		t.Errorf("expected json to win, got %q", got)
	}
}

func TestResolveLocation_Empty(t *testing.T) {
	urn, err := resolveLocation("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if urn != "" {
		t.Errorf("expected empty URN for empty location, got %q", urn)
	}
}

func TestResolveLocation_RawURN(t *testing.T) {
	urn, err := resolveLocation("urn:li:fsd_geo:12345")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if urn != "urn:li:fsd_geo:12345" {
		t.Errorf("expected raw URN passthrough, got %q", urn)
	}
}

func TestResolveLocation_KnownCity(t *testing.T) {
	urn, err := resolveLocation("Cape Town")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if urn != "urn:li:fsd_geo:105013608" {
		t.Errorf("expected Cape Town URN, got %q", urn)
	}
}

func TestResolveLocation_CaseInsensitive(t *testing.T) {
	urn, err := resolveLocation("JOHANNESBURG")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if urn != "urn:li:fsd_geo:104273735" {
		t.Errorf("expected Joburg URN, got %q", urn)
	}
}

func TestResolveLocation_Abbreviation(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"za", "urn:li:fsd_geo:104035573"},
		{"jhb", "urn:li:fsd_geo:104273735"},
		{"ct", "urn:li:fsd_geo:105013608"},
		{"pta", "urn:li:fsd_geo:105944906"},
		{"dbn", "urn:li:fsd_geo:106463985"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			urn, err := resolveLocation(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if urn != tt.expected {
				t.Errorf("for %q: expected %q, got %q", tt.input, tt.expected, urn)
			}
		})
	}
}

func TestResolveLocation_Unknown(t *testing.T) {
	_, err := resolveLocation("Atlantis")
	if err == nil {
		t.Error("expected error for unknown location")
	}
}

func TestParseJobTypes_Empty(t *testing.T) {
	codes, err := parseJobTypes("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if codes != nil {
		t.Errorf("expected nil, got %v", codes)
	}
}

func TestParseJobTypes_FriendlyNames(t *testing.T) {
	codes, err := parseJobTypes("full-time,contract,internship")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d", len(codes))
	}
	if codes[0] != "F" || codes[1] != "C" || codes[2] != "I" {
		t.Errorf("unexpected codes: %v", codes)
	}
}

func TestParseJobTypes_RawCodes(t *testing.T) {
	codes, err := parseJobTypes("F,P,T")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d", len(codes))
	}
}

func TestParseJobTypes_Invalid(t *testing.T) {
	_, err := parseJobTypes("banana")
	if err == nil {
		t.Error("expected error for invalid job type")
	}
}

func TestParseExperience_Empty(t *testing.T) {
	codes, err := parseExperience("")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if codes != nil {
		t.Errorf("expected nil, got %v", codes)
	}
}

func TestParseExperience_FriendlyNames(t *testing.T) {
	codes, err := parseExperience("entry,mid-senior,executive")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d", len(codes))
	}
	if codes[0] != "2" || codes[1] != "4" || codes[2] != "6" {
		t.Errorf("unexpected codes: %v", codes)
	}
}

func TestParseExperience_NumericCodes(t *testing.T) {
	codes, err := parseExperience("1,3,5")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("expected 3 codes, got %d: %v", len(codes), codes)
	}
	if codes[0] != "1" || codes[1] != "3" || codes[2] != "5" {
		t.Errorf("unexpected codes: %v", codes)
	}
}

func TestParseExperience_Invalid(t *testing.T) {
	_, err := parseExperience("god-mode")
	if err == nil {
		t.Error("expected error for invalid experience level")
	}
}

func TestParseSortOrder(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"recent", "R"},
		{"Recent", "R"},
		{"RECENT", "R"},
		{"r", "R"},
		{"relevant", "DD"},
		{"", "DD"},
		{"other", "DD"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseSortOrder(tt.input)
			if got != tt.expected {
				t.Errorf("for %q: expected %q, got %q", tt.input, tt.expected, got)
			}
		})
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"exact", 5, "exact"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncateStr(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}
