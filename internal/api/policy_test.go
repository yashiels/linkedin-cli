package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/safety"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type controlledBody struct {
	reader io.Reader
	close  func()
}

func (b *controlledBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *controlledBody) Close() error               { b.close(); return nil }

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("BODY-READ-SENTINEL") }

func TestRefusalPersistsAcrossClientsForEveryWrapper(t *testing.T) {
	for _, tc := range requestWrappers() {
		for _, status := range []int{429, 401, 403, 999, 302} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, status), func(t *testing.T) {
				count := 0
				c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { count++; return fixtureResponse(status, "/authwall"), nil }))
				if err := tc.call(c); err == nil {
					t.Fatal("first refusal succeeded")
				}
				fresh := New(auth.Credentials{}, WithHTTPClient(c.http), func(next *Client) { next.policy = c.policy })
				if err := tc.call(fresh); err == nil || count != 1 {
					t.Fatalf("refusal not persistent: %v calls=%d", err, count)
				}
			})
		}
	}
}
func TestRefusalSurvivesBodyFailures(t *testing.T) {
	for _, status := range []int{200, 429, 401, 403, 999, 302} {
		for _, kind := range []string{"read", "oversize"} {
			t.Run(fmt.Sprintf("%d/%s", status, kind), func(t *testing.T) {
				closed := 0
				var reader io.Reader = brokenReader{}
				if kind == "oversize" {
					reader = io.LimitReader(infiniteReader{}, safety.MaxBody+2)
				}
				c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					resp := fixtureResponse(status, "/authwall")
					resp.Body = &controlledBody{reader: reader, close: func() { closed++ }}
					return resp, nil
				}))
				_, err := c.Ping()
				expected := 4
				if status == 429 {
					expected = 5
				} else if status != 200 {
					expected = 3
				}
				var coded *types.LnkError
				if !errors.As(err, &coded) || coded.Code != expected || closed != 1 {
					t.Fatalf("error=%v closed=%d", err, closed)
				}
				state, e := c.policy.Status(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				if state.Admissions != 1 {
					t.Fatal("failed read not counted")
				}
				if status != 200 && state.Block == "" && state.Cooldown.IsZero() {
					t.Fatal("lost refusal during read error")
				}
			})
		}
	}
}

type infiniteReader struct{}

func (infiniteReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'x'
	}
	return len(b), nil
}

type blockingReader struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingReader) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return 0, io.EOF
}

func TestBodyReadRetainsLeaseAndCancellationSendsZero(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	p, err := safety.New(root, safety.Config{})
	if err != nil {
		t.Fatal(err)
	}
	body := &blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	var calls atomic.Int32
	var closed atomic.Bool
	c := New(auth.Credentials{}, WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		resp := fixtureResponse(200, "")
		resp.Body = &controlledBody{reader: body, close: func() { closed.Store(true) }}
		return resp, nil
	})}), func(c *Client) { c.policy = p })
	done := make(chan error, 1)
	go func() {
		resp, err := c.send(context.Background(), readOperation, baseURL+"/voyager/api/me", nil)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-body.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("body not entered")
	}
	fresh := New(auth.Credentials{}, WithHTTPClient(c.http), func(c *Client) { c.policy = p })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = fresh.send(ctx, readOperation, baseURL+"/voyager/api/me", nil)
	if err == nil || calls.Load() != 1 || closed.Load() {
		t.Fatal("lease released before original body completed")
	}
	close(body.release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if !closed.Load() {
		t.Fatal("original body not closed")
	}
}

func TestAdmissionCountsFailedSends(t *testing.T) {
	c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("failed") }))
	for i := 0; i < 100; i++ {
		if _, err := c.Ping(); err == nil {
			t.Fatal("failed send succeeded")
		}
	}
	_, err := c.Ping()
	var coded *types.LnkError
	if !errors.As(err, &coded) || coded.Code != 5 {
		t.Fatalf("failed sends did not consume quota: %v", err)
	}
}

