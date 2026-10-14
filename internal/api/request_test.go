package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yashiels/linkedin-cli/internal/auth"
	"github.com/yashiels/linkedin-cli/internal/types"
)

type wrapperCase struct {
	name   string
	method string
	accept string
	call   func(*Client) error
}

func requestWrappers() []wrapperCase {
	return []wrapperCase{
		{"graphql", "GET", "application/json", func(c *Client) error {
			_, err := c.QueryGraphQL("fixture", "fixture.1", map[string]string{"key": "value"})
			return err
		}},
		{"get", "GET", "application/vnd.linkedin.normalized+json+2.1", func(c *Client) error { _, err := c.Get("/voyager/api/me", nil); return err }},
		{"profile", "GET", "application/json", func(c *Client) error { _, err := c.GetProfileByIdentity("fixture"); return err }},
		{"ping", "GET", "application/json", func(c *Client) error { _, err := c.Ping(); return err }},
		{"post", "POST", "application/json", func(c *Client) error {
			_, err := c.Post("/voyager/api/fixture", map[string]string{"test": "value"})
			return err
		}},
		{"postJSON", "POST", "application/json", func(c *Client) error {
			_, err := c.PostJSON("/voyager/api/fixture", nil, map[string]string{"test": "value"})
			return err
		}},
		{"delete", "DELETE", "application/json", func(c *Client) error { return c.Delete("/voyager/api/fixture") }},
		{"deleteResource", "DELETE", "application/json", func(c *Client) error { return c.DeleteResource("/voyager/api/fixture") }},
	}
}

func fixtureResponse(code int, location string) *http.Response {
	h := make(http.Header)
	if location != "" {
		h.Set("Location", location)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(strings.NewReader(`{}`))}
}

func fixtureClient(rt http.RoundTripper, opts ...Option) *Client {
	return New(auth.Credentials{LiAt: "synthetic-li-at", CSRFToken: "synthetic-csrf"}, append([]Option{WithHTTPClient(&http.Client{Transport: rt})}, opts...)...)
}

func checkTracking(t *testing.T, raw string) {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(raw))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("invalid tracking JSON: %q", raw)
	}
	values := map[string]interface{}{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		name := key.(string)
		if _, exists := values[name]; exists {
			t.Errorf("duplicate tracking key %q", name)
		}
		var value interface{}
		if err := d.Decode(&value); err != nil {
			t.Fatal(err)
		}
		values[name] = value
	}
	if _, err := d.Token(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Token(); err != io.EOF {
		t.Fatal("trailing tracking JSON")
	}
	if values["osVersion"] != "36" || values["clientVersion"] != "4.1.1209" {
		t.Errorf("incorrect tracking versions: %v", values)
	}
}

func TestRequestWrappersHeaders(t *testing.T) {
	for _, tc := range requestWrappers() {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := fixtureClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != tc.method || req.Header.Get("Accept") != tc.accept {
					t.Errorf("method/accept: %s %s", req.Method, req.Header.Get("Accept"))
				}
				if req.URL.Scheme != "https" || req.URL.Host != "www.linkedin.com" || !strings.HasPrefix(req.URL.Path, "/voyager/api/") {
					t.Error("wrong destination")
				}
				if !strings.Contains(req.Header.Get("Cookie"), "li_at=synthetic-li-at") || req.Header.Get("Csrf-Token") != "ajax:synthetic-csrf" {
					t.Error("missing credentials")
				}
				if req.Header.Get("X-Restli-Protocol-Version") != "2.0.0" {
					t.Error("missing RestLi version")
				}
				checkTracking(t, req.Header.Get("X-LI-Track"))
				if !strings.HasPrefix(req.UserAgent(), "com.linkedin.android/211700 ") {
					t.Error("incorrect legacy build")
				}
				if req.Method != "GET" && (req.GetBody != nil || req.Header.Get("Idempotency-Key") != "" || req.Header.Get("X-Idempotency-Key") != "") {
					t.Error("mutation is replayable")
				}
				if _, ok := req.Context().Deadline(); !ok {
					t.Error("request has no bounded context")
				}
				return fixtureResponse(200, ""), nil
			}))
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("admissions=%d", calls)
			}
		})
	}
}

