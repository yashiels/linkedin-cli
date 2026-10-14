package cmd

import (
	"testing"

	"github.com/yashiels/linkedin-cli/internal/types"
)

func TestTypedHTTPStatusPreservesProfileFallbackContract(t *testing.T) {
	for _, tc := range []struct {
		code   int
		reason types.DiagnosticReason
		stop   bool
	}{
		{1, types.ReasonHTTPStatus, false}, {1, types.ReasonJSON, true}, {1, types.ReasonGraphQL, true}, {3, types.ReasonRefusal, true}, {4, types.ReasonTransport, true}, {5, types.ReasonRefusal, true},
	} {
		err := types.WithRequestDetails(types.NewError(tc.code, "untrusted"), "GET", "read", 410, tc.reason)
		if got := isRequestFailure(err); got != tc.stop {
			t.Fatalf("code=%d reason=%d stop=%t", tc.code, tc.reason, got)
		}
	}
}
