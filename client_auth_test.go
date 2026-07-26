package amazon

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClientUsesPersistedBrowserFingerprint(t *testing.T) {
	cookieFile := t.TempDir() + "/cookies.json"
	store, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}
	addEssentialTestCookies(store)
	fingerprint := BrowserFingerprint{
		UserAgent:       "Mozilla/5.0 Chrome/143.0.7499.4 Safari/537.36",
		SecCHUA:         `"Chromium";v="143", "Not A(Brand";v="24"`,
		SecCHUAMobile:   "?0",
		SecCHUAPlatform: `"macOS"`,
	}
	store.SetBrowserFingerprint(fingerprint)
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var requestHeaders http.Header
	restore := replaceDefaultTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestHeaders = req.Header.Clone()
		return ordersPageResponse(req), nil
	}))
	t.Cleanup(restore)

	client, err := NewClient(WithCookieFile(cookieFile), WithAutoSave(false))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	if got := requestHeaders.Get("User-Agent"); got != fingerprint.UserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, fingerprint.UserAgent)
	}
	if got := requestHeaders.Get("Sec-CH-UA"); got != fingerprint.SecCHUA {
		t.Fatalf("Sec-CH-UA = %q, want %q", got, fingerprint.SecCHUA)
	}
	if got := requestHeaders.Get("Sec-CH-UA-Mobile"); got != fingerprint.SecCHUAMobile {
		t.Fatalf("Sec-CH-UA-Mobile = %q, want %q", got, fingerprint.SecCHUAMobile)
	}
	if got := requestHeaders.Get("Sec-CH-UA-Platform"); got != fingerprint.SecCHUAPlatform {
		t.Fatalf("Sec-CH-UA-Platform = %q, want %q", got, fingerprint.SecCHUAPlatform)
	}
}

func TestClientSendsOnlyCookiesApplicableToRequest(t *testing.T) {
	cookieFile := t.TempDir() + "/cookies.json"
	store, err := NewCookieStore(cookieFile)
	if err != nil {
		t.Fatalf("NewCookieStore failed: %v", err)
	}
	addEssentialTestCookies(store)
	store.Set(&Cookie{Name: "expired", Value: "old", Domain: ".amazon.com", Path: "/", Expires: time.Now().Add(-time.Hour).Unix()})
	store.Set(&Cookie{Name: "signin-only", Value: "wrong-path", Domain: ".amazon.com", Path: "/ap/"})
	store.Set(&Cookie{Name: "other-domain", Value: "wrong-domain", Domain: ".example.com", Path: "/"})
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	var cookieHeader string
	restore := replaceDefaultTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cookieHeader = req.Header.Get("Cookie")
		return ordersPageResponse(req), nil
	}))
	t.Cleanup(restore)

	client, err := NewClient(WithCookieFile(cookieFile), WithAutoSave(false))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	if !strings.Contains(cookieHeader, "session-id=session") {
		t.Fatalf("essential cookie missing from request: %q", cookieHeader)
	}
	for _, excluded := range []string{"expired=", "signin-only=", "other-domain="} {
		if strings.Contains(cookieHeader, excluded) {
			t.Fatalf("request included inapplicable cookie %q: %q", excluded, cookieHeader)
		}
	}
}

func addEssentialTestCookies(store *CookieStore) {
	store.Set(&Cookie{Name: "session-id", Value: "session", Domain: ".amazon.com", Path: "/"})
	store.Set(&Cookie{Name: "session-token", Value: "token", Domain: ".amazon.com", Path: "/"})
	store.Set(&Cookie{Name: "ubid-main", Value: "ubid", Domain: ".amazon.com", Path: "/"})
	store.Set(&Cookie{Name: "at-main", Value: "at", Domain: ".amazon.com", Path: "/"})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func replaceDefaultTransport(transport http.RoundTripper) func() {
	original := http.DefaultTransport
	http.DefaultTransport = transport
	return func() {
		http.DefaultTransport = original
	}
}

func ordersPageResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`<html><title>Your Orders</title></html>`)),
		Request:    req,
	}
}