func TestRequestRedirects(t *testing.T) {
	locations := []struct {
		target string
		auth   bool
	}{
		{"https://external.invalid/private?token=secret-target", false},
		{"/voyager/api/other?token=secret-target", false},
		{"/voyager/api/me", false},
		{"/login?token=secret-target", true},
		{"/uas/login/step", true},
		{"/authwall", true},
		{"/checkpoint/challenge/step", true},
		{"/%6cogin", true},
		{"/voyager/api/../../authwall", true},
		{"/voyager/api/%2e%2e/%2e%2e/checkpoint%2fchallenge", true},
		{"/%2561uthwall", true},
		{"/login-other", false},
		{"https://external.invalid/login", false},
		{"https://www.linkedin.com:443/login", false},
		{"http://www.linkedin.com/login", false},
		{"https://user@www.linkedin.com/login", false},
		{"https://%zz", false},
	}
	for _, tc := range requestWrappers() {
		for _, loc := range locations {
			for _, status := range []int{301, 302, 303, 307, 308} {
				t.Run(fmt.Sprintf("%s/%d/%s", tc.name, status, loc.target), func(t *testing.T) {
					calls := 0
					var logs bytes.Buffer
					c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
						calls++
						if calls > 1 {
							return fixtureResponse(200, ""), nil
						}
						return fixtureResponse(status, loc.target), nil
					}), WithDebug(true), WithVerbose(true), WithErrWriter(&logs))
					err := tc.call(c)
					if err == nil {
						t.Error("redirect accepted")
					}
					var coded *types.LnkError
					expectedCode := types.ExitNetwork
					if loc.auth {
						expectedCode = types.ExitAuth
					}
					if !errors.As(err, &coded) || coded.Code != expectedCode {
						t.Errorf("expected exit %d, got %v", expectedCode, err)
					}
					if calls != 1 {
						t.Errorf("admissions=%d", calls)
					}
					if strings.Contains(fmt.Sprint(err)+logs.String(), "secret-target") {
						t.Error("redirect target leaked")
					}
				})
			}
		}
	}
}

func TestRequestClientOptions(t *testing.T) {
	for _, timeoutFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(timeoutFirst), func(t *testing.T) {
			redirectCalls := 0
			caller := &http.Client{Timeout: 17 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
				redirectCalls++
				return errors.New("caller redirect callback")
			}, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return fixtureResponse(302, "/login"), nil })}
			before := *caller
			opts := []Option{WithHTTPClient(caller), WithTimeout(2 * time.Second)}
			if timeoutFirst {
				opts[0], opts[1] = opts[1], opts[0]
			}
			c := New(auth.Credentials{}, opts...)
			if c.http == caller || c.http.Timeout != 2*time.Second {
				t.Error("client ownership or explicit timeout lost")
			}
			if caller.Timeout != before.Timeout || reflect.ValueOf(caller.CheckRedirect).Pointer() != reflect.ValueOf(before.CheckRedirect).Pointer() || reflect.ValueOf(caller.Transport).Pointer() != reflect.ValueOf(before.Transport).Pointer() {
				t.Error("caller client mutated")
			}
			_, err := c.Ping()
			var coded *types.LnkError
			if !errors.As(err, &coded) || coded.Code != types.ExitAuth || redirectCalls != 0 {
				t.Errorf("redirect policy bypass: %v, callbacks=%d", err, redirectCalls)
			}
		})
	}
}

func TestRequestMutationFailures(t *testing.T) {
	for _, tc := range requestWrappers() {
		if tc.method == "GET" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("transport synthetic-secret https://external.invalid/private")
			}))
			err := tc.call(c)
			if err == nil || calls != 1 {
				t.Fatalf("error=%v admissions=%d", err, calls)
			}
			if strings.Contains(err.Error(), "synthetic-secret") || strings.Contains(err.Error(), "https://") {
				t.Errorf("transport detail leaked: %v", err)
			}
		})
	}
}

