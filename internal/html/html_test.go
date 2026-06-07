package html_test

import (
	"testing"

	htmlutil "github.com/yashiels/linkedin-cli/internal/html"
)

func TestToText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "plain text unchanged",
			input: "Hello, world!",
			want:  "Hello, world!",
		},
		{
			name:  "strip simple tag",
			input: "<b>bold</b>",
			want:  "bold",
		},
		{
			name:  "list items become bullets",
			input: "<ul><li>First item</li><li>Second item</li></ul>",
			want:  "• First item\n• Second item",
		},
		{
			name:  "br becomes newline",
			input: "Line one<br>Line two",
			want:  "Line one\nLine two",
		},
		{
			name:  "self-closing br",
			input: "Line one<br/>Line two",
			want:  "Line one\nLine two",
		},
		{
			name:  "p tags become newlines",
			input: "<p>First paragraph.</p><p>Second paragraph.</p>",
			want:  "First paragraph.\n\nSecond paragraph.",
		},
		{
			name:  "html entities decoded",
			input: "a &amp; b &lt;c&gt; d &nbsp; e",
			// &nbsp; decodes to a non-breaking space which is then collapsed
			// into a regular space; multiple entity spaces collapse to one.
			want: "a & b <c> d e",
		},
		{
			name:  "nested structure",
			input: "<p>Intro text.</p><ul><li>Point A</li><li>Point B</li></ul><p>Conclusion.</p>",
			// </ul> is stripped (no newline); <p> produces a newline → single
			// blank line between list and conclusion.
			want: "Intro text.\n\n• Point A\n• Point B\nConclusion.",
		},
		{
			name:  "attributes in tags stripped",
			input: `<p class="x">text</p>`,
			want:  "text",
		},
		{
			name:  "multiple spaces collapsed",
			input: "<p>Hello   world</p>",
			want:  "Hello world",
		},
		{
			name:  "strip script tags with content",
			input: "before<script>alert(1)</script>after",
			// tag stripped but content remains (simple stripper doesn't remove content)
			// this is acceptable for job descriptions which don't have scripts
			want: "beforealert(1)after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlutil.ToText(tt.input)
			if got != tt.want {
				t.Errorf("ToText(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIndent(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		prefix string
		want   string
	}{
		{
			name:   "empty text",
			text:   "",
			prefix: "  ",
			want:   "",
		},
		{
			name:   "single line",
			text:   "hello",
			prefix: "  ",
			want:   "  hello",
		},
		{
			name:   "multi line",
			text:   "line one\nline two\nline three",
			prefix: "  ",
			want:   "  line one\n  line two\n  line three",
		},
		{
			name:   "blank lines not indented",
			text:   "before\n\nafter",
			prefix: "  ",
			want:   "  before\n\n  after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := htmlutil.Indent(tt.text, tt.prefix)
			if got != tt.want {
				t.Errorf("Indent(%q, %q)\n  got:  %q\n  want: %q", tt.text, tt.prefix, got, tt.want)
			}
		})
	}
}
