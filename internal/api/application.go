package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/yashiels/linkedin-cli/internal/types"
)

const (
	employerCheckTimeout  = 8 * time.Second
	employerBodyLimit     = 1024 * 1024
	employerRedirectLimit = 3
)

var nonPublicApplicationNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

type employerApplicationChecker struct {
	http     *http.Client
	lookupIP func(context.Context, string) ([]net.IP, error)
	now      func() time.Time
}

func newEmployerApplicationChecker() *employerApplicationChecker {
	checker := &employerApplicationChecker{
		lookupIP: func(ctx context.Context, host string) ([]net.IP, error) {
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			ips := make([]net.IP, 0, len(addresses))
			for _, address := range addresses {
				ips = append(ips, address.IP)
			}
			return ips, nil
		},
		now: time.Now,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = checker.dialContext
	checker.http = &http.Client{Transport: transport, Timeout: employerCheckTimeout}
	return checker
}

func (*Client) CheckApplication(ctx context.Context, detail *types.JobDetail) types.ApplicationAvailability {
	current := detail.Application
	if current.CheckedAt == "" {
		current.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if current.Status == types.ApplicationClosed || current.Status == types.ApplicationAccepting {
		return current
	}
	if current.ApplyURL == "" {
		return current
	}
	return newEmployerApplicationChecker().Check(ctx, current.ApplyURL, current.ApplicantTrackingSystem)
}

func (c *employerApplicationChecker) Check(ctx context.Context, applyURL, ats string) types.ApplicationAvailability {
	provider := applicationProvider(applyURL, ats)
	result := types.ApplicationAvailability{
		Status:                  types.ApplicationUnverified,
		Source:                  "employer-verification",
		Reason:                  "No supported employer-specific proof was available.",
		CheckedAt:               c.now().UTC().Format(time.RFC3339),
		ApplyURL:                applyURL,
		ApplicantTrackingSystem: ats,
	}
	if provider == "" {
		return result
	}

	checkCtx, cancel := context.WithTimeout(ctx, employerCheckTimeout)
	defer cancel()
	currentURL, err := url.Parse(applyURL)
	if err != nil {
		result.Reason = "The employer application URL is invalid."
		return result
	}

	httpClient := *c.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer httpClient.CloseIdleConnections()
	expectedIdentity := applicationIdentity(currentURL, provider)
	expectedHost := strings.ToLower(currentURL.Hostname())
	if expectedIdentity == "" {
		result.Reason = "The employer application URL does not identify a posting."
		return result
	}
	for redirects := 0; redirects <= employerRedirectLimit; redirects++ {
		if err := c.validateURL(checkCtx, currentURL); err != nil {
			if redirects > 0 {
				result.Reason = "The employer redirect was rejected: " + err.Error()
			} else {
				result.Reason = "The employer application URL was rejected: " + err.Error()
			}
			return result
		}
		if applicationProvider(currentURL.String(), ats) != provider {
			result.Reason = "The employer redirect left the verified ATS domain."
			return result
		}
		if strings.ToLower(currentURL.Hostname()) != expectedHost {
			result.Reason = "The employer redirect changed the verified ATS host."
			return result
		}
		if applicationIdentity(currentURL, provider) != expectedIdentity {
			result.Reason = "The employer redirect identified a different posting."
			return result
		}

		request, err := http.NewRequestWithContext(checkCtx, http.MethodGet, currentURL.String(), nil)
		if err != nil {
			result.Reason = "The employer verification request could not be created."
			return result
		}
		request.Header.Set("Accept", "text/html,application/json")
		request.Header.Set("User-Agent", "lnk-application-verifier/1")
		response, err := httpClient.Do(request)
		if err != nil {
			result.Reason = "The employer verification request failed: " + err.Error()
			return result
		}

		if response.StatusCode >= 300 && response.StatusCode < 400 {
			_ = response.Body.Close()
			location := response.Header.Get("Location")
			if location == "" {
				result.Reason = "The employer returned a redirect without a destination."
				return result
			}
			nextURL, err := currentURL.Parse(location)
			if err != nil {
				result.Reason = "The employer returned an invalid redirect."
				return result
			}
			currentURL = nextURL
			continue
		}

		body, tooLarge, err := readEmployerBody(response.Body)
		_ = response.Body.Close()
		if err != nil {
			result.Reason = "The employer response could not be read."
			return result
		}
		if tooLarge {
			result.Reason = "The employer response exceeded the verification size limit."
			return result
		}
		return evaluateEmployerResponseAtURL(result, provider, response.StatusCode, body, currentURL.String())
	}

	result.Reason = "The employer exceeded the redirect limit."
	return result
}

func (c *employerApplicationChecker) validateURL(ctx context.Context, target *url.URL) error {
	if target.Scheme != "https" {
		return fmt.Errorf("HTTPS is required")
	}
	if target.User != nil || target.Hostname() == "" {
		return fmt.Errorf("invalid host")
	}
	if port := target.Port(); port != "" && port != "443" {
		return fmt.Errorf("non-standard port is not allowed")
	}
	addresses, err := c.lookupIP(ctx, target.Hostname())
	if err != nil {
		return fmt.Errorf("DNS lookup failed")
	}
	if len(addresses) == 0 {
		return fmt.Errorf("DNS returned no addresses")
	}
	for _, address := range addresses {
		if !isPublicApplicationIP(address) {
			return fmt.Errorf("destination resolves to a non-public address")
		}
	}
	return nil
}

func (c *employerApplicationChecker) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := c.lookupIP(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if isPublicApplicationIP(ip) {
			return (&net.Dialer{Timeout: employerCheckTimeout}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
	}
	return nil, fmt.Errorf("destination resolves to a non-public address")
}

func isPublicApplicationIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() {
		return false
	}
	for _, network := range nonPublicApplicationNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}

func applicationProvider(rawURL, ats string) string {
	target, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(target.Hostname())
	path := strings.ToLower(strings.TrimSuffix(target.EscapedPath(), "/"))
	switch {
	case strings.EqualFold(ats, "Lever") && host == "jobs.lever.co" && strings.HasSuffix(path, "/apply") && applicationIdentity(target, "lever") != "":
		return "lever"
	case strings.EqualFold(ats, "Workday") && strings.HasSuffix(host, ".myworkdayjobs.com") && applicationIdentity(target, "workday") != "":
		return "workday"
	default:
		return ""
	}
}

func applicationIdentity(target *url.URL, provider string) string {
	parts := strings.Split(strings.Trim(target.EscapedPath(), "/"), "/")
	for index, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err == nil {
			parts[index] = decoded
		}
	}
	switch provider {
	case "lever":
		if len(parts) < 3 || !strings.EqualFold(parts[len(parts)-1], "apply") {
			return ""
		}
		return strings.ToLower(parts[len(parts)-2])
	case "workday":
		jobSegment := -1
		for index, part := range parts {
			if strings.EqualFold(part, "job") {
				jobSegment = index
			}
		}
		if jobSegment < 0 || jobSegment == len(parts)-1 {
			return ""
		}
		return strings.ToLower(parts[len(parts)-1])
	default:
		return ""
	}
}

func readEmployerBody(reader io.Reader) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(reader, employerBodyLimit+1))
	if err != nil {
		return nil, false, err
	}
	if len(body) > employerBodyLimit {
		return nil, true, nil
	}
	return body, false, nil
}