func TestRequestSendSites(t *testing.T) {
	root := filepath.Clean("../..")
	allowed := map[string]bool{"internal/api/request.go:send": false, "internal/api/application.go:Check": false}
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".worktrees" || entry.Name() == "vendor") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			return err
		}
		httpPackages := map[string]bool{}
		for _, imp := range file.Imports {
			name, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if name == "net/http" {
				alias := "http"
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				httpPackages[alias] = true
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			isSend := sel.Sel.Name == "Do" || sel.Sel.Name == "RoundTrip"
			if id, ok := sel.X.(*ast.Ident); ok && httpPackages[id.Name] {
				switch sel.Sel.Name {
				case "Get", "Head", "Post", "PostForm":
					isSend = true
				}
			}
			if !isSend {
				return true
			}
			owner := "<package>"
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Pos() <= sel.Pos() && sel.End() <= fn.End() {
					owner = fn.Name.Name
				}
			}
			rel, err := filepath.Rel(root, filename)
			if err != nil {
				t.Fatal(err)
			}
			key := filepath.ToSlash(rel) + ":" + owner
			if seen, exists := allowed[key]; !exists || seen {
				t.Errorf("unexpected HTTP send site %s", key)
			} else {
				allowed[key] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, found := range allowed {
		if !found {
			t.Errorf("missing expected send site %s", key)
		}
	}
}

func TestRequestDestinations(t *testing.T) {
	invalid := []string{
		"http://www.linkedin.com/voyager/api/me",
		"https://linkedin.com/voyager/api/me",
		"https://www.linkedin.com.evil.invalid/voyager/api/me",
		"https://www.linkedin.com:443/voyager/api/me",
		"https://www.linkedin.com:/voyager/api/me",
		"https://user:synthetic-secret@www.linkedin.com/voyager/api/me",
		"https://www.linkedin.com/voyager/api/me#synthetic-secret",
		"https://www.linkedin.com/voyager/api/me#",
		"https://www.linkedin.com/voyager/api/me%09",
		"https://www.linkedin.com/voyager/api/../../login",
		"https://www.linkedin.com/voyager/api/%2e%2e/me",
		"https://www.linkedin.com/voyager/api/%252e%252e/me",
		"https://www.linkedin.com/voyager/api/x%2fy",
		"https://www.linkedin.com/voyager/api/x%252fy",
		"https://www.linkedin.com/voyager/api/x%5cy",
		"https://www.linkedin.com/voyager/api//me",
		"https://www.linkedin.com/voyager/api/./me",
		"https://www.linkedin.com/voyager/api/me%3fsecret",
		"https://www.linkedin.com/voyager/api/me%23secret",
		"https://www.linkedin.com/voyager/api/me%00",
		"https://www.linkedin.com/voyager/apix/me",
		"https://www.linkedin.com/login",
		"https://www.linkedin.com/voyager/api/%zz",
		"https://external.invalid/voyager/api/me",
		"//www.linkedin.com/voyager/api/me",
	}
	for _, target := range invalid {
		for _, op := range []operation{readOperation, graphQLReadOperation, normalizedReadOperation, postOperation, deleteOperation} {
			t.Run(fmt.Sprintf("%d/%s", op, target), func(t *testing.T) {
				calls := 0
				var logs bytes.Buffer
				c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(200, ""), nil }), WithDebug(true), WithErrWriter(&logs))
				_, err := c.send(context.Background(), op, target, nil)
				if err == nil || calls != 0 {
					t.Fatalf("error=%v admissions=%d", err, calls)
				}
				if strings.Contains(err.Error()+logs.String(), target) || strings.Contains(err.Error()+logs.String(), "synthetic-secret") {
					t.Error("destination leaked")
				}
			})
		}
	}
	for _, target := range []string{baseURL + "/voyager/api/me", baseURL + "/voyager/api/identity/profiles/fixture-name", baseURL + "/voyager/api/subscriptions/(jobPosting:urn%3Ali%3Afsd_jobPosting%3A123)", baseURL + "/voyager/api/me?value=https%3A%2F%2Fexternal.invalid"} {
		if _, err := safeDestination(target); err != nil {
			t.Errorf("safe destination rejected: %s: %v", target, err)
		}
	}
}