func TestJSONFailuresAndEmptyMutation(t *testing.T) {
	for _, body := range []string{"<html>RESPONSE-SENTINEL</html>", `{"errors":[{"message":"RESPONSE-SENTINEL"}]}`, `{"errors":"RESPONSE-SENTINEL"}`, `{"bad":`, "null", "[]"} {
		c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			r := fixtureResponse(200, "")
			r.Body = io.NopCloser(strings.NewReader(body))
			return r, nil
		}))
		_, err := c.Get("/voyager/api/me", nil)
		if err == nil || strings.Contains(err.Error(), "SENTINEL") {
			t.Fatalf("bad JSON accepted or leaked: %v", err)
		}
		status, e := c.policy.Status(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if status.Block != "" || !status.Cooldown.IsZero() {
			t.Fatal("body text latched auth stop")
		}
	}
	c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		r := fixtureResponse(204, "")
		r.Body = io.NopCloser(strings.NewReader(""))
		return r, nil
	}))
	if _, err := c.Post("/voyager/api/fixture", nil); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticSentinelMatrix(t *testing.T) {
	for _, flags := range []struct{ verbose, debug bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		for _, kind := range []string{"success", "429", "401", "403", "999", "redirect", "oversize", "malformed", "transport", "url-error", "body-error"} {
			t.Run(fmt.Sprintf("%v/%s", flags, kind), func(t *testing.T) {
				var logs bytes.Buffer
				c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
					switch kind {
					case "transport":
						return nil, errors.New("TRANSPORT-SENTINEL https://sensitive.invalid/FULL-URL-SENTINEL")
					case "url-error":
						return nil, &url.Error{Op: "GET", URL: "https://sensitive.invalid/FULL-URL-SENTINEL", Err: errors.New("URL-ERROR-SENTINEL")}
					}
					r := fixtureResponse(200, "")
					switch kind {
					case "429":
						r.StatusCode = 429
					case "401":
						r.StatusCode = 401
					case "403":
						r.StatusCode = 403
					case "999":
						r.StatusCode = 999
					case "redirect":
						r.StatusCode = 302
						r.Header.Set("Location", "/login?REDIRECT-SENTINEL")
					case "oversize":
						r.Body = io.NopCloser(io.LimitReader(infiniteReader{}, safety.MaxBody+1))
					case "malformed":
						r.Body = io.NopCloser(strings.NewReader("<html>BODY-SENTINEL</html>"))
					case "body-error":
						r.Body = io.NopCloser(brokenReader{})
					}
					return r, nil
				}), WithVerbose(flags.verbose), WithDebug(flags.debug), WithErrWriter(&logs))
				root, rootErr := filepath.EvalSymlinks(t.TempDir())
				if rootErr != nil {
					t.Fatal(rootErr)
				}
				if err := os.Chmod(root, 0700); err != nil {
					t.Fatal(err)
				}
				policy, policyErr := safety.New(root, safety.Config{})
				if policyErr != nil {
					t.Fatal(policyErr)
				}
				c.policy = policy
				c.creds = auth.Credentials{LiAt: "COOKIE-SENTINEL", CSRFToken: "CSRF-SENTINEL"}
				_, err := c.PostJSON("/voyager/api/fixture", map[string]string{"key": "QUERY-SENTINEL"}, map[string]string{"field": "REQUEST-BODY-SENTINEL", "urn": "urn:li:PERSONAL-SENTINEL"})
				if (kind == "success") != (err == nil) {
					t.Fatalf("unexpected success/error: %v", err)
				}
				state, _ := c.policy.Status(context.Background())
				persisted, readErr := os.ReadFile(filepath.Join(root, "state.json"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				diagnostics := fmt.Sprint(err) + logs.String() + fmt.Sprint(state) + string(persisted)
				for _, sentinel := range []string{"COOKIE-SENTINEL", "CSRF-SENTINEL", "QUERY-SENTINEL", "REQUEST-BODY-SENTINEL", "PERSONAL-SENTINEL", "BODY-SENTINEL", "FULL-URL-SENTINEL", "TRANSPORT-SENTINEL", "URL-ERROR-SENTINEL", "REDIRECT-SENTINEL", "BODY-READ-SENTINEL"} {
					if strings.Contains(diagnostics, sentinel) {
						t.Fatalf("diagnostic leaked %s", sentinel)
					}
				}
			})
		}
	}
}

func TestProductionClientInjectionCannotBypassPolicy(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("LNK_CONFIG", filepath.Join(home, "alternate.toml"))
	if err = os.WriteFile(filepath.Join(home, "alternate.toml"), []byte("[safety]\nbudget=1\ndelay_seconds=6\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(200, ""), nil })}
	first := New(auth.Credentials{LiAt: "FIRST"}, WithHTTPClient(transport))
	if _, err = first.Ping(); err != nil {
		t.Fatal(err)
	}
	second := New(auth.Credentials{LiAt: "SECOND"}, WithHTTPClient(transport))
	_, err = second.Ping()
	var coded *types.LnkError
	if !errors.As(err, &coded) || coded.Code != 5 || calls != 1 {
		t.Fatalf("injection bypassed policy: %v calls=%d", err, calls)
	}
	if _, err = os.Stat(filepath.Join(home, ".local", "share", "lnk", "safety", "state.json")); err != nil {
		t.Fatal("config moved policy root", err)
	}
}

func TestNegativeControlHarness(t *testing.T) {
	if os.Getenv("LNK_TEST_NEGATIVE_CONTROL") == "1" {
		t.Fatal("intentional false assertion")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestNegativeControlHarness$")
	command.Env = append(os.Environ(), "LNK_TEST_NEGATIVE_CONTROL=1")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "intentional false assertion") || strings.Contains(string(output), "PASS") {
		t.Fatalf("negative-control harness gave false success: %v %s", err, output)
	}
}

func TestAPIExchangeSubprocess(t *testing.T) {
	root := os.Getenv("LNK_TEST_EXCHANGE_ROOT")
	if root == "" {
		return
	}
	clockPath := filepath.Join(filepath.Dir(root), "clock")
	readClock := func() time.Time {
		b, err := os.ReadFile(clockPath)
		if err != nil {
			panic(err)
		}
		var now time.Time
		if err = now.UnmarshalText(b); err != nil {
			panic(err)
		}
		return now
	}
	policy, err := safety.New(root, safety.Config{Budget: 3, Now: readClock, Wait: func(ctx context.Context, d time.Duration) error {
		if d < time.Second {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
		data, _ := readClock().Add(d).MarshalText()
		return os.WriteFile(clockPath, data, 0600)
	}})
	if err != nil {
		t.Fatal(err)
	}
	c := New(auth.Credentials{}, WithHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		local, err := http.NewRequestWithContext(req.Context(), http.MethodGet, os.Getenv("LNK_TEST_EXCHANGE_URL"), nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultTransport.RoundTrip(local)
	})}), func(c *Client) { c.policy = policy })
	_, err = c.Ping()
	if err != nil {
		var coded *types.LnkError
		if !errors.As(err, &coded) || coded.Code != 5 {
			t.Fatal(err)
		}
		fmt.Println("REFUSED")
		return
	}
	fmt.Println("SENT")
}