func evaluateEmployerResponse(result types.ApplicationAvailability, provider string, statusCode int, body []byte) types.ApplicationAvailability {
	return evaluateEmployerResponseAtURL(result, provider, statusCode, body, result.ApplyURL)
}

func evaluateEmployerResponseAtURL(result types.ApplicationAvailability, provider string, statusCode int, body []byte, pageURL string) types.ApplicationAvailability {
	lower := strings.ToLower(string(body))
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden || statusCode == http.StatusTooManyRequests || isLoginOrChallengePage(lower) {
		result.Reason = "The employer returned a login or challenge page instead of application proof."
		return result
	}
	if statusCode != http.StatusOK {
		result.Reason = fmt.Sprintf("The employer returned HTTP %d without application proof.", statusCode)
		return result
	}
	switch provider {
	case "lever":
		if hasActiveLeverApplicationForm(body, pageURL) {
			result.Status = types.ApplicationAccepting
			result.Source = "employer-lever"
			result.Evidence = "The employer returned a POST Lever application form with an active submission control for this posting."
			result.Reason = "The Lever application form is currently available."
			return result
		}
		result.Reason = "Lever did not return its live application form and submit control."
	case "workday":
		canApply, externalPaths, ok := parseWorkdayCanApply(body)
		if ok && workdayIdentityMatches(externalPaths, applicationIdentityFromString(pageURL, "workday")) {
			result.Source = "employer-workday"
			result.Evidence = fmt.Sprintf("The employer returned jobPostingInfo.canApply=%t in JSON.", canApply)
			if canApply {
				result.Status = types.ApplicationAccepting
				result.Reason = "Workday job JSON explicitly reports that applications are accepted."
			} else {
				result.Status = types.ApplicationClosed
				result.Reason = "Workday job JSON explicitly reports that applications are closed."
			}
			return result
		}
		result.Reason = "Workday did not return matching JSON jobPostingInfo.canApply proof."
	}
	return result
}

