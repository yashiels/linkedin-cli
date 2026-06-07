package restli_test

import (
	"testing"

	"github.com/yashiels/linkedin-cli/internal/restli"
)

func TestEncodeBool(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{true, "true"},
		{false, "false"},
	}
	for _, tt := range tests {
		got, err := restli.Encode(tt.input)
		if err != nil {
			t.Fatalf("Encode(%v) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("Encode(%v) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeIntegers(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{int(0), "0"},
		{int(10), "10"},
		{int64(-42), "-42"},
		{uint(25), "25"},
	}
	for _, tt := range tests {
		got, err := restli.Encode(tt.input)
		if err != nil {
			t.Fatalf("Encode(%v) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("Encode(%v) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"hello", "hello"},
		{"graduate criminology", "graduate%20criminology"},
		// URN colons are kept as-is inside RestLi values
		{"urn:li:fsd_geo:104035573", "urn%3Ali%3Afsd_geo%3A104035573"},
		{"JOB_SEARCH_PAGE_SEARCH_BUTTON", "JOB_SEARCH_PAGE_SEARCH_BUTTON"},
		{"R", "R"},
		{"JOBS", "JOBS"},
	}
	for _, tt := range tests {
		got, err := restli.Encode(tt.input)
		if err != nil {
			t.Fatalf("Encode(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("Encode(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeList(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{[]string{"a", "b", "c"}, "List(a,b,c)"},
		{[]string{}, "List()"},
		{[]bool{true, false}, "List(true,false)"},
		{[]int{1, 2, 3}, "List(1,2,3)"},
		// Empty-string items become ''
		{[]string{""}, "List('')"},
		{[]string{"R"}, "List(R)"},
		{[]string{"true"}, "List(true)"},
	}
	for _, tt := range tests {
		got, err := restli.Encode(tt.input)
		if err != nil {
			t.Fatalf("Encode(%v) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("Encode(%v) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeMap(t *testing.T) {
	tests := []struct {
		input map[string]interface{}
		want  string
	}{
		{
			map[string]interface{}{"key": "value"},
			"(key:value)",
		},
		{
			map[string]interface{}{},
			"()",
		},
		{
			// Keys are sorted alphabetically when SortKeys=true (the default).
			map[string]interface{}{"b": 2, "a": 1},
			"(a:1,b:2)",
		},
		{
			map[string]interface{}{"spellCorrectionEnabled": true},
			"(spellCorrectionEnabled:true)",
		},
	}
	for _, tt := range tests {
		got, err := restli.Encode(tt.input)
		if err != nil {
			t.Fatalf("Encode error: %v", err)
		}
		if got != tt.want {
			t.Errorf("Encode(%v) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestEncodeNested(t *testing.T) {
	input := map[string]interface{}{
		"locationUnion": map[string]interface{}{
			"geoUrn": "urn:li:fsd_geo:104035573",
		},
	}
	got, err := restli.Encode(input)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	want := "(locationUnion:(geoUrn:urn%3Ali%3Afsd_geo%3A104035573))"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// TestLiveCaptureEquivalent reproduces the real query captured from LinkedIn Android.
// The example from the spec (spaces + nested + list-of-booleans + list-of-empty):
//
//	(query:(keywords:graduate%20criminology%20,...),includeJobState:true,count:10,start:0)
//
// We compare field by field because map ordering is non-deterministic in Go, so
// we build the inner query sub-map separately then verify its encoding.
func TestLiveCaptureQuery(t *testing.T) {
	selectedFilters := map[string]interface{}{
		"timePostedRange":   []string{""},
		"sortBy":            []string{"R"},
		"resultType":        []string{"JOBS"},
		"applyWithLinkedin": []bool{true},
	}

	sfEncoded, err := restli.Encode(selectedFilters)
	if err != nil {
		t.Fatalf("selectedFilters encode: %v", err)
	}

	// Verify individual nested pieces.
	if want := "List('')"; !contains(sfEncoded, "timePostedRange:"+want) {
		t.Errorf("expected timePostedRange:List('') in %q", sfEncoded)
	}
	if want := "List(R)"; !contains(sfEncoded, "sortBy:"+want) {
		t.Errorf("expected sortBy:List(R) in %q", sfEncoded)
	}
	if want := "List(JOBS)"; !contains(sfEncoded, "resultType:"+want) {
		t.Errorf("expected resultType:List(JOBS) in %q", sfEncoded)
	}
	if want := "List(true)"; !contains(sfEncoded, "applyWithLinkedin:"+want) {
		t.Errorf("expected applyWithLinkedin:List(true) in %q", sfEncoded)
	}

	query := map[string]interface{}{
		"keywords":               "graduate criminology ",
		"locationUnion":          map[string]interface{}{"geoUrn": "urn:li:fsd_geo:104035573"},
		"origin":                 "JOB_SEARCH_PAGE_SEARCH_BUTTON",
		"selectedFilters":        selectedFilters,
		"spellCorrectionEnabled": true,
	}

	qEncoded, err := restli.Encode(query)
	if err != nil {
		t.Fatalf("query encode: %v", err)
	}

	if !contains(qEncoded, "keywords:graduate%20criminology%20") {
		t.Errorf("expected URL-encoded keywords in %q", qEncoded)
	}
	if !contains(qEncoded, "spellCorrectionEnabled:true") {
		t.Errorf("expected spellCorrectionEnabled:true in %q", qEncoded)
	}
	if !contains(qEncoded, "geoUrn:urn%3Ali%3Afsd_geo%3A104035573") {
		t.Errorf("expected URL-encoded geoUrn in %q", qEncoded)
	}
}

func TestEncodeStruct(t *testing.T) {
	type Inner struct {
		Value string `json:"value"`
	}
	type Outer struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		Inner Inner  `json:"inner"`
	}

	v := Outer{
		Name:  "test",
		Count: 5,
		Inner: Inner{Value: "hello"},
	}

	got, err := restli.Encode(v)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}
	want := "(count:5,inner:(value:hello),name:test)"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestEncodeNil(t *testing.T) {
	got, err := restli.Encode(nil)
	if err != nil {
		t.Fatalf("Encode(nil) error: %v", err)
	}
	if got != "null" {
		t.Errorf("Encode(nil) = %q; want %q", got, "null")
	}
}

func TestMustEncode(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MustEncode panicked: %v", r)
		}
	}()
	got := restli.MustEncode(map[string]interface{}{"k": "v"})
	if got != "(k:v)" {
		t.Errorf("MustEncode = %q; want %q", got, "(k:v)")
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		(s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
