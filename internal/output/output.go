// Package output provides terminal output formatters for the lnk CLI.
// It respects NO_COLOR, --no-color, and TTY detection.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// Format selects the output mode.
type Format int

const (
	FormatAuto  Format = iota // Choose based on TTY + flags.
	FormatTable               // Aligned columns with colours.
	FormatJSON                // Pretty-printed JSON.
	FormatPlain               // Tab-separated, one record per line.
)

// Writer is the output abstraction used by all lnk commands.
type Writer struct {
	out     io.Writer
	errOut  io.Writer
	format  Format
	noColor bool
	quiet   bool
}

// Option configures a Writer.
type Option func(*Writer)

// WithFormat sets the output format explicitly.
func WithFormat(f Format) Option { return func(w *Writer) { w.format = f } }

// WithNoColor disables ANSI colour codes.
func WithNoColor(v bool) Option { return func(w *Writer) { w.noColor = v } }

// WithQuiet suppresses informational output.
func WithQuiet(v bool) Option { return func(w *Writer) { w.quiet = v } }

// WithStdout overrides the output writer (default os.Stdout).
func WithStdout(out io.Writer) Option { return func(w *Writer) { w.out = out } }

// New creates a Writer with the given options.
func New(opts ...Option) *Writer {
	w := &Writer{
		out:    os.Stdout,
		errOut: os.Stderr,
	}
	for _, o := range opts {
		o(w)
	}
	// Honour NO_COLOR env var (https://no-color.org/).
	if os.Getenv("NO_COLOR") != "" {
		w.noColor = true
	}
	// Disable colour when not writing to a TTY.
	if !isTTY(w.out) {
		w.noColor = true
	}
	if w.noColor {
		color.NoColor = true
	}
	return w
}

// isTTY reports whether w is a terminal file descriptor.
func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// IsColorEnabled returns true when colour output is active.
func (w *Writer) IsColorEnabled() bool { return !w.noColor }

// EffectiveFormat resolves the active format, defaulting to Table for TTYs
// and Plain for pipes.
func (w *Writer) EffectiveFormat() Format {
	if w.format != FormatAuto {
		return w.format
	}
	if isTTY(w.out) {
		return FormatTable
	}
	return FormatPlain
}

// --- Table output ---

// Column defines a table column.
type Column struct {
	Header string
	// Color is an optional fatih/color attribute applied to cell values.
	Color *color.Color
}

// Table writes rows as an aligned column table to stdout.
// headers is the ordered list of column definitions; rows is a slice of
// string slices where each inner slice corresponds to one row.
func (w *Writer) Table(cols []Column, rows [][]string) {
	if w.quiet {
		return
	}
	tw := tabwriter.NewWriter(w.out, 0, 0, 2, ' ', 0)

	// Header row.
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = strings.ToUpper(c.Header)
	}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))

	// Separator.
	seps := make([]string, len(cols))
	for i, c := range cols {
		seps[i] = strings.Repeat("─", utf8.RuneCountInString(c.Header))
	}
	fmt.Fprintln(tw, strings.Join(seps, "\t"))

	// Data rows.
	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, cell := range row {
			if i < len(cols) && cols[i].Color != nil && !w.noColor {
				cells[i] = cols[i].Color.Sprint(cell)
			} else if i < len(cols) {
				cells[i] = cell
			}
		}
		// Pad if row is shorter than columns.
		for i := len(row); i < len(cols); i++ {
			cells[i] = ""
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}

	tw.Flush()
}

// --- JSON output ---

// JSON pretty-prints v to stdout.
func (w *Writer) JSON(v interface{}) error {
	enc := json.NewEncoder(w.out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// --- Plain output ---

// Plain writes a tab-separated record to stdout.
// Each call to Plain writes one line; fields are joined by tabs.
// This format is stable and suitable for piping into awk/cut/etc.
func (w *Writer) Plain(fields ...string) {
	fmt.Fprintln(w.out, strings.Join(fields, "\t"))
}

// --- Utility ---

// Info writes an informational message to stderr (suppressed with --quiet).
func (w *Writer) Info(format string, args ...interface{}) {
	if w.quiet {
		return
	}
	fmt.Fprintf(w.errOut, format+"\n", args...)
}

// Error writes an error message to stderr (never suppressed).
func (w *Writer) Error(format string, args ...interface{}) {
	fmt.Fprintf(w.errOut, "error: "+format+"\n", args...)
}

// Warn writes a warning message to stderr (suppressed with --quiet).
func (w *Writer) Warn(format string, args ...interface{}) {
	if w.quiet {
		return
	}
	fmt.Fprintf(w.errOut, "warning: "+format+"\n", args...)
}