func applicationIdentityFromString(rawURL, provider string) string {
	target, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return applicationIdentity(target, provider)
}

func workdayIdentityMatches(paths []string, expected string) bool {
	if expected == "" {
		return false
	}
	for _, path := range paths {
		target, err := url.Parse(path)
		if err != nil || applicationIdentity(target, "workday") != expected {
			return false
		}
	}
	return true
}

func parseWorkdayCanApply(body []byte) (bool, []string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false, nil, false
	}
	seenTrue := false
	seenFalse := false
	seenInfo := false
	var paths []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, nil, false
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return false, nil, false
		}
		if key != "jobPostingInfo" {
			continue
		}
		infoTrue, infoFalse, infoPaths, ok := parseWorkdayJobPostingInfo(raw)
		if !ok {
			return false, nil, false
		}
		seenInfo = true
		seenTrue = seenTrue || infoTrue
		seenFalse = seenFalse || infoFalse
		paths = append(paths, infoPaths...)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !jsonAtEOF(decoder) || !seenInfo || !seenTrue && !seenFalse {
		return false, nil, false
	}
	if seenFalse {
		return false, paths, true
	}
	return true, paths, true
}

func parseWorkdayJobPostingInfo(raw json.RawMessage) (bool, bool, []string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false, false, nil, false
	}
	seenTrue := false
	seenFalse := false
	var paths []string
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return false, false, nil, false
		}
		switch key {
		case "canApply":
			var value bool
			if err := decoder.Decode(&value); err != nil {
				return false, false, nil, false
			}
			seenTrue = seenTrue || value
			seenFalse = seenFalse || !value
		case "externalPath":
			var value string
			if err := decoder.Decode(&value); err != nil {
				return false, false, nil, false
			}
			paths = append(paths, value)
		default:
			var discarded json.RawMessage
			if err := decoder.Decode(&discarded); err != nil {
				return false, false, nil, false
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !jsonAtEOF(decoder) {
		return false, false, nil, false
	}
	return seenTrue, seenFalse, paths, true
}

func jsonAtEOF(decoder *json.Decoder) bool {
	var extra interface{}
	return decoder.Decode(&extra) == io.EOF
}

func isLoginOrChallengePage(lower string) bool {
	loginTitles := []string{"<title>sign in", "<title>log in", "<title>login", "<title>just a moment", "access denied", "challenge-platform"}
	for _, marker := range loginTitles {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