func TestAPIProcessesSerializeCompleteHTTPExchanges(t *testing.T) {
	for run := 0; run < 5; run++ {
		t.Run(fmt.Sprint(run), func(t *testing.T) {
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			data, _ := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC).MarshalText()
			if err = os.WriteFile(filepath.Join(parent, "clock"), data, 0600); err != nil {
				t.Fatal(err)
			}
			var active, peak, calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				current := active.Add(1)
				calls.Add(1)
				for prior := peak.Load(); current > prior; prior = peak.Load() {
					if peak.CompareAndSwap(prior, current) {
						break
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(200)
				w.Write([]byte(`{"ok":`))
				w.(http.Flusher).Flush()
				time.Sleep(10 * time.Millisecond)
				active.Add(-1)
				w.Write([]byte(`true}`))
			}))
			defer server.Close()
			var wg sync.WaitGroup
			var sent, refused atomic.Int32
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					command := exec.Command(os.Args[0], "-test.run=^TestAPIExchangeSubprocess$", "-test.count=1")
					command.Env = append(os.Environ(), "HOME="+parent, "LNK_TEST_EXCHANGE_ROOT="+filepath.Join(parent, "policy"), "LNK_TEST_EXCHANGE_URL="+server.URL)
					output, err := command.CombinedOutput()
					if err != nil {
						t.Errorf("child failed %v %s", err, output)
					}
					if strings.Contains(string(output), "SENT") {
						sent.Add(1)
					}
					if strings.Contains(string(output), "REFUSED") {
						refused.Add(1)
					}
				}()
			}
			wg.Wait()
			if calls.Load() != 3 || sent.Load() != 3 || refused.Load() != 3 || peak.Load() != 1 {
				t.Fatalf("calls=%d sent=%d refused=%d peak=%d", calls.Load(), sent.Load(), refused.Load(), peak.Load())
			}
		})
	}
}

func TestUnsafeStateSendsZero(t *testing.T) {
	for _, kind := range []string{"corrupt", "missing", "symlink", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("LNK_CONFIG", filepath.Join(home, "missing.toml"))
			calls := 0
			c := New(auth.Credentials{}, WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(200, ""), nil })}))
			if _, err = c.Ping(); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(home, ".local", "share", "lnk", "safety", "state.json")
			switch kind {
			case "corrupt":
				err = os.WriteFile(state, []byte("broken"), 0600)
			case "missing":
				err = os.Remove(state)
			case "symlink":
				if err = os.Remove(state); err == nil {
					err = os.Symlink("/dev/null", state)
				}
			case "permissions":
				err = os.Chmod(state, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Ping()
			var coded *types.LnkError
			if !errors.As(err, &coded) || coded.Code != 4 || calls != 1 {
				t.Fatalf("unsafe state sent: %v calls=%d", err, calls)
			}
		})
	}
}

func TestRequestedProfileDataStillReturnedWithoutDiagnosticEcho(t *testing.T) {
	const body = `{"firstName":"REQUESTED-PERSONAL-DATA","headline":"REQUESTED-PERSONAL-DATA"}`
	var logs bytes.Buffer
	c := fixtureClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		resp := fixtureResponse(200, "")
		resp.Body = io.NopCloser(strings.NewReader(body))
		return resp, nil
	}), WithDebug(true), WithVerbose(true), WithErrWriter(&logs))
	raw, err := c.GetProfileByIdentity("fixture")
	if err != nil || string(raw) != body || strings.Contains(logs.String(), "REQUESTED-PERSONAL-DATA") {
		t.Fatalf("data contract or diagnostic redaction failed: %v", err)
	}
}
