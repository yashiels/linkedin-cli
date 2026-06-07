// Package html provides a simple HTML-to-plain-text converter for
// LinkedIn job descriptions. It is intentionally lightweight — no full
// parser dependency — and handles only the subset of HTML that LinkedIn
// returns in job description fields.
package html

import (
	gohtml "html"
	"regexp"
	"strings"
)

var (
	// Block-level elements that become newlines.
	reLiOpen  = regexp.MustCompile(`(?i)<li(\s[^>]*)?>`)
	reBr      = regexp.MustCompile(`(?i)<br\s*/?>`)
	rePOpen   = regexp.MustCompile(`(?i)<p(\s[^>]*)?>`)
	rePClose  = regexp.MustCompile(`(?i)</p\s*>`)
	reDiv     = regexp.MustCompile(`(?i)</?div(\s[^>]*)?>`)
	reH       = regexp.MustCompile(`(?i)<h[1-6](\s[^>]*)?>`)
	reHClose  = regexp.MustCompile(`(?i)</h[1-6]\s*>`)
	reUlOl    = regexp.MustCompile(`(?i)</?[uo]l(\s[^>]*)?>`)
	reLiClose = regexp.MustCompile(`(?i)</li\s*>`)

	// Strip everything else.
	reTag = regexp.MustCompile(`<[^>]+>`)

	// Collapse horizontal whitespace within a line.
	reHSpace = regexp.MustCompile(`[ \t]+`)

	// Collapse runs of blank lines down to at most two newlines.
	reMultiNL = regexp.MustCompile(`\n{3,}`)
)

// ToText converts the HTML string h to human-readable plain text.
//
// Rules applied in order:
//   - <li …>  → newline + "• " prefix
//   - </li>   → stripped
//   - <br>    → newline
//   - <p …>   → newline
//   - </p>    → newline
//   - <h1>…<h6> / <div> → newline
//   - <ul>/<ol> and their closing tags → stripped
//   - All remaining tags → stripped
//   - HTML entities decoded (via standard library)
//   - Horizontal whitespace collapsed
//   - More than two consecutive blank lines collapsed to two
func ToText(h string) string {
	if h == "" {
		return ""
	}

	s := h

	// List items → bullet prefix.
	s = reLiOpen.ReplaceAllString(s, "\n• ")
	s = reLiClose.ReplaceAllString(s, "")

	// Line-break elements.
	s = reBr.ReplaceAllString(s, "\n")
	s = rePOpen.ReplaceAllString(s, "\n")
	s = rePClose.ReplaceAllString(s, "\n")
	s = reDiv.ReplaceAllString(s, "\n")
	s = reH.ReplaceAllString(s, "\n")
	s = reHClose.ReplaceAllString(s, "\n")

	// Block-level containers that don't need special text.
	s = reUlOl.ReplaceAllString(s, "")

	// Strip all remaining tags.
	s = reTag.ReplaceAllString(s, "")

	// Decode HTML entities (&amp; → &, &lt; → <, &nbsp; → space, etc.).
	s = gohtml.UnescapeString(s)

	// Replace non-breaking spaces with regular spaces.
	s = strings.ReplaceAll(s, " ", " ")

	// Clean up per-line whitespace.
	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = reHSpace.ReplaceAllString(line, " ")
		line = strings.TrimRight(line, " \t")
		cleaned = append(cleaned, line)
	}
	s = strings.Join(cleaned, "\n")

	// Collapse excess blank lines.
	s = reMultiNL.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// Indent prefixes every non-empty line of text with the given prefix string.
// Useful for formatting job description output with visual indentation.
func Indent(text, prefix string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
