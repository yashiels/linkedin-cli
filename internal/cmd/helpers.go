package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/output"
	"github.com/yashiels/linkedin-cli/internal/types"
)

// newAPIClient loads credentials and constructs an API client, wiring
// the verbose/debug flags from the root command.
func newAPIClient(cmd *cobra.Command) (*api.Client, error) {
	store, err := auth.Default()
	if err != nil {
		return nil, err
	}
	creds, err := store.Load()
	if err != nil {
		return nil, err
	}
	if creds.LiAt == "" {
		return nil, types.AuthError("not logged in — run 'lnk auth login' first")
	}

	verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
	debug, _ := cmd.Root().PersistentFlags().GetBool("debug")

	return api.New(creds,
		api.WithVerbose(verbose),
		api.WithDebug(debug),
	), nil
}

// newOutputWriter builds an output.Writer from root-level persistent flags.
func newOutputWriter(cmd *cobra.Command) *output.Writer {
	jsonFmt, _ := cmd.Root().PersistentFlags().GetBool("json")
	plainFmt, _ := cmd.Root().PersistentFlags().GetBool("plain")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")

	var fmt output.Format
	switch {
	case jsonFmt:
		fmt = output.FormatJSON
	case plainFmt:
		fmt = output.FormatPlain
	default:
		fmt = output.FormatAuto
	}

	return output.New(
		output.WithFormat(fmt),
		output.WithNoColor(noColor),
		output.WithQuiet(quiet),
	)
}

// isJSONMode returns true when --json was passed.
func isJSONMode(cmd *cobra.Command) bool {
	v, _ := cmd.Root().PersistentFlags().GetBool("json")
	return v
}

// --- JSON traversal helpers ---

// jget walks a JSON object by successive string keys.
// Returns nil when any key is missing or the value is not an object.
func jget(raw json.RawMessage, keys ...string) json.RawMessage {
	if len(keys) == 0 || raw == nil {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	v, ok := m[keys[0]]
	if !ok {
		return nil
	}
	return jget(v, keys[1:]...)
}

// jstr extracts a string from a JSON value, returning "" on failure.
func jstr(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// jint extracts an integer from a JSON value, returning 0 on failure.
func jint(raw json.RawMessage) int {
	if raw == nil {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0
	}
	return n
}

// jbool extracts a bool from a JSON value, returning false on failure.
func jbool(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// jelems extracts the "elements" array from a JSON object.
// Returns nil if the object or elements key is absent.
func jelems(raw json.RawMessage) []json.RawMessage {
	e := jget(raw, "elements")
	if e == nil {
		return nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(e, &arr); err != nil {
		return nil
	}
	return arr
}

// jdataElems finds the first key under "data" and returns its "elements" array.
func jdataElems(raw json.RawMessage) []json.RawMessage {
	data := jget(raw, "data")
	if data == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	for _, v := range m {
		if elems := jelems(v); elems != nil {
			return elems
		}
	}
	return nil
}

// formatYear returns "YYYY" from a LinkedIn date object {"year": N, "month": M}.
func formatYear(raw json.RawMessage) string {
	y := jint(jget(raw, "year"))
	if y == 0 {
		return ""
	}
	return itoa(y)
}

// itoa converts an int to string without importing strconv in every file.
func itoa(n int) string {
	if n == 0 {
		return ""
	}
	// Fast path for common years.
	b := make([]byte, 0, 4)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// formatConnections returns a display-friendly connection count string.
// LinkedIn caps displayed connections at 500.
func formatConnections(n int) string {
	if n == 0 {
		return ""
	}
	if n >= 500 {
		return "500+ connections"
	}
	return itoa(n) + " connections"
}