func TestRequestPublicPathValidation(t *testing.T) {
	for _, target := range []string{"/login", "/voyager/api/../login", "/voyager/api/%2e%2e/login", "//external.invalid/voyager/api/me", "/voyager/api/me#synthetic-secret", "https://external.invalid/voyager/api/me"} {
		c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Error("unsafe wrapper reached transport")
			return fixtureResponse(200, ""), nil
		}))
		if _, err := c.Get(target, nil); err == nil {
			t.Error("Get accepted unsafe path")
		}
		if _, err := c.Post(target, nil); err == nil {
			t.Error("Post accepted unsafe path")
		}
		if _, err := c.PostJSON(target, nil, nil); err == nil {
			t.Error("PostJSON accepted unsafe path")
		}
		if err := c.Delete(target); err == nil {
			t.Error("Delete accepted unsafe path")
		}
		if err := c.DeleteResource(target); err == nil {
			t.Error("DeleteResource accepted unsafe path")
		}
	}
}

func TestRequestQueryEncoding(t *testing.T) {
	params := map[string]string{"action": "submit & continue?x=1#value", "key&=": "a+b / snow ☃"}
	calls := 0
	c := fixtureClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		for key, value := range params {
			if req.URL.Query().Get(key) != value {
				t.Errorf("query value corrupted for %q", key)
			}
		}
		if req.URL.Fragment != "" {
			t.Error("query created fragment")
		}
		return fixtureResponse(201, ""), nil
	}))
	if _, err := c.PostJSON("/voyager/api/fixture", params, nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("admissions=%d", calls)
	}
}

func TestRequestContextLifecycle(t *testing.T) {
	var request *http.Request
	c := fixtureClient(roundTripFunc(func(req *http.Request) (*http.Response, error) { request = req; return fixtureResponse(200, ""), nil }))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	response, err := c.send(ctx, readOperation, baseURL+"/voyager/api/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	if request.Context().Err() != nil {
		t.Fatal("context canceled before body consumed")
	}
	response.Body.Close()
	if !errors.Is(request.Context().Err(), context.Canceled) {
		t.Fatal("body close did not release context")
	}
}

func TestRequestMutationRateLimits(t *testing.T) {
	for _, tc := range requestWrappers() {
		if tc.method == "GET" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return fixtureResponse(429, ""), nil }))
			err := tc.call(c)
			var coded *types.LnkError
			if !errors.As(err, &coded) || coded.Code != types.ExitRateLimit || calls != 1 {
				t.Fatalf("error=%v admissions=%d", err, calls)
			}
		})
	}
}

func TestRequestClientDefaults(t *testing.T) {
	caller := &http.Client{Timeout: 7 * time.Second}
	for _, c := range []*Client{New(auth.Credentials{}), New(auth.Credentials{}, WithHTTPClient(nil)), New(auth.Credentials{}, WithHTTPClient(caller))} {
		if c.http.CheckRedirect == nil {
			t.Fatal("missing redirect policy")
		}
	}
	c := New(auth.Credentials{}, WithHTTPClient(caller))
	if c.http == caller || c.http.Timeout != caller.Timeout || caller.CheckRedirect != nil {
		t.Fatal("caller settings not preserved")
	}
}

func TestRequestReadRateLimitCompatibility(t *testing.T) {
	for _, tc := range requestWrappers() {
		if tc.name == "ping" || tc.method != "GET" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := fixtureClient(roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return fixtureResponse(429, ""), nil
				}
				return fixtureResponse(200, ""), nil
			}))
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("admissions=%d", calls)
			}
		})
	}
}
