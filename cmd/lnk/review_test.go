package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yashiels/linkedin-cli/internal/api"
	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type reviewTransport func(*http.Request) (*http.Response, error)

func (f reviewTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestAPIStatusDiagnosticsAcrossModes(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		for _, plain := range []bool{false, true} {
			for _, verbose := range []bool{false, true} {
				for _, debug := range []bool{false, true} {
					for _, status := range []int{418, 500, 401, 429, 999, 302} {
						for _, method := range []string{"GET", "POST", "PING"} {
							t.Run(fmt.Sprintf("%t/%t/%t/%t/%d/%s", jsonMode, plain, verbose, debug, status, method), func(t *testing.T) {
								isolatedHome(t)
								var logs bytes.Buffer
								client := api.New(auth.Credentials{LiAt: "COOKIE-SENTINEL", CSRFToken: "CSRF-SENTINEL"}, api.WithHTTPClient(&http.Client{Transport: reviewTransport(func(*http.Request) (*http.Response, error) {
									headers := make(http.Header)
									headers.Set("Location", "/authwall?URL-SENTINEL")
									return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("BODY-SENTINEL"))}, nil
								})}), api.WithVerbose(verbose), api.WithDebug(debug), api.WithErrWriter(&logs))
								var err error
								switch method {
								case "GET":
									_, err = client.Get("/voyager/api/fixture", nil)
								case "POST":
									_, err = client.Post("/voyager/api/fixture", map[string]string{"private": "REQUEST-SENTINEL"})
								case "PING":
									_, err = client.Ping()
								}
								if err == nil {
									t.Fatal("unexpected status accepted")
								}
								root := newRootCmd()
								for flag, value := range map[string]bool{"json": jsonMode, "plain": plain, "verbose": verbose, "debug": debug} {
									if flagErr := root.PersistentFlags().Set(flag, fmt.Sprint(value)); flagErr != nil {
										t.Fatal(flagErr)
									}
								}
								var output bytes.Buffer
								got := writeError(root, &output, fmt.Errorf("WRAPPER-SENTINEL: %w", err))
								expected := 1
								if status == 401 || status == 999 || status == 302 {
									expected = 3
								} else if status == 429 {
									expected = 5
								}
								if got != expected {
									t.Fatalf("code=%d expected=%d", got, expected)
								}
								if strings.Contains(output.String()+logs.String(), "SENTINEL") {
									t.Fatal("private diagnostic leaked")
								}
								httpMethod := method
								operation := "normalized_read"
								if method == "POST" {
									operation = "post"
								} else if method == "PING" {
									httpMethod = "GET"
									operation = "read"
								}
								if jsonMode {
									var fields map[string]any
									if err = json.Unmarshal(output.Bytes(), &fields); err != nil {
										t.Fatal(err)
									}
									if fields["status"] != float64(status) || fields["method"] != httpMethod || fields["operation"] != operation {
										t.Fatalf("structured HTTP context lost: %v", fields)
									}
								} else {
									for _, field := range []string{fmt.Sprintf("status=%d", status), "method=" + httpMethod, "operation=" + operation} {
										if !strings.Contains(output.String(), field) {
											t.Fatalf("safe HTTP context lost: %s", output.String())
										}
									}
								}
							})
						}
					}
				}
			}
		}
	}
}

func TestUntrustedDiagnosticMetadataIsFiltered(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		isolatedHome(t)
		root := newRootCmd()
		if err := root.PersistentFlags().Set("json", fmt.Sprint(jsonMode)); err != nil {
			t.Fatal(err)
		}
		coded := &types.LnkError{Code: 4, Message: "MESSAGE-SENTINEL", Cause: fmt.Errorf("CAUSE-SENTINEL"), Reason: types.DiagnosticReason(255), Method: "METHOD-SENTINEL", Operation: "OPERATION-SENTINEL", Status: 123456789}
		var output bytes.Buffer
		if got := writeError(root, &output, coded); got != 4 {
			t.Fatal("exit code lost")
		}
		if strings.Contains(output.String(), "SENTINEL") || strings.Contains(output.String(), "123456789") {
			t.Fatal("untrusted diagnostic metadata escaped allowlist")
		}
	}
}
