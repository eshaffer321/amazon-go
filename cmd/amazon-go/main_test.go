package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	amazon "github.com/eshaffer321/amazon-go"
)

func TestNewClientDoesNotPersistCookiesFromFailedAuthCheck(t *testing.T) {
	cookieFile := filepath.Join(t.TempDir(), "cookies.json")
	original := amazon.CookieFile{
		Cookies: []*amazon.Cookie{
			{Name: "session-id", Value: "original-session", Domain: ".amazon.com", Path: "/"},
			{Name: "session-token", Value: "original-token", Domain: ".amazon.com", Path: "/"},
			{Name: "ubid-main", Value: "original-ubid", Domain: ".amazon.com", Path: "/"},
			{Name: "at-main", Value: "original-at", Domain: ".amazon.com", Path: "/"},
		},
		UpdatedAt: time.Now(),
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if err := os.WriteFile(cookieFile, data, 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	originalTransport := http.DefaultTransport
	http.DefaultTransport = commandRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Set-Cookie": []string{"session-token=poisoned-by-sign-in; Path=/; Domain=.amazon.com"},
			},
			Body:    io.NopCloser(strings.NewReader(`<html><title>Amazon Sign-In</title><input id="ap_email"></html>`)),
			Request: req,
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	client, err := newClient(cookieFile, "")
	if err != nil {
		t.Fatalf("newClient failed: %v", err)
	}
	client.CookieStore().Replace([]*amazon.Cookie{
		{Name: "session-id", Value: "exported-session", Domain: ".amazon.com", Path: "/"},
		{Name: "session-token", Value: "exported-token", Domain: ".amazon.com", Path: "/"},
		{Name: "ubid-main", Value: "exported-ubid", Domain: ".amazon.com", Path: "/"},
		{Name: "at-main", Value: "exported-at", Domain: ".amazon.com", Path: "/"},
	})
	if err := client.HealthCheck(); err == nil {
		t.Fatal("expected failed auth check")
	}

	after, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var stored amazon.CookieFile
	if err := json.Unmarshal(after, &stored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	for _, cookie := range stored.Cookies {
		if cookie.Name == "session-token" {
			if cookie.Value != "original-token" {
				t.Fatalf("failed auth check overwrote session token with %q", cookie.Value)
			}
			return
		}
	}
	t.Fatal("session-token missing after failed auth check")
}

type commandRoundTripFunc func(*http.Request) (*http.Response, error)

func (f commandRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
